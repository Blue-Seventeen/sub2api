package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/dgraph-io/ristretto"
	"golang.org/x/sync/singleflight"
)

// MaxExpiresAt is the maximum allowed expiration date (year 2099)
// This prevents time.Time JSON serialization errors (RFC 3339 requires year <= 9999)
var MaxExpiresAt = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)

// MaxValidityDays is the maximum allowed validity days for subscriptions (100 years)
const MaxValidityDays = 36500

const subscriptionAssignLocalLockStripes = 256

var subscriptionAssignLocalLocks [subscriptionAssignLocalLockStripes]sync.Mutex

var (
	ErrSubscriptionNotFound            = infraerrors.NotFound("SUBSCRIPTION_NOT_FOUND", "subscription not found")
	ErrSubscriptionExpired             = infraerrors.Forbidden("SUBSCRIPTION_EXPIRED", "subscription has expired")
	ErrSubscriptionSuspended           = infraerrors.Forbidden("SUBSCRIPTION_SUSPENDED", "subscription is suspended")
	ErrSubscriptionAlreadyExists       = infraerrors.Conflict("SUBSCRIPTION_ALREADY_EXISTS", "subscription already exists for this user and group")
	ErrSubscriptionAssignConflict      = infraerrors.Conflict("SUBSCRIPTION_ASSIGN_CONFLICT", "subscription exists but request conflicts with existing assignment semantics")
	ErrSubscriptionNotRevoked          = infraerrors.Conflict("SUBSCRIPTION_NOT_REVOKED", "subscription is not revoked")
	ErrSubscriptionRestoreConflict     = infraerrors.Conflict("SUBSCRIPTION_RESTORE_CONFLICT", "subscription already exists for this user and group")
	ErrGroupNotSubscriptionType        = infraerrors.BadRequest("GROUP_NOT_SUBSCRIPTION_TYPE", "group is not a subscription type")
	ErrInvalidInput                    = infraerrors.BadRequest("INVALID_INPUT", "at least one of resetDaily, resetWeekly, resetMonthly, or resetCustom must be true")
	ErrDailyLimitExceeded              = infraerrors.TooManyRequests("DAILY_LIMIT_EXCEEDED", "daily usage limit exceeded")
	ErrWeeklyLimitExceeded             = infraerrors.TooManyRequests("WEEKLY_LIMIT_EXCEEDED", "weekly usage limit exceeded")
	ErrMonthlyLimitExceeded            = infraerrors.TooManyRequests("MONTHLY_LIMIT_EXCEEDED", "monthly usage limit exceeded")
	ErrCustomLimitExceeded             = infraerrors.TooManyRequests("CUSTOM_LIMIT_EXCEEDED", "custom usage limit exceeded")
	ErrSubscriptionNilInput            = infraerrors.BadRequest("SUBSCRIPTION_NIL_INPUT", "subscription input cannot be nil")
	ErrAdjustWouldExpire               = infraerrors.BadRequest("ADJUST_WOULD_EXPIRE", "adjustment would result in expired subscription (remaining days must be > 0)")
	ErrAdjustNoFields                  = infraerrors.BadRequest("INVALID_INPUT", "at least one adjustment field must be provided")
	ErrSubscriptionRestoreInvalid      = infraerrors.BadRequest("SUBSCRIPTION_RESTORE_NOT_ALLOWED", "only revoked or soft-deleted subscriptions can be restored")
	ErrSubscriptionRestoreGroupInvalid = infraerrors.BadRequest("SUBSCRIPTION_RESTORE_GROUP_INVALID", "subscription group is no longer active or subscription type")
	ErrSubscriptionHardDeleteInvalid   = infraerrors.BadRequest("SUBSCRIPTION_HARD_DELETE_NOT_ALLOWED", "only revoked, soft-deleted, or expired subscriptions can be hard deleted")
)

// SubscriptionService 订阅服务
type SubscriptionService struct {
	groupRepo           GroupRepository
	userSubRepo         UserSubscriptionRepository
	billingCacheService *BillingCacheService
	entClient           *dbent.Client

	// L1 缓存：加速中间件热路径的订阅查询
	subCacheL1       *ristretto.Cache
	subCacheGroup    singleflight.Group
	subCacheVersions sync.Map
	subCacheTTL      time.Duration
	subCacheJitter   int // 抖动百分比

	maintenanceQueue *SubscriptionMaintenanceQueue
	now              func() time.Time
}

type userSubscriptionHistoryRepository interface {
	GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error)
	ListByUserIDIncludeDeleted(ctx context.Context, userID int64) ([]UserSubscription, error)
}

type userSubscriptionAdminMutationRepository interface {
	GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error)
	Restore(ctx context.Context, id int64, restoredStatus string) (*UserSubscription, error)
	HardDelete(ctx context.Context, id int64) error
}

type userSubscriptionGroupSnapshotRepository interface {
	UpdateGroupSnapshot(ctx context.Context, sub *UserSubscription) error
}

// NewSubscriptionService 创建订阅服务
func NewSubscriptionService(groupRepo GroupRepository, userSubRepo UserSubscriptionRepository, billingCacheService *BillingCacheService, entClient *dbent.Client, cfg *config.Config) *SubscriptionService {
	svc := &SubscriptionService{
		groupRepo:           groupRepo,
		userSubRepo:         userSubRepo,
		billingCacheService: billingCacheService,
		entClient:           entClient,
		now:                 time.Now,
	}
	svc.initSubCache(cfg)
	svc.initMaintenanceQueue(cfg)
	if billingCacheService != nil {
		billingCacheService.SetSubscriptionL1Invalidator(svc.InvalidateSubCache)
		billingCacheService.SetSubscriptionL1UsageUpdater(svc.IncrementSubCacheUsage)
	}
	svc.StartSubCacheInvalidationSubscriber(context.Background())
	return svc
}

func (s *SubscriptionService) initMaintenanceQueue(cfg *config.Config) {
	if cfg == nil {
		return
	}
	mc := cfg.SubscriptionMaintenance
	if mc.WorkerCount <= 0 || mc.QueueSize <= 0 {
		return
	}
	s.maintenanceQueue = NewSubscriptionMaintenanceQueue(mc.WorkerCount, mc.QueueSize)
}

// Stop stops the maintenance worker pool.
func (s *SubscriptionService) Stop() {
	if s == nil {
		return
	}
	if s.maintenanceQueue != nil {
		s.maintenanceQueue.Stop()
	}
}

// initSubCache 初始化订阅 L1 缓存
func (s *SubscriptionService) initSubCache(cfg *config.Config) {
	if cfg == nil {
		return
	}
	sc := cfg.SubscriptionCache
	if sc.L1Size <= 0 || sc.L1TTLSeconds <= 0 {
		return
	}
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: int64(sc.L1Size) * 10,
		MaxCost:     int64(sc.L1Size),
		BufferItems: 64,
	})
	if err != nil {
		log.Printf("Warning: failed to init subscription L1 cache: %v", err)
		return
	}
	s.subCacheL1 = cache
	s.subCacheTTL = time.Duration(sc.L1TTLSeconds) * time.Second
	s.subCacheJitter = sc.JitterPercent
}

// subCacheKey 生成订阅缓存 key（热路径，避免 fmt.Sprintf 开销）
func subCacheKey(userID, groupID int64) string {
	return "sub:" + strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(groupID, 10)
}

type invalidatedSubCacheEntry struct{}

type subCacheEntry struct {
	sub     *UserSubscription
	version uint64
}

func (s *SubscriptionService) subCacheVersion(key string) uint64 {
	if s == nil {
		return 0
	}
	v, ok := s.subCacheVersions.Load(key)
	if !ok {
		return 0
	}
	n, ok := v.(uint64)
	if !ok {
		return 0
	}
	return n
}

func (s *SubscriptionService) bumpSubCacheVersion(key string) uint64 {
	next := s.subCacheVersion(key) + 1
	s.subCacheVersions.Store(key, next)
	return next
}

// jitteredTTL 为 TTL 添加抖动，避免集中过期
func (s *SubscriptionService) jitteredTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 || s.subCacheJitter <= 0 {
		return ttl
	}
	pct := s.subCacheJitter
	if pct > 100 {
		pct = 100
	}
	delta := float64(pct) / 100
	factor := 1 - delta + rand.Float64()*(2*delta)
	if factor <= 0 {
		return ttl
	}
	return time.Duration(float64(ttl) * factor)
}

// InvalidateSubCache 失效指定用户+分组的订阅 L1 缓存
func (s *SubscriptionService) InvalidateSubCache(userID, groupID int64) {
	if s.subCacheL1 == nil {
		return
	}
	key := subCacheKey(userID, groupID)
	s.bumpSubCacheVersion(key)
	s.subCacheL1.Del(key)
	_ = s.subCacheL1.SetWithTTL(key, invalidatedSubCacheEntry{}, 1, time.Second)
	s.subCacheL1.Wait()
}

func (s *SubscriptionService) IncrementSubCacheUsage(userID, groupID int64, costUSD float64) {
	if s == nil || s.subCacheL1 == nil || costUSD <= 0 {
		return
	}
	key := subCacheKey(userID, groupID)
	v, ok := s.subCacheL1.Get(key)
	if !ok {
		return
	}
	entry, ok := v.(*subCacheEntry)
	if !ok || entry == nil || entry.sub == nil || entry.version != s.subCacheVersion(key) {
		return
	}
	cp := *entry.sub
	cp.DailyUsageUSD += costUSD
	cp.WeeklyUsageUSD += costUSD
	cp.MonthlyUsageUSD += costUSD
	cp.CustomUsageUSD += costUSD
	if cp.StackedAvailableUSD != nil {
		remaining := *cp.StackedAvailableUSD - costUSD
		if remaining < 0 {
			remaining = 0
		}
		cp.StackedAvailableUSD = &remaining
	}
	_ = s.subCacheL1.SetWithTTL(key, &subCacheEntry{sub: &cp, version: entry.version}, 1, s.jitteredTTL(s.subCacheTTL))
	s.subCacheL1.Wait()
}

// InvalidateSubCacheSync 失效订阅 L1 缓存并等待 Ristretto 删除操作生效。
func (s *SubscriptionService) InvalidateSubCacheSync(userID, groupID int64) {
	s.invalidateSubCacheKeySync(subCacheKey(userID, groupID))
}

func (s *SubscriptionService) invalidateSubCacheKeySync(key string) {
	if s.subCacheL1 == nil {
		return
	}
	s.bumpSubCacheVersion(key)
	s.subCacheL1.Del(key)
	_ = s.subCacheL1.SetWithTTL(key, invalidatedSubCacheEntry{}, 1, time.Second)
	s.subCacheL1.Wait()
}

// StartSubCacheInvalidationSubscriber 启动跨实例订阅 L1 缓存失效订阅。
func (s *SubscriptionService) StartSubCacheInvalidationSubscriber(ctx context.Context) {
	if s.billingCacheService == nil || s.subCacheL1 == nil {
		return
	}
	if err := s.billingCacheService.SubscribeSubscriptionCacheInvalidation(ctx, func(cacheKey string) {
		s.invalidateSubCacheKeySync(cacheKey)
	}); err != nil {
		log.Printf("Warning: failed to start subscription cache invalidation subscriber: %v", err)
	}
}

func (s *SubscriptionService) invalidateSubscriptionCaches(userID, groupID int64) error {
	s.InvalidateSubCacheSync(userID, groupID)
	if s.billingCacheService == nil {
		return nil
	}

	cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID); err != nil {
		return fmt.Errorf("invalidate billing subscription cache: %w", err)
	}
	if err := s.billingCacheService.PublishSubscriptionCacheInvalidation(cacheCtx, subCacheKey(userID, groupID)); err != nil {
		return fmt.Errorf("publish subscription cache invalidation: %w", err)
	}
	return nil
}

// AssignSubscriptionInput 分配订阅输入
type AssignSubscriptionInput struct {
	UserID             int64
	GroupID            int64
	ValidityDays       int
	AssignedBy         int64
	Notes              string
	SourceType         string
	SourceRefID        string
	SourceRedeemCodeID *int64
	RedeemCodeSnapshot string
}

type AdjustSubscriptionInput struct {
	Days            *int
	DailyUsageUSD   *float64
	WeeklyUsageUSD  *float64
	MonthlyUsageUSD *float64
	CustomUsageUSD  *float64
}

