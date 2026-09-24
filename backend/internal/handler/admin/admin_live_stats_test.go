package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type liveStatsUsageRepo struct {
	service.UsageLogRepository
	dashboardStats *usagestats.DashboardStats
	groupStats     []usagestats.GroupStat
}

func (r *liveStatsUsageRepo) GetDashboardStats(context.Context) (*usagestats.DashboardStats, error) {
	return r.dashboardStats, nil
}

func (r *liveStatsUsageRepo) GetGroupStatsWithFilters(context.Context, time.Time, time.Time, int64, int64, int64, int64, *int16, *bool, *int8) ([]usagestats.GroupStat, error) {
	return r.groupStats, nil
}

type liveStatsAdminService struct {
	service.AdminService
	stats map[string]any
}

func (s *liveStatsAdminService) GetGroupStats(context.Context, int64) (map[string]any, error) {
	return s.stats, nil
}

func decodeLiveStatsData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &envelope))
	return envelope.Data
}

func TestDashboardRealtimeMetricsReadsDashboardStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &liveStatsUsageRepo{
		dashboardStats: &usagestats.DashboardStats{
			Rpm:               7,
			AverageDurationMs: 42,
		},
	}
	handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/admin/dashboard/realtime", handler.GetRealtimeMetrics)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/realtime", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeLiveStatsData(t, rec.Body.Bytes())
	require.Equal(t, float64(7), data["requests_per_minute"])
	require.Equal(t, float64(42), data["average_response_time"])
}

func TestGroupStatsReadsConfiguredProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminService := &liveStatsAdminService{
		stats: map[string]any{
			"total_api_keys":  12,
			"active_api_keys": 9,
			"total_requests":  37,
			"total_cost":      1.25,
		},
	}
	handler := NewGroupHandler(adminService, nil, nil)
	router := gin.New()
	router.GET("/admin/groups/:id/stats", handler.GetStats)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/groups/8/stats", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeLiveStatsData(t, rec.Body.Bytes())
	require.Equal(t, float64(12), data["total_api_keys"])
	require.Equal(t, float64(9), data["active_api_keys"])
	require.Equal(t, float64(37), data["total_requests"])
	require.Equal(t, 1.25, data["total_cost"])
}
