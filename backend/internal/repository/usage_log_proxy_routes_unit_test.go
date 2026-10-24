//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogProxyRoutesInsertColumn(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{RequestID: "proxy-route-column", Model: "model"})
	key := usageLogBatchKey(prepared.requestID, 0)
	batch, _ := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
	bestEffort, _ := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	for name, query := range map[string]string{"batch": batch, "best_effort": bestEffort} {
		t.Run(name, func(t *testing.T) {
			match := regexp.MustCompile(`(?s)INSERT INTO usage_logs\s*\((.*?)\)`).FindStringSubmatch(query)
			require.Len(t, match, 2)
			var columns []string
			for _, column := range strings.Split(match[1], ",") {
				columns = append(columns, strings.TrimSpace(column))
			}
			require.Contains(t, columns, "proxy_routes", "every usage insertion path must persist actual routes")
		})
	}
}

func TestUsageLogProxyRoutesInsertSnapshotAndHistoricalNull(t *testing.T) {
	routes := []service.ProxyRouteSnapshot{
		{Kind: "proxy", ProxyID: 81, Name: "first-node", Host: "2001:db8::7", Port: 1080},
		{Kind: "direct"},
		{Kind: "proxy", ProxyID: 82, Name: "fallback-node", Host: "proxy.example", Port: 8080},
	}
	for _, tc := range []struct {
		name   string
		routes []service.ProxyRouteSnapshot
		want   string
	}{
		{"observed", routes, `[{"kind":"proxy","proxy_id":81,"name":"first-node","host":"2001:db8::7","port":1080},{"kind":"direct"},{"kind":"proxy","proxy_id":82,"name":"fallback-node","host":"proxy.example","port":8080}]`},
		{"historical", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &service.UsageLog{RequestID: "snapshot:" + tc.name, Model: "model", ProxyRoutes: tc.routes}
			prepared := prepareUsageLogInsert(log)
			key := usageLogBatchKey(prepared.requestID, 0)
			query, args := buildUsageLogBatchInsertQuery([]string{key}, map[string]usageLogInsertPrepared{key: prepared})
			match := regexp.MustCompile(`(?s)INSERT INTO usage_logs\s*\((.*?)\)`).FindStringSubmatch(query)
			require.Len(t, match, 2)
			idx := -1
			for i, column := range strings.Split(match[1], ",") {
				if strings.TrimSpace(column) == "proxy_routes" {
					idx = i
					break
				}
			}
			require.NotEqual(t, -1, idx)
			require.Equal(t, "jsonb", usageLogInsertArgTypes[idx])
			require.Len(t, prepared.args, len(usageLogInsertArgTypes))
			if tc.want == "" {
				require.Nil(t, prepared.args[idx], "historical routes must persist SQL NULL")
			} else {
				payload, ok := prepared.args[idx].(string)
				require.True(t, ok, "JSONB must be passed as a JSON string to the SQL driver")
				require.JSONEq(t, tc.want, payload)
				// Once queued, a mutable source slice cannot rewrite the stored snapshot.
				log.ProxyRoutes[0].Name = "renamed-after-enqueue"
				require.JSONEq(t, tc.want, prepared.args[idx].(string))
			}
			require.Equal(t, prepared.args[idx], args[idx+1])
		})
	}
}

func TestUsageLogProxyRoutesReadback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload *string
		want    []service.ProxyRouteSnapshot
	}{
		{name: "historical"},
		{name: "observed", payload: proxyRoutesJSONPointer(`[{"kind":"proxy","proxy_id":91,"name":"removed-node","host":"2001:db8::8","port":1080},{"kind":"system"}]`), want: []service.ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 91, Name: "removed-node", Host: "2001:db8::8", Port: 1080}, {Kind: "system"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			columns := strings.Split(usageLogSelectColumns, ", ")
			require.Contains(t, columns, "proxy_routes")
			values := buildUsageLogScanValues(usageLogScanRowOptions{ID: 8, Model: "model", CreatedAt: time.Now().UTC()})
			require.Len(t, values, len(columns))
			for i, column := range columns {
				if column == "proxy_routes" {
					values[i] = nullString(tc.payload)
				}
			}
			db, mock := newSQLMock(t)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT " + usageLogSelectColumns + " FROM usage_logs WHERE id = $1")).WithArgs(int64(8)).WillReturnRows(sqlmock.NewRows(columns).AddRow(anySliceToDriverValues(values)...))
			got, err := (&usageLogRepository{sql: db}).GetByID(context.Background(), 8)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.ProxyRoutes)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageLogProxyRoutesStaticInsertPaths(t *testing.T) {
	for _, noResult := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "billing_no_result"}[noResult], func(t *testing.T) {
			log := &service.UsageLog{Model: "model", ProxyRoutes: []service.ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 12, Host: "node.example", Port: 3128}}, CreatedAt: time.Now().UTC()}
			prepared := prepareUsageLogInsert(log)
			var captured []string
			db, mock := newSQLCapturingMock(t, &captured)
			args := anySliceToDriverValues(prepared.args)
			if noResult {
				mock.ExpectExec("INSERT INTO usage_logs").WithArgs(args...).WillReturnResult(sqlmock.NewResult(0, 1))
				require.NoError(t, execUsageLogInsertNoResult(context.Background(), db, prepared))
			} else {
				mock.ExpectQuery("INSERT INTO usage_logs").WithArgs(args...).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(1), log.CreatedAt))
				_, err := (&usageLogRepository{sql: db}).Create(context.Background(), log)
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
			require.Len(t, captured, 1)
			requireStaticInsertMatchesArgTypes(t, captured[0])
			match := usageLogStaticInsertShapeRe.FindStringSubmatch(captured[0])
			found := false
			for i, column := range strings.Split(match[1], ",") {
				if strings.TrimSpace(column) != "proxy_routes" {
					continue
				}
				found = true
				var routes []service.ProxyRouteSnapshot
				require.NoError(t, json.Unmarshal([]byte(args[i].(string)), &routes))
				require.Equal(t, log.ProxyRoutes, routes)
			}
			require.True(t, found)
		})
	}
}

func proxyRoutesJSONPointer(value string) *string { return &value }
