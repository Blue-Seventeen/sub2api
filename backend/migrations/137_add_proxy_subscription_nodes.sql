DROP INDEX IF EXISTS idx_proxies_subscription_id_active_unique;

CREATE TABLE IF NOT EXISTS proxy_subscription_nodes (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES proxy_subscriptions(id) ON DELETE CASCADE,
    proxy_id BIGINT REFERENCES proxies(id) ON DELETE SET NULL,
    node_key VARCHAR(64) NOT NULL,
    name TEXT NOT NULL,
    provider_name VARCHAR(120) NOT NULL,
    type VARCHAR(40) NOT NULL,
    server VARCHAR(255),
    port INT,
    username VARCHAR(100) NOT NULL,
    password VARCHAR(100) NOT NULL,
    raw_config TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_proxy_subscription_nodes_subscription_key
    ON proxy_subscription_nodes (subscription_id, node_key);

CREATE UNIQUE INDEX IF NOT EXISTS idx_proxy_subscription_nodes_proxy_id
    ON proxy_subscription_nodes (proxy_id)
    WHERE proxy_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_proxy_subscription_nodes_subscription_status
    ON proxy_subscription_nodes (subscription_id, status);
