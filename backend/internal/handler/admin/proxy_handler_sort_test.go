package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSortProxyRuntimeRowsByLatency(t *testing.T) {
	latency120 := int64(120)
	latency50 := int64(50)
	rows := []service.ProxyWithAccountCount{
		{Proxy: service.Proxy{ID: 1}, LatencyMs: &latency120},
		{Proxy: service.Proxy{ID: 2}},
		{Proxy: service.Proxy{ID: 3}, LatencyMs: &latency50},
	}

	sortProxyRuntimeRows(rows, "latency_ms", "asc")
	require.Equal(t, []int64{3, 1, 2}, proxyRuntimeRowIDs(rows))

	sortProxyRuntimeRows(rows, "latency", "desc")
	require.Equal(t, []int64{1, 3, 2}, proxyRuntimeRowIDs(rows))
}

func TestSortProxyRuntimeRowsByActiveEgressAccountCount(t *testing.T) {
	rows := []service.ProxyWithAccountCount{
		{Proxy: service.Proxy{ID: 1}, ActiveEgressAccountCount: 2},
		{Proxy: service.Proxy{ID: 2}, ActiveEgressAccountCount: 5},
		{Proxy: service.Proxy{ID: 3}, ActiveEgressAccountCount: 5},
	}

	sortProxyRuntimeRows(rows, "active_egress_account_count", "desc")
	require.Equal(t, []int64{3, 2, 1}, proxyRuntimeRowIDs(rows))

	sortProxyRuntimeRows(rows, "active_egress_account_count", "asc")
	require.Equal(t, []int64{1, 3, 2}, proxyRuntimeRowIDs(rows))
}

func TestProxyHandlerListSortsLatencyBeforePagination(t *testing.T) {
	router, adminSvc := setupAdminRouter()
	latency300 := int64(300)
	latency80 := int64(80)
	adminSvc.proxyCounts = []service.ProxyWithAccountCount{
		{Proxy: service.Proxy{ID: 1, Name: "slow"}, LatencyMs: &latency300},
		{Proxy: service.Proxy{ID: 2, Name: "unknown"}},
		{Proxy: service.Proxy{ID: 3, Name: "fast"}, LatencyMs: &latency80},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies?sort_by=latency_ms&sort_order=asc&page=1&page_size=2", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, int64(3), body.Data.Total)
	require.Equal(t, []int64{3, 1}, []int64{body.Data.Items[0].ID, body.Data.Items[1].ID})
}

func proxyRuntimeRowIDs(rows []service.ProxyWithAccountCount) []int64 {
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	return ids
}
