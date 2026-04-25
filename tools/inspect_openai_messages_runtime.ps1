param(
    [string]$ServiceContainer = "sub2api-dev",
    [string]$PostgresContainer = "sub2api-postgres-dev",
    [string]$RedisContainer = "sub2api-redis-dev",
    [int]$ApiKeyId = 13,
    [int]$AccountId = 14,
    [int]$UsageLimit = 50,
    [int]$OpsLimit = 50,
    [int]$LogTail = 5000,
    [string]$OutputPrefix = "openai_messages_runtime_inspect"
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$resultsDir = Join-Path $repoRoot "test-results"
New-Item -ItemType Directory -Path $resultsDir -Force | Out-Null

$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$artifactStem = Join-Path $resultsDir "${OutputPrefix}_${timestamp}"
$markdownPath = "${artifactStem}.md"
$jsonPath = "${artifactStem}.json"

function Invoke-DockerCapture {
    param(
        [string]$Title,
        [string[]]$Arguments
    )

    try {
        $output = (& docker @Arguments 2>&1 | Out-String).TrimEnd()
        [pscustomobject]@{
            title  = $Title
            ok     = $true
            output = $output
        }
    }
    catch {
        [pscustomobject]@{
            title  = $Title
            ok     = $false
            output = $_.Exception.Message
        }
    }
}

function Invoke-ContainerShellCapture {
    param(
        [string]$Title,
        [string]$Container,
        [string]$Script
    )

    Invoke-DockerCapture -Title $Title -Arguments @("exec", $Container, "sh", "-lc", $Script)
}

function Invoke-PostgresQueryCapture {
    param(
        [string]$Title,
        [string]$Sql
    )

    $scriptTemplate = @'
export PGPASSWORD="${POSTGRES_PASSWORD:-}"
psql -v ON_ERROR_STOP=1 \
  -U "${POSTGRES_USER:-sub2api}" \
  -d "${POSTGRES_DB:-sub2api}" \
  -P pager=off \
  -P border=2 <<'SQL'
__SQL__
SQL
'@
    $script = $scriptTemplate.Replace("__SQL__", $Sql)
    Invoke-ContainerShellCapture -Title $Title -Container $PostgresContainer -Script $script
}

function Invoke-RedisScanCapture {
    param(
        [string]$Title,
        [string[]]$Patterns
    )

    try {
        $lines = New-Object System.Collections.Generic.List[string]
        foreach ($pattern in $Patterns) {
            [void]$lines.Add(">>> pattern=$pattern")
            $scan = Invoke-ContainerShellCapture -Title $Title -Container $RedisContainer -Script "unset REDISCLI_AUTH; redis-cli --scan --pattern '$pattern'"
            if (-not $scan.ok) {
                [void]$lines.Add("ERROR: $($scan.output)")
                [void]$lines.Add("")
                continue
            }
            $matched = @()
            if (-not [string]::IsNullOrWhiteSpace($scan.output)) {
                $matched = $scan.output -split "`r?`n" | Select-Object -First 50
            }
            if (($matched | Measure-Object).Count -eq 0) {
                [void]$lines.Add("(no keys)")
            }
            else {
                foreach ($line in $matched) {
                    [void]$lines.Add($line)
                }
            }
            [void]$lines.Add("")
        }
        [pscustomobject]@{
            title  = $Title
            ok     = $true
            output = ($lines -join "`n").TrimEnd()
        }
    }
    catch {
        [pscustomobject]@{
            title  = $Title
            ok     = $false
            output = $_.Exception.Message
        }
    }
}

$logKeywordPattern = "openai_messages|openai\.messages|account_select_failed|pool_mode_same_account_retry|upstream_failover_switching|520|502|request_completed"

$sections = @()

$sections += Invoke-DockerCapture -Title "docker_ps" -Arguments @("ps", "--format", "table {{.Names}}`t{{.Status}}`t{{.Ports}}")
$sections += Invoke-DockerCapture -Title "service_logs_filtered" -Arguments @(
    "logs",
    "--tail", "$LogTail",
    $ServiceContainer
)
$sections += Invoke-DockerCapture -Title "service_health" -Arguments @(
    "inspect",
    "--format",
    "{{json .State}}",
    $ServiceContainer
)

$usageSql = @"
SELECT
  created_at,
  inbound_endpoint,
  upstream_endpoint,
  requested_model,
  upstream_model,
  client_profile,
  compatibility_route,
  fallback_chain,
  upstream_transport,
  duration_ms,
  first_token_ms
FROM usage_logs
WHERE api_key_id = $ApiKeyId
  AND inbound_endpoint = '/v1/messages'
ORDER BY created_at DESC
LIMIT $UsageLimit;
"@
$sections += Invoke-PostgresQueryCapture -Title "usage_logs_recent" -Sql $usageSql

$opsSql = @"
SELECT
  created_at,
  status_code,
  upstream_status_code,
  inbound_endpoint,
  upstream_endpoint,
  requested_model,
  upstream_model,
  client_profile,
  compatibility_route,
  fallback_chain,
  upstream_transport,
  response_latency_ms,
  time_to_first_token_ms,
  upstream_error_message
FROM ops_error_logs
WHERE api_key_id = $ApiKeyId
  AND inbound_endpoint = '/v1/messages'
ORDER BY created_at DESC
LIMIT $OpsLimit;
"@
$sections += Invoke-PostgresQueryCapture -Title "ops_error_logs_recent" -Sql $opsSql

$accountSql = @"
SELECT
  id,
  name,
  platform,
  schedulable,
  rate_limited_at,
  overload_until,
  temp_unschedulable_until,
  temp_unschedulable_reason,
  updated_at
FROM accounts
WHERE id = $AccountId;
"@
$sections += Invoke-PostgresQueryCapture -Title "account_runtime_state" -Sql $accountSql

$sections += Invoke-RedisScanCapture -Title "redis_key_scan" -Patterns @(
    "*sticky*",
    "*session*",
    "*cooldown*",
    "*failover*",
    "openai*",
    "scheduler*"
)

$serviceLogsSection = $sections | Where-Object { $_.title -eq "service_logs_filtered" } | Select-Object -First 1
if ($serviceLogsSection) {
    $filteredLogs = ($serviceLogsSection.output -split "`r?`n" | Select-String -Pattern $logKeywordPattern | ForEach-Object { $_.Line }) -join "`n"
    if ([string]::IsNullOrWhiteSpace($filteredLogs)) {
        $filteredLogs = "(no matched log lines)"
    }
    $serviceLogsSection.output = $filteredLogs
}

$metadata = [ordered]@{
    generated_at       = (Get-Date).ToString("o")
    script             = $MyInvocation.MyCommand.Path
    repo_root          = $repoRoot
    service_container  = $ServiceContainer
    postgres_container = $PostgresContainer
    redis_container    = $RedisContainer
    api_key_id         = $ApiKeyId
    account_id         = $AccountId
    usage_limit        = $UsageLimit
    ops_limit          = $OpsLimit
    log_tail           = $LogTail
    artifacts          = @{
        markdown = $markdownPath
        json     = $jsonPath
    }
}

$report = [ordered]@{
    metadata = $metadata
    sections = $sections
}

$markdown = New-Object System.Text.StringBuilder
[void]$markdown.AppendLine("# OpenAI /v1/messages Runtime Inspect")
[void]$markdown.AppendLine()
[void]$markdown.AppendLine("- Generated At: $($metadata.generated_at)")
[void]$markdown.AppendLine("- Service Container: $ServiceContainer")
[void]$markdown.AppendLine("- Postgres Container: $PostgresContainer")
[void]$markdown.AppendLine("- Redis Container: $RedisContainer")
[void]$markdown.AppendLine("- API Key ID: $ApiKeyId")
[void]$markdown.AppendLine("- Account ID: $AccountId")
[void]$markdown.AppendLine()

foreach ($section in $sections) {
    [void]$markdown.AppendLine("## $($section.title)")
    [void]$markdown.AppendLine()
    [void]$markdown.AppendLine("- ok: $($section.ok)")
    [void]$markdown.AppendLine()
    [void]$markdown.AppendLine('```text')
    if ([string]::IsNullOrWhiteSpace($section.output)) {
        [void]$markdown.AppendLine("(empty)")
    }
    else {
        [void]$markdown.AppendLine($section.output.TrimEnd())
    }
    [void]$markdown.AppendLine('```')
    [void]$markdown.AppendLine()
}

$report | ConvertTo-Json -Depth 6 | Set-Content -Path $jsonPath -Encoding UTF8
$markdown.ToString() | Set-Content -Path $markdownPath -Encoding UTF8

Write-Host "Markdown: $markdownPath"
Write-Host "JSON    : $jsonPath"

foreach ($section in $sections) {
    Write-Host ""
    Write-Host "===== $($section.title) (ok=$($section.ok)) ====="
    if ([string]::IsNullOrWhiteSpace($section.output)) {
        Write-Host "(empty)"
    }
    else {
        Write-Host $section.output
    }
}
