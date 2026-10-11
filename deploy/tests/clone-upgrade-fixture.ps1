param(
    [string]$AppContainer = 'sub2api',
    [string]$PostgresContainer = 'sub2api-postgres',
    [string]$RedisContainer = 'sub2api-redis',
    [ValidateRange(1024, 65535)][int]$Port = 18085,
    [string]$OutputDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-CheckedDocker {
    $Arguments = [string[]]$args
    $output = & docker @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Fixture Docker operation failed: $($Arguments[0])" }
    return $output
}

function Inspect-Container([string]$Name) {
    $items = @(Invoke-CheckedDocker inspect $Name | ConvertFrom-Json)
    if ($items.Count -ne 1 -or -not $items[0].State.Running) {
        throw 'All selected source containers must be running.'
    }
    return $items[0]
}

function Environment-Map($Container) {
    $map = @{}
    foreach ($entry in $Container.Config.Env) {
        $parts = $entry -split '=', 2
        $map[$parts[0]] = $parts[1]
    }
    return $map
}

function Wait-FixtureHealth([string]$Name) {
    $deadline = [DateTime]::UtcNow.AddSeconds(150)
    do {
        $state = @(Invoke-CheckedDocker inspect $Name | ConvertFrom-Json)[0].State
        if (-not $state.Running) { throw 'Fixture application exited before becoming healthy.' }
        if ($state.Health.Status -eq 'healthy') { return }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Fixture application health timeout.'
}

$app = Inspect-Container $AppContainer
$pg = Inspect-Container $PostgresContainer
$redis = Inspect-Container $RedisContainer
$appEnv = Environment-Map $app
$pgEnv = Environment-Map $pg
if ($appEnv.DATABASE_DBNAME -cne $pgEnv.POSTGRES_DB -or
    $appEnv.DATABASE_USER -cne $pgEnv.POSTGRES_USER) {
    throw 'Selected PostgreSQL does not match the application database/user.'
}
foreach ($source in @($pg, $redis)) {
    $common = @($app.NetworkSettings.Networks.PSObject.Properties.Name |
        Where-Object { $_ -in $source.NetworkSettings.Networks.PSObject.Properties.Name })
    if (-not $common.Count) { throw 'Selected source containers do not share an application network.' }
}
if (-not $appEnv.DATABASE_PASSWORD) { throw 'Application database password is required for the fixture.' }
if ($appEnv.DATABASE_HOST -notin @('postgres', $PostgresContainer) -or
    $appEnv.REDIS_HOST -notin @('redis', $RedisContainer)) {
    throw 'Source uses external database/cache endpoints; this helper only clones selected Compose services.'
}

$id = 'sub2api-v0215-' + [guid]::NewGuid().ToString('N').Substring(0, 10)
if (-not $OutputDirectory) {
    $repo = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
    $OutputDirectory = Join-Path $repo "artifacts/$id"
}
$root = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDirectory)
if (Test-Path -LiteralPath $root) { throw 'Fixture output directory must be new.' }
$null = New-Item -ItemType Directory -Path $root
if ($IsWindows) {
    & icacls $root /inheritance:r /grant:r "$env:USERDOMAIN\$env:USERNAME`:(OI)(CI)F" 'SYSTEM:(OI)(CI)F' 'BUILTIN\Administrators:(OI)(CI)F' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot restrict private fixture directory permissions.' }
} else {
    & chmod 700 $root
    if ($LASTEXITCODE -ne 0) { throw 'Cannot restrict private fixture directory permissions.' }
}
$snapshot = Join-Path $root 'snapshot'
$null = New-Item -ItemType Directory -Path $snapshot
$temp = "/tmp/$id"
$stopped = $false
try {
    # Quiesce application writers while taking the coordinated DB/cache/data copy.
    Invoke-CheckedDocker stop --time 30 $app.Id | Out-Null
    $stopped = $true
    Invoke-CheckedDocker exec $pg.Id pg_dump -U $appEnv.DATABASE_USER -d $appEnv.DATABASE_DBNAME -Fc -f "$temp.dump" | Out-Null
    Invoke-CheckedDocker exec $pg.Id pg_restore --list "$temp.dump" | Out-Null
    Invoke-CheckedDocker cp "$($pg.Id):$temp.dump" (Join-Path $snapshot 'postgres.dump') | Out-Null
    if ($appEnv.REDIS_PASSWORD) {
        $redisArgs = @('exec', '-e', "REDISCLI_AUTH=$($appEnv.REDIS_PASSWORD)", $redis.Id, 'redis-cli')
        if ($appEnv.REDIS_USERNAME) { $redisArgs += @('--user', $appEnv.REDIS_USERNAME) }
        $redisArgs += @('--rdb', "$temp.rdb")
        Invoke-CheckedDocker @redisArgs | Out-Null
    } else {
        Invoke-CheckedDocker exec $redis.Id sh -c 'unset REDISCLI_AUTH; exec redis-cli --rdb "$1"' snapshot "$temp.rdb" | Out-Null
    }
    Invoke-CheckedDocker exec $redis.Id redis-check-rdb "$temp.rdb" | Out-Null
    Invoke-CheckedDocker cp "$($redis.Id):$temp.rdb" (Join-Path $snapshot 'redis.rdb') | Out-Null
    Invoke-CheckedDocker cp "$($app.Id):/app/data" (Join-Path $snapshot 'app-data') | Out-Null
    $app.Config.Env | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $snapshot 'app-env.private.json') -Encoding utf8
} finally {
    if ($stopped) { Invoke-CheckedDocker start $app.Id | Out-Null }
    Invoke-CheckedDocker exec $pg.Id rm -f "$temp.dump" | Out-Null
    Invoke-CheckedDocker exec $redis.Id rm -f "$temp.rdb" | Out-Null
}
Wait-FixtureHealth $AppContainer

