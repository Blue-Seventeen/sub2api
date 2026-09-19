-- Display-only models_list_config remains independent and usable by old images.
-- Request admission is opt-in; never enable it from an existing display list.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN groups.model_allowlist IS
    'Optional group model admission allowlist, independent of models_list_config';
