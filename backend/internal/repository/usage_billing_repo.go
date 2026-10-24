package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageBillingRepository struct {
	db *sql.DB
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, cmd.MatchesRequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string, matchesFingerprint func(string) bool) (bool, error) {
	if matchesFingerprint == nil {
		expected := strings.TrimSpace(requestFingerprint)
		matchesFingerprint = func(candidate string) bool {
			return strings.TrimSpace(candidate) == expected
		}
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if !matchesFingerprint(existingFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if !matchesFingerprint(archivedFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint, nil)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 {
		var (
			subID int64
			err   error
		)
		if cmd.SubscriptionID != nil && *cmd.SubscriptionID > 0 {
			subID = *cmd.SubscriptionID
			err = incrementUsageBillingSubscription(ctx, tx, subID, cmd.SubscriptionCost)
		} else if cmd.SubscriptionUserID > 0 && cmd.SubscriptionGroupID > 0 {
			subID, err = incrementUsageBillingSubscriptionStacked(ctx, tx, cmd.SubscriptionUserID, cmd.SubscriptionGroupID, cmd.SubscriptionCost)
		}
		if err != nil {
			return err
		}
		if subID > 0 {
			result.SubscriptionID = &subID
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	// API Key 在请求进行中被删除时，其自身的额度/限速计数已无意义，跳过即可；
	// 但用户余额、订阅与账号额度必须照常结算，不能因此回滚整个计费事务。
	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost); err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
	}

	// 账号已不存在时跳过其配额计数，其余结算项不受影响。
	if cmd.AccountQuotaCost > 0 && service.AccountTypeSupportsQuota(cmd.AccountType) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost, cmd.AccountQuotaHasDailyLimit, cmd.AccountQuotaHasWeeklyLimit)
		if err != nil && !errors.Is(err, service.ErrAccountNotFound) {
			return err
		}
		result.QuotaState = quotaState
	}

	return nil
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			custom_usage_usd = us.custom_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

type billingSubscriptionCandidate struct {
	id                 int64
	startsAt           time.Time
	expiresAt          time.Time
	dailyUsage         float64
	weeklyUsage        float64
	monthlyUsage       float64
	customUsage        float64
	dailyWindowStart   sql.NullTime
	weeklyWindowStart  sql.NullTime
	monthlyWindowStart sql.NullTime
	customWindowStart  sql.NullTime
	dailyLimit         sql.NullFloat64
	weeklyLimit        sql.NullFloat64
	monthlyLimit       sql.NullFloat64
	customLimitHours   sql.NullInt64
	customLimit        sql.NullFloat64
}

const billingMaxCustomLimitHours = 24 * 365 * 10

func incrementUsageBillingSubscriptionStacked(ctx context.Context, tx *sql.Tx, userID, groupID int64, costUSD float64) (int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			us.id,
			us.starts_at,
			us.expires_at,
			us.daily_usage_usd,
			us.weekly_usage_usd,
			us.monthly_usage_usd,
			us.custom_usage_usd,
			us.daily_window_start,
			us.weekly_window_start,
			us.monthly_window_start,
			us.custom_window_start,
			g.daily_limit_usd,
			g.weekly_limit_usd,
			g.monthly_limit_usd,
			g.custom_limit_hours,
			g.custom_limit_usd
		FROM user_subscriptions us
		JOIN groups g ON us.group_id = g.id AND g.deleted_at IS NULL
		WHERE us.user_id = $1
			AND us.group_id = $2
			AND us.status = $3
			AND us.expires_at > NOW()
			AND us.deleted_at IS NULL
		ORDER BY us.starts_at ASC, us.id ASC
		FOR UPDATE OF us
	`, userID, groupID, service.SubscriptionStatusActive)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	now := time.Now()
	candidates := make([]billingSubscriptionCandidate, 0)
	for rows.Next() {
		var c billingSubscriptionCandidate
		if err := rows.Scan(
			&c.id,
			&c.startsAt,
			&c.expiresAt,
			&c.dailyUsage,
			&c.weeklyUsage,
			&c.monthlyUsage,
			&c.customUsage,
			&c.dailyWindowStart,
			&c.weeklyWindowStart,
			&c.monthlyWindowStart,
			&c.customWindowStart,
			&c.dailyLimit,
			&c.weeklyLimit,
			&c.monthlyLimit,
			&c.customLimitHours,
			&c.customLimit,
		); err != nil {
			return 0, err
		}
		normalizeBillingSubscriptionCandidate(&c, now)
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, service.ErrSubscriptionNotFound
	}

	remaining := costUSD
	var firstChargedID int64
	charged := make([]*billingSubscriptionCandidate, 0, len(candidates))
	for i := range candidates {
		available := billingSubscriptionAvailable(&candidates[i])
		if available <= 0 {
			continue
		}
		charge := remaining
		if !math.IsInf(available, 1) && charge > available {
			charge = available
		}
		if charge <= 0 {
			continue
		}
		candidates[i].dailyUsage += charge
		candidates[i].weeklyUsage += charge
		candidates[i].monthlyUsage += charge
		candidates[i].customUsage += charge
		if firstChargedID == 0 {
			firstChargedID = candidates[i].id
		}
		charged = append(charged, &candidates[i])
		remaining -= charge
		if remaining <= 1e-9 {
			break
		}
	}
	if len(charged) == 0 {
		chosen := &candidates[0]
		chosen.dailyUsage += remaining
		chosen.weeklyUsage += remaining
		chosen.monthlyUsage += remaining
		chosen.customUsage += remaining
		firstChargedID = chosen.id
		charged = append(charged, chosen)
		remaining = 0
	}
	if remaining > 1e-9 {
		chosen := charged[len(charged)-1]
		chosen.dailyUsage += remaining
		chosen.weeklyUsage += remaining
		chosen.monthlyUsage += remaining
		chosen.customUsage += remaining
	}

	for _, chosen := range charged {
		res, err := tx.ExecContext(ctx, `
			UPDATE user_subscriptions
			SET daily_usage_usd = $1,
				weekly_usage_usd = $2,
				monthly_usage_usd = $3,
				custom_usage_usd = $4,
				daily_window_start = $5,
				weekly_window_start = $6,
				monthly_window_start = $7,
				custom_window_start = $8,
				updated_at = NOW()
			WHERE id = $9 AND deleted_at IS NULL
		`, chosen.dailyUsage, chosen.weeklyUsage, chosen.monthlyUsage, chosen.customUsage,
			nullTimeValue(chosen.dailyWindowStart), nullTimeValue(chosen.weeklyWindowStart), nullTimeValue(chosen.monthlyWindowStart), nullTimeValue(chosen.customWindowStart), chosen.id)
		if err != nil {
			return 0, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if affected == 0 {
			return 0, service.ErrSubscriptionNotFound
		}
	}
	return firstChargedID, nil
}

func normalizeBillingSubscriptionCandidate(c *billingSubscriptionCandidate, now time.Time) {
	if c == nil {
		return
	}
	dailyOneTime := !c.expiresAt.After(c.startsAt.AddDate(0, 0, 1))
	c.dailyWindowStart = normalizeBillingWindow(c.dailyWindowStart, c.startsAt, 24*time.Hour, now, dailyOneTime, &c.dailyUsage, true)
	c.weeklyWindowStart = normalizeBillingWindow(c.weeklyWindowStart, c.startsAt, 7*24*time.Hour, now, false, &c.weeklyUsage, true)
	c.monthlyWindowStart = normalizeBillingWindow(c.monthlyWindowStart, c.startsAt, 30*24*time.Hour, now, false, &c.monthlyUsage, true)
	customPeriod := time.Duration(0)
	if c.customLimitHours.Valid && c.customLimitHours.Int64 > 0 && c.customLimit.Valid && c.customLimit.Float64 > 0 {
		hours := c.customLimitHours.Int64
		if hours > billingMaxCustomLimitHours {
			hours = billingMaxCustomLimitHours
		}
		customPeriod = time.Duration(hours) * time.Hour
	}
	c.customWindowStart = normalizeBillingWindow(c.customWindowStart, c.startsAt, customPeriod, now, false, &c.customUsage, false)
}

func normalizeBillingWindow(start sql.NullTime, fallback time.Time, period time.Duration, now time.Time, oneTime bool, usage *float64, useInitialLegacyAnchor bool) sql.NullTime {
	if !start.Valid {
		start = sql.NullTime{Time: fallback, Valid: true}
	}
	if useInitialLegacyAnchor && !oneTime {
		year, month, day := fallback.Date()
		legacyAnchor := time.Date(year, month, day, 0, 0, 0, 0, fallback.Location())
		if legacyAnchor.Before(fallback) && start.Time.Equal(legacyAnchor) {
			start.Time = fallback
		}
	}
	if oneTime || period <= 0 || now.Before(start.Time.Add(period)) {
		return start
	}
	elapsed := now.Sub(start.Time)
	periods := int64(elapsed / period)
	if periods < 1 {
		return start
	}
	start.Time = start.Time.Add(time.Duration(periods) * period)
	if usage != nil {
		*usage = 0
	}
	return start
}

func billingSubscriptionAvailable(c *billingSubscriptionCandidate) float64 {
	if c == nil {
		return 0
	}
	available := math.Inf(1)
	applyLimit := func(limit sql.NullFloat64, usage float64) {
		if !limit.Valid || limit.Float64 <= 0 {
			return
		}
		remaining := limit.Float64 - usage
		if remaining < available {
			available = remaining
		}
	}
	applyLimit(c.dailyLimit, c.dailyUsage)
	applyLimit(c.weeklyLimit, c.weeklyUsage)
	applyLimit(c.monthlyLimit, c.monthlyUsage)
	if c.customLimitHours.Valid && c.customLimitHours.Int64 > 0 {
		applyLimit(c.customLimit, c.customUsage)
	}
	if available < 0 {
		return 0
	}
	return available
}

func nullTimeValue(v sql.NullTime) any {
	if !v.Valid {
		return nil
	}
	return v.Time
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

// accountQuotaTotalIncrementExpr 累加总额度已用量。
const accountQuotaTotalIncrementExpr = `COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)`

// accountQuotaDailyIncrementExpr 在库中设置了日上限时累加日用量，周期过期时从本次金额重新计数。
const accountQuotaDailyIncrementExpr = `
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN ` + dailyExpiredExpr + `
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN ` + dailyExpiredExpr + `
					THEN ` + nowUTC + `
					ELSE COALESCE(extra->>'quota_daily_start', ` + nowUTC + `) END
				)
				|| CASE WHEN ` + dailyExpiredExpr + ` AND ` + nextDailyResetAtExpr + ` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', ` + nextDailyResetAtExpr + `)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END`

