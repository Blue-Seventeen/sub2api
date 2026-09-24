-- models_list_config is the canonical custom-fork model policy. Keep the
-- upstream v0.2.4 admission column synchronized as a compatibility mirror.
UPDATE groups
SET model_allowlist = models_list_config
WHERE model_allowlist IS DISTINCT FROM models_list_config;

COMMENT ON COLUMN groups.models_list_config IS
    'Canonical group model display and request admission policy';
COMMENT ON COLUMN groups.model_allowlist IS
    'Compatibility mirror of models_list_config; not independently editable';
