param(
    [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$AppContainer,
    [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$PostgresContainer,
    [Parameter(Mandatory = $true)][ValidateNotNullOrEmpty()][string]$RedisContainer,
    [string]$OutputDirectory,
    [switch]$KeepAppStopped
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$script:BackupStage = 'inspect selected containers'

# Shared verbatim with the generated, standalone rollback script.
$script:EngineTransportHelpers = @'
function Get-DockerEngineEndpoint {
    if (-not [string]::IsNullOrWhiteSpace($env:DOCKER_CONTEXT)) {
        $contextName = $env:DOCKER_CONTEXT
    } elseif (-not [string]::IsNullOrWhiteSpace($env:DOCKER_HOST)) {
        return $env:DOCKER_HOST
    } else {
        $contextName = & docker context show 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $contextName) { throw 'Cannot resolve the active Docker context.' }
    }
    $raw = & docker context inspect $contextName --format '{{json .Endpoints.docker}}' 2>$null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the active Docker endpoint.' }
    try { $endpoint = ($raw -join "`n") | ConvertFrom-Json }
    catch { throw 'Invalid Docker context endpoint metadata.' }
    if (-not $endpoint.Host) { throw 'Docker context has no Engine endpoint.' }
    return [string]$endpoint.Host
}
function Get-EngineTransport([string]$Endpoint) {
    if ($Endpoint -cmatch '^npipe:////\./pipe/([A-Za-z0-9_.-]+)$') { return 'npipe' }
    if ($Endpoint -cmatch '^(unix:///[^\r\n]+|ssh://[^\r\n]+|tcp://[^\r\n]+)$') { return 'stdio' }
    throw 'Unsupported Docker endpoint; only local npipe, unix, ssh, and tcp endpoints are supported.'
}
function Throw-EngineFailure([string]$Reason) {
    $error = [InvalidOperationException]::new($Reason)
    $error.Data['RollbackDiagnostic'] = $Reason
    throw $error
}
function Read-EngineBytes([IO.Stream]$Stream, [int]$Length, [Threading.CancellationToken]$Token) {
    $buffer = [byte[]]::new($Length)
    for ($offset = 0; $offset -lt $Length;) {
        $count = $Stream.ReadAsync($buffer, $offset, $Length - $offset, $Token).GetAwaiter().GetResult()
        if ($count -eq 0) { Throw-EngineFailure 'Engine response truncated.' }
        $offset += $count
    }
    return ,$buffer
}
function Read-EngineLine([IO.Stream]$Stream, [Threading.CancellationToken]$Token) {
    $bytes = [Collections.Generic.List[byte]]::new()
    while ($true) {
        $one = Read-EngineBytes $Stream 1 $Token
        if ($one[0] -eq 10) {
            if ($bytes.Count -eq 0 -or $bytes[-1] -ne 13) { Throw-EngineFailure 'Engine response has invalid line framing.' }
            return [Text.Encoding]::ASCII.GetString($bytes.ToArray(), 0, $bytes.Count - 1)
        }
        $bytes.Add($one[0])
        if ($bytes.Count -gt 8192) { Throw-EngineFailure 'Engine response header exceeds limit.' }
    }
}
function New-DockerEngineProcess {
    $process = [Diagnostics.Process]::new()
    $process.StartInfo.FileName = (Get-Command docker -CommandType Application -ErrorAction Stop).Source
    $process.StartInfo.ArgumentList.Add('system')
    $process.StartInfo.ArgumentList.Add('dial-stdio')
    $process.StartInfo.UseShellExecute = $false
    $process.StartInfo.CreateNoWindow = $true
    $process.StartInfo.RedirectStandardInput = $true
    $process.StartInfo.RedirectStandardOutput = $true
    $process.StartInfo.RedirectStandardError = $true
    return $process
}
function Invoke-DockerEngineRequest {
    param([string]$Endpoint, [string]$Method, [string]$Path, [string]$Json = '',
        [int]$ExpectedStatus = 200, [ValidateRange(1, 600)][int]$TimeoutSeconds = 30)
    $transport = Get-EngineTransport $Endpoint
    $cancel = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds($TimeoutSeconds))
    $inputStream = $null
    $outputStream = $null
    $process = $null
    $stderrTask = $null
    $body = [IO.MemoryStream]::new()
    try {
        if ($transport -eq 'npipe') {
            $pipeName = $Endpoint.Substring('npipe:////./pipe/'.Length)
            $inputStream = [IO.Pipes.NamedPipeClientStream]::new('.', $pipeName, [IO.Pipes.PipeDirection]::InOut, [IO.Pipes.PipeOptions]::Asynchronous)
            $null = $inputStream.ConnectAsync($cancel.Token).GetAwaiter().GetResult()
            $outputStream = $inputStream
        } else {
            $process = New-DockerEngineProcess
            if (-not $process.Start()) { Throw-EngineFailure 'Engine transport process could not start.' }
            $inputStream = $process.StandardInput.BaseStream
            $outputStream = $process.StandardOutput.BaseStream
            # Drain privately without retaining/logging possibly sensitive stderr.
            $stderrTask = $process.StandardError.BaseStream.CopyToAsync([IO.Stream]::Null, $cancel.Token)
        }
        $length = [Text.Encoding]::UTF8.GetByteCount($Json)
        $request = "$Method $Path HTTP/1.1`r`nHost: docker`r`nContent-Type: application/json`r`nContent-Length: $length`r`nConnection: close`r`n`r`n$Json"
        $bytes = [Text.Encoding]::UTF8.GetBytes($request)
        $null = $inputStream.WriteAsync($bytes, 0, $bytes.Length, $cancel.Token).GetAwaiter().GetResult()
        $null = $inputStream.FlushAsync($cancel.Token).GetAwaiter().GetResult()
        # Never half-close stdin here. Docker Desktop can return empty HTTP 500
        # when dial-stdio observes EOF before the Engine response is consumed.
        $statusLine = Read-EngineLine $outputStream $cancel.Token
        if ($statusLine -notmatch '^HTTP/1\.[01] ([0-9]{3})(?: |$)') { Throw-EngineFailure 'Engine response has invalid HTTP status.' }
        $status = [int]$Matches[1]
        if ($status -ne $ExpectedStatus) { Throw-EngineFailure "Engine response HTTP $status." }
        $headers = @{}
        $headerBytes = 0
        while (($line = Read-EngineLine $outputStream $cancel.Token) -ne '') {
            $headerBytes += $line.Length + 2
            $parts = $line -split ':', 2
            if ($headerBytes -gt 65536 -or $parts.Count -ne 2 -or $headers.ContainsKey($parts[0])) { Throw-EngineFailure 'Engine response has invalid HTTP headers.' }
            $headers[$parts[0]] = $parts[1].Trim()
        }
        if ($headers['Transfer-Encoding'] -eq 'chunked') {
            if ($headers.ContainsKey('Content-Length')) { Throw-EngineFailure 'Engine response has ambiguous framing.' }
            do {
                $chunkLine = (Read-EngineLine $outputStream $cancel.Token) -split ';', 2
                $size = 0
                if (-not [int]::TryParse($chunkLine[0], [Globalization.NumberStyles]::AllowHexSpecifier, [Globalization.CultureInfo]::InvariantCulture, [ref]$size) -or
                    $size -lt 0 -or $size + $body.Length -gt 8388608) { Throw-EngineFailure 'Engine response has invalid chunk size.' }
                if ($size -eq 0) {
                    while (($line = Read-EngineLine $outputStream $cancel.Token) -ne '') {
                        $headerBytes += $line.Length + 2
                        if ($headerBytes -gt 65536) { Throw-EngineFailure 'Engine response trailers exceed limit.' }
                    }
                    break
                }
                $chunk = Read-EngineBytes $outputStream $size $cancel.Token
                $body.Write($chunk, 0, $size)
                if ((Read-EngineLine $outputStream $cancel.Token) -ne '') { Throw-EngineFailure 'Engine response has invalid chunk boundary.' }
            } while ($true)
        } elseif (-not $headers.ContainsKey('Transfer-Encoding') -and $headers.ContainsKey('Content-Length')) {
            $size = 0
            if (-not [int]::TryParse($headers['Content-Length'], [ref]$size) -or $size -lt 0 -or $size -gt 8388608) { Throw-EngineFailure 'Engine response has invalid body size.' }
            $data = Read-EngineBytes $outputStream $size $cancel.Token
            $body.Write($data, 0, $size)
        } else { Throw-EngineFailure 'Engine response has unsupported HTTP framing.' }
        return [Text.Encoding]::UTF8.GetString($body.ToArray())
    } catch {
        if ($_.Exception.Data['RollbackDiagnostic']) { throw }
        if ($cancel.IsCancellationRequested) { Throw-EngineFailure 'Engine transport timed out.' }
        Throw-EngineFailure 'Engine transport I/O failed.'
    } finally {
        $cancel.Cancel()
        if ($inputStream) { $inputStream.Dispose() }
        if ($outputStream -and $outputStream -ne $inputStream) { $outputStream.Dispose() }
        if ($process) {
            try { if (-not $process.HasExited) { $process.Kill($true) }; $process.WaitForExit() } catch { }
            $process.Dispose()
        }
        if ($stderrTask) { try { $null = $stderrTask.GetAwaiter().GetResult() } catch { } }
        $body.Dispose()
        $cancel.Dispose()
    }
}
'@
. ([scriptblock]::Create($script:EngineTransportHelpers))

function Invoke-Docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)

    $result = & docker @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker operation failed during '$script:BackupStage': docker $($Arguments[0])"
    }
    return $result
}

