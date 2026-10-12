package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminUsageRejectsInvalidIP(t *testing.T) {
	router := newAdminUsageRequestTypeTestRouter(&adminUsageRepoCapture{})
	for _, path := range []string{"/admin/usage", "/admin/usage/stats"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?ip_addresses=not-an-ip", nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
}
func TestUsageIPStatsCacheIsolation(t *testing.T) {
	a := usagestats.UsageLogFilters{IPAddresses: []string{"192.0.2.1", "::1"}}
	b := usagestats.UsageLogFilters{IPAddresses: []string{"::1", "192.0.2.1", "::1"}}
	require.Equal(t, usageStatsCacheKey(a), usageStatsCacheKey(b))
	require.NotEqual(t, usageStatsCacheKey(a), usageStatsCacheKey(usagestats.UsageLogFilters{}))
	require.NotEqual(t, usageStatsCacheKey(a), usageStatsCacheKey(usagestats.UsageLogFilters{IPAddresses: []string{"192.0.2.2"}}))
}
func TestDashboardRejectsInvalidIP(t *testing.T) {
	router := newDashboardRequestTypeTestRouter(&dashboardUsageRepoCapture{})
	for _, path := range []string{"trend", "models", "groups"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/dashboard/"+path+"?ip_addresses=host", nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
}

type adminIPOptionsCapture struct {
	*adminUsageRepoCapture
	optionsFilters usagestats.UsageLogFilters
}

func (r *adminIPOptionsCapture) GetIPOptionsWithFilters(_ context.Context, f usagestats.UsageLogFilters, _ string, _ int, _ int) (*usagestats.UsageIPOptions, error) {
	r.optionsFilters = f
	return &usagestats.UsageIPOptions{IPs: []string{}}, nil
}
func TestAdminUsageIPBindingAndOptions(t *testing.T) {
	repo := &adminIPOptionsCapture{adminUsageRepoCapture: &adminUsageRepoCapture{}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/usage", h.List)
	router.GET("/usage/stats", h.Stats)
	router.GET("/usage/ip-options", h.IPOptions)
	query := "?ip_addresses=::1,192.0.2.1&user_id=7&account_id=8&group_id=9&model=gpt&start_date=2026-10-01&end_date=2026-10-02&request_type=sync&native_compaction_v2=true&upstream_model_mismatch=true"
	for _, path := range []string{"/usage", "/usage/stats"} {
		url := path + query
		if path == "/usage/stats" {
			url += "&nocache=true"
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	for _, f := range []usagestats.UsageLogFilters{repo.listFilters, repo.statsFilters} {
		require.Equal(t, []string{"192.0.2.1", "::1"}, f.IPAddresses)
		require.EqualValues(t, 7, f.UserID)
		require.EqualValues(t, 8, f.AccountID)
		require.EqualValues(t, 9, f.GroupID)
		require.Equal(t, "gpt", f.Model)
		require.NotNil(t, f.StartTime)
		require.NotNil(t, f.EndTime)
		require.NotNil(t, f.NativeCompactionV2)
		require.NotNil(t, f.UpstreamModelMismatch)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/ip-options"+query, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, repo.optionsFilters.IPAddresses)
	require.EqualValues(t, 7, repo.optionsFilters.UserID)
	require.EqualValues(t, 8, repo.optionsFilters.AccountID)
	require.NotNil(t, repo.optionsFilters.StartTime)
}

type dashboardIPCapture struct {
	service.UsageLogRepository
	filters []usagestats.UsageLogFilters
}

func (r *dashboardIPCapture) GetUsageTrendWithUsageFilters(_ context.Context, _ time.Time, _ time.Time, _ string, f usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.filters = append(r.filters, f)
	return []usagestats.TrendDataPoint{}, nil
}
func (r *dashboardIPCapture) GetModelStatsWithUsageFiltersBySource(_ context.Context, _ time.Time, _ time.Time, f usagestats.UsageLogFilters, _ string) ([]usagestats.ModelStat, error) {
	r.filters = append(r.filters, f)
	return []usagestats.ModelStat{}, nil
}
func (r *dashboardIPCapture) GetGroupStatsWithUsageFilters(_ context.Context, _ time.Time, _ time.Time, f usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	r.filters = append(r.filters, f)
	return []usagestats.GroupStat{}, nil
}
func TestDashboardIPBindingAndCacheIsolation(t *testing.T) {
	resetDashboardReadCachesForTest()
	t.Cleanup(resetDashboardReadCachesForTest)
	repo := &dashboardIPCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/trend", h.GetUsageTrend)
	router.GET("/models", h.GetModelStats)
	router.GET("/groups", h.GetGroupStats)
	for _, path := range []string{"/trend", "/models", "/groups"} {
		for i, raw := range []string{"192.0.2.1,::1", "::1,192.0.2.1,::1", "192.0.2.2", ""} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?start_date=2026-10-01&end_date=2026-10-02&user_id=7&ip_addresses="+raw, nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			if i == 1 {
				require.Equal(t, "hit", rec.Header().Get("X-Snapshot-Cache"))
			} else {
				require.Equal(t, "miss", rec.Header().Get("X-Snapshot-Cache"))
			}
		}
	}
	require.Len(t, repo.filters, 9)
	for i, f := range repo.filters {
		require.EqualValues(t, 7, f.UserID)
		switch i % 3 {
		case 0:
			require.Equal(t, []string{"192.0.2.1", "::1"}, f.IPAddresses)
		case 1:
			require.Equal(t, []string{"192.0.2.2"}, f.IPAddresses)
		case 2:
			require.Empty(t, f.IPAddresses)
		}
	}
}
func TestDashboardSnapshotV2IPBindingAndCacheIsolation(t *testing.T) {
	resetDashboardReadCachesForTest()
	dashboardSnapshotV2Cache = newSnapshotCache(30 * time.Second)
	t.Cleanup(func() {
		resetDashboardReadCachesForTest()
		dashboardSnapshotV2Cache = newSnapshotCache(30 * time.Second)
	})
	repo := &dashboardIPCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/snapshot-v2", h.GetSnapshotV2)
	base := "/snapshot-v2?include_stats=false&include_users_trend=false&include_trend=true&include_model_stats=true&include_group_stats=true&start_date=2026-10-01&end_date=2026-10-02&user_id=7&ip_addresses="
	for i, raw := range []string{"192.0.2.1,::1", "::1,192.0.2.1,::1", "192.0.2.2", ""} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base+raw, nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		if i == 1 {
			require.Equal(t, "hit", rec.Header().Get("X-Snapshot-Cache"))
		} else {
			require.Equal(t, "miss", rec.Header().Get("X-Snapshot-Cache"))
		}
		if i == 0 {
			require.Len(t, repo.filters, 3)
			for _, f := range repo.filters {
				require.Equal(t, []string{"192.0.2.1", "::1"}, f.IPAddresses)
			}
		}
	}
	require.Len(t, repo.filters, 9)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, base+"host", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
func TestDashboardSnapshotV2RejectsUnsupportedIPSections(t *testing.T) {
	repo := &dashboardIPCapture{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/snapshot-v2", h.GetSnapshotV2)
	for _, query := range []string{"include_stats=true", "include_stats=false&include_users_trend=true"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshot-v2?ip_addresses=192.0.2.1&"+query, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
}
func (r *dashboardIPCapture) GetDashboardStats(context.Context) (*usagestats.DashboardStats, error) {
	return &usagestats.DashboardStats{}, nil
}
func (r *dashboardIPCapture) GetUserUsageTrend(context.Context, time.Time, time.Time, string, int, string) ([]usagestats.UserUsageTrendPoint, error) {
	return []usagestats.UserUsageTrendPoint{}, nil
}
