$ErrorActionPreference = 'Stop'

. (Join-Path $PSScriptRoot 'subscription-window-assertions.ps1')

function Assert-True([bool]$Value, [string]$Message) {
    if (-not $Value) {
        throw $Message
    }
}

function Assert-False([bool]$Value, [string]$Message) {
    Assert-True (-not $Value) $Message
}

$start = [DateTimeOffset]'2026-09-10T12:00:00+08:00'
$nextDaily = $start.AddDays(1)
$nextWeekly = $start.AddDays(7)

Assert-True (Test-AnchorPhasePreserved $start $start ([TimeSpan]::FromDays(1))) 'unchanged daily anchor should preserve phase'
Assert-True (Test-AnchorPhasePreserved $start $nextDaily ([TimeSpan]::FromDays(1))) 'daily window advancement should preserve phase'
Assert-True (Test-WindowStartProgressionValid $start $start ([TimeSpan]::FromDays(1))) 'unchanged daily window should be valid'
Assert-True (Test-WindowStartProgressionValid $start $nextDaily ([TimeSpan]::FromDays(1))) 'one daily period advancement should be valid'
Assert-True (Test-WindowStartProgressionValid $start $nextWeekly ([TimeSpan]::FromDays(7))) 'one weekly period advancement should be valid'
Assert-False (Test-AnchorPhasePreserved $start $start.AddHours(12) ([TimeSpan]::FromDays(1))) 'half-period movement must not preserve phase'
Assert-False (Test-WindowStartProgressionValid $start $start.AddHours(12) ([TimeSpan]::FromDays(1))) 'half-period movement must not be valid progression'
Assert-True (Test-AnchorPhasePreserved $null $null ([TimeSpan]::FromDays(1))) 'two null windows should remain equivalent'
Assert-False (Test-AnchorPhasePreserved $null $start ([TimeSpan]::FromDays(1))) 'null to timestamp must not be equivalent'

Write-Output 'subscription window assertion tests passed'
