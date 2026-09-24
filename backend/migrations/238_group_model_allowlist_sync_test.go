package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupModelAllowlistSyncMigrationCopiesCanonicalDisplayPolicy(t *testing.T) {
	content, err := FS.ReadFile("238_group_model_allowlist_sync.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "UPDATE groups SET model_allowlist = models_list_config")
	require.Contains(t, sql, "WHERE model_allowlist IS DISTINCT FROM models_list_config")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.model_allowlist")
}
