package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

// dailyMidnightResetRepo 记录 ResetDailyUsage 收到的新窗口起点。
type dailyMidnightResetRepo struct {
	userSubRepoNoop

	resetCalled    bool
	newWindowStart time.Time
}

func (r *dailyMidnightResetRepo) ResetDailyUsage(_ context.Context, _ int64, _ *time.Time, newWindowStart time.Time) error {
	r.resetCalled = true
	r.newWindowStart = newWindowStart
	return nil
}

// midnightTestBase 返回一个固定日期在配置时区的 0 点，测试用时刻均从它推导，
// 保证断言在任意本地时区下都与生产逻辑同构。
func midnightTestBase() time.Time {
	return timezone.StartOfDay(time.Date(2026, 8, 6, 12, 0, 0, 0, timezone.Location()))
}

func newMidnightTestSub(dailyWindowStart time.Time, base time.Time) *UserSubscription {
	start := dailyWindowStart
	return &UserSubscription{
		ID:               1,
		UserID:           10,
		GroupID:          20,
		StartsAt:         base.AddDate(0, 0, -3),
		ExpiresAt:        base.AddDate(0, 0, 30),
		DailyUsageUSD:    43.34,
		DailyWindowStart: &start,
	}
}

// 手动重置留下的非 0 点锚点，只有经过完整 24 小时后才重置，
// 新窗口起点继续按锚点滚动，而不是被拉回自然日 0 点。
func TestCheckAndResetWindows_DailyResetsAfterRollingWindow(t *testing.T) {
	base := midnightTestBase()
	manualResetAt := base.Add(16*time.Hour + 49*time.Minute) // 昨日 16:49 手动重置
	now := manualResetAt.Add(24*time.Hour + 5*time.Minute)   // 下一窗口已到期 5 分钟

	repo := &dailyMidnightResetRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	sub := newMidnightTestSub(manualResetAt, base)

	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))

	require.True(t, repo.resetCalled, "经过完整 24 小时后日窗口必须重置")
	require.Equal(t, manualResetAt.Add(24*time.Hour), repo.newWindowStart, "新窗口起点应按原锚点滚动")
	require.Zero(t, sub.DailyUsageUSD)
	require.Equal(t, manualResetAt.Add(24*time.Hour), *sub.DailyWindowStart)
}

// 未经过完整 24 小时不得重置，即使已经跨过自然日边界。
func TestCheckAndResetWindows_DailyNoResetBeforeRollingWindowEnds(t *testing.T) {
	base := midnightTestBase()
	manualResetAt := base.Add(16*time.Hour + 49*time.Minute)
	now := manualResetAt.Add(23*time.Hour + 59*time.Minute)

	repo := &dailyMidnightResetRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	sub := newMidnightTestSub(manualResetAt, base)

	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))

	require.False(t, repo.resetCalled, "24 小时未到不应重置日窗口")
	require.Equal(t, 43.34, sub.DailyUsageUSD)
}

// 历史滚动锚点保持权威，下一窗口起点仍由该锚点推进。
func TestCheckAndResetWindows_LegacyRollingAnchorRemainsAuthoritative(t *testing.T) {
	base := midnightTestBase()
	staleAnchor := base.AddDate(0, 0, -3).Add(17*time.Hour + 18*time.Minute)
	now := staleAnchor.Add(24 * time.Hour)

	repo := &dailyMidnightResetRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	sub := newMidnightTestSub(staleAnchor, base)

	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))

	require.True(t, repo.resetCalled)
	require.Equal(t, staleAnchor.Add(24*time.Hour), repo.newWindowStart, "历史滚动锚点不应被改成自然日 0 点")
}

// 手动重置写入的锚点决定下一次刷新：自然日边界不影响滚动节奏。
func TestNeedsDailyReset_RollingScheduleSurvivesManualReset(t *testing.T) {
	base := midnightTestBase()
	manualResetAt := base.Add(16*time.Hour + 49*time.Minute)
	sub := newMidnightTestSub(manualResetAt, base)

	require.False(t, sub.NeedsDailyResetAt(manualResetAt.Add(23*time.Hour+54*time.Minute)), "24 小时未到不应重置")
	require.True(t, sub.NeedsDailyResetAt(manualResetAt.Add(24*time.Hour)), "到达锚点+24h 后应重置")
}

// 多日订阅的日窗口展示下次刷新时间为当前滚动锚点+24h。
func TestDailyResetTime_NextRollingBoundaryForMultiDaySubscription(t *testing.T) {
	base := midnightTestBase()

	// 0 点锚点 → 24 小时后
	sub := newMidnightTestSub(base, base)
	resetAt := sub.DailyResetTime()
	require.NotNil(t, resetAt)
	require.Equal(t, base.Add(24*time.Hour), *resetAt)

	// 非 0 点滚动锚点 → 锚点+24h
	rollingAnchor := base.Add(16*time.Hour + 49*time.Minute)
	rolling := newMidnightTestSub(rollingAnchor, base)
	resetAt = rolling.DailyResetTime()
	require.NotNil(t, resetAt)
	require.Equal(t, rollingAnchor.Add(24*time.Hour), *resetAt)
}

// 后台尚未收到请求推进窗口时，列表归一化也按滚动边界清零日用量。
func TestNormalizeExpiredWindows_DailyUsageClearsAfterRollingBoundary(t *testing.T) {
	base := midnightTestBase()
	manualResetAt := base.Add(16*time.Hour + 49*time.Minute)
	now := manualResetAt.Add(24*time.Hour + time.Minute)

	subs := []UserSubscription{*newMidnightTestSub(manualResetAt, base)}
	normalizeExpiredWindowsAt(subs, now)

	require.Zero(t, subs[0].DailyUsageUSD, "滚动窗口到期后展示的日用量应清零")
	require.NotNil(t, subs[0].DailyWindowStart)
	require.Equal(t, manualResetAt.Add(24*time.Hour), *subs[0].DailyWindowStart)
}

// 日卡（一次性日额度）不受滚动窗口语义影响：不会自动重置。
func TestCheckAndResetWindows_OneTimeDailyCardStillExemptFromRollingReset(t *testing.T) {
	base := midnightTestBase()
	startsAt := base.Add(17 * time.Hour)
	anchor := base
	now := base.AddDate(0, 0, 1).Add(2 * time.Hour)

	repo := &dailyMidnightResetRepo{}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	sub := &UserSubscription{
		ID:               1,
		UserID:           10,
		GroupID:          20,
		StartsAt:         startsAt,
		ExpiresAt:        startsAt.AddDate(0, 0, 1),
		DailyUsageUSD:    10,
		DailyWindowStart: &anchor,
	}

	require.NoError(t, svc.CheckAndResetWindows(context.Background(), sub))

	require.False(t, repo.resetCalled, "日卡为一次性配额，不应自动重置")
	require.Equal(t, 10.0, sub.DailyUsageUSD)
}
