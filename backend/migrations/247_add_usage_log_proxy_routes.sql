-- Keep historical route attribution unknown. No account/proxy relationship is
-- consulted: proxy names and addresses can change after a request completes.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS proxy_routes JSONB;

COMMENT ON COLUMN usage_logs.proxy_routes IS
    'Ordered logical route snapshots for actual outbound attempts; NULL means attribution was not recorded. Contains no proxy authentication credentials.';
