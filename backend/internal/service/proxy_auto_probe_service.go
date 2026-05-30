package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	defaultProxyAutoProbeIntervalSec = 60
	defaultProxyAutoProbeRetrySec    = 5
	defaultProxyStickyTTLSeconds     = 604800
	proxyAutoProbeTickInterval       = time.Second
	proxyAutoProbeReconcileInterval  = 10 * time.Second
	proxyAutoProbePageSize           = 200
	proxyAutoProbeRunTimeout         = 45 * time.Second
	proxyAutoProbeCompletionRingSize = 32
	proxyStickyCacheTTL              = 30 * time.Second
	proxyStickyRejectedProxyTTL      = 60 * time.Second
	proxyStickyOperationTimeout      = 100 * time.Millisecond
	proxyStickyWriteQueueSize        = 1024
)

const (
	ProxyAutoProbeQueueSuccess = "success"
	ProxyAutoProbeQueueFailed  = "failed"
)

type ProxyAutoProbeConfig struct {
	Enabled            bool `json:"enabled"`
	DefaultIntervalSec int  `json:"default_interval_sec"`
	RetryIntervalSec   int  `json:"retry_interval_sec"`
	StickyEnabled      bool `json:"sticky_enabled"`
	StickyTTLSeconds   int  `json:"sticky_ttl_seconds"`
}

type ProxyAutoProbeStatus struct {
	NodeID             string                     `json:"node_id,omitempty"`
	Enabled            bool                       `json:"enabled"`
	DefaultIntervalSec int                        `json:"default_interval_sec"`
	RetryIntervalSec   int                        `json:"retry_interval_sec"`
	StickyEnabled      bool                       `json:"sticky_enabled"`
	StickyTTLSeconds   int                        `json:"sticky_ttl_seconds"`
	Running            bool                       `json:"running"`
	SuccessQueueCount  int                        `json:"success_queue_count"`
	FailedQueueCount   int                        `json:"failed_queue_count"`
	CurrentProxyID     *int64                     `json:"current_proxy_id,omitempty"`
	RecentCompletions  []ProxyAutoProbeCompletion `json:"recent_completions"`
}

type ProxyAutoProbeCompletion struct {
	Seq           int64     `json:"seq"`
	ProxyID       int64     `json:"proxy_id"`
	FinishedAt    time.Time `json:"finished_at"`
	Success       bool      `json:"success"`
	QualityStatus string    `json:"quality_status"`
}

type ProxyAutoProbeUpdateInput struct {
	Enabled            bool `json:"enabled"`
	DefaultIntervalSec int  `json:"default_interval_sec"`
	RetryIntervalSec   int  `json:"retry_interval_sec"`
	StickyEnabled      *bool
	StickyTTLSeconds   int `json:"sticky_ttl_seconds"`
}

type proxyAutoProbeEntry struct {
	ProxyID       int64
	Queue         string
	NextDueAt     time.Time
	LastLatencyMs *int64
}

type proxyAutoProbeOutcome struct {
	Success       bool
	LatencyMs     *int64
	QualityStatus string
}

type proxyStickyCacheEntry struct {
	ProxyID   int64
	ExpiresAt time.Time
}

type proxyStickyWriteTask struct {
	fn func(context.Context)
}

type ProxyAutoProbeService struct {
	adminService      AdminService
	proxyRepo         ProxyRepository
	settingRepo       SettingRepository
	proxyLatencyCache ProxyLatencyCache
	proxyStickyStore  ProxyStickyStore
	nodeID            string
	tickInterval      time.Duration

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	mu              sync.RWMutex
	config          ProxyAutoProbeConfig
	running         bool
	currentProxyID  *int64
	lastReconcileAt time.Time
	entries         map[int64]*proxyAutoProbeEntry
	proxySnapshots  map[int64]Proxy
	completionSeq   int64
	completions     []ProxyAutoProbeCompletion

	stickyMu      sync.Mutex
	stickyCache   map[int64]proxyStickyCacheEntry
	stickyRejects map[int64]proxyStickyCacheEntry
	stickyWriteCh chan proxyStickyWriteTask
}

var (
	defaultProxyAutoProbeServiceMu sync.RWMutex
	defaultProxyAutoProbeService   *ProxyAutoProbeService
)

