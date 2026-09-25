function Test-AnchorPhasePreserved {
    param(
        [object]$Before,
        [object]$After,
        [TimeSpan]$Period
    )

    if ($null -eq $Before -or $null -eq $After) {
        return $Before -eq $After
    }
    if ($Period.TotalSeconds -le 0) {
        return $false
    }

    $previous = [DateTimeOffset]$Before
    $current = [DateTimeOffset]$After
    $elapsed = ($current - $previous).TotalSeconds
    if ($elapsed -lt -1) {
        return $false
    }

    # A rolling window may stay put or advance by complete periods.
    $remainder = [Math]::Abs($elapsed % $Period.TotalSeconds)
    $distanceToBoundary = [Math]::Min($remainder, $Period.TotalSeconds - $remainder)
    return $distanceToBoundary -le 1
}

function Test-WindowStartProgressionValid {
    param(
        [object]$Before,
        [object]$After,
        [TimeSpan]$Period
    )

    if ($null -eq $Before -or $null -eq $After) {
        return $Before -eq $After
    }
    if ($Period.TotalSeconds -le 0) {
        return $false
    }

    $previous = [DateTimeOffset]$Before
    $current = [DateTimeOffset]$After
    if ($current -gt [DateTimeOffset]::UtcNow.AddSeconds(1)) {
        return $false
    }

    $elapsed = ($current - $previous).TotalSeconds
    if ($elapsed -lt -1) {
        return $false
    }
    if ([Math]::Abs($elapsed) -le 1) {
        return $true
    }

    return $elapsed -ge ($Period.TotalSeconds - 1) -and
        (Test-AnchorPhasePreserved $Before $After $Period)
}
