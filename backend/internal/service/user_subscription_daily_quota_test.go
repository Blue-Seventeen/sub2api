package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type dailyResetTrackingUserSubRepo struct {
	userSubRepoNoop

	resetDailyCalled bool
}

func (r *dailyResetTrackingUserSubRepo) ResetDailyUsage(context.Context, int64, *time.Time, time.Time) error {
	r.resetDailyCalled = true
	return nil
}

type rollingWindowResetUserSubRepo struct {
	userSubRepoNoop

	dailyWindowStart     *time.Time
	weeklyWindowStart    *time.Time
	monthlyWindowStart   *time.Time
	customWindowStart    *time.Time
	dailyPreviousUsage   float64
	weeklyPreviousUsage  float64
	monthlyPreviousUsage float64
	customPreviousUsage  float64
	dailyExpectedAt      time.Time
	weeklyExpectedAt     time.Time
	monthlyExpectedAt    time.Time
	customExpectedAt     time.Time
}

func (r *rollingWindowResetUserSubRepo) ResetDailyUsage(_ context.Context, _ int64, _ *time.Time, windowStart time.Time) error {
	r.dailyWindowStart = &windowStart
	return nil
}

func (r *rollingWindowResetUserSubRepo) ResetWeeklyUsage(_ context.Context, _ int64, _ *time.Time, windowStart time.Time) error {
	r.weeklyWindowStart = &windowStart
	return nil
}

func (r *rollingWindowResetUserSubRepo) ResetMonthlyUsage(_ context.Context, _ int64, _ *time.Time, windowStart time.Time) error {
	r.monthlyWindowStart = &windowStart
	return nil
}

func (r *rollingWindowResetUserSubRepo) ResetCustomUsage(_ context.Context, _ int64, windowStart time.Time) error {
	r.customWindowStart = &windowStart
	return nil
}

func (r *rollingWindowResetUserSubRepo) RollDailyUsageWindow(_ context.Context, _ int64, _ time.Time, windowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	r.dailyWindowStart = &windowStart
	r.dailyPreviousUsage = previousUsage
	r.dailyExpectedAt = expectedUpdatedAt
	return true, nil
}

func (r *rollingWindowResetUserSubRepo) RollWeeklyUsageWindow(_ context.Context, _ int64, _ time.Time, windowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	r.weeklyWindowStart = &windowStart
	r.weeklyPreviousUsage = previousUsage
	r.weeklyExpectedAt = expectedUpdatedAt
	return true, nil
}

func (r *rollingWindowResetUserSubRepo) RollMonthlyUsageWindow(_ context.Context, _ int64, _ time.Time, windowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	r.monthlyWindowStart = &windowStart
	r.monthlyPreviousUsage = previousUsage
	r.monthlyExpectedAt = expectedUpdatedAt
	return true, nil
}

func (r *rollingWindowResetUserSubRepo) RollCustomUsageWindow(_ context.Context, _ int64, _ time.Time, windowStart time.Time, previousUsage float64, expectedUpdatedAt time.Time) (bool, error) {
	r.customWindowStart = &windowStart
	r.customPreviousUsage = previousUsage
	r.customExpectedAt = expectedUpdatedAt
	return true, nil
}

type activateWindowUserSubRepo struct {
	userSubRepoNoop

	windowStart *time.Time
}

func (r *activateWindowUserSubRepo) ActivateWindows(_ context.Context, _ int64, windowStart time.Time) error {
	r.windowStart = &windowStart
	return nil
}

