-- Merge the former standalone xAI configurable platform into Grok.
-- Grok remains backed by xAI upstream APIs internally, but administrators now
-- configure OAuth and API Key + Base URL accounts under the single "grok" platform.

UPDATE accounts
SET platform = 'grok'
WHERE platform = 'xai';

UPDATE groups
SET platform = 'grok'
WHERE platform = 'xai';

DO $$
BEGIN
  IF to_regclass('public.channel_model_pricing') IS NOT NULL THEN
    UPDATE channel_model_pricing
    SET platform = 'grok'
    WHERE platform = 'xai';
  END IF;

  IF to_regclass('public.ops_alert_silences') IS NOT NULL THEN
    UPDATE ops_alert_silences
    SET platform = 'grok'
    WHERE platform = 'xai';
  END IF;

  IF to_regclass('public.ops_system_logs') IS NOT NULL THEN
    UPDATE ops_system_logs
    SET platform = 'grok'
    WHERE platform = 'xai';
  END IF;

  IF to_regclass('public.error_passthrough_rules') IS NOT NULL THEN
    UPDATE error_passthrough_rules
    SET platforms = (
      SELECT COALESCE(jsonb_agg(DISTINCT CASE WHEN value = 'xai' THEN 'grok' ELSE value END), '[]'::jsonb)
      FROM jsonb_array_elements_text(COALESCE(platforms, '[]'::jsonb)) AS t(value)
    )
    WHERE COALESCE(platforms, '[]'::jsonb) ? 'xai';
  END IF;
END $$;
