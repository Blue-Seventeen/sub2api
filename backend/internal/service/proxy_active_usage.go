package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/alitto/pond/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultProxyActiveUsageWorkerCount = 16
	defaultProxyActiveUsageQueueSize   = 8192
	defaultProxyActiveUsageTaskTimeout = 750 * time.Millisecond
	defaultProxyActiveUsageTTL         = 2 * time.Minute
	defaultProxyActiveUsageHeartbeat   = 30 * time.Second
	proxyActiveUsageDropLogInterval    = 5 * time.Second
)

const proxyActiveUsageUpsertScript = `
if redis.call('EXISTS', KEYS[4]) == 1 then
  return 0
end
redis.call('SADD', KEYS[2], ARGV[1])
redis.call('PEXPIRE', KEYS[2], ARGV[2])
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[4])
redis.call('PEXPIRE', KEYS[1], ARGV[5])
redis.call('SET', KEYS[3], ARGV[6], 'PX', ARGV[2])
return 1
`

const proxyActiveUsageRemoveScript = `
redis.call('SET', KEYS[4], '1', 'PX', ARGV[2])
redis.call('DEL', KEYS[3])
redis.call('SREM', KEYS[2], ARGV[1])
local remaining = redis.call('SCARD', KEYS[2])
if remaining <= 0 then
  redis.call('DEL', KEYS[2])
  redis.call('ZREM', KEYS[1], ARGV[4])
else
  redis.call('PEXPIRE', KEYS[2], ARGV[2])
end
return remaining
`

type ProxyActiveUsageEntry struct {
	Token     string
	ProxyID   int64
	AccountID int64
}

type ProxyActiveUsageStore interface {
	UpsertProxyActiveUsage(ctx context.Context, entry ProxyActiveUsageEntry, ttl time.Duration) error
	RemoveProxyActiveUsage(ctx context.Context, entry ProxyActiveUsageEntry, ttl time.Duration) error
	CountProxyActiveAccounts(ctx context.Context, proxyIDs []int64) (map[int64]int64, error)
}

func (e ProxyActiveUsageEntry) Valid() bool {
	return e.Token != "" && e.ProxyID > 0 && e.AccountID > 0
}

type ProxyActiveUsageTrackerOptions struct {
	WorkerCount       int
	QueueSize         int
	TaskTimeout       time.Duration
	TTL               time.Duration
	HeartbeatInterval time.Duration
}

type ProxyActiveUsageTracker struct {
	store             ProxyActiveUsageStore
	pool              pond.Pool
	taskTimeout       time.Duration
	ttl               time.Duration
	heartbeatInterval time.Duration
	inflight          sync.Map
	stopCh            chan struct{}
	heartbeatDone     chan struct{}
	stopped           atomic.Bool
	droppedQueueFull  atomic.Uint64
	droppedStopped    atomic.Uint64
	lastDropLogNanos  atomic.Int64
}

type ProxyActiveUsageHandle struct {
	tracker *ProxyActiveUsageTracker
	token   string
	once    sync.Once
}

func NewProxyActiveUsageTracker(store ProxyActiveUsageStore) *ProxyActiveUsageTracker {
	return NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{})
}

func NewProxyActiveUsageTrackerWithOptions(store ProxyActiveUsageStore, opts ProxyActiveUsageTrackerOptions) *ProxyActiveUsageTracker {
	opts = normalizeProxyActiveUsageTrackerOptions(opts)
	tracker := &ProxyActiveUsageTracker{
		store:             store,
		taskTimeout:       opts.TaskTimeout,
		ttl:               opts.TTL,
		heartbeatInterval: opts.HeartbeatInterval,
		stopCh:            make(chan struct{}),
		heartbeatDone:     make(chan struct{}),
	}
	if store != nil {
		tracker.pool = pond.NewPool(opts.WorkerCount, pond.WithQueueSize(opts.QueueSize))
		go tracker.heartbeatLoop()
	} else {
		close(tracker.heartbeatDone)
	}
	return tracker
}

func (t *ProxyActiveUsageTracker) Begin(account *Account) *ProxyActiveUsageHandle {
	if account == nil || account.ID <= 0 {
		return nil
	}
	proxyID := int64(0)
	if effectiveProxyID := account.EffectiveProxyID(); effectiveProxyID != nil {
		proxyID = *effectiveProxyID
	}
	return t.BeginProxy(proxyID, account.ID)
}

func (t *ProxyActiveUsageTracker) BeginProxy(proxyID, accountID int64) *ProxyActiveUsageHandle {
	if t == nil || t.store == nil || proxyID <= 0 || accountID <= 0 || t.stopped.Load() {
		return nil
	}
	entry := ProxyActiveUsageEntry{
		Token:     uuid.NewString(),
		ProxyID:   proxyID,
		AccountID: accountID,
	}
	t.inflight.Store(entry.Token, entry)
	if !t.submit(entry, func(ctx context.Context, current ProxyActiveUsageEntry) {
		if _, ok := t.inflight.Load(current.Token); !ok {
			return
		}
		if err := t.store.UpsertProxyActiveUsage(ctx, current, t.ttl); err != nil {
			t.logTaskError("begin", err, current)
		}
	}) {
		t.inflight.Delete(entry.Token)
		return nil
	}
	return &ProxyActiveUsageHandle{tracker: t, token: entry.Token}
}

func (h *ProxyActiveUsageHandle) End() {
	if h == nil || h.tracker == nil || h.token == "" {
		return
	}
	h.once.Do(func() {
		h.tracker.End(h.token)
	})
}

