ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS request_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS task_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS usage_estimated BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS billable_unit_type VARCHAR(32);

CREATE TABLE IF NOT EXISTS newapi_tasks (
    id BIGSERIAL PRIMARY KEY,
    public_task_id VARCHAR(128) NOT NULL UNIQUE,
    upstream_task_id VARCHAR(256),
    platform VARCHAR(64) NOT NULL,
    account_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT,
    model VARCHAR(255),
    route VARCHAR(128) NOT NULL,
    status VARCHAR(64) NOT NULL DEFAULT 'submitted',
    progress INTEGER NOT NULL DEFAULT 0,
    request_body JSONB,
    raw_response JSONB,
    result JSONB,
    precharged_cost NUMERIC(20, 10) NOT NULL DEFAULT 0,
    final_cost NUMERIC(20, 10) NOT NULL DEFAULT 0,
    usage_log_id BIGINT,
    next_poll_at TIMESTAMPTZ,
    last_polled_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_newapi_tasks_status_next_poll
    ON newapi_tasks (status, next_poll_at);

CREATE INDEX IF NOT EXISTS idx_newapi_tasks_user_created_at
    ON newapi_tasks (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_newapi_tasks_account_created_at
    ON newapi_tasks (account_id, created_at DESC);
