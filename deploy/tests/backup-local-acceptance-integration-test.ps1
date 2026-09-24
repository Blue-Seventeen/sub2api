param(
    [Parameter(Mandatory = $true)][string]$AppContainer,
    [Parameter(Mandatory = $true)][string]$PostgresContainer,
    [Parameter(Mandatory = $true)][string]$RedisContainer,
    [ValidateRange(1, 600)][int]$HealthTimeoutSeconds = 120,
    # Only for disposable fixtures: removes the stopped original container.
    [switch]$RecreateApp
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Invoke-DockerJson {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
    $output = & docker @Arguments 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker operation failed: $($Arguments[0])"
    }
    return ($output | ConvertFrom-Json)[0]
}

$deployRoot = Split-Path $PSScriptRoot -Parent
if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA must identify a private integration-test backup directory.' }
$privateBackupRoot = Join-Path $env:LOCALAPPDATA 'Sub2API\private-backups'
$backupPath = Join-Path $privateBackupRoot ('backup-local-acceptance-it-' + [guid]::NewGuid().ToString('N'))
$privateBackupRootFull = [System.IO.Path]::GetFullPath($privateBackupRoot).TrimEnd([System.IO.Path]::DirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
$scriptPath = Join-Path $deployRoot 'backup-local-acceptance.ps1'
$scriptCommand = Get-Command -Name $scriptPath -ErrorAction Stop
foreach ($parameterName in @('AppContainer', 'PostgresContainer', 'RedisContainer', 'KeepAppStopped')) {
    if (-not $scriptCommand.Parameters.ContainsKey($parameterName)) {
        throw "Backup script is missing the required parameter: $parameterName"
    }
}

$appBefore = Invoke-DockerJson inspect $AppContainer
$postgresBefore = Invoke-DockerJson inspect $PostgresContainer
$redisBefore = Invoke-DockerJson inspect $RedisContainer
if (-not $appBefore.State.Running) {
    throw "Test app container is not running: $AppContainer"
}
$backupVerified = $false
$appHealthy = $false
$manifest = $null
$originalRemoved = $false

try {
    & $scriptPath `
        -OutputDirectory $backupPath `
        -AppContainer $AppContainer `
        -PostgresContainer $PostgresContainer `
        -RedisContainer $RedisContainer `
        -KeepAppStopped

    $appAfterBackup = Invoke-DockerJson inspect $AppContainer
    if ($appAfterBackup.State.Running) {
        throw 'Backup completed without keeping the application stopped for cutover.'
    }

    $manifestPath = Join-Path $backupPath 'manifest.json'
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($manifest.appRollbackMode -ne 'engine-recreate-or-retained' -or
        -not [System.IO.Path]::IsPathFullyQualified($manifest.appImageArchive) -or
        -not [System.IO.Path]::IsPathFullyQualified($manifest.appRollbackScript) -or
        -not [System.IO.Path]::IsPathFullyQualified($manifest.appRecoveryDefinition) -or
        -not (Test-Path -LiteralPath $manifest.appRecoveryDefinition -PathType Leaf) -or
        -not (Test-Path -LiteralPath $manifest.appRollbackScript -PathType Leaf)) {
        throw 'Rollback must provide absolute image, private definition, and recovery script paths.'
    }
    $definition = Get-Content -LiteralPath $manifest.appRecoveryDefinition -Raw | ConvertFrom-Json -AsHashtable
    if ($definition.createBody.Image -cne $appBefore.Image -or
        $definition.recoveryName -cne $manifest.appRecoveryContainerName -or
        @($definition.createBody.Env).Count -ne @($appBefore.Config.Env).Count -or
        @(Compare-Object @($definition.createBody.Env) @($appBefore.Config.Env) -CaseSensitive).Count -ne 0) {
        throw 'Private recovery definition does not preserve application image/environment.'
    }
    if ($manifest.appImage -ne $appBefore.Image -or $manifest.appRollbackImage -ne $appBefore.Image) {
        throw 'Rollback image does not match the original running application image ID.'
    }
    $databaseSetting = @($appBefore.Config.Env | Where-Object { $_ -like 'DATABASE_DBNAME=*' })
    if ($databaseSetting.Count -ne 1 -or $manifest.postgresDatabase -cne $databaseSetting[0].Substring('DATABASE_DBNAME='.Length)) {
        throw 'Backup database does not match the application database.'
    }
    if ($manifest.appContainerId -ne $appBefore.Id -or
        $manifest.postgresContainerId -ne $postgresBefore.Id -or
        $manifest.redisContainerId -ne $redisBefore.Id) {
        throw 'Backup manifest does not match the explicitly requested containers.'
    }

    $checksumPath = Join-Path $backupPath 'SHA256SUMS'
    if (-not (Test-Path -LiteralPath $checksumPath)) {
        throw 'Backup SHA256SUMS file is missing.'
    }
    $checksumLines = @(Get-Content -LiteralPath $checksumPath)
    $verifiedPaths = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)
    foreach ($line in $checksumLines) {
        if ($line -notmatch '^([0-9A-Fa-f]{64})  (.+)$') {
            throw "Malformed checksum entry: $line"
        }
        $expectedHash = $Matches[1].ToUpperInvariant()
        $relativePath = $Matches[2].Replace('/', [System.IO.Path]::DirectorySeparatorChar)
        if (-not $verifiedPaths.Add($relativePath)) {
            throw "Duplicate checksum entry: $relativePath"
        }
        $filePath = [System.IO.Path]::GetFullPath((Join-Path $backupPath $relativePath))
        $backupPrefix = [System.IO.Path]::GetFullPath($backupPath).TrimEnd([System.IO.Path]::DirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
        if (-not $filePath.StartsWith($backupPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
            throw 'Checksum entry points outside the backup directory.'
        }
        if (-not (Test-Path -LiteralPath $filePath -PathType Leaf)) {
            throw "Checksummed backup file is missing: $relativePath"
        }
        $actualHash = (Get-FileHash -LiteralPath $filePath -Algorithm SHA256).Hash
        if ($actualHash -ne $expectedHash) {
            throw "Backup checksum mismatch: $relativePath"
        }
    }

    $backupEntries = @(Get-ChildItem -LiteralPath $backupPath -File -Recurse)
    $expectedChecksumCount = @($backupEntries | Where-Object { $_.Name -ne 'SHA256SUMS' }).Count
    if ($verifiedPaths.Count -ne $expectedChecksumCount) {
        throw "Checksum manifest covers $($verifiedPaths.Count) files, expected $expectedChecksumCount."
    }
    if (-not ($backupEntries | Where-Object { $_.Name -eq 'postgres.dump' })) {
        throw 'PostgreSQL archive is missing.'
    }
    if (-not ($backupEntries | Where-Object { $_.Name -eq 'redis.rdb' })) {
        throw 'Redis RDB archive is missing.'
    }
    if (-not ($backupEntries | Where-Object { $_.Name -eq 'app-image.tar' })) {
        throw 'Application image archive is missing.'
    }

    if ($RecreateApp) {
        # Verify the real Engine transport before crossing the removal boundary.
        & $manifest.appRollbackScript -EnginePreflightOnly
        & docker rm $appBefore.Id 2>$null | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'Failed to remove the stopped disposable application fixture.' }
        $originalRemoved = $true
    }
    $backupVerified = $true
}
finally {
    try {
        $recoveryTarget = $appBefore.Id
        if ($backupVerified) {
            & $manifest.appRollbackScript -HealthTimeoutSeconds $HealthTimeoutSeconds -ConfirmAppWritersStopped:$originalRemoved
            if ($originalRemoved) { $recoveryTarget = $manifest.appRecoveryContainerName }
        } else {
            $appState = Invoke-DockerJson inspect $appBefore.Id
            if ($appState.Id -ne $appBefore.Id -or $appState.Image -ne $appBefore.Image) {
                throw 'The original application container changed during the integration test.'
            }
            if (-not $appState.State.Running) {
                & docker start $appBefore.Id 2>$null | Out-Null
                if ($LASTEXITCODE -ne 0) { throw "Failed to restart test application container: $AppContainer" }
            }
        }
        $deadline = [DateTime]::UtcNow.AddSeconds($HealthTimeoutSeconds)
        do {
            $appState = Invoke-DockerJson inspect $recoveryTarget
            if ($appState.Image -cne $appBefore.Image) { throw 'Recovered application image does not match the original.' }
            if (-not $appState.State.Running) { throw 'Application stopped during recovery health verification.' }
            if (-not $appState.State.PSObject.Properties['Health']) { throw 'Integration recovery requires a configured Docker healthcheck.' }
            if ($appState.State.Health.Status -eq 'healthy') { $appHealthy = $true; break }
            if ($appState.State.Health.Status -eq 'unhealthy') { throw 'Application is unhealthy after integration recovery.' }
            Start-Sleep -Seconds 2
        } while ([DateTime]::UtcNow -lt $deadline)
        if (-not $appHealthy) { throw 'Application recovery healthcheck timed out.' }
    } finally {
        if ($backupVerified -and $appHealthy) {
            $resolvedBackupPath = [System.IO.Path]::GetFullPath($backupPath)
            if (-not $resolvedBackupPath.StartsWith($privateBackupRootFull, [System.StringComparison]::OrdinalIgnoreCase) -or
                (Split-Path $resolvedBackupPath -Leaf) -notlike 'backup-local-acceptance-it-*') {
                throw "Refusing to remove an unexpected backup path: $resolvedBackupPath"
            }
            if (Test-Path -LiteralPath $resolvedBackupPath) {
                Remove-Item -LiteralPath $resolvedBackupPath -Recurse -Force
            }
        } else {
            Write-Warning "Integration test failed; recovery artifacts retained at: $backupPath"
        }
    }
}

Write-Output "Backup integration test passed after application health verification and cleanup: $backupPath"
