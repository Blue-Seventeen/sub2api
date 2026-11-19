package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func (s *GatewayService) getUserGroupRateMultiplier(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	if s == nil {
		return groupDefaultMultiplier
	}
	resolver := s.userGroupRateResolver
	if resolver == nil {
		resolver = newUserGroupRateResolver(
			s.userGroupRateRepo,
			s.userGroupRateCache,
			resolveUserGroupRateCacheTTL(s.cfg),
			&s.userGroupRateSF,
			"service.gateway",
		)
	}
	return resolver.Resolve(ctx, userID, groupID, groupDefaultMultiplier)
}

// ResolveUserGroupRateMultiplier resolves the same cached multiplier used by usage billing.
func (s *GatewayService) ResolveUserGroupRateMultiplier(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	return s.getUserGroupRateMultiplier(ctx, userID, groupID, groupDefaultMultiplier)
}

// RecordUsageInput 记录使用量的输入参数。
// 异步 worker 只接收计费所需快照，不能持有 ParsedRequest/RequestBodyRef 这类大请求体引用。
type RecordUsageInput struct {
	Result                *ForwardResult
	APIKey                *APIKey
	User                  *User
	Account               *Account
	Subscription          *UserSubscription  // 可选：订阅信息
	PricingAt             time.Time          // token 售价固定时刻；零值保持既有的记录时刻语义
	InboundEndpoint       string             // 入站端点（客户端请求路径）
	UpstreamEndpoint      string             // 上游端点（标准化后的上游路径）
	UserAgent             string             // 请求的 User-Agent
	IPAddress             string             // 请求的客户端 IP 地址
	SessionID             string             // 客户端显式会话标识，仅用于用量行会话关联
	RequestPayloadHash    string             // 请求体语义哈希，用于降低 request_id 误复用时的静默误去重风险
	ForceCacheBilling     bool               // 强制缓存计费：将 input_tokens 转为 cache_read 计费（用于粘性会话切换）
	APIKeyService         APIKeyQuotaUpdater // 可选：用于更新API Key配额
	ClientProfile         string
	CompatibilityRoute    string
	FallbackChain         string
	UpstreamTransport     string
	QuotaPlatform         string // user×platform 配额计量平台：handler 在请求 ctx 内经 QuotaPlatform() 算定后传入（后扣运行在 worker 池 background ctx 上，取不到 ForcePlatform）
	CompatibleInputTokens int

	ChannelUsageFields // 渠道映射信息（由 handler 在 Forward 前解析）
}

// APIKeyQuotaUpdater defines the interface for updating API Key quota and rate limit usage
type APIKeyQuotaUpdater interface {
	UpdateQuotaUsed(ctx context.Context, apiKeyID int64, cost float64) error
	UpdateRateLimitUsage(ctx context.Context, apiKeyID int64, cost float64) error
}

type apiKeyAuthCacheInvalidator interface {
	InvalidateAuthCacheByKey(ctx context.Context, key string)
}

type usageLogBestEffortWriter interface {
	CreateBestEffort(ctx context.Context, log *UsageLog) error
}

// postUsageBillingParams 统一扣费所需的参数
type postUsageBillingParams struct {
	Cost                  *CostBreakdown
	User                  *User
	APIKey                *APIKey
	Account               *Account
	Subscription          *UserSubscription
	RequestPayloadHash    string
	IsSubscriptionBill    bool
	AccountRateMultiplier float64
	APIKeyService         APIKeyQuotaUpdater
	Platform              string // 来自 APIKey 关联 Group 的平台标识
	// SimpleModeKeyRateLimitOnly opts the request into the simple-mode billing
	// path that records only API-key 5h/1d/7d window usage. It must not trigger
	// balance, subscription, account, platform, or lifetime-key-quota effects.
	SimpleModeKeyRateLimitOnly bool
}

var ErrSimpleModeKeyRateLimitBillingUnavailable = errors.New("simple mode api key rate-limit billing unavailable")

func simpleModeKeyRateLimitBillingEnabled(cfg *config.Config, apiKey *APIKey) bool {
	return cfg != nil && cfg.RunMode == config.RunModeSimple && cfg.SimpleModeKeyRateLimitEnabled && apiKey != nil && apiKey.HasRateLimits()
}

// PlatformFromAPIKey 从 APIKey 关联的 Group 推导 platform 名称。
// apiKey 为 nil 或 Group 信息缺失时返回空串（调用方据此 short-circuit quota 累加）。
// 导出供 handler 层调用。
func PlatformFromAPIKey(apiKey *APIKey) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}

// QuotaPlatform 返回 user×platform 配额计量使用的平台标识。
// 强制平台路由（如 /antigravity）优先按 ctx 中的 ForcePlatform 计量，否则回退到
// APIKey 关联 Group 的平台。
//
// 注意：必须用带 ForcePlatform 的请求 context 调用（如 handler 的 c.Request.Context()）。
// 后扣运行在 worker 池的 background ctx 上没有 ForcePlatform，因此后扣平台由 handler
// 预先算定、经 RecordUsageInput.QuotaPlatform 传入，不要在后扣链路用 worker ctx 调用本函数。
func QuotaPlatform(ctx context.Context, apiKey *APIKey) string {
	if ctx != nil {
		if fp, ok := ctx.Value(ctxkey.ForcePlatform).(string); ok && fp != "" {
			return fp
		}
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		return platform
	}
	platform := PlatformFromAPIKey(apiKey)
	if platform == PlatformComposite {
		return ""
	}
	return platform
}

func (p *postUsageBillingParams) shouldDeductAPIKeyQuota() bool {
	return p.Cost.ActualCost > 0 && p.APIKey.Quota > 0 && p.APIKeyService != nil
}

func (p *postUsageBillingParams) shouldUpdateRateLimits() bool {
	return p.Cost.ActualCost > 0 && p.APIKey.HasRateLimits() && p.APIKeyService != nil
}

func (p *postUsageBillingParams) shouldUpdateAccountQuota() bool {
	return p.Cost.TotalCost > 0 && p.Account.SupportsAccountQuota() && p.Account.HasAnyQuotaLimit()
}

// postUsageBilling is the legacy fallback billing path used when the unified
// billing repo is unavailable (nil). Production uses applyUsageBilling → repo.Apply
// for atomic billing. This path only runs in tests or degraded mode.
func postUsageBilling(ctx context.Context, p *postUsageBillingParams, deps *billingDeps) {
	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	cost := p.Cost

	if p.IsSubscriptionBill {
		// Subscription usage tracked by ActualCost so group rate multiplier
		// consumes the quota at the expected speed.
		if cost.ActualCost > 0 {
			if err := deps.userSubRepo.IncrementUsage(billingCtx, p.Subscription.ID, cost.ActualCost); err != nil {
				slog.Error("increment subscription usage failed", "subscription_id", p.Subscription.ID, "error", err)
			}
		}
	} else {
		if cost.RealActualCost > 0 {
			if err := deps.userRepo.DeductBalance(billingCtx, p.User.ID, cost.RealActualCost); err != nil {
				slog.Error("deduct balance failed", "user_id", p.User.ID, "error", err)
			} else if deps.billingCacheService != nil {
				if err := deps.billingCacheService.InvalidateUserBalance(billingCtx, p.User.ID); err != nil {
					slog.Warn("invalidate balance cache after legacy deduction failed", "user_id", p.User.ID, "error", err)
				}
			}
		}
	}

	if p.shouldDeductAPIKeyQuota() {
		if err := p.APIKeyService.UpdateQuotaUsed(billingCtx, p.APIKey.ID, cost.ActualCost); err != nil {
			slog.Error("update api key quota failed", "api_key_id", p.APIKey.ID, "error", err)
		}
	}

	if p.shouldUpdateRateLimits() {
		if err := p.APIKeyService.UpdateRateLimitUsage(billingCtx, p.APIKey.ID, cost.ActualCost); err != nil {
			slog.Error("update api key rate limit usage failed", "api_key_id", p.APIKey.ID, "error", err)
		}
	}

	if p.shouldUpdateAccountQuota() {
		accountCost := cost.TotalCost * p.AccountRateMultiplier
		if err := deps.accountRepo.IncrementQuotaUsed(billingCtx, p.Account.ID, accountCost); err != nil {
			slog.Error("increment account quota used failed", "account_id", p.Account.ID, "cost", accountCost, "error", err)
		}
	}

	// Platform quota 累加（legacy 兜底路径）：仅对 standard（余额）模式生效；订阅模式豁免；仅对有 limit 的用户写
	//   - HasUserPlatformQuotaLimit 守卫:与正常路径对齐，无 limit 公司跳过
	//   - 新增 Redis 同步写:enforcement 走 Redis，legacy 路径也必须同步写，否则 preflight 看不到消费
	//   - flusher_enabled=false（降级）:保留原有同步直写 DB
	//   - flusher_enabled=true:跳过直写 DB，由 flusher 异步批量刷（markDirty 在 IncrementUserPlatformQuotaUsage 内部完成）
	//   - 失败仅记 ALERT log + counter，不阻断主扣费流程
	if !p.IsSubscriptionBill && p.Platform != "" && cost.ActualCost > 0 && p.User != nil && deps.userPlatformQuotaRepo != nil {
		if deps.billingCacheService.HasUserPlatformQuotaLimit(billingCtx, p.User.ID, p.Platform) {
			cacheSynced := deps.billingCacheService.IncrementUserPlatformQuotaUsage(p.User.ID, p.Platform, cost.ActualCost)
			if !cacheSynced || deps.cfg == nil || !deps.cfg.Database.UserPlatformQuotaFlusherEnabled {
				// 降级路径:flusher 未启用时保留原有同步直写 DB；cache 未同步时也兜底写 DB 防漏记。
				if err := deps.userPlatformQuotaRepo.IncrementUsageWithReset(billingCtx, p.User.ID, p.Platform, cost.ActualCost, time.Now().UTC()); err != nil {
					userPlatformQuotaDBIncrLegacyErrorTotal.Add(1)
					logger.LegacyPrintf("service.gateway", "ALERT: legacy incr user platform quota DB failed user=%d platform=%s cost=%f: %v", p.User.ID, p.Platform, cost.ActualCost, err)
				}
			}
			// flusher_enabled=true:不直写 DB，flusher 异步批量刷
		}
	}

	// NOTE: finalizePostUsageBilling is NOT called here to avoid double-queuing
	// cache updates. The legacy path does DB writes directly; the finalize path
	// does cache queue + notifications. Notifications are dispatched separately
	// by the caller after recording the usage log.
}

