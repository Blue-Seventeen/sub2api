package handler

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

// TempUnscheduler 用于 HandleFailoverError 中同账号重试耗尽后的临时封禁。
// GatewayService 隐式实现此接口。
type TempUnscheduler interface {
	TempUnscheduleRetryableError(ctx context.Context, accountID int64, failoverErr *service.UpstreamFailoverError)
}

// FailoverAction 表示 failover 错误处理后的下一步动作
type FailoverAction int

const (
	// FailoverContinue 继续循环（同账号重试或切换账号，调用方统一 continue）
	FailoverContinue FailoverAction = iota
	// FailoverExhausted 无剩余候选或请求不允许重放（调用方返回最终错误）
	FailoverExhausted
	// FailoverCanceled context 已取消（调用方应直接 return）
	FailoverCanceled
)

const (
	// maxSameAccountRetries 同账号重试次数默认上限（针对 RetryableOnSameAccount 错误）。
	// 生产调用方通常传入账号级配置 account.GetPoolModeRetryCount()，该常量仅作兜底/测试默认值。
	maxSameAccountRetries = 3
	// sameAccountRetryDelay 同账号重试间隔
	sameAccountRetryDelay = 500 * time.Millisecond
	// maxRequestScopedRetryDelay 限制请求级瞬时错误的指数退避上限，避免高重试配置
	// 将单次请求拖入分钟级等待。
	maxRequestScopedRetryDelay = 8 * time.Second
	// singleAccountBackoffDelay 单账号分组 503 退避重试固定延时。
	// Service 层在 SingleAccountRetry 模式下已做充分原地重试（最多 3 次、总等待 30s），
	// Handler 层只需短暂间隔后重新进入 Service 层即可。
	singleAccountBackoffDelay = 2 * time.Second
)

// profitVetoExhaustedMessage 是利润否决次数耗尽时返回给客户端的文案。
// 语义上等同于「无可用账号」：候选账号都不满足分组的利润约束。
const profitVetoExhaustedMessage = "No available accounts: all candidates rejected by group profit control"

func sameAccountRetryDelayFor(failoverErr *service.UpstreamFailoverError, retryCount int) time.Duration {
	if failoverErr == nil {
		return sameAccountRetryDelay
	}
	if failoverErr.SameAccountRetryDelay > 0 {
		return failoverErr.SameAccountRetryDelay
	}
	if !failoverErr.RequestScopedTransient || retryCount <= 1 {
		return sameAccountRetryDelay
	}

	delay := sameAccountRetryDelay
	for i := 1; i < retryCount; i++ {
		if delay >= maxRequestScopedRetryDelay/2 {
			return maxRequestScopedRetryDelay
		}
		delay *= 2
	}
	return delay
}

func sameAccountRetryAllowed(failoverErr *service.UpstreamFailoverError, retryCount, retryLimit int) bool {
	if failoverErr == nil || !failoverErr.RetryableOnSameAccount {
		return false
	}
	if !sameAccountRetryDeadlineAllows(failoverErr) {
		return false
	}
	// Error-specific caps (Grok capacity/stream-idle) remain hard limits even
	// when the error also carries a freshly reconstructed deadline.
	if failoverErr.SameAccountRetryMax > 0 {
		if retryLimit <= 0 {
			return false
		}
		if failoverErr.SameAccountRetryMax < retryLimit {
			retryLimit = failoverErr.SameAccountRetryMax
		}
		return retryCount < retryLimit
	}
	// OAuth 429 explicitly opts into a deadline window. It is intentionally not
	// bounded by the ordinary/default pool retry count.
	if !failoverErr.SameAccountRetryDeadline.IsZero() {
		return true
	}
	return retryLimit > 0 && retryCount < retryLimit
}

// sameAccountRetryDeadlineAllows prevents a retry from starting after the
// service-provided same-account retry window has elapsed.
func sameAccountRetryDeadlineAllows(failoverErr *service.UpstreamFailoverError) bool {
	return failoverErr == nil || failoverErr.SameAccountRetryDeadline.IsZero() || time.Now().Before(failoverErr.SameAccountRetryDeadline)
}

// effectiveSameAccountRetryLimit applies an error-specific cap without
// overriding an explicit account setting of zero (which disables retries).
func effectiveSameAccountRetryLimit(failoverErr *service.UpstreamFailoverError, account *service.Account) int {
	if account == nil {
		return 0
	}
	limit := account.GetPoolModeRetryCount()
	if limit > 0 && failoverErr != nil && failoverErr.SameAccountRetryMax > 0 && failoverErr.SameAccountRetryMax < limit {
		return failoverErr.SameAccountRetryMax
	}
	return limit
}