func NewProxyAutoProbeService(
	adminService AdminService,
	proxyRepo ProxyRepository,
	settingRepo SettingRepository,
	proxyLatencyCache ProxyLatencyCache,
	proxyStickyStores ...ProxyStickyStore,
) *ProxyAutoProbeService {
	var proxyStickyStore ProxyStickyStore
	if len(proxyStickyStores) > 0 {
		proxyStickyStore = proxyStickyStores[0]
	}
	svc := &ProxyAutoProbeService{
		adminService:      adminService,
		proxyRepo:         proxyRepo,
		settingRepo:       settingRepo,
		proxyLatencyCache: proxyLatencyCache,
		proxyStickyStore:  proxyStickyStore,
		nodeID:            CurrentNodeID(),
		tickInterval:      proxyAutoProbeTickInterval,
		stopCh:            make(chan struct{}),
		config:            defaultProxyAutoProbeConfig(),
		entries:           make(map[int64]*proxyAutoProbeEntry),
		proxySnapshots:    make(map[int64]Proxy),
		stickyCache:       make(map[int64]proxyStickyCacheEntry),
		stickyRejects:     make(map[int64]proxyStickyCacheEntry),
	}
	if proxyStickyStore != nil {
		svc.stickyWriteCh = make(chan proxyStickyWriteTask, proxyStickyWriteQueueSize)
		svc.wg.Add(1)
		go svc.runProxyStickyWriteWorker()
	}
	return svc
}

func SetDefaultProxyAutoProbeService(svc *ProxyAutoProbeService) {
	defaultProxyAutoProbeServiceMu.Lock()
	defer defaultProxyAutoProbeServiceMu.Unlock()
	defaultProxyAutoProbeService = svc
}

func GetDefaultProxyAutoProbeService() *ProxyAutoProbeService {
	defaultProxyAutoProbeServiceMu.RLock()
	defer defaultProxyAutoProbeServiceMu.RUnlock()
	return defaultProxyAutoProbeService
}

func defaultProxyAutoProbeConfig() ProxyAutoProbeConfig {
	return ProxyAutoProbeConfig{
		Enabled:            false,
		DefaultIntervalSec: defaultProxyAutoProbeIntervalSec,
		RetryIntervalSec:   defaultProxyAutoProbeRetrySec,
		StickyEnabled:      true,
		StickyTTLSeconds:   defaultProxyStickyTTLSeconds,
	}
}

func normalizeProxyAutoProbeConfig(cfg ProxyAutoProbeConfig) ProxyAutoProbeConfig {
	if cfg.DefaultIntervalSec < 1 {
		cfg.DefaultIntervalSec = defaultProxyAutoProbeIntervalSec
	}
	if cfg.RetryIntervalSec < 1 {
		cfg.RetryIntervalSec = defaultProxyAutoProbeRetrySec
	}
	if cfg.StickyTTLSeconds < 1 {
		cfg.StickyTTLSeconds = defaultProxyStickyTTLSeconds
	}
	return cfg
}

