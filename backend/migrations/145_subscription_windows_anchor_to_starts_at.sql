-- Align active subscription quota windows to the subscription start timestamp.
-- Existing active subscriptions are moved from natural-day windows to redemption-time
-- rolling windows. Usage is reset so the first post-upgrade window starts cleanly
-- under the new semantics, including legacy rows with NULL or partially activated windows.
UPDATE user_subscriptions
SET
    daily_usage_usd = 0,
    weekly_usage_usd = 0,
    monthly_usage_usd = 0,
    daily_window_start = starts_at,
    weekly_window_start = starts_at,
    monthly_window_start = starts_at,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND status = 'active'
  AND expires_at > NOW();
