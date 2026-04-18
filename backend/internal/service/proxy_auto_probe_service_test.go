package service

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type proxyAutoProbeSettingRepoStub struct {
	values map[string]string
}

func (s *proxyAutoProbeSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	if value, ok := s.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (s *proxyAutoProbeSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (s *proxyAutoProbeSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *proxyAutoProbeSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *proxyAutoProbeSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range settings {
		s.values[key] = value
	}
	return nil
}

func (s *proxyAutoProbeSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *proxyAutoProbeSettingRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type proxyAutoProbeRepoStub struct {
	proxies []Proxy
}

func (s *proxyAutoProbeRepoStub) Create(ctx context.Context, proxy *Proxy) error { return nil }
func (s *proxyAutoProbeRepoStub) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	for i := range s.proxies {
		if s.proxies[i].ID == id {
			proxy := s.proxies[i]
			return &proxy, nil
		}
	}
	return nil, ErrProxyNotFound
}
func (s *proxyAutoProbeRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	return nil, nil
}
func (s *proxyAutoProbeRepoStub) Update(ctx context.Context, proxy *Proxy) error { return nil }
func (s *proxyAutoProbeRepoStub) Delete(ctx context.Context, id int64) error     { return nil }
func (s *proxyAutoProbeRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	if len(s.proxies) == 0 {
		return []Proxy{}, paginationResultForTest(0, params), nil
	}
	start := params.Offset()
	if start >= len(s.proxies) {
		return []Proxy{}, paginationResultForTest(int64(len(s.proxies)), params), nil
	}
	end := start + params.Limit()
	if end > len(s.proxies) {
		end = len(s.proxies)
	}
	items := append([]Proxy(nil), s.proxies[start:end]...)
	return items, paginationResultForTest(int64(len(s.proxies)), params), nil
}
func (s *proxyAutoProbeRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
	return s.List(ctx, params)
}
func (s *proxyAutoProbeRepoStub) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	return nil, paginationResultForTest(0, params), nil
}
func (s *proxyAutoProbeRepoStub) ListActive(ctx context.Context) ([]Proxy, error) {
	return s.proxies, nil
}
func (s *proxyAutoProbeRepoStub) ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error) {
	return nil, nil
}
func (s *proxyAutoProbeRepoStub) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	return false, nil
}
func (s *proxyAutoProbeRepoStub) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	return 0, nil
}
func (s *proxyAutoProbeRepoStub) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	return nil, nil
}

type proxyAutoProbeLatencyCacheStub struct {
	items map[int64]*ProxyLatencyInfo
}

func (s *proxyAutoProbeLatencyCacheStub) GetProxyLatencies(ctx context.Context, proxyIDs []int64) (map[int64]*ProxyLatencyInfo, error) {
	result := make(map[int64]*ProxyLatencyInfo, len(proxyIDs))
	for _, id := range proxyIDs {
		if item, ok := s.items[id]; ok {
			result[id] = item
		}
	}
	return result, nil
}

func (s *proxyAutoProbeLatencyCacheStub) SetProxyLatency(ctx context.Context, proxyID int64, info *ProxyLatencyInfo) error {
	if s.items == nil {
		s.items = map[int64]*ProxyLatencyInfo{}
	}
	s.items[proxyID] = info
	return nil
}

func TestProxyAutoProbeServiceUpdateConfigRejectsInvalidIntervals(t *testing.T) {
	svc := NewProxyAutoProbeService(nil, &proxyAutoProbeRepoStub{}, &proxyAutoProbeSettingRepoStub{}, nil)

	_, err := svc.UpdateConfig(context.Background(), &ProxyAutoProbeUpdateInput{
		Enabled:            true,
		DefaultIntervalSec: 0,
		RetryIntervalSec:   5,
	})
	require.Error(t, err)

	_, err = svc.UpdateConfig(context.Background(), &ProxyAutoProbeUpdateInput{
		Enabled:            true,
		DefaultIntervalSec: 5,
		RetryIntervalSec:   0,
	})
	require.Error(t, err)
}