func (s *ProxyAutoProbeService) Start() {
	if s == nil || s.settingRepo == nil || s.proxyRepo == nil || s.adminService == nil {
		return
	}
	SetDefaultProxyAutoProbeService(s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cfg, err := s.loadConfig(ctx)
	cancel()
	if err != nil {
		logger.LegacyPrintf("service.proxy_auto_probe", "[ProxyAutoProbe] load config failed at startup: %v", err)
		cfg = defaultProxyAutoProbeConfig()
	}

	s.mu.Lock()
	s.config = cfg
	s.running = cfg.Enabled
	s.mu.Unlock()

	if cfg.Enabled {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.initializeEntries(ctx, time.Now()); err != nil {
			logger.LegacyPrintf("service.proxy_auto_probe", "[ProxyAutoProbe] initialize entries failed: %v", err)
		}
		cancel()
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.tickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.runTick()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *ProxyAutoProbeService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
	s.mu.Lock()
	s.running = false
	s.currentProxyID = nil
	s.entries = make(map[int64]*proxyAutoProbeEntry)
	s.proxySnapshots = make(map[int64]Proxy)
	s.completionSeq = 0
	s.completions = nil
	s.mu.Unlock()
	SetDefaultProxyAutoProbeService(nil)
}

func (s *ProxyAutoProbeService) GetStatus() ProxyAutoProbeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg := normalizeProxyAutoProbeConfig(s.config)
	successCount := 0
	failedCount := 0
	for _, entry := range s.entries {
		switch entry.Queue {
		case ProxyAutoProbeQueueSuccess:
			successCount++
		case ProxyAutoProbeQueueFailed:
			failedCount++
		}
	}

	status := ProxyAutoProbeStatus{
		NodeID:             s.nodeID,
		Enabled:            cfg.Enabled,
		DefaultIntervalSec: cfg.DefaultIntervalSec,
		RetryIntervalSec:   cfg.RetryIntervalSec,
		StickyEnabled:      cfg.StickyEnabled,
		StickyTTLSeconds:   cfg.StickyTTLSeconds,
		Running:            s.running,
		SuccessQueueCount:  successCount,
		FailedQueueCount:   failedCount,
		RecentCompletions:  append([]ProxyAutoProbeCompletion(nil), s.completions...),
	}
	if status.RecentCompletions == nil {
		status.RecentCompletions = []ProxyAutoProbeCompletion{}
	}
	if s.currentProxyID != nil {
		current := *s.currentProxyID
		status.CurrentProxyID = &current
	}
	return status
}

func applyAutoSelectedProxy(ctx context.Context, account *Account) *Account {
	if account == nil || !account.IsAutoSelectProxyEnabled() {
		return account
	}
	svc := GetDefaultProxyAutoProbeService()
	if svc == nil {
		account.Proxy = nil
		return account
	}
	best := svc.selectProxyForAccount(ctx, account)
	account.Proxy = best
	return account
}

func resolveAccountProxy(ctx context.Context, account *Account, proxyRepo ProxyRepository) *Proxy {
	if account == nil {
		return nil
	}
	if account.IsAutoSelectProxyEnabled() {
		if account.Proxy != nil {
			return account.Proxy
		}
		svc := GetDefaultProxyAutoProbeService()
		if svc != nil {
			selected := svc.selectProxyForAccount(ctx, account)
			account.Proxy = selected
			return selected
		}
		return nil
	}
	if account.Proxy != nil {
		return account.Proxy
	}
	if account.ProxyID != nil && proxyRepo != nil {
		proxy, err := proxyRepo.GetByID(ctx, *account.ProxyID)
		if err == nil && proxy != nil {
			return proxy
		}
	}
	return nil
}

func resolveAccountProxyURL(ctx context.Context, account *Account, proxyRepo ProxyRepository) string {
	proxy := resolveAccountProxy(ctx, account, proxyRepo)
	if proxy == nil {
		return ""
	}
	return ResolveProxyURL(ctx, proxy)
}

func ClearAutoSelectedProxyStickyOnTransportError(ctx context.Context, account *Account, err error) {
	if err == nil || account == nil || !account.IsAutoSelectProxyEnabled() || account.ID <= 0 {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	proxyID := account.EffectiveProxyID()
	if proxyID == nil || *proxyID <= 0 {
		return
	}
	svc := GetDefaultProxyAutoProbeService()
	if svc == nil {
		return
	}
	svc.ClearStickyProxy(ctx, account.ID, *proxyID)
}

func (s *ProxyAutoProbeService) UpdateConfig(ctx context.Context, input *ProxyAutoProbeUpdateInput) (ProxyAutoProbeStatus, error) {
	if input == nil {
		return ProxyAutoProbeStatus{}, errors.New("config is required")
	}
	if input.DefaultIntervalSec < 1 {
		return ProxyAutoProbeStatus{}, errors.New("default_interval_sec must be >= 1")
	}
	if input.RetryIntervalSec < 1 {
		return ProxyAutoProbeStatus{}, errors.New("retry_interval_sec must be >= 1")
	}
	if input.StickyTTLSeconds < 0 {
		return ProxyAutoProbeStatus{}, errors.New("sticky_ttl_seconds must be >= 1")
	}

	currentCfg := s.snapshotConfig()
	stickyEnabled := currentCfg.StickyEnabled
	if input.StickyEnabled != nil {
		stickyEnabled = *input.StickyEnabled
	}
	stickyTTLSeconds := input.StickyTTLSeconds
	if stickyTTLSeconds == 0 {
		stickyTTLSeconds = currentCfg.StickyTTLSeconds
	}

	cfg := ProxyAutoProbeConfig{
		Enabled:            input.Enabled,
		DefaultIntervalSec: input.DefaultIntervalSec,
		RetryIntervalSec:   input.RetryIntervalSec,
		StickyEnabled:      stickyEnabled,
		StickyTTLSeconds:   stickyTTLSeconds,
	}

	payload, err := json.Marshal(cfg)
	if err != nil {
		return ProxyAutoProbeStatus{}, fmt.Errorf("marshal proxy auto probe config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyProxyAutoProbeConfig, string(payload)); err != nil {
		return ProxyAutoProbeStatus{}, fmt.Errorf("save proxy auto probe config: %w", err)
	}

	now := time.Now()

	s.mu.Lock()
	prevEnabled := s.config.Enabled
	s.config = cfg
	s.running = cfg.Enabled
	if !cfg.Enabled {
		s.currentProxyID = nil
		s.entries = make(map[int64]*proxyAutoProbeEntry)
		s.mu.Unlock()
		return s.GetStatus(), nil
	}

	if prevEnabled {
		for _, entry := range s.entries {
			entry.NextDueAt = now.Add(s.intervalForQueueLocked(entry.Queue))
		}
	}
	s.mu.Unlock()

	if !prevEnabled {
		if err := s.initializeEntries(ctx, now); err != nil {
			return ProxyAutoProbeStatus{}, err
		}
	} else if err := s.reconcileEntries(ctx, now); err != nil {
		logger.LegacyPrintf("service.proxy_auto_probe", "[ProxyAutoProbe] reconcile after config update failed: %v", err)
	}

	return s.GetStatus(), nil
}

func (s *ProxyAutoProbeService) runTick() {
	cfg := s.snapshotConfig()
	if !cfg.Enabled {
		s.mu.Lock()
		s.running = false
		s.currentProxyID = nil
		s.mu.Unlock()
		return
	}

	now := time.Now()
	if s.shouldReconcile(now) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.reconcileEntries(ctx, now); err != nil {
			logger.LegacyPrintf("service.proxy_auto_probe", "[ProxyAutoProbe] reconcile entries failed: %v", err)
		}
		cancel()
	}

	proxyID, ok := s.acquireDueProxy(now)
	if !ok {
		return
	}

	probeCtx, probeCancel := context.WithTimeout(context.Background(), proxyAutoProbeRunTimeout)
	outcome := s.probeProxy(probeCtx, proxyID)
	probeCancel()
	s.finishProbe(proxyID, outcome, time.Now())
}

func (s *ProxyAutoProbeService) snapshotConfig() ProxyAutoProbeConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeProxyAutoProbeConfig(s.config)
}

func (s *ProxyAutoProbeService) loadConfig(ctx context.Context) (ProxyAutoProbeConfig, error) {
	cfg := defaultProxyAutoProbeConfig()
	if s == nil || s.settingRepo == nil {
		return cfg, nil
	}

	raw, err := s.settingRepo.GetValue(ctx, SettingKeyProxyAutoProbeConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return cfg, nil
		}
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultProxyAutoProbeConfig(), nil
	}
	cfg = applyProxyAutoProbeConfigCompatibility(raw, cfg)
	return normalizeProxyAutoProbeConfig(cfg), nil
}

