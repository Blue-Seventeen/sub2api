-- Channel Monitor V2 is designed around fixed UI buckets; refresh passive
-- aggregation every 5 minutes and recompute the trailing bucket for each range.
-- Migration 197 advances the untouched factory row from version 1 to 2.
-- Operator saves carry updated_by; preserve their chosen interval.
UPDATE channel_monitor_v2_config
SET refresh_interval_seconds = 300,
    updated_at = NOW()
WHERE id = 1
  AND version IN (1, 2)
  AND updated_by IS NULL
  AND enabled = TRUE
  AND cardinality(group_ids) = 0
  AND refresh_interval_seconds = 60;
