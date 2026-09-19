package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestNewUsageIndexMigrationsRecoverInvalidIndexes(t *testing.T) {
	for name, indexes := range map[string][]string{
		"195_add_usage_log_upstream_model_mismatch_index_notx.sql": {"idx_usage_logs_upstream_model_mismatch_created_at"},
		"226_add_usage_log_effective_model_indexes_notx.sql":       {"idx_usage_logs_effective_requested_model_created", "idx_usage_logs_effective_upstream_model_created"},
		"233_add_usage_log_upstream_request_id_index_notx.sql":     {"idx_usage_logs_upstream_request_id"},
	} {
		for _, state := range []string{"invalid", "valid", "mixed", "drop_failure"} {
			t.Run(name+"/"+state, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer db.Close()
				content, err := migrations.FS.ReadFile(name)
				require.NoError(t, err)
				prepareMigrationsBootstrapExpectations(mock)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT checksum FROM schema_migrations WHERE filename = $1")).
					WithArgs(name).WillReturnError(sql.ErrNoRows)
				for i, index := range indexes {
					invalid := state == "invalid" || state == "drop_failure" || (state == "mixed" && i == len(indexes)-1)
					mock.ExpectQuery(`SELECT EXISTS \(`).WithArgs(index).
						WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(invalid))
					if invalid {
						drop := mock.ExpectExec(regexp.QuoteMeta("DROP INDEX CONCURRENTLY IF EXISTS " + index))
						if state == "drop_failure" {
							drop.WillReturnError(errors.New("index cleanup unavailable"))
							break
						}
						drop.WillReturnResult(sqlmock.NewResult(0, 0))
					}
				}
				if state != "drop_failure" {
					for _, statement := range splitSQLStatements(strings.TrimSpace(string(content))) {
						if strings.TrimSpace(stripSQLLineComment(statement)) != "" {
							mock.ExpectExec(regexp.QuoteMeta(strings.TrimSpace(statement))).WillReturnResult(sqlmock.NewResult(0, 0))
						}
					}
					mock.ExpectExec(regexp.QuoteMeta("INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)")).
						WithArgs(name, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
				}
				mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
					WithArgs(migrationsAdvisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
				err = applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: content}})
				if state == "drop_failure" {
					require.ErrorContains(t, err, "index cleanup unavailable")
				} else {
					require.NoError(t, err)
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