func applyProxyAutoProbeConfigCompatibility(raw string, cfg ProxyAutoProbeConfig) ProxyAutoProbeConfig {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return cfg
	}
	if _, ok := fields["sticky_enabled"]; !ok {
		cfg.StickyEnabled = true
	}
	if _, ok := fields["sticky_ttl_seconds"]; !ok {
		cfg.StickyTTLSeconds = defaultProxyStickyTTLSeconds
	}
	return cfg
}

func (s *ProxyAutoProbeService) initializeEntries(ctx context.Context, now time.Time) error {
	proxies, err := s.listAllProxies(ctx)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(proxies))
	for i := range proxies {
		ids = append(ids, proxies[i].ID)
	}

	latencies := map[int64]*ProxyLatencyInfo{}
	if s.proxyLatencyCache != nil && len(ids) > 0 {
		latencies, err = s.proxyLatencyCache.GetProxyLatencies(ctx, ids)
		if err != nil {
			logger.LegacyPrintf("service.proxy_auto_probe", "[ProxyAutoProbe] load latency cache failed: %v", err)
			latencies = map[int64]*ProxyLatencyInfo{}
		}
	}

	cfg := s.snapshotConfig()
	entries := make(map[int64]*proxyAutoProbeEntry, len(proxies))
	proxySnapshots := make(map[int64]Proxy, len(proxies))
	for i := range proxies {
		proxy := proxies[i]
		proxySnapshots[proxy.ID] = proxy
		queue := ProxyAutoProbeQueueSuccess
		var latency *int64
		if info := latencies[proxy.ID]; info != nil {
			latency = info.LatencyMs
			if isAutoProbeSuccessStatus(info.QualityStatus, info.Success) {
				queue = ProxyAutoProbeQueueSuccess
			} else {
				queue = ProxyAutoProbeQueueFailed
			}
		}

		nextDueAt := now.Add(queueInterval(cfg, queue))
		entries[proxy.ID] = &proxyAutoProbeEntry{
			ProxyID:       proxy.ID,
			Queue:         queue,
			NextDueAt:     nextDueAt,
			LastLatencyMs: latency,
		}
	}

	s.mu.Lock()
	s.entries = entries
	s.proxySnapshots = proxySnapshots
	s.running = cfg.Enabled
	s.currentProxyID = nil
	s.lastReconcileAt = now
	s.mu.Unlock()
	return nil
}