// accountQuotaWeeklyIncrementExpr 在库中设置了周上限时累加周用量，周期过期时从本次金额重新计数。
const accountQuotaWeeklyIncrementExpr = `
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN ` + weeklyExpiredExpr + `
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN ` + weeklyExpiredExpr + `
					THEN ` + nowUTC + `
					ELSE COALESCE(extra->>'quota_weekly_start', ` + nowUTC + `) END
				)
				|| CASE WHEN ` + weeklyExpiredExpr + ` AND ` + nextWeeklyResetAtExpr + ` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', ` + nextWeeklyResetAtExpr + `)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END`

// accountQuotaExtraIsObjectCond 要求 extra 为 JSON 对象：省略某个维度的语句只在对象上与完整语句等价。
//
// accountQuotaDailyLimitUnsetCond / accountQuotaWeeklyLimitUnsetCond 在库中未设置日/周上限时为真：
// 键不存在、值为 JSON null 或不大于 0 的数字。其他取值一律为假，交给包含该维度的语句处理。
// 三个条件都只使用 ->、jsonb_typeof 与 jsonb 比较，对任何行求值都不会报错。
const (
	accountQuotaExtraIsObjectCond   = `jsonb_typeof(extra) = 'object'`
	accountQuotaDailyLimitUnsetCond = `(extra->'quota_daily_limit' IS NULL
			OR jsonb_typeof(extra->'quota_daily_limit') = 'null'
			OR (jsonb_typeof(extra->'quota_daily_limit') = 'number' AND extra->'quota_daily_limit' <= '0'::jsonb))`
	accountQuotaWeeklyLimitUnsetCond = `(extra->'quota_weekly_limit' IS NULL
			OR jsonb_typeof(extra->'quota_weekly_limit') = 'null'
			OR (jsonb_typeof(extra->'quota_weekly_limit') = 'number' AND extra->'quota_weekly_limit' <= '0'::jsonb))`
)