func resolveUsageBillingRequestID(ctx context.Context, upstreamRequestID string) string {
	if requestID := strings.TrimSpace(upstreamRequestID); isForcedUsageBillingRequestID(requestID) {
		return requestID
	}
	if ctx != nil {
		if clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
			return "client:" + strings.TrimSpace(clientRequestID)
		}
		if requestID, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
			return "local:" + strings.TrimSpace(requestID)
		}
	}
	if requestID := strings.TrimSpace(upstreamRequestID); requestID != "" {
		return requestID
	}
	return "generated:" + generateRequestID()
}

func isForcedUsageBillingRequestID(requestID string) bool {
	id := strings.TrimSpace(requestID)
	return strings.HasPrefix(id, "web_search:") ||
		strings.HasPrefix(id, "grok-video:") ||
		strings.HasPrefix(id, "grok_audio:") ||
		strings.HasPrefix(id, "grok_realtime:") ||
		strings.HasPrefix(id, GeminiCachedContentUsageRequestIDPrefix)
}

func StableGrokAudioBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "grok_audio:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		upstreamRequestID = generateRequestID()
	}
	return "grok_audio:" + upstreamRequestID
}

func StableGrokRealtimeBillingRequestID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if strings.HasPrefix(sessionID, "grok_realtime:") {
		return sessionID
	}
	if sessionID == "" {
		sessionID = generateRequestID()
	}
	return "grok_realtime:" + sessionID
}

func resolveUsageBillingPayloadFingerprint(ctx context.Context, requestPayloadHash string) string {
	if payloadHash := strings.TrimSpace(requestPayloadHash); payloadHash != "" {
		return payloadHash
	}
	if ctx != nil {
		if clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
			return "client:" + strings.TrimSpace(clientRequestID)
		}
		if requestID, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
			return "local:" + strings.TrimSpace(requestID)
		}
	}
	return ""
}

func buildUsageBillingCommand(requestID string, usageLog *UsageLog, p *postUsageBillingParams) *UsageBillingCommand {
	if p == nil || p.Cost == nil || p.APIKey == nil || p.User == nil || p.Account == nil {
		return nil
	}

	cmd := &UsageBillingCommand{
		RequestID:          requestID,
		APIKeyID:           p.APIKey.ID,
		UserID:             p.User.ID,
		AccountID:          p.Account.ID,
		AccountType:        p.Account.Type,
		RequestPayloadHash: strings.TrimSpace(p.RequestPayloadHash),
	}
	if usageLog != nil {
		cmd.Model = usageLog.Model
		cmd.BillingType = usageLog.BillingType
		cmd.InputTokens = usageLog.InputTokens
		cmd.OutputTokens = usageLog.OutputTokens
		cmd.CacheCreationTokens = usageLog.CacheCreationTokens
		cmd.CacheReadTokens = usageLog.CacheReadTokens
		cmd.ImageCount = usageLog.ImageCount
		switch usageLogBillingMode(usageLog) {
		case BillingModeDuration:
			cmd.BillableDurationSeconds = usageLog.BillableDurationSeconds
		case BillingModeCharacter:
			cmd.BillableCharacterCount = usageLog.BillableCharacterCount
		}
		if usageLog.ServiceTier != nil {
			cmd.ServiceTier = *usageLog.ServiceTier
		}
		if usageLog.ReasoningEffort != nil {
			cmd.ReasoningEffort = *usageLog.ReasoningEffort
		}
		if usageLog.SubscriptionID != nil {
			cmd.SubscriptionID = usageLog.SubscriptionID
		}
	}

	if p.SimpleModeKeyRateLimitOnly {
		if p.Cost.ActualCost > 0 && p.APIKey.HasRateLimits() {
			cmd.APIKeyRateLimitCost = p.Cost.ActualCost
		}
		cmd.Normalize()
		return cmd
	}

	// Record subscription / balance cost using ActualCost so the group (and any
	// user-specific) rate multiplier consumes subscription quota at the expected
	// speed. TotalCost remains the raw (pre-multiplier) value; downstream guards
	// on "> 0" still correctly skip free subscriptions (RateMultiplier == 0).
	if p.IsSubscriptionBill && p.Subscription != nil && p.Cost.ActualCost > 0 {
		cmd.SubscriptionID = nil
		cmd.SubscriptionUserID = p.Subscription.UserID
		cmd.SubscriptionGroupID = p.Subscription.GroupID
		if p.User != nil {
			cmd.SubscriptionUserID = p.User.ID
		}
		if p.APIKey != nil && p.APIKey.GroupID != nil {
			cmd.SubscriptionGroupID = *p.APIKey.GroupID
		}
		cmd.SubscriptionCost = p.Cost.ActualCost
	} else if p.Cost.RealActualCost > 0 {
		cmd.BalanceCost = p.Cost.RealActualCost
	}

	if p.shouldDeductAPIKeyQuota() {
		cmd.APIKeyQuotaCost = p.Cost.ActualCost
	}
	if p.shouldUpdateRateLimits() {
		cmd.APIKeyRateLimitCost = p.Cost.ActualCost
	}
	if p.shouldUpdateAccountQuota() {
		cmd.AccountQuotaCost = p.Cost.TotalCost * p.AccountRateMultiplier
		cmd.AccountQuotaHasDailyLimit = p.Account.GetQuotaDailyLimit() > 0
		cmd.AccountQuotaHasWeeklyLimit = p.Account.GetQuotaWeeklyLimit() > 0
	}

	cmd.Normalize()
	return cmd
}

func usageLogBillingMode(usageLog *UsageLog) BillingMode {
	if usageLog == nil || usageLog.BillingMode == nil {
		return ""
	}
	return BillingMode(strings.TrimSpace(*usageLog.BillingMode))
}

func applyUsageBilling(ctx context.Context, requestID string, usageLog *UsageLog, p *postUsageBillingParams, deps *billingDeps, repo UsageBillingRepository) (bool, error) {
	if p == nil || deps == nil {
		return false, nil
	}

	cmd := buildUsageBillingCommand(requestID, usageLog, p)
	if cmd == nil || cmd.RequestID == "" || repo == nil {
		if p.SimpleModeKeyRateLimitOnly {
			return false, ErrSimpleModeKeyRateLimitBillingUnavailable
		}
		// The legacy path is only a fallback for standard billing. Simple mode
		// must retain request-id deduplication and never bill other balances.
		postUsageBilling(ctx, p, deps)
		return true, nil
	}

	applyCtx, cancelApply := detachedBillingContext(ctx)
	result, err := repo.Apply(applyCtx, cmd)
	cancelApply()
	if err != nil {
		return false, err
	}

	if result == nil || !result.Applied {
		deps.deferredService.ScheduleLastUsedUpdate(p.Account.ID)
		return false, nil
	}

	// 结算事务可能把费用分摊到与预检快照不同的订阅卡（聚合订阅按卡拆分、卡在
	// 请求期间切换），使用日志必须记录实际扣费的卡：否则对账会把用量挂到虚拟
	// 聚合 ID 或已失效的快照卡上，预检快照本身保持只读。非正数 ID 不是真实卡
	// （0 = 虚拟聚合、负数 = 上游异常），不能成为归因。
	if result.SubscriptionID != nil && *result.SubscriptionID > 0 && usageLog != nil {
		usageLog.SubscriptionID = result.SubscriptionID
	}

	// Apply 可能在按用户合并的结算通道中排队，结算后的缓存同步使用独立的超时。
	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	if result.APIKeyQuotaExhausted {
		if invalidator, ok := p.APIKeyService.(apiKeyAuthCacheInvalidator); ok && p.APIKey != nil && p.APIKey.Key != "" {
			invalidator.InvalidateAuthCacheByKey(billingCtx, p.APIKey.Key)
		}
	}

	finalizePostUsageBilling(billingCtx, p, deps, result)
	return true, nil
}

func finalizePostUsageBilling(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	if p == nil || p.Cost == nil || deps == nil {
		return
	}

	if p.SimpleModeKeyRateLimitOnly {
		// Simple-mode key windows are DB-authoritative during preflight. Clear
		// any Redis snapshot after the committed increment as a best-effort aid
		// when switching back to standard mode. Enforcement here never relies on
		// successful cache invalidation.
		if p.APIKey != nil && deps.billingCacheService != nil {
			if err := deps.billingCacheService.InvalidateAPIKeyRateLimit(ctx, p.APIKey.ID); err != nil {
				logger.LegacyPrintf("service.gateway", "Warning: invalidate simple-mode api key rate-limit cache failed for key %d: %v", p.APIKey.ID, err)
			}
		}
		deps.deferredService.ScheduleLastUsedUpdate(p.Account.ID)
		return
	}

	if p.IsSubscriptionBill {
		if p.Cost.ActualCost > 0 && p.User != nil && p.APIKey != nil && p.APIKey.GroupID != nil {
			deps.billingCacheService.QueueUpdateSubscriptionUsage(p.User.ID, *p.APIKey.GroupID, p.Cost.ActualCost)
		}
	} else if p.Cost.RealActualCost > 0 && p.User != nil {
		syncBalanceCacheAfterDeduction(ctx, p, deps, result)
	}

	if p.Cost.ActualCost > 0 && p.APIKey != nil && p.APIKey.HasRateLimits() && deps.billingCacheService != nil {
		deps.billingCacheService.QueueUpdateAPIKeyRateLimitUsage(p.APIKey.ID, p.Cost.ActualCost)
	}

	deps.deferredService.ScheduleLastUsedUpdate(p.Account.ID)

	// Redis is updated before returning so subsequent preflight checks observe the
	// charge. Without the batch flusher, persist inline with the usage task (in a
	// worker or its synchronous overflow caller), without spawning detached work.
	// Synchronous overflow remains backpressure, not a global DB concurrency cap.
	if !p.IsSubscriptionBill && p.Platform != "" && p.Cost.ActualCost > 0 && p.User != nil && deps.userPlatformQuotaRepo != nil && deps.billingCacheService != nil {
		if deps.billingCacheService.HasUserPlatformQuotaLimit(ctx, p.User.ID, p.Platform) {
			cacheSynced := deps.billingCacheService.IncrementUserPlatformQuotaUsage(p.User.ID, p.Platform, p.Cost.ActualCost)
			if !cacheSynced || deps.cfg == nil || !deps.cfg.Database.UserPlatformQuotaFlusherEnabled {
				// 降级路径:flusher 未启用时直写 DB；cache 未同步时也兜底写 DB 防漏记。
				// Persistence stays inline with the usage task (bounded by its own
				// independent deadline) so a detached goroutine cannot outlive the
				// worker slot and silently reorder quota state.
				persistPostUsagePlatformQuota(ctx, deps.userPlatformQuotaRepo, p.User.ID, p.Platform, p.Cost.ActualCost)
			}
		}
	}

	if deps.balanceNotifyService != nil {
		deps.balanceNotifyService.submitUsageNotifications(p, deps, result)
	}
}