func (s *ProxyAutoProbeService) reconcileEntries(ctx context.Context, now time.Time) error {
	proxies, err := s.listAllProxies(ctx)
	if err != nil {
		return err
	}
	currentIDs := make(map[int64]struct{}, len(proxies))
	cfg := s.snapshotConfig()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.entries == nil {
		s.entries = make(map[int64]*proxyAutoProbeEntry)
	}
	if s.proxySnapshots == nil {
		s.proxySnapshots = make(map[int64]Proxy)
	}

	for i := range proxies {
		proxy := proxies[i]
		id := proxy.ID
		currentIDs[id] = struct{}{}
		s.proxySnapshots[id] = proxy
		if _, ok := s.entries[id]; ok {
			continue
		}
		s.entries[id] = &proxyAutoProbeEntry{
			ProxyID:   id,
			Queue:     ProxyAutoProbeQueueSuccess,
			NextDueAt: now.Add(queueInterval(cfg, ProxyAutoProbeQueueSuccess)),
		}
	}

	for id := range s.entries {
		if _, ok := currentIDs[id]; ok {
			continue
		}
		delete(s.entries, id)
		delete(s.proxySnapshots, id)
		if s.currentProxyID != nil && *s.currentProxyID == id {
			s.currentProxyID = nil
		}
	}

	s.lastReconcileAt = now
	return nil
}

func (s *ProxyAutoProbeService) shouldReconcile(now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastReconcileAt.IsZero() || now.Sub(s.lastReconcileAt) >= proxyAutoProbeReconcileInterval
}

func (s *ProxyAutoProbeService) listAllProxies(ctx context.Context) ([]Proxy, error) {
	page := 1
	all := make([]Proxy, 0)
	for {
		items, result, err := s.proxyRepo.List(ctx, pagination.PaginationParams{
			Page:      page,
			PageSize:  proxyAutoProbePageSize,
			SortBy:    "id",
			SortOrder: "asc",
		})
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) == 0 || result == nil || int64(len(all)) >= result.Total {
			break
		}
		page++
	}
	return all, nil
}

func (s *ProxyAutoProbeService) selectProxyForAccount(ctx context.Context, account *Account) *Proxy {
	if s == nil || account == nil || account.ID <= 0 {
		return s.getBestProxy(ctx)
	}
	cfg := s.snapshotConfig()
	if !cfg.StickyEnabled || s.proxyStickyStore == nil {
		return s.getBestProxy(ctx)
	}

	ttl := proxyStickyTTL(cfg)
	rejectedProxyID, hasRejectedProxy := s.getProxyStickyReject(account.ID)
	if proxyID, ok := s.getProxyStickyCache(account.ID); ok {
		if hasRejectedProxy && proxyID == rejectedProxyID {
			s.deleteProxyStickyCacheIfMatch(account.ID, proxyID)
			s.deleteStickyProxy(account.ID, proxyID)
		} else if proxy := s.getUsableSuccessProxy(ctx, proxyID); proxy != nil {
			s.setProxyStickyCache(account.ID, proxyID, ttl)
			s.refreshStickyProxy(account.ID, ttl)
			return proxy
		} else {
			s.deleteProxyStickyCacheIfMatch(account.ID, proxyID)
			s.deleteStickyProxy(account.ID, proxyID)
		}
	}

	if proxyID, ok, err := s.getStickyProxyFromStore(ctx, account.ID); err != nil {
		s.deleteProxyStickyCache(account.ID)
		return s.getBestProxy(ctx)
	} else if ok && proxyID > 0 {
		if hasRejectedProxy && proxyID == rejectedProxyID {
			s.deleteStickyProxy(account.ID, proxyID)
		} else if proxy := s.getUsableSuccessProxy(ctx, proxyID); proxy != nil {
			s.setProxyStickyCache(account.ID, proxyID, ttl)
			s.refreshStickyProxy(account.ID, ttl)
			return proxy
		} else {
			s.deleteStickyProxy(account.ID, proxyID)
		}
	}

	best := s.getBestProxyExcluding(ctx, rejectedProxyID)
	if best == nil && hasRejectedProxy {
		best = s.getBestProxy(ctx)
	}
	if best != nil && best.ID > 0 {
		s.setProxyStickyCache(account.ID, best.ID, ttl)
		s.setStickyProxy(account.ID, best.ID, ttl)
	}
	return best
}

