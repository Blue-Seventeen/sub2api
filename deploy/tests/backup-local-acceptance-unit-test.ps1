# Docker is replaced in this script's scope; no call is forwarded to an executable.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$deployRoot = Split-Path $PSScriptRoot -Parent
$backupScript = Join-Path $deployRoot 'backup-local-acceptance.ps1'
$integrationScript = Join-Path $PSScriptRoot 'backup-local-acceptance-integration-test.ps1'
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('sub2api-backup-unit-' + [guid]::NewGuid().ToString('N'))
$originalLocalAppData = $env:LOCALAPPDATA
$originalDockerHost = $env:DOCKER_HOST
$originalDockerContext = $env:DOCKER_CONTEXT
$env:DOCKER_HOST = $null
$env:DOCKER_CONTEXT = $null
$null = New-Item -ItemType Directory -Path $testRoot
$env:LOCALAPPDATA = $testRoot
$global:BackupUnitfailures = [Collections.Generic.List[string]]::new()

# A real full-duplex local pipe catches native framing/EOF bugs which a
# PowerShell function receiving $input cannot reproduce. Never contacts Docker.
Add-Type -TypeDefinition @'
using System;
using System.Collections.Concurrent;
using System.IO;
using System.IO.Pipes;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
public sealed class BackupEnginePipeFixture : IDisposable {
    public readonly string Name = "sub2api-test-" + Guid.NewGuid().ToString("N");
    public readonly ConcurrentQueue<string> Requests = new ConcurrentQueue<string>();
    public volatile string Mode = "success";
    public volatile bool Chunked;
    public volatile bool Created;
    public int PingCount;
    public volatile string Failure;
    private readonly CancellationTokenSource stop = new CancellationTokenSource();
    private readonly Task loop;
    public BackupEnginePipeFixture() { loop = Task.Run(Run); }
    private static async Task<string> Line(Stream pipe, CancellationToken token) {
        using (var data = new MemoryStream()) {
            var one = new byte[1];
            while (await pipe.ReadAsync(one, 0, 1, token) == 1) {
                data.WriteByte(one[0]);
                if (one[0] == 10) return Encoding.ASCII.GetString(data.ToArray());
                if (data.Length > 65536) throw new Exception("oversized request header");
            }
            throw new Exception("request ended before headers");
        }
    }
    private async Task Run() {
        try {
            while (!stop.IsCancellationRequested) {
                using (var pipe = new NamedPipeServerStream(Name, PipeDirection.InOut, 1, PipeTransmissionMode.Byte, PipeOptions.Asynchronous)) {
                    await pipe.WaitForConnectionAsync(stop.Token);
                    string first = await Line(pipe, stop.Token);
                    bool ping = first == "GET /_ping HTTP/1.1\r\n";
                    if ((!ping && !first.StartsWith("POST /v1.47/containers/create?name=")) || !first.EndsWith(" HTTP/1.1\r\n"))
                        throw new Exception("unexpected request target");
                    int length = -1;
                    string line;
                    while ((line = await Line(pipe, stop.Token)) != "\r\n") {
                        if (line.StartsWith("Content-Length: ")) length = int.Parse(line.Substring(16).Trim());
                    }
                    if (length < 0 || (!ping && length == 0) || length > 1048576) throw new Exception("invalid request byte length");
                    var bytes = new byte[length];
                    for (int read = 0; read < length;) {
                        int count = await pipe.ReadAsync(bytes, read, length - read, stop.Token);
                        if (count == 0) throw new Exception("stdin EOF before request body");
                        read += count;
                    }
                    // No trailing newline and no client half-close before response.
                    using (var probeCancel = CancellationTokenSource.CreateLinkedTokenSource(stop.Token)) {
                        var probe = pipe.ReadAsync(new byte[1], 0, 1, probeCancel.Token);
                        if (await Task.WhenAny(probe, Task.Delay(60, stop.Token)) == probe)
                            throw new Exception(await probe == 0 ? "stdin closed before response" : "extra bytes after request body");
                        probeCancel.Cancel();
                        try { await probe; } catch (OperationCanceledException) { }
                    }
                    if (ping) Interlocked.Increment(ref PingCount);
                    else Requests.Enqueue(Encoding.UTF8.GetString(bytes));
                    if (Mode == "timeout") { await Task.Delay(30000, stop.Token); continue; }
                    bool failResponse = Mode == "failure" || (Mode == "createFailure" && !ping);
                    if (!ping) Created = !failResponse;
                    if (Mode == "lost") continue;
                    string body = ping ? "OK" : "{\"Id\":\"" + new string('c', 64) + "\",\"Warnings\":[]}";
                    string status = ping ? "200 OK" : "201 Created";
                    string response;
                    if (failResponse) response = "HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n";
                    else if (Mode == "malformed") response = "HTTP/1.1 201 Created\r\nContent-Length: invalid\r\n\r\nunit-secret-never-print";
                    else if (Mode == "truncated") response = "HTTP/1.1 201 Created\r\nContent-Length: 500\r\n\r\n{}";
                    else if (Chunked) response = "HTTP/1.1 " + status + "\r\nTransfer-Encoding: chunked\r\n\r\n" + Encoding.UTF8.GetByteCount(body).ToString("x") + ";test=1\r\n" + body + "\r\n0\r\nX-Test: trailer\r\n\r\n";
                    else response = "HTTP/1.1 " + status + "\r\nContent-Length: " + Encoding.UTF8.GetByteCount(body) + "\r\n\r\n" + body;
                    byte[] output = Encoding.UTF8.GetBytes(response);
                    // Split header/body across writes to exercise partial stream reads.
                    await pipe.WriteAsync(output, 0, 17, stop.Token);
                    await pipe.FlushAsync(stop.Token);
                    await Task.Delay(10, stop.Token);
                    await pipe.WriteAsync(output, 17, output.Length - 17, stop.Token);
                    await pipe.FlushAsync(stop.Token);
                    if (Mode != "truncated") {
                        // Do not close first: HTTP framing, not EOF, must finish the client read.
                        await pipe.ReadAsync(new byte[1], 0, 1, stop.Token);
                    }
                }
            }
        } catch (OperationCanceledException) { }
          catch (IOException) when (Mode == "failure" || Mode == "createFailure" || Mode == "malformed" || Mode == "truncated") { }
          catch (Exception ex) { if (!stop.IsCancellationRequested) Failure = ex.Message; }
    }
    public void Dispose() { stop.Cancel(); try { loop.GetAwaiter().GetResult(); } catch { } stop.Dispose(); }
}
'@
$global:BackupUnitpipe = $null

