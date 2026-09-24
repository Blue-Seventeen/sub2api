package migrations

import (
	"io/fs"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreGroupModelsListConfigMigrationSortsBetween237And238(t *testing.T) {
	files, err := fs.Glob(FS, "*.sql")
	require.NoError(t, err)
	sort.Strings(files)

	index := func(name string) int {
		for i, file := range files {
			if file == name {
				return i
			}
		}
		return -1
	}
	require.NotEqual(t, -1, index("237_add_minimax_platform.sql"))
	require.NotEqual(t, -1, index("237a_restore_group_models_list_config.sql"))
	require.NotEqual(t, -1, index("238_group_model_allowlist_sync.sql"))
	require.Less(t, index("237_add_minimax_platform.sql"), index("237a_restore_group_models_list_config.sql"))
	require.Less(t, index("237a_restore_group_models_list_config.sql"), index("238_group_model_allowlist_sync.sql"))
}