func (s *ProxyAutoProbeService) ClearStickyProxy(ctx context.Context, accountID, proxyID int64) {
	if s == nil || accountID <= 0 || proxyID <= 0 {
		return
	}
	s.deleteProxyStickyCacheIfMatch(accountID, proxyID)
	s.setProxyStickyReject(accountID, proxyID)
	s.deleteStickyProxy(accountID, proxyID)
}

func (s *ProxyAutoProbeService) getStickyProxyFromStore(ctx context.Context, accountID int64) (int64, bool, error) {
	if s == nil || s.proxyStickyStore == nil || accountID <= 0 {
		return 0, false, nil
	}
	stickyCtx, cancel := proxyStickyContext(ctx)
	defer cancel()
	proxyID, ok, err := s.proxyStickyStore.Get(stickyCtx, accountID)
	if err != nil || !ok || proxyID <= 0 {
		return 0, false, err
	}
	return proxyID, true, nil
}

func (s *ProxyAutoProbeService) getUsableSuccessProxy(ctx context.Context, proxyID int64) *Proxy {
	if s == nil || proxyID <= 0 || !s.isProxyInSuccessQueue(proxyID) {
		return nil
	}
	proxy, ok := s.getProxySnapshot(proxyID)
	if !ok || !proxy.IsActive() {
		return nil
	}
	return &proxy
}

func (s *ProxyAutoProbeService) isProxyInSuccessQueue(proxyID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry := s.entries[proxyID]
	return entry != nil && entry.Queue == ProxyAutoProbeQueueSuccess
}

func (s *ProxyAutoProbeService) getProxyStickyCache(accountID int64) (int64, bool) {
	s.stickyMu.Lock()
	defer s.stickyMu.Unlock()
	entry, ok := s.stickyCache[accountID]
	if !ok {
		return 0, false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(s.stickyCache, accountID)
		return 0, false
	}
	if entry.ProxyID <= 0 {
		return 0, false
	}
	return entry.ProxyID, true
}

func (s *ProxyAutoProbeService) setProxyStickyCache(accountID, proxyID int64, ttl ...time.Duration) {
	if s == nil || accountID <= 0 || proxyID <= 0 {
		return
	}
	s.stickyMu.Lock()
	defer s.stickyMu.Unlock()
	if s.stickyCache == nil {
		s.stickyCache = make(map[int64]proxyStickyCacheEntry)
	}
	s.stickyCache[accountID] = proxyStickyCacheEntry{
		ProxyID:   proxyID,
		ExpiresAt: time.Now().Add(proxyStickyCacheTTL),
	}
}

func (s *ProxyAutoProbeService) deleteProxyStickyCacheIfMatch(accountID, proxyID int64) {
	if s == nil || accountID <= 0 || proxyID <= 0 {
		return
	}
	s.stickyMu.Lock()
	defer s.stickyMu.Unlock()
	entry, ok := s.stickyCache[accountID]
	if ok && entry.ProxyID == proxyID {
		delete(s.stickyCache, accountID)
	}
}

func (s *ProxyAutoProbeService) deleteProxyStickyCache(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.stickyMu.Lock()
	defer s.stickyMu.Unlock()
	delete(s.stickyCache, accountID)
}

func (s *ProxyAutoProbeService) getProxyStickyReject(accountID int64) (int64, bool) {
	if s == nil || accountID <= 0 {
		return 0, false
	}
	s.stickyMu.Lock()
	defer s.stickyMu.Unlock()
	entry, ok := s.stickyRejects[accountID]
	if !ok {
		return 0, false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(s.stickyRejects, accountID)
		return 0, false
	}
	if entry.ProxyID <= 0 {
		return 0, false
	}
	return entry.ProxyID, true
}

func (s *ProxyAutoProbeService) setProxyStickyReject(accountID, proxyID int64) {
	if s == nil || accountID <= 0 || proxyID <= 0 {
		return
	}
	s.stickyMu.Lock()
	defer s.stickyMu.Unlock()
	if s.stickyRejects == nil {
		s.stickyRejects = make(map[int64]proxyStickyCacheEntry)
	}
	s.stickyRejects[accountID] = proxyStickyCacheEntry{
		ProxyID:   proxyID,
		ExpiresAt: time.Now().Add(proxyStickyRejectedProxyTTL),
	}
}

func (s *ProxyAutoProbeService) setStickyProxy(accountID, proxyID int64, ttl time.Duration) {
	s.submitProxyStickyWrite(func(ctx context.Context) {
		_ = s.proxyStickyStore.Set(ctx, accountID, proxyID, ttl)
	})
}

