package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupModelAllowlistRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("236_group_model_allowlist_repair.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	// Missing columns are repaired independently; display choices are not access policy.
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.NotContains(t, sql, "RENAME COLUMN")
	require.NotContains(t, sql, "SET model_allowlist = models_list_config")
	require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN models_list_config SET NOT NULL")
	require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN model_allowlist SET NOT NULL")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.model_allowlist")

	// Unqualified DDL follows search_path without assuming the public schema.
	require.NotContains(t, sql, "table_schema = 'public'")
}