const accountQuotaIncrementReturning = `
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`

// accountQuotaIncrementSQLs 按 accountQuotaIncrementSQLIndex 存放账号配额累加语句。省略日或周维度的语句
// 在 WHERE 中附加 accountQuotaExtraIsObjectCond 与被省略维度的未设置条件，条件不成立时命中 0 行；
// 同时包含日、周维度的语句不附加这些条件，对任何账号配置都给出完整的累加结果。
var accountQuotaIncrementSQLs = buildAccountQuotaIncrementSQLs()

func accountQuotaIncrementSQLIndex(daily, weekly bool) int {
	index := 0
	if daily {
		index |= 1
	}
	if weekly {
		index |= 2
	}
	return index
}

func buildAccountQuotaIncrementSQLs() [4]string {
	var sqls [4]string
	for _, daily := range []bool{false, true} {
		for _, weekly := range []bool{false, true} {
			set := accountQuotaTotalIncrementExpr
			where := "id = $2 AND deleted_at IS NULL"
			if !daily || !weekly {
				where += "\n\t\t\tAND " + accountQuotaExtraIsObjectCond
			}
			if daily {
				set += accountQuotaDailyIncrementExpr
			} else {
				where += "\n\t\t\tAND " + accountQuotaDailyLimitUnsetCond
			}
			if weekly {
				set += accountQuotaWeeklyIncrementExpr
			} else {
				where += "\n\t\t\tAND " + accountQuotaWeeklyLimitUnsetCond
			}
			sqls[accountQuotaIncrementSQLIndex(daily, weekly)] = "UPDATE accounts SET extra = (\n\t\t\t" + set +
				"\n\t\t), updated_at = NOW()\n\t\tWHERE " + where + accountQuotaIncrementReturning
		}
	}
	return sqls
}