// A committed charge must still attempt quota persistence if the request/worker
// context expired. Bound that independent attempt, and keep failures observable;
// this path has no durable retry queue.
func persistPostUsagePlatformQuota(ctx context.Context, repo UserPlatformQuotaRepository, userID int64, platform string, cost float64) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	dbCtx, cancel := context.WithTimeout(base, 5*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			userPlatformQuotaDBIncrErrorTotal.Add(1)
			logger.LegacyPrintf("service.gateway", "ALERT: panic persisting user platform quota user=%d platform=%s: %v", userID, platform, r)
		}
	}()
	if err := repo.IncrementUsageWithReset(dbCtx, userID, platform, cost, time.Now().UTC()); err != nil {
		userPlatformQuotaDBIncrErrorTotal.Add(1)
		logger.LegacyPrintf("service.gateway", "ALERT: incr user platform quota DB failed user=%d platform=%s cost=%f: %v", userID, platform, cost, err)
	}
}

func syncBalanceCacheAfterDeduction(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	if p == nil || p.Cost == nil || p.User == nil || deps == nil || deps.billingCacheService == nil {
		return
	}
	if result != nil && result.NewBalance != nil && deps.billingCacheService.balanceBelowEligibilityThreshold(*result.NewBalance) {
		if err := deps.billingCacheService.InvalidateUserBalance(ctx, p.User.ID); err != nil {
			slog.Warn("invalidate balance cache after exhausted deduction failed",
				"user_id", p.User.ID,
				"new_balance", *result.NewBalance,
				"balance_overdrafted", result.BalanceOverdrafted,
				"error", err,
			)
		}
		return
	}
	amount := p.Cost.RealActualCost
	if amount <= 0 {
		// Older callers and compatibility tests only populated ActualCost. Keep
		// those records aligned with the balance deduction path while preferring
		// the custom fork's real-cost field whenever it is available.
		amount = p.Cost.ActualCost
	}
	if deps.billingCacheService.InflightReservationEnabled() {
		// 在途预留开启时同步扣减余额缓存：计费任务结束后才会释放预留，
		// 必须保证此时准入读取的缓存余额已反映本次扣费，否则释放与扣减之间
		// 仍存在「在途=0 且余额未扣」的窗口。本函数运行在计费 worker 中，不在请求热路径。
		// RealActualCost is the amount deducted from the user's balance in this
		// fork; ActualCost is the user-facing compatibility field.
		err := deps.billingCacheService.DeductBalanceCache(ctx, p.User.ID, amount)
		if err == nil {
			return
		}
		logger.LegacyPrintf("service.gateway", "Warning: sync deduct balance cache failed for user %d, falling back to queue: %v", p.User.ID, err)
	}
	deps.billingCacheService.QueueDeductBalance(p.User.ID, amount)
}

// notifyBalanceLow sends balance low notification after deduction.
// When result.NewBalance is available (from DB transaction RETURNING), it is used directly
// to reconstruct oldBalance, avoiding stale Redis reads and concurrent-deduction races.
func notifyBalanceLow(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in notifyBalanceLow", "recover", r)
		}
	}()
	// RealActualCost is the amount deducted from the user's balance in this fork;
	// ActualCost is the user-facing compatibility field. Prefer the deducted
	// amount and fall back to ActualCost for older callers that only populate it.
	cost := p.Cost.RealActualCost
	if cost <= 0 {
		cost = p.Cost.ActualCost
	}
	if p.IsSubscriptionBill || cost <= 0 || p.User == nil || deps.balanceNotifyService == nil {
		slog.Debug("notifyBalanceLow: skipped",
			"is_subscription", p.IsSubscriptionBill,
			"actual_cost", p.Cost.ActualCost,
			"real_actual_cost", p.Cost.RealActualCost,
			"user_nil", p.User == nil,
			"service_nil", deps.balanceNotifyService == nil,
		)
		return
	}

	oldBalance := resolveOldBalance(p, result, cost)
	slog.Debug("notifyBalanceLow: calling CheckBalanceAfterDeduction",
		"user_id", p.User.ID,
		"old_balance", oldBalance,
		"cost", cost,
		"notify_enabled", p.User.BalanceNotifyEnabled,
		"threshold", p.User.BalanceNotifyThreshold,
		"result_has_new_balance", result != nil && result.NewBalance != nil,
	)
	deps.balanceNotifyService.CheckBalanceAfterDeduction(ctx, p.User, oldBalance, cost)
}

// resolveOldBalance returns the pre-deduction balance.
// Prefers the DB transaction result (newBalance + cost) over snapshot.
func resolveOldBalance(p *postUsageBillingParams, result *UsageBillingApplyResult, cost float64) float64 {
	if result != nil && result.NewBalance != nil {
		return *result.NewBalance + cost
	}
	// Legacy fallback: snapshot balance from request context
	return p.User.Balance
}

// notifyAccountQuota sends account quota threshold notification after increment.
// When result.QuotaState is available (from DB transaction RETURNING), it is passed directly
// to avoid a separate DB read that may see stale or concurrently-modified data.
func notifyAccountQuota(ctx context.Context, p *postUsageBillingParams, deps *billingDeps, result *UsageBillingApplyResult) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in notifyAccountQuota", "recover", r)
		}
	}()
	if p.Cost.TotalCost <= 0 || p.Account == nil || !p.Account.SupportsAccountQuota() || !p.Account.HasAnyQuotaLimit() || deps.balanceNotifyService == nil {
		slog.Debug("notifyAccountQuota: skipped",
			"total_cost", p.Cost.TotalCost,
			"account_nil", p.Account == nil,
			"supports_account_quota", p.Account != nil && p.Account.SupportsAccountQuota(),
			"has_quota_limit", p.Account != nil && p.Account.HasAnyQuotaLimit(),
			"service_nil", deps.balanceNotifyService == nil,
		)
		return
	}
	accountCost := p.Cost.TotalCost * p.AccountRateMultiplier
	var quotaState *AccountQuotaState
	if result != nil {
		quotaState = result.QuotaState
		// 本应累加却无递增后状态（账号已删除，计数被跳过）：没有新用量，不做阈值判定。
		if quotaState == nil && p.shouldUpdateAccountQuota() {
			return
		}
	}
	slog.Debug("notifyAccountQuota: calling CheckAccountQuotaAfterIncrement",
		"account_id", p.Account.ID,
		"account_cost", accountCost,
		"has_quota_state", quotaState != nil,
	)
	deps.balanceNotifyService.CheckAccountQuotaAfterIncrement(ctx, p.Account, accountCost, quotaState)
}

func detachedBillingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, postUsageBillingTimeout)
}

func detachStreamUpstreamContext(ctx context.Context, stream bool) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.Background(), func() {}
	}
	if !stream {
		return ctx, func() {}
	}
	return pkghttputil.DetachUpstreamContext(ctx), func() {}
}

func detachUpstreamContext(ctx context.Context, drainTimeout ...time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.Background(), func() {}
	}
	return pkghttputil.DetachUpstreamContext(ctx, drainTimeout...), func() {}
}

// billingDeps 扣费逻辑依赖的服务（由各 gateway service 提供）
type billingDeps struct {
	accountRepo           AccountRepository
	userRepo              UserRepository
	userSubRepo           UserSubscriptionRepository
	billingCacheService   *BillingCacheService
	deferredService       *DeferredService
	balanceNotifyService  *BalanceNotifyService
	userPlatformQuotaRepo UserPlatformQuotaRepository
	cfg                   *config.Config
}

func (s *GatewayService) billingDeps() *billingDeps {
	return &billingDeps{
		accountRepo:           s.accountRepo,
		userRepo:              s.userRepo,
		userSubRepo:           s.userSubRepo,
		billingCacheService:   s.billingCacheService,
		deferredService:       s.deferredService,
		balanceNotifyService:  s.balanceNotifyService,
		userPlatformQuotaRepo: s.userPlatformQuotaRepo,
		cfg:                   s.cfg,
	}
}

func writeUsageLogBestEffort(ctx context.Context, repo UsageLogRepository, usageLog *UsageLog, logKey string) {
	if repo == nil || usageLog == nil {
		return
	}
	usageCtx, cancel := detachedBillingContext(ctx)
	defer cancel()

	if writer, ok := repo.(usageLogBestEffortWriter); ok {
		if err := writer.CreateBestEffort(usageCtx, usageLog); err != nil {
			logger.LegacyPrintf(logKey, "Create usage log failed: %v", err)
			// 计费已在此前完成，日志必须落库：dropped（批处理队列超时）同样走同步兜底，
			// 否则会出现“已扣费但无 usage_log”的对账缺口（issue #3656）。
			// 重复写入由 usage_logs 的 ON CONFLICT (request_id, api_key_id) DO NOTHING 防护。
			fallbackCtx := usageCtx
			if usageCtx.Err() != nil {
				// usageCtx 已耗尽（best-effort 入队阻塞到期限）：换新的 detached 窗口，避免兜底必然失败。
				var fallbackCancel context.CancelFunc
				fallbackCtx, fallbackCancel = detachedBillingContext(context.Background())
				defer fallbackCancel()
			}
			if _, syncErr := repo.Create(fallbackCtx, usageLog); syncErr != nil {
				logger.LegacyPrintf(logKey, "Create usage log sync fallback failed: %v", syncErr)
			}
		}
		return
	}

	if _, err := repo.Create(usageCtx, usageLog); err != nil {
		logger.LegacyPrintf(logKey, "Create usage log failed: %v", err)
	}
}

func writeBillingFailedUsageLogBestEffort(ctx context.Context, repo UsageLogRepository, usageLog *UsageLog, logKey string) {
	if repo == nil || usageLog == nil {
		return
	}
	failed := *usageLog
	failed.RequestID = strings.TrimSpace(failed.RequestID) + ":billing_failed"
	failed.InputTokens = 0
	failed.OutputTokens = 0
	failed.CacheCreationTokens = 0
	failed.CacheCreation5mTokens = 0
	failed.CacheCreation1hTokens = 0
	failed.CacheReadTokens = 0
	failed.ImageOutputTokens = 0
	failed.InputCost = 0
	failed.OutputCost = 0
	failed.ImageOutputCost = 0
	failed.CacheCreationCost = 0
	failed.CacheReadCost = 0
	failed.TotalCost = 0
	failed.ActualCost = 0
	failed.RealActualCost = 0
	writeUsageLogBestEffort(ctx, repo, &failed, logKey)
}

