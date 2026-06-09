package service

import (
	"context"
	"errors"
	"sort"
	"sync"
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
func (s *proxyAutoProbeRepoStub) ExistsByProtocolHostPortAuth(ctx context.Context, protocol, host string, port int, username, password string) (bool, error) {
	return false, nil
}
func (s *proxyAutoProbeRepoStub) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	return 0, nil
}
func (s *proxyAutoProbeRepoStub) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	return nil, nil
}
func (s *proxyAutoProbeRepoStub) SweepExpiredProxies(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
}
func (s *proxyAutoProbeRepoStub) ListAllForFallback(ctx context.Context) ([]Proxy, error) {
	return s.proxies, nil
}
func (s *proxyAutoProbeRepoStub) CountExpired(ctx context.Context) (int64, error) {
	return 0, nil
}
func (s *proxyAutoProbeRepoStub) CountExpiringSoon(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
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

type proxyStickyStoreStub struct {
	mu     sync.Mutex
	items  map[int64]int64
	getErr error
}

func (s *proxyStickyStoreStub) Get(ctx context.Context, accountID int64) (int64, bool, error) {
	if s.getErr != nil {
		return 0, false, s.getErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		return 0, false, nil
	}
	proxyID, ok := s.items[accountID]
	return proxyID, ok, nil
}

func (s *proxyStickyStoreStub) Set(ctx context.Context, accountID, proxyID int64, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items == nil {
		s.items = map[int64]int64{}
	}
	s.items[accountID] = proxyID
	return nil
}

func (s *proxyStickyStoreStub) Refresh(ctx context.Context, accountID int64, ttl time.Duration) error {
	return nil
}

func (s *proxyStickyStoreStub) DeleteIfMatch(ctx context.Context, accountID, proxyID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items != nil && s.items[accountID] == proxyID {
		delete(s.items, accountID)
	}
	return nil
}

func seedProxyAutoProbeSnapshots(svc *ProxyAutoProbeService, proxies ...Proxy) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.proxySnapshots == nil {
		svc.proxySnapshots = map[int64]Proxy{}
	}
	for _, proxy := range proxies {
		svc.proxySnapshots[proxy.ID] = proxy
	}
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

func TestProxyAutoProbeServiceLoadConfigDefaultsStickyForOldConfig(t *testing.T) {
	settingRepo := &proxyAutoProbeSettingRepoStub{values: map[string]string{
		SettingKeyProxyAutoProbeConfig: `{"enabled":true,"default_interval_sec":60,"retry_interval_sec":5}`,
	}}
	svc := NewProxyAutoProbeService(nil, &proxyAutoProbeRepoStub{}, settingRepo, nil)

	cfg, err := svc.loadConfig(context.Background())

	require.NoError(t, err)
	require.True(t, cfg.StickyEnabled)
	require.Equal(t, defaultProxyStickyTTLSeconds, cfg.StickyTTLSeconds)
}

func TestProxyAutoProbeServiceInitializeEntriesUsesCachedQueues(t *testing.T) {
	healthyLatency := int64(30)
	warnLatency := int64(45)
	failedLatency := int64(80)
	repo := &proxyAutoProbeRepoStub{
		proxies: []Proxy{
			{ID: 1, Name: "p1"},
			{ID: 2, Name: "p2"},
			{ID: 3, Name: "p3"},
			{ID: 4, Name: "p4"},
			{ID: 5, Name: "p5"},
		},
	}
	cache := &proxyAutoProbeLatencyCacheStub{
		items: map[int64]*ProxyLatencyInfo{
			1: {Success: true, QualityStatus: "healthy", LatencyMs: &healthyLatency},
			2: {Success: false, LatencyMs: &failedLatency},
			4: {Success: true, QualityStatus: "warn", LatencyMs: &warnLatency},
			5: {Success: false, QualityStatus: "healthy"},
		},
	}
	svc := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, cache)
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5}

	now := time.Now().UTC()
	require.NoError(t, svc.initializeEntries(context.Background(), now))

	require.Len(t, svc.entries, 5)
	require.Equal(t, ProxyAutoProbeQueueSuccess, svc.entries[1].Queue)
	require.Equal(t, ProxyAutoProbeQueueFailed, svc.entries[2].Queue)
	require.Equal(t, ProxyAutoProbeQueueSuccess, svc.entries[3].Queue)
	require.Equal(t, ProxyAutoProbeQueueSuccess, svc.entries[4].Queue)
	require.Equal(t, ProxyAutoProbeQueueFailed, svc.entries[5].Queue)
	require.Equal(t, now.Add(60*time.Second), svc.entries[1].NextDueAt)
	require.Equal(t, now.Add(5*time.Second), svc.entries[2].NextDueAt)
	require.Equal(t, now.Add(60*time.Second), svc.entries[4].NextDueAt)
	require.Equal(t, now.Add(5*time.Second), svc.entries[5].NextDueAt)
	require.NotNil(t, svc.entries[1].LastLatencyMs)
	require.Equal(t, healthyLatency, *svc.entries[1].LastLatencyMs)
	require.NotNil(t, svc.entries[4].LastLatencyMs)
	require.Equal(t, warnLatency, *svc.entries[4].LastLatencyMs)
}