// AssignSubscription 分配订阅给用户（不允许重复分配）
func (s *SubscriptionService) AssignSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, error) {
	sub, _, err := s.assignSubscriptionWithReuse(ctx, input)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// AssignOrExtendSubscription 分配或续期订阅（用于兑换码等场景）
// 如果用户已有同分组的订阅：
//   - 未过期：从当前过期时间累加天数
//   - 已过期：从当前时间开始计算新的过期时间，并激活订阅
//
// 如果没有订阅：创建新订阅
func (s *SubscriptionService) AssignOrExtendSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	if input == nil {
		return nil, false, ErrSubscriptionNilInput
	}
	return s.withSerializedSubscriptionAssignment(ctx, input.UserID, input.GroupID, func(txCtx context.Context) (*UserSubscription, bool, error) {
		return s.assignOrExtendSubscription(txCtx, input, false)
	})
}

func (s *SubscriptionService) assignOrExtendSubscription(ctx context.Context, input *AssignSubscriptionInput, deferCacheInvalidation bool) (*UserSubscription, bool, error) {
	if input == nil {
		return nil, false, ErrSubscriptionNilInput
	}
	// 检查分组是否存在且为订阅类型
	group, err := s.groupRepo.GetByID(ctx, input.GroupID)
	if err != nil {
		return nil, false, fmt.Errorf("group not found: %w", err)
	}
	if !group.IsActive() || !group.IsSubscriptionType() {
		return nil, false, ErrGroupNotSubscriptionType
	}

	// 查询是否已有订阅
	existingSub, err := s.userSubRepo.GetByUserIDAndGroupID(ctx, input.UserID, input.GroupID)
	if err != nil {
		// 不存在记录是正常情况，其他错误需要返回
		existingSub = nil
	}

	validityDays := input.ValidityDays
	if validityDays <= 0 {
		validityDays = 30
	}
	if validityDays > MaxValidityDays {
		validityDays = MaxValidityDays
	}

	// 已有订阅，执行续期（在事务中完成所有更新）
	if existingSub != nil {
		now := time.Now()
		var newExpiresAt time.Time

		isExpired := !existingSub.ExpiresAt.After(now)
		if !isExpired {
			// 未过期：从当前过期时间累加
			newExpiresAt = existingSub.ExpiresAt.AddDate(0, 0, validityDays)
		} else {
			// 已过期：从当前时间开始计算
			newExpiresAt = now.AddDate(0, 0, validityDays)
		}

		// 确保不超过最大过期时间
		if newExpiresAt.After(MaxExpiresAt) {
			newExpiresAt = MaxExpiresAt
		}

		if err := s.updateExistingSubscriptionTerm(ctx, existingSub, input.Notes, now, newExpiresAt, isExpired); err != nil {
			return nil, false, err
		}

		// 失效订阅缓存
		s.maybeInvalidateAssignmentCaches(input.UserID, input.GroupID, deferCacheInvalidation)

		// 返回更新后的订阅
		sub, err := s.userSubRepo.GetByID(ctx, existingSub.ID)
		return sub, true, err // true 表示是续期
	}

	// 没有订阅，创建新订阅
	sub, err := s.createSubscription(ctx, input)
	if err != nil {
		return nil, false, err
	}

	// 失效订阅缓存
	s.maybeInvalidateAssignmentCaches(input.UserID, input.GroupID, deferCacheInvalidation)

	return sub, false, nil // false 表示是新建
}

func (s *SubscriptionService) maybeInvalidateAssignmentCaches(userID, groupID int64, deferred bool) {
	// Payment fulfillment owns an outer transaction and performs a synchronous
	// invalidation after commit. Invalidating inside that transaction can reload
	// the pre-commit subscription into cache.
	if deferred {
		return
	}

	s.InvalidateSubCache(userID, groupID)
	if s.billingCacheService != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID)
		}()
	}
}

func (s *SubscriptionService) withSerializedSubscriptionAssignment(ctx context.Context, userID, groupID int64, fn func(context.Context) (*UserSubscription, bool, error)) (*UserSubscription, bool, error) {
	localUnlock := lockSubscriptionAssignmentLocal(userID, groupID)
	defer localUnlock()

	if s.entClient == nil {
		return fn(ctx)
	}
	if dbent.TxFromContext(ctx) != nil {
		if err := s.lockSubscriptionAssignmentInDB(ctx, userID, groupID); err != nil {
			return nil, false, err
		}
		return fn(ctx)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin subscription assignment transaction: %w", err)
	}
	txCtx := dbent.NewTxContext(ctx, tx)

	if err := s.lockSubscriptionAssignmentInDB(txCtx, userID, groupID); err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}

	sub, reused, err := fn(txCtx)
	if err != nil {
		_ = tx.Rollback()
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit subscription assignment transaction: %w", err)
	}
	return sub, reused, nil
}

func lockSubscriptionAssignmentLocal(userID, groupID int64) func() {
	key := subscriptionAssignmentLockKey(userID, groupID)
	mu := &subscriptionAssignLocalLocks[uint64(key)%subscriptionAssignLocalLockStripes]
	mu.Lock()
	return mu.Unlock
}

func subscriptionAssignmentLockKey(userID, groupID int64) int64 {
	h := uint64(1469598103934665603)
	h ^= uint64(userID)
	h *= 1099511628211
	h ^= uint64(groupID)
	h *= 1099511628211
	h ^= 0x7375623261706921
	return int64(h)
}

func (s *SubscriptionService) lockSubscriptionAssignmentInDB(ctx context.Context, userID, groupID int64) error {
	if s.entClient == nil || s.entClient.Driver().Dialect() != dialect.Postgres {
		return nil
	}
	client := s.entClient
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	rows, err := client.QueryContext(ctx, "SELECT pg_advisory_xact_lock($1)", subscriptionAssignmentLockKey(userID, groupID))
	if err != nil {
		return fmt.Errorf("lock subscription assignment: %w", err)
	}
	return rows.Close()
}

func (s *SubscriptionService) AssignStackedSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	return s.assignStackedSubscription(ctx, input, false)
}

func (s *SubscriptionService) assignStackedSubscription(ctx context.Context, input *AssignSubscriptionInput, deferCacheInvalidation bool) (*UserSubscription, bool, error) {
	if input == nil {
		return nil, false, ErrSubscriptionNilInput
	}
	group, err := s.groupRepo.GetByID(ctx, input.GroupID)
	if err != nil {
		return nil, false, fmt.Errorf("group not found: %w", err)
	}
	if !group.IsActive() || !group.IsSubscriptionType() {
		return nil, false, ErrGroupNotSubscriptionType
	}
	if strings.TrimSpace(input.SourceType) != "" && strings.TrimSpace(input.SourceRefID) != "" {
		if existing, getErr := s.userSubRepo.GetBySource(ctx, input.SourceType, input.SourceRefID); getErr == nil && existing != nil {
			return existing, true, nil
		}
	}
	sub, err := s.createSubscriptionWithGroup(ctx, input, group)
	if err != nil {
		return nil, false, err
	}
	s.maybeInvalidateAssignmentCaches(input.UserID, input.GroupID, deferCacheInvalidation)
	return sub, false, nil
}

func (s *SubscriptionService) updateExistingSubscriptionTerm(
	ctx context.Context,
	existingSub *UserSubscription,
	notes string,
	startsAt time.Time,
	newExpiresAt time.Time,
	isExpired bool,
) error {
	return s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		if isExpired {
			renewed := renewedSubscriptionTerm(existingSub, notes, startsAt, newExpiresAt)
			if err := s.userSubRepo.Update(txCtx, renewed); err != nil {
				return fmt.Errorf("renew expired subscription: %w", err)
			}
			return nil
		}

		// 更新过期时间
		if err := s.userSubRepo.ExtendExpiry(txCtx, existingSub.ID, newExpiresAt); err != nil {
			return fmt.Errorf("extend subscription: %w", err)
		}

		// 如果订阅被暂停，恢复为 active 状态
		if existingSub.Status != SubscriptionStatusActive {
			if err := s.userSubRepo.UpdateStatus(txCtx, existingSub.ID, SubscriptionStatusActive); err != nil {
				return fmt.Errorf("update subscription status: %w", err)
			}
		}

		// 追加备注
		if notes != "" {
			if err := s.userSubRepo.UpdateNotes(txCtx, existingSub.ID, appendSubscriptionNotes(existingSub.Notes, notes)); err != nil {
				return fmt.Errorf("update subscription notes: %w", err)
			}
		}

		return nil
	})
}