// recordUsageOpts 内部选项，参数化普通计费与长上下文计费的差异点。
type recordUsageOpts struct {
	// 长上下文计费（仅 Gemini 路径需要）
	LongContextThreshold  int
	LongContextMultiplier float64
	PricingAt             time.Time
}

// RecordUsage 记录使用量并扣费（或更新订阅用量）
func (s *GatewayService) RecordUsage(ctx context.Context, input *RecordUsageInput) error {
	if input == nil {
		return errors.New("usage input is nil")
	}
	if input.Result == nil {
		return errors.New("usage result is nil")
	}
	return s.recordUsageCore(ctx, &recordUsageCoreInput{
		Result:                input.Result,
		APIKey:                input.APIKey,
		User:                  input.User,
		Account:               input.Account,
		Subscription:          input.Subscription,
		PricingAt:             input.PricingAt,
		InboundEndpoint:       input.InboundEndpoint,
		UpstreamEndpoint:      input.UpstreamEndpoint,
		UserAgent:             input.UserAgent,
		IPAddress:             input.IPAddress,
		SessionID:             input.SessionID,
		RequestPayloadHash:    input.RequestPayloadHash,
		ForceCacheBilling:     input.ForceCacheBilling,
		APIKeyService:         input.APIKeyService,
		ClientProfile:         input.ClientProfile,
		CompatibilityRoute:    input.CompatibilityRoute,
		FallbackChain:         input.FallbackChain,
		UpstreamTransport:     input.UpstreamTransport,
		QuotaPlatform:         input.QuotaPlatform,
		CompatibleInputTokens: input.CompatibleInputTokens,
		ChannelUsageFields:    input.ChannelUsageFields,
	}, &recordUsageOpts{})
}

// RecordUsageLongContextInput 记录使用量的输入参数（支持长上下文双倍计费）
type RecordUsageLongContextInput struct {
	Result                *ForwardResult
	APIKey                *APIKey
	User                  *User
	Account               *Account
	Subscription          *UserSubscription  // 可选：订阅信息
	PricingAt             time.Time          // token 售价固定时刻；零值保持既有的记录时刻语义
	InboundEndpoint       string             // 入站端点（客户端请求路径）
	UpstreamEndpoint      string             // 上游端点（标准化后的上游路径）
	UserAgent             string             // 请求的 User-Agent
	IPAddress             string             // 请求的客户端 IP 地址
	SessionID             string             // 客户端显式会话标识（session_id / X-Session-Id 等请求头），仅用于用量行会话关联
	RequestPayloadHash    string             // 请求体语义哈希，用于降低 request_id 误复用时的静默误去重风险
	LongContextThreshold  int                // 长上下文阈值（如 200000）
	LongContextMultiplier float64            // 超出阈值部分的倍率（如 2.0）
	ForceCacheBilling     bool               // 强制缓存计费：将 input_tokens 转为 cache_read 计费（用于粘性会话切换）
	APIKeyService         APIKeyQuotaUpdater // API Key 配额服务（可选）
	ClientProfile         string
	CompatibilityRoute    string
	FallbackChain         string
	UpstreamTransport     string
	QuotaPlatform         string // user×platform 配额计量平台：handler 在请求 ctx 内经 QuotaPlatform() 算定后传入（后扣运行在 worker 池 background ctx 上，取不到 ForcePlatform）

	ChannelUsageFields // 渠道映射信息（由 handler 在 Forward 前解析）
}

// RecordUsageWithLongContext 记录使用量并扣费，支持长上下文双倍计费（用于 Gemini）
func (s *GatewayService) RecordUsageWithLongContext(ctx context.Context, input *RecordUsageLongContextInput) error {
	if input == nil {
		return errors.New("usage input is nil")
	}
	if input.Result == nil {
		return errors.New("usage result is nil")
	}
	return s.recordUsageCore(ctx, &recordUsageCoreInput{
		Result:             input.Result,
		APIKey:             input.APIKey,
		User:               input.User,
		Account:            input.Account,
		Subscription:       input.Subscription,
		PricingAt:          input.PricingAt,
		InboundEndpoint:    input.InboundEndpoint,
		UpstreamEndpoint:   input.UpstreamEndpoint,
		UserAgent:          input.UserAgent,
		IPAddress:          input.IPAddress,
		SessionID:          input.SessionID,
		RequestPayloadHash: input.RequestPayloadHash,
		ForceCacheBilling:  input.ForceCacheBilling,
		APIKeyService:      input.APIKeyService,
		ClientProfile:      input.ClientProfile,
		CompatibilityRoute: input.CompatibilityRoute,
		FallbackChain:      input.FallbackChain,
		UpstreamTransport:  input.UpstreamTransport,
		QuotaPlatform:      input.QuotaPlatform,
		ChannelUsageFields: input.ChannelUsageFields,
	}, &recordUsageOpts{
		LongContextThreshold:  input.LongContextThreshold,
		LongContextMultiplier: input.LongContextMultiplier,
	})
}

// recordUsageCoreInput 是 recordUsageCore 的公共输入字段，从两种输入结构体中提取。
type recordUsageCoreInput struct {
	Result                *ForwardResult
	APIKey                *APIKey
	User                  *User
	Account               *Account
	Subscription          *UserSubscription
	PricingAt             time.Time
	InboundEndpoint       string
	UpstreamEndpoint      string
	UserAgent             string
	IPAddress             string
	SessionID             string
	RequestPayloadHash    string
	ForceCacheBilling     bool
	APIKeyService         APIKeyQuotaUpdater
	ClientProfile         string
	CompatibilityRoute    string
	FallbackChain         string
	UpstreamTransport     string
	QuotaPlatform         string
	CompatibleInputTokens int
	ChannelUsageFields
}

