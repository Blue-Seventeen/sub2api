//go:build unit

package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type adminUserUsageRepoStub struct {
	UsageLogRepository
	userID    int64
	startTime time.Time
	endTime   time.Time
	stats     *usagestats.UsageStats
}

func (r *adminUserUsageRepoStub) GetUserStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	r.userID = userID
	r.startTime = startTime
	r.endTime = endTime
	return r.stats, nil
}

var _ UsageLogRepository = (*adminUserUsageRepoStub)(nil)

func TestAdminService_GetUserUsageStatsRejectsUnsupportedPeriod(t *testing.T) {
	_, err := (&adminServiceImpl{}).GetUserUsageStats(context.Background(), 42, "invalid")

	require.Error(t, err)
}

func TestAdminService_GetUserUsageStatsUsesAggregatedUsageForPeriod(t *testing.T) {
	repo := &adminUserUsageRepoStub{stats: &usagestats.UsageStats{
		TotalRequests:     7,
		TotalCost:         1.25,
		TotalTokens:       345,
		AverageDurationMs: 42.5,
	}}
	svc := &adminServiceImpl{
		usageService: NewUsageService(repo, nil, nil, nil),
	}

	before := time.Now()
	got, err := svc.GetUserUsageStats(context.Background(), 42, "month")
	after := time.Now()

	require.NoError(t, err)
	stats, ok := got.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "month", stats["period"])
	require.Equal(t, int64(7), stats["total_requests"])
	require.Equal(t, 1.25, stats["total_cost"])
	require.Equal(t, int64(345), stats["total_tokens"])
	require.Equal(t, 42.5, stats["avg_duration_ms"])
	require.Equal(t, int64(42), repo.userID)
	require.WithinDuration(t, before.AddDate(0, -1, 0), repo.startTime, time.Second)
	require.True(t, !repo.endTime.Before(before) && !repo.endTime.After(after))
}

func TestUserUsageStatsPeriodRange(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 5, 0, timezone.Location())
	tests := []struct {
		name      string
		period    string
		wantStart time.Time
	}{
		{name: "today", period: "today", wantStart: timezone.StartOfDay(now)},
		{name: "week", period: "week", wantStart: now.AddDate(0, 0, -7)},
		{name: "month", period: "month", wantStart: now.AddDate(0, -1, 0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := userUsageStatsPeriodRange(now, tt.period)

			require.NoError(t, err)
			require.Equal(t, tt.wantStart, start)
			require.Equal(t, now, end)
		})
	}
}

type adminGroupStatsUsageRepoStub struct {
	UsageLogRepository
	stats   *usagestats.UsageStats
	groupID int64
}

func (r *adminGroupStatsUsageRepoStub) GetStatsWithFilters(_ context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.groupID = filters.GroupID
	return r.stats, nil
}

func newAdminGroupStatsTestClient(t *testing.T) *dbent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:admin_group_stats_%d?mode=memory&cache=shared&_fk=1", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

func TestAdminService_GetGroupStatsAggregatesKeysAndUsage(t *testing.T) {
	ctx := context.Background()
	client := newAdminGroupStatsTestClient(t)
	user, err := client.User.Create().
		SetEmail("admin-group-stats@example.com").
		SetPasswordHash("hash").
		SetUsername("admin-group-stats").
		Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().
		SetName("admin-group-stats").
		SetStatus(StatusActive).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.APIKey.Create().
		SetUserID(user.ID).
		SetName("active-key").
		SetKey("active-key").
		SetGroupID(group.ID).
		SetStatus(StatusActive).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.APIKey.Create().
		SetUserID(user.ID).
		SetName("disabled-key").
		SetKey("disabled-key").
		SetGroupID(group.ID).
		SetStatus(StatusDisabled).
		Save(ctx)
	require.NoError(t, err)

	repo := &adminGroupStatsUsageRepoStub{stats: &usagestats.UsageStats{
		TotalRequests:     7,
		TotalCost:         1.25,
		TotalActualCost:   1.1,
		TotalTokens:       345,
		AverageDurationMs: 42.5,
	}}
	svc := &adminServiceImpl{
		entClient:    client,
		usageService: NewUsageService(repo, nil, nil, nil),
	}

	got, err := svc.GetGroupStats(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), got["total_api_keys"])
	require.Equal(t, int64(1), got["active_api_keys"])
	require.Equal(t, int64(7), got["total_requests"])
	require.Equal(t, 1.25, got["total_cost"])
	require.Equal(t, 1.1, got["total_actual_cost"])
	require.Equal(t, int64(345), got["total_tokens"])
	require.Equal(t, 42.5, got["avg_duration_ms"])
	require.Equal(t, group.ID, repo.groupID)
}

func TestAdminService_GetGroupStatsRejectsMissingGroup(t *testing.T) {
	client := newAdminGroupStatsTestClient(t)
	repo := &adminGroupStatsUsageRepoStub{stats: &usagestats.UsageStats{TotalRequests: 99}}
	svc := &adminServiceImpl{
		entClient:    client,
		usageService: NewUsageService(repo, nil, nil, nil),
	}

	got, err := svc.GetGroupStats(context.Background(), 999999)
	require.ErrorIs(t, err, ErrGroupNotFound)
	require.Nil(t, got)
	require.Zero(t, repo.groupID)
}
