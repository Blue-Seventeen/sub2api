package service

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// quotaDirtyCache 是 flusher 依赖的窄接口（来自 BillingCache）。
type quotaDirtyCache interface {
	PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]UserPlatformQuotaKey, error)
	ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []UserPlatformQuotaKey) error
	BatchGetUserPlatformQuotaCache(ctx context.Context, keys []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error)
}

// quotaSnapshotWriter 是 flusher 依赖的 DB 写入窄接口。
// 使用 service 层的 UserPlatformQuotaSnapshot，避免与 repository 包形成循环依赖；
// 实际实现由 repository adapter 在 B7 注入。
type quotaSnapshotWriter interface {
	BatchSnapshotUsage(ctx context.Context, snapshots []UserPlatformQuotaSnapshot, now time.Time) error
}

// FlusherMetrics 记录 flusher 运行时指标（原子量，零值可用）。
type FlusherMetrics struct {
	FlushSuccessTotal   atomic.Int64
	FlushErrorTotal     atomic.Int64
	FlushBatchSizeTotal atomic.Int64
	FlushLatencyMsMax   atomic.Int64
	DirtyReaddTotal     atomic.Int64
	// DirtyLostTotal：Readd 失败导致脏 key 丢失——已 SPOP+主操作失败+Readd 也失败；
	// Redis 仍权威，活跃 key 下次 SADD 自愈。
	DirtyLostTotal        atomic.Int64
	FlushFKViolationTotal atomic.Int64
}

// flusherMaxBatchesPerTick 单次 tick 最多消费的批数，防止 tick 执行时间过长。
const flusherMaxBatchesPerTick = 16

// maxFlushBatchSize 限制单批行数,必须 ≤ repository.BatchSnapshotUsage 的 batchRows(6000),
// 以保证单次 flush 的 snapshots 仅生成一条 UPSERT(单事务原子)。两处需手动保持一致。
const maxFlushBatchSize = 6000

// defaultFlushBatchSize 是配置 flush_batch_size 非法(≤0)时的回退值。
const defaultFlushBatchSize = 1000

const (
	userPlatformQuotaFlusherLeaderLockKey = "user-platform-quota:flusher:leader"
	userPlatformQuotaFlusherLeaderLockTTL = 2 * time.Minute
)

// UserPlatformQuotaUsageFlusher 将 Redis 脏集快照定期批量写入 DB。
// 不维护任何 delta/in-process 状态；每批读取 Redis 当前绝对值覆盖写入。
type UserPlatformQuotaUsageFlusher struct {
	cache       quotaDirtyCache
	quotaRepo   quotaSnapshotWriter
	timingWheel *TimingWheelService
	lockCache   LeaderLockCache
	lockDB      *sql.DB
	instanceID  string
	// enabled 对应 flusher_enabled 配置；false 时 Start() 不注册定时器。
	enabled      bool
	interval     time.Duration
	batchSize    int
	flushTimeout time.Duration
	metrics      *FlusherMetrics
	stopped      atomic.Bool
	flushMu      sync.Mutex
}

// NewUserPlatformQuotaUsageFlusher 创建 UserPlatformQuotaUsageFlusher。
// cache(BillingCache) 隐式满足 quotaDirtyCache；quotaRepo(UserPlatformQuotaRepository) 隐式满足 quotaSnapshotWriter。
func NewUserPlatformQuotaUsageFlusher(cfg *config.Config, cache BillingCache, quotaRepo UserPlatformQuotaRepository, tw *TimingWheelService) *UserPlatformQuotaUsageFlusher {
	batchSize := cfg.Database.UserPlatformQuotaFlushBatchSize
	if batchSize <= 0 {
		batchSize = defaultFlushBatchSize
	}
	if batchSize > maxFlushBatchSize {
		logger.LegacyPrintf("quota_flusher",
			"[QuotaFlusher] flush_batch_size %d 超过上限 %d,已 clamp(避免 BatchSnapshotUsage 多子批非原子)",
			cfg.Database.UserPlatformQuotaFlushBatchSize, maxFlushBatchSize)
		batchSize = maxFlushBatchSize
	}
	interval := time.Duration(cfg.Database.UserPlatformQuotaFlushIntervalMs) * time.Millisecond
	if interval <= 0 {
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] flush_interval_ms %d 非法,回退 2000ms", cfg.Database.UserPlatformQuotaFlushIntervalMs)
		interval = 2 * time.Second
	}
	return &UserPlatformQuotaUsageFlusher{
		cache:        cache,
		quotaRepo:    quotaRepo,
		timingWheel:  tw,
		instanceID:   CurrentNodeID() + ":user-platform-quota-flusher",
		enabled:      cfg.Database.UserPlatformQuotaFlusherEnabled,
		interval:     interval,
		batchSize:    batchSize,
		flushTimeout: 3 * time.Second,
		metrics:      &FlusherMetrics{},
	}
}

// SetLeaderLock injects distributed lock backends for multi-instance deployments.
func (s *UserPlatformQuotaUsageFlusher) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.lockDB = db
}

