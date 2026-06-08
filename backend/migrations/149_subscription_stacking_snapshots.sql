-- Allow multiple active subscription cards for the same user/group and keep
-- historical snapshots for evidence after group edits or deletion.

DROP INDEX IF EXISTS user_subscriptions_user_group_unique_active;
ALTER TABLE user_subscriptions DROP CONSTRAINT IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS usersubscription_user_id_group_id;

ALTER TABLE user_subscriptions
	ADD COLUMN IF NOT EXISTS source_type VARCHAR(32),
	ADD COLUMN IF NOT EXISTS source_ref_id VARCHAR(128),
	ADD COLUMN IF NOT EXISTS source_redeem_code_id BIGINT REFERENCES redeem_codes(id) ON DELETE SET NULL,
	ADD COLUMN IF NOT EXISTS redeem_code_snapshot TEXT,
	ADD COLUMN IF NOT EXISTS group_name_snapshot TEXT,
	ADD COLUMN IF NOT EXISTS group_platform_snapshot VARCHAR(50),
	ADD COLUMN IF NOT EXISTS group_rate_multiplier_snapshot DECIMAL(10,4),
	ADD COLUMN IF NOT EXISTS daily_limit_usd_snapshot DECIMAL(20,10),
	ADD COLUMN IF NOT EXISTS weekly_limit_usd_snapshot DECIMAL(20,10),
	ADD COLUMN IF NOT EXISTS monthly_limit_usd_snapshot DECIMAL(20,10),
	ADD COLUMN IF NOT EXISTS custom_limit_hours_snapshot INTEGER,
	ADD COLUMN IF NOT EXISTS custom_limit_usd_snapshot DECIMAL(20,10);

UPDATE user_subscriptions us
SET
	group_name_snapshot = COALESCE(us.group_name_snapshot, g.name),
	group_platform_snapshot = COALESCE(us.group_platform_snapshot, g.platform),
	group_rate_multiplier_snapshot = COALESCE(us.group_rate_multiplier_snapshot, g.rate_multiplier),
	daily_limit_usd_snapshot = COALESCE(us.daily_limit_usd_snapshot, g.daily_limit_usd),
	weekly_limit_usd_snapshot = COALESCE(us.weekly_limit_usd_snapshot, g.weekly_limit_usd),
	monthly_limit_usd_snapshot = COALESCE(us.monthly_limit_usd_snapshot, g.monthly_limit_usd),
	custom_limit_hours_snapshot = COALESCE(us.custom_limit_hours_snapshot, g.custom_limit_hours),
	custom_limit_usd_snapshot = COALESCE(us.custom_limit_usd_snapshot, g.custom_limit_usd)
FROM groups g
WHERE us.group_id = g.id;

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_user_group_status_expiry_deleted
	ON user_subscriptions(user_id, group_id, status, expires_at, deleted_at);

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_source
	ON user_subscriptions(source_type, source_ref_id)
	WHERE source_type IS NOT NULL AND source_ref_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_redeem_code
	ON user_subscriptions(source_redeem_code_id)
	WHERE source_redeem_code_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_payment_source_unique_active
	ON user_subscriptions(source_type, source_ref_id)
	WHERE source_type = 'payment_order' AND source_ref_id IS NOT NULL AND deleted_at IS NULL;
