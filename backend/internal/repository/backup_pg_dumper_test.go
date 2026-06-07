package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPgDumperRestoreArgsStopOnSQLFailure(t *testing.T) {
	dumper := &PgDumper{cfg: &config.DatabaseConfig{
		Host:   "127.0.0.1",
		Port:   5432,
		User:   "sub2api",
		DBName: "sub2api_test",
	}}

	require.Contains(t, dumper.restoreArgs(), "--single-transaction")
	require.Contains(t, dumper.restoreArgs(), "ON_ERROR_STOP=1")
}
