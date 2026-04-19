-- Promotion center / commission settlement module
-- Forward-only migration. Do not modify after applied.

CREATE TABLE IF NOT EXISTS promotion_users (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    invite_code VARCHAR(32) NOT NULL UNIQUE,
    parent_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    binding_source VARCHAR(20) NOT NULL DEFAULT 'self',
    bound_at TIMESTAMPTZ NULL,
    bound_note TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_promotion_users_parent_not_self
        CHECK (parent_user_id IS NULL OR parent_user_id <> user_id),
    CONSTRAINT chk_promotion_users_binding_source
        CHECK (binding_source IN ('self', 'admin'))
);

CREATE INDEX IF NOT EXISTS idx_promotion_users_parent_user_id
    ON promotion_users(parent_user_id);

CREATE TABLE IF NOT EXISTS promotion_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1,
    activation_threshold_amount DECIMAL(20,8) NOT NULL DEFAULT 5.00000000,
    activation_bonus_amount DECIMAL(20,8) NOT NULL DEFAULT 0.00000000,
    daily_settlement_time TIME NOT NULL DEFAULT TIME '00:00:00',
    settlement_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_promotion_settings_singleton CHECK (id = 1),
    CONSTRAINT chk_promotion_settings_activation_threshold CHECK (activation_threshold_amount >= 0),
    CONSTRAINT chk_promotion_settings_activation_bonus CHECK (activation_bonus_amount >= 0)
);

INSERT INTO promotion_settings (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS promotion_level_configs (
    id BIGSERIAL PRIMARY KEY,
    level_no INT NOT NULL UNIQUE,
    level_name VARCHAR(50) NOT NULL,
    required_activated_invites INT NOT NULL DEFAULT 0,
    direct_rate DECIMAL(8,4) NOT NULL DEFAULT 0.0000,
    indirect_rate DECIMAL(8,4) NOT NULL DEFAULT 0.0000,
    sort_order INT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_promotion_level_required_nonnegative CHECK (required_activated_invites >= 0),
    CONSTRAINT chk_promotion_level_rate_nonnegative CHECK (direct_rate >= 0 AND indirect_rate >= 0)
);

CREATE INDEX IF NOT EXISTS idx_promotion_level_configs_sort_order
    ON promotion_level_configs(sort_order);

CREATE TABLE IF NOT EXISTS promotion_commission_records (
    id BIGSERIAL PRIMARY KEY,
    beneficiary_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    business_date DATE NOT NULL,
    commission_type VARCHAR(20) NOT NULL,
    relation_depth SMALLINT NOT NULL DEFAULT 0,
    level_id BIGINT NULL REFERENCES promotion_level_configs(id) ON DELETE SET NULL,
    level_snapshot VARCHAR(50) NULL,
    rate_snapshot DECIMAL(8,4) NULL,
    base_amount DECIMAL(20,8) NOT NULL DEFAULT 0.00000000,
    amount DECIMAL(20,8) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    settlement_batch_id BIGINT NULL,
    note TEXT NULL,
    created_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    settled_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_promotion_commission_type
        CHECK (commission_type IN ('commission', 'activation', 'manual', 'adjustment', 'promotion')),
    CONSTRAINT chk_promotion_commission_status
        CHECK (status IN ('pending', 'settled', 'cancelled')),
    CONSTRAINT chk_promotion_commission_depth
        CHECK (relation_depth BETWEEN 0 AND 2)
);

CREATE INDEX IF NOT EXISTS idx_promotion_commission_beneficiary_status_date
    ON promotion_commission_records(beneficiary_user_id, status, business_date DESC);

CREATE INDEX IF NOT EXISTS idx_promotion_commission_status_date
    ON promotion_commission_records(status, business_date DESC);

CREATE INDEX IF NOT EXISTS idx_promotion_commission_source_date
    ON promotion_commission_records(source_user_id, business_date DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_promotion_commission_daily
    ON promotion_commission_records(business_date, beneficiary_user_id, source_user_id, relation_depth)
    WHERE commission_type = 'commission' AND source_user_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_promotion_commission_activation
    ON promotion_commission_records(beneficiary_user_id, source_user_id)
    WHERE commission_type = 'activation' AND source_user_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS promotion_activations (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    promoter_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    activated_at TIMESTAMPTZ NOT NULL,
    threshold_amount DECIMAL(20,8) NOT NULL,
    trigger_usage_amount DECIMAL(20,8) NOT NULL,
    commission_record_id BIGINT NULL REFERENCES promotion_commission_records(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_promotion_activations_amounts
        CHECK (threshold_amount >= 0 AND trigger_usage_amount >= 0)
);

CREATE INDEX IF NOT EXISTS idx_promotion_activations_promoter_user_id
    ON promotion_activations(promoter_user_id);

CREATE TABLE IF NOT EXISTS promotion_settlement_batches (
    id BIGSERIAL PRIMARY KEY,
    business_date DATE NOT NULL UNIQUE,
    status VARCHAR(20) NOT NULL DEFAULT 'running',
    total_records INT NOT NULL DEFAULT 0,
    total_amount DECIMAL(20,8) NOT NULL DEFAULT 0.00000000,
    executed_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    executed_at TIMESTAMPTZ NULL,
    note TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_promotion_settlement_status
        CHECK (status IN ('running', 'settled', 'failed', 'cancelled'))
);

ALTER TABLE promotion_commission_records
    ADD CONSTRAINT fk_promotion_commission_records_settlement_batch
    FOREIGN KEY (settlement_batch_id)
    REFERENCES promotion_settlement_batches(id)
    ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS promotion_scripts (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    category VARCHAR(32) NOT NULL DEFAULT 'default',
    content TEXT NOT NULL,
    use_count BIGINT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_promotion_scripts_category
        CHECK (category IN ('default', 'wechat', 'tech', 'social', 'email'))
);

CREATE INDEX IF NOT EXISTS idx_promotion_scripts_enabled_created_at
    ON promotion_scripts(enabled DESC, created_at DESC);
