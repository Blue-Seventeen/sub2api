-- Seed non-ops error exclusions only on an untouched factory configuration.
-- Keep migration 198's tolerant cache thresholds; zero is also an operator choice.
UPDATE channel_monitor_v2_config
SET ignored_error_categories = ARRAY[
    'authentication',
    'client_cancelled',
    'content_policy',
    'context_limit',
    'group_access',
    'model_unsupported',
    'not_found',
    'quota_or_balance'
]::text[]
WHERE id = 1
  AND version IN (1, 2)
  AND updated_by IS NULL
  AND enabled = TRUE
  AND cardinality(group_ids) = 0
  AND COALESCE(cardinality(ignored_error_categories), 0) = 0;
