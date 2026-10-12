package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUserUsageRejectsInvalidIP(t *testing.T) {
	router := newUserUsageRequestTypeTestRouter(&userUsageRepoCapture{})
	for _, raw := range []string{"example.com", strings.Repeat("192.0.2.1,", 100) + "192.0.2.1"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage?ip_addresses="+raw, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
}

type userIPOptionsCapture struct {
	*userUsageRepoCapture
	optionsFilters usagestats.UsageLogFilters
	chartFilters   []usagestats.UsageLogFilters
	search         string
	offset, limit  int
}

func (r *userIPOptionsCapture) GetIPOptionsWithFilters(_ context.Context, f usagestats.UsageLogFilters, s string, o, l int) (*usagestats.UsageIPOptions, error) {
	r.optionsFilters = f
	r.search = s
	r.offset = o
	r.limit = l
	return &usagestats.UsageIPOptions{IPs: []string{"192.0.2.1"}}, nil
}
func (r *userIPOptionsCapture) GetUsageTrendWithUsageFilters(_ context.Context, _ time.Time, _ time.Time, _ string, f usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.chartFilters = append(r.chartFilters, f)
	return []usagestats.TrendDataPoint{}, nil
}
func (r *userIPOptionsCapture) GetModelStatsWithUsageFiltersBySource(_ context.Context, _ time.Time, _ time.Time, f usagestats.UsageLogFilters, _ string) ([]usagestats.ModelStat, error) {
	r.chartFilters = append(r.chartFilters, f)
	return []usagestats.ModelStat{}, nil
}
func (r *userIPOptionsCapture) GetGroupStatsWithUsageFilters(_ context.Context, _ time.Time, _ time.Time, f usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	r.chartFilters = append(r.chartFilters, f)
	return []usagestats.GroupStat{}, nil
}
func userIPRouter(repo *userIPOptionsCapture, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if authenticated {
		r.Use(func(c *gin.Context) {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
			c.Next()
		})
	}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	r.GET("/usage", h.List)
	r.GET("/usage/stats", h.Stats)
	r.GET("/usage/ip-options", h.IPOptions)
	r.GET("/usage/dashboard/snapshot-v2", h.DashboardSnapshotV2)
	return r
}
func TestUserUsageIPBindingAndCandidateScope(t *testing.T) {
	repo := &userIPOptionsCapture{userUsageRepoCapture: &userUsageRepoCapture{}}
	router := userIPRouter(repo, true)
	query := "?ip_addresses=2001:0DB8::1,192.0.2.1,192.0.2.1&user_id=999&account_id=999&start_date=2026-10-01&end_date=2026-10-10&group_id=3&model=x&request_type=stream&billing_mode=token&include_group_stats=true"
	for _, path := range []string{"/usage", "/usage/stats", "/usage/dashboard/snapshot-v2"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+query, nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	all := append([]usagestats.UsageLogFilters{repo.listFilters, repo.statsFilters}, repo.chartFilters...)
	require.GreaterOrEqual(t, len(all), 5)
	for _, f := range all {
		require.Equal(t, int64(42), f.UserID)
		require.Zero(t, f.AccountID)
		require.Equal(t, []string{"192.0.2.1", "2001:0DB8::1"}, f.IPAddresses)
		require.EqualValues(t, 3, f.GroupID)
		require.Equal(t, "x", f.Model)
		require.NotNil(t, f.StartTime)
		require.NotNil(t, f.EndTime)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/ip-options"+query+"&search=192&offset=50&limit=200", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.EqualValues(t, 42, repo.optionsFilters.UserID)
	require.Zero(t, repo.optionsFilters.AccountID)
	require.Empty(t, repo.optionsFilters.IPAddresses)
	require.Equal(t, 50, repo.offset)
	require.Equal(t, 100, repo.limit)
	require.Equal(t, "192", repo.search)
	require.Contains(t, rec.Body.String(), `"ips":["192.0.2.1"]`)
	for _, q := range []string{"offset=-1", "offset=x", "limit=0", "limit=x"} {
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/ip-options?"+q, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	router = userIPRouter(repo, false)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/ip-options", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
func TestUserIPOptionsRejectsForeignAPIKey(t *testing.T) {
	repo := &userIPOptionsCapture{userUsageRepoCapture: &userUsageRepoCapture{}}
	keys := &dailyUsageAPIKeyRepoStub{keys: map[int64]*service.APIKey{7: {ID: 7, UserID: 99}, 8: {ID: 8, UserID: 42}}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), service.NewAPIKeyService(keys, nil, nil, nil, nil, nil, nil), nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/usage/ip-options", h.IPOptions)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/ip-options?api_key_id=7", nil))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Zero(t, repo.optionsFilters.UserID)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/ip-options?api_key_id=8&user_id=99", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 42, repo.optionsFilters.UserID)
	require.EqualValues(t, 8, repo.optionsFilters.APIKeyID)
}