$appData = Join-Path $root 'app-data'
Copy-Item -LiteralPath (Join-Path $snapshot 'app-data') -Destination $appData -Recurse
$redisData = Join-Path $root 'redis-data'
$null = New-Item -ItemType Directory -Path $redisData
Copy-Item -LiteralPath (Join-Path $snapshot 'redis.rdb') -Destination (Join-Path $redisData 'dump.rdb')
$appEnv.DATABASE_HOST = 'postgres'
$appEnv.REDIS_HOST = 'redis'
$appEnv.SERVER_HOST = '0.0.0.0'
$appEnv.SERVER_PORT = '8080'
$appEnv.AUTO_SETUP = 'true'
$appEnv.DATABASE_USER_PLATFORM_QUOTA_FLUSHER_ENABLED = 'false'
$envFile = Join-Path $root 'app.env.private'
$lines = foreach ($name in $appEnv.Keys | Sort-Object) {
    $value = [string]$appEnv[$name]
    if ($value.Contains("`n") -or $value.Contains("`r")) { throw 'Multiline source environment cannot be passed through an env file.' }
    "$name=$value"
}
$lines | Set-Content -LiteralPath $envFile -Encoding utf8NoBOM
$network = "$id-network"
$pgName = "$id-postgres"
$redisName = "$id-redis"
$appName = "$id-baseline"
Invoke-CheckedDocker network create $network | Out-Null
Invoke-CheckedDocker run -d --name $pgName --network $network --network-alias postgres `
    -e "POSTGRES_USER=$($appEnv.DATABASE_USER)" -e "POSTGRES_DB=$($appEnv.DATABASE_DBNAME)" `
    -e "POSTGRES_PASSWORD=$($appEnv.DATABASE_PASSWORD)" $pg.Image | Out-Null
$deadline = [DateTime]::UtcNow.AddSeconds(90)
do {
    & docker exec $pgName pg_isready -U $appEnv.DATABASE_USER -d $appEnv.DATABASE_DBNAME *> $null
    if ($LASTEXITCODE -eq 0) { break }
    Start-Sleep -Seconds 1
} while ([DateTime]::UtcNow -lt $deadline)
if ($LASTEXITCODE -ne 0) { throw 'Fixture PostgreSQL readiness timeout.' }
Invoke-CheckedDocker cp (Join-Path $snapshot 'postgres.dump') "${pgName}:/tmp/baseline.dump" | Out-Null
Invoke-CheckedDocker exec $pgName pg_restore --exit-on-error --no-owner -U $appEnv.DATABASE_USER -d $appEnv.DATABASE_DBNAME /tmp/baseline.dump | Out-Null
$redisRun = @('run', '-d', '--name', $redisName, '--network', $network, '--network-alias', 'redis',
    '--mount', "type=bind,src=$redisData,dst=/data", $redis.Image, 'redis-server', '--appendonly', 'no')
if ($appEnv.REDIS_PASSWORD) { $redisRun += @('--requirepass', $appEnv.REDIS_PASSWORD) }
Invoke-CheckedDocker @redisRun | Out-Null
Invoke-CheckedDocker run -d --name $appName --network $network --env-file $envFile `
    --mount "type=bind,src=$appData,dst=/app/data" -p "127.0.0.1:${Port}:8080" `
    --add-host host.docker.internal:host-gateway $app.Image | Out-Null
Wait-FixtureHealth $appName
$manifest = [ordered]@{
    captured_at = [DateTimeOffset]::Now.ToString('o')
    network = $network
    baseline_image = $app.Image
    baseline_app = $appName
    postgres = $pgName
    redis = $redisName
    port = $Port
    snapshot = $snapshot
    app_data = $appData
    env_file = $envFile
    source_app = $AppContainer
    source_recovered_healthy = $true
    snapshot_files = @(Get-ChildItem -LiteralPath $snapshot -Recurse -File | ForEach-Object {
        @{ name = $_.FullName.Substring($snapshot.Length + 1); bytes = $_.Length; sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash }
    })
}
$manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $root 'manifest.json') -Encoding utf8
Write-Output "FIXTURE_DIR=$root"
Write-Output "FIXTURE_BASE_URL=http://127.0.0.1:$Port"
