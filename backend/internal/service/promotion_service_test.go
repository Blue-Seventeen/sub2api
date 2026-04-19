package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseSettlementClock(t *testing.T) {
	hour, minute, err := parseSettlementClock("18:30")
	require.NoError(t, err)
	require.Equal(t, 18, hour)
	require.Equal(t, 30, minute)

	_, _, err = parseSettlementClock("25:00")
	require.ErrorIs(t, err, ErrPromotionInvalidSettlementTime)
}

func TestSettlementBoundaryDate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	beforeCutoff := time.Date(2026, 4, 20, 9, 0, 0, 0, loc)
	afterCutoff := time.Date(2026, 4, 20, 19, 0, 0, 0, loc)

	beforeBoundary := settlementBoundaryDate(beforeCutoff, "18:00", loc)
	afterBoundary := settlementBoundaryDate(afterCutoff, "18:00", loc)

	require.Equal(t, time.Date(2026, 4, 19, 0, 0, 0, 0, loc), beforeBoundary)
	require.Equal(t, time.Date(2026, 4, 20, 0, 0, 0, 0, loc), afterBoundary)
}

func TestRenderPromotionScriptPreview(t *testing.T) {
	rendered := renderPromotionScriptPreview(
		"邀请码 {{INVITE_CODE}} 链接 {{REF_LINK}}",
		map[string]string{
			"INVITE_CODE": "ABC123",
			"REF_LINK":    "https://example.com/register?ref=ABC123",
		},
	)
	require.Equal(t, "邀请码 ABC123 链接 https://example.com/register?ref=ABC123", rendered)
}

func TestRenderPromotionRuleTemplates(t *testing.T) {
	overview := &PromotionOverview{
		ActivationThresholdAmount: 5,
		ActivationBonusAmount:     1,
		CurrentDirectRate:         2,
		CurrentIndirectRate:       0.2,
		CurrentTotalRate:          2.2,
	}
	levels := []PromotionLevelConfig{
		{LevelNo: 1, LevelName: "初出茅庐", RequiredActivatedInvites: 0, DirectRate: 1, IndirectRate: 0.1, Enabled: true},
		{LevelNo: 2, LevelName: "推广达人", RequiredActivatedInvites: 5, DirectRate: 2, IndirectRate: 0.2, Enabled: true},
	}
	rendered := renderPromotionRuleTemplates(PromotionSettings{DailySettlementTime: "00:00"}, overview, levels)
	require.Contains(t, rendered.Activation, "$1")
	require.Contains(t, rendered.Direct, "当前等级：2%")
	require.Contains(t, rendered.Indirect, "当前等级：0.2%")
	require.Contains(t, rendered.LevelSummary, "Lv1 初出茅庐：1% + 0.1% = 1.1%")
}

func TestNormalizeTeamFilterDefaults(t *testing.T) {
	filter := normalizeTeamFilter(PromotionTeamFilter{})
	require.Equal(t, 1, filter.Page)
	require.Equal(t, 10, filter.PageSize)
	require.Equal(t, "today_contribution", filter.SortBy)
	require.Equal(t, "desc", filter.SortOrder)
}