func TestProxyAutoProbeServiceSelectionUsesInjectedNodeLatency(t *testing.T) {
	nodeAFast := int64(10)
	nodeASlow := int64(80)
	nodeBFast := int64(12)
	nodeBSlow := int64(90)
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{
		{ID: 1, Name: "p1", Status: StatusActive},
		{ID: 2, Name: "p2", Status: StatusActive},
	}}
	svcA := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, &proxyAutoProbeLatencyCacheStub{
		items: map[int64]*ProxyLatencyInfo{
			1: {Success: true, QualityStatus: "healthy", LatencyMs: &nodeAFast},
			2: {Success: true, QualityStatus: "healthy", LatencyMs: &nodeASlow},
		},
	})
	svcB := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, &proxyAutoProbeLatencyCacheStub{
		items: map[int64]*ProxyLatencyInfo{
			1: {Success: true, QualityStatus: "healthy", LatencyMs: &nodeBSlow},
			2: {Success: true, QualityStatus: "healthy", LatencyMs: &nodeBFast},
		},
	})
	svcA.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5}
	svcB.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5}

	require.NoError(t, svcA.initializeEntries(context.Background(), time.Now()))
	require.NoError(t, svcB.initializeEntries(context.Background(), time.Now()))

	require.Equal(t, int64(1), svcA.getBestProxy(context.Background()).ID)
	require.Equal(t, int64(2), svcB.getBestProxy(context.Background()).ID)
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
	svc.currentProxyID = ptrProxyInt64(7)

	finishedAt := time.Now()
	svc.finishProbe(7, proxyAutoProbeOutcome{Success: false, QualityStatus: "failed"}, finishedAt)
	require.Nil(t, svc.currentProxyID)
	require.Equal(t, ProxyAutoProbeQueueFailed, svc.entries[7].Queue)
	require.Equal(t, finishedAt.Add(5*time.Second), svc.entries[7].NextDueAt)
	status := svc.GetStatus()
	require.Len(t, status.RecentCompletions, 1)
	require.Equal(t, int64(1), status.RecentCompletions[0].Seq)
	require.Equal(t, int64(7), status.RecentCompletions[0].ProxyID)
	require.False(t, status.RecentCompletions[0].Success)
	require.Equal(t, "failed", status.RecentCompletions[0].QualityStatus)

	latency := int64(18)
	svc.currentProxyID = ptrProxyInt64(7)
	svc.finishProbe(7, proxyAutoProbeOutcome{Success: true, QualityStatus: "healthy", LatencyMs: &latency}, finishedAt)
	require.Equal(t, ProxyAutoProbeQueueSuccess, svc.entries[7].Queue)
	require.Equal(t, finishedAt.Add(60*time.Second), svc.entries[7].NextDueAt)
	require.NotNil(t, svc.entries[7].LastLatencyMs)
	require.Equal(t, latency, *svc.entries[7].LastLatencyMs)
	status = svc.GetStatus()
	require.Len(t, status.RecentCompletions, 2)
	require.Equal(t, int64(2), status.RecentCompletions[1].Seq)
	require.Equal(t, int64(7), status.RecentCompletions[1].ProxyID)
	require.True(t, status.RecentCompletions[1].Success)
	require.Equal(t, "healthy", status.RecentCompletions[1].QualityStatus)
}

