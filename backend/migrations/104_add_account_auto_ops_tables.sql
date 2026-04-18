-- 104_add_account_auto_ops_tables.sql
-- Account auto ops runs and steps

CREATE TABLE IF NOT EXISTS account_auto_ops_runs (
    id                   BIGSERIAL PRIMARY KEY,
    trigger_mode         VARCHAR(20) NOT NULL,
    status               VARCHAR(20) NOT NULL DEFAULT 'running',
    requested_account_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    total_accounts       INT NOT NULL DEFAULT 0,
    eligible_accounts    INT NOT NULL DEFAULT 0,
    completed_accounts   INT NOT NULL DEFAULT 0,
    error_message        TEXT NOT NULL DEFAULT '',
    started_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_auto_ops_runs_trigger_started
    ON account_auto_ops_runs(trigger_mode, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_account_auto_ops_runs_started_at
    ON account_auto_ops_runs(started_at DESC);

CREATE TABLE IF NOT EXISTS account_auto_ops_steps (
    id                 BIGSERIAL PRIMARY KEY,
    run_id             BIGINT NOT NULL REFERENCES account_auto_ops_runs(id) ON DELETE CASCADE,
    account_id         BIGINT NOT NULL,
    account_name       TEXT NOT NULL DEFAULT '',
    step_index         INT NOT NULL DEFAULT 0,
    subject            VARCHAR(50) NOT NULL DEFAULT '',
    action             VARCHAR(50) NOT NULL DEFAULT '',
    status             VARCHAR(50) NOT NULL DEFAULT '',
    matched_rule_id    VARCHAR(100) NOT NULL DEFAULT '',
    matched_rule_name  TEXT NOT NULL DEFAULT '',
    response_text      TEXT NOT NULL DEFAULT '',
    response_hash      VARCHAR(64) NOT NULL DEFAULT '',
    action_result_text TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_auto_ops_steps_run_id
    ON account_auto_ops_steps(run_id, account_id, step_index, id);

CREATE INDEX IF NOT EXISTS idx_account_auto_ops_steps_created_subject
    ON account_auto_ops_steps(created_at DESC, subject);

CREATE INDEX IF NOT EXISTS idx_account_auto_ops_steps_response_hash
    ON account_auto_ops_steps(subject, response_hash, created_at DESC)
    WHERE response_hash <> '';