function Get-Container {
    param([Parameter(Mandatory = $true)][string]$Name)

    $containers = @(Invoke-Docker inspect $Name | ConvertFrom-Json)
    if ($containers.Count -ne 1) {
        throw "Expected exactly one container for '$Name', found $($containers.Count)."
    }
    return $containers[0]
}

function Get-RollbackImageId {
    param([Parameter(Mandatory = $true)]$Container)

    $imageId = [string]$Container.Image
    if ($imageId -notmatch '^sha256:[0-9a-fA-F]{64}$') {
        throw 'The selected application container has no immutable image ID.'
    }
    $images = @(Invoke-Docker image inspect $imageId | ConvertFrom-Json)
    if ($images.Count -ne 1 -or $images[0].Id -ne $imageId) {
        throw 'The selected application image ID is not available locally.'
    }
    return $imageId
}

function Confirm-ApplicationDatabase {
    param($App, $Postgres, [hashtable]$Environment)

    # Nonempty env values override config.yaml in the application. Do not guess
    # defaults when config.yaml may select a different database or credentials.
    foreach ($key in @('DATABASE_HOST', 'DATABASE_PORT', 'DATABASE_USER', 'DATABASE_PASSWORD', 'DATABASE_DBNAME', 'DATABASE_SSLMODE')) {
        if (-not $Environment.ContainsKey($key) -or [string]::IsNullOrWhiteSpace($Environment[$key])) {
            throw "Cannot prove the application's database configuration: set a nonempty $key on the application container."
        }
    }
    $probe = @'
set -eu
export PGHOST="$DATABASE_HOST" PGPORT="$DATABASE_PORT" PGUSER="$DATABASE_USER"
export PGPASSWORD="$DATABASE_PASSWORD" PGDATABASE="$DATABASE_DBNAME" PGSSLMODE="$DATABASE_SSLMODE" PGCONNECT_TIMEOUT=10
unset PGHOSTADDR PGSERVICE PGSERVICEFILE PGOPTIONS
exec psql -X -w -v ON_ERROR_STOP=1 -tAc "SELECT json_build_object('database', current_database(), 'serverAddress', inet_server_addr(), 'serverPort', inet_server_port())"
'@
    $identityJson = Invoke-Docker exec $App.Id sh -c ($probe.Replace("`r`n", "`n"))
    try { $identity = ($identityJson -join "`n") | ConvertFrom-Json }
    catch { throw 'Application database identity probe did not return valid JSON.' }
    $addresses = @($Postgres.NetworkSettings.Networks.PSObject.Properties | ForEach-Object {
        $_.Value.IPAddress
        $_.Value.GlobalIPv6Address
    } | Where-Object { $_ })
    if ($identity.database -cne $Environment.DATABASE_DBNAME -or
        -not $identity.serverAddress -or $identity.serverAddress -notin $addresses -or
        [string]$identity.serverPort -ne $Environment.DATABASE_PORT) {
        throw 'Application database does not match the selected PostgreSQL container. A direct Docker-network database endpoint is required.'
    }
    $validate = @'
set -eu
export PGDATABASE="$1" PGPORT="$2" PGUSER="${POSTGRES_USER:-postgres}" PGHOST=/var/run/postgresql PGCONNECT_TIMEOUT=10
unset PGHOSTADDR PGSERVICE PGSERVICEFILE PGOPTIONS
exec psql -X -w -v ON_ERROR_STOP=1 -tAc 'SELECT current_database()'
'@
    $selectedDatabase = Invoke-Docker exec $Postgres.Id sh -c ($validate.Replace("`r`n", "`n")) backup-validate $Environment.DATABASE_DBNAME $Environment.DATABASE_PORT
    if (($selectedDatabase -join "`n") -cne $Environment.DATABASE_DBNAME) {
        throw 'Selected PostgreSQL container cannot validate the application database.'
    }
    return $Environment.DATABASE_DBNAME
}

