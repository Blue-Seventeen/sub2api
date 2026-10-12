package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type usageIPLegacyRepo struct{ UsageLogRepository }

func TestUsageIPUnsupportedChartsFailClosed(t *testing.T) {
	repo := &usageIPLegacyRepo{}
	usage := NewUsageService(repo, nil, nil, nil)
	dashboard := NewDashboardService(repo, nil, nil, nil)
	f := usagestats.UsageLogFilters{IPAddresses: []string{"192.0.2.1"}}
	now := time.Now()
	_, err := usage.GetUsageTrendWithFilters(context.Background(), now, now, "day", f)
	require.Error(t, err)
	_, err = usage.GetModelStatsWithFiltersBySource(context.Background(), now, now, f, "requested")
	require.Error(t, err)
	_, err = usage.GetGroupStatsWithFilters(context.Background(), now, now, f)
	require.Error(t, err)
	_, err = dashboard.GetUsageTrendWithUsageFilters(context.Background(), now, now, "day", f)
	require.Error(t, err)
	_, err = dashboard.GetModelStatsWithUsageFiltersBySource(context.Background(), now, now, f, "requested")
	require.Error(t, err)
	_, err = dashboard.GetGroupStatsWithUsageFilters(context.Background(), now, now, f)
	require.Error(t, err)
}
func (r *usageIPLegacyRepo) GetUsageTrendWithFilters(context.Context, time.Time, time.Time, string, int64, int64, int64, int64, string, *int16, *bool, *int8) ([]usagestats.TrendDataPoint, error) {
	return []usagestats.TrendDataPoint{}, nil
}
func (r *usageIPLegacyRepo) GetModelStatsWithFilters(context.Context, time.Time, time.Time, int64, int64, int64, int64, *int16, *bool, *int8) ([]usagestats.ModelStat, error) {
	return []usagestats.ModelStat{}, nil
}
func (r *usageIPLegacyRepo) GetGroupStatsWithFilters(context.Context, time.Time, time.Time, int64, int64, int64, int64, *int16, *bool, *int8) ([]usagestats.GroupStat, error) {
	return []usagestats.GroupStat{}, nil
}
func TestUsageIPOptionsUnavailable(t *testing.T) {
	usage := NewUsageService(&usageIPLegacyRepo{}, nil, nil, nil)
	_, err := usage.GetIPOptionsWithFilters(context.Background(), usagestats.UsageLogFilters{UserID: 42}, "", 0, 50)
	require.Error(t, err)
}