func TestAssignOrExtendSubscription_ExpiredDailyCardStartsNewOneTimeQuota(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Now().AddDate(0, 0, -3)
	oldWindowStart := oldStart
	subRepo.seed(&UserSubscription{
		ID:                 100,
		UserID:             200,
		GroupID:            1,
		StartsAt:           oldStart,
		ExpiresAt:          oldStart.AddDate(0, 0, 1),
		Status:             SubscriptionStatusExpired,
		DailyWindowStart:   &oldWindowStart,
		WeeklyWindowStart:  &oldWindowStart,
		MonthlyWindowStart: &oldWindowStart,
		CustomWindowStart:  &oldWindowStart,
		DailyUsageUSD:      10,
		WeeklyUsageUSD:     20,
		MonthlyUsageUSD:    30,
		CustomUsageUSD:     40,
		Notes:              "old",
	})
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	renewed, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       200,
		GroupID:      1,
		ValidityDays: 1,
		Notes:        "new",
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.True(t, renewed.HasOneTimeDailyQuota(), "过期后重新购买 1 日卡仍应被识别为一次性日额度")
	require.Equal(t, SubscriptionStatusActive, renewed.Status)
	require.True(t, renewed.StartsAt.After(oldStart), "重新购买过期订阅时应重置当前周期 StartsAt")
	require.False(t, renewed.ExpiresAt.After(renewed.StartsAt.AddDate(0, 0, 1)))
	require.NotNil(t, renewed.DailyWindowStart)
	require.Equal(t, renewed.StartsAt, *renewed.DailyWindowStart)
	require.NotNil(t, renewed.WeeklyWindowStart)
	require.Equal(t, renewed.StartsAt, *renewed.WeeklyWindowStart)
	require.NotNil(t, renewed.MonthlyWindowStart)
	require.Equal(t, renewed.StartsAt, *renewed.MonthlyWindowStart)
	require.NotNil(t, renewed.CustomWindowStart)
	require.Equal(t, renewed.StartsAt, *renewed.CustomWindowStart)
	require.Equal(t, 0.0, renewed.DailyUsageUSD)
	require.Equal(t, 0.0, renewed.WeeklyUsageUSD)
	require.Equal(t, 0.0, renewed.MonthlyUsageUSD)
	require.Equal(t, 0.0, renewed.CustomUsageUSD)
	require.Equal(t, "old\nnew", renewed.Notes)
}

func TestAssignOrExtendSubscription_ExpiredSubscriptionAppendsMatchingNotes(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Now().AddDate(0, 0, -3)
	subRepo.seed(&UserSubscription{
		ID:        101,
		UserID:    201,
		GroupID:   1,
		StartsAt:  oldStart,
		ExpiresAt: oldStart.AddDate(0, 0, 1),
		Status:    SubscriptionStatusExpired,
		Notes:     "same",
	})
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	renewed, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       201,
		GroupID:      1,
		ValidityDays: 1,
		Notes:        "same",
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, "same\nsame", renewed.Notes)
}

func TestUserSubscriptionNeedsDailyReset_DailyCardKeepsOneTimeQuota(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := start
	sub := &UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.Add(24 * time.Hour),
		DailyWindowStart: &dailyWindowStart,
		DailyUsageUSD:    10,
	}

	require.True(t, sub.HasOneTimeDailyQuota())
	require.False(t, sub.NeedsDailyResetAt(dailyWindowStart.Add(25*time.Hour)), "日卡应作为一次性配额，跨 0 点后不再刷新日额度")
}

func TestUserSubscriptionNeedsDailyReset_MultiDaySubscriptionStillRefreshes(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := start
	sub := &UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.AddDate(0, 0, 2),
		DailyWindowStart: &dailyWindowStart,
	}

	require.False(t, sub.HasOneTimeDailyQuota())
	require.False(t, sub.NeedsDailyResetAt(dailyWindowStart.Add(24*time.Hour-time.Second)))
	require.True(t, sub.NeedsDailyResetAt(dailyWindowStart.Add(24*time.Hour)), "多日订阅仍应按 24 小时日窗口刷新")
}

func TestUserSubscriptionNeedsDailyReset_UsesRollingRedemptionWindow(t *testing.T) {
	start := time.Date(2026, 5, 18, 16, 0, 0, 0, time.UTC)
	dailyWindowStart := start
	sub := &UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.Add(48 * time.Hour),
		DailyWindowStart: &dailyWindowStart,
	}

	require.False(t, sub.NeedsDailyResetAt(time.Date(2026, 5, 19, 15, 59, 59, 0, time.UTC)))
	require.True(t, sub.NeedsDailyResetAt(time.Date(2026, 5, 19, 16, 0, 0, 0, time.UTC)))
	require.Equal(t, time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC), sub.ExpiresAt)
}

func TestUserSubscriptionDailyResetTime_DailyCardReturnsExpiry(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := start
	expiresAt := start.Add(24 * time.Hour)
	sub := &UserSubscription{
		StartsAt:         start,
		ExpiresAt:        expiresAt,
		DailyWindowStart: &dailyWindowStart,
	}

	resetAt := sub.DailyResetTime()
	require.NotNil(t, resetAt)
	require.Equal(t, expiresAt, *resetAt, "日卡展示的日额度结束时间应为订阅过期时间")
}

