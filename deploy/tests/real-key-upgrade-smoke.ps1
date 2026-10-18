#Requires -Version 7.3
param(
    [Parameter(Mandatory = $true)][string]$ManifestPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
Add-Type -AssemblyName System.Net.Http
$manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
if ($manifest.postgres -cnotmatch '^sub2api-v0215-[a-f0-9]{10}-postgres$' -or $manifest.port -eq 8080) {
    throw 'This runner only accepts an isolated upgrade fixture.'
}
$base = "http://127.0.0.1:$($manifest.port)"
$rows = & docker exec $manifest.postgres psql -U sub2api -d sub2api -Atc @'
SELECT COALESCE(json_agg(t), '[]'::json) FROM (
  SELECT DISTINCT ON (g.platform) g.platform, k.key, g.id AS group_id,
         a.id AS account_id, a.credentials->'model_mapping' AS model_mapping
  FROM groups g JOIN api_keys k ON k.group_id=g.id
  JOIN account_groups ag ON ag.group_id=g.id JOIN accounts a ON a.id=ag.account_id
  WHERE g.deleted_at IS NULL AND k.deleted_at IS NULL AND a.deleted_at IS NULL
    AND g.status='active' AND k.status='active' AND a.status='active'
    AND a.platform=g.platform
    AND a.name NOT LIKE 'v0215-%' AND g.name NOT LIKE 'v0215-%'
    AND COALESCE(a.credentials->>'api_key','') NOT LIKE 'sk-fixture-%'
  ORDER BY g.platform, k.id, a.id
) t;
'@
if ($LASTEXITCODE -ne 0) { throw 'Cannot read fixture platform-group keys.' }
$keys = @(($rows -join "`n") | ConvertFrom-Json)
$privateEnv = @{}
foreach ($entry in (Get-Content (Join-Path $manifest.snapshot 'app-env.private.json') -Raw | ConvertFrom-Json)) {
    $parts = $entry -split '=', 2
    $privateEnv[$parts[0]] = $parts[1]
}
$http = [Net.Http.HttpClient]::new()
$http.Timeout = [TimeSpan]::FromSeconds(60)
$results = [Collections.Generic.List[object]]::new()
$business = [Collections.Generic.List[object]]::new()
$preferred = @{
    openai = @('gpt-5.5', 'gpt-5.4', 'gpt-5.1-codex', 'gpt-5.1')
    anthropic = @('claude-sonnet-4-6', 'claude-sonnet-4-5-20250929')
    grok = @('grok-4.7', 'grok-4.2', 'grok-4')
    zhipu = @('glm-5.3', 'glm-5.2', 'glm-5', 'glm-4.7')
    deepseek = @('deepseek-v4-flash', 'deepseek-chat', 'deepseek-v4-pro')
}
try {
    $loginBody = @{email=$privateEnv.ADMIN_EMAIL; password=$privateEnv.ADMIN_PASSWORD} | ConvertTo-Json -Compress
    $login = $http.PostAsync("$base/api/v1/auth/login", [Net.Http.StringContent]::new($loginBody, [Text.Encoding]::UTF8, 'application/json')).GetAwaiter().GetResult()
    $business.Add(@{name='login'; status=[int]$login.StatusCode; passed=$login.IsSuccessStatusCode})
    if ($login.IsSuccessStatusCode) {
        $loginData = $login.Content.ReadAsStringAsync().GetAwaiter().GetResult() | ConvertFrom-Json
        $token = [string]$loginData.data.access_token
        foreach ($path in @('admin/accounts', 'admin/groups', 'admin/subscriptions', 'admin/usage',
            'promotion/me/overview', 'admin/promotion/dashboard', 'admin/proxies',
            'admin/proxies/clash-subscriptions', 'admin/accounts/auto-ops', 'model-plaza',
            'admin/ops/dashboard/overview')) {
            $req = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Get, "$base/api/v1/$path")
            $req.Headers.Authorization = [Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', $token)
            $resp = $http.SendAsync($req).GetAwaiter().GetResult()
            $business.Add(@{name=$path; status=[int]$resp.StatusCode; passed=$resp.IsSuccessStatusCode})
            $resp.Dispose()
            $req.Dispose()
        }
    }
    $login.Dispose()
    foreach ($row in $keys) {
        $platform = [string]$row.platform
        if ($platform -notin @('openai', 'anthropic', 'grok', 'zhipu', 'deepseek', 'kimi')) { continue }
        $modelRequest = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Get, "$base/v1/models")
        $modelRequest.Headers.Authorization = [Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', [string]$row.key)
        $modelResponse = $http.SendAsync($modelRequest).GetAwaiter().GetResult()
        $models = @()
        if ($modelResponse.IsSuccessStatusCode) {
            $modelData = $modelResponse.Content.ReadAsStringAsync().GetAwaiter().GetResult() | ConvertFrom-Json
            $models = @($modelData.data | ForEach-Object { [string]$_.id } | Where-Object { $_ -and -not $_.Contains('*') })
        }
        $modelResponse.Dispose()
        $modelRequest.Dispose()
        $choices = if ($preferred.ContainsKey($platform)) { $preferred[$platform] } else { @() }
        $model = @($choices | Where-Object { $_ -in $models } | Select-Object -First 1)
        if (-not $model.Count) { $model = @($models | Select-Object -First 1) }
        if (-not $model.Count) {
            $results.Add(@{platform=$platform; passed=$false; reason='no_admitted_model'; status=0})
            continue
        }
        $model = [string]$model[0]
        $route = if ($platform -eq 'anthropic') { '/v1/messages' } elseif ($platform -eq 'openai') { '/v1/responses' } else { '/v1/chat/completions' }
        $body = @{model=$model; stream=$true; messages=@(@{role='user';content='Reply only OK'})}
        if ($route -eq '/v1/messages') { $body.max_tokens=32 }
        elseif ($route -eq '/v1/responses') { $body.Remove('messages'); $body.input='Reply only OK'; $body.max_output_tokens=64 }
        else { $body.max_tokens=64 }
        $clientId = 'v0215real_' + [guid]::NewGuid().ToString('N')
        $req = [Net.Http.HttpRequestMessage]::new([Net.Http.HttpMethod]::Post, "$base$route")
        $req.Headers.Authorization = [Net.Http.Headers.AuthenticationHeaderValue]::new('Bearer', [string]$row.key)
        $req.Headers.Add('X-Client-Request-ID', $clientId)
        if ($route -eq '/v1/messages') { $req.Headers.Add('anthropic-version', '2023-06-01') }
        $req.Content = [Net.Http.StringContent]::new(($body | ConvertTo-Json -Depth 8 -Compress), [Text.Encoding]::UTF8, 'application/json')
        try {
            $resp = $http.SendAsync($req).GetAwaiter().GetResult()
            $correlation = @($resp.Headers.GetValues('X-Client-Request-ID'))[0]
            $wire = $resp.Content.ReadAsStringAsync().GetAwaiter().GetResult()
            $terminal = $false
            $failure = $false
            foreach ($line in ($wire -split '\r?\n')) {
                if ($line -notmatch '^data:\s*(.*)$') { continue }
                $data = $Matches[1].Trim()
                if ($data -eq '[DONE]') { $terminal = $true; continue }
                try { $event = $data | ConvertFrom-Json } catch { $failure = $true; continue }
                $type = if ($event.PSObject.Properties['type']) { [string]$event.type } else { '' }
                if ($type -in @('response.completed', 'message_stop')) { $terminal = $true }
                if ($type -in @('response.failed', 'response.incomplete', 'error') -or
                    $event.PSObject.Properties['error']) { $failure = $true }
            }
            $errorCode = ''
            if (-not $resp.IsSuccessStatusCode) {
                try {
                    $errorBody = $wire | ConvertFrom-Json
                    if ($errorBody.PSObject.Properties['error'] -and $errorBody.error.PSObject.Properties['type']) {
                        $errorCode = [string]$errorBody.error.type
                    }
                } catch {}
            }
            $results.Add(@{platform=$platform;model=$model;route=$route;status=[int]$resp.StatusCode;error_code=$errorCode;
                terminal=$terminal;failed_event=$failure;request_id="client:$correlation";
                passed=($resp.IsSuccessStatusCode -and $terminal -and -not $failure)})
            $resp.Dispose()
        } catch {
            $results.Add(@{platform=$platform;model=$model;route=$route;status=0;request_id="client:$clientId";passed=$false;reason='transport_or_timeout'})
        } finally { $req.Dispose() }
    }
} finally {
    $http.Dispose()
    $privateEnv.Clear()
}
$ids = @($results | Where-Object { $_.ContainsKey('request_id') } | ForEach-Object { "'$($_.request_id)'" })
$usage = @()
if ($ids.Count) {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        $raw = & docker exec $manifest.postgres psql -U sub2api -d sub2api -Atc "select coalesce(json_agg(t),'[]'::json) from (select request_id,model,requested_model,upstream_model,upstream_response_model,input_tokens,output_tokens,actual_cost,real_actual_cost,first_token_ms,inbound_endpoint,upstream_endpoint,rate_multiplier,unified_rate_multiplier from usage_logs where request_id in ($($ids -join ','))) t"
        if ($LASTEXITCODE -ne 0) { throw 'Fixture usage query failed.' }
        $usage = @(($raw -join "`n") | ConvertFrom-Json)
        if ($usage.Count -ge @($results | Where-Object { $_.passed }).Count) { break }
        Start-Sleep -Seconds 1
    } while ([DateTime]::UtcNow -lt $deadline)
}
foreach ($result in $results) {
    if (-not $result.ContainsKey('request_id')) { continue }
    $found = @($usage | Where-Object { $_.request_id -eq $result.request_id })
    $result.usage_rows = $found.Count
    $result.billing_verified = $found.Count -eq 1 -and $found[0].actual_cost -gt 0 -and
        ($found[0].input_tokens + $found[0].output_tokens) -gt 0 -and $null -ne $found[0].first_token_ms
    if ($result.passed) { $result.passed = [bool]$result.billing_verified }
}
$report = @{
    tested_at=[DateTimeOffset]::Now.ToString('o');base_url=$base;business=$business;
    real_key_results=$results;usage=$usage;missing_credentials=@(@('kimi','opencode_go','command_code','cline','typesafe') | Where-Object { $_ -notin $keys.platform });
    websocket_real_key_tested=$false
}
$output = Join-Path (Split-Path $ManifestPath -Parent) 'real-key-smoke.json'
$report | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $output -Encoding utf8
$results | Select-Object platform,model,status,terminal,usage_rows,billing_verified,passed | Format-Table -AutoSize
$business | ForEach-Object { [pscustomobject]$_ } | Format-Table name,status,passed -AutoSize
Write-Output "SMOKE_REPORT=$output"
if (@($results | Where-Object { -not $_.passed }).Count -or @($business | Where-Object { -not $_.passed }).Count) { exit 1 }
