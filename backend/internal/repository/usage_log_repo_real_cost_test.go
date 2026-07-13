package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryGetUserBreakdownStatsReturnsRealActualCost(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("(?s)SELECT.*real_actual_cost.*account_cost.*FROM usage_logs ul").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "email", "requests", "input_tokens", "output_tokens",
			"cache_tokens", "total_tokens", "cost", "actual_cost", "real_actual_cost", "account_cost",
		}).AddRow(int64(7), "alice@example.com", int64(3), int64(10), int64(20), int64(2), int64(32), 1.5, 1.2, 0.8, 1.1))

	stats, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{}, 50)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, 0.8, stats[0].RealActualCost)
	require.Equal(t, 1.1, stats[0].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetBatchUserUsageStatsReturnsRealActualCost(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(30 * 24 * time.Hour)
	today := timezone.Today()

	mock.ExpectQuery("(?s)SELECT\\s+ul.user_id,.*real_today_cost.*FROM usage_logs ul").
		WithArgs(sqlmock.AnyArg(), start, end, today).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "platform", "total_cost", "today_cost", "real_total_cost", "real_today_cost",
		}).
			AddRow(int64(1), "openai", 10.0, 4.0, 2.0, 1.0).
			AddRow(int64(1), "anthropic", 5.0, 2.0, 1.5, 0.5).
			AddRow(int64(2), "openai", 3.0, 1.0, 0.9, 0.2))

	stats, err := repo.GetBatchUserUsageStats(context.Background(), []int64{1, 2}, start, end)
	require.NoError(t, err)
	require.InDelta(t, 15.0, stats[1].TotalActualCost, 0.0001)
	require.InDelta(t, 3.5, stats[1].RealTotalActualCost, 0.0001)
	require.Len(t, stats[1].ByPlatform, 2)
	require.InDelta(t, 1.0, stats[1].ByPlatform[0].RealTodayActualCost, 0.0001)
	require.InDelta(t, 0.5, stats[1].ByPlatform[1].RealTodayActualCost, 0.0001)
	require.InDelta(t, 0.9, stats[2].RealTotalActualCost, 0.0001)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetBatchAPIKeyUsageStatsReturnsRealActualCost(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(30 * 24 * time.Hour)
	today := timezone.Today()

	mock.ExpectQuery("(?s)SELECT\\s+api_key_id,.*real_today_cost.*FROM usage_logs").
		WithArgs(sqlmock.AnyArg(), start, end, today).
		WillReturnRows(sqlmock.NewRows([]string{
			"api_key_id", "total_cost", "today_cost", "real_total_cost", "real_today_cost",
		}).
			AddRow(int64(11), 6.0, 2.0, 1.2, 0.4))

	stats, err := repo.GetBatchAPIKeyUsageStats(context.Background(), []int64{11}, start, end)
	require.NoError(t, err)
	require.InDelta(t, 1.2, stats[11].RealTotalActualCost, 0.0001)
	require.InDelta(t, 0.4, stats[11].RealTodayActualCost, 0.0001)
	require.NoError(t, mock.ExpectationsWereMet())
}
