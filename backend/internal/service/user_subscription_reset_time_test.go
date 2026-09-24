package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 周窗口初始化在开通日零点（legacy anchor）时，automaticWindowStartAt 会把实际
// 推进锚点修正为 StartsAt。展示层的 WeeklyResetTime 必须应用同一修正，
// 否则用户看到的重置时间会比实际重置时间早（差值 = StartsAt 的时分秒）。
func TestWeeklyResetTime_LegacyMidnightAnchor_UsesStartsAt(t *testing.T) {
	startsAt := time.Date(2026, 7, 31, 13, 37, 6, 0, time.FixedZone("UTC+8", 8*3600))
	windowStart := startOfDay(startsAt)

	sub := &UserSubscription{
		StartsAt:          startsAt,
		ExpiresAt:         startsAt.AddDate(0, 0, 30),
		WeeklyWindowStart: ptrTime(windowStart),
	}

	got := sub.WeeklyResetTime()
	require.NotNil(t, got)
	assert.True(t, startsAt.Add(7*24*time.Hour).Equal(*got),
		"legacy 午夜锚点应按 StartsAt+7d 计算重置时间，而不是窗口起点+7d")
}

// 非 legacy 锚点（手动重置或已推进过的窗口）保持权威，不做修正。
func TestWeeklyResetTime_RegularAnchor_UsesWindowStart(t *testing.T) {
	startsAt := time.Date(2026, 7, 31, 13, 37, 6, 0, time.FixedZone("UTC+8", 8*3600))
	windowStart := startsAt.Add(7 * 24 * time.Hour) // 已推进过一轮，非开通日零点

	sub := &UserSubscription{
		StartsAt:          startsAt,
		ExpiresAt:         startsAt.AddDate(0, 0, 30),
		WeeklyWindowStart: ptrTime(windowStart),
	}

	got := sub.WeeklyResetTime()
	require.NotNil(t, got)
	assert.True(t, windowStart.Add(7*24*time.Hour).Equal(*got),
		"非 legacy 锚点应按窗口起点+7d 计算重置时间")
}

// 展示与执行一致性：WeeklyResetTime 前一秒窗口不应推进，到点后应推进。
func TestWeeklyResetTime_MatchesAutomaticWindowStart(t *testing.T) {
	startsAt := time.Date(2026, 7, 31, 13, 37, 6, 0, time.FixedZone("UTC+8", 8*3600))
	windowStart := startOfDay(startsAt)

	sub := &UserSubscription{
		StartsAt:          startsAt,
		ExpiresAt:         startsAt.AddDate(0, 0, 30),
		WeeklyWindowStart: ptrTime(windowStart),
	}

	resetAt := *sub.WeeklyResetTime()

	_, ok := sub.automaticWindowStartAt(sub.WeeklyWindowStart, 7*24*time.Hour, resetAt.Add(-time.Second))
	assert.False(t, ok, "展示的重置时间之前窗口不应可推进")

	newStart, ok := sub.automaticWindowStartAt(sub.WeeklyWindowStart, 7*24*time.Hour, resetAt)
	assert.True(t, ok, "到达展示的重置时间后窗口应可推进")
	assert.True(t, resetAt.Equal(newStart), "推进后的新窗口起点应等于展示的重置时间")
}

// 月窗口与周窗口同属期限对齐滚动窗口，应用同一 legacy 锚点修正。
func TestMonthlyResetTime_LegacyMidnightAnchor_UsesStartsAt(t *testing.T) {
	startsAt := time.Date(2026, 7, 31, 13, 37, 6, 0, time.FixedZone("UTC+8", 8*3600))
	windowStart := startOfDay(startsAt)

	sub := &UserSubscription{
		StartsAt:           startsAt,
		ExpiresAt:          startsAt.AddDate(0, 0, 60),
		MonthlyWindowStart: ptrTime(windowStart),
	}

	monthly := sub.MonthlyResetTime()
	require.NotNil(t, monthly)
	assert.True(t, startsAt.Add(30*24*time.Hour).Equal(*monthly),
		"legacy 午夜锚点应按 StartsAt+30d 计算重置时间，而不是窗口起点+30d")
}

// 日窗口展示与执行都使用当前持久化的滚动窗口起点。
func TestDailyResetTime_UsesRollingWindowStart(t *testing.T) {
	startsAt := time.Date(2026, 7, 31, 13, 37, 6, 0, time.FixedZone("UTC+8", 8*3600))
	windowStart := startsAt

	sub := &UserSubscription{
		StartsAt:         startsAt,
		ExpiresAt:        startsAt.AddDate(0, 0, 30),
		DailyWindowStart: ptrTime(windowStart),
	}

	got := sub.DailyResetTime()
	require.NotNil(t, got)
	assert.True(t, got.Equal(windowStart.Add(24*time.Hour)),
		"日窗口应按持久化窗口起点滚动 24 小时")
}
