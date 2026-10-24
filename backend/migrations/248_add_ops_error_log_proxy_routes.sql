-- Preserve unknown historical attribution: do not backfill from account bindings.
ALTER TABLE ops_error_logs ADD COLUMN IF NOT EXISTS proxy_routes JSONB;

COMMENT ON COLUMN ops_error_logs.proxy_routes IS
    'Ordered request-time logical route snapshots for actual outbound attempts; NULL means attribution was not recorded. Contains no proxy authentication credentials.';