// recordUsageCore 是 RecordUsage 和 RecordUsageWithLongContext 的统一实现。
// LongContextThreshold > 0 时 Token 计费回退走 CalculateCostWithLongContext。
func (s *GatewayService) recordUsageCore(ctx context.Context, input *recordUsageCoreInput, opts *recordUsageOpts) error {
	result := input.Result
	apiKey := input.APIKey
	user := input.User
	account := input.Account
	subscription := input.Subscription
	if result.SkipUsageBilling {
		return nil
	}
	ApplyForwardImageBillingResolution(result)
	// 按"实际服务档位"计费（上游每次请求会计费口径）：当上游在自己可裁定的响应里
	// 声明只给了 standard（而我们请求的是 fast/priority/ultrafast）时，把
	// result.ServiceTier 降到 standard，后续定价与落库都用这个档位。
	// 该判定只降不升：上游没声明或声明更高时保持请求档位。
	logServiceTierBillingDowngrade("service.gateway", account, result.RequestID, ApplyForwardServiceTierBillingResolution(result))
	var apiKeyGroup *Group
	if apiKey != nil {
		apiKeyGroup = apiKey.Group
	}
	applyCompatibleUsageFallback(result, account, apiKeyGroup, input.CompatibleInputTokens)

	// 强制缓存计费：将 input_tokens 转为 cache_read_input_tokens
	// 用于粘性会话切换时的特殊计费处理
	if input.ForceCacheBilling && result.Usage.InputTokens > 0 {
		logger.LegacyPrintf("service.gateway", "force_cache_billing: %d input_tokens → cache_read_input_tokens (account=%d)",
			result.Usage.InputTokens, account.ID)
		result.Usage.CacheReadInputTokens += result.Usage.InputTokens
		result.Usage.InputTokens = 0
	}

	// Cache TTL Override: 确保计费时 token 分类与账号设置一致。
	// 账号级设置优先；全局 1h 请求注入开启时，默认把 usage 计费归回 5m。
	cacheTTLOverridden := false
	if overrideTarget, ok := s.resolveCacheTTLUsageOverrideTarget(ctx, account); ok {
		applyCacheTTLOverride(&result.Usage, overrideTarget)
		cacheTTLOverridden = (result.Usage.CacheCreation5mTokens + result.Usage.CacheCreation1hTokens) > 0
	}

	// 获取费率倍数（优先级：用户专属 > 分组默认 > 系统默认）
	multiplier := 1.0
	if s.cfg != nil {
		multiplier = s.cfg.Default.RateMultiplier
	}
	if apiKey != nil && apiKey.GroupID != nil && apiKey.Group != nil {
		groupDefault := apiKey.Group.RateMultiplier
		multiplier = s.ResolveUserGroupRateMultiplier(ctx, user.ID, *apiKey.GroupID, groupDefault)
	}
	baseMultiplier := multiplier
	unifiedRateMultiplier := effectiveUnifiedMultiplier(user)
	finalBaseMultiplier := finalRateFromBaseMultiplier(baseMultiplier, user)
	pricingAt := input.PricingAt
	if pricingAt.IsZero() {
		pricingAt = timezone.Now()
	}
	pricingOpts := recordUsageOpts{}
	if opts != nil {
		pricingOpts = *opts
	}
	pricingOpts.PricingAt = pricingAt
	opts = &pricingOpts
	realMultiplier, realImageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, pricingAt)
	// token 倍率叠加高峰因子（token 计费含图片 token，图片按次倍率不受影响）。高峰因子按请求时刻现算，
	// 不并入上面的 getUserGroupRateMultiplier，以免污染 user:group 倍率缓存。

	// 确定计费模型
	multiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, finalBaseMultiplier, pricingAt)
	concreteBillingModel := forwardResultBillingModel(result.Model, result.UpstreamModel)
	billingModel := concreteBillingModel
	if input.BillingModelSource == BillingModelSourceChannelMapped && input.ChannelMappedModel != "" {
		billingModel = input.ChannelMappedModel
	}
	if input.BillingModelSource == BillingModelSourceRequested && input.OriginalModel != "" {
		billingModel = input.OriginalModel
	}
	// composite 分组的公开别名（如 all/claude）会经 OriginalModel/ChannelMappedModel
	// 进入上面的来源覆盖：任意别名查无价会静默落 $0，含家族词的别名则被价格表的
	// 家族模糊匹配错计（如 Opus 流量按 Sonnet 兜底价）。除非管理员为别名显式配置了
	// 渠道定价（OpenRouter 式自定价），composite 请求一律按实际转发的具体模型计费。
	if apiKey.Group != nil && apiKey.Group.Platform == PlatformComposite {
		billingModel = s.compositeBillableModel(ctx, apiKey, billingModel, concreteBillingModel)
	}
	// 通用兜底（与 OpenAI 路径的 usageBillingModelCandidates 语义对齐）：
	// 选定模型查不到任何价格时回退到实际转发的具体模型。已定价流量不受影响。
	billingModel = s.billableModelWithFallback(ctx, apiKey, billingModel, result.UpstreamModel, result.Model)
	cacheStorageCost := s.cacheStorageCost(result, billingModel)

	// 确定 RequestedModel（渠道映射前的原始模型）
	requestedModel := result.Model
	if input.OriginalModel != "" {
		requestedModel = input.OriginalModel
	}

	// 计算费用
	cost, pricingErr := s.calculateRecordUsageCost(ctx, result, apiKey, billingModel, multiplier, imageMultiplier, opts)
	if pricingErr != nil {
		cost = &CostBreakdown{}
	} else {
		cost = s.applyResponseModelBilling(ctx, input, result, apiKey, billingModel, multiplier, imageMultiplier, opts, cost)
	}
	useImageRate := usageUsesImageMultiplier(result.ImageCount, costBillingMode(cost), result.RequestCount, result.TaskCount, result.BillableDurationSeconds, result.BillableCharacterCount)
	applyRealActualCostFromRates(cost, useImageRate, multiplier, imageMultiplier, realMultiplier, realImageMultiplier, user)

	// 判断计费方式：订阅模式 vs 余额模式
	isSubscriptionBilling := subscription != nil && apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
	billingType := BillingTypeBalance
	if isSubscriptionBilling {
		billingType = BillingTypeSubscription
	}

	// 创建使用日志
	accountRateMultiplier := account.BillingRateMultiplier()
	usageLog := s.buildRecordUsageLog(ctx, input, result, apiKey, user, account, subscription,
		requestedModel, multiplier, imageMultiplier, unifiedRateMultiplier, accountRateMultiplier, billingType, cacheTTLOverridden, cost, opts)
	if pricingErr != nil {
		writeBillingFailedUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")
		return fmt.Errorf("calculate usage pricing: %w", pricingErr)
	}

	// 计算账号统计定价费用（使用最终上游模型匹配自定义规则）。纯存储费不是一次模型请求，
	// 账号统计成本回落 total_cost（官方存储价）。
	if apiKey.GroupID != nil && !isCacheStorageOnlyResult(result) {
		applyAccountStatsCost(ctx, usageLog, s.channelService, s.billingService,
			account.ID, *apiKey.GroupID, result.UpstreamModel, result.Model,
			// Anthropic's input_tokens excludes cache_read and cache_creation (billed separately);
			// OpenAI gateway uses actualInputTokens which also excludes cache_read for the same reason.
			result.Usage.billingUsageTokens(),
			cost.TotalCost-cacheStorageCost, pricingAt,
			accountStatsLongContextPricingEnabled(nil), result.upstreamPriceFactor(),
		)
		addCacheStorageToAccountStatsCost(usageLog, cacheStorageCost)
	}

	simpleModeKeyRateLimitOnly := simpleModeKeyRateLimitBillingEnabled(s.cfg, apiKey)
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple && !simpleModeKeyRateLimitOnly {
		writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")
		logger.LegacyPrintf("service.gateway", "[SIMPLE MODE] Usage recorded (not billed): user=%d, tokens=%d", usageLog.UserID, usageLog.TotalTokens())
		s.deferredService.ScheduleLastUsedUpdate(account.ID)
		return nil
	}

	// 配额平台由 handler 在请求 ctx 内经 QuotaPlatform() 算定并通过 input 传入；
	// 后扣运行在 worker 池的 background ctx 上，无法再从 ctx 取 ForcePlatform。
	// 缺省（未设置）时回退到分组平台，保持对其它调用方的兼容。
	quotaPlatform := input.QuotaPlatform
	if quotaPlatform == "" {
		quotaPlatform = PlatformFromAPIKey(apiKey)
		if quotaPlatform == PlatformComposite && account != nil {
			quotaPlatform = account.Platform
		}
	}
	requestID := usageLog.RequestID
	proxyDurationMs := int64(0)
	if result.Duration > 0 {
		proxyDurationMs = result.Duration.Milliseconds()
	}
	s.RecordProxyRequestStat(ctx, account, apiKey, true, proxyDurationMs, requestID, "service.gateway")
	_, billingErr := applyUsageBilling(ctx, requestID, usageLog, &postUsageBillingParams{
		Cost:                       cost,
		User:                       user,
		APIKey:                     apiKey,
		Account:                    account,
		Subscription:               subscription,
		RequestPayloadHash:         resolveUsageBillingPayloadFingerprint(ctx, input.RequestPayloadHash),
		IsSubscriptionBill:         isSubscriptionBilling && !simpleModeKeyRateLimitOnly,
		AccountRateMultiplier:      accountRateMultiplier,
		APIKeyService:              input.APIKeyService,
		Platform:                   quotaPlatform,
		SimpleModeKeyRateLimitOnly: simpleModeKeyRateLimitOnly,
	}, s.billingDeps(), s.usageBillingRepo)

	if billingErr != nil {
		usageLog.ActualCost = 0
		usageLog.RealActualCost = 0
		writeBillingFailedUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")
		return billingErr
	}
	writeUsageLogBestEffort(ctx, s.usageLogRepo, usageLog, "service.gateway")

	return nil
}

// recordUsageMultipliers 返回 token / 图片售价倍率（用户专属 > 分组默认 > 系统默认，token 倍率叠加高峰因子）
// 与计价时刻；pricingAt 为零值时取当前时刻。
func (s *GatewayService) recordUsageMultipliers(ctx context.Context, apiKey *APIKey, user *User, pricingAt time.Time) (float64, float64, time.Time) {
	multiplier := 1.0
	if s.cfg != nil {
		multiplier = s.cfg.Default.RateMultiplier
	}
	if apiKey != nil && apiKey.GroupID != nil && apiKey.Group != nil {
		multiplier = apiKey.Group.RateMultiplier
		if user != nil {
			multiplier = s.ResolveUserGroupRateMultiplier(ctx, user.ID, *apiKey.GroupID, multiplier)
		}
	}
	// 高峰因子按请求时刻现算，不并入 getUserGroupRateMultiplier，以免污染 user:group 倍率缓存。
	if pricingAt.IsZero() {
		pricingAt = timezone.Now()
	}
	multiplier = finalRateFromBaseMultiplier(multiplier, user)
	multiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, multiplier, pricingAt)
	return multiplier, imageMultiplier, pricingAt
}

// recordUsageBillingModel 按计费模型来源、composite 规则与无价兜底确定计费模型。
func (s *GatewayService) recordUsageBillingModel(ctx context.Context, apiKey *APIKey, result *ForwardResult, fields ChannelUsageFields) string {
	concreteBillingModel := forwardResultBillingModel(result.Model, result.UpstreamModel)
	billingModel := concreteBillingModel
	if fields.BillingModelSource == BillingModelSourceChannelMapped && fields.ChannelMappedModel != "" {
		billingModel = fields.ChannelMappedModel
	}
	if fields.BillingModelSource == BillingModelSourceRequested && fields.OriginalModel != "" {
		billingModel = fields.OriginalModel
	}
	// composite 分组的公开别名（如 all/claude）会经 OriginalModel/ChannelMappedModel
	// 进入上面的来源覆盖：任意别名查无价会静默落 $0，含家族词的别名则被价格表的
	// 家族模糊匹配错计（如 Opus 流量按 Sonnet 兜底价）。除非管理员为别名显式配置了
	// 渠道定价（OpenRouter 式自定价），composite 请求一律按实际转发的具体模型计费。
	if apiKey.Group != nil && apiKey.Group.Platform == PlatformComposite {
		billingModel = s.compositeBillableModel(ctx, apiKey, billingModel, concreteBillingModel)
	}
	// 通用兜底（与 OpenAI 路径的 usageBillingModelCandidates 语义对齐）：
	// 选定模型查不到任何价格时回退到实际转发的具体模型。已定价流量不受影响。
	return s.billableModelWithFallback(ctx, apiKey, billingModel, result.UpstreamModel, result.Model)
}

// cacheStoragePrice 返回计费模型的显式缓存存储单价；计费模型未配置时依次回退到实际转发模型与请求模型。
func (s *GatewayService) cacheStoragePrice(result *ForwardResult, billingModel string) float64 {
	if s.billingService == nil {
		return 0
	}
	for _, model := range []string{billingModel, result.UpstreamModel, result.Model} {
		if strings.TrimSpace(model) == "" {
			continue
		}
		if price := s.billingService.CacheStoragePricePerTokenHour(model); price > 0 {
			return price
		}
	}
	return 0
}

// cacheStorageCost 返回 result 的显式缓存存储费（USD，倍率前）。
func (s *GatewayService) cacheStorageCost(result *ForwardResult, billingModel string) float64 {
	if result.CacheStorageTokenHours <= 0 {
		return 0
	}
	return result.CacheStorageTokenHours * s.cacheStoragePrice(result, billingModel)
}

// isCacheStorageOnlyResult 判断 result 是否只有显式缓存存储费（延长有效期）：不含 token 用量，不算一次模型请求。
func isCacheStorageOnlyResult(result *ForwardResult) bool {
	if result == nil {
		return false
	}
	u := result.Usage
	return result.CacheStorageTokenHours > 0 && u.InputTokens == 0 && u.OutputTokens == 0 &&
		u.CacheCreationInputTokens == 0 && u.CacheReadInputTokens == 0 && u.ImageOutputTokens == 0 &&
		result.ImageCount == 0 && result.AudioUsage == nil && result.SearchCount == 0 &&
		result.RequestCount == 0 && result.TaskCount == 0 &&
		result.BillableDurationSeconds == 0 && result.BillableCharacterCount == 0
}

// CacheStoragePriced 判断 RecordUsage 对 input.Result 采用的计费模型（含回退候选）是否配置了显式缓存存储单价。
func (s *GatewayService) CacheStoragePriced(ctx context.Context, input *RecordUsageInput) bool {
	billingModel := s.recordUsageBillingModel(ctx, input.APIKey, input.Result, input.ChannelUsageFields)
	return s.cacheStoragePrice(input.Result, billingModel) > 0
}