func (s *ProxyAutoProbeService) refreshStickyProxy(accountID int64, ttl time.Duration) {
	s.submitProxyStickyWrite(func(ctx context.Context) {
		_ = s.proxyStickyStore.Refresh(ctx, accountID, ttl)
	})
}

func (s *ProxyAutoProbeService) deleteStickyProxy(accountID, proxyID int64) {
	s.submitProxyStickyWrite(func(ctx context.Context) {
		_ = s.proxyStickyStore.DeleteIfMatch(ctx, accountID, proxyID)
	})
}

func (s *ProxyAutoProbeService) submitProxyStickyWrite(fn func(context.Context)) {
	if s == nil || s.proxyStickyStore == nil || s.stickyWriteCh == nil || fn == nil {
		return
	}
	select {
	case s.stickyWriteCh <- proxyStickyWriteTask{fn: fn}:
	default:
	}
}

func (s *ProxyAutoProbeService) runProxyStickyWriteWorker() {
	defer s.wg.Done()
	for {
		select {
		case task := <-s.stickyWriteCh:
			if task.fn == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), proxyStickyOperationTimeout)
			task.fn(ctx)
			cancel()
		case <-s.stopCh:
			return
		}
	}
}

func proxyStickyContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, proxyStickyOperationTimeout)
}

func proxyStickyTTL(cfg ProxyAutoProbeConfig) time.Duration {
	ttl := cfg.StickyTTLSeconds
	if ttl < 1 {
		ttl = defaultProxyStickyTTLSeconds
	}
	return time.Duration(ttl) * time.Second
}

func (s *ProxyAutoProbeService) getBestProxy(ctx context.Context) *Proxy {
	return s.getBestProxyExcluding(ctx, 0)
}

func (s *ProxyAutoProbeService) getBestProxyExcluding(ctx context.Context, excludedProxyID int64) *Proxy {
	if s == nil {
		return nil
	}

	type proxyCandidate struct {
		Entry *proxyAutoProbeEntry
		Proxy Proxy
	}
	s.mu.RLock()
	candidates := make([]proxyCandidate, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry == nil || entry.Queue != ProxyAutoProbeQueueSuccess {
			continue
		}
		if excludedProxyID > 0 && entry.ProxyID == excludedProxyID {
			continue
		}
		proxy, ok := s.proxySnapshots[entry.ProxyID]
		if !ok || !proxy.IsActive() {
			continue
		}
		candidates = append(candidates, proxyCandidate{
			Entry: &proxyAutoProbeEntry{
				ProxyID:       entry.ProxyID,
				Queue:         entry.Queue,
				NextDueAt:     entry.NextDueAt,
				LastLatencyMs: entry.LastLatencyMs,
			},
			Proxy: proxy,
		})
	}
	s.mu.RUnlock()

	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i].Entry
		right := candidates[j].Entry
		switch {
		case left.LastLatencyMs == nil && right.LastLatencyMs != nil:
			return false
		case left.LastLatencyMs != nil && right.LastLatencyMs == nil:
			return true
		case left.LastLatencyMs != nil && right.LastLatencyMs != nil && *left.LastLatencyMs != *right.LastLatencyMs:
			return *left.LastLatencyMs < *right.LastLatencyMs
		default:
			return left.ProxyID < right.ProxyID
		}
	})

	for _, candidate := range candidates {
		proxy := candidate.Proxy
		return &proxy
	}
	return nil
}

func (s *ProxyAutoProbeService) getProxySnapshot(proxyID int64) (Proxy, bool) {
	if s == nil || proxyID <= 0 {
		return Proxy{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	proxy, ok := s.proxySnapshots[proxyID]
	return proxy, ok
}

func (s *ProxyAutoProbeService) acquireDueProxy(now time.Time) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentProxyID != nil || !s.config.Enabled {
		return 0, false
	}

	candidates := make([]*proxyAutoProbeEntry, 0)
	for _, entry := range s.entries {
		if entry == nil || entry.NextDueAt.After(now) {
			continue
		}
		candidates = append(candidates, entry)
	}
	if len(candidates) == 0 {
		return 0, false
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return proxyAutoProbeEntryLess(candidates[i], candidates[j])
	})

	id := candidates[0].ProxyID
	s.currentProxyID = &id
	s.running = true
	return id, true
}