func (s *SubscriptionService) withSubscriptionUpdateTx(ctx context.Context, fn func(context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	if s.entClient == nil {
		return fn(ctx)
	}
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	txCtx := dbent.NewTxContext(ctx, tx)

	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func renewedSubscriptionTerm(existingSub *UserSubscription, notes string, startsAt, expiresAt time.Time) *UserSubscription {
	renewed := *existingSub
	dailyWindowStart := startsAt
	weeklyWindowStart := startsAt
	monthlyWindowStart := startsAt
	customWindowStart := startsAt
	renewed.StartsAt = startsAt
	renewed.ExpiresAt = expiresAt
	renewed.Status = SubscriptionStatusActive
	renewed.DailyWindowStart = &dailyWindowStart
	renewed.WeeklyWindowStart = &weeklyWindowStart
	renewed.MonthlyWindowStart = &monthlyWindowStart
	renewed.CustomWindowStart = &customWindowStart
	renewed.DailyUsageUSD = 0
	renewed.WeeklyUsageUSD = 0
	renewed.MonthlyUsageUSD = 0
	renewed.CustomUsageUSD = 0
	renewed.Notes = appendSubscriptionNotes(existingSub.Notes, notes)
	return &renewed
}

func appendSubscriptionNotes(existingNotes, newNotes string) string {
	if newNotes == "" {
		return existingNotes
	}
	if existingNotes == "" {
		return newNotes
	}
	return existingNotes + "\n" + newNotes
}

// createSubscription 创建新订阅（内部方法）
func (s *SubscriptionService) createSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, error) {
	group, err := s.groupRepo.GetByID(ctx, input.GroupID)
	if err != nil {
		return nil, fmt.Errorf("group not found: %w", err)
	}
	return s.createSubscriptionWithGroup(ctx, input, group)
}

func (s *SubscriptionService) createSubscriptionWithGroup(ctx context.Context, input *AssignSubscriptionInput, group *Group) (*UserSubscription, error) {
	validityDays := input.ValidityDays
	if validityDays <= 0 {
		validityDays = 30
	}
	if validityDays > MaxValidityDays {
		validityDays = MaxValidityDays
	}

	now := time.Now()
	expiresAt := now.AddDate(0, 0, validityDays)
	if expiresAt.After(MaxExpiresAt) {
		expiresAt = MaxExpiresAt
	}
	dailyWindowStart := now
	weeklyWindowStart := now
	monthlyWindowStart := now
	customWindowStart := now

	sub := &UserSubscription{
		UserID:             input.UserID,
		GroupID:            input.GroupID,
		StartsAt:           now,
		ExpiresAt:          expiresAt,
		Status:             SubscriptionStatusActive,
		DailyWindowStart:   &dailyWindowStart,
		WeeklyWindowStart:  &weeklyWindowStart,
		MonthlyWindowStart: &monthlyWindowStart,
		CustomWindowStart:  &customWindowStart,
		AssignedAt:         now,
		Notes:              input.Notes,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	applySubscriptionSource(sub, input)
	snapshotSubscriptionGroup(sub, group)
	// 只有当 AssignedBy > 0 时才设置（0 表示系统分配，如兑换码）
	if input.AssignedBy > 0 {
		sub.AssignedBy = &input.AssignedBy
	}

	if err := s.userSubRepo.Create(ctx, sub); err != nil {
		return nil, err
	}

	// 重新获取完整订阅信息（包含关联）
	return s.userSubRepo.GetByID(ctx, sub.ID)
}

// BulkAssignSubscriptionInput 批量分配订阅输入
func applySubscriptionSource(sub *UserSubscription, input *AssignSubscriptionInput) {
	if sub == nil || input == nil {
		return
	}
	if sourceType := strings.TrimSpace(input.SourceType); sourceType != "" {
		sub.SourceType = &sourceType
	}
	if sourceRefID := strings.TrimSpace(input.SourceRefID); sourceRefID != "" {
		sub.SourceRefID = &sourceRefID
	}
	if input.SourceRedeemCodeID != nil {
		v := *input.SourceRedeemCodeID
		sub.SourceRedeemCodeID = &v
	}
	if redeemCode := strings.TrimSpace(input.RedeemCodeSnapshot); redeemCode != "" {
		sub.RedeemCodeSnapshot = &redeemCode
	}
}

func snapshotSubscriptionGroup(sub *UserSubscription, group *Group) {
	if sub == nil || group == nil {
		return
	}
	name := group.Name
	sub.GroupNameSnapshot = &name
	platform := group.Platform
	sub.GroupPlatformSnapshot = &platform
	rate := group.RateMultiplier
	sub.GroupRateMultiplierSnapshot = &rate
	sub.DailyLimitUSDSnapshot = snapshotSubscriptionLimit(group.DailyLimitUSD)
	sub.WeeklyLimitUSDSnapshot = snapshotSubscriptionLimit(group.WeeklyLimitUSD)
	sub.MonthlyLimitUSDSnapshot = snapshotSubscriptionLimit(group.MonthlyLimitUSD)
	hours := group.CustomLimitHours
	sub.CustomLimitHoursSnapshot = &hours
	sub.CustomLimitUSDSnapshot = snapshotSubscriptionLimit(group.CustomLimitUSD)
}

func snapshotSubscriptionLimit(limit *float64) *float64 {
	if limit == nil {
		v := 0.0
		return &v
	}
	v := *limit
	return &v
}

func (s *SubscriptionService) refreshSubscriptionGroupSnapshot(ctx context.Context, sub *UserSubscription) error {
	if sub == nil {
		return nil
	}
	group := sub.Group
	if group == nil && s.groupRepo != nil {
		loaded, err := s.groupRepo.GetByID(ctx, sub.GroupID)
		if err == nil {
			group = loaded
		}
	}
	if group == nil {
		return nil
	}
	snapshotSubscriptionGroup(sub, group)
	if repo, ok := s.userSubRepo.(userSubscriptionGroupSnapshotRepository); ok {
		return repo.UpdateGroupSnapshot(ctx, sub)
	}
	return s.userSubRepo.Update(ctx, sub)
}

type BulkAssignSubscriptionInput struct {
	UserIDs      []int64
	GroupID      int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

// BulkAssignResult 批量分配结果
type BulkAssignResult struct {
	SuccessCount  int
	CreatedCount  int
	ReusedCount   int
	FailedCount   int
	Subscriptions []UserSubscription
	Errors        []string
	Statuses      map[int64]string
}

// BulkAssignSubscription 批量分配订阅
func (s *SubscriptionService) BulkAssignSubscription(ctx context.Context, input *BulkAssignSubscriptionInput) (*BulkAssignResult, error) {
	result := &BulkAssignResult{
		Subscriptions: make([]UserSubscription, 0),
		Errors:        make([]string, 0),
		Statuses:      make(map[int64]string),
	}

	for _, userID := range input.UserIDs {
		sub, reused, err := s.assignSubscriptionWithReuse(ctx, &AssignSubscriptionInput{
			UserID:       userID,
			GroupID:      input.GroupID,
			ValidityDays: input.ValidityDays,
			AssignedBy:   input.AssignedBy,
			Notes:        input.Notes,
		})
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("user %d: %v", userID, err))
			result.Statuses[userID] = "failed"
		} else {
			result.SuccessCount++
			result.Subscriptions = append(result.Subscriptions, *sub)
			if reused {
				result.ReusedCount++
				result.Statuses[userID] = "reused"
			} else {
				result.CreatedCount++
				result.Statuses[userID] = "created"
			}
		}
	}

	return result, nil
}

func (s *SubscriptionService) assignSubscriptionWithReuse(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	if input == nil {
		return nil, false, ErrSubscriptionNilInput
	}
	return s.withSerializedSubscriptionAssignment(ctx, input.UserID, input.GroupID, func(txCtx context.Context) (*UserSubscription, bool, error) {
		return s.assignSubscriptionWithReuseLocked(txCtx, input)
	})
}

func (s *SubscriptionService) assignSubscriptionWithReuseLocked(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	// 检查分组是否存在且为订阅类型
	group, err := s.groupRepo.GetByID(ctx, input.GroupID)
	if err != nil {
		return nil, false, fmt.Errorf("group not found: %w", err)
	}
	if !group.IsActive() || !group.IsSubscriptionType() {
		return nil, false, ErrGroupNotSubscriptionType
	}

	// 检查是否已存在订阅；若已存在，则按幂等成功返回现有订阅
	exists, err := s.userSubRepo.ExistsByUserIDAndGroupID(ctx, input.UserID, input.GroupID)
	if err != nil {
		return nil, false, err
	}
	if exists {
		sub, getErr := s.userSubRepo.GetByUserIDAndGroupID(ctx, input.UserID, input.GroupID)
		if getErr != nil {
			return nil, false, getErr
		}
		now := time.Now()
		if sub.Status == SubscriptionStatusExpired ||
			(sub.Status != SubscriptionStatusSuspended && !sub.ExpiresAt.After(now)) {
			validityDays := normalizeAssignValidityDays(input.ValidityDays)
			newExpiresAt := now.AddDate(0, 0, validityDays)
			if newExpiresAt.After(MaxExpiresAt) {
				newExpiresAt = MaxExpiresAt
			}
			renewalNotes := input.Notes
			if strings.TrimSpace(sub.Notes) == strings.TrimSpace(input.Notes) {
				renewalNotes = ""
			}
			if err := s.updateExistingSubscriptionTerm(ctx, sub, renewalNotes, now, newExpiresAt, true); err != nil {
				return nil, false, err
			}
			s.maybeInvalidateAssignmentCaches(input.UserID, input.GroupID, false)
			renewed, getErr := s.userSubRepo.GetByID(ctx, sub.ID)
			return renewed, true, getErr
		}
		if conflictReason, conflict := detectAssignSemanticConflict(sub, input); conflict {
			return nil, false, ErrSubscriptionAssignConflict.WithMetadata(map[string]string{
				"conflict_reason": conflictReason,
			})
		}
		return sub, true, nil
	}

	sub, err := s.createSubscription(ctx, input)
	if err != nil {
		return nil, false, err
	}

	// 失效订阅缓存
	s.InvalidateSubCache(input.UserID, input.GroupID)
	if s.billingCacheService != nil {
		userID, groupID := input.UserID, input.GroupID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID)
		}()
	}

	return sub, false, nil
}

func detectAssignSemanticConflict(existing *UserSubscription, input *AssignSubscriptionInput) (string, bool) {
	if existing == nil || input == nil {
		return "", false
	}

	normalizedDays := normalizeAssignValidityDays(input.ValidityDays)
	if !existing.StartsAt.IsZero() {
		expectedExpiresAt := existing.StartsAt.AddDate(0, 0, normalizedDays)
		if expectedExpiresAt.After(MaxExpiresAt) {
			expectedExpiresAt = MaxExpiresAt
		}
		if !existing.ExpiresAt.Equal(expectedExpiresAt) {
			return "validity_days_mismatch", true
		}
	}

	existingNotes := strings.TrimSpace(existing.Notes)
	inputNotes := strings.TrimSpace(input.Notes)
	if existingNotes != inputNotes {
		return "notes_mismatch", true
	}

	return "", false
}

func normalizeAssignValidityDays(days int) int {
	if days <= 0 {
		days = 30
	}
	if days > MaxValidityDays {
		days = MaxValidityDays
	}
	return days
}

// RevokeSubscription 撤销订阅
func (s *SubscriptionService) RevokeSubscription(ctx context.Context, subscriptionID int64) error {
	var sub *UserSubscription
	if err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		var err error
		sub, err = s.userSubRepo.GetByID(txCtx, subscriptionID)
		if err != nil {
			return err
		}
		if err := s.refreshSubscriptionGroupSnapshot(txCtx, sub); err != nil {
			return err
		}
		if err := s.userSubRepo.UpdateStatus(txCtx, subscriptionID, SubscriptionStatusRevoked); err != nil {
			return err
		}
		return s.userSubRepo.Delete(txCtx, subscriptionID)
	}); err != nil {
		return err
	}

	if err := s.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
		return err
	}

	return nil
}

// RestoreSubscription 恢复已撤销订阅
func (s *SubscriptionService) RestoreSubscription(ctx context.Context, subscriptionID int64) (*UserSubscription, error) {
	repo, ok := s.userSubRepo.(userSubscriptionAdminMutationRepository)
	if !ok {
		return nil, fmt.Errorf("subscription repository does not support restore")
	}

	var restored *UserSubscription
	var cacheUserID, cacheGroupID int64
	if err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		sub, err := repo.GetByIDIncludeDeleted(txCtx, subscriptionID)
		if err != nil {
			return err
		}
		if sub.DeletedAt == nil && sub.Status != SubscriptionStatusRevoked {
			return ErrSubscriptionNotRevoked
		}
		exists, err := s.userSubRepo.ExistsActiveByUserIDAndGroupID(txCtx, sub.UserID, sub.GroupID)
		if err != nil {
			return err
		}
		if exists {
			return ErrSubscriptionRestoreConflict
		}

		restoredStatus := SubscriptionStatusActive
		if !sub.ExpiresAt.After(time.Now()) {
			restoredStatus = SubscriptionStatusExpired
		}
		if restoredStatus == SubscriptionStatusActive {
			if err := s.ensureSubscriptionCanReactivate(txCtx, sub); err != nil {
				return err
			}
		}

		restored, err = repo.Restore(txCtx, subscriptionID, restoredStatus)
		if err != nil {
			return err
		}
		cacheUserID, cacheGroupID = sub.UserID, sub.GroupID
		return nil
	}); err != nil {
		return nil, err
	}

	if restored != nil {
		cacheUserID, cacheGroupID = restored.UserID, restored.GroupID
		normalizeSubscriptionSnapshot(restored)
	}
	if err := s.invalidateSubscriptionCaches(cacheUserID, cacheGroupID); err != nil {
		return nil, err
	}
	return restored, nil
}

// ExtendSubscription 调整订阅时长（正数延长，负数缩短）
func (s *SubscriptionService) ExtendSubscription(ctx context.Context, subscriptionID int64, days int) (*UserSubscription, error) {
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}

	// 限制调整天数范围
	if days > MaxValidityDays {
		days = MaxValidityDays
	}
	if days < -MaxValidityDays {
		days = -MaxValidityDays
	}

	now := time.Now()
	isExpired := !sub.ExpiresAt.After(now)
	wasHistorical := sub.UsesHistoricalGroupSnapshotAt(now)

	// 如果订阅已过期，不允许负向调整
	if isExpired && days < 0 {
		return nil, infraerrors.BadRequest("CANNOT_SHORTEN_EXPIRED", "cannot shorten an expired subscription")
	}

	// 计算新的过期时间
	var newExpiresAt time.Time
	if isExpired {
		// 已过期：从当前时间开始增加天数
		newExpiresAt = now.AddDate(0, 0, days)
	} else {
		// 未过期：从原过期时间增加/减少天数
		newExpiresAt = sub.ExpiresAt.AddDate(0, 0, days)
	}

	if newExpiresAt.After(MaxExpiresAt) {
		newExpiresAt = MaxExpiresAt
	}

	// 检查新的过期时间必须大于当前时间
	if !newExpiresAt.After(now) {
		return nil, ErrAdjustWouldExpire
	}
	if wasHistorical {
		if err := s.ensureSubscriptionCanReactivate(ctx, sub); err != nil {
			return nil, err
		}
	}

	if err := s.userSubRepo.ExtendExpiry(ctx, subscriptionID, newExpiresAt); err != nil {
		return nil, err
	}

	// 如果订阅已过期，恢复为active状态
	if sub.Status == SubscriptionStatusExpired {
		if err := s.userSubRepo.UpdateStatus(ctx, subscriptionID, SubscriptionStatusActive); err != nil {
			return nil, err
		}
	}

	// 失效订阅缓存
	s.InvalidateSubCache(sub.UserID, sub.GroupID)
	if s.billingCacheService != nil {
		userID, groupID := sub.UserID, sub.GroupID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID)
		}()
	}

	return s.userSubRepo.GetByID(ctx, subscriptionID)
}

// HardDeleteSubscription physically deletes a revoked, soft-deleted, or expired subscription record.
func (s *SubscriptionService) HardDeleteSubscription(ctx context.Context, subscriptionID int64) error {
	repo, ok := s.userSubRepo.(userSubscriptionAdminMutationRepository)
	if !ok {
		return fmt.Errorf("subscription repository does not support hard delete")
	}

	var sub *UserSubscription
	if err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		var err error
		sub, err = repo.GetByIDIncludeDeleted(txCtx, subscriptionID)
		if err != nil {
			return err
		}
		isRevokedOrDeleted := sub.DeletedAt != nil || sub.Status == SubscriptionStatusRevoked
		isExpired := !sub.ExpiresAt.After(time.Now())
		if !isRevokedOrDeleted && !isExpired {
			return ErrSubscriptionHardDeleteInvalid
		}
		return repo.HardDelete(txCtx, subscriptionID)
	}); err != nil {
		return err
	}

	return s.invalidateSubscriptionCaches(sub.UserID, sub.GroupID)
}

