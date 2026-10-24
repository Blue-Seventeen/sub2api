package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsErrorProxyRoutes_InsertSnapshotAndUnknown(t *testing.T) {
	var input service.OpsInsertErrorLogInput
	require.NoError(t, json.Unmarshal([]byte(`{"proxy_routes":[{"kind":"proxy","proxy_id":152,"name":"request-time-node","host":"192.0.2.10","port":1080},{"kind":"direct"},{"kind":"system_proxy"}]}`), &input))
	args := opsInsertErrorLogArgs(&input)
	require.Contains(t, insertOpsErrorLogSQL, "proxy_routes")
	require.Len(t, args, strings.Count(insertOpsErrorLogSQL, "$"))
	require.Len(t, args, 47)
	encoded, ok := args[46].(string)
	require.True(t, ok, "route snapshots must be materialized JSON before the DB call")
	require.JSONEq(t, `[{"kind":"proxy","proxy_id":152,"name":"request-time-node","host":"192.0.2.10","port":1080},{"kind":"direct"},{"kind":"system_proxy"}]`, encoded)
	require.NotContains(t, encoded, "password")
	require.NotContains(t, encoded, "://")
	unknown := opsInsertErrorLogArgs(&service.OpsInsertErrorLogInput{})
	require.Nil(t, unknown[46], "unobserved and historical attribution must remain SQL NULL")
}

// Complete row fixtures exercise the SQL projection/Scan boundary for both
// endpoints, including the historical NULL case, without a database process.
func opsProxyRouteRowForTest(detail bool, payload driver.Value) *sqlmock.Rows {
	count := 43
	if detail {
		count = 58
	}
	columns := make([]string, count)
	values := make([]driver.Value, count)
	for i := range columns {
		columns[i] = fmt.Sprintf("column_%d", i)
		values[i] = ""
	}
	values[0], values[1] = int64(11), time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	values[2], values[3], values[6], values[7], values[10] = "upstream", "upstream_error", "P2", int64(502), false
	if detail {
		for _, i := range []int{11, 12, 17, 25, 27, 28, 30, 32, 43, 45, 46, 47, 48, 49, 51, 56} {
			values[i] = nil
		}
		values[22], values[23], values[24], values[34] = false, false, int64(502), false
	} else {
		for _, i := range []int{11, 12, 17, 19, 20, 22, 24, 36, 38, 40} {
			values[i] = nil
		}
		values[26] = false
	}
	values[count-1] = payload
	return sqlmock.NewRows(columns).AddRow(values...)
}

func TestOpsErrorProxyRoutes_ListAndDetailReadStoredSnapshot(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, known := range []bool{false, true} {
			t.Run(fmt.Sprintf("detail=%v/known=%v", detail, known), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				defer db.Close()
				var payload driver.Value
				if known {
					payload = `[{"kind":"proxy","proxy_id":152,"name":"name-at-request-time","host":"192.0.2.10","port":1080},{"kind":"direct"}]`
				}
				if !detail {
					mock.ExpectQuery(`SELECT COUNT`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
				}
				mock.ExpectQuery(`(?s)SELECT\s+e.id,.*e.proxy_routes\s+FROM ops_error_logs e`).WillReturnRows(opsProxyRouteRowForTest(detail, payload))
				repo := NewOpsRepository(db)
				var routes []service.ProxyRouteSnapshot
				if detail {
					entry, err := repo.GetErrorLogByID(context.Background(), 11)
					require.NoError(t, err)
					routes = entry.ProxyRoutes
				} else {
					entries, err := repo.ListErrorLogs(context.Background(), &service.OpsErrorLogFilter{})
					require.NoError(t, err)
					require.Len(t, entries.Errors, 1)
					routes = entries.Errors[0].ProxyRoutes
				}
				if known {
					require.Equal(t, []service.ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 152, Name: "name-at-request-time", Host: "192.0.2.10", Port: 1080}, {Kind: "direct"}}, routes)
				} else {
					require.Nil(t, routes)
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}
