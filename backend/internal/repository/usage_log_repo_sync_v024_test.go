package repository

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogSyncV024SelectAndScan(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			columns := strings.Split(usageLogSelectColumns, ", ")
			require.Contains(t, columns, "native_compaction_v2")
			require.Contains(t, columns, "requested_reasoning_effort")
			requested, forwarded := "max", "xhigh"
			values := buildUsageLogScanValues(usageLogScanRowOptions{
				ID: 7, Model: "gpt-5", RequestedModel: "gpt-5", LegacyStream: true,
				NativeCompactionV2: enabled, ReasoningEffort: &forwarded,
				RequestedReasoningEffort: &requested, CreatedAt: time.Now().UTC(),
			})
			require.Len(t, values, len(columns))
			db, mock := newSQLMock(t)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT " + usageLogSelectColumns + " FROM usage_logs WHERE id = $1")).
				WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows(columns).AddRow(anySliceToDriverValues(values)...))
			repo := &usageLogRepository{sql: db}
			log, err := repo.GetByID(context.Background(), 7)
			require.NoError(t, err)
			require.Equal(t, enabled, log.NativeCompactionV2)
			require.True(t, log.Stream)
			require.Equal(t, &requested, log.RequestedReasoningEffort)
			require.Equal(t, &forwarded, log.ReasoningEffort)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageLogSyncV024InsertColumns(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			requested := "max"
			upstreamRequestID := "req-upstream-v024"
			prepared := prepareUsageLogInsert(&service.UsageLog{RequestID: "sync-v024", NativeCompactionV2: enabled, RequestedReasoningEffort: &requested, UpstreamRequestID: &upstreamRequestID})
			key := usageLogBatchKey(prepared.requestID, 0)
			batchQuery, _ := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
			bestEffortQuery, _ := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
			for _, query := range []string{batchQuery, bestEffortQuery} {
				match := regexp.MustCompile(`(?s)INSERT INTO usage_logs\s*\((.*?)\)`).FindStringSubmatch(query)
				require.Len(t, match, 2)
				columns := strings.Split(match[1], ",")
				require.Len(t, columns, len(prepared.args))
				indices := make(map[string]int, len(columns))
				for i, column := range columns {
					indices[strings.TrimSpace(column)] = i
				}
				require.Contains(t, indices, "native_compaction_v2")
				require.Contains(t, indices, "requested_reasoning_effort")
				require.Contains(t, indices, "upstream_request_id")
				require.Equal(t, sql.NullString{String: upstreamRequestID, Valid: true}, prepared.args[indices["upstream_request_id"]])
				require.Equal(t, "text", usageLogInsertArgTypes[indices["upstream_request_id"]])
				require.Equal(t, enabled, prepared.args[indices["native_compaction_v2"]])
				require.Equal(t, sql.NullString{String: requested, Valid: true}, prepared.args[indices["requested_reasoning_effort"]])
				require.Equal(t, "boolean", usageLogInsertArgTypes[indices["native_compaction_v2"]])
				require.Equal(t, "text", usageLogInsertArgTypes[indices["requested_reasoning_effort"]])
			}
		})
	}
}

func TestUsageLogUpstreamRequestIDReadback(t *testing.T) {
	id := "req-upstream-readback"
	for _, upstreamRequestID := range []*string{nil, &id} {
		name := "absent"
		if upstreamRequestID != nil {
			name = "present"
		}
		t.Run(name, func(t *testing.T) {
			columns := strings.Split(usageLogSelectColumns, ", ")
			require.Contains(t, columns, "upstream_request_id")
			values := buildUsageLogScanValues(usageLogScanRowOptions{
				ID: 9, RequestID: "client-request", Model: "gpt-5",
				UpstreamRequestID: upstreamRequestID, CreatedAt: time.Now().UTC(),
			})
			db, mock := newSQLMock(t)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT " + usageLogSelectColumns + " FROM usage_logs WHERE id = $1")).
				WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows(columns).AddRow(anySliceToDriverValues(values)...))
			repo := &usageLogRepository{sql: db}
			log, err := repo.GetByID(context.Background(), 9)
			require.NoError(t, err)
			require.Equal(t, upstreamRequestID, log.UpstreamRequestID)
			require.Equal(t, "client-request", log.RequestID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
