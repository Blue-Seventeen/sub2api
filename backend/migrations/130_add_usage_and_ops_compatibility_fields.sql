-- Add compatibility observability fields to usage_logs and ops_error_logs.
-- All columns are nullable without defaults to preserve backward compatibility.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS client_profile VARCHAR(64),
    ADD COLUMN IF NOT EXISTS compatibility_route VARCHAR(128),
    ADD COLUMN IF NOT EXISTS fallback_chain TEXT,
    ADD COLUMN IF NOT EXISTS upstream_transport VARCHAR(64);

ALTER TABLE ops_error_logs
    ADD COLUMN IF NOT EXISTS client_profile VARCHAR(64),
    ADD COLUMN IF NOT EXISTS compatibility_route VARCHAR(128),
    ADD COLUMN IF NOT EXISTS fallback_chain TEXT,
    ADD COLUMN IF NOT EXISTS upstream_transport VARCHAR(64);

COMMENT ON COLUMN usage_logs.client_profile IS 'Detected downstream client family, e.g. claude_code / codex / cherry_studio.';
COMMENT ON COLUMN usage_logs.compatibility_route IS 'Canonical compatibility lane selected for this request.';
COMMENT ON COLUMN usage_logs.fallback_chain IS 'Observed fallback chain, e.g. native -> relay -> chat_fallback.';
COMMENT ON COLUMN usage_logs.upstream_transport IS 'Actual upstream transport lane, e.g. http_json / sse / ws_v2.';

COMMENT ON COLUMN ops_error_logs.client_profile IS 'Detected downstream client family, e.g. claude_code / codex / cherry_studio.';
COMMENT ON COLUMN ops_error_logs.compatibility_route IS 'Canonical compatibility lane selected for this request.';
COMMENT ON COLUMN ops_error_logs.fallback_chain IS 'Observed fallback chain, e.g. native -> relay -> chat_fallback.';
COMMENT ON COLUMN ops_error_logs.upstream_transport IS 'Actual upstream transport lane, e.g. http_json / sse / ws_v2.';