func (s *SubscriptionService) AdjustSubscription(ctx context.Context, subscriptionID int64, input AdjustSubscriptionInput) (*UserSubscription, error) {
	if input.Days == nil && input.DailyUsageUSD == nil && input.WeeklyUsageUSD == nil && input.MonthlyUsageUSD == nil && input.CustomUsageUSD == nil {
		return nil, ErrAdjustNoFields
	}
	for _, usage := range []*float64{input.DailyUsageUSD, input.WeeklyUsageUSD, input.MonthlyUsageUSD, input.CustomUsageUSD} {
		if usage != nil && *usage < 0 {
			return nil, infraerrors.BadRequest("INVALID_USAGE", "usage values must be non-negative")
		}
	}
	if input.Days != nil {
		if *input.Days == 0 {
			return nil, infraerrors.BadRequest("INVALID_DAYS", "days must be non-zero when provided")
		}
		if *input.Days > MaxValidityDays || *input.Days < -MaxValidityDays {
			return nil, infraerrors.BadRequest("INVALID_DAYS", "days is out of allowed range")
		}
	}
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}
	adjustNow := time.Now()
	wasHistorical := sub.UsesHistoricalGroupSnapshotAt(adjustNow)
	if wasHistorical {
		if err := s.ensureSubscriptionCanReactivate(ctx, sub); err != nil {
			return nil, err
		}
	}
	fields := UserSubscriptionMutableFields{}
	if input.Days != nil {
		days := *input.Days
		now := adjustNow
		isExpired := !sub.ExpiresAt.After(now)
		if isExpired && days < 0 {
			return nil, infraerrors.BadRequest("CANNOT_SHORTEN_EXPIRED", "cannot shorten an expired subscription")
		}
		var newExpiresAt time.Time
		if isExpired {
			newExpiresAt = now.AddDate(0, 0, days)
		} else {
			newExpiresAt = sub.ExpiresAt.AddDate(0, 0, days)
		}
		if newExpiresAt.After(MaxExpiresAt) {
			newExpiresAt = MaxExpiresAt
		}
		if !newExpiresAt.After(now) {
			return nil, ErrAdjustWouldExpire
		}
		fields.ExpiresAt = &newExpiresAt
		if sub.Status == SubscriptionStatusExpired {
			status := SubscriptionStatusActive
			fields.Status = &status
		}
	}
	if input.DailyUsageUSD != nil {
		fields.DailyUsageUSD = input.DailyUsageUSD
	}
	if input.WeeklyUsageUSD != nil {
		fields.WeeklyUsageUSD = input.WeeklyUsageUSD
	}
	if input.MonthlyUsageUSD != nil {
		fields.MonthlyUsageUSD = input.MonthlyUsageUSD
	}
	if input.CustomUsageUSD != nil {
		fields.CustomUsageUSD = input.CustomUsageUSD
	}
	if err := s.userSubRepo.UpdateMutableFields(ctx, subscriptionID, fields); err != nil {
		return nil, err
	}
	s.InvalidateSubCache(sub.UserID, sub.GroupID)
	if s.subCacheL1 != nil {
		s.subCacheL1.Wait()
	}
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateSubscription(ctx, sub.UserID, sub.GroupID)
	}
	return s.userSubRepo.GetByID(ctx, subscriptionID)
}

func (s *SubscriptionService) ensureSubscriptionCanReactivate(ctx context.Context, sub *UserSubscription) error {
	if sub == nil {
		return ErrSubscriptionNotFound
	}
	if s.groupRepo == nil {
		return ErrSubscriptionRestoreGroupInvalid
	}
	group, err := s.groupRepo.GetByID(ctx, sub.GroupID)
	if err != nil || group == nil || group.Status != StatusActive || !group.IsSubscriptionType() {
		return ErrSubscriptionRestoreGroupInvalid
	}
	return nil
}

func (s *SubscriptionService) DeductSubscriptionDaysNewest(ctx context.Context, userID, groupID int64, days int, note string) error {
	_, err := s.DeductSubscriptionDaysNewestWithSnapshots(ctx, userID, groupID, days, note)
	return err
}

func (s *SubscriptionService) DeductSubscriptionDaysNewestWithSnapshots(ctx context.Context, userID, groupID int64, days int, note string) ([]UserSubscription, error) {
	if days <= 0 {
		return nil, nil
	}
	var snapshots []UserSubscription
	err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		subs, err := s.userSubRepo.ListActiveByUserIDAndGroupID(txCtx, userID, groupID)
		if err != nil {
			return err
		}
		if len(subs) == 0 {
			return ErrSubscriptionNotFound
		}
		remainingToDeduct := days
		now := time.Now()
		trimmedNote := strings.TrimSpace(note)
		for i := len(subs) - 1; i >= 0 && remainingToDeduct > 0; i-- {
			sub := subs[i]
			updated := sub
			snapshot := cloneUserSubscription(sub)
			remainingDays := int(sub.ExpiresAt.Sub(now).Hours()/24) + 1
			if remainingDays < 1 {
				remainingDays = 1
			}
			if remainingToDeduct >= remainingDays {
				updated.Status = SubscriptionStatusExpired
				updated.ExpiresAt = now
				if trimmedNote != "" {
					updated.Notes = appendSubscriptionNotes(sub.Notes, trimmedNote)
				}
				if updated.UsesHistoricalGroupSnapshotAt(now) {
					if err := s.applyCurrentGroupSnapshot(txCtx, &updated); err != nil {
						return err
					}
				}
				snapshots = append(snapshots, snapshot)
				if err := s.userSubRepo.Update(txCtx, &updated); err != nil {
					return err
				}
				remainingToDeduct -= remainingDays
				continue
			}
			newExpiresAt := sub.ExpiresAt.AddDate(0, 0, -remainingToDeduct)
			if !newExpiresAt.After(now) {
				newExpiresAt = now
			}
			updated.ExpiresAt = newExpiresAt
			if trimmedNote != "" {
				updated.Notes = appendSubscriptionNotes(sub.Notes, trimmedNote)
			}
			if updated.UsesHistoricalGroupSnapshotAt(now) {
				if err := s.applyCurrentGroupSnapshot(txCtx, &updated); err != nil {
					return err
				}
			}
			snapshots = append(snapshots, snapshot)
			if err := s.userSubRepo.Update(txCtx, &updated); err != nil {
				return err
			}
			remainingToDeduct = 0
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateSubCache(userID, groupID)
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateSubscription(ctx, userID, groupID)
	}
	return snapshots, nil
}