// QuoteUsageCost 按与 RecordUsage 相同的倍率、计费模型与定价计算 input.Result 的应付金额（倍率后 USD），不记录也不扣费。
func (s *GatewayService) QuoteUsageCost(ctx context.Context, input *RecordUsageInput) float64 {
	user := input.User
	if user == nil {
		user = input.APIKey.User
	}
	multiplier, imageMultiplier, pricingAt := s.recordUsageMultipliers(ctx, input.APIKey, user, input.PricingAt)
	billingModel := s.recordUsageBillingModel(ctx, input.APIKey, input.Result, input.ChannelUsageFields)
	cost, err := s.calculateRecordUsageCost(ctx, input.Result, input.APIKey, billingModel, multiplier, imageMultiplier, &recordUsageOpts{PricingAt: pricingAt})
	if err != nil || cost == nil {
		return 0
	}
	return cost.ActualCost
}

// addCacheStorageToAccountStatsCost 把显式缓存存储费按官方价叠加到账号统计成本。
// 账号统计定价只覆盖 token 部分；统计成本为空时成本统计回落 total_cost，其中已含存储费。
func addCacheStorageToAccountStatsCost(usageLog *UsageLog, storageCost float64) {
	if usageLog == nil || usageLog.AccountStatsCost == nil || storageCost <= 0 {
		return
	}
	cost := *usageLog.AccountStatsCost + storageCost
	usageLog.AccountStatsCost = &cost
}

// upstreamPriceFactorApplies 判断上游端点价格倍数是否作用于本次 token 费用：只作用于默认模型价（LiteLLM/fallback）。
// 管理员配置的分组价、渠道价视为最终价，不再叠加倍数，与账号统计成本只在默认价档位加价的口径一致。
func (s *GatewayService) upstreamPriceFactorApplies(ctx context.Context, result *ForwardResult, apiKey *APIKey, billingModel string) bool {
	if result.upstreamPriceFactor() == 1 {
		return false
	}
	return s.resolveChannelPricing(ctx, billingModel, apiKey) == nil
}

// applyUpstreamPriceFactor 把上游端点价格倍数作用于按 token 计费的费用，按次等其他计费模式不受影响。
func applyUpstreamPriceFactor(cost *CostBreakdown, result *ForwardResult) {
	if cost == nil || (cost.BillingMode != "" && cost.BillingMode != string(BillingModeToken)) {
		return
	}
	applyCostBreakdownMultiplier(cost, result.upstreamPriceFactor())
}
func (s *GatewayService) applyResponseModelBilling(
	ctx context.Context,
	input *recordUsageCoreInput,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	imageMultiplier float64,
	opts *recordUsageOpts,
	baseline *CostBreakdown,
) *CostBreakdown {
	if s == nil || input == nil || result == nil || baseline == nil || s.billingService == nil {
		return baseline
	}
	responseModel := responseModelBillingDeclaration(
		input.BillingModelSource,
		result.UpstreamResponseModel,
		result.UpstreamResponseModelConflict,
		forwardResultHasNonTokenBilling(result),
	)
	if responseModel == "" {
		return baseline
	}
	responseChannelPriced := s.resolveChannelPricing(ctx, responseModel, apiKey) != nil
	if !responseChannelPriced && !s.billingService.HasIdentifiedTokenPricing(responseModel) {
		return baseline
	}
	responseCost, err := s.calculateRecordUsageCost(ctx, result, apiKey, responseModel, multiplier, imageMultiplier, opts)
	if err != nil {
		return baseline
	}
	if responseModelBillingAdoptable(
		baseline,
		responseCost,
		s.resolveChannelPricing(ctx, billingModel, apiKey) != nil,
		responseChannelPriced,
	) {
		return responseCost
	}
	return baseline
}

func forwardResultHasNonTokenBilling(result *ForwardResult) bool {
	if result == nil {
		return false
	}
	return result.ImageCount > 0 ||
		result.AudioUsage != nil ||
		result.BillableDurationSeconds > 0 ||
		result.BillableCharacterCount > 0 ||
		result.RequestCount > 0 ||
		result.TaskCount > 0 ||
		result.SearchCount > 0 ||
		result.CacheStorageTokenHours > 0
}

func (s *GatewayService) calculateRecordUsageCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	imageMultiplier float64,
	opts *recordUsageOpts,
) (*CostBreakdown, error) {
	var cost *CostBreakdown
	var err error
	if isCacheStorageOnlyResult(result) {
		cost = &CostBreakdown{}
	} else {
		cost, err = s.calculateRecordUsageBaseCost(ctx, result, apiKey, billingModel, multiplier, imageMultiplier, opts)
	}
	if err != nil {
		return nil, err
	}
	if storageCost := s.cacheStorageCost(result, billingModel); storageCost > 0 {
		if cost == nil {
			cost = &CostBreakdown{}
		}
		cost.TotalCost += storageCost
		cost.ActualCost += storageCost * multiplier
	}
	return cost, nil
}

func (s *GatewayService) calculateRecordUsageBaseCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	imageMultiplier float64,
	opts *recordUsageOpts,
) (*CostBreakdown, error) {
	if result != nil && result.EmulatedWebSearch {
		return s.billingService.CalculateSearchCost(result.SearchCount, groupSearchPricePer1kFromAPIKey(apiKey), multiplier), nil
	}
	if cost, err := s.calculateQuantityModeCost(ctx, result, apiKey, billingModel, multiplier); cost != nil || err != nil {
		if err != nil {
			return nil, err
		}
		return s.addSearchSurcharge(result, apiKey, multiplier, cost), nil
	}
	if result != nil && result.AudioUsage != nil {
		if resolved := resolveMediaPricing(ctx, s.resolver, billingModel, apiKey); resolved != nil &&
			resolved.Mode == BillingModePerRequest {
			gid, ok := apiKeyBillingGroupID(apiKey)
			if !ok {
				return nil, errors.New("channel audio pricing requires a billing group")
			}
			cost, err := s.billingService.CalculateCostUnified(CostInput{
				Ctx:             ctx,
				Model:           billingModel,
				GroupID:         &gid,
				Group:           apiKey.Group,
				UsageUnits:      result.AudioUsage.DurationOrUnits,
				SizeTier:        result.AudioUsage.Mode,
				RateMultiplier:  multiplier,
				ReasoningEffort: optionalStringValue(result.ReasoningEffort),
				Resolver:        s.resolver,
				Resolved:        resolved,
			})
			if err == nil {
				return s.addSearchSurcharge(result, apiKey, multiplier, cost), nil
			}
		}
		cfg := groupAudioPriceConfigFromAPIKey(apiKey)
		return s.billingService.CalculateAudioCost(
			result.AudioUsage.Mode,
			result.AudioUsage.DurationOrUnits,
			cfg,
			multiplier,
		), nil
	}
	var cost *CostBreakdown
	var err error
	if unitCount := resultBillableRequestCount(result); unitCount > 0 {
		if forwardResultHasTokenUsage(result) {
			if resolved := s.resolveChannelPricing(ctx, billingModel, apiKey); resolved == nil || resolved.Mode == BillingModeToken {
				cost, err = s.calculateTokenCost(ctx, result, apiKey, billingModel, multiplier, opts)
			} else {
				cost, err = s.calculateRequestUnitCost(ctx, result, apiKey, billingModel, multiplier, unitCount)
			}
		} else {
			cost, err = s.calculateRequestUnitCost(ctx, result, apiKey, billingModel, multiplier, unitCount)
		}
	} else if result.ImageCount > 0 {
		// 与 OpenAI 流水线同口径：分组显式开启按张计费且请求来自专用图片端点时，
		// 即便渠道/分组是 token 定价也必须按张结算，否则会以 token 价（分组图片
		// 定价缺失时甚至是 $0）覆盖图片单价。未开启该开关时保持原有回落顺序：
		// token 定价 + 有 token 用量 → 按 token，否则按图片价。
		if imageCountBillingApplies(apiKey, result.ImageEndpoint) {
			cost, err = s.calculateImageCost(ctx, result, apiKey, billingModel, imageMultiplier)
		} else if resolved := resolveMediaPricing(ctx, s.resolver, billingModel, apiKey); resolved != nil && resolved.Mode == BillingModeToken && forwardResultHasTokenUsage(result) {
			cost, err = s.calculateTokenCost(ctx, result, apiKey, billingModel, multiplier, opts)
		} else {
			cost, err = s.calculateImageCost(ctx, result, apiKey, billingModel, imageMultiplier)
		}
	} else {
		cost, err = s.calculateTokenCost(ctx, result, apiKey, billingModel, multiplier, opts)
	}
	if err != nil {
		return nil, err
	}
	return s.addSearchSurcharge(result, apiKey, multiplier, cost), nil
}

func (s *GatewayService) addSearchSurcharge(result *ForwardResult, apiKey *APIKey, multiplier float64, cost *CostBreakdown) *CostBreakdown {
	if s == nil || s.billingService == nil || result == nil || result.SearchCount <= 0 {
		return cost
	}
	searchCost := s.billingService.CalculateSearchCost(
		result.SearchCount,
		groupSearchPricePer1kFromAPIKey(apiKey),
		multiplier,
	)
	if cost == nil {
		return searchCost
	}
	cost.TotalCost += searchCost.TotalCost
	cost.ActualCost += searchCost.ActualCost
	return cost
}

// compositeBillableModel 决定 composite 分组请求的计费模型：来源覆盖把计费模型
// 换成公开别名等非具体模型时，只有管理员为该名字显式配置了渠道定价才按其计费
// （OpenRouter 式自定价），否则回退到实际转发的具体模型，避免别名落入价格表的
// 家族模糊匹配（错价）或查无价（$0）。未发生来源覆盖时原样返回。
func (s *GatewayService) compositeBillableModel(ctx context.Context, apiKey *APIKey, billingModel, concreteBillingModel string) string {
	if concreteBillingModel == "" || billingModel == concreteBillingModel {
		return billingModel
	}
	if s.resolveChannelPricing(ctx, billingModel, apiKey) != nil {
		return billingModel
	}
	logger.LegacyPrintf("service.gateway", "[Billing] composite billing model %q has no explicit channel pricing, billing by concrete model %q", billingModel, concreteBillingModel)
	return concreteBillingModel
}

// billableModelWithFallback 在选定计费模型（可能是 composite 公开别名或未定价的映射名）
// 查不到任何价格（渠道价与全局价均无）时，按序回退到实际转发的具体模型，避免静默 $0 计费。
// 所有候选都无价时保持原值，由计费路径返回价格缺失错误。
func (s *GatewayService) billableModelWithFallback(ctx context.Context, apiKey *APIKey, billingModel string, fallbacks ...string) string {
	if s.hasResolvableTokenPricing(ctx, billingModel, apiKey) {
		return billingModel
	}
	for _, fallback := range fallbacks {
		fallback = strings.TrimSpace(fallback)
		if fallback == "" || fallback == billingModel {
			continue
		}
		if s.hasResolvableTokenPricing(ctx, fallback, apiKey) {
			logger.LegacyPrintf("service.gateway", "[Billing] billing model %q has no pricing, falling back to concrete model %q", billingModel, fallback)
			return fallback
		}
	}
	return billingModel
}

