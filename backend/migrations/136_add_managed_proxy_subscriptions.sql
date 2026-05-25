CREATE TABLE IF NOT EXISTS proxy_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    subscription_url TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    refresh_interval_sec INT NOT NULL DEFAULT 3600 CHECK (refresh_interval_sec > 0),
    test_url TEXT NOT NULL DEFAULT 'https://www.gstatic.com/generate_204',
    revision BIGINT NOT NULL DEFAULT 1,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS source_type VARCHAR(40) NOT NULL DEFAULT 'manual';

ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS subscription_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_proxies_subscription_id'
    ) THEN
        ALTER TABLE proxies
            ADD CONSTRAINT fk_proxies_subscription_id
            FOREIGN KEY (subscription_id)
            REFERENCES proxy_subscriptions(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_proxy_subscriptions_status
    ON proxy_subscriptions (status);

CREATE INDEX IF NOT EXISTS idx_proxies_source_type
    ON proxies (source_type);

CREATE INDEX IF NOT EXISTS idx_proxies_subscription_id
    ON proxies (subscription_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_proxies_subscription_id_active_unique
    ON proxies (subscription_id)
    WHERE subscription_id IS NOT NULL AND deleted_at IS NULL;