func (s *SubscriptionService) RestoreSubscriptionSnapshots(ctx context.Context, snapshots []UserSubscription) error {
	if len(snapshots) == 0 {
		return nil
	}
	type pair struct {
		userID  int64
		groupID int64
	}
	seen := make(map[pair]struct{}, len(snapshots))
	err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		for i := range snapshots {
			snapshot := cloneUserSubscription(snapshots[i])
			if snapshot.ID <= 0 {
				continue
			}
			current, err := s.userSubRepo.GetByID(txCtx, snapshot.ID)
			if err != nil {
				return err
			}
			if err := s.userSubRepo.Update(txCtx, &snapshot); err != nil {
				return err
			}
			seen[pair{userID: current.UserID, groupID: current.GroupID}] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for key := range seen {
		s.InvalidateSubCache(key.userID, key.groupID)
		if s.billingCacheService != nil {
			_ = s.billingCacheService.InvalidateSubscription(ctx, key.userID, key.groupID)
		}
	}
	return nil
}

// GetByID 根据ID获取订阅
func (s *SubscriptionService) applyCurrentGroupSnapshot(ctx context.Context, sub *UserSubscription) error {
	if sub == nil {
		return nil
	}
	group := sub.Group
	if group == nil && s.groupRepo != nil {
		loaded, err := s.groupRepo.GetByID(ctx, sub.GroupID)
		if err != nil {
			return err
		}
		group = loaded
	}
	if group == nil {
		return nil
	}
	snapshotSubscriptionGroup(sub, group)
	return nil
}

func cloneUserSubscription(sub UserSubscription) UserSubscription {
	cp := sub
	cp.StackedAvailableUSD = cloneSubscriptionFloat64Ptr(sub.StackedAvailableUSD)
	cp.DailyWindowStart = cloneSubscriptionTimePtr(sub.DailyWindowStart)
	cp.WeeklyWindowStart = cloneSubscriptionTimePtr(sub.WeeklyWindowStart)
	cp.MonthlyWindowStart = cloneSubscriptionTimePtr(sub.MonthlyWindowStart)
	cp.CustomWindowStart = cloneSubscriptionTimePtr(sub.CustomWindowStart)
	cp.AssignedBy = cloneSubscriptionInt64Ptr(sub.AssignedBy)
	cp.SourceType = cloneSubscriptionStringPtr(sub.SourceType)
	cp.SourceRefID = cloneSubscriptionStringPtr(sub.SourceRefID)
	cp.SourceRedeemCodeID = cloneSubscriptionInt64Ptr(sub.SourceRedeemCodeID)
	cp.RedeemCodeSnapshot = cloneSubscriptionStringPtr(sub.RedeemCodeSnapshot)
	cp.GroupNameSnapshot = cloneSubscriptionStringPtr(sub.GroupNameSnapshot)
	cp.GroupPlatformSnapshot = cloneSubscriptionStringPtr(sub.GroupPlatformSnapshot)
	cp.GroupRateMultiplierSnapshot = cloneSubscriptionFloat64Ptr(sub.GroupRateMultiplierSnapshot)
	cp.DailyLimitUSDSnapshot = cloneSubscriptionFloat64Ptr(sub.DailyLimitUSDSnapshot)
	cp.WeeklyLimitUSDSnapshot = cloneSubscriptionFloat64Ptr(sub.WeeklyLimitUSDSnapshot)
	cp.MonthlyLimitUSDSnapshot = cloneSubscriptionFloat64Ptr(sub.MonthlyLimitUSDSnapshot)
	cp.CustomLimitHoursSnapshot = cloneSubscriptionIntPtr(sub.CustomLimitHoursSnapshot)
	cp.CustomLimitUSDSnapshot = cloneSubscriptionFloat64Ptr(sub.CustomLimitUSDSnapshot)
	cp.DeletedAt = cloneSubscriptionTimePtr(sub.DeletedAt)
	return cp
}

func cloneSubscriptionTimePtr(v *time.Time) *time.Time {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func cloneSubscriptionStringPtr(v *string) *string {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func cloneSubscriptionFloat64Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func cloneSubscriptionInt64Ptr(v *int64) *int64 {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func cloneSubscriptionIntPtr(v *int) *int {
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

func (s *SubscriptionService) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := s.userSubRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	normalizeSubscriptionSnapshot(sub)
	return sub, nil
}

func (s *SubscriptionService) GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error) {
	if repo, ok := s.userSubRepo.(userSubscriptionHistoryRepository); ok {
		sub, err := repo.GetByIDIncludeDeleted(ctx, id)
		if err != nil {
			return nil, err
		}
		normalizeSubscriptionSnapshot(sub)
		return sub, nil
	}
	return s.GetByID(ctx, id)
}

// GetActiveSubscription 获取用户对特定分组的有效订阅
// 使用 L1 缓存 + singleflight 加速中间件热路径。
// 返回缓存对象的浅拷贝，调用方可安全修改字段而不会污染缓存或触发 data race。
func (s *SubscriptionService) GetActiveSubscription(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	key := subCacheKey(userID, groupID)

	// L1 缓存命中：返回浅拷贝
	if s.subCacheL1 != nil {
		if v, ok := s.subCacheL1.Get(key); ok {
			if entry, ok := v.(*subCacheEntry); ok && entry != nil && entry.sub != nil && entry.version == s.subCacheVersion(key) {
				cp := *entry.sub
				return &cp, nil
			}
		}
	}

	// singleflight 防止并发击穿
	value, err, _ := s.subCacheGroup.Do(key, func() (any, error) {
		cacheVersion := s.subCacheVersion(key)
		subs, err := s.userSubRepo.ListActiveByUserIDAndGroupID(ctx, userID, groupID)
		if err != nil {
			return nil, err // 直接透传 repo 已翻译的错误（NotFound → ErrSubscriptionNotFound，其他错误原样返回）
		}
		// 写入 L1 缓存
		if len(subs) == 0 {
			return nil, ErrSubscriptionNotFound
		}
		sub := aggregateActiveSubscriptionsForDisplay(subs)
		entry := &subCacheEntry{sub: sub, version: cacheVersion}
		if s.subCacheL1 != nil {
			_ = s.subCacheL1.SetWithTTL(key, entry, 1, s.jitteredTTL(s.subCacheTTL))
		}
		return entry, nil
	})
	if err != nil {
		return nil, err
	}
	// singleflight 返回的也是缓存指针，需要浅拷贝
	entry, ok := value.(*subCacheEntry)
	if !ok || entry == nil || entry.sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	if entry.version != s.subCacheVersion(key) {
		return s.loadActiveSubscriptionFresh(ctx, userID, groupID, key)
	}
	sub := entry.sub
	cp := *sub
	return &cp, nil
}

func (s *SubscriptionService) loadActiveSubscriptionFresh(ctx context.Context, userID, groupID int64, key string) (*UserSubscription, error) {
	subs, err := s.userSubRepo.ListActiveByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, ErrSubscriptionNotFound
	}
	sub := aggregateActiveSubscriptionsForDisplay(subs)
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	version := s.subCacheVersion(key)
	if s.subCacheL1 != nil {
		_ = s.subCacheL1.SetWithTTL(key, &subCacheEntry{sub: sub, version: version}, 1, s.jitteredTTL(s.subCacheTTL))
	}
	cp := *sub
	return &cp, nil
}

// ListUserSubscriptions 获取用户的所有订阅
func aggregateActiveSubscriptions(subs []UserSubscription) *UserSubscription {
	return aggregateActiveSubscriptionsInternal(subs, false)
}

func aggregateActiveSubscriptionsForDisplay(subs []UserSubscription) *UserSubscription {
	return aggregateActiveSubscriptionsInternal(subs, true)
}

func aggregateActiveSubscriptionsForUserDisplay(subs []UserSubscription) *UserSubscription {
	agg := aggregateActiveSubscriptionsForDisplay(subs)
	if agg == nil {
		return agg
	}
	applyUserEffectiveDisplay(agg, subs)
	return agg
}

func aggregateActiveSubscriptionsInternal(subs []UserSubscription, normalizeWindows bool) *UserSubscription {
	if len(subs) == 0 {
		return nil
	}
	now := time.Now()
	normalized := make([]UserSubscription, 0, len(subs))
	for i := range subs {
		sub := subs[i]
		if normalizeWindows {
			normalizeSubscriptionWindowsAt(&sub, now)
		}
		normalized = append(normalized, sub)
	}
	if len(normalized) == 1 {
		return &normalized[0]
	}
	agg := normalized[0]
	agg.ID = 0
	agg.IsAggregate = true
	agg.SubscriptionCount = len(normalized)
	agg.SourceType = nil
	agg.SourceRefID = nil
	agg.SourceRedeemCodeID = nil
	agg.RedeemCodeSnapshot = nil
	agg.GroupNameSnapshot = nil
	agg.GroupPlatformSnapshot = nil
	agg.GroupRateMultiplierSnapshot = nil
	agg.DailyLimitUSDSnapshot = nil
	agg.WeeklyLimitUSDSnapshot = nil
	agg.MonthlyLimitUSDSnapshot = nil
	agg.CustomLimitHoursSnapshot = nil
	agg.CustomLimitUSDSnapshot = nil
	agg.StackedAvailableUSD = aggregateStackedAvailable(normalized)
	agg.AssignedBy = nil
	agg.AssignedAt = time.Time{}
	agg.AssignedByUser = nil
	agg.Notes = ""
	agg.StartsAt = normalized[0].StartsAt
	agg.ExpiresAt = normalized[0].ExpiresAt
	agg.DailyUsageUSD = 0
	agg.WeeklyUsageUSD = 0
	agg.MonthlyUsageUSD = 0
	agg.CustomUsageUSD = 0

	dailyLimit, dailyUnlimited := aggregateLimit(normalized, func(sub *UserSubscription) *float64 { return sub.EffectiveDailyLimitUSD(sub.Group) })
	weeklyLimit, weeklyUnlimited := aggregateLimit(normalized, func(sub *UserSubscription) *float64 { return sub.EffectiveWeeklyLimitUSD(sub.Group) })
	monthlyLimit, monthlyUnlimited := aggregateLimit(normalized, func(sub *UserSubscription) *float64 { return sub.EffectiveMonthlyLimitUSD(sub.Group) })
	customLimit, customUnlimited := aggregateLimit(normalized, func(sub *UserSubscription) *float64 { return sub.EffectiveCustomLimitUSD(sub.Group) })
	customHours := aggregateCustomHours(normalized)

	for i := range normalized {
		sub := &normalized[i]
		if sub.StartsAt.Before(agg.StartsAt) {
			agg.StartsAt = sub.StartsAt
		}
		if sub.ExpiresAt.After(agg.ExpiresAt) {
			agg.ExpiresAt = sub.ExpiresAt
		}
		agg.DailyUsageUSD += sub.DailyUsageUSD
		agg.WeeklyUsageUSD += sub.WeeklyUsageUSD
		agg.MonthlyUsageUSD += sub.MonthlyUsageUSD
		agg.CustomUsageUSD += sub.CustomUsageUSD
	}

	if !dailyUnlimited {
		agg.DailyLimitUSDSnapshot = dailyLimit
	}
	if !weeklyUnlimited {
		agg.WeeklyLimitUSDSnapshot = weeklyLimit
	}
	if !monthlyUnlimited {
		agg.MonthlyLimitUSDSnapshot = monthlyLimit
	}
	if !customUnlimited {
		agg.CustomLimitUSDSnapshot = customLimit
	}
	if customHours > 0 {
		agg.CustomLimitHoursSnapshot = &customHours
	}
	agg.DailyWindowStart = aggregateWindowStart(normalized, "daily")
	agg.WeeklyWindowStart = aggregateWindowStart(normalized, "weekly")
	agg.MonthlyWindowStart = aggregateWindowStart(normalized, "monthly")
	agg.CustomWindowStart = aggregateWindowStart(normalized, "custom")
	agg.Group = agg.EffectiveGroup(agg.Group)
	return &agg
}

type subscriptionWindowDisplayInfo struct {
	limit    *float64
	usage    float64
	resetAt  *time.Time
	duration time.Duration
	enabled  bool
}

type subscriptionCardDisplayAvailability struct {
	available float64
	unlimited bool
	resetsAt  *time.Time
	windows   map[string]subscriptionWindowDisplayInfo
}

func applyUserEffectiveDisplay(agg *UserSubscription, subs []UserSubscription) {
	if agg == nil || len(subs) == 0 {
		return
	}
	now := time.Now()
	normalized := make([]UserSubscription, 0, len(subs))
	for i := range subs {
		sub := subs[i]
		normalizeSubscriptionWindowsAt(&sub, now)
		normalized = append(normalized, sub)
	}

	cards := make([]subscriptionCardDisplayAvailability, 0, len(normalized))
	totalAvailable := 0.0
	hasUnlimited := false
	var effectiveResetsAt *time.Time
	for i := range normalized {
		card := subscriptionDisplayAvailability(&normalized[i])
		cards = append(cards, card)
		if card.unlimited {
			hasUnlimited = true
		} else {
			totalAvailable += card.available
		}
		if card.resetsAt != nil && (effectiveResetsAt == nil || card.resetsAt.Before(*effectiveResetsAt)) {
			v := *card.resetsAt
			effectiveResetsAt = &v
		}
	}
	if hasUnlimited {
		agg.EffectiveAvailableUSD = nil
		effectiveResetsAt = nil
	} else {
		agg.EffectiveAvailableUSD = subscriptionFloatPtr(totalAvailable)
	}
	applyUserDisplayAggregateLimits(agg, cards, hasUnlimited)
	agg.EffectiveResetsAt = effectiveResetsAt
	agg.EffectiveDailyUsageUSD = aggregateEffectiveWindowUsage(cards, "daily", agg.EffectiveDailyLimitUSD(agg.Group))
	agg.EffectiveWeeklyUsageUSD = aggregateEffectiveWindowUsage(cards, "weekly", agg.EffectiveWeeklyLimitUSD(agg.Group))
	agg.EffectiveMonthlyUsageUSD = aggregateEffectiveWindowUsage(cards, "monthly", agg.EffectiveMonthlyLimitUSD(agg.Group))
	agg.EffectiveDailyResetsAt = aggregateEffectiveWindowResetTime(cards, "daily")
	agg.EffectiveWeeklyResetsAt = aggregateEffectiveWindowResetTime(cards, "weekly")
	agg.EffectiveMonthlyResetsAt = aggregateEffectiveWindowResetTime(cards, "monthly")
	if agg.EffectiveCustomLimitHours(agg.Group) > 0 {
		agg.EffectiveCustomUsageUSD = aggregateEffectiveWindowUsage(cards, "custom", agg.EffectiveCustomLimitUSD(agg.Group))
		agg.EffectiveCustomResetsAt = aggregateEffectiveWindowResetTime(cards, "custom")
	}
}

func applyUserDisplayAggregateLimits(agg *UserSubscription, cards []subscriptionCardDisplayAvailability, hasUnlimited bool) {
	if agg == nil {
		return
	}
	group := agg.EffectiveGroup(agg.Group)
	if group == nil {
		group = &Group{ID: agg.GroupID, SubscriptionType: SubscriptionTypeSubscription}
	}
	groupCopy := *group
	if hasUnlimited {
		agg.DailyLimitUSDSnapshot = nil
		agg.WeeklyLimitUSDSnapshot = nil
		agg.MonthlyLimitUSDSnapshot = nil
		agg.CustomLimitHoursSnapshot = nil
		agg.CustomLimitUSDSnapshot = nil
		groupCopy.DailyLimitUSD = nil
		groupCopy.WeeklyLimitUSD = nil
		groupCopy.MonthlyLimitUSD = nil
		groupCopy.CustomLimitHours = 0
		groupCopy.CustomLimitUSD = nil
		agg.Group = &groupCopy
		return
	}

	agg.DailyLimitUSDSnapshot = aggregateEnabledWindowLimit(cards, "daily")
	agg.WeeklyLimitUSDSnapshot = aggregateEnabledWindowLimit(cards, "weekly")
	agg.MonthlyLimitUSDSnapshot = aggregateEnabledWindowLimit(cards, "monthly")
	agg.CustomLimitUSDSnapshot = aggregateEnabledWindowLimit(cards, "custom")
	if agg.CustomLimitUSDSnapshot == nil {
		agg.CustomLimitHoursSnapshot = nil
	}

	groupCopy.DailyLimitUSD = agg.DailyLimitUSDSnapshot
	groupCopy.WeeklyLimitUSD = agg.WeeklyLimitUSDSnapshot
	groupCopy.MonthlyLimitUSD = agg.MonthlyLimitUSDSnapshot
	if agg.CustomLimitUSDSnapshot == nil {
		groupCopy.CustomLimitHours = 0
		groupCopy.CustomLimitUSD = nil
	} else {
		groupCopy.CustomLimitUSD = agg.CustomLimitUSDSnapshot
		if agg.CustomLimitHoursSnapshot != nil {
			groupCopy.CustomLimitHours = *agg.CustomLimitHoursSnapshot
		}
	}
	agg.Group = &groupCopy
}

func aggregateEnabledWindowLimit(cards []subscriptionCardDisplayAvailability, window string) *float64 {
	var sum float64
	found := false
	for _, card := range cards {
		info, ok := card.windows[window]
		if !ok || !info.enabled || !hasPositiveLimit(info.limit) {
			continue
		}
		found = true
		sum += *info.limit
	}
	if !found {
		return nil
	}
	return subscriptionFloatPtr(sum)
}

func subscriptionDisplayAvailability(sub *UserSubscription) subscriptionCardDisplayAvailability {
	out := subscriptionCardDisplayAvailability{
		available: math.Inf(1),
		unlimited: true,
		windows:   make(map[string]subscriptionWindowDisplayInfo, 4),
	}
	if sub == nil {
		out.available = 0
		out.unlimited = false
		return out
	}
	group := sub.EffectiveGroup(sub.Group)
	addWindow := func(name string, limit *float64, usage float64, resetAt *time.Time, duration time.Duration, enabled bool) {
		if !enabled || !hasPositiveLimit(limit) {
			out.windows[name] = subscriptionWindowDisplayInfo{limit: limit, usage: usage, resetAt: resetAt, duration: duration, enabled: false}
			return
		}
		out.unlimited = false
		remaining := *limit - usage
		if remaining < out.available {
			out.available = remaining
		}
		out.windows[name] = subscriptionWindowDisplayInfo{
			limit:    limit,
			usage:    usage,
			resetAt:  resetAt,
			duration: duration,
			enabled:  true,
		}
	}
	addWindow("daily", sub.EffectiveDailyLimitUSD(group), sub.DailyUsageUSD, sub.EffectiveDisplayDailyResetTime(), subscriptionDailyWindow, true)
	addWindow("weekly", sub.EffectiveWeeklyLimitUSD(group), sub.WeeklyUsageUSD, sub.WeeklyResetTime(), subscriptionWeeklyWindow, true)
	addWindow("monthly", sub.EffectiveMonthlyLimitUSD(group), sub.MonthlyUsageUSD, sub.MonthlyResetTime(), subscriptionMonthlyWindow, true)
	addWindow("custom", sub.EffectiveCustomLimitUSD(group), sub.CustomUsageUSD, sub.CustomResetTime(group), customSubscriptionWindow(group), sub.EffectiveCustomLimitHours(group) > 0)
	if out.unlimited {
		out.available = math.Inf(1)
		return out
	}
	if out.available < 0 {
		out.available = 0
	}
	out.resetsAt = cardEffectiveResetTime(out.windows, out.available)
	return out
}

func cardEffectiveResetTime(windows map[string]subscriptionWindowDisplayInfo, available float64) *time.Time {
	var latest *time.Time
	for _, window := range windows {
		if !window.enabled {
			continue
		}
		remaining := 0.0
		if hasPositiveLimit(window.limit) {
			remaining = *window.limit - window.usage
		}
		if remaining > available+1e-9 {
			continue
		}
		if window.resetAt == nil {
			return nil
		}
		if latest == nil || window.resetAt.After(*latest) {
			v := *window.resetAt
			latest = &v
		}
	}
	return latest
}

func aggregateEffectiveWindowResetTime(cards []subscriptionCardDisplayAvailability, window string) *time.Time {
	var earliest *time.Time
	for _, card := range cards {
		resetAt := cardEffectiveWindowResetTime(card, window)
		if resetAt == nil {
			continue
		}
		if earliest == nil || resetAt.Before(*earliest) {
			v := *resetAt
			earliest = &v
		}
	}
	return earliest
}

func cardEffectiveWindowResetTime(card subscriptionCardDisplayAvailability, target string) *time.Time {
	targetInfo, ok := card.windows[target]
	if !ok || !targetInfo.enabled || math.IsInf(card.available, 1) {
		return nil
	}

	eligible := displayBlockingWindowsForTarget(card.windows, target)
	targetRemaining := windowRemaining(targetInfo)
	currentAvailable := effectiveDisplayAvailableForWindow(card, target)
	if currentAvailable < 0 {
		currentAvailable = 0
	}

	if targetRemaining > currentAvailable+1e-9 {
		// The target window itself is not part of the current bottleneck.
		// Its displayed recovery waits for same-or-longer windows that currently block it.
		return cardEffectiveResetTime(eligible, currentAvailable)
	}

	return cardRecoveryAfterWindowReset(eligible, target)
}

func cardRecoveryAfterWindowReset(windows map[string]subscriptionWindowDisplayInfo, target string) *time.Time {
	targetInfo := windows[target]
	if targetInfo.resetAt == nil {
		return nil
	}
	recoveryAt := *targetInfo.resetAt
	var latest *time.Time
	setLatest := func(resetAt *time.Time) bool {
		if resetAt == nil {
			return false
		}
		if latest == nil || resetAt.After(*latest) {
			v := *resetAt
			latest = &v
		}
		return true
	}
	if !setLatest(targetInfo.resetAt) {
		return nil
	}

	for name, info := range windows {
		if name == target || !info.enabled {
			continue
		}
		remaining := windowRemaining(info)
		if info.resetAt == nil {
			if remaining <= 1e-9 {
				return nil
			}
			continue
		}
		if !recoveryAt.Before(*info.resetAt) {
			remaining = *info.limit
		}
		if remaining > 1e-9 {
			continue
		}
		if !setLatest(info.resetAt) {
			return nil
		}
	}
	return latest
}

func windowRemaining(info subscriptionWindowDisplayInfo) float64 {
	if !info.enabled || !hasPositiveLimit(info.limit) {
		return math.Inf(1)
	}
	remaining := *info.limit - info.usage
	if remaining < 0 {
		return 0
	}
	return remaining
}

func aggregateEffectiveWindowUsage(cards []subscriptionCardDisplayAvailability, window string, limit *float64) *float64 {
	if !hasPositiveLimit(limit) {
		return nil
	}
	var available float64
	found := false
	for _, card := range cards {
		info, ok := card.windows[window]
		if !ok || !info.enabled {
			continue
		}
		if math.IsInf(card.available, 1) {
			return subscriptionFloatPtr(0)
		}
		found = true
		available += effectiveDisplayAvailableForWindow(card, window)
	}
	if !found {
		return nil
	}
	if available < 0 {
		available = 0
	}
	if available > *limit {
		available = *limit
	}
	usage := *limit - available
	if usage < 0 {
		usage = 0
	}
	return subscriptionFloatPtr(usage)
}

func effectiveDisplayAvailableForWindow(card subscriptionCardDisplayAvailability, target string) float64 {
	targetInfo, ok := card.windows[target]
	if !ok || !targetInfo.enabled || !hasPositiveLimit(targetInfo.limit) {
		return math.Inf(1)
	}
	available := windowRemaining(targetInfo)
	for _, info := range displayBlockingWindowsForTarget(card.windows, target) {
		if !info.enabled {
			continue
		}
		remaining := windowRemaining(info)
		if remaining < available {
			available = remaining
		}
	}
	if available < 0 {
		return 0
	}
	if available > *targetInfo.limit {
		return *targetInfo.limit
	}
	return available
}

func displayBlockingWindowsForTarget(windows map[string]subscriptionWindowDisplayInfo, target string) map[string]subscriptionWindowDisplayInfo {
	targetInfo, ok := windows[target]
	if !ok || !targetInfo.enabled {
		return nil
	}
	eligible := make(map[string]subscriptionWindowDisplayInfo, len(windows))
	for name, info := range windows {
		if !info.enabled {
			continue
		}
		if info.duration+time.Nanosecond < targetInfo.duration {
			continue
		}
		eligible[name] = info
	}
	return eligible
}

func subscriptionDisplayUsageForWindow(sub *UserSubscription, window string) float64 {
	if sub == nil {
		return 0
	}
	switch window {
	case "daily":
		if sub.EffectiveDailyUsageUSD != nil {
			return *sub.EffectiveDailyUsageUSD
		}
		return sub.DailyUsageUSD
	case "weekly":
		if sub.EffectiveWeeklyUsageUSD != nil {
			return *sub.EffectiveWeeklyUsageUSD
		}
		return sub.WeeklyUsageUSD
	case "monthly":
		if sub.EffectiveMonthlyUsageUSD != nil {
			return *sub.EffectiveMonthlyUsageUSD
		}
		return sub.MonthlyUsageUSD
	case "custom":
		if sub.EffectiveCustomUsageUSD != nil {
			return *sub.EffectiveCustomUsageUSD
		}
		return sub.CustomUsageUSD
	default:
		return 0
	}
}

func subscriptionFloatPtr(v float64) *float64 {
	return &v
}

func aggregateStackedAvailable(subs []UserSubscription) *float64 {
	var total float64
	for i := range subs {
		available := subscriptionBillingAvailable(&subs[i])
		if math.IsInf(available, 1) {
			return nil
		}
		total += available
	}
	return &total
}

func subscriptionBillingAvailable(sub *UserSubscription) float64 {
	if sub == nil {
		return 0
	}
	group := sub.EffectiveGroup(sub.Group)
	available := math.Inf(1)
	applyLimit := func(limit *float64, usage float64) {
		if limit == nil || *limit <= 0 {
			return
		}
		remaining := *limit - usage
		if remaining < available {
			available = remaining
		}
	}
	applyLimit(sub.EffectiveDailyLimitUSD(group), sub.DailyUsageUSD)
	applyLimit(sub.EffectiveWeeklyLimitUSD(group), sub.WeeklyUsageUSD)
	applyLimit(sub.EffectiveMonthlyLimitUSD(group), sub.MonthlyUsageUSD)
	if sub.EffectiveCustomLimitHours(group) > 0 {
		applyLimit(sub.EffectiveCustomLimitUSD(group), sub.CustomUsageUSD)
	}
	if available < 0 {
		return 0
	}
	return available
}

func aggregateLimit(subs []UserSubscription, get func(*UserSubscription) *float64) (*float64, bool) {
	var sum float64
	for i := range subs {
		limit := get(&subs[i])
		if limit == nil || *limit <= 0 {
			return nil, true
		}
		sum += *limit
	}
	return &sum, false
}

func aggregateCustomHours(subs []UserSubscription) int {
	hours := 0
	for i := range subs {
		h := subs[i].EffectiveCustomLimitHours(subs[i].Group)
		if h <= 0 {
			continue
		}
		if hours == 0 || h < hours {
			hours = h
		}
	}
	return hours
}

func aggregateWindowStart(subs []UserSubscription, window string) *time.Time {
	var fallback *time.Time
	var chosen *time.Time
	var chosenReset time.Time
	for i := range subs {
		sub := &subs[i]
		start, usage, reset := subscriptionWindowInfo(sub, window)
		if start == nil {
			continue
		}
		if fallback == nil || start.Before(*fallback) {
			v := *start
			fallback = &v
		}
		if usage <= 0 || reset == nil {
			continue
		}
		if chosen == nil || reset.Before(chosenReset) {
			v := *start
			chosen = &v
			chosenReset = *reset
		}
	}
	if chosen != nil {
		return chosen
	}
	return fallback
}

func subscriptionWindowInfo(sub *UserSubscription, window string) (*time.Time, float64, *time.Time) {
	switch window {
	case "daily":
		return sub.DailyWindowStart, sub.DailyUsageUSD, sub.DailyResetTime()
	case "weekly":
		return sub.WeeklyWindowStart, sub.WeeklyUsageUSD, sub.WeeklyResetTime()
	case "monthly":
		return sub.MonthlyWindowStart, sub.MonthlyUsageUSD, sub.MonthlyResetTime()
	case "custom":
		return sub.CustomWindowStart, sub.CustomUsageUSD, sub.CustomResetTime(sub.Group)
	default:
		return nil, 0, nil
	}
}

func aggregateActiveByGroupForUserDisplay(subs []UserSubscription) []UserSubscription {
	return aggregateActiveByGroupInternal(subs, true, true)
}

func aggregateUserVisibleByGroupForUserDisplay(subs []UserSubscription) []UserSubscription {
	return aggregateByGroupAndStatusForDisplay(subs, true, true)
}

func aggregateActiveByGroupInternal(subs []UserSubscription, normalizeWindows bool, userDisplay bool) []UserSubscription {
	if len(subs) == 0 {
		return subs
	}
	now := time.Now()
	grouped := make(map[int64][]UserSubscription)
	order := make([]int64, 0)
	for i := range subs {
		sub := subs[i]
		if sub.Status == SubscriptionStatusActive && sub.ExpiresAt.After(now) {
			if _, ok := grouped[sub.GroupID]; !ok {
				order = append(order, sub.GroupID)
			}
			grouped[sub.GroupID] = append(grouped[sub.GroupID], sub)
			continue
		}
		key := -sub.ID
		order = append(order, key)
		grouped[key] = []UserSubscription{sub}
	}
	out := make([]UserSubscription, 0, len(order))
	for _, key := range order {
		items := grouped[key]
		if key > 0 {
			var agg *UserSubscription
			if userDisplay {
				agg = aggregateActiveSubscriptionsForUserDisplay(items)
			} else if normalizeWindows {
				agg = aggregateActiveSubscriptionsForDisplay(items)
			} else {
				agg = aggregateActiveSubscriptions(items)
			}
			if agg != nil {
				out = append(out, *agg)
			}
			continue
		}
		out = append(out, items[0])
	}
	return out
}

func aggregateByGroupAndStatusForDisplay(subs []UserSubscription, normalizeWindows bool, userDisplay bool) []UserSubscription {
	if len(subs) == 0 {
		return subs
	}
	now := time.Now()
	grouped := make(map[string][]UserSubscription)
	order := make([]string, 0, len(subs))
	for i := range subs {
		sub := subs[i]
		status := sub.Status
		if status == SubscriptionStatusActive && !sub.ExpiresAt.After(now) {
			status = SubscriptionStatusExpired
			sub.Status = status
		}
		key := fmt.Sprintf("%d:%s", sub.GroupID, status)
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], sub)
	}
	out := make([]UserSubscription, 0, len(order))
	for _, key := range order {
		items := grouped[key]
		var agg *UserSubscription
		isActiveBucket := len(items) > 0 && items[0].Status == SubscriptionStatusActive && items[0].ExpiresAt.After(now)
		if userDisplay && isActiveBucket {
			agg = aggregateActiveSubscriptionsForUserDisplay(items)
		} else if normalizeWindows && isActiveBucket {
			agg = aggregateActiveSubscriptionsForDisplay(items)
		} else {
			agg = aggregateActiveSubscriptions(items)
		}
		if agg != nil {
			if agg.Status == SubscriptionStatusActive && !agg.ExpiresAt.After(now) {
				agg.Status = SubscriptionStatusExpired
			}
			out = append(out, *agg)
		}
	}
	return out
}

