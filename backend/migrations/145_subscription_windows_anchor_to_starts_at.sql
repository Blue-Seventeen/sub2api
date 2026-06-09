-- Backfill missing active subscription quota windows without changing existing
-- usage or already-activated reset anchors.
--
-- Important upgrade invariant:
--   v0.1.133 -> v0.1.134 must not grant free quota by clearing active
--   subscription usage, and must not move reset times for rows that already
--   have window_start values.
--
-- For partially activated legacy rows, only NULL windows are initialized to
-- the rolling period containing the migration time. This avoids an immediate
-- post-upgrade reset on the next request while preserving existing usage.
UPDATE user_subscriptions
SET
    daily_window_start = COALESCE(
        daily_window_start,
        CASE
            WHEN starts_at IS NULL OR NOW() <= starts_at THEN starts_at
            WHEN expires_at <= starts_at + INTERVAL '1 day' THEN starts_at
            ELSE starts_at + (FLOOR(EXTRACT(EPOCH FROM (NOW() - starts_at)) / 86400)::BIGINT * INTERVAL '1 second' * 86400)
        END
    ),
    weekly_window_start = COALESCE(
        weekly_window_start,
        CASE
            WHEN starts_at IS NULL OR NOW() <= starts_at THEN starts_at
            ELSE starts_at + (FLOOR(EXTRACT(EPOCH FROM (NOW() - starts_at)) / 604800)::BIGINT * INTERVAL '1 second' * 604800)
        END
    ),
    monthly_window_start = COALESCE(
        monthly_window_start,
        CASE
            WHEN starts_at IS NULL OR NOW() <= starts_at THEN starts_at
            ELSE starts_at + (FLOOR(EXTRACT(EPOCH FROM (NOW() - starts_at)) / 2592000)::BIGINT * INTERVAL '1 second' * 2592000)
        END
    ),
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND status = 'active'
  AND expires_at > NOW()
  AND (
    daily_window_start IS NULL
    OR weekly_window_start IS NULL
    OR monthly_window_start IS NULL
  );