// updateLatencyMax 用 CAS 单调更新最大延迟。
func (s *UserPlatformQuotaUsageFlusher) updateLatencyMax(ms int64) {
	for {
		old := s.metrics.FlushLatencyMsMax.Load()
		if ms <= old {
			return
		}
		if s.metrics.FlushLatencyMsMax.CompareAndSwap(old, ms) {
			return
		}
	}
}

// readdOrCountLost 尝试把 keys 回填脏集：成功计 DirtyReaddTotal，失败计 DirtyLostTotal 并 ALERT。
func (s *UserPlatformQuotaUsageFlusher) readdOrCountLost(ctx context.Context, keys []UserPlatformQuotaKey, stage string) {
	readdCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.cache.ReaddDirtyUserPlatformQuotaKeys(readdCtx, keys); err != nil {
		s.metrics.DirtyLostTotal.Add(int64(len(keys)))
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] ALERT: Readd after %s failed, %d keys 丢出脏集(DB 镜像缺这批,Redis 仍权威,活跃 key 下次 SADD 自愈): %v", stage, len(keys), err)
		return
	}
	s.metrics.DirtyReaddTotal.Add(int64(len(keys)))
}

// flushOneBatch 处理单批：Pop → BatchGet → 组装 snaps → BatchSnapshotUsage。
// 返回 (shouldContinue bool)：false 表示本轮循环应停止（空集/错误/最后一批）。
// 每次调用独立创建带 timeout 的 ctx 并 defer cancel，不会在循环中累积泄漏。
func (s *UserPlatformQuotaUsageFlusher) flushOneBatch(parentCtx context.Context) bool {
	ctx, cancel := context.WithTimeout(parentCtx, s.flushTimeout)
	defer cancel()

	// 1. Pop 脏集
	keys, err := s.cache.PopDirtyUserPlatformQuotaKeys(ctx, s.batchSize)
	if err != nil {
		s.metrics.FlushErrorTotal.Add(1)
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] PopDirty error: %v", err)
		return false
	}
	if len(keys) == 0 {
		// 脏集已空
		return false
	}

	// 2. 批量读 Redis 快照
	snapshotTakenAt := time.Now().UTC()
	entries, err := s.cache.BatchGetUserPlatformQuotaCache(ctx, keys)
	if err != nil {
		s.metrics.FlushErrorTotal.Add(1)
		s.readdOrCountLost(ctx, keys, "BatchGet")
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] BatchGet error: %v", err)
		return false
	}

	// 3. 组装 snapshots（MISS 或任一 WindowStart==nil → 跳过）
	snaps := make([]UserPlatformQuotaSnapshot, 0, len(keys))
	snapKeys := make([]UserPlatformQuotaKey, 0, len(keys))
	for i, key := range keys {
		e := entries[i]
		if e == nil {
			continue
		}
		if e.DailyWindowStart == nil || e.WeeklyWindowStart == nil || e.MonthlyWindowStart == nil {
			continue
		}
		snaps = append(snaps, UserPlatformQuotaSnapshot{
			UserID:             key.UserID,
			Platform:           key.Platform,
			DailyUsageUSD:      e.DailyUsageUSD,
			WeeklyUsageUSD:     e.WeeklyUsageUSD,
			MonthlyUsageUSD:    e.MonthlyUsageUSD,
			DailyWindowStart:   *e.DailyWindowStart,
			WeeklyWindowStart:  *e.WeeklyWindowStart,
			MonthlyWindowStart: *e.MonthlyWindowStart,
			SnapshotTakenAt:    snapshotTakenAt,
		})
		snapKeys = append(snapKeys, key)
	}

	// 4. 全部 MISS/异常跳过时
	if len(snaps) == 0 {
		// 若 Pop 数量已不满一批，表示脏集将空，停止
		if len(keys) < s.batchSize {
			return false
		}
		// 否则继续下一批（可能还有更多脏 key）
		return true
	}

	// BatchSnapshotUsage uses SnapshotTakenAt as an OCC guard, so this Redis
	// snapshot cannot overwrite a newer admin DB write or resurrect a soft-deleted
	// user/platform row.

	// 5. 写入 DB
	start := time.Now()
	now := time.Now().UTC()
	writeErr := s.quotaRepo.BatchSnapshotUsage(ctx, snaps, now)
	s.updateLatencyMax(time.Since(start).Milliseconds())

	if writeErr != nil {
		if errors.Is(writeErr, ErrUserPlatformQuotaFKViolation) {
			s.metrics.FlushFKViolationTotal.Add(1)
			s.metrics.FlushErrorTotal.Add(1)
			s.recoverFKViolationBatch(ctx, snapKeys, snaps, now, writeErr)
		} else {
			// 其他错误：回填脏集，保留下次重试
			s.metrics.FlushErrorTotal.Add(1)
			s.readdOrCountLost(ctx, keys, "BatchSnapshotUsage")
			logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] BatchSnapshotUsage error: %v", writeErr)
		}
		return false
	}

	// 6. 成功
	s.metrics.FlushSuccessTotal.Add(1)
	s.metrics.FlushBatchSizeTotal.Add(int64(len(snaps)))

	// 若 Pop 数量不满一批，脏集已空，停止
	if len(keys) < s.batchSize {
		return false
	}
	return true
}