func (s *SubscriptionService) ListUserSubscriptionRecords(ctx context.Context, userID int64) ([]UserSubscription, error) {
	var (
		subs []UserSubscription
		err  error
	)
	if repo, ok := s.userSubRepo.(userSubscriptionHistoryRepository); ok {
		subs, err = repo.ListByUserIDIncludeDeleted(ctx, userID)
	} else {
		subs, err = s.userSubRepo.ListByUserID(ctx, userID)
	}
	if err != nil {
		return nil, err
	}
	normalizeSubscriptionStatus(subs)
	return subs, nil
}

func (s *SubscriptionService) ListUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	subs, err := s.userSubRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return aggregateUserVisibleByGroupForUserDisplay(subs), nil
}

// ListActiveUserSubscriptions 获取用户的所有有效订阅
func (s *SubscriptionService) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	normalizeExpiredWindows(subs)
	return aggregateActiveByGroupForUserDisplay(subs), nil
}

// ListGroupSubscriptions 获取分组的所有订阅
func (s *SubscriptionService) ListGroupSubscriptions(ctx context.Context, groupID int64, page, pageSize int) ([]UserSubscription, *pagination.PaginationResult, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	subs, pag, err := s.userSubRepo.ListByGroupID(ctx, groupID, params)
	if err != nil {
		return nil, nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, pag, nil
}

// List 获取所有订阅（分页，支持筛选和排序）
func (s *SubscriptionService) List(ctx context.Context, page, pageSize int, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	subs, pag, err := s.userSubRepo.List(ctx, params, userID, groupID, status, platform, sortBy, sortOrder)
	if err != nil {
		return nil, nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, pag, nil
}

// normalizeExpiredWindows 将已过期窗口的数据清零（仅影响返回数据，不影响数据库）
// 这确保前端显示正确的当前窗口状态，而不是过期窗口的历史数据
func normalizeSubscriptionSnapshot(sub *UserSubscription) {
	if sub == nil {
		return
	}
	if sub.DeletedAt != nil {
		sub.Status = SubscriptionStatusRevoked
		return
	}
	now := time.Now()
	if sub.Status == SubscriptionStatusActive && !sub.ExpiresAt.After(now) {
		sub.Status = SubscriptionStatusExpired
		return
	}
	if sub.Status == SubscriptionStatusActive {
		normalizeSubscriptionWindowsAt(sub, now)
	}
}

func normalizeSubscriptionWindowsAt(sub *UserSubscription, now time.Time) {
	if sub == nil {
		return
	}
	// 日窗口过期：清零展示数据
	if windowStart, ok := sub.automaticWindowStartAt(sub.DailyWindowStart, subscriptionDailyWindow, now); !sub.HasOneTimeDailyQuota() && ok {
		sub.DailyWindowStart = &windowStart
		sub.DailyUsageUSD = 0
	}
	// 周窗口过期：清零展示数据
	if windowStart, ok := sub.automaticWindowStartAt(sub.WeeklyWindowStart, subscriptionWeeklyWindow, now); ok {
		sub.WeeklyWindowStart = &windowStart
		sub.WeeklyUsageUSD = 0
	}
	// 月窗口过期：清零展示数据
	if windowStart, ok := sub.automaticWindowStartAt(sub.MonthlyWindowStart, subscriptionMonthlyWindow, now); ok {
		sub.MonthlyWindowStart = &windowStart
		sub.MonthlyUsageUSD = 0
	}
	if effectiveGroup := sub.EffectiveGroup(sub.Group); sub.NeedsCustomResetAt(effectiveGroup, now) {
		period := customSubscriptionWindowHours(sub.EffectiveCustomLimitHours(effectiveGroup))
		windowStart := currentSubscriptionWindowStart(*sub.CustomWindowStart, period, now)
		sub.CustomWindowStart = &windowStart
		sub.CustomUsageUSD = 0
	}
}

func normalizeExpiredWindows(subs []UserSubscription) {
	normalizeExpiredWindowsAt(subs, time.Now())
}

func normalizeExpiredWindowsAt(subs []UserSubscription, now time.Time) {
	for i := range subs {
		normalizeSubscriptionWindowsAt(&subs[i], now)
	}
}

// normalizeSubscriptionStatus 根据实际过期时间修正状态（仅影响返回数据，不影响数据库）
// 这确保前端显示正确的状态，即使定时任务尚未更新数据库
func normalizeSubscriptionStatus(subs []UserSubscription) {
	now := time.Now()
	for i := range subs {
		sub := &subs[i]
		if sub.DeletedAt != nil {
			sub.Status = SubscriptionStatusRevoked
			continue
		}
		if sub.Status == SubscriptionStatusActive && !sub.ExpiresAt.After(now) {
			sub.Status = SubscriptionStatusExpired
		}
	}
}

// currentSubscriptionWindowStart advances a rolling window to the period containing now.
func currentSubscriptionWindowStart(start time.Time, period time.Duration, now time.Time) time.Time {
	if start.IsZero() || period <= 0 {
		return now
	}
	if now.Before(start.Add(period)) {
		return start
	}
	elapsed := now.Sub(start)
	periods := int64(elapsed / period)
	return start.Add(time.Duration(periods) * period)
}

// CheckAndActivateWindow 检查并激活窗口（首次使用时）
func (s *SubscriptionService) CheckAndActivateWindow(ctx context.Context, sub *UserSubscription) error {
	return s.checkAndActivateWindowAt(ctx, sub, s.now())
}

func (s *SubscriptionService) checkAndActivateWindowAt(ctx context.Context, sub *UserSubscription, now time.Time) error {
	if sub.IsWindowActivated() {
		return nil
	}

	windowStart := now
	if err := s.userSubRepo.ActivateWindows(ctx, sub.ID, windowStart); err != nil {
		return err
	}
	dailyWindowStart := windowStart
	weeklyWindowStart := windowStart
	monthlyWindowStart := windowStart
	customWindowStart := windowStart
	sub.DailyWindowStart = &dailyWindowStart
	sub.WeeklyWindowStart = &weeklyWindowStart
	sub.MonthlyWindowStart = &monthlyWindowStart
	sub.CustomWindowStart = &customWindowStart
	return nil
}

// AdminResetQuota manually resets the selected usage windows.
// Uses the reset time as the new window start.
func (s *SubscriptionService) AdminResetQuota(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly bool, resetCustom ...bool) (*UserSubscription, error) {
	custom := len(resetCustom) > 0 && resetCustom[0]
	if !resetDaily && !resetWeekly && !resetMonthly && !custom {
		return nil, ErrInvalidInput
	}
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	windowStart := s.now()
	if err := s.userSubRepo.ResetUsageWindows(ctx, sub.ID, resetDaily, resetWeekly, resetMonthly, windowStart); err != nil {
		return nil, err
	}
	if custom {
		if err := s.userSubRepo.ResetCustomUsage(ctx, sub.ID, windowStart); err != nil {
			return nil, err
		}
	}
	// Invalidate L1 ristretto cache. Ristretto's Del() is asynchronous by design,
	// so call Wait() immediately after to flush pending operations and guarantee
	// the deleted key is not returned on the very next Get() call.
	s.InvalidateSubCacheSync(sub.UserID, sub.GroupID)
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateSubscription(ctx, sub.UserID, sub.GroupID)
	}
	// Return the refreshed subscription from DB
	return s.userSubRepo.GetByID(ctx, subscriptionID)
}

// CheckAndResetWindows 检查并重置过期的窗口
func (s *SubscriptionService) CheckAndResetWindows(ctx context.Context, sub *UserSubscription) error {
	now := s.now()
	needsInvalidateCache := false

	if windowStart, ok := sub.automaticWindowStartAt(sub.DailyWindowStart, subscriptionDailyWindow, now); !sub.HasOneTimeDailyQuota() && ok {
		expectedWindowStart := sub.DailyWindowStart
		if roller, ok := s.userSubRepo.(dailyUsageWindowRoller); ok && !sub.UpdatedAt.IsZero() {
			if _, err := roller.RollDailyUsageWindow(ctx, sub.ID, *expectedWindowStart, windowStart, sub.DailyUsageUSD, sub.UpdatedAt); err != nil {
				return err
			}
		} else if err := s.userSubRepo.ResetDailyUsage(ctx, sub.ID, expectedWindowStart, windowStart); err != nil {
			return err
		}
		sub.DailyWindowStart = &windowStart
		sub.DailyUsageUSD = 0
		needsInvalidateCache = true
	}

	if windowStart, ok := sub.automaticWindowStartAt(sub.WeeklyWindowStart, subscriptionWeeklyWindow, now); ok {
		expectedWindowStart := sub.WeeklyWindowStart
		if roller, ok := s.userSubRepo.(weeklyUsageWindowRoller); ok && !sub.UpdatedAt.IsZero() {
			if _, err := roller.RollWeeklyUsageWindow(ctx, sub.ID, *expectedWindowStart, windowStart, sub.WeeklyUsageUSD, sub.UpdatedAt); err != nil {
				return err
			}
		} else if err := s.userSubRepo.ResetWeeklyUsage(ctx, sub.ID, expectedWindowStart, windowStart); err != nil {
			return err
		}
		sub.WeeklyWindowStart = &windowStart
		sub.WeeklyUsageUSD = 0
		needsInvalidateCache = true
	}

	if windowStart, ok := sub.automaticWindowStartAt(sub.MonthlyWindowStart, subscriptionMonthlyWindow, now); ok {
		expectedWindowStart := sub.MonthlyWindowStart
		if roller, ok := s.userSubRepo.(monthlyUsageWindowRoller); ok && !sub.UpdatedAt.IsZero() {
			if _, err := roller.RollMonthlyUsageWindow(ctx, sub.ID, *expectedWindowStart, windowStart, sub.MonthlyUsageUSD, sub.UpdatedAt); err != nil {
				return err
			}
		} else if err := s.userSubRepo.ResetMonthlyUsage(ctx, sub.ID, expectedWindowStart, windowStart); err != nil {
			return err
		}
		sub.MonthlyWindowStart = &windowStart
		sub.MonthlyUsageUSD = 0
		needsInvalidateCache = true
	}
	if sub.CustomWindowStart != nil {
		group := sub.Group
		if group == nil && s.groupRepo != nil {
			loaded, err := s.groupRepo.GetByID(ctx, sub.GroupID)
			if err != nil && !sub.HasGroupSnapshot() {
				return err
			}
			if err == nil {
				group = loaded
			}
		}
		effectiveGroup := sub.EffectiveGroup(group)
		if sub.NeedsCustomResetAt(effectiveGroup, now) {
			oldWindowStart := *sub.CustomWindowStart
			period := customSubscriptionWindowHours(sub.EffectiveCustomLimitHours(effectiveGroup))
			windowStart := currentSubscriptionWindowStart(oldWindowStart, period, now)
			if roller, ok := s.userSubRepo.(customUsageWindowRoller); ok && !sub.UpdatedAt.IsZero() {
				if _, err := roller.RollCustomUsageWindow(ctx, sub.ID, oldWindowStart, windowStart, sub.CustomUsageUSD, sub.UpdatedAt); err != nil {
					return err
				}
			} else {
				if err := s.userSubRepo.ResetCustomUsage(ctx, sub.ID, windowStart); err != nil {
					return err
				}
			}
			sub.CustomWindowStart = &windowStart
			sub.CustomUsageUSD = 0
			needsInvalidateCache = true
		}
	}

	// 如果有窗口被重置，失效缓存以保持一致性
	if needsInvalidateCache {
		s.InvalidateSubCache(sub.UserID, sub.GroupID)
		if s.billingCacheService != nil {
			_ = s.billingCacheService.InvalidateSubscription(ctx, sub.UserID, sub.GroupID)
		}
	}

	return nil
}

// EnsureWindowMaintenance advances expired usage windows before a request is
// allowed to proceed. It returns a fresh database snapshot because a competing
// request may have won one of the conditional resets.
func (s *SubscriptionService) EnsureWindowMaintenance(ctx context.Context, sub *UserSubscription) (*UserSubscription, error) {
	if sub == nil {
		return nil, ErrSubscriptionNilInput
	}
	if !sub.IsWindowActivated() {
		if err := s.CheckAndActivateWindow(ctx, sub); err != nil {
			return nil, err
		}
	}
	if err := s.CheckAndResetWindows(ctx, sub); err != nil {
		return nil, err
	}

	// GetByID bypasses the service caches. This prevents a stale loser of the
	// CAS from validating limits against zeroed in-memory usage.
	refreshed, err := s.userSubRepo.GetByID(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	s.InvalidateSubCacheSync(sub.UserID, sub.GroupID)
	return refreshed, nil
}

// CheckUsageLimits 检查使用限额（返回错误如果超限）
// 用于中间件的快速预检查，additionalCost 通常为 0
func (s *SubscriptionService) CheckUsageLimits(ctx context.Context, sub *UserSubscription, group *Group, additionalCost float64) error {
	if !sub.CheckDailyLimit(group, additionalCost) {
		return ErrDailyLimitExceeded
	}
	if !sub.CheckWeeklyLimit(group, additionalCost) {
		return ErrWeeklyLimitExceeded
	}
	if !sub.CheckMonthlyLimit(group, additionalCost) {
		return ErrMonthlyLimitExceeded
	}
	if !sub.CheckCustomLimit(group, additionalCost) {
		return ErrCustomLimitExceeded
	}
	return nil
}

// ValidateAndCheckLimits 合并验证+限额检查（中间件热路径专用）
// 仅做内存检查，不触发 DB 写入。调用方必须在放行请求前同步完成窗口维护。
// 返回 needsMaintenance 表示是否需要执行窗口维护并回读数据库快照。
func (s *SubscriptionService) ValidateAndCheckLimits(sub *UserSubscription, group *Group) (needsMaintenance bool, err error) {
	if sub == nil {
		return false, ErrSubscriptionNotFound
	}
	group = sub.EffectiveGroup(group)
	now := s.now()
	// 1. 验证订阅状态
	if sub.Status == SubscriptionStatusExpired {
		return false, ErrSubscriptionExpired
	}
	if sub.Status == SubscriptionStatusSuspended {
		return false, ErrSubscriptionSuspended
	}
	if !sub.ExpiresAt.After(now) {
		return false, ErrSubscriptionExpired
	}
	if sub.IsAggregate {
		if sub.StackedAvailableUSD != nil && *sub.StackedAvailableUSD <= 0 {
			return false, ErrDailyLimitExceeded
		}
		if !sub.CheckDailyLimit(group, 0) {
			return false, ErrDailyLimitExceeded
		}
		if !sub.CheckWeeklyLimit(group, 0) {
			return false, ErrWeeklyLimitExceeded
		}
		if !sub.CheckMonthlyLimit(group, 0) {
			return false, ErrMonthlyLimitExceeded
		}
		if !sub.CheckCustomLimit(group, 0) {
			return false, ErrCustomLimitExceeded
		}
		return false, nil
	}

	// 2. 内存中修正过期窗口的用量，确保预检查不会误拒绝用户。
	//    调用方随后同步推进 DB 窗口，并用回读快照重新校验。
	if sub.canAutomaticallyResetDailyAt(now) {
		sub.DailyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.canAutomaticallyResetWeeklyAt(now) {
		sub.WeeklyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.canAutomaticallyResetMonthlyAt(now) {
		sub.MonthlyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.NeedsCustomResetAt(group, now) {
		sub.CustomUsageUSD = 0
		needsMaintenance = true
	}
	if !sub.IsWindowActivated() {
		needsMaintenance = true
	}

	// 3. 检查用量限额
	if !sub.CheckDailyLimit(group, 0) {
		return needsMaintenance, ErrDailyLimitExceeded
	}
	if !sub.CheckWeeklyLimit(group, 0) {
		return needsMaintenance, ErrWeeklyLimitExceeded
	}
	if !sub.CheckMonthlyLimit(group, 0) {
		return needsMaintenance, ErrMonthlyLimitExceeded
	}
	if !sub.CheckCustomLimit(group, 0) {
		return needsMaintenance, ErrCustomLimitExceeded
	}

	return needsMaintenance, nil
}

// DoWindowMaintenance 异步执行窗口维护（激活+重置）
// 使用独立 context，不受请求取消影响。
// 注意：此方法仅在 ValidateAndCheckLimits 返回 needsMaintenance=true 时调用，
// 而 IsExpired()=true 的订阅在 ValidateAndCheckLimits 中已被拦截返回错误，
// 因此进入此方法的订阅一定未过期，无需处理过期状态同步。
func (s *SubscriptionService) DoWindowMaintenance(sub *UserSubscription) {
	if s == nil {
		return
	}
	if s.maintenanceQueue != nil {
		err := s.maintenanceQueue.TryEnqueue(func() {
			s.doWindowMaintenance(sub)
		})
		if err != nil {
			log.Printf("Subscription maintenance enqueue failed: %v", err)
		}
		return
	}

	s.doWindowMaintenance(sub)
}

func (s *SubscriptionService) doWindowMaintenance(sub *UserSubscription) {
	if sub == nil {
		return
	}
	if sub.IsAggregate {
		s.InvalidateSubCache(sub.UserID, sub.GroupID)
		if s.billingCacheService != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(ctx, sub.UserID, sub.GroupID)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 激活窗口（首次使用时）
	if !sub.IsWindowActivated() {
		if err := s.CheckAndActivateWindow(ctx, sub); err != nil {
			log.Printf("Failed to activate subscription windows: %v", err)
		}
	}

	// 重置过期窗口
	if err := s.CheckAndResetWindows(ctx, sub); err != nil {
		log.Printf("Failed to reset subscription windows: %v", err)
	}

	// 失效 L1 缓存，确保后续请求拿到更新后的数据
	s.InvalidateSubCache(sub.UserID, sub.GroupID)
}

// RecordUsage 记录使用量到订阅
func (s *SubscriptionService) RecordUsage(ctx context.Context, subscriptionID int64, costUSD float64) error {
	return s.userSubRepo.IncrementUsage(ctx, subscriptionID, costUSD)
}

type dailyUsageWindowRoller interface {
	RollDailyUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error)
}

type weeklyUsageWindowRoller interface {
	RollWeeklyUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error)
}

type monthlyUsageWindowRoller interface {
	RollMonthlyUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error)
}

type customUsageWindowRoller interface {
	RollCustomUsageWindow(ctx context.Context, id int64, oldWindowStart, newWindowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error)
}

// SubscriptionProgress 订阅进度
type SubscriptionProgress struct {
	ID            int64                `json:"id"`
	GroupName     string               `json:"group_name"`
	ExpiresAt     time.Time            `json:"expires_at"`
	ExpiresInDays int                  `json:"expires_in_days"`
	Daily         *UsageWindowProgress `json:"daily,omitempty"`
	Weekly        *UsageWindowProgress `json:"weekly,omitempty"`
	Monthly       *UsageWindowProgress `json:"monthly,omitempty"`
	Custom        *UsageWindowProgress `json:"custom,omitempty"`
}

// UsageWindowProgress 使用窗口进度
type UsageWindowProgress struct {
	LimitUSD        float64   `json:"limit_usd"`
	UsedUSD         float64   `json:"used_usd"`
	RemainingUSD    float64   `json:"remaining_usd"`
	Percentage      float64   `json:"percentage"`
	WindowStart     time.Time `json:"window_start"`
	ResetsAt        time.Time `json:"resets_at"`
	ResetsInSeconds int64     `json:"resets_in_seconds"`
}

// GetSubscriptionProgress 获取订阅使用进度
func (s *SubscriptionService) GetSubscriptionProgress(ctx context.Context, subscriptionID int64) (*SubscriptionProgress, error) {
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}
	normalizeSubscriptionSnapshot(sub)

	group := sub.Group
	if group == nil {
		group, err = s.groupRepo.GetByID(ctx, sub.GroupID)
		if err != nil {
			return nil, err
		}
	}
	group = sub.EffectiveGroup(group)

	return s.calculateProgress(sub, group), nil
}

// calculateProgress 根据已加载的订阅和分组数据计算使用进度（纯内存计算，无 DB 查询）
func (s *SubscriptionService) calculateProgress(sub *UserSubscription, group *Group) *SubscriptionProgress {
	progress := &SubscriptionProgress{
		ID:            subscriptionDisplayID(sub),
		GroupName:     group.Name,
		ExpiresAt:     sub.ExpiresAt,
		ExpiresInDays: sub.DaysRemaining(),
	}

	// 日进度
	if group.HasDailyLimit() && sub.DailyWindowStart != nil {
		limit := *group.DailyLimitUSD
		used := subscriptionDisplayUsageForWindow(sub, "daily")
		resetsAt := sub.DailyWindowStart.Add(subscriptionDailyWindow)
		if dailyResetTime := sub.DailyResetTime(); dailyResetTime != nil {
			resetsAt = *dailyResetTime
		}
		if sub.EffectiveDailyResetsAt != nil {
			resetsAt = *sub.EffectiveDailyResetsAt
		}
		progress.Daily = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         used,
			RemainingUSD:    limit - used,
			Percentage:      (used / limit) * 100,
			WindowStart:     *sub.DailyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Daily.RemainingUSD < 0 {
			progress.Daily.RemainingUSD = 0
		}
		if progress.Daily.Percentage > 100 {
			progress.Daily.Percentage = 100
		}
		if progress.Daily.ResetsInSeconds < 0 {
			progress.Daily.ResetsInSeconds = 0
		}
	}

	// 周进度
	if group.HasWeeklyLimit() && sub.WeeklyWindowStart != nil {
		limit := *group.WeeklyLimitUSD
		used := subscriptionDisplayUsageForWindow(sub, "weekly")
		resetsAt := sub.WeeklyWindowStart.Add(subscriptionWeeklyWindow)
		if sub.EffectiveWeeklyResetsAt != nil {
			resetsAt = *sub.EffectiveWeeklyResetsAt
		}
		progress.Weekly = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         used,
			RemainingUSD:    limit - used,
			Percentage:      (used / limit) * 100,
			WindowStart:     *sub.WeeklyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Weekly.RemainingUSD < 0 {
			progress.Weekly.RemainingUSD = 0
		}
		if progress.Weekly.Percentage > 100 {
			progress.Weekly.Percentage = 100
		}
		if progress.Weekly.ResetsInSeconds < 0 {
			progress.Weekly.ResetsInSeconds = 0
		}
	}

	// 月进度
	if group.HasMonthlyLimit() && sub.MonthlyWindowStart != nil {
		limit := *group.MonthlyLimitUSD
		used := subscriptionDisplayUsageForWindow(sub, "monthly")
		resetsAt := sub.MonthlyWindowStart.Add(subscriptionMonthlyWindow)
		if sub.EffectiveMonthlyResetsAt != nil {
			resetsAt = *sub.EffectiveMonthlyResetsAt
		}
		progress.Monthly = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         used,
			RemainingUSD:    limit - used,
			Percentage:      (used / limit) * 100,
			WindowStart:     *sub.MonthlyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Monthly.RemainingUSD < 0 {
			progress.Monthly.RemainingUSD = 0
		}
		if progress.Monthly.Percentage > 100 {
			progress.Monthly.Percentage = 100
		}
		if progress.Monthly.ResetsInSeconds < 0 {
			progress.Monthly.ResetsInSeconds = 0
		}
	}

	if group.HasCustomLimit() && sub.CustomWindowStart != nil {
		limit := *group.CustomLimitUSD
		used := subscriptionDisplayUsageForWindow(sub, "custom")
		resetsAt := sub.CustomWindowStart.Add(customSubscriptionWindow(group))
		if customResetTime := sub.CustomResetTime(group); customResetTime != nil {
			resetsAt = *customResetTime
		}
		if sub.EffectiveCustomResetsAt != nil {
			resetsAt = *sub.EffectiveCustomResetsAt
		}
		progress.Custom = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         used,
			RemainingUSD:    limit - used,
			Percentage:      (used / limit) * 100,
			WindowStart:     *sub.CustomWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Custom.RemainingUSD < 0 {
			progress.Custom.RemainingUSD = 0
		}
		if progress.Custom.Percentage > 100 {
			progress.Custom.Percentage = 100
		}
		if progress.Custom.ResetsInSeconds < 0 {
			progress.Custom.ResetsInSeconds = 0
		}
	}

	return progress
}

func subscriptionDisplayID(sub *UserSubscription) int64 {
	if sub == nil {
		return 0
	}
	if sub.ID > 0 {
		return sub.ID
	}
	return sub.GroupID
}

// GetUserSubscriptionsWithProgress 获取用户所有订阅及进度
func (s *SubscriptionService) GetUserSubscriptionsWithProgress(ctx context.Context, userID int64) ([]SubscriptionProgress, error) {
	subs, err := s.ListActiveUserSubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}

	progresses := make([]SubscriptionProgress, 0, len(subs))
	for i := range subs {
		sub := &subs[i]
		group := sub.EffectiveGroup(sub.Group)
		if group == nil {
			continue
		}
		progresses = append(progresses, *s.calculateProgress(sub, group))
	}

	return progresses, nil
}

// ValidateSubscription 验证订阅是否有效
func (s *SubscriptionService) ValidateSubscription(ctx context.Context, sub *UserSubscription) error {
	if sub.Status == SubscriptionStatusExpired {
		return ErrSubscriptionExpired
	}
	if sub.Status == SubscriptionStatusSuspended {
		return ErrSubscriptionSuspended
	}
	if sub.IsExpired() {
		// 更新状态
		_ = s.userSubRepo.UpdateStatus(ctx, sub.ID, SubscriptionStatusExpired)
		return ErrSubscriptionExpired
	}
	return nil
}
