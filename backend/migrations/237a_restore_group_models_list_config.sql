-- Restore the canonical models_list_config column after the v0.2.4 rename-only
-- path. The branch is intentionally column-presence based: when the canonical
-- column exists, including an existing custom value, it is never overwritten.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'groups'
          AND column_name = 'models_list_config'
    ) THEN
        ALTER TABLE groups
            ADD COLUMN models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb;

        IF EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'public'
              AND table_name = 'groups'
              AND column_name = 'model_allowlist'
        ) THEN
            UPDATE groups
               SET models_list_config = COALESCE(model_allowlist, '{}'::jsonb);
        END IF;
    END IF;
END
$$;

COMMENT ON COLUMN groups.models_list_config IS
    'Canonical group model display and request admission policy';