func (t *ProxyActiveUsageTracker) End(token string) {
	if t == nil || t.store == nil || token == "" {
		return
	}
	value, ok := t.inflight.LoadAndDelete(token)
	if !ok {
		return
	}
	entry, ok := value.(ProxyActiveUsageEntry)
	if !ok || !entry.Valid() {
		return
	}
	t.submit(entry, func(ctx context.Context, current ProxyActiveUsageEntry) {
		if err := t.store.RemoveProxyActiveUsage(ctx, current, t.ttl); err != nil {
			t.logTaskError("end", err, current)
		}
	})
}

func (t *ProxyActiveUsageTracker) GetActiveAccountCounts(ctx context.Context, proxyIDs []int64) map[int64]int64 {
	counts := zeroProxyActiveUsageCounts(proxyIDs)
	if t == nil || t.store == nil || len(counts) == 0 {
		return counts
	}
	queryCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && t.taskTimeout > 0 {
		queryCtx, cancel = context.WithTimeout(ctx, t.taskTimeout)
	}
	defer cancel()
	out, err := t.store.CountProxyActiveAccounts(queryCtx, proxyIDs)
	if err != nil {
		logger.L().With(
			zap.String("component", "service.proxy_active_usage_tracker"),
			zap.Error(err),
		).Warn("proxy_active_usage.count_failed")
		return counts
	}
	for proxyID, count := range out {
		counts[proxyID] = count
	}
	return counts
}

func (t *ProxyActiveUsageTracker) Stop() {
	if t == nil {
		return
	}
	if !t.stopped.CompareAndSwap(false, true) {
		return
	}
	close(t.stopCh)
	<-t.heartbeatDone
	if t.pool != nil {
		t.pool.StopAndWait()
	}
}

func (t *ProxyActiveUsageTracker) heartbeatLoop() {
	defer close(t.heartbeatDone)
	ticker := time.NewTicker(t.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.touchInflight()
		}
	}
}

func (t *ProxyActiveUsageTracker) touchInflight() {
	if t == nil || t.store == nil || t.stopped.Load() {
		return
	}
	t.inflight.Range(func(_, value any) bool {
		entry, ok := value.(ProxyActiveUsageEntry)
		if !ok || !entry.Valid() {
			return true
		}
		t.submit(entry, func(ctx context.Context, current ProxyActiveUsageEntry) {
			if _, ok := t.inflight.Load(current.Token); !ok {
				return
			}
			if err := t.store.UpsertProxyActiveUsage(ctx, current, t.ttl); err != nil {
				t.logTaskError("heartbeat", err, current)
			}
		})
		return true
	})
}

func (t *ProxyActiveUsageTracker) submit(entry ProxyActiveUsageEntry, task func(context.Context, ProxyActiveUsageEntry)) bool {
	if t == nil || t.store == nil || task == nil {
		return false
	}
	if t.pool == nil || t.pool.Stopped() || t.stopped.Load() {
		t.droppedStopped.Add(1)
		t.logDrop("stopped")
		return false
	}
	_, ok := t.pool.TrySubmit(func() {
		ctx, cancel := context.WithTimeout(context.Background(), t.taskTimeout)
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.L().With(
					zap.String("component", "service.proxy_active_usage_tracker"),
					zap.Any("panic", recovered),
				).Error("proxy_active_usage.task_panic")
			}
		}()
		task(ctx, entry)
	})
	if ok {
		return true
	}
	if t.pool.Stopped() {
		t.droppedStopped.Add(1)
		t.logDrop("stopped")
		return false
	}
	t.droppedQueueFull.Add(1)
	t.logDrop("full")
	return false
}

func (t *ProxyActiveUsageTracker) logTaskError(operation string, err error, entry ProxyActiveUsageEntry) {
	if err == nil {
		return
	}
	logger.L().With(
		zap.String("component", "service.proxy_active_usage_tracker"),
		zap.String("operation", operation),
		zap.Int64("proxy_id", entry.ProxyID),
		zap.Int64("account_id", entry.AccountID),
		zap.Error(err),
	).Debug("proxy_active_usage.task_failed")
}

func (t *ProxyActiveUsageTracker) logDrop(reason string) {
	now := time.Now().UnixNano()
	last := t.lastDropLogNanos.Load()
	if now-last < int64(proxyActiveUsageDropLogInterval) {
		return
	}
	if !t.lastDropLogNanos.CompareAndSwap(last, now) {
		return
	}
	logger.L().With(
		zap.String("component", "service.proxy_active_usage_tracker"),
		zap.String("reason", reason),
		zap.Uint64("dropped_queue_full", t.droppedQueueFull.Load()),
		zap.Uint64("dropped_stopped", t.droppedStopped.Load()),
	).Warn("proxy_active_usage.task_dropped")
}

func normalizeProxyActiveUsageTrackerOptions(opts ProxyActiveUsageTrackerOptions) ProxyActiveUsageTrackerOptions {
	if opts.WorkerCount <= 0 {
		opts.WorkerCount = defaultProxyActiveUsageWorkerCount
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultProxyActiveUsageQueueSize
	}
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = defaultProxyActiveUsageTaskTimeout
	}
	if opts.TTL <= 0 {
		opts.TTL = defaultProxyActiveUsageTTL
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = defaultProxyActiveUsageHeartbeat
	}
	if opts.HeartbeatInterval >= opts.TTL {
		opts.HeartbeatInterval = opts.TTL / 3
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = defaultProxyActiveUsageHeartbeat
	}
	return opts
}

func zeroProxyActiveUsageCounts(proxyIDs []int64) map[int64]int64 {
	out := make(map[int64]int64)
	for _, proxyID := range proxyIDs {
		if proxyID > 0 {
			out[proxyID] = 0
		}
	}
	return out
}

func ProxyActiveUsageUpsertScript() string {
	return proxyActiveUsageUpsertScript
}

func ProxyActiveUsageRemoveScript() string {
	return proxyActiveUsageRemoveScript
}
