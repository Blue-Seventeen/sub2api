CREATE TABLE IF NOT EXISTS proxy_request_stats (
    id BIGSERIAL PRIMARY KEY,
    proxy_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    api_key_id BIGINT,
    request_id VARCHAR(128),
    success BOOLEAN NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_proxy_request_stats_proxy_created
    ON proxy_request_stats (proxy_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_proxy_request_stats_proxy_success
    ON proxy_request_stats (proxy_id, success);

CREATE INDEX IF NOT EXISTS idx_proxy_request_stats_proxy_cover
    ON proxy_request_stats (proxy_id) INCLUDE (success, duration_ms);