func proxyAutoProbeEntryLess(left, right *proxyAutoProbeEntry) bool {
	if left == nil || right == nil {
		return left != nil
	}
	if !left.NextDueAt.Equal(right.NextDueAt) {
		return left.NextDueAt.Before(right.NextDueAt)
	}
	if left.Queue != right.Queue {
		return left.Queue == ProxyAutoProbeQueueFailed
	}
	if left.Queue == ProxyAutoProbeQueueSuccess {
		switch {
		case left.LastLatencyMs == nil && right.LastLatencyMs != nil:
			return false
		case left.LastLatencyMs != nil && right.LastLatencyMs == nil:
			return true
		case left.LastLatencyMs != nil && right.LastLatencyMs != nil && *left.LastLatencyMs != *right.LastLatencyMs:
			return *left.LastLatencyMs < *right.LastLatencyMs
		}
	}
	return left.ProxyID < right.ProxyID
}

func (s *ProxyAutoProbeService) probeProxy(ctx context.Context, proxyID int64) proxyAutoProbeOutcome {
	outcome := proxyAutoProbeOutcome{
		Success:       false,
		QualityStatus: "failed",
	}

	testResult, err := s.adminService.TestProxy(ctx, proxyID)
	if err != nil {
		logger.LegacyPrintf("service.proxy_auto_probe", "[ProxyAutoProbe] test proxy failed: proxy=%d err=%v", proxyID, err)
		return outcome
	}
	if testResult == nil || !testResult.Success {
		return outcome
	}
	if testResult.LatencyMs > 0 {
		latency := testResult.LatencyMs
		outcome.LatencyMs = &latency
	}

	qualityResult, err := s.adminService.CheckProxyQuality(ctx, proxyID)
	if err != nil {
		logger.LegacyPrintf("service.proxy_auto_probe", "[ProxyAutoProbe] quality check failed: proxy=%d err=%v", proxyID, err)
		return outcome
	}
	qualityStatus := classifyAutoProbeQueueFromQuality(qualityResult)
	outcome.QualityStatus = qualityStatus
	if qualityResult != nil && qualityResult.BaseLatencyMs > 0 {
		latency := qualityResult.BaseLatencyMs
		outcome.LatencyMs = &latency
	}
	outcome.Success = isAutoProbeSuccessStatus(qualityStatus, true)
	return outcome
}

func isAutoProbeSuccessStatus(qualityStatus string, success bool) bool {
	if !success {
		return false
	}
	switch qualityStatus {
	case "healthy", "warn":
		return true
	case "challenge", "failed":
		return false
	default:
		return success
	}
}

func classifyAutoProbeQueueFromQuality(result *ProxyQualityCheckResult) string {
	if result == nil {
		return "failed"
	}
	for _, item := range result.Items {
		if item.Target != "openai" {
			continue
		}
		switch item.Status {
		case "pass":
			return "healthy"
		case "warn":
			return "warn"
		case "challenge":
			return "challenge"
		default:
			return "failed"
		}
	}
	return "failed"
}

func (s *ProxyAutoProbeService) finishProbe(proxyID int64, outcome proxyAutoProbeOutcome, finishedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentProxyID != nil && *s.currentProxyID == proxyID {
		s.currentProxyID = nil
	}
	s.recordProbeCompletionLocked(proxyID, outcome, finishedAt)

	entry, ok := s.entries[proxyID]
	if !ok || entry == nil {
		return
	}

	if outcome.Success {
		entry.Queue = ProxyAutoProbeQueueSuccess
	} else {
		entry.Queue = ProxyAutoProbeQueueFailed
	}
	entry.LastLatencyMs = outcome.LatencyMs
	entry.NextDueAt = finishedAt.Add(s.intervalForQueueLocked(entry.Queue))
}

func (s *ProxyAutoProbeService) recordProbeCompletionLocked(proxyID int64, outcome proxyAutoProbeOutcome, finishedAt time.Time) {
	s.completionSeq++
	s.completions = append(s.completions, ProxyAutoProbeCompletion{
		Seq:           s.completionSeq,
		ProxyID:       proxyID,
		FinishedAt:    finishedAt,
		Success:       outcome.Success,
		QualityStatus: outcome.QualityStatus,
	})
	if len(s.completions) > proxyAutoProbeCompletionRingSize {
		s.completions = append([]ProxyAutoProbeCompletion(nil), s.completions[len(s.completions)-proxyAutoProbeCompletionRingSize:]...)
	}
}

func (s *ProxyAutoProbeService) intervalForQueueLocked(queue string) time.Duration {
	return queueInterval(normalizeProxyAutoProbeConfig(s.config), queue)
}

func queueInterval(cfg ProxyAutoProbeConfig, queue string) time.Duration {
	sec := cfg.DefaultIntervalSec
	if queue == ProxyAutoProbeQueueFailed {
		sec = cfg.RetryIntervalSec
	}
	if sec < 1 {
		sec = 1
	}
	return time.Duration(sec) * time.Second
}
