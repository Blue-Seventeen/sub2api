-- Add one optional custom rolling quota window for subscription groups.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS custom_limit_hours INTEGER NOT NULL DEFAULT 0;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS custom_limit_usd DECIMAL(20,8);

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS custom_window_start TIMESTAMPTZ;

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS custom_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0;

-- Existing active subscriptions should have a deterministic custom window anchor
-- when admins enable the new custom quota later. The usage starts clean because
-- there was no historical custom-window accounting before this migration.
UPDATE user_subscriptions
SET
    custom_window_start = starts_at,
    custom_usage_usd = 0,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND status = 'active'
  AND expires_at > NOW()
  AND custom_window_start IS NULL;
