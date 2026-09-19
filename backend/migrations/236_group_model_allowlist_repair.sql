-- Repair missing columns without renaming or copying between distinct policies.
-- Existing non-null display and admission choices are always preserved.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE groups SET models_list_config = '{}'::jsonb WHERE models_list_config IS NULL;
UPDATE groups SET model_allowlist = '{}'::jsonb WHERE model_allowlist IS NULL;

ALTER TABLE groups ALTER COLUMN models_list_config SET DEFAULT '{}'::jsonb;
ALTER TABLE groups ALTER COLUMN models_list_config SET NOT NULL;
ALTER TABLE groups ALTER COLUMN model_allowlist SET DEFAULT '{}'::jsonb;
ALTER TABLE groups ALTER COLUMN model_allowlist SET NOT NULL;

COMMENT ON COLUMN groups.models_list_config IS
    'Optional custom model display list; does not enable request admission restrictions';
COMMENT ON COLUMN groups.model_allowlist IS
    'Optional group model admission allowlist, independent of models_list_config';