// incrementUsageBillingAccountQuota 累加账号配额并返回累加后的状态。hasDaily/hasWeekly 是调用方所见的
// 账号日/周上限设置，只决定先执行哪条语句；该语句的附加条件不成立而命中 0 行时，再执行同时包含日、周维度的语句。
func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64, hasDaily, hasWeekly bool) (*service.AccountQuotaState, error) {
	index := accountQuotaIncrementSQLIndex(hasDaily, hasWeekly)
	fullIndex := accountQuotaIncrementSQLIndex(true, true)
	state, err := execAccountQuotaIncrement(ctx, tx, accountQuotaIncrementSQLs[index], accountID, amount)
	if err != nil {
		return nil, err
	}
	if state == nil && index != fullIndex {
		state, err = execAccountQuotaIncrement(ctx, tx, accountQuotaIncrementSQLs[fullIndex], accountID, amount)
		if err != nil {
			return nil, err
		}
	}
	if state == nil {
		return nil, service.ErrAccountNotFound
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return state, nil
}

// execAccountQuotaIncrement 执行一条账号配额累加语句，未命中任何行时返回 nil 状态。
func execAccountQuotaIncrement(ctx context.Context, tx *sql.Tx, query string, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	rows, err := tx.QueryContext(ctx, query, amount, accountID)
	if err != nil {
		return nil, err
	}

	var state *service.AccountQuotaState
	if rows.Next() {
		state = &service.AccountQuotaState{}
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return state, nil
}