// hasResolvableTokenPricing 判断模型是否能在渠道定价或全局价格表中解析出 token 价格。
func (s *GatewayService) hasResolvableTokenPricing(ctx context.Context, model string, apiKey *APIKey) bool {
	if strings.TrimSpace(model) == "" {
		return false
	}
	if s.resolveChannelPricing(ctx, model, apiKey) != nil {
		return true
	}
	if s.billingService == nil {
		return false
	}
	_, err := s.billingService.GetModelPricing(model)
	return err == nil
}

// resolveChannelPricing 检查指定模型是否存在渠道级别定价。
// 返回非 nil 的 ResolvedPricing 表示有渠道定价，nil 表示走默认定价路径。
func (s *GatewayService) resolveChannelPricing(ctx context.Context, billingModel string, apiKey *APIKey) *ResolvedPricing {
	if s.resolver == nil {
		return nil
	}
	gid, ok := apiKeyBillingGroupID(apiKey)
	if !ok {
		return nil
	}
	resolved := s.resolver.Resolve(ctx, PricingInput{Model: billingModel, GroupID: &gid, Group: apiKey.Group})
	if resolved.Source == PricingSourceChannel || resolved.Source == PricingSourceGroup {
		return resolved
	}
	return nil
}

// calculateImageCost 计算图片生成费用：渠道级别定价优先，否则走按次计费。
func (s *GatewayService) calculateImageCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
) (*CostBreakdown, error) {
	sizeTier := NormalizeImageBillingTierOrDefault(result.ImageSize)
	forcePerImage := imageCountBillingApplies(apiKey, result.ImageEndpoint)
	resolved := s.resolveChannelPricing(ctx, billingModel, apiKey)
	// 分组定价只有按次/图片模式才能按张结算。token 模式缺少 token 输入时会算出
	// $0，必须回落到分组图片价或默认图片价，避免开关开启后图片完全免费。
	if resolved != nil && resolved.Source == PricingSourceGroup &&
		(resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage) {
		gid := apiKey.Group.ID
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx: ctx, Model: billingModel, GroupID: &gid, Group: apiKey.Group,
			RequestCount: result.ImageCount, SizeTier: sizeTier,
			RateMultiplier: multiplier, Resolver: s.resolver, Resolved: resolved,
			ReasoningEffort: optionalStringValue(result.ReasoningEffort),
		})
		if err == nil {
			return cost, nil
		}
	}
	groupConfig := imagePriceConfigFromAPIKey(apiKey)
	if apiKeyHasConfiguredImagePrice(apiKey, sizeTier) {
		return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier), nil
	}
	if resolved := resolveMediaPricing(ctx, s.resolver, billingModel, apiKey); resolved != nil {
		tokens := UsageTokens{
			InputTokens:       result.Usage.InputTokens,
			OutputTokens:      result.Usage.OutputTokens,
			ImageOutputTokens: result.Usage.ImageOutputTokens,
		}
		switch {
		case resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage:
			// 按次/图片模式按请求张数结算。Tokens 不参与计价，但尺寸档未命中时
			// calculatePerRequestCost 要用它匹配按上下文分档的单价，不能省略。
			gid := apiKey.Group.ID
			cost, err := s.billingService.CalculateCostUnified(CostInput{
				Ctx:             ctx,
				Model:           billingModel,
				GroupID:         &gid,
				Group:           apiKey.Group,
				Tokens:          tokens,
				RequestCount:    result.ImageCount,
				SizeTier:        sizeTier,
				ReasoningEffort: optionalStringValue(result.ReasoningEffort),
				RateMultiplier:  multiplier,
				Resolver:        s.resolver,
				Resolved:        resolved,
			})
			if err != nil {
				logger.LegacyPrintf("service.gateway", "Calculate image channel cost failed: %v", err)
				return &CostBreakdown{ActualCost: 0}, nil
			}
			return cost, nil
		case !forcePerImage:
			// token 模式且开关未强制按张：保持原 token 计费。
			gid := apiKey.Group.ID
			cost, err := s.billingService.CalculateCostUnified(CostInput{
				Ctx:             ctx,
				Model:           billingModel,
				GroupID:         &gid,
				Group:           apiKey.Group,
				Tokens:          tokens,
				RequestCount:    result.ImageCount,
				SizeTier:        sizeTier,
				ReasoningEffort: optionalStringValue(result.ReasoningEffort),
				RateMultiplier:  multiplier,
				Resolver:        s.resolver,
				Resolved:        resolved,
			})
			if err != nil {
				logger.LegacyPrintf("service.gateway", "Calculate image token cost failed: %v", err)
				return &CostBreakdown{ActualCost: 0}, nil
			}
			return cost, nil
		}
		// token 模式 + 开关强制按张：跳过 token 分支，落到下面的按张计费。
	}

	return s.billingService.CalculateImageCost(billingModel, sizeTier, result.ImageCount, groupConfig, multiplier), nil
}

func (usage ClaudeUsage) billingUsageTokens() UsageTokens {
	creationTokens := usage.CacheCreationInputTokens
	if creationTokens <= 0 {
		creationTokens = max(usage.CacheCreation5mTokens, 0) + max(usage.CacheCreation1hTokens, 0)
	}
	tokens := UsageTokens{
		InputTokens:           usage.InputTokens,
		OutputTokens:          usage.OutputTokens,
		CacheCreationTokens:   creationTokens,
		CacheReadTokens:       usage.CacheReadInputTokens,
		CacheCreation5mTokens: usage.CacheCreation5mTokens,
		CacheCreation1hTokens: usage.CacheCreation1hTokens,
		ImageOutputTokens:     usage.ImageOutputTokens,
	}
	return tokens
}

// calculateTokenCost 计算 Token 计费，并在返回前叠加“上游端点价格倍数”
// （Backport 自上游 v0.2.23 的 calculateRecordUsageCost：Vertex 多区域端点
// 相对 global 价按倍数计价，见 ForwardResult.UpstreamPriceMultiplier）。
// 倍数只作用于默认模型价（LiteLLM/fallback）；管理员配置的分组价、渠道价
// 视为最终价，不再叠加（判定见 upstreamPriceFactorApplies）。
// 按次/图片等非 token 计费模式由 applyUpstreamPriceFactor 自行跳过。
func (s *GatewayService) calculateTokenCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	opts *recordUsageOpts,
) (*CostBreakdown, error) {
	cost, err := s.calculateTokenCostBase(ctx, result, apiKey, billingModel, multiplier, opts)
	if err != nil || cost == nil {
		return cost, err
	}
	if s.upstreamPriceFactorApplies(ctx, result, apiKey, billingModel) {
		applyUpstreamPriceFactor(cost, result)
	}
	return cost, nil
}

// calculateTokenCostBase 计算 Token 计费：路径选择（分组/渠道定价 → 内置定价）
// 统一交给 BillingService.CalculateTokenCostForRequest，与模型广场的阶梯表查询同源。
func (s *GatewayService) calculateTokenCostBase(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	opts *recordUsageOpts,
) (*CostBreakdown, error) {
	tokens := UsageTokens{
		InputTokens:           result.Usage.InputTokens,
		OutputTokens:          result.Usage.OutputTokens,
		CacheCreationTokens:   result.Usage.CacheCreationInputTokens,
		CacheReadTokens:       result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens: result.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens: result.Usage.CacheCreation1hTokens,
		ImageOutputTokens:     result.Usage.ImageOutputTokens,
	}

	// Default DeepSeek cards need the same request-time policy as the unified
	// gateway. Resolve custom cards first so explicit prices, including zero,
	// remain exempt from the default-card peak factor.
	if isDeepSeekModel(billingModel) {
		resolver := s.resolver
		if resolver == nil {
			resolver = NewModelPricingResolver(nil, s.billingService)
		}
		input := CostInput{
			Ctx:            ctx,
			Model:          billingModel,
			Tokens:         tokens,
			RequestCount:   1,
			RateMultiplier: multiplier,
			Resolver:       resolver,
		}
		if apiKey != nil {
			input.Group = apiKey.Group
			if gid, ok := apiKeyBillingGroupID(apiKey); ok {
				input.GroupID = &gid
			}
		}
		if opts != nil {
			input.PricingAt = opts.PricingAt
		}
		input.Resolved = resolver.Resolve(ctx, PricingInput{Model: billingModel, GroupID: input.GroupID, Group: input.Group})
		if opts != nil && opts.LongContextThreshold > 0 && input.Resolved.Source != PricingSourceChannel && input.Resolved.Source != PricingSourceGroup {
			return calculateCostWithLongContext(tokens, multiplier, opts.LongContextThreshold, opts.LongContextMultiplier, func(part UsageTokens, rate float64) (*CostBreakdown, error) {
				partInput := input
				partInput.Tokens, partInput.RateMultiplier = part, rate
				return s.billingService.CalculateCostUnified(partInput)
			})
		}
		return s.billingService.CalculateCostUnified(input)
	}

	var cost *CostBreakdown
	var err error

	// 优先尝试渠道定价 → CalculateCostUnified
	if resolved := s.resolveChannelPricing(ctx, billingModel, apiKey); resolved != nil {
		gid, ok := apiKeyBillingGroupID(apiKey)
		if !ok {
			return nil, errors.New("channel token pricing requires a billing group")
		}
		var pricingAt time.Time
		if opts != nil {
			pricingAt = opts.PricingAt
		}
		cost, err = s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          billingModel,
			GroupID:        &gid,
			Group:          apiKey.Group,
			Tokens:         tokens,
			RequestCount:   1,
			RateMultiplier: multiplier,
			PricingAt:      pricingAt,
			// 按实际服务档位计价：channel 定价里的 fast/priority/ultrafast 分档价
			// 只有在把档位传进来时才会命中；此前该字段始终为空，等于所有请求
			// 一律按标准档收费（"点了加急却按普通件收钱"）。
			ServiceTier:     optionalStringValue(result.ServiceTier),
			ReasoningEffort: optionalStringValue(result.ReasoningEffort),
			Resolver:        s.resolver,
			Resolved:        resolved,
		})
	} else if opts != nil && opts.LongContextThreshold > 0 {
		// 长上下文双倍计费（如 Gemini 200K 阈值）。该分支沿用既有实现（阈值 + 倍率），
		// 档位不在其中叠加，避免"长上下文 × 档位"两个乘数重复作用。
		cost, err = s.billingService.CalculateCostWithLongContext(billingModel, tokens, multiplier, opts.LongContextThreshold, opts.LongContextMultiplier)
	} else {
		// 内置目录定价：同样按实际服务档位取价（fast/priority 价卡由 service tier 策略改写）。
		var pricingAt time.Time
		if opts != nil {
			pricingAt = opts.PricingAt
		}
		cost, err = s.billingService.calculateCostWithServiceTierPolicyAt(
			billingModel,
			tokens,
			multiplier,
			optionalStringValue(result.ServiceTier),
			true,
			pricingAt,
		)
	}
	return cost, err
}