function Sync-PipeFixture {
    if ($null -eq $global:BackupUnitpipe) { return }
    $global:BackupUnitpipe.Chunked = $global:BackupUnitchunkedResponse
    $global:BackupUnitpipe.Mode = if ($global:BackupUnitfailCreate) { 'failure' } elseif ($global:BackupUnitfailureMode -eq 'responseLost') { 'lost' } elseif ($global:BackupUnitfailureMode) { $global:BackupUnitfailureMode } else { 'success' }
    $requests = $global:BackupUnitpipe.Requests.ToArray()
    while ($global:BackupUnitcreateRequests.Count -lt $requests.Length) {
        $body = $requests[$global:BackupUnitcreateRequests.Count] | ConvertFrom-Json -AsHashtable
        $global:BackupUnitcreateRequests.Add($body)
    }
    if ($global:BackupUnitpipe.Created -and $requests.Length -gt 0) {
        $body = $global:BackupUnitcreateRequests[-1]
        $global:BackupUnitcreated = @{ Id = ('c' * 64); Name = 'sub2api-rollback-' + $body.Labels['org.sub2api.rollback.snapshot']; Body = $body }
    }
}

function Assert-True($Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

function Reset-Fixture {
    $env:DOCKER_HOST = $null
    $env:DOCKER_CONTEXT = $null
    if ($global:BackupUnitpipe) { $global:BackupUnitpipe.Dispose() }
    $global:BackupUnitpipe = [BackupEnginePipeFixture]::new()
    $global:BackupUnitendpoint = 'npipe:////./pipe/' + $global:BackupUnitpipe.Name
    $global:BackupUnitcalls = [Collections.Generic.List[object]]::new()
    $global:BackupUnitoutput = [Collections.Generic.List[string]]::new()
    $global:BackupUnitappRunning = $true
    $global:BackupUnithealth = 'healthy'
    $global:BackupUnitfailStart = $false
    $global:BackupUnitfailDump = $false
    $global:BackupUnitfailLoad = $false
    $global:BackupUnitwrongServer = $false
    $global:BackupUnitwrongDatabase = $false
    $global:BackupUnitwrongLocalDatabase = $false
    $global:BackupUnitdatabasePort = 5432
    $global:BackupUnitappId = 'original-app-id'
    $global:BackupUnitimageId = 'sha256:' + ('a' * 64)
    $global:BackupUnitsavedImage = ''
    $global:BackupUnitoriginalRemoved = $false
    $global:BackupUnitreplacementRunning = $false
    $global:BackupUnitcreated = $null
    $global:BackupUnitcreateRequests = [Collections.Generic.List[object]]::new()
    $global:BackupUnitfailCreate = $false
    $global:BackupUnitdaemonId = 'fixture-daemon'
    $global:BackupUnitnetworkId = 'fixture-network-id'
    $global:BackupUnitmissingVolume = $false
    $global:BackupUnitrelativeBind = $false
    $global:BackupUnitchunkedResponse = $false
    $global:BackupUnitomitMounts = $false
    $global:BackupUnitadditionalNetwork = $false
    $global:BackupUnitfailureMode = ''
    $global:BackupUnitnoPersistentData = $false
    $global:BackupUnitappEnvironment = @(
        'DATABASE_HOST=postgres', 'DATABASE_PORT=5432', 'DATABASE_USER=app-user',
        'DATABASE_PASSWORD=unit-secret-never-print', 'DATABASE_DBNAME=selected-app-db',
        'DATABASE_SSLMODE=disable', 'LITERAL_SECRET=unit-secret-$literal', "MULTILINE_SECRET=unit-secret-line1`nline2"
    )
}

function docker {
    Sync-PipeFixture
    $arguments = @($args | ForEach-Object { [string]$_ })
    $global:BackupUnitcalls.Add($arguments)
    $global:LASTEXITCODE = 0
    switch ($arguments[0]) {
        'context' {
            if ($arguments[1] -eq 'show') { return 'unit-context' }
            if ($arguments[1] -eq 'inspect') { return (@{ Host = $global:BackupUnitendpoint; SkipTLSVerify = $false } | ConvertTo-Json -Compress) }
            throw 'Unexpected context operation'
        }
        'inspect' {
            $name = $arguments[-1]
            $isCreated = $null -ne $global:BackupUnitcreated -and $name -in @($global:BackupUnitcreated.Name, $global:BackupUnitcreated.Id)
            $isApp = $name -in @('app', 'original-app-id') -or $isCreated
            if ($name -eq 'original-app-id' -and $global:BackupUnitoriginalRemoved) { $global:LASTEXITCODE = 1; return }
            $container = @{
                Id = $(if ($isCreated) { $global:BackupUnitcreated.Id } elseif ($isApp) { $global:BackupUnitappId } else { "$name-id" })
                Name = $(if ($isCreated) { '/' + $global:BackupUnitcreated.Name } elseif ($isApp) { '/app' } else { "/$name" })
                Image = $global:BackupUnitimageId
                Config = @{
                    Image = 'example/app:mutable'
                    Env = $(if ($isApp) { $global:BackupUnitappEnvironment } else { @('POSTGRES_USER=postgres', 'POSTGRES_DB=wrong-default-db') })
                    Labels = @{ 'private.fixture' = 'unit-secret-label'; 'com.docker.compose.project' = 'original-project' }
                    Entrypoint = @('/app/docker-entrypoint.sh')
                    Cmd = @('/app/sub2api')
                    User = '1000:1000'
                    WorkingDir = '/app'
                    Healthcheck = @{ Test = @('CMD-SHELL', 'wget -q -O /dev/null http://localhost:8080/health'); Interval = 30000000000 }
                    ExposedPorts = @{ '8080/tcp' = @{} }
                    Volumes = @{ '/app/data' = @{}; '/app/cache' = @{} }
                }
                HostConfig = @{
                    NetworkMode = 'selected'; Binds = @('/srv/sub2api/app data:/app/data:rw', 'app-cache-existing:/app/cache:ro')
                    Mounts = @(); Tmpfs = @{}; VolumesFrom = @(); Links = @(); AutoRemove = $false
                    Privileged = $false; ReadonlyRootfs = $true; SecurityOpt = @('no-new-privileges:true')
                    CapDrop = @('NET_RAW'); RestartPolicy = @{ Name = 'unless-stopped'; MaximumRetryCount = 0 }
                    PortBindings = @{ '8080/tcp' = @(@{ HostIp = '127.0.0.1'; HostPort = '18080' }) }
                    PublishAllPorts = $false; LogConfig = @{ Type = 'json-file'; Config = @{ 'max-size' = '10m' } }
                }
                Mounts = @(
                    @{ Type = 'bind'; Source = $(if ($global:BackupUnitrelativeBind) { 'relative/data' } else { '/srv/sub2api/app data' }); Destination = '/app/data'; RW = $true; Propagation = 'rprivate'; Mode = 'rw' },
                    @{ Type = 'volume'; Name = 'app-cache-existing'; Source = '/var/lib/docker/volumes/app-cache-existing/_data'; Destination = '/app/cache'; RW = $false; Mode = 'ro' }
                )
                State = @{ Running = $(if ($isApp) { $global:BackupUnitappRunning } else { $true }); Health = @{ Status = $global:BackupUnithealth } }
                NetworkSettings = @{
                    Networks = @{ selected = @{ NetworkID = 'fixture-network-id'; IPAddress = '172.29.0.3'; GlobalIPv6Address = ''; Aliases = @('app', 'sub2api'); IPAMConfig = $null; DriverOpts = $null } }
                    Ports = @{ '8080/tcp' = @(@{ HostIp = '127.0.0.1'; HostPort = '18080' }) }
                }
            }
            if ($isCreated) { $container.Config = $global:BackupUnitcreated.Body }
            if ($isApp -and $global:BackupUnitomitMounts) { $container.HostConfig.Remove('Mounts') }
            if ($isApp -and $global:BackupUnitnoPersistentData) { $container.Mounts = @($container.Mounts | Where-Object { $_.Destination -ne '/app/data' }) }
            if ($isApp -and $global:BackupUnitadditionalNetwork) {
                $container.NetworkSettings.Networks['frontend'] = @{
                    NetworkID = 'frontend-network-id'; IPAddress = '172.30.0.20'; GlobalIPv6Address = ''
                    Aliases = @('frontend-app'); IPAMConfig = @{ IPv4Address = '172.30.0.20' }; DriverOpts = @{ 'test-option' = 'preserved' }
                }
            }
            return (ConvertTo-Json -InputObject @($container) -Depth 10 -Compress)
        }
        'info' { return (@{ ID = $global:BackupUnitdaemonId; OSType = 'linux' } | ConvertTo-Json -Compress) }
        'version' { return '1.47' }
        'network' {
            if ($arguments[-1] -eq 'frontend-network-id') { return (ConvertTo-Json -InputObject @(@{ Id = 'frontend-network-id'; Name = 'frontend' }) -Compress) }
            return (ConvertTo-Json -InputObject @(@{ Id = $global:BackupUnitnetworkId; Name = 'selected' }) -Compress)
        }
        'volume' {
            if ($global:BackupUnitmissingVolume) { $global:LASTEXITCODE = 1; return }
            return (ConvertTo-Json -InputObject @(@{ Name = 'app-cache-existing'; Driver = 'local'; Mountpoint = '/var/lib/docker/volumes/app-cache-existing/_data'; CreatedAt = '2026-01-01T00:00:00Z'; Options = $null }) -Compress)
        }
        'container' {
            Assert-True ($arguments[1] -eq 'ls') 'Unexpected container command'
            if (-not $global:BackupUnitoriginalRemoved) { @{ ID = 'original-app-id'; Names = 'app'; State = 'exited' } | ConvertTo-Json -Compress }
            else { @{ ID = 'replacement-id'; Names = 'app'; State = $(if ($global:BackupUnitreplacementRunning) { 'running' } else { 'exited' }) } | ConvertTo-Json -Compress }
            if ($global:BackupUnitcreated) { @{ ID = $global:BackupUnitcreated.Id; Names = $global:BackupUnitcreated.Name; State = 'exited' } | ConvertTo-Json -Compress }
            return
        }
        'system' { throw 'Local npipe recovery must not invoke dial-stdio.' }
        'rm' { Assert-True (-not $global:BackupUnitappRunning) 'Test must stop app before removal'; $global:BackupUnitoriginalRemoved = $true; return }
        'image' {
            $id = if ($arguments[-1] -eq $global:BackupUnitimageId) { $global:BackupUnitimageId } else { 'sha256:' + ('b' * 64) }
            return (ConvertTo-Json -InputObject @(@{ Id = $id; RepoDigests = @('example/app@sha256:' + ('b' * 64)) }) -Compress)
        }
        'stop' { $global:BackupUnitappRunning = $false; return }
        'start' {
            if ($global:BackupUnitfailStart) { $global:LASTEXITCODE = 1; return }
            $global:BackupUnitappRunning = $true
            return
        }
        'exec' {
            $command = $arguments -join ' '
            if ($command -match 'pg_dump') {
                if ($global:BackupUnitfailDump) { $global:LASTEXITCODE = 1; return }
            } elseif ($command -match 'psql' -and $arguments[1] -in @('app', 'original-app-id')) {
                return (@{
                    database = $(if ($global:BackupUnitwrongDatabase) { 'wrong-db' } else { 'selected-app-db' })
                    serverAddress = $(if ($global:BackupUnitwrongServer) { '172.29.0.99' } else { '172.29.0.3' })
                    serverPort = $global:BackupUnitdatabasePort
                } | ConvertTo-Json -Compress)
            } elseif ($command -match 'psql') {
                Assert-True ($arguments -contains 'selected-app-db') 'Database validation must select the application database'
                if ($global:BackupUnitwrongLocalDatabase) { return 'wrong-local-db' }
                return 'selected-app-db'
            } elseif ($command -match 'PING') { return 'PONG' }
            return
        }
        'cp' {
            $destination = $arguments[-1]
            if ($arguments[1] -like '*/app/data') {
                $null = New-Item -ItemType Directory -Path $destination
                [IO.File]::WriteAllText((Join-Path $destination 'config.yaml'), 'fixture: true')
            } else { [IO.File]::WriteAllText($destination, 'mock archive') }
            return
        }
        'save' {
            $global:BackupUnitsavedImage = $arguments[-1]
            [IO.File]::WriteAllText($arguments[2], $global:BackupUnitsavedImage)
            return
        }
        'load' {
            if ($global:BackupUnitfailLoad) { $global:LASTEXITCODE = 1 }
            return
        }
        default { throw "Unexpected mocked Docker command: $($arguments[0])" }
    }
}

function Start-Sleep { }

function Invoke-Backup([string]$Path, [switch]$KeepAppStopped) {
    & $backupScript -AppContainer app -PostgresContainer postgres -RedisContainer redis `
        -OutputDirectory $Path -KeepAppStopped:$KeepAppStopped *>&1 |
        ForEach-Object { $global:BackupUnitoutput.Add([string]$_) }
}

function Run-Test([string]$Name, [scriptblock]$Body) {
    Reset-Fixture
    try { & $Body; Assert-True (-not $global:BackupUnitpipe.Failure) 'Native pipe fixture detected invalid request framing/EOF'; Write-Output "PASS: $Name" }
    catch { $global:BackupUnitfailures.Add("${Name}: $($_.Exception.Message)"); Write-Output "FAIL: $Name" }
    finally { $global:BackupUnitpipe.Dispose(); $global:BackupUnitpipe = $null }
}

function Get-TransportHelpers {
    $tokens = $null
    $errors = $null
    $ast = [Management.Automation.Language.Parser]::ParseFile($backupScript, [ref]$tokens, [ref]$errors)
    if ($errors.Count -gt 0) { throw 'Backup source did not parse.' }
    $assignment = $ast.Find({ param($node)
        $node -is [Management.Automation.Language.AssignmentStatementAst] -and
        $node.Left.Extent.Text -eq '$script:EngineTransportHelpers'
    }, $true)
    if (-not $assignment) { throw 'Shared production transport helper is missing.' }
    . ([scriptblock]::Create($assignment.Extent.Text))
    return $script:EngineTransportHelpers
}

try {
    Run-Test 'non-npipe fallback keeps native stdin open and suppresses stderr' {
        . ([scriptblock]::Create((Get-TransportHelpers)))
        function New-DockerEngineProcess {
            $process = [Diagnostics.Process]::new()
            $process.StartInfo.FileName = (Get-Command pwsh -CommandType Application).Source
            $child = @'
$inputPipe = [Console]::OpenStandardInput()
$one = [byte[]]::new(1)
$header = [Collections.Generic.List[byte]]::new()
while ($inputPipe.Read($one, 0, 1) -eq 1) {
    $header.Add($one[0])
    if ($header.Count -ge 4 -and [Text.Encoding]::ASCII.GetString($header.ToArray()).EndsWith("`r`n`r`n")) { break }
}
$check = $inputPipe.ReadAsync($one, 0, 1)
[Threading.Thread]::Sleep(100)
if ($check.IsCompleted) {
    [Console]::Out.Write("HTTP/1.1 500 Internal Server Error`r`nContent-Length: 0`r`n`r`n")
} else {
    [Console]::Error.Write('unit-secret-native-stderr')
    [Console]::Out.Write("HTTP/1.1 200 OK`r`nContent-Length: 2`r`n`r`nOK")
    [Console]::Out.Flush()
    [Threading.Thread]::Sleep(30000)
}
'@
            foreach ($arg in @('-NoLogo', '-NoProfile', '-EncodedCommand', [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($child)))) {
                $process.StartInfo.ArgumentList.Add($arg)
            }
            $process.StartInfo.UseShellExecute = $false
            $process.StartInfo.CreateNoWindow = $true
            $process.StartInfo.RedirectStandardInput = $true
            $process.StartInfo.RedirectStandardOutput = $true
            $process.StartInfo.RedirectStandardError = $true
            $global:BackupUnitchild = $process
            return $process
        }
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $result = @(Invoke-DockerEngineRequest -Endpoint 'unix:///fixture.sock' -Method GET -Path '/_ping' -TimeoutSeconds 5 *>&1)
        Assert-True ($result.Count -eq 1 -and $result[0] -ceq 'OK') 'Native process transport closed stdin early or leaked stderr/task results'
        Assert-True ($watch.Elapsed.TotalSeconds -lt 10) 'Native fallback waited for EOF instead of HTTP framing'
    }

    Run-Test 'explicit Docker context takes precedence over DOCKER_HOST' {
        . ([scriptblock]::Create((Get-TransportHelpers)))
        $env:DOCKER_HOST = 'npipe:////./pipe/wrong-host'
        $env:DOCKER_CONTEXT = 'unit-context'
        Assert-True ((Get-DockerEngineEndpoint) -ceq $global:BackupUnitendpoint) 'Context precedence diverged from Docker CLI'
    }

    Run-Test 'native pipe transport keeps private request bytes open through the response' {
        $path = Join-Path $testRoot 'native-transport'
        $global:BackupUnitappEnvironment += 'UNICODE_TEST=' + [char]0x4E2D + [char]0x6587
        Invoke-Backup $path -KeepAppStopped
        $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
        $global:BackupUnitoriginalRemoved = $true
        & $manifest.appRollbackScript -ConfirmAppWritersStopped | Out-Null
        Assert-True ($global:BackupUnitpipe.Requests.Count -eq 1) 'Native named-pipe server never received the create request'
        $body = $global:BackupUnitpipe.Requests.ToArray()[0] | ConvertFrom-Json -AsHashtable
        Assert-True ($body.Env -contains ('UNICODE_TEST=' + [char]0x4E2D + [char]0x6587)) 'UTF-8 private request changed in transport'
        Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'system' }).Count -eq 0) 'Local npipe recovery used broken dial-stdio transport'
    }

    foreach ($scenario in @('changedEndpoint', 'remotePipe', 'timeout', 'malformed', 'truncated', 'failure')) {
        Run-Test "native pipe transport fails closed without secrets: $scenario" {
            $path = Join-Path $testRoot "native-$scenario"
            Invoke-Backup $path -KeepAppStopped
            $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
            $global:BackupUnitoriginalRemoved = $true
            if ($scenario -eq 'changedEndpoint') { $env:DOCKER_HOST = 'npipe:////./pipe/different-endpoint' }
            elseif ($scenario -eq 'remotePipe') { $global:BackupUnitendpoint = 'npipe:////remote-host/pipe/engine' }
            elseif ($scenario -eq 'failure') { $global:BackupUnitfailCreate = $true }
            else { $global:BackupUnitfailureMode = $scenario }
            $watch = [Diagnostics.Stopwatch]::StartNew()
            $failed = $false
            try { & $manifest.appRollbackScript -ConfirmAppWritersStopped -EngineTimeoutSeconds 1 *>&1 | ForEach-Object { $global:BackupUnitoutput.Add([string]$_) } }
            catch { $failed = $true; $global:BackupUnitoutput.Add([string]$_.Exception.Message) }
            Assert-True ($failed -and $watch.Elapsed.TotalSeconds -lt 10) 'Native transport failure did not terminate promptly'
            Assert-True (($global:BackupUnitoutput -join "`n") -notmatch 'unit-secret') 'Transport diagnostic leaked secret content'
            Assert-True (Test-Path -LiteralPath $manifest.appImageArchive) 'Transport failure removed recovery image'
            Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'start' }).Count -eq 0) 'Transport failure started app'
            if ($scenario -in @('changedEndpoint', 'remotePipe')) { Assert-True ($global:BackupUnitpipe.Requests.Count -eq 0) 'Mismatched endpoint received private request' }
            if ($scenario -eq 'failure') { Assert-True (($global:BackupUnitoutput -join "`n") -match 'HTTP 500') 'Safe HTTP status was discarded' }
        }
    }

    Run-Test 'recreation integration transport preflight fails before original removal' {
        $env:LOCALAPPDATA = Join-Path $testRoot 'integration-preflight'
        $global:BackupUnitfailCreate = $true
        $failed = $false
        try { & $integrationScript -AppContainer app -PostgresContainer postgres -RedisContainer redis -RecreateApp *>&1 | ForEach-Object { $global:BackupUnitoutput.Add([string]$_) } }
        catch { $failed = $true }
        Assert-True ($failed -and $global:BackupUnitpipe.PingCount -gt 0) 'Integration did not test Engine transport'
        Assert-True (-not $global:BackupUnitoriginalRemoved) 'Integration removed original before transport validation'
        Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'rm' }).Count -eq 0) 'Failed preflight removed original'
        Assert-True $global:BackupUnitappRunning 'Failed preflight did not recover original app'
    }

    Run-Test 'application database overrides POSTGRES_DB; moved tag is never archived' {
        $path = Join-Path $testRoot 'selected-database'
        Invoke-Backup $path -KeepAppStopped
        $dumpCall = @($global:BackupUnitcalls | Where-Object { ($_[0] -eq 'exec') -and (($_ -join ' ') -match 'pg_dump') })[-1]
        Assert-True ($dumpCall -contains 'selected-app-db') 'pg_dump must receive the application database, not POSTGRES_DB'
        Assert-True (($dumpCall -join ' ') -notmatch '\$POSTGRES_DB') 'pg_dump must not use the initialization database'
        $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
        Assert-True ($manifest.postgresDatabase -eq 'selected-app-db') 'Manifest must identify the validated application database'
        Assert-True ($global:BackupUnitsavedImage -eq $global:BackupUnitimageId) 'Archive must use the running image ID even if its configured tag moved'
        Assert-True ($manifest.appRollbackImage -eq $global:BackupUnitimageId) 'Rollback must use the archived image ID'
        Assert-True ((Get-Content (Join-Path $path 'manifest.json') -Raw) -notmatch 'unit-secret') 'Manifest leaked a secret'
        Assert-True (($global:BackupUnitoutput -join "`n") -notmatch 'unit-secret') 'Output leaked a secret'
        Assert-True (-not $global:BackupUnitappRunning) 'KeepAppStopped was ignored'
        $global:BackupUnitrecoveryPath = $path
    }

    Run-Test 'archive uses the running image when the configured tag moved' {
        Invoke-Backup (Join-Path $testRoot 'moved-tag') -KeepAppStopped
        Assert-True ($global:BackupUnitsavedImage -eq $global:BackupUnitimageId) 'Saved a different image through the moved tag'
    }

    Run-Test 'database probe and dump pin the selected PostgreSQL port' {
        $global:BackupUnitdatabasePort = 55432
        $global:BackupUnitappEnvironment = @($global:BackupUnitappEnvironment | ForEach-Object { $_.Replace('DATABASE_PORT=5432', 'DATABASE_PORT=55432') })
        Invoke-Backup (Join-Path $testRoot 'custom-port') -KeepAppStopped
        $databaseCalls = @($global:BackupUnitcalls | Where-Object { $_[0] -eq 'exec' -and $_[1] -eq 'postgres-id' -and ($_ -join ' ') -match 'psql|pg_dump' })
        Assert-True ($databaseCalls.Count -eq 2) 'Database must be validated and dumped'
        foreach ($call in $databaseCalls) {
            Assert-True ($call -contains '55432') 'Selected PostgreSQL port was not passed to the database command'
            Assert-True (($call -join ' ') -match 'PGPORT=') 'Database command did not override inherited/default PGPORT'
        }
        foreach ($call in @($global:BackupUnitcalls | Where-Object { $_[0] -eq 'exec' -and $_ -contains 'sh' })) {
            Assert-True (-not ($call -join ' ').Contains("`r")) 'Linux shell received Windows CRLF line endings'
        }
    }

    foreach ($scenario in @('wrongServer', 'wrongDatabase', 'wrongLocalDatabase', 'missingDatabase')) {
        Run-Test "reject $scenario before stopping the application" {
            switch ($scenario) {
                wrongServer { $global:BackupUnitwrongServer = $true }
                wrongDatabase { $global:BackupUnitwrongDatabase = $true }
                wrongLocalDatabase { $global:BackupUnitwrongLocalDatabase = $true }
                missingDatabase { $global:BackupUnitappEnvironment = @($global:BackupUnitappEnvironment | Where-Object { $_ -notlike 'DATABASE_DBNAME=*' }) }
            }
            $failed = $false
            try { Invoke-Backup (Join-Path $testRoot $scenario) } catch { $failed = $true }
            Assert-True $failed 'Unsafe database selection was accepted'
            Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'stop' }).Count -eq 0) 'Validation must fail before stopping the application'
        }
    }

    Run-Test 'restart failure retains backup and never reports success' {
        $global:BackupUnitfailStart = $true
        $path = Join-Path $testRoot 'restart-failure'
        $failed = $false
        try { Invoke-Backup $path } catch { $failed = $true }
        Assert-True $failed 'Restart failure was swallowed'
        Assert-True (Test-Path (Join-Path $path 'postgres.dump')) 'Recovery archive was removed'
        Assert-True (($global:BackupUnitoutput -join "`n") -notmatch 'Verified PostgreSQL') 'Success was printed before restart'
    }

    Run-Test 'rollback uses absolute paths and fails before start when load fails' {
        $path = Join-Path $testRoot "rollback path's space"
        Invoke-Backup $path -KeepAppStopped
        $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
        Assert-True ([IO.Path]::IsPathFullyQualified($manifest.appRollbackScript)) 'Rollback script path is not absolute'
        $global:BackupUnitfailLoad = $true
        $failed = $false
        try { & ([scriptblock]::Create($manifest.appRollbackCommand)) } catch { $failed = $true }
        Assert-True $failed 'Rollback ignored a failed docker load'
        Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'start' }).Count -eq 0) 'Rollback started after load failure'
        $load = @($global:BackupUnitcalls | Where-Object { $_[0] -eq 'load' })[-1]
        Assert-True ($load[-1] -eq (Join-Path $path 'app-image.tar')) 'Rollback archive path depends on working directory'
        $global:BackupUnitfailLoad = $false
        $global:BackupUnitappId = 'replacement-container-id'
        $failed = $false
        try { & ([scriptblock]::Create($manifest.appRollbackCommand)) } catch { $failed = $true }
        Assert-True $failed 'Rollback accepted a replaced application container'
        Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'start' }).Count -eq 0) 'Rollback started the replacement container'
    }

    foreach ($scenario in @('healthy', 'restartFailure', 'unhealthy', 'corruptArchive')) {
        Run-Test "retained-container rollback: $scenario" {
            $path = Join-Path $testRoot "rollback-$scenario"
            Invoke-Backup $path -KeepAppStopped
            $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
            if ($scenario -eq 'restartFailure') { $global:BackupUnitfailStart = $true }
            if ($scenario -eq 'unhealthy') { $global:BackupUnithealth = 'unhealthy' }
            if ($scenario -eq 'corruptArchive') { [IO.File]::AppendAllText($manifest.appImageArchive, 'corrupted') }
            $failed = $false
            try {
                Push-Location $deployRoot
                & ([scriptblock]::Create($manifest.appRollbackCommand)) *>&1 |
                    ForEach-Object { $global:BackupUnitoutput.Add([string]$_) }
            } catch { $failed = $true }
            finally { Pop-Location }
            $passed = ($global:BackupUnitoutput -join "`n") -match 'rollback passed health verification'
            if ($scenario -eq 'healthy') {
                Assert-True (-not $failed -and $passed -and $global:BackupUnitappRunning) 'Retained original container did not recover'
            } else {
                Assert-True ($failed -and -not $passed) 'Rollback reported success on failure'
                Assert-True (Test-Path -LiteralPath $manifest.appImageArchive) 'Rollback deleted its recovery archive'
            }
            if ($scenario -eq 'corruptArchive') {
                Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -in @('load', 'start') }).Count -eq 0) 'Corrupt archive was loaded or started'
            }
            Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'compose' }).Count -eq 0) 'Recovery must not guess Compose configuration'
        }
    }

    Run-Test 'private recovery definition pins immutable image and resolved configuration' {
        $path = Join-Path $testRoot 'private-definition'
        Invoke-Backup $path -KeepAppStopped
        $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
        Assert-True ([IO.Path]::IsPathFullyQualified($manifest.appRecoveryDefinition)) 'Missing absolute recovery definition'
        $definition = Get-Content -LiteralPath $manifest.appRecoveryDefinition -Raw | ConvertFrom-Json -AsHashtable
        $body = $definition.createBody
        Assert-True ($body.Image -eq $global:BackupUnitimageId) 'Recovery definition used a mutable image'
        Assert-True ($body.Env -contains 'LITERAL_SECRET=unit-secret-$literal') 'Environment interpolation changed a secret'
        Assert-True ($body.Env -contains "MULTILINE_SECRET=unit-secret-line1`nline2") 'Multiline environment was lost'
        Assert-True ($body.Entrypoint[0] -eq '/app/docker-entrypoint.sh' -and $body.Cmd[0] -eq '/app/sub2api') 'Entrypoint or command was lost'
        Assert-True ($body.User -eq '1000:1000' -and $body.WorkingDir -eq '/app') 'Execution identity was lost'
        Assert-True ($body.HostConfig.ReadonlyRootfs -and $body.HostConfig.SecurityOpt -contains 'no-new-privileges:true') 'Security options were lost'
        Assert-True ($body.HostConfig.Mounts[0].Source -eq '/srv/sub2api/app data') 'Bind source must use its resolved daemon-absolute path'
        Assert-True ($body.HostConfig.Mounts[0].BindOptions.CreateMountpoint -eq $false) 'Recovery must not create missing bind directories'
        Assert-True ($definition.engineEndpoint -ceq $global:BackupUnitendpoint) 'Recovery definition did not capture effective Docker endpoint'
        Assert-True ($body.HostConfig.Mounts[0].Target -eq '/app/data') 'Bind target was lost'
        Assert-True ($body.HostConfig.Mounts[1].Source -eq 'app-cache-existing' -and $body.HostConfig.Mounts[1].ReadOnly) 'Existing volume identity or mode was lost'
        Assert-True ($body.HostConfig.PortBindings['8080/tcp'][0].HostIp -eq '127.0.0.1' -and $body.HostConfig.PortBindings['8080/tcp'][0].HostPort -eq '18080') 'Resolved port binding was lost'
        Assert-True ($body.HostConfig.NetworkMode -eq 'fixture-network-id') 'Primary network was not pinned'
        Assert-True ($body.NetworkingConfig.EndpointsConfig['fixture-network-id'].Aliases -contains 'sub2api') 'Network alias was lost'
        Assert-True (-not $body.Labels.ContainsKey('com.docker.compose.project')) 'Recreated container must not impersonate the original Compose project'
        Assert-True (@($manifest.files | Where-Object { $_.name -eq 'app-container.private.json' }).Count -eq 1) 'Private recovery definition is not checksummed'
        foreach ($file in @('manifest.json', 'rollback-app.ps1', 'SHA256SUMS')) {
            Assert-True ((Get-Content -LiteralPath (Join-Path $path $file) -Raw) -notmatch 'unit-secret') 'Public recovery metadata leaked a secret'
        }
        if ($IsWindows) {
            Assert-True ((Get-Acl -LiteralPath $path).AreAccessRulesProtected) 'Private backup directory inherits broad permissions'
        } else {
            Assert-True ([int][IO.File]::GetUnixFileMode($manifest.appRecoveryDefinition) -eq 384) 'Private definition must be mode 0600'
        }
    }

    foreach ($chunked in @($false, $true)) {
        Run-Test "removed original is reconstructed through private stdin (chunked=$chunked)" {
            $path = Join-Path $testRoot "recreate-$chunked path's space"
            Invoke-Backup $path -KeepAppStopped
            $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
            $global:BackupUnitoriginalRemoved = $true
            $global:BackupUnitchunkedResponse = $chunked
            Push-Location $deployRoot
            try {
                & $manifest.appRollbackScript -ConfirmAppWritersStopped *>&1 | ForEach-Object { $global:BackupUnitoutput.Add([string]$_) }
                & $manifest.appRollbackScript -ConfirmAppWritersStopped *>&1 | ForEach-Object { $global:BackupUnitoutput.Add([string]$_) }
            } finally { Pop-Location }
            Assert-True ($global:BackupUnitcreateRequests.Count -eq 1) 'Recovery must create once and reuse its own recovery container on retry'
            Assert-True ($global:BackupUnitcreateRequests[0].Image -eq $global:BackupUnitimageId) 'Recreation did not use the archived image'
            Assert-True ($global:BackupUnitcreateRequests[0].Env -contains 'DATABASE_PASSWORD=unit-secret-never-print') 'Private environment did not reach Docker'
            Assert-True ($global:BackupUnitcreated.Name -ne 'app') 'Recovery would overwrite the stopped replacement container'
            Assert-True ($global:BackupUnitappRunning) 'Recreated container did not start'
            Assert-True (($global:BackupUnitoutput -join "`n") -match 'rollback passed health verification') 'Recreated container did not pass health verification'
            Assert-True (($global:BackupUnitoutput -join "`n") -notmatch 'unit-secret') 'Recovery output leaked secrets'
            Assert-True ((($global:BackupUnitcalls | ForEach-Object { $_ -join ' ' }) -join "`n") -notmatch 'unit-secret') 'Recovery exposed environment values in Docker arguments'
        }
    }

    foreach ($scenario in @('noAcknowledgement', 'replacementRunning', 'changedDaemon', 'changedNetwork', 'missingVolume', 'corruptDefinition', 'createFailure')) {
        Run-Test "reconstruction fails closed and retains artifacts: $scenario" {
            $path = Join-Path $testRoot "recreate-failure-$scenario"
            Invoke-Backup $path -KeepAppStopped
            $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
            $global:BackupUnitoriginalRemoved = $true
            switch ($scenario) {
                replacementRunning { $global:BackupUnitreplacementRunning = $true }
                changedDaemon { $global:BackupUnitdaemonId = 'other-daemon' }
                changedNetwork { $global:BackupUnitnetworkId = 'other-network' }
                missingVolume { $global:BackupUnitmissingVolume = $true }
                corruptDefinition { [IO.File]::AppendAllText($manifest.appRecoveryDefinition, 'corrupt') }
                createFailure { $global:BackupUnitfailCreate = $true }
            }
            $failed = $false
            try { & $manifest.appRollbackScript -ConfirmAppWritersStopped:($scenario -ne 'noAcknowledgement') *>&1 | ForEach-Object { $global:BackupUnitoutput.Add([string]$_) } }
            catch { $failed = $true; $global:BackupUnitoutput.Add([string]$_.Exception.Message) }
            Assert-True $failed 'Unsafe recovery was accepted'
            Assert-True (Test-Path -LiteralPath $manifest.appImageArchive) 'Image archive was removed on failure'
            Assert-True (Test-Path -LiteralPath $manifest.appRecoveryDefinition) 'Private definition was removed on failure'
            Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'start' }).Count -eq 0) 'Failed reconstruction started an application'
            Assert-True (($global:BackupUnitoutput -join "`n") -notmatch 'unit-secret|rollback passed') 'Failed recovery leaked a secret or reported success'
            if ($scenario -ne 'createFailure') { Assert-True ($global:BackupUnitcreateRequests.Count -eq 0) 'Preflight failure still created a container' }
        }
    }

    Run-Test 'relative bind source is rejected before application downtime' {
        $global:BackupUnitrelativeBind = $true
        $failed = $false
        try { Invoke-Backup (Join-Path $testRoot 'relative-bind') } catch { $failed = $true }
        Assert-True $failed 'Unresolved bind source was accepted'
        Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'stop' }).Count -eq 0) 'Unsafe definition validation happened after downtime'
    }

    Run-Test 'legacy bind inspection without HostConfig Mounts is reconstructable' {
        $global:BackupUnitomitMounts = $true
        Invoke-Backup (Join-Path $testRoot 'legacy-binds') -KeepAppStopped
        Assert-True (-not $global:BackupUnitappRunning) 'Legacy bind backup did not complete'
    }

    Run-Test 'container-local application data is rejected before removal can lose it' {
        $global:BackupUnitnoPersistentData = $true
        $failed = $false
        try { Invoke-Backup (Join-Path $testRoot 'nonpersistent-data') } catch { $failed = $true }
        Assert-True $failed 'Recreation would silently lose container-local app data'
        Assert-True (@($global:BackupUnitcalls | Where-Object { $_[0] -eq 'stop' }).Count -eq 0) 'Nonpersistent data was detected after stopping the app'
    }

    Run-Test 'multiple networks preserve aliases and explicitly configured addresses' {
        $global:BackupUnitadditionalNetwork = $true
        $path = Join-Path $testRoot 'multiple-networks'
        Invoke-Backup $path -KeepAppStopped
        $definition = Get-Content -LiteralPath (Join-Path $path 'app-container.private.json') -Raw | ConvertFrom-Json -AsHashtable
        $secondary = $definition.createBody.NetworkingConfig.EndpointsConfig['frontend-network-id']
        Assert-True ($definition.networks.Count -eq 2) 'Secondary network was omitted'
        Assert-True ($secondary.Aliases -contains 'frontend-app' -and $secondary.IPAMConfig.IPv4Address -eq '172.30.0.20') 'Static network configuration changed'
        Assert-True ($secondary.DriverOpts['test-option'] -eq 'preserved') 'Endpoint driver options changed'
    }

    Run-Test 'lost create response retains artifacts and retry reuses owned container' {
        $path = Join-Path $testRoot 'lost-create-response'
        Invoke-Backup $path -KeepAppStopped
        $manifest = Get-Content (Join-Path $path 'manifest.json') -Raw | ConvertFrom-Json
        $global:BackupUnitoriginalRemoved = $true
        $global:BackupUnitfailureMode = 'responseLost'
        $failed = $false
        try { & $manifest.appRollbackScript -ConfirmAppWritersStopped } catch { $failed = $true }
        Assert-True ($failed -and (Test-Path -LiteralPath $manifest.appRecoveryDefinition)) 'Lost response destroyed recovery state'
        $global:BackupUnitfailureMode = ''
        & $manifest.appRollbackScript -ConfirmAppWritersStopped | Out-Null
        Assert-True ($global:BackupUnitcreateRequests.Count -eq 1 -and $global:BackupUnitappRunning) 'Retry failed to reuse the already-created container'
    }

    foreach ($scenario in @('healthy', 'createFailure', 'unhealthy')) {
        Run-Test "integration recreation via mocks: $scenario" {
            $env:LOCALAPPDATA = Join-Path $testRoot "integration-recreate-$scenario"
            if ($scenario -eq 'createFailure') { $global:BackupUnitfailureMode = 'createFailure' }
            if ($scenario -eq 'unhealthy') { $global:BackupUnithealth = 'unhealthy' }
            $failed = $false
            try { & $integrationScript -AppContainer app -PostgresContainer postgres -RedisContainer redis -RecreateApp *>&1 | ForEach-Object { $global:BackupUnitoutput.Add([string]$_) } }
            catch { $failed = $true }
            $privateRoot = Join-Path $env:LOCALAPPDATA 'Sub2API/private-backups'
            $remaining = @(Get-ChildItem $privateRoot -Directory -ErrorAction SilentlyContinue)
            Assert-True ($global:BackupUnitoriginalRemoved) 'Integration recreation never removed the stopped original fixture'
            if ($scenario -eq 'healthy') {
                Assert-True (-not $failed -and $global:BackupUnitcreateRequests.Count -eq 1) 'Integration never proved removed-container recovery'
                Assert-True ($remaining.Count -eq 0) 'Healthy integration did not clean up'
            } else {
                Assert-True ($failed -and $remaining.Count -gt 0) 'Failed integration discarded recovery artifacts'
                Assert-True (($global:BackupUnitoutput -join "`n") -notmatch 'Backup integration test passed') 'Failed integration claimed success'
            }
        }
    }

    foreach ($scenario in @('healthy', 'restartFailure', 'unhealthy', 'dumpFailure')) {
        Run-Test "integration test cleanup: $scenario" {
            $env:LOCALAPPDATA = Join-Path $testRoot "integration-$scenario"
            if ($scenario -eq 'restartFailure') { $global:BackupUnitfailStart = $true }
            if ($scenario -eq 'unhealthy') { $global:BackupUnithealth = 'unhealthy' }
            if ($scenario -eq 'dumpFailure') { $global:BackupUnitfailDump = $true }
            $failed = $false
            try {
                & $integrationScript -AppContainer app -PostgresContainer postgres -RedisContainer redis *>&1 |
                    ForEach-Object { $global:BackupUnitoutput.Add([string]$_) }
            } catch { $failed = $true }
            $privateRoot = Join-Path $env:LOCALAPPDATA 'Sub2API/private-backups'
            $remaining = @(Get-ChildItem $privateRoot -Directory -Filter 'backup-local-acceptance-it-*' -ErrorAction SilentlyContinue)
            $passed = ($global:BackupUnitoutput -join "`n") -match 'Backup integration test passed'
            if ($scenario -eq 'healthy') {
                Assert-True (-not $failed) 'Healthy integration fixture failed'
                Assert-True $passed 'Healthy integration test did not report success'
                Assert-True ($remaining.Count -eq 0) 'Successful integration test did not clean up'
                $starts = @($global:BackupUnitcalls | Where-Object { $_[0] -eq 'start' })
                Assert-True ($starts.Count -eq 1) 'Application was not restarted exactly once'
            } else {
                Assert-True $failed 'Failed integration fixture was accepted'
                Assert-True (-not $passed) 'Integration success printed before recovery and health verification'
                Assert-True ($remaining.Count -gt 0) 'Failed integration test deleted recovery artifacts'
            }
        }
    }
} finally {
    $env:LOCALAPPDATA = $originalLocalAppData
    $env:DOCKER_HOST = $originalDockerHost
    $env:DOCKER_CONTEXT = $originalDockerContext
    if ($global:BackupUnitpipe) { $global:BackupUnitpipe.Dispose(); $global:BackupUnitpipe = $null }
    $resolved = [IO.Path]::GetFullPath($testRoot)
    $tempPrefix = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolved.StartsWith($tempPrefix, [StringComparison]::OrdinalIgnoreCase) -or
        (Split-Path $resolved -Leaf) -notlike 'sub2api-backup-unit-*') { throw 'Unexpected unit test cleanup path' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}

if ($global:BackupUnitfailures.Count -gt 0) { throw ($global:BackupUnitfailures -join "`n") }
Write-Output 'Backup mocked unit tests passed (no Docker executable invoked).'
