-- Add one optional custom rolling quota window for subscription groups.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS custom_limit_hours INTEGER NOT NULL DEFAULT 0;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS custom_limit_usd DECIMAL(20,8);

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS custom_window_start TIMESTAMPTZ;

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS custom_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0;

-- Existing active subscriptions should have a deterministic custom window
-- anchor when admins enable the new custom quota later.
--
-- Do not overwrite custom_usage_usd here. New installs get the column default
-- of 0, while partially upgraded databases keep any value they already have.
UPDATE user_subscriptions
SET
    custom_window_start = CASE
        WHEN user_subscriptions.starts_at IS NULL OR NOW() <= user_subscriptions.starts_at THEN user_subscriptions.starts_at
        WHEN g.custom_limit_hours IS NULL OR g.custom_limit_hours <= 0 THEN user_subscriptions.starts_at
        ELSE user_subscriptions.starts_at + (
            FLOOR(EXTRACT(EPOCH FROM (NOW() - user_subscriptions.starts_at)) / (LEAST(g.custom_limit_hours, 87600) * 3600))::BIGINT
            * LEAST(g.custom_limit_hours, 87600)
            * 3600
            * INTERVAL '1 second'
        )
    END,
    updated_at = NOW()
FROM groups g
WHERE user_subscriptions.deleted_at IS NULL
  AND user_subscriptions.status = 'active'
  AND user_subscriptions.expires_at > NOW()
  AND user_subscriptions.custom_window_start IS NULL
  AND user_subscriptions.group_id = g.id;
