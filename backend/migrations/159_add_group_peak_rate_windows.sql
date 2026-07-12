ALTER TABLE groups ADD COLUMN IF NOT EXISTS peak_rate_windows JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE groups ALTER COLUMN peak_rate_windows SET DEFAULT '[]'::jsonb;

UPDATE groups
SET peak_rate_windows = '[]'::jsonb
WHERE peak_rate_windows IS NULL;

ALTER TABLE groups ALTER COLUMN peak_rate_windows SET NOT NULL;

UPDATE groups
SET peak_rate_windows = jsonb_build_array(
    jsonb_build_object(
        'start', lpad(split_part(peak_start, ':', 1), 2, '0') || ':' || split_part(peak_start, ':', 2),
        'end', lpad(split_part(peak_end, ':', 1), 2, '0') || ':' || split_part(peak_end, ':', 2),
        'multiplier', peak_rate_multiplier::float8
    )
)
WHERE peak_rate_enabled = TRUE
  AND COALESCE(peak_start, '') ~ '^([01]?[0-9]|2[0-3]):[0-5][0-9]$'
  AND COALESCE(peak_end, '') ~ '^([01]?[0-9]|2[0-3]):[0-5][0-9]$'
  AND (split_part(peak_start, ':', 1)::int * 60 + split_part(peak_start, ':', 2)::int)
      < (split_part(peak_end, ':', 1)::int * 60 + split_part(peak_end, ':', 2)::int)
  AND peak_rate_multiplier >= 0
  AND peak_rate_windows = '[]'::jsonb;
