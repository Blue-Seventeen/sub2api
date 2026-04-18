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
	proxyAutoProbeTickInterval       = time.Second
	proxyAutoProbeReconcileInterval  = 10 * time.Second
	proxyAutoProbePageSize           = 200
	proxyAutoProbeRunTimeout         = 45 * time.Second
)

const (
	ProxyAutoProbeQueueSuccess = "success"
	ProxyAutoProbeQueueFailed  = "failed"
)

type ProxyAutoProbeConfig struct {
	Enabled            bool `json:"enabled"`
	DefaultIntervalSec int  `json:"default_interval_sec"`
	RetryIntervalSec   int  `json:"retry_interval_sec"`
}

type ProxyAutoProbeStatus struct {
	Enabled            bool   `json:"enabled"`
	DefaultIntervalSec int    `json:"default_interval_sec"`
	RetryIntervalSec   int    `json:"retry_interval_sec"`
	Running            bool   `json:"running"`
	SuccessQueueCount  int    `json:"success_queue_count"`
	FailedQueueCount   int    `json:"failed_queue_count"`
	CurrentProxyID     *int64 `json:"current_proxy_id,omitempty"`
}

type ProxyAutoProbeUpdateInput struct {
	Enabled            bool `json:"enabled"`
	DefaultIntervalSec int  `json:"default_interval_sec"`
	RetryIntervalSec   int  `json:"retry_interval_sec"`
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

type ProxyAutoProbeService struct {
	adminService      AdminService
	proxyRepo         ProxyRepository
	settingRepo       SettingRepository
	proxyLatencyCache ProxyLatencyCache
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
) *ProxyAutoProbeService {
	return &ProxyAutoProbeService{
		adminService:      adminService,
		proxyRepo:         proxyRepo,
		settingRepo:       settingRepo,
		proxyLatencyCache: proxyLatencyCache,
		tickInterval:      proxyAutoProbeTickInterval,
		stopCh:            make(chan struct{}),
		config:            defaultProxyAutoProbeConfig(),
		entries:           make(map[int64]*proxyAutoProbeEntry),
	}
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
	}
}

func normalizeProxyAutoProbeConfig(cfg ProxyAutoProbeConfig) ProxyAutoProbeConfig {
	if cfg.DefaultIntervalSec < 1 {
		cfg.DefaultIntervalSec = defaultProxyAutoProbeIntervalSec
	}
	if cfg.RetryIntervalSec < 1 {
		cfg.RetryIntervalSec = defaultProxyAutoProbeRetrySec
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
		Enabled:            cfg.Enabled,
		DefaultIntervalSec: cfg.DefaultIntervalSec,
		RetryIntervalSec:   cfg.RetryIntervalSec,
		Running:            s.running,
		SuccessQueueCount:  successCount,
		FailedQueueCount:   failedCount,
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
	best := svc.getBestProxy(ctx)
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
			return svc.getBestProxy(ctx)
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
	return proxy.URL()
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

	cfg := ProxyAutoProbeConfig{
		Enabled:            input.Enabled,
		DefaultIntervalSec: input.DefaultIntervalSec,
		RetryIntervalSec:   input.RetryIntervalSec,
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
	return normalizeProxyAutoProbeConfig(cfg), nil
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
	for i := range proxies {
		proxy := proxies[i]
		queue := ProxyAutoProbeQueueSuccess
		var latency *int64
		if info := latencies[proxy.ID]; info != nil {
			latency = info.LatencyMs
			switch {
			case info.QualityStatus == "healthy":
				queue = ProxyAutoProbeQueueSuccess
			case info.QualityStatus == "warn", info.QualityStatus == "challenge", info.QualityStatus == "failed", !info.Success:
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

	for i := range proxies {
		id := proxies[i].ID
		currentIDs[id] = struct{}{}
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

func (s *ProxyAutoProbeService) getBestProxy(ctx context.Context) *Proxy {
	if s == nil || s.proxyRepo == nil {
		return nil
	}

	s.mu.RLock()
	candidates := make([]*proxyAutoProbeEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry == nil || entry.Queue != ProxyAutoProbeQueueSuccess {
			continue
		}
		candidates = append(candidates, &proxyAutoProbeEntry{
			ProxyID:       entry.ProxyID,
			Queue:         entry.Queue,
			NextDueAt:     entry.NextDueAt,
			LastLatencyMs: entry.LastLatencyMs,
		})
	}
	s.mu.RUnlock()

	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
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
		proxy, err := s.proxyRepo.GetByID(ctx, candidate.ProxyID)
		if err != nil || proxy == nil {
			continue
		}
		return proxy
	}
	return nil
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
	qualityStatus := summarizeProxyQualityStatus(qualityResult)
	outcome.QualityStatus = qualityStatus
	if qualityResult != nil && qualityResult.BaseLatencyMs > 0 {
		latency := qualityResult.BaseLatencyMs
		outcome.LatencyMs = &latency
	}
	outcome.Success = qualityStatus == "healthy"
	return outcome
}

func summarizeProxyQualityStatus(result *ProxyQualityCheckResult) string {
	if result == nil {
		return "failed"
	}
	if result.ChallengeCount > 0 {
		return "challenge"
	}
	if result.FailedCount > 0 {
		return "failed"
	}
	if result.WarnCount > 0 {
		return "warn"
	}
	return "healthy"
}

func (s *ProxyAutoProbeService) finishProbe(proxyID int64, outcome proxyAutoProbeOutcome, finishedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentProxyID != nil && *s.currentProxyID == proxyID {
		s.currentProxyID = nil
	}

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