func TestUserSubscriptionCustomLimitUsesConfiguredWindow(t *testing.T) {
	start := time.Date(2026, 5, 18, 16, 0, 0, 0, time.UTC)
	customLimit := 100.0
	customWindowStart := start
	group := &Group{
		SubscriptionType: SubscriptionTypeSubscription,
		CustomLimitHours: 72,
		CustomLimitUSD:   &customLimit,
	}
	sub := &UserSubscription{
		Status:            SubscriptionStatusActive,
		StartsAt:          start,
		ExpiresAt:         start.Add(7 * 24 * time.Hour),
		CustomWindowStart: &customWindowStart,
		CustomUsageUSD:    95,
	}

	require.False(t, sub.NeedsCustomResetAt(group, start.Add(72*time.Hour-time.Second)))
	require.True(t, sub.NeedsCustomResetAt(group, start.Add(72*time.Hour)))
	require.True(t, sub.CheckCustomLimit(group, 5))
	require.False(t, sub.CheckCustomLimit(group, 5.01))
}

func TestCheckAndActivateWindow_UsesSubscriptionStartsAtAsAnchor(t *testing.T) {
	start := time.Date(2026, 5, 18, 16, 0, 0, 0, time.UTC)
	repo := &activateWindowUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	sub := &UserSubscription{ID: 1, StartsAt: start}

	err := svc.CheckAndActivateWindow(context.Background(), sub)

	require.NoError(t, err)
	require.NotNil(t, repo.windowStart)
	require.Equal(t, start, *repo.windowStart)
	require.NotNil(t, sub.DailyWindowStart)
	require.Equal(t, start, *sub.DailyWindowStart)
	require.NotNil(t, sub.WeeklyWindowStart)
	require.Equal(t, start, *sub.WeeklyWindowStart)
	require.NotNil(t, sub.MonthlyWindowStart)
	require.Equal(t, start, *sub.MonthlyWindowStart)
}

func TestCheckAndActivateWindow_FallsBackToNowWhenStartsAtMissing(t *testing.T) {
	repo := &activateWindowUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	sub := &UserSubscription{ID: 1}
	before := time.Now()

	err := svc.CheckAndActivateWindow(context.Background(), sub)

	after := time.Now()
	require.NoError(t, err)
	require.NotNil(t, repo.windowStart)
	require.False(t, repo.windowStart.Before(before))
	require.False(t, repo.windowStart.After(after))
}

func TestCheckAndResetWindows_DailyCardDoesNotResetDailyUsage(t *testing.T) {
	now := time.Now()
	startsAt := now.Add(-23 * time.Hour)
	dailyWindowStart := now.Add(-25 * time.Hour)
	repo := &dailyResetTrackingUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	sub := &UserSubscription{
		ID:               1,
		UserID:           10,
		GroupID:          20,
		StartsAt:         startsAt,
		ExpiresAt:        startsAt.Add(24 * time.Hour),
		DailyUsageUSD:    10,
		DailyWindowStart: &dailyWindowStart,
	}

	err := svc.CheckAndResetWindows(context.Background(), sub)

	require.NoError(t, err)
	require.False(t, repo.resetDailyCalled, "日卡作为一次性配额，过了 24 小时日窗口也不应重置 daily usage")
	require.Equal(t, 10.0, sub.DailyUsageUSD)
}

func TestCheckAndResetWindows_MultiDaySubscriptionStillResetsDailyUsage(t *testing.T) {
	now := time.Now()
	startsAt := now.Add(-48 * time.Hour)
	dailyWindowStart := now.Add(-25 * time.Hour)
	repo := &dailyResetTrackingUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	sub := &UserSubscription{
		ID:               1,
		UserID:           10,
		GroupID:          20,
		StartsAt:         startsAt,
		ExpiresAt:        startsAt.AddDate(0, 0, 2),
		DailyUsageUSD:    10,
		DailyWindowStart: &dailyWindowStart,
	}

	err := svc.CheckAndResetWindows(context.Background(), sub)

	require.NoError(t, err)
	require.True(t, repo.resetDailyCalled, "多日订阅仍应重置过期 daily window")
	require.Equal(t, 0.0, sub.DailyUsageUSD)
}

