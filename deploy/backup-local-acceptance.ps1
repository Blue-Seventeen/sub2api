param([string]$OutputDirectory)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-Docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    $result = & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker operation failed: $($Arguments[0])" }
    return $result
}

function Get-RollbackImageReference {
    param([Parameter(Mandatory = $true)]$Container)

    foreach ($reference in @($Container.RepoDigests)) {
        if ($reference -and $reference -match '.+@sha256:[0-9a-fA-F]{64}$') {
            return $reference.Trim()
        }
    }
    foreach ($reference in @($Container.RepoTags)) {
        if ($reference -and $reference -notmatch '^sha256:[0-9a-fA-F]{64}$') {
            return $reference.Trim()
        }
    }
    throw "Container has no repository digest or tag suitable for rollback: $($Container.Name)"
}

$root = Split-Path $PSScriptRoot -Parent
if (-not $OutputDirectory) {
    $OutputDirectory = Join-Path $root ('artifacts/local-acceptance-backup-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
}
$backup = New-Item -ItemType Directory -Path $OutputDirectory
$app = (Invoke-Docker inspect sub2api | ConvertFrom-Json)[0]
$postgres = (Invoke-Docker inspect sub2api-postgres | ConvertFrom-Json)[0]
$redis = (Invoke-Docker inspect sub2api-redis | ConvertFrom-Json)[0]
foreach ($container in @($app, $postgres, $redis)) {
    if (-not $container.State.Running) { throw "Container is not running: $($container.Name)" }
}

$envMap = @{}
foreach ($entry in $app.Config.Env) {
    $parts = $entry -split '=', 2
    $envMap[$parts[0]] = $parts[1]
}

$dump = '/tmp/sub2api-local-acceptance.dump'
Invoke-Docker exec sub2api-postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f /tmp/sub2api-local-acceptance.dump' | Out-Null
Invoke-Docker exec sub2api-postgres pg_restore --list $dump | Out-Null
Invoke-Docker cp "sub2api-postgres:$dump" (Join-Path $backup.FullName 'postgres.dump') | Out-Null

$unauthenticatedPing = & docker exec sub2api-redis sh -c 'unset REDISCLI_AUTH; redis-cli PING' 2>$null
$needsRedisAuth = $LASTEXITCODE -ne 0 -or $unauthenticatedPing -ne 'PONG'
$redisArgs = @('exec')
if ($needsRedisAuth -and $envMap.ContainsKey('REDIS_PASSWORD') -and $envMap.REDIS_PASSWORD) {
    $redisArgs += @('-e', "REDISCLI_AUTH=$($envMap.REDIS_PASSWORD)")
}
$redisArgs += @('sub2api-redis')
if (-not $needsRedisAuth) {
    $redisArgs += @('sh', '-c', 'unset REDISCLI_AUTH; exec redis-cli --rdb /tmp/sub2api-local-acceptance.rdb')
} else {
    $redisArgs += @('redis-cli')
}
if ($needsRedisAuth -and $envMap.ContainsKey('REDIS_USERNAME') -and $envMap.REDIS_USERNAME) {
    $redisArgs += @('--user', $envMap.REDIS_USERNAME)
}
if ($needsRedisAuth) { $redisArgs += @('--rdb', '/tmp/sub2api-local-acceptance.rdb') }
Invoke-Docker @redisArgs | Out-Null
Invoke-Docker exec sub2api-redis redis-check-rdb /tmp/sub2api-local-acceptance.rdb | Out-Null
Invoke-Docker cp 'sub2api-redis:/tmp/sub2api-local-acceptance.rdb' (Join-Path $backup.FullName 'redis.rdb') | Out-Null
Invoke-Docker cp 'sub2api:/app/data' (Join-Path $backup.FullName 'app-data') | Out-Null

$appRollbackImage = Get-RollbackImageReference -Container $app

# These private snapshots contain credentials and must never be published.
@($app, $postgres, $redis) | ConvertTo-Json -Depth 50 | Set-Content -LiteralPath (Join-Path $backup.FullName 'containers.private.json') -Encoding utf8
if (Test-Path (Join-Path $PSScriptRoot '.env')) {
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot '.env') -Destination (Join-Path $backup.FullName 'compose.private.env')
}
$metadata = [ordered]@{
    capturedAt = (Get-Date).ToUniversalTime().ToString('o')
    appImage = $app.Image
    appContainer = $app.Id
    postgresImage = $postgres.Image
    redisImage = $redis.Image
    files = @(Get-ChildItem $backup.FullName -File | Where-Object { $_.Name -in @('postgres.dump', 'redis.rdb') } | ForEach-Object {
        @{ name = $_.Name; bytes = $_.Length; sha256 = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash }
    })
    appRollback = '$env:SUB2API_IMAGE=''' + $appRollbackImage + '''; docker compose -f deploy/docker-compose.yml up -d --no-deps sub2api'
    rollbackNote = 'App-only rollback requires backward-compatible migrations. Restore DB/Redis/app-data together only after stopping app and accepting loss of post-backup writes.'
}
$metadata | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $backup.FullName 'manifest.json') -Encoding utf8
Write-Output "Verified PostgreSQL archive and Redis RDB: $($backup.FullName)"
Write-Output "Saved application image: $($app.Image)"