func TestProxyAutoProbeStickyKeepsAccountOnSameProxyWhenFasterProxyAppears(t *testing.T) {
	latency10 := int64(10)
	latency50 := int64(50)
	store := &proxyStickyStoreStub{}
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{
		{ID: 1, Name: "p1", Status: StatusActive},
		{ID: 2, Name: "p2", Status: StatusActive},
	}}
	svc := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, nil, store)
	defer svc.Stop()
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5, StickyEnabled: true, StickyTTLSeconds: 60}
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency10}
	svc.entries[2] = &proxyAutoProbeEntry{ProxyID: 2, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency50}
	seedProxyAutoProbeSnapshots(svc, repo.proxies...)
	account := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}}

	first := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, first)
	require.Equal(t, int64(1), first.ID)

	fasterLatency := int64(1)
	svc.entries[2].LastLatencyMs = &fasterLatency
	second := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, second)
	require.Equal(t, int64(1), second.ID)
}

func TestProxyAutoProbeStickySwitchesWhenStickyProxyFails(t *testing.T) {
	latency10 := int64(10)
	latency50 := int64(50)
	store := &proxyStickyStoreStub{}
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{
		{ID: 1, Name: "p1", Status: StatusActive},
		{ID: 2, Name: "p2", Status: StatusActive},
	}}
	svc := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, nil, store)
	defer svc.Stop()
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5, StickyEnabled: true, StickyTTLSeconds: 60}
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency10}
	svc.entries[2] = &proxyAutoProbeEntry{ProxyID: 2, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency50}
	seedProxyAutoProbeSnapshots(svc, repo.proxies...)
	account := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}}

	first := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, first)
	require.Equal(t, int64(1), first.ID)

	svc.finishProbe(1, proxyAutoProbeOutcome{Success: false, QualityStatus: "failed"}, time.Now())
	second := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, second)
	require.Equal(t, int64(2), second.ID)
}

func TestProxyAutoProbeStickyStoreErrorFallsBackToCurrentBest(t *testing.T) {
	latency10 := int64(10)
	latency50 := int64(50)
	store := &proxyStickyStoreStub{getErr: errors.New("redis unavailable")}
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{
		{ID: 1, Name: "p1", Status: StatusActive},
		{ID: 2, Name: "p2", Status: StatusActive},
	}}
	svc := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, nil, store)
	defer svc.Stop()
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5, StickyEnabled: true, StickyTTLSeconds: 60}
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency10}
	svc.entries[2] = &proxyAutoProbeEntry{ProxyID: 2, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency50}
	seedProxyAutoProbeSnapshots(svc, repo.proxies...)

	account := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}}
	selected := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, selected)
	require.Equal(t, int64(1), selected.ID)

	fasterLatency := int64(1)
	svc.entries[2].LastLatencyMs = &fasterLatency
	selected = svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.ID)
}

func TestAccountEffectiveProxyIDPrefersRuntimeProxyForAutoSelectAccount(t *testing.T) {
	staleProxyID := int64(1)
	account := &Account{
		ID:      100,
		ProxyID: &staleProxyID,
		Proxy:   &Proxy{ID: 2, Status: StatusActive},
		Extra:   map[string]any{"auto_select_proxy": true},
	}

	effectiveProxyID := account.EffectiveProxyID()
	require.NotNil(t, effectiveProxyID)
	require.Equal(t, int64(2), *effectiveProxyID)

	account.Extra = nil
	effectiveProxyID = account.EffectiveProxyID()
	require.NotNil(t, effectiveProxyID)
	require.Equal(t, int64(1), *effectiveProxyID)
}