// FailoverState 跨循环迭代共享的 failover 状态
type FailoverState struct {
	SwitchCount           int
	MaxSwitches           int
	FailedAccountIDs      map[int64]struct{}
	SameAccountRetryCount map[int64]int
	LastFailoverErr       *service.UpstreamFailoverError
	ForceCacheBilling     bool
	hasBoundSession       bool
	pendingRetryAccountID int64

	// profitVetoCount 本次请求累计的利润否决次数，用于诊断候选耗尽。
	profitVetoCount int
}

// NewFailoverState 创建 failover 状态
func NewFailoverState(maxSwitches int, hasBoundSession bool) *FailoverState {
	return &FailoverState{
		MaxSwitches:           maxSwitches,
		FailedAccountIDs:      make(map[int64]struct{}),
		SameAccountRetryCount: make(map[int64]int),
		hasBoundSession:       hasBoundSession,
	}
}

// SelectionContext pins a pending same-account retry without depending on a
// best-effort sticky binding. The selector still enforces fresh hard limits.
func (s *FailoverState) SelectionContext(ctx context.Context) context.Context {
	if s.MaxSwitches == 0 && s.pendingRetryAccountID == 0 {
		for id := range s.FailedAccountIDs {
			// Retain the explicit no-switch contract on admission failures too.
			// Pinning an excluded owner makes selection terminate without roaming.
			return service.WithGatewayRetryAccount(ctx, id)
		}
	}
	return service.WithGatewayRetryAccount(ctx, s.pendingRetryAccountID)
}

// HandleRetrySelectionError retires an account that became ineligible between
// retries. Infrastructure errors and cancellation retain their normal handling.
func (s *FailoverState) HandleRetrySelectionError(err error) bool {
	if s.pendingRetryAccountID == 0 || !errors.Is(err, service.ErrSameAccountRetryUnavailable) {
		return false
	}
	if s.MaxSwitches == 0 {
		return false
	}
	s.FailedAccountIDs[s.pendingRetryAccountID] = struct{}{}
	s.pendingRetryAccountID = 0
	s.SwitchCount++
	if s.hasBoundSession {
		s.ForceCacheBilling = true
	}
	return true
}

// RecordProfitVeto excludes each rejected candidate once. A repeated excluded
// ID indicates a selector contract violation and stops an otherwise empty loop.
func (s *FailoverState) RecordProfitVeto(accountID int64) FailoverAction {
	if _, repeated := s.FailedAccountIDs[accountID]; repeated {
		return FailoverExhausted
	}
	s.FailedAccountIDs[accountID] = struct{}{}
	s.pendingRetryAccountID = 0
	s.profitVetoCount++
	if s.MaxSwitches == 0 {
		return FailoverExhausted
	}
	return FailoverContinue
}

// ProfitVetoCount 返回本次请求累计的利润否决次数（供日志使用）。
func (s *FailoverState) ProfitVetoCount() int { return s.profitVetoCount }