func TestProxyAutoProbeServiceInitializeEntriesUsesCachedQueues(t *testing.T) {
	healthyLatency := int64(30)
	failedLatency := int64(80)
	repo := &proxyAutoProbeRepoStub{
		proxies: []Proxy{
			{ID: 1, Name: "p1"},
			{ID: 2, Name: "p2"},
			{ID: 3, Name: "p3"},
		},
	}
	cache := &proxyAutoProbeLatencyCacheStub{
		items: map[int64]*ProxyLatencyInfo{
			1: {Success: true, QualityStatus: "healthy", LatencyMs: &healthyLatency},
			2: {Success: false, LatencyMs: &failedLatency},
		},
	}
	svc := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, cache)
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5}

	now := time.Now().UTC()
	require.NoError(t, svc.initializeEntries(context.Background(), now))

	require.Len(t, svc.entries, 3)
	require.Equal(t, ProxyAutoProbeQueueSuccess, svc.entries[1].Queue)
	require.Equal(t, ProxyAutoProbeQueueFailed, svc.entries[2].Queue)
	require.Equal(t, ProxyAutoProbeQueueSuccess, svc.entries[3].Queue)
	require.Equal(t, now.Add(60*time.Second), svc.entries[1].NextDueAt)
	require.Equal(t, now.Add(5*time.Second), svc.entries[2].NextDueAt)
	require.NotNil(t, svc.entries[1].LastLatencyMs)
	require.Equal(t, healthyLatency, *svc.entries[1].LastLatencyMs)
}

func TestProxyAutoProbeEntryLessPrefersFailedThenSuccessLatency(t *testing.T) {
	now := time.Now()
	latency20 := int64(20)
	latency50 := int64(50)
	entries := []*proxyAutoProbeEntry{
		{ProxyID: 3, Queue: ProxyAutoProbeQueueSuccess, NextDueAt: now, LastLatencyMs: &latency50},
		{ProxyID: 2, Queue: ProxyAutoProbeQueueSuccess, NextDueAt: now, LastLatencyMs: &latency20},
		{ProxyID: 1, Queue: ProxyAutoProbeQueueFailed, NextDueAt: now},
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return proxyAutoProbeEntryLess(entries[i], entries[j])
	})

	require.Equal(t, int64(1), entries[0].ProxyID)
	require.Equal(t, int64(2), entries[1].ProxyID)
	require.Equal(t, int64(3), entries[2].ProxyID)
}

func TestProxyAutoProbeServiceFinishProbeTransitionsQueue(t *testing.T) {
	svc := NewProxyAutoProbeService(nil, &proxyAutoProbeRepoStub{}, &proxyAutoProbeSettingRepoStub{}, nil)
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5}
	svc.entries[7] = &proxyAutoProbeEntry{ProxyID: 7, Queue: ProxyAutoProbeQueueSuccess}
	svc.currentProxyID = ptrInt64(7)

	finishedAt := time.Now()
	svc.finishProbe(7, proxyAutoProbeOutcome{Success: false, QualityStatus: "failed"}, finishedAt)
	require.Nil(t, svc.currentProxyID)
	require.Equal(t, ProxyAutoProbeQueueFailed, svc.entries[7].Queue)
	require.Equal(t, finishedAt.Add(5*time.Second), svc.entries[7].NextDueAt)

	latency := int64(18)
	svc.currentProxyID = ptrInt64(7)
	svc.finishProbe(7, proxyAutoProbeOutcome{Success: true, QualityStatus: "healthy", LatencyMs: &latency}, finishedAt)
	require.Equal(t, ProxyAutoProbeQueueSuccess, svc.entries[7].Queue)
	require.Equal(t, finishedAt.Add(60*time.Second), svc.entries[7].NextDueAt)
	require.NotNil(t, svc.entries[7].LastLatencyMs)
	require.Equal(t, latency, *svc.entries[7].LastLatencyMs)
}

func ptrInt64(v int64) *int64 {
	return &v
}

func paginationResultForTest(total int64, params pagination.PaginationParams) *pagination.PaginationResult {
	limit := params.Limit()
	pages := 0
	if limit > 0 {
		pages = int((total + int64(limit) - 1) / int64(limit))
	}
	page := params.Page
	if page < 1 {
		page = 1
	}
	return &pagination.PaginationResult{
		Total:    total,
		Page:     page,
		PageSize: limit,
		Pages:    pages,
	}
}