func TestClearAutoSelectedProxyStickyOnTransportErrorDeletesBindingForNextRequest(t *testing.T) {
	latency10 := int64(10)
	latency50 := int64(50)
	store := &proxyStickyStoreStub{}
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{
		{ID: 1, Name: "p1", Status: StatusActive},
		{ID: 2, Name: "p2", Status: StatusActive},
	}}
	svc := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, nil, store)
	defer svc.Stop()
	SetDefaultProxyAutoProbeService(svc)
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5, StickyEnabled: true, StickyTTLSeconds: 60}
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency10}
	svc.entries[2] = &proxyAutoProbeEntry{ProxyID: 2, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency50}
	seedProxyAutoProbeSnapshots(svc, repo.proxies...)
	account := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}}

	first := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, first)
	account.Proxy = first
	ClearAutoSelectedProxyStickyOnTransportError(context.Background(), account, errors.New("dial tcp failed"))

	second := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, second)
	require.Equal(t, int64(2), second.ID)
}

func TestClearAutoSelectedProxyStickyFallsBackToOnlyAvailableProxy(t *testing.T) {
	latency10 := int64(10)
	store := &proxyStickyStoreStub{}
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{{ID: 1, Name: "p1", Status: StatusActive}}}
	svc := NewProxyAutoProbeService(nil, repo, &proxyAutoProbeSettingRepoStub{}, nil, store)
	defer svc.Stop()
	SetDefaultProxyAutoProbeService(svc)
	svc.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5, StickyEnabled: true, StickyTTLSeconds: 60}
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency10}
	seedProxyAutoProbeSnapshots(svc, repo.proxies...)
	account := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}}

	first := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, first)
	account.Proxy = first
	ClearAutoSelectedProxyStickyOnTransportError(context.Background(), account, errors.New("dial tcp failed"))

	second := svc.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, second)
	require.Equal(t, int64(1), second.ID)
}

func TestIsAutoProbeSuccessStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		success bool
		want    bool
	}{
		{name: "healthy success", status: "healthy", success: true, want: true},
		{name: "warn success", status: "warn", success: true, want: true},
		{name: "challenge failed", status: "challenge", success: true, want: false},
		{name: "failed failed", status: "failed", success: true, want: false},
		{name: "success flag wins over healthy", status: "healthy", success: false, want: false},
		{name: "unknown preserves success flag", status: "", success: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isAutoProbeSuccessStatus(tt.status, tt.success))
		})
	}
}

func TestClassifyAutoProbeQueueFromQuality_OpenAIStatusRules(t *testing.T) {
	tests := []struct {
		name   string
		result *ProxyQualityCheckResult
		want   string
	}{
		{
			name: "openai pass wins even if others fail",
			result: &ProxyQualityCheckResult{
				Items: []ProxyQualityCheckItem{
					{Target: "anthropic", Status: "fail"},
					{Target: "openai", Status: "pass"},
				},
			},
			want: "healthy",
		},
		{
			name: "openai warn is success queue",
			result: &ProxyQualityCheckResult{
				Items: []ProxyQualityCheckItem{
					{Target: "gemini", Status: "fail"},
					{Target: "openai", Status: "warn"},
				},
			},
			want: "warn",
		},
		{
			name: "openai fail is failed queue",
			result: &ProxyQualityCheckResult{
				Items: []ProxyQualityCheckItem{
					{Target: "anthropic", Status: "pass"},
					{Target: "openai", Status: "fail"},
				},
			},
			want: "failed",
		},
		{
			name: "openai challenge is failed queue",
			result: &ProxyQualityCheckResult{
				Items: []ProxyQualityCheckItem{
					{Target: "gemini", Status: "pass"},
					{Target: "openai", Status: "challenge"},
				},
			},
			want: "challenge",
		},
		{
			name: "missing openai item is failed queue",
			result: &ProxyQualityCheckResult{
				Items: []ProxyQualityCheckItem{
					{Target: "anthropic", Status: "pass"},
				},
			},
			want: "failed",
		},
		{
			name:   "nil result is failed queue",
			result: nil,
			want:   "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyAutoProbeQueueFromQuality(tt.result))
		})
	}
}

func ptrProxyInt64(v int64) *int64 {
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
