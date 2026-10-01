package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorFullURLMigrationIsIdempotent(t *testing.T) {
	sql, err := FS.ReadFile("239_channel_monitor_full_url.sql")
	require.NoError(t, err)

	statement := strings.TrimSpace(string(sql))
	require.Contains(t, statement, "ALTER TABLE channel_monitors")
	require.Contains(t, statement, "ADD COLUMN IF NOT EXISTS endpoint_is_full_url")
	require.Contains(t, statement, "BOOLEAN NOT NULL DEFAULT FALSE")
	// ADD COLUMN IF NOT EXISTS makes a retry a no-op for an already upgraded DB.
	require.Equal(t, 1, strings.Count(statement, "ADD COLUMN IF NOT EXISTS endpoint_is_full_url"))
}