func TestCheckAndResetWindows_RollsExpiredWindowsToCurrentPeriod(t *testing.T) {
	now := time.Now()
	startsAt := now.Add(-90 * 24 * time.Hour)
	dailyStart := now.Add(-55 * time.Hour)
	weeklyStart := now.Add(-16 * 24 * time.Hour)
	monthlyStart := now.Add(-65 * 24 * time.Hour)
	repo := &rollingWindowResetUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	sub := &UserSubscription{
		ID:                 1,
		UserID:             10,
		GroupID:            20,
		StartsAt:           startsAt,
		ExpiresAt:          now.Add(24 * time.Hour),
		UpdatedAt:          now.Add(-time.Minute),
		DailyUsageUSD:      10,
		WeeklyUsageUSD:     20,
		MonthlyUsageUSD:    30,
		DailyWindowStart:   &dailyStart,
		WeeklyWindowStart:  &weeklyStart,
		MonthlyWindowStart: &monthlyStart,
	}

	err := svc.CheckAndResetWindows(context.Background(), sub)

	require.NoError(t, err)
	require.NotNil(t, repo.dailyWindowStart)
	require.WithinDuration(t, dailyStart.Add(48*time.Hour), *repo.dailyWindowStart, time.Second)
	require.Equal(t, 10.0, repo.dailyPreviousUsage)
	require.Equal(t, sub.UpdatedAt, repo.dailyExpectedAt)
	require.NotNil(t, repo.weeklyWindowStart)
	require.WithinDuration(t, weeklyStart.Add(14*24*time.Hour), *repo.weeklyWindowStart, time.Second)
	require.Equal(t, 20.0, repo.weeklyPreviousUsage)
	require.Equal(t, sub.UpdatedAt, repo.weeklyExpectedAt)
	require.NotNil(t, repo.monthlyWindowStart)
	require.WithinDuration(t, monthlyStart.Add(60*24*time.Hour), *repo.monthlyWindowStart, time.Second)
	require.Equal(t, 30.0, repo.monthlyPreviousUsage)
	require.Equal(t, sub.UpdatedAt, repo.monthlyExpectedAt)
	require.Equal(t, 0.0, sub.DailyUsageUSD)
	require.Equal(t, 0.0, sub.WeeklyUsageUSD)
	require.Equal(t, 0.0, sub.MonthlyUsageUSD)
}

func TestCheckAndResetWindows_CustomWindowRollsToCurrentPeriod(t *testing.T) {
	now := time.Now()
	customLimit := 100.0
	customStart := now.Add(-80 * time.Hour)
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		CustomLimitHours: 72,
		CustomLimitUSD:   &customLimit,
	}
	repo := &rollingWindowResetUserSubRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	sub := &UserSubscription{
		ID:                1,
		UserID:            10,
		GroupID:           20,
		Status:            SubscriptionStatusActive,
		StartsAt:          now.Add(-10 * 24 * time.Hour),
		ExpiresAt:         now.Add(24 * time.Hour),
		UpdatedAt:         now.Add(-time.Minute),
		Group:             group,
		CustomWindowStart: &customStart,
		CustomUsageUSD:    99,
	}

	err := svc.CheckAndResetWindows(context.Background(), sub)

	require.NoError(t, err)
	require.NotNil(t, repo.customWindowStart)
	require.WithinDuration(t, customStart.Add(72*time.Hour), *repo.customWindowStart, time.Second)
	require.Equal(t, 99.0, repo.customPreviousUsage)
	require.Equal(t, sub.UpdatedAt, repo.customExpectedAt)
	require.Equal(t, 0.0, sub.CustomUsageUSD)
	require.NotNil(t, sub.CustomWindowStart)
	require.WithinDuration(t, customStart.Add(72*time.Hour), *sub.CustomWindowStart, time.Second)
}

func TestValidateAndCheckLimits_DailyCardDoesNotAllowSecondQuotaAfterMidnight(t *testing.T) {
	start := time.Now().Add(-23 * time.Hour)
	dailyWindowStart := time.Now().Add(-25 * time.Hour)
	dailyLimit := 10.0
	sub := &UserSubscription{
		Status:           SubscriptionStatusActive,
		StartsAt:         start,
		ExpiresAt:        start.Add(24 * time.Hour),
		DailyWindowStart: &dailyWindowStart,
		DailyUsageUSD:    dailyLimit + 0.01,
	}
	group := &Group{
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &dailyLimit,
	}
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)

	needsMaintenance, err := svc.ValidateAndCheckLimits(sub, group)

	require.False(t, needsMaintenance, "日卡跨过日窗口后不应触发 daily reset 维护")
	require.True(t, errors.Is(err, ErrDailyLimitExceeded))
	require.Equal(t, dailyLimit+0.01, sub.DailyUsageUSD, "热路径不应清零日卡已用额度")
}