function ConvertTo-PowerShellLiteral([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

function New-ContainerRecoveryDefinition {
    param($App, [string]$ImageId, [string]$SnapshotId)

    # Use Engine request data, not generated shell arguments or inferred Compose
    # configuration. Keep all credentials in the permission-restricted JSON file.
    $captured = $App | ConvertTo-Json -Depth 100 | ConvertFrom-Json -AsHashtable
    $body = $captured.Config
    $hostConfig = $captured.HostConfig
    if ($hostConfig['AutoRemove'] -or $hostConfig['VolumesFrom'] -or $hostConfig['Links'] -or $hostConfig['ContainerIDFile'] -or
        $hostConfig.NetworkMode -match '^(host|none|container:)' -or
        $hostConfig['IpcMode'] -like 'container:*' -or $hostConfig['PidMode'] -like 'container:*') {
        throw 'Recovery cannot safely reproduce this container namespace, removal, or inherited-volume configuration.'
    }
    $daemon = Invoke-Docker info --format '{{json .}}' | ConvertFrom-Json
    if ($daemon.OSType -ne 'linux' -or -not $daemon.ID) { throw 'Recovery requires an identifiable Linux Docker daemon.' }
    $apiVersion = [string](Invoke-Docker version --format '{{.Server.APIVersion}}')
    if ($apiVersion -notmatch '^1\.[0-9]+$') { throw 'Cannot determine the Docker Engine API version.' }
    $engineEndpoint = Get-DockerEngineEndpoint
    $null = Get-EngineTransport $engineEndpoint

    $mounts = @()
    $volumes = @()
    if (@($captured.Mounts | Where-Object { $_.Type -in @('bind', 'volume') -and $_.Destination -ceq '/app/data' }).Count -ne 1) {
        throw 'Reconstruction requires /app/data to be an explicit persistent bind or volume mount, not container-local data.'
    }
    foreach ($mount in $captured.Mounts) {
        if ($mount.Destination -notmatch '^/') { throw 'Recovery requires absolute container mount destinations.' }
        $existing = @($hostConfig['Mounts'] | Where-Object { $null -ne $_ -and $_.Target -ceq $mount.Destination })
        $spec = if ($existing.Count -eq 1) { $existing[0] } else { @{} }
        $spec.Type = $mount.Type
        $spec.Target = $mount.Destination
        $spec.ReadOnly = -not $mount.RW
        switch ($mount.Type) {
            bind {
                if ($mount.Source -notmatch '^/' -or ($mount.Mode -split ',') -cmatch '^[zZ]$') {
                    throw 'Recovery requires resolved absolute Linux bind sources without implicit SELinux relabeling.'
                }
                $spec.Source = $mount.Source
                if (-not $spec['BindOptions']) { $spec.BindOptions = @{} }
                $spec.BindOptions.Propagation = $mount.Propagation
                $spec.BindOptions.CreateMountpoint = $false
            }
            volume {
                if (-not $mount.Name) { throw 'Recovery requires an existing named or anonymous volume identity.' }
                $spec.Source = $mount.Name
                if (-not $spec['VolumeOptions']) { $spec.VolumeOptions = @{} }
                $spec.VolumeOptions.NoCopy = $true
                $volume = @(Invoke-Docker volume inspect $mount.Name | ConvertFrom-Json -AsHashtable)
                if ($volume.Count -ne 1 -or $volume[0].Name -cne $mount.Name) { throw 'Cannot capture the mounted volume identity.' }
                $volumes += $volume[0]
            }
            tmpfs {
                if ($hostConfig['Tmpfs'] -and $hostConfig.Tmpfs.ContainsKey($mount.Destination)) { continue }
                if ($existing.Count -ne 1) { throw 'Cannot reproduce tmpfs mount options.' }
            }
            default { throw 'Recovery does not support this mount type.' }
        }
        $mounts += $spec
    }
    $hostConfig.Binds = @()
    $hostConfig.Mounts = $mounts
    # Pin actual published ports, including dynamically allocated host ports.
    $hostConfig.PortBindings = @{}
    foreach ($port in $(if ($captured.NetworkSettings['Ports']) { $captured.NetworkSettings.Ports.Keys })) {
        if ($captured.NetworkSettings.Ports[$port]) { $hostConfig.PortBindings[$port] = $captured.NetworkSettings.Ports[$port] }
    }
    $hostConfig.PublishAllPorts = $false

    $networks = @()
    $endpoints = @{}
    $primaryNetwork = $null
    foreach ($name in $captured.NetworkSettings.Networks.Keys) {
        $endpoint = $captured.NetworkSettings.Networks[$name]
        $network = @(Invoke-Docker network inspect $endpoint.NetworkID | ConvertFrom-Json -AsHashtable)
        if ($network.Count -ne 1 -or $network[0].Id -cne $endpoint.NetworkID -or $network[0].Name -cne $name) {
            throw 'Cannot pin the original Docker network identity.'
        }
        $networks += @{ id = $endpoint.NetworkID; name = $name }
        $options = @{}
        foreach ($key in @('IPAMConfig', 'Aliases', 'DriverOpts', 'GwPriority')) {
            if ($endpoint.ContainsKey($key) -and $null -ne $endpoint[$key]) { $options[$key] = $endpoint[$key] }
        }
        if ($endpoint['Links']) { throw 'Recovery cannot safely reproduce legacy network links.' }
        $endpoints[$endpoint.NetworkID] = $options
        if ($hostConfig.NetworkMode -in @($name, $endpoint.NetworkID) -or ($hostConfig.NetworkMode -eq 'default' -and $name -eq 'bridge')) {
            $primaryNetwork = $endpoint.NetworkID
        }
    }
    if (-not $primaryNetwork) { throw 'Cannot pin the primary Docker network.' }
    $hostConfig.NetworkMode = $primaryNetwork
    $body.Image = $ImageId
    $body.HostConfig = $hostConfig
    $body.NetworkingConfig = @{ EndpointsConfig = $endpoints }
    if (-not $body.Labels) { $body.Labels = @{} }
    foreach ($key in @($body.Labels.Keys)) {
        if ($key -like 'com.docker.compose.*') { $body.Labels.Remove($key) }
    }
    $body.Labels['org.sub2api.rollback.snapshot'] = $SnapshotId
    return @{
        schemaVersion = 1
        daemonId = $daemon.ID
        apiVersion = $apiVersion
        engineEndpoint = $engineEndpoint
        originalName = $captured.Name.TrimStart('/')
        recoveryName = 'sub2api-rollback-' + $SnapshotId
        snapshotId = $SnapshotId
        networks = $networks
        volumes = $volumes
        createBody = $body
    }
}

function Write-ContainerRollback {
    param([string]$Path, [string]$Archive, [string]$ImageId, [string]$ContainerId, [string]$Definition, [string]$EngineEndpoint)

    if (-not $EngineEndpoint) {
        $privateDefinition = Get-Content -LiteralPath $Definition -Raw | ConvertFrom-Json -AsHashtable
        $EngineEndpoint = $privateDefinition['engineEndpoint']
    }
    if (-not $EngineEndpoint) { throw 'A verified Docker Engine endpoint is required to generate rollback.' }
    $null = Get-EngineTransport $EngineEndpoint

    $rollback = @'
#requires -Version 7.4
param(
    [ValidateRange(1, 600)][int]$HealthTimeoutSeconds = 120,
    [switch]$ConfirmAppWritersStopped,
    [switch]$EnginePreflightOnly,
    [ValidateRange(1, 600)][int]$EngineTimeoutSeconds = 30
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$archive = __ARCHIVE__
$imageId = __IMAGE__
$containerId = __CONTAINER__
$definitionPath = __DEFINITION__
$expectedEngineEndpoint = __ENDPOINT__
$stage = 'validate recovery artifacts'
__ENGINE_HELPERS__
function Invoke-CheckedDocker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    $result = & docker @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Rollback failed: docker $($Arguments[0]). Recovery artifacts were retained." }
    return $result
}
function Invoke-PrivateContainerCreate($Definition) {
    $json = ConvertTo-Json -InputObject $Definition.createBody -Depth 100 -Compress
    $name = [Uri]::EscapeDataString($Definition.recoveryName)
    if ((Get-DockerEngineEndpoint) -cne $expectedEngineEndpoint) { throw 'Docker endpoint changed.' }
    $response = Invoke-DockerEngineRequest -Endpoint $expectedEngineEndpoint -Method POST -Path "/v$($Definition.apiVersion)/containers/create?name=$name" -Json $json -ExpectedStatus 201 -TimeoutSeconds $EngineTimeoutSeconds
    $result = $response | ConvertFrom-Json
    if ($result.Id -notmatch '^[0-9a-f]{64}$') { throw 'Invalid created container identity.' }
    return $result.Id
}
try {
    if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ne __HASH__) { throw 'Image checksum mismatch.' }
    if ((Get-FileHash -LiteralPath $definitionPath -Algorithm SHA256).Hash -ne __DEFINITION_HASH__) { throw 'Definition checksum mismatch.' }
    $definition = Get-Content -LiteralPath $definitionPath -Raw | ConvertFrom-Json -AsHashtable
    if ($definition.schemaVersion -ne 1 -or $definition.createBody.Image -cne $imageId) { throw 'Invalid recovery definition.' }
    $stage = 'verify captured Docker Engine endpoint'
    if ((Get-DockerEngineEndpoint) -cne $expectedEngineEndpoint) { throw 'Docker endpoint changed.' }
    $null = Get-EngineTransport $expectedEngineEndpoint
    $stage = 'verify original Docker daemon and storage/network identities'
    $daemon = Invoke-CheckedDocker info --format '{{json .}}' | ConvertFrom-Json
    if ($daemon.ID -cne $definition.daemonId) { throw 'Docker daemon changed.' }
    foreach ($network in $definition.networks) {
        $actual = @(Invoke-CheckedDocker network inspect $network.id | ConvertFrom-Json)
        if ($actual.Count -ne 1 -or $actual[0].Id -cne $network.id -or $actual[0].Name -cne $network.name) { throw 'Docker network changed.' }
    }
    foreach ($volume in $definition.volumes) {
        $actual = @(Invoke-CheckedDocker volume inspect $volume.Name | ConvertFrom-Json -AsHashtable)
        if ($actual.Count -ne 1) { throw 'Docker volume missing.' }
        foreach ($key in @('Name', 'Driver', 'Mountpoint', 'CreatedAt')) {
            if ($actual[0][$key] -cne $volume[$key]) { throw 'Docker volume identity changed.' }
        }
    }
    if ($EnginePreflightOnly) {
        $stage = 'read-only Docker Engine transport preflight'
        $pong = Invoke-DockerEngineRequest -Endpoint $expectedEngineEndpoint -Method GET -Path '/_ping' -ExpectedStatus 200 -TimeoutSeconds $EngineTimeoutSeconds
        if ($pong -cne 'OK') { throw 'Unexpected Docker Engine ping response.' }
        Write-Output 'Docker Engine transport preflight passed (read-only).'
        return
    }
    $stage = 'verify replacement app writers are stopped (reconstruction requires -ConfirmAppWritersStopped)'
    $entries = @(Invoke-CheckedDocker container ls --all --no-trunc --format '{{json .}}' | ForEach-Object { $_ | ConvertFrom-Json })
    foreach ($entry in $entries) {
        if ($entry.ID -cne $containerId -and $entry.Names -ceq $definition.originalName -and $entry.State -notin @('exited', 'created')) {
            throw 'Stop the replacement application before rollback.'
        }
    }
    $originalExists = @($entries | Where-Object { $_.ID -ceq $containerId }).Count -eq 1
    if (-not $originalExists -and -not $ConfirmAppWritersStopped) { throw 'Confirm all application writers are stopped before reconstruction.' }
    $stage = 'load and verify archived immutable image'
    Invoke-CheckedDocker load --input $archive | Out-Null
    $images = @(Invoke-CheckedDocker image inspect $imageId | ConvertFrom-Json)
    if ($images.Count -ne 1 -or $images[0].Id -cne $imageId) { throw 'Archived image is unavailable.' }
    if (-not $originalExists) {
        $stage = 'recreate application from private definition'
        $recovered = @($entries | Where-Object { $_.Names -ceq $definition.recoveryName })
        if ($recovered.Count -gt 1) { throw 'Ambiguous recovery container.' }
        $containerId = if ($recovered.Count -eq 1) { $recovered[0].ID } else { Invoke-PrivateContainerCreate $definition }
    }
    $stage = 'verify recovered container identity'
    $containers = @(Invoke-CheckedDocker inspect $containerId | ConvertFrom-Json -AsHashtable)
    if ($containers.Count -ne 1 -or $containers[0].Id -cne $containerId -or $containers[0].Image -cne $imageId) { throw 'Container identity mismatch.' }
    if (-not $originalExists -and $containers[0].Config.Labels['org.sub2api.rollback.snapshot'] -cne $definition.snapshotId) { throw 'Recovery ownership mismatch.' }
    $stage = 'start application and verify health'
    Invoke-CheckedDocker start $containerId | Out-Null
    $deadline = [DateTime]::UtcNow.AddSeconds($HealthTimeoutSeconds)
    do {
        $state = @(Invoke-CheckedDocker inspect $containerId | ConvertFrom-Json)[0].State
        if (-not $state.Running) { throw 'Rollback application is not running.' }
        if (-not $state.PSObject.Properties['Health']) { throw 'Rollback requires a configured Docker healthcheck.' }
        if ($state.Health.Status -eq 'healthy') { Write-Output 'Application container rollback passed health verification.'; return }
        if ($state.Health.Status -eq 'unhealthy') { throw 'Rollback application is unhealthy.' }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Rollback application healthcheck timed out.'
} catch {
    $diagnostic = if ($_.Exception.Data['RollbackDiagnostic']) { ' ' + [string]$_.Exception.Data['RollbackDiagnostic'] } else { '' }
    throw "Rollback failed during '$stage'.$diagnostic Recovery artifacts were retained."
}
'@
    $rollback = $rollback.Replace('__ARCHIVE__', (ConvertTo-PowerShellLiteral $Archive)).
        Replace('__IMAGE__', (ConvertTo-PowerShellLiteral $ImageId)).
        Replace('__CONTAINER__', (ConvertTo-PowerShellLiteral $ContainerId)).
        Replace('__HASH__', (ConvertTo-PowerShellLiteral (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash)).
        Replace('__DEFINITION__', (ConvertTo-PowerShellLiteral $Definition)).
        Replace('__DEFINITION_HASH__', (ConvertTo-PowerShellLiteral (Get-FileHash -LiteralPath $Definition -Algorithm SHA256).Hash)).
        Replace('__ENDPOINT__', (ConvertTo-PowerShellLiteral $EngineEndpoint)).
        Replace('__ENGINE_HELPERS__', $script:EngineTransportHelpers)
    $rollback | Set-Content -LiteralPath $Path -Encoding utf8
}

function Get-BackupFileManifest {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [string[]]$Exclude = @()
    )

    $rootPrefix = [System.IO.Path]::GetFullPath($Root).TrimEnd([System.IO.Path]::DirectorySeparatorChar) +
        [System.IO.Path]::DirectorySeparatorChar
    return @(Get-ChildItem -LiteralPath $Root -File -Recurse |
        Where-Object { $_.Name -notin $Exclude } |
        Sort-Object FullName |
        ForEach-Object {
            [ordered]@{
                name = $_.FullName.Substring($rootPrefix.Length).Replace('\', '/')
                bytes = $_.Length
                sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash
            }
        })
}

if (-not $OutputDirectory) {
    if (-not $env:LOCALAPPDATA) {
        throw 'LOCALAPPDATA is unavailable; provide an explicit private OutputDirectory.'
    }
    $privateRoot = Join-Path $env:LOCALAPPDATA 'Sub2API\private-backups'
    $OutputDirectory = Join-Path $privateRoot ('local-acceptance-backup-' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
}
$OutputDirectory = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDirectory)
$backup = New-Item -ItemType Directory -Path $OutputDirectory
if ($IsWindows) {
    & icacls $backup.FullName /inheritance:r /grant:r "$env:USERDOMAIN\$env:USERNAME`:(OI)(CI)F" 'SYSTEM:(OI)(CI)F' 'BUILTIN\Administrators:(OI)(CI)F' | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'Failed to restrict backup directory permissions.'
    }
} else {
    & chmod 700 $backup.FullName
    if ($LASTEXITCODE -ne 0) { throw 'Failed to restrict backup directory permissions.' }
}

$app = Get-Container -Name $AppContainer
$postgres = Get-Container -Name $PostgresContainer
$redis = Get-Container -Name $RedisContainer
$containers = @($app, $postgres, $redis)
foreach ($container in $containers) {
    if (-not $container.State.Running) {
        throw "Container is not running: $($container.Name)"
    }
}

$envMap = @{}
foreach ($entry in $app.Config.Env) {
    $parts = $entry -split '=', 2
    $envMap[$parts[0]] = if ($parts.Count -gt 1) { $parts[1] } else { '' }
}
$script:BackupStage = 'validate application database against selected PostgreSQL container'
$database = Confirm-ApplicationDatabase -App $app -Postgres $postgres -Environment $envMap
$script:BackupStage = 'validate immutable application image'
$rollbackImage = Get-RollbackImageId -Container $app
$snapshotId = [guid]::NewGuid().ToString('N')
$script:BackupStage = 'capture private container recovery definition'
$recoveryDefinition = New-ContainerRecoveryDefinition -App $app -ImageId $rollbackImage -SnapshotId $snapshotId
$recoveryDefinitionPath = Join-Path $backup.FullName 'app-container.private.json'
$recoveryDefinition | ConvertTo-Json -Depth 100 | Set-Content -LiteralPath $recoveryDefinitionPath -Encoding utf8
if (-not $IsWindows) {
    & chmod 600 $recoveryDefinitionPath
    if ($LASTEXITCODE -ne 0) { throw 'Failed to restrict private recovery definition permissions.' }
}
$dump = "/tmp/sub2api-local-acceptance-$snapshotId.dump"
$rdb = "/tmp/sub2api-local-acceptance-$snapshotId.rdb"
$appWasStoppedByScript = $false
$backupSucceeded = $false
$script:BackupStage = 'stop selected application'

try {
    Invoke-Docker stop --time 30 $app.Id | Out-Null
    $appWasStoppedByScript = $true

    $script:BackupStage = 'dump selected PostgreSQL container'
    $dumpCommand = @'
set -eu
export PGDATABASE="$1" PGPORT="$2" PGUSER="${POSTGRES_USER:-postgres}" PGHOST=/var/run/postgresql PGCONNECT_TIMEOUT=10
unset PGHOSTADDR PGSERVICE PGSERVICEFILE PGOPTIONS
exec pg_dump -w -Fc -f "$3"
'@
    Invoke-Docker exec $postgres.Id sh -c ($dumpCommand.Replace("`r`n", "`n")) backup-dump $database $envMap.DATABASE_PORT $dump | Out-Null
    Invoke-Docker exec $postgres.Id pg_restore --list $dump | Out-Null
    Invoke-Docker cp "$($postgres.Id):$dump" (Join-Path $backup.FullName 'postgres.dump') | Out-Null

    $script:BackupStage = 'snapshot selected Redis container'
    $unauthenticatedPing = & docker exec $redis.Id sh -c 'unset REDISCLI_AUTH; redis-cli PING' 2>$null
    $needsRedisAuth = $LASTEXITCODE -ne 0 -or $unauthenticatedPing -ne 'PONG'
    $redisArgs = @('exec')
    if ($needsRedisAuth -and $envMap.ContainsKey('REDIS_PASSWORD') -and $envMap.REDIS_PASSWORD) {
        $redisArgs += @('-e', "REDISCLI_AUTH=$($envMap.REDIS_PASSWORD)")
    }
    $redisArgs += @($redis.Id)
    if ($needsRedisAuth) {
        $redisArgs += @('redis-cli')
        if ($envMap.ContainsKey('REDIS_USERNAME') -and $envMap.REDIS_USERNAME) {
            $redisArgs += @('--user', $envMap.REDIS_USERNAME)
        }
        $redisArgs += @('--rdb', $rdb)
    } else {
        $redisArgs += @('sh', '-c', 'unset REDISCLI_AUTH; exec redis-cli --rdb "$1"', 'redis-cli', $rdb)
    }
    Invoke-Docker @redisArgs | Out-Null
    Invoke-Docker exec $redis.Id redis-check-rdb $rdb | Out-Null
    Invoke-Docker cp "$($redis.Id):$rdb" (Join-Path $backup.FullName 'redis.rdb') | Out-Null

    $script:BackupStage = 'copy selected application data'
    Invoke-Docker cp "$($app.Id):/app/data" (Join-Path $backup.FullName 'app-data') | Out-Null

    $script:BackupStage = 'archive selected application image'
    $appImageArchive = Join-Path $backup.FullName 'app-image.tar'
    Invoke-Docker save --output $appImageArchive $rollbackImage | Out-Null
    $rollbackScript = Join-Path $backup.FullName 'rollback-app.ps1'
    Write-ContainerRollback -Path $rollbackScript -Archive $appImageArchive -ImageId $rollbackImage -ContainerId $app.Id -Definition $recoveryDefinitionPath

    $metadata = [ordered]@{
        capturedAt = (Get-Date).ToUniversalTime().ToString('o')
        appContainer = $AppContainer
        appContainerId = $app.Id
        appImage = $app.Image
        appConfiguredImage = $app.Config.Image
        appRollbackImage = $rollbackImage
        postgresContainer = $PostgresContainer
        postgresContainerId = $postgres.Id
        postgresImage = $postgres.Image
        postgresDatabase = $database
        postgresPort = [int]$envMap.DATABASE_PORT
        redisContainer = $RedisContainer
        redisContainerId = $redis.Id
        redisImage = $redis.Image
        files = Get-BackupFileManifest -Root $backup.FullName -Exclude @('manifest.json', 'SHA256SUMS')
        appImageArchive = $appImageArchive
        appRollbackMode = 'engine-recreate-or-retained'
        appRecoveryDefinition = $recoveryDefinitionPath
        appRecoveryContainerName = $recoveryDefinition.recoveryName
        appRollbackScript = $rollbackScript
        appRollbackCommand = '& ' + (ConvertTo-PowerShellLiteral $rollbackScript)
        rollbackNote = 'Run appRollbackCommand in PowerShell 7.4+ on the original Docker daemon. Keep this entire backup private: app-container.private.json contains credentials. Stop all replacement app writers first and prevent their automatic restart. If the original container was removed, append -ConfirmAppWritersStopped; recovery creates a separately named container using the immutable image and private Engine definition, with the original existing mounts, networks, and published ports. Missing networks/volumes or bind sources fail closed; resources are not guessed or recreated. No Compose project is inferred. Health is checked before success. App-only rollback requires backward-compatible migrations and does not restore data; restore DB, Redis, and app-data together only after stopping all app writers and accepting loss of post-backup writes.'
    }
    $metadata | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $backup.FullName 'manifest.json') -Encoding utf8

    $checksumLines = @(Get-ChildItem -LiteralPath $backup.FullName -File -Recurse |
        Where-Object { $_.Name -ne 'SHA256SUMS' } |
        Sort-Object FullName |
        ForEach-Object {
            $relativePath = $_.FullName.Substring($backup.FullName.TrimEnd('\').Length + 1).Replace('\', '/')
            $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            "$hash  $relativePath"
        })
    $checksumLines | Set-Content -LiteralPath (Join-Path $backup.FullName 'SHA256SUMS') -Encoding ascii
    $backupSucceeded = $true
} finally {
    foreach ($temporaryFile in @(
        @{ Container = $postgres.Id; Path = $dump },
        @{ Container = $redis.Id; Path = $rdb }
    )) {
        try {
            Invoke-Docker exec $temporaryFile.Container rm -f $temporaryFile.Path | Out-Null
        } catch {
            Write-Warning "Failed to remove temporary snapshot from $($temporaryFile.Container)."
        }
    }
    if ($appWasStoppedByScript -and (-not $backupSucceeded -or -not $KeepAppStopped)) {
        $script:BackupStage = 'restart selected application (backup files retained on failure)'
        Invoke-Docker start $app.Id | Out-Null
    }
}

Write-Output "Verified PostgreSQL archive, Redis RDB, app data, and image: $($backup.FullName)"
Write-Output "Saved application image: $($app.Image)"
