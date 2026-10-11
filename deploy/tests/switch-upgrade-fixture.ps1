param(
    [Parameter(Mandatory = $true)][string]$ManifestPath,
    [string]$Image = 'sub2api-custom:v0.2.15-candidate',
    [switch]$Rollback
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Docker-Checked {
    $arguments = [string[]]$args
    $result = & docker @arguments 2>$null
    if ($LASTEXITCODE -ne 0) { throw "Fixture switch failed: docker $($arguments[0])" }
    return $result
}

$manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
if ($manifest.baseline_app -cnotmatch '^sub2api-v0215-[a-f0-9]{10}-baseline$' -or
    $manifest.network -cne ($manifest.baseline_app -replace '-baseline$', '-network') -or
    $manifest.port -eq 8080) {
    throw 'Refusing to switch a non-disposable application or the primary application port.'
}
$statePath = Join-Path (Split-Path $ManifestPath -Parent) 'switch-state.json'
$current = $manifest.baseline_app
if (Test-Path -LiteralPath $statePath) {
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if ($state.app -cnotmatch '^sub2api-v0215-[a-f0-9]{10}-(baseline|candidate-[a-f0-9]{8})$' -or
        $state.network -cne $manifest.network) { throw 'Invalid fixture switch state.' }
    $current = $state.app
}
if ($Rollback) {
    $target = $manifest.baseline_app
} else {
    $target = ($manifest.baseline_app -replace '-baseline$', '-candidate-') + [guid]::NewGuid().ToString('N').Substring(0, 8)
    Docker-Checked image inspect $Image | Out-Null
}
Docker-Checked stop --time 30 $current | Out-Null
try {
    if ($Rollback) {
        Docker-Checked start $target | Out-Null
    } else {
        Docker-Checked run -d --name $target --network $manifest.network --env-file $manifest.env_file `
            --mount "type=bind,src=$($manifest.app_data),dst=/app/data" `
            -p "127.0.0.1:$($manifest.port):8080" --add-host host.docker.internal:host-gateway $Image | Out-Null
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(150)
    do {
        $container = @(Docker-Checked inspect $target | ConvertFrom-Json)[0]
        if (-not $container.State.Running) { throw 'Switched fixture application exited.' }
        if ($container.State.Health.Status -eq 'healthy') { break }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($container.State.Health.Status -ne 'healthy') { throw 'Switched fixture health timeout.' }
    $state = [ordered]@{
        app = $target
        image = $container.Image
        configured_image = $container.Config.Image
        network = $manifest.network
        checked_at = [DateTimeOffset]::Now.ToString('o')
        rollback_on_upgraded_data = [bool]$Rollback
        healthy = $true
    }
    $state | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding utf8
    Write-Output "FIXTURE_APP=$target"
    Write-Output "FIXTURE_HEALTH=healthy"
} catch {
    & docker stop --time 15 $target *> $null
    Docker-Checked start $current | Out-Null
    throw
}
