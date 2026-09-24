package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/model_policy_v024/*.sql
var modelPolicyV024Migrations embed.FS

func TestModelPolicyMigrationChecksumCompatibilityIsExact(t *testing.T) {
	// Historical hashes were audited against git v0.2.4; current hashes use
	// the same TrimSpace-before-SHA256 rule as the migration runner.
	for _, tc := range []struct{ name, old, current string }{
		{"235_group_model_allowlist.sql", "546fd53d114f9a8c402b019af71bf4685dc1968fd5cd25d054d095a58d806fbc", "ae77820b57e67383ac10a5f65dce0b350d24be7433238cdc72965b3e01bffc26"},
		{"236_group_model_allowlist_repair.sql", "0d8fbcd98750be1a58cec45fe2b866031b656ac6286b073ca061f8fa8fdc040e", "0c3199adbee50ed838f522d2a972255cf059c646e2141263b863480ab0506e6d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, err := dbmigrations.FS.ReadFile(tc.name)
			require.NoError(t, err)
			checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(content)))))
			require.Equal(t, tc.current, checksum, "historical files must stay immutable")
			historical, err := modelPolicyV024Migrations.ReadFile("testdata/model_policy_v024/" + tc.name)
			require.NoError(t, err)
			require.Equal(t, tc.old, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(historical))))), "fixtures must be exact v0.2.4 SQL")
			require.True(t, isMigrationChecksumCompatible(tc.name, tc.old, checksum))
			for _, unknown := range []string{"", strings.Repeat("0", 64), tc.old[:63] + "f"} {
				require.False(t, isMigrationChecksumCompatible(tc.name, unknown, checksum))
				require.False(t, isMigrationChecksumCompatible(tc.name, tc.old, unknown))
			}
			require.False(t, isMigrationChecksumCompatible("238_group_model_allowlist_sync.sql", tc.old, checksum))
			require.False(t, isMigrationChecksumCompatible(tc.name, tc.old, fmt.Sprintf("%x", sha256.Sum256(append(content, 'x')))))
		})
	}
}

func TestModelPolicyMigrationRunnerOrdersRestoreBeforeSynchronization(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	prepareMigrationsBootstrapExpectations(mock)
	files := fstest.MapFS{}
	for _, name := range []string{"235_group_model_allowlist.sql", "236_group_model_allowlist_repair.sql", "237_add_minimax_platform.sql", "237a_restore_group_models_list_config.sql", "238_group_model_allowlist_sync.sql", "239_group_model_policy_auth_cache_invalidation.sql"} {
		content, readErr := dbmigrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		files[name] = &fstest.MapFile{Data: content}
		checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(content)))))
		lookup := mock.ExpectQuery(regexp.QuoteMeta("SELECT checksum FROM schema_migrations WHERE filename = $1")).WithArgs(name)
		switch name {
		case "235_group_model_allowlist.sql":
			lookup.WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow("546fd53d114f9a8c402b019af71bf4685dc1968fd5cd25d054d095a58d806fbc"))
		case "236_group_model_allowlist_repair.sql":
			lookup.WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow("0d8fbcd98750be1a58cec45fe2b866031b656ac6286b073ca061f8fa8fdc040e"))
		case "237_add_minimax_platform.sql":
			lookup.WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(checksum))
		default:
			lookup.WillReturnError(sql.ErrNoRows)
			mock.ExpectBegin()
			mock.ExpectExec(regexp.QuoteMeta(strings.TrimSpace(string(content)))).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta("INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)")).WithArgs(name, checksum).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
	}
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).WithArgs(migrationsAdvisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, applyMigrationsFS(context.Background(), db, files))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestModelPolicyMigrationRunnerAcceptsOnlyAuditedHistory(t *testing.T) {
	for _, tc := range []struct{ name, old string }{
		{"235_group_model_allowlist.sql", "546fd53d114f9a8c402b019af71bf4685dc1968fd5cd25d054d095a58d806fbc"},
		{"236_group_model_allowlist_repair.sql", "0d8fbcd98750be1a58cec45fe2b866031b656ac6286b073ca061f8fa8fdc040e"},
	} {
		for _, state := range []string{"audited", "unknown database checksum", "edited file"} {
			t.Run(tc.name+"/"+state, func(t *testing.T) {
				content, err := dbmigrations.FS.ReadFile(tc.name)
				require.NoError(t, err)
				dbChecksum := tc.old
				if state == "unknown database checksum" {
					dbChecksum = strings.Repeat("0", 64)
				}
				if state == "edited file" {
					content = append(content, []byte("\n-- unaudited modification")...)
				}
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer db.Close()
				prepareMigrationsBootstrapExpectations(mock)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT checksum FROM schema_migrations WHERE filename = $1")).
					WithArgs(tc.name).WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(dbChecksum))
				// No migration SQL or migration-record UPDATE/INSERT is permitted.
				mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
					WithArgs(migrationsAdvisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
				err = applyMigrationsFS(context.Background(), db, fstest.MapFS{tc.name: &fstest.MapFile{Data: content}})
				if state == "audited" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "checksum mismatch")
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
