package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyAutoProbeHandlerSettingRepoStub struct {
	values map[string]string
}

func (s *proxyAutoProbeHandlerSettingRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	if value, ok := s.values[key]; ok {
		return &service.Setting{Key: key, Value: value}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (s *proxyAutoProbeHandlerSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (s *proxyAutoProbeHandlerSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *proxyAutoProbeHandlerSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *proxyAutoProbeHandlerSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range settings {
		s.values[key] = value
	}
	return nil
}

func (s *proxyAutoProbeHandlerSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *proxyAutoProbeHandlerSettingRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type proxyAutoProbeHandlerProxyRepoStub struct{}

func (s *proxyAutoProbeHandlerProxyRepoStub) Create(ctx context.Context, proxy *service.Proxy) error {
	return nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) GetByID(ctx context.Context, id int64) (*service.Proxy, error) {
	return &service.Proxy{ID: id}, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]service.Proxy, error) {
	return []service.Proxy{}, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) Update(ctx context.Context, proxy *service.Proxy) error {
	return nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) Delete(ctx context.Context, id int64) error { return nil }
func (s *proxyAutoProbeHandlerProxyRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]service.Proxy, *pagination.PaginationResult, error) {
	return []service.Proxy{}, &pagination.PaginationResult{Total: 0, Page: 1, PageSize: params.Limit()}, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]service.Proxy, *pagination.PaginationResult, error) {
	return []service.Proxy{}, &pagination.PaginationResult{Total: 0, Page: 1, PageSize: params.Limit()}, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]service.ProxyWithAccountCount, *pagination.PaginationResult, error) {
	return []service.ProxyWithAccountCount{}, &pagination.PaginationResult{Total: 0, Page: 1, PageSize: params.Limit()}, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) ListActive(ctx context.Context) ([]service.Proxy, error) {
	return []service.Proxy{}, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) ListActiveWithAccountCount(ctx context.Context) ([]service.ProxyWithAccountCount, error) {
	return []service.ProxyWithAccountCount{}, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	return false, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	return 0, nil
}
func (s *proxyAutoProbeHandlerProxyRepoStub) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]service.ProxyAccountSummary, error) {
	return []service.ProxyAccountSummary{}, nil
}

func TestProxyHandlerAutoProbeConfigEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	adminSvc := newStubAdminService()
	settingRepo := &proxyAutoProbeHandlerSettingRepoStub{}
	proxyRepo := &proxyAutoProbeHandlerProxyRepoStub{}
	autoProbeSvc := service.NewProxyAutoProbeService(nil, proxyRepo, settingRepo, nil)
	handler := NewProxyHandler(adminSvc, autoProbeSvc)

	router.GET("/api/v1/admin/proxies/auto-probe/config", handler.GetAutoProbeConfig)
	router.PUT("/api/v1/admin/proxies/auto-probe/config", handler.UpdateAutoProbeConfig)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/auto-probe/config", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{
		"enabled":              true,
		"default_interval_sec": 60,
		"retry_interval_sec":   5,
	})
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/proxies/auto-probe/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/auto-probe/config", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"enabled":true`)
	require.Contains(t, rec.Body.String(), `"default_interval_sec":60`)
	require.Contains(t, rec.Body.String(), `"retry_interval_sec":5`)
}