// HandleFailoverError 处理 UpstreamFailoverError，返回下一步动作。
// 包含：缓存计费判断、同账号重试、临时封禁、切换计数、Antigravity 延时。
func (s *FailoverState) HandleFailoverError(
	ctx context.Context,
	gatewayService TempUnscheduler,
	accountID int64,
	platform string,
	retryLimit int,
	failoverErr *service.UpstreamFailoverError,
) FailoverAction {
	// 客户端已断开：failover 只会用已取消的 context 重新选号并必然失败，
	// 不应再被当成账号耗尽处理（误报 502）。
	if ctx != nil && ctx.Err() != nil {
		return FailoverCanceled
	}
	s.LastFailoverErr = failoverErr
	if failoverErr == nil || !failoverErr.ShouldRetryNextAccount() {
		return FailoverExhausted
	}

	// 同账号重试不算切换账号，粘性会话仅在实际切换时强制缓存计费。
	retryCount := s.SameAccountRetryCount[accountID]
	sameAccountRetry := sameAccountRetryAllowed(failoverErr, retryCount, retryLimit)
	if needForceCacheBilling(s.hasBoundSession, failoverErr, sameAccountRetry) {
		s.ForceCacheBilling = true
	}

	// 同账号重试：对 RetryableOnSameAccount 的临时性错误，先在同一账号上重试。
	// 重试次数上限 retryLimit 由调用方传入（账号级 pool_mode_retry_count 配置）。
	if sameAccountRetry {
		s.pendingRetryAccountID = accountID
		s.SameAccountRetryCount[accountID]++
		retryDelay := sameAccountRetryDelayFor(failoverErr, s.SameAccountRetryCount[accountID])
		logger.FromContext(ctx).Warn("gateway.failover_same_account_retry",
			zap.Int64("account_id", accountID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("same_account_retry_count", s.SameAccountRetryCount[accountID]),
			zap.Int("same_account_retry_max", retryLimit),
			zap.Duration("retry_delay", retryDelay),
		)
		if !sleepWithContext(ctx, retryDelay) {
			return FailoverCanceled
		}
		return FailoverContinue
	}

	// 同账号重试用尽，执行临时封禁
	if failoverErr.RetryableOnSameAccount && gatewayService != nil {
		gatewayService.TempUnscheduleRetryableError(ctx, accountID, failoverErr)
	}

	// 加入失败列表
	s.pendingRetryAccountID = 0
	s.FailedAccountIDs[accountID] = struct{}{}

	// 检查是否耗尽
	if s.MaxSwitches == 0 {
		return FailoverExhausted
	}

	// 递增切换计数
	s.SwitchCount++
	logger.FromContext(ctx).Warn("gateway.failover_switch_account",
		zap.Int64("account_id", accountID),
		zap.Int("upstream_status", failoverErr.StatusCode),
		zap.Int("switch_count", s.SwitchCount),
		zap.Int("max_switches", s.MaxSwitches),
	)

	// Antigravity 平台换号线性递增延时
	if platform == service.PlatformAntigravity {
		delay := time.Duration(s.SwitchCount-1) * time.Second
		if delay > singleAccountBackoffDelay {
			delay = singleAccountBackoffDelay
		}
		if !sleepWithContext(ctx, delay) {
			return FailoverCanceled
		}
	}

	return FailoverContinue
}

// HandleSelectionExhausted stops after the finite initial candidate set has
// been visited. Per-account retry budgets were already consumed before exclusion.
func (s *FailoverState) HandleSelectionExhausted(ctx context.Context) FailoverAction {
	// 客户端已断开时选号失败是 context canceled 的必然结果，
	// 不代表账号耗尽，直接按取消终止。
	if ctx.Err() != nil {
		return FailoverCanceled
	}

	// The per-account retry budget is consumed before exclusion. Reopening the
	// failed set here would replay the entire pool and make exhaustion unbounded.
	return FailoverExhausted
}

// needForceCacheBilling 判断 failover 时是否需要强制缓存计费。
// 粘性会话实际切换账号、或上游明确标记时，将 input_tokens 转为 cache_read 计费。
func needForceCacheBilling(hasBoundSession bool, failoverErr *service.UpstreamFailoverError, sameAccountRetry bool) bool {
	return (hasBoundSession && !sameAccountRetry) || (failoverErr != nil && failoverErr.ForceCacheBilling)
}

// failoverClientGone 判断下游客户端是否已断开（请求 context 已取消）。
// 客户端断开后 failover 必须静默终止：用已取消的 context 重新选号只会得到
// context.Canceled，并被误报成账号耗尽（通用 502）；上游 detach 的在途请求
// 照常完成计费，但不再为无人接收的响应启动新的上游尝试。
// 响应尚未提交时把状态码标记为 499（client closed request），供访问日志归类。
func failoverClientGone(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Context().Err() == nil {
		return false
	}
	// 先停 compact 心跳（接管 ResponseWriter，建立 happens-before），与
	// handleStreamingAwareError/errorResponse 等终结路径对齐，避免心跳
	// goroutine 与下面的状态标记并发触碰同一 writer。心跳已提交 200 时
	// 状态码已固化，不再标 499。
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		return true
	}
	if !c.Writer.Written() {
		c.Status(statusClientClosedRequest)
	}
	return true
}

// sleepWithContext 等待指定时长，返回 false 表示 context 已取消。
func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// accountAdmissionUnavailable distinguishes a busy account from a global or
// infrastructure failure, without committing an intermediate client error.
func accountAdmissionUnavailable(ctx context.Context, err error) bool {
	if err == nil || ctx == nil || ctx.Err() != nil {
		return false
	}
	var busy *ConcurrencyError
	return errors.As(err, &busy) && busy.SlotType == "account"
}