func (s *UserPlatformQuotaUsageFlusher) recoverFKViolationBatch(ctx context.Context, keys []UserPlatformQuotaKey, snaps []UserPlatformQuotaSnapshot, now time.Time, batchErr error) {
	recovered := 0
	dropped := 0
	for i, snap := range snaps {
		start := time.Now()
		err := s.quotaRepo.BatchSnapshotUsage(ctx, []UserPlatformQuotaSnapshot{snap}, now)
		s.updateLatencyMax(time.Since(start).Milliseconds())
		if err == nil {
			recovered++
			continue
		}
		if errors.Is(err, ErrUserPlatformQuotaFKViolation) {
			dropped++
			continue
		}
		s.readdOrCountLost(ctx, keys[i:], "BatchSnapshotUsageFKRetry")
		logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] FK recovery retry failed after recovered=%d dropped=%d remaining=%d batch_err=%v err=%v", recovered, dropped, len(keys)-i, batchErr, err)
		return
	}
	if recovered > 0 {
		s.metrics.FlushBatchSizeTotal.Add(int64(recovered))
	}
	logger.LegacyPrintf("quota_flusher", "[QuotaFlusher] FK violation recovered=%d dropped_deleted=%d batch_err=%v", recovered, dropped, batchErr)
}

// flush 执行一次完整的 flush，循环消费至脏集空或达到 maxBatchesPerTick。
func (s *UserPlatformQuotaUsageFlusher) flush() {
	if s == nil {
		return
	}
	parentCtx := context.Background()
	for b := 0; b < flusherMaxBatchesPerTick; b++ {
		if !s.flushOneBatch(parentCtx) {
			return
		}
	}
	// 连续消费满 flusherMaxBatchesPerTick 批仍未取空脏集:本 tick 主动让出,剩余积压留待下一 tick。
	// 记一条 log 便于 oncall 发现 distinct 活跃 key 远超 maxBatchesPerTick×batchSize(DB 镜像延迟上升);
	// 可配合 Redis SCARD billing:upq:dirty 观察脏集存量。
	logger.LegacyPrintf("quota_flusher",
		"[QuotaFlusher] 单 tick 达到 max batches 上限(%d × batchSize=%d),脏集仍非空,积压顺延至下一 tick",
		flusherMaxBatchesPerTick, s.batchSize)
}

func (s *UserPlatformQuotaUsageFlusher) flushWithLeaderLock() {
	if s == nil {
		return
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), s.flushTimeout)
	defer cancel()

	release, ok := tryAcquirePeriodicLeaderLease(ctx, s.lockCache, s.lockDB, userPlatformQuotaFlusherLeaderLockKey, s.instanceID, s.periodicLeaderLeaseTTL())
	if !ok {
		return
	}
	defer release()

	s.flush()
}

func (s *UserPlatformQuotaUsageFlusher) flushWithReleasableLeaderLock() {
	if s == nil {
		return
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), s.flushTimeout)
	defer cancel()

	if s.lockCache != nil {
		ok, err := s.lockCache.TryAcquireOrRenewLeaderLock(ctx, userPlatformQuotaFlusherLeaderLockKey, s.instanceID, s.periodicLeaderLeaseTTL())
		if err != nil || !ok {
			return
		}
		defer func() {
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer releaseCancel()
			_ = s.lockCache.ReleaseLeaderLock(releaseCtx, userPlatformQuotaFlusherLeaderLockKey, s.instanceID)
		}()
	} else {
		release, ok := tryAcquireSingletonLeaderLock(ctx, nil, s.lockDB, userPlatformQuotaFlusherLeaderLockKey, s.instanceID, userPlatformQuotaFlusherLeaderLockTTL)
		if !ok {
			return
		}
		defer release()
	}

	s.flush()
}

func (s *UserPlatformQuotaUsageFlusher) periodicLeaderLeaseTTL() time.Duration {
	ttl := s.interval + s.flushTimeout + 30*time.Second
	if ttl < userPlatformQuotaFlusherLeaderLockTTL {
		return userPlatformQuotaFlusherLeaderLockTTL
	}
	return ttl
}

// tick 是 TimingWheel 回调。若 flusher 已停止则直接返回。
func (s *UserPlatformQuotaUsageFlusher) tick() {
	if s == nil || s.stopped.Load() {
		return
	}
	s.flushWithLeaderLock()
}

// Start 注册定时 tick。flusher_enabled=false 时直接返回，不注册定时器。
func (s *UserPlatformQuotaUsageFlusher) Start() {
	if s == nil || !s.enabled {
		return
	}
	s.timingWheel.ScheduleRecurring("deferred:platform_quota", s.interval, s.tick)
}

// Stop 停止 flusher：标记 stopped → Cancel 定时器 → 执行最后一次 flush。
func (s *UserPlatformQuotaUsageFlusher) Stop() {
	if s == nil {
		return
	}
	s.stopped.Store(true)
	s.timingWheel.Cancel("deferred:platform_quota")
	s.flushWithReleasableLeaderLock()
}