// buildRecordUsageLog 构建使用日志并设置计费模式。
func (s *GatewayService) buildRecordUsageLog(
	ctx context.Context,
	input *recordUsageCoreInput,
	result *ForwardResult,
	apiKey *APIKey,
	user *User,
	account *Account,
	subscription *UserSubscription,
	requestedModel string,
	multiplier float64,
	imageMultiplier float64,
	unifiedRateMultiplier float64,
	accountRateMultiplier float64,
	billingType int8,
	cacheTTLOverridden bool,
	cost *CostBreakdown,
	opts *recordUsageOpts,
) *UsageLog {
	durationMs := int(result.Duration.Milliseconds())
	requestID := resolveUsageBillingRequestID(ctx, result.RequestID)
	sentModel := upstreamSentModel(result.Model, result.UpstreamModel)
	if result.UpstreamResponseModelConflict {
		slog.Warn("upstream_response_model_conflict",
			"platform", account.Platform,
			"account_id", account.ID,
			"request_id", requestID,
			"sent_model", sentModel,
			"selected_response_model", strings.TrimSpace(result.UpstreamResponseModel),
		)
	}
	usageLog := &UsageLog{
		ProxyRoutes:           ProxyRoutesFromContext(ctx),
		UserID:                user.ID,
		APIKeyID:              apiKey.ID,
		AccountID:             account.ID,
		RequestID:             requestID,
		Model:                 result.Model,
		RequestedModel:        requestedModel,
		UpstreamModel:         optionalUpstreamModelPtr(result.UpstreamModel, result.Model, requestedModel),
		UpstreamResponseModel: optionalTrimmedStringPtr(result.UpstreamResponseModel),
		UpstreamModelMismatch: upstreamModelMismatch(sentModel, result.UpstreamResponseModel),
		// 落库的档位必须是"实际计费档位"（已在 recordUsageCore 顶部按上游声明降级），
		// 否则报表显示 priority 而金额按 standard 算，对不上账。
		ServiceTier:              result.ServiceTier,
		ReasoningEffort:          result.ReasoningEffort,
		RequestedReasoningEffort: result.RequestedReasoningEffort,
		InboundEndpoint:          optionalTrimmedStringPtr(input.InboundEndpoint),
		UpstreamEndpoint:         optionalTrimmedStringPtr(input.UpstreamEndpoint),
		ClientProfile:            optionalTrimmedStringPtr(input.ClientProfile),
		CompatibilityRoute:       optionalTrimmedStringPtr(input.CompatibilityRoute),
		FallbackChain:            optionalTrimmedStringPtr(input.FallbackChain),
		UpstreamTransport:        optionalTrimmedStringPtr(input.UpstreamTransport),
		InputTokens:              result.Usage.InputTokens,
		OutputTokens:             result.Usage.OutputTokens,
		CacheCreationTokens:      result.Usage.CacheCreationInputTokens,
		CacheReadTokens:          result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens:    result.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens:    result.Usage.CacheCreation1hTokens,
		ImageOutputTokens:        result.Usage.ImageOutputTokens,
		RateMultiplier:           multiplier,
		UnifiedRateMultiplier:    unifiedRateMultiplier,
		AccountRateMultiplier:    &accountRateMultiplier,
		BillingType:              billingType,
		BillingMode:              resolveBillingMode(result, cost),
		Stream:                   result.Stream,
		DurationMs:               &durationMs,
		FirstTokenMs:             result.FirstTokenMs,
		ImageCount:               result.ImageCount,
		ImageSize:                optionalTrimmedStringPtr(result.ImageSize),
		ImageInputSize:           optionalTrimmedStringPtr(result.ImageInputSize),
		ImageOutputSize:          optionalTrimmedStringPtr(result.ImageOutputSize),
		ImageSizeSource:          optionalTrimmedStringPtr(result.ImageSizeSource),
		ImageSizeBreakdown:       result.ImageSizeBreakdown,
		RequestCount:             result.RequestCount,
		TaskCount:                result.TaskCount,
		BillableDurationSeconds:  result.BillableDurationSeconds,
		BillableCharacterCount:   result.BillableCharacterCount,
		UsageEstimated:           result.UsageEstimated,
		BillableUnitType:         optionalTrimmedStringPtr(result.BillableUnitType),
		CacheTTLOverridden:       cacheTTLOverridden,
		ChannelID:                optionalInt64Ptr(input.ChannelID),
		ModelMappingChain:        optionalTrimmedStringPtr(input.ModelMappingChain),
		UserAgent:                optionalTrimmedStringPtr(input.UserAgent),
		IPAddress:                optionalTrimmedStringPtr(input.IPAddress),
		SessionID:                optionalTrimmedStringPtr(input.SessionID),
		GroupID:                  apiKey.GroupID,
		SubscriptionID:           optionalSubscriptionID(subscription),
		CreatedAt:                time.Now(),
	}
	if usageUsesImageMultiplier(result.ImageCount, costBillingMode(cost), result.RequestCount, result.TaskCount, result.BillableDurationSeconds, result.BillableCharacterCount) {
		usageLog.RateMultiplier = imageMultiplier
	}
	if cost != nil {
		usageLog.InputCost = cost.InputCost
		usageLog.OutputCost = cost.OutputCost
		usageLog.ImageOutputCost = cost.ImageOutputCost
		usageLog.CacheCreationCost = cost.CacheCreationCost
		usageLog.CacheReadCost = cost.CacheReadCost
		usageLog.TotalCost = cost.TotalCost
		usageLog.ActualCost = cost.ActualCost
		usageLog.RealActualCost = cost.RealActualCost
		usageLog.LongContextBillingApplied = cost.LongContextBillingApplied
	}

	return usageLog
}

// resolveBillingMode 根据计费结果和请求类型确定计费模式。
func resolveBillingMode(result *ForwardResult, cost *CostBreakdown) *string {
	var mode string
	switch {
	case cost != nil && cost.BillingMode != "":
		mode = cost.BillingMode
	case result != nil && result.BillableDurationSeconds > 0:
		mode = string(BillingModeDuration)
	case result != nil && result.BillableCharacterCount > 0:
		mode = string(BillingModeCharacter)
	case resultBillableRequestCount(result) > 0:
		mode = string(BillingModePerRequest)
	case result != nil && result.ImageCount > 0:
		mode = string(BillingModeImage)
	default:
		mode = string(BillingModeToken)
	}
	return &mode
}

func optionalSubscriptionID(subscription *UserSubscription) *int64 {
	if subscription != nil && subscription.ID > 0 && !subscription.IsAggregate {
		return &subscription.ID
	}
	return nil
}

func (s *GatewayService) calculateQuantityModeCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
) (*CostBreakdown, error) {
	if result == nil {
		return nil, nil
	}
	resolved := s.resolveChannelPricing(ctx, billingModel, apiKey)
	if resolved == nil || (resolved.Mode != BillingModeDuration && resolved.Mode != BillingModeCharacter) {
		return nil, nil
	}
	gid, ok := apiKeyBillingGroupID(apiKey)
	if !ok {
		return nil, errors.New("channel quantity pricing requires a billing group")
	}
	cost, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx:             ctx,
		Model:           billingModel,
		GroupID:         &gid,
		DurationSeconds: result.BillableDurationSeconds,
		CharacterCount:  result.BillableCharacterCount,
		RateMultiplier:  multiplier,
		Resolver:        s.resolver,
		Resolved:        resolved,
	})
	return cost, err
}

func forwardResultHasTokenUsage(result *ForwardResult) bool {
	if result == nil {
		return false
	}
	return result.Usage.InputTokens > 0 ||
		result.Usage.OutputTokens > 0 ||
		result.Usage.CacheCreationInputTokens > 0 ||
		result.Usage.CacheReadInputTokens > 0 ||
		result.Usage.CacheCreation5mTokens > 0 ||
		result.Usage.CacheCreation1hTokens > 0 ||
		result.Usage.ImageOutputTokens > 0
}

func resultBillableRequestCount(result *ForwardResult) int {
	if result == nil {
		return 0
	}
	if result.TaskCount > 0 {
		return result.TaskCount
	}
	if result.RequestCount > 0 {
		return result.RequestCount
	}
	return 0
}

func (s *GatewayService) calculateRequestUnitCost(
	ctx context.Context,
	result *ForwardResult,
	apiKey *APIKey,
	billingModel string,
	multiplier float64,
	count int,
) (*CostBreakdown, error) {
	if count <= 0 {
		return &CostBreakdown{ActualCost: 0, BillingMode: string(BillingModePerRequest)}, nil
	}
	if resolved := s.resolveChannelPricing(ctx, billingModel, apiKey); resolved != nil {
		gid, ok := apiKeyBillingGroupID(apiKey)
		if !ok {
			return nil, errors.New("channel request pricing requires a billing group")
		}
		cost, err := s.billingService.CalculateCostUnified(CostInput{
			Ctx:            ctx,
			Model:          billingModel,
			GroupID:        &gid,
			RequestCount:   count,
			SizeTier:       strings.TrimSpace(result.BillableUnitType),
			RateMultiplier: multiplier,
			Resolver:       s.resolver,
			Resolved:       resolved,
		})
		return cost, err
	}
	logger.LegacyPrintf(
		"service.gateway",
		"new-api billable unit has no channel pricing: model=%s request_count=%d task_count=%d unit_type=%s",
		billingModel,
		result.RequestCount,
		result.TaskCount,
		result.BillableUnitType,
	)
	return nil, fmt.Errorf("request/task pricing not found for model: %s", billingModel)
}

func apiKeyBillingGroupID(apiKey *APIKey) (int64, bool) {
	if apiKey == nil {
		return 0, false
	}
	if apiKey.GroupID != nil {
		return *apiKey.GroupID, true
	}
	if apiKey.Group != nil {
		return apiKey.Group.ID, true
	}
	return 0, false
}
