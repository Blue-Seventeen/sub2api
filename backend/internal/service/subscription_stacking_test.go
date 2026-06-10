//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAggregateActiveSubscriptionsSumsUsageAndLimits(t *testing.T) {
	now := time.Now().UTC()
	dailyLimit := 100.0
	customLimit := 20.0
	sourceType := "redeem_code"
	sourceRef := "10"
	redeemCode := "RC-STACK-1"

	subs := []UserSubscription{
		{
			ID:                 1,
			UserID:             100,
			GroupID:            200,
			StartsAt:           now.Add(-2 * time.Hour),
			ExpiresAt:          now.Add(22 * time.Hour),
			Status:             SubscriptionStatusActive,
			DailyUsageUSD:      70,
			CustomUsageUSD:     20,
			DailyWindowStart:   subscriptionTimePtr(now.Add(-2 * time.Hour)),
			CustomWindowStart:  subscriptionTimePtr(now.Add(-30 * time.Minute)),
			SourceType:         &sourceType,
			SourceRefID:        &sourceRef,
			RedeemCodeSnapshot: &redeemCode,
			Group: &Group{
				ID:               200,
				Name:             "pro",
				Platform:         PlatformAnthropic,
				SubscriptionType: SubscriptionTypeSubscription,
				DailyLimitUSD:    &dailyLimit,
				CustomLimitHours: 1,
				CustomLimitUSD:   &customLimit,
				RateMultiplier:   1,
			},
		},
		{
			ID:                2,
			UserID:            100,
			GroupID:           200,
			StartsAt:          now.Add(-1 * time.Hour),
			ExpiresAt:         now.Add(47 * time.Hour),
			Status:            SubscriptionStatusActive,
			DailyUsageUSD:     0,
			CustomUsageUSD:    0,
			DailyWindowStart:  subscriptionTimePtr(now.Add(-1 * time.Hour)),
			CustomWindowStart: subscriptionTimePtr(now.Add(-1 * time.Hour)),
			Group: &Group{
				ID:               200,
				Name:             "pro",
				Platform:         PlatformAnthropic,
				SubscriptionType: SubscriptionTypeSubscription,
				DailyLimitUSD:    &dailyLimit,
				CustomLimitHours: 1,
				CustomLimitUSD:   &customLimit,
				RateMultiplier:   1,
			},
		},
	}

	agg := aggregateActiveSubscriptions(subs)

	require.NotNil(t, agg)
	require.True(t, agg.IsAggregate)
	require.Equal(t, 2, agg.SubscriptionCount)
	require.Equal(t, int64(0), agg.ID)
	require.InDelta(t, 70, agg.DailyUsageUSD, 0.000001)
	require.InDelta(t, 20, agg.CustomUsageUSD, 0.000001)
	require.NotNil(t, agg.DailyLimitUSDSnapshot)
	require.InDelta(t, 200, *agg.DailyLimitUSDSnapshot, 0.000001)
	require.NotNil(t, agg.CustomLimitUSDSnapshot)
	require.InDelta(t, 40, *agg.CustomLimitUSDSnapshot, 0.000001)
	require.Nil(t, agg.SourceType)
	require.Nil(t, agg.SourceRefID)
	require.Nil(t, agg.RedeemCodeSnapshot)
	require.Equal(t, subs[0].StartsAt, agg.StartsAt)
	require.Equal(t, subs[1].ExpiresAt, agg.ExpiresAt)
	require.NotNil(t, agg.Group)
	require.InDelta(t, 200, *agg.Group.DailyLimitUSD, 0.000001)
}

func TestActiveSubscriptionEffectiveGroupUsesCurrentGroupLimits(t *testing.T) {
	now := time.Now().UTC()
	oldLimit := 100.0
	currentLimit := 50.0
	oldName := "historic"
	sub := &UserSubscription{
		ID:                    1,
		GroupID:               20,
		StartsAt:              now.Add(-time.Hour),
		ExpiresAt:             now.Add(time.Hour),
		Status:                SubscriptionStatusActive,
		GroupNameSnapshot:     &oldName,
		DailyLimitUSDSnapshot: &oldLimit,
		Group: &Group{
			ID:               20,
			Name:             "current",
			SubscriptionType: SubscriptionTypeSubscription,
			DailyLimitUSD:    &currentLimit,
		},
	}

	group := sub.EffectiveGroup(nil)

	require.NotNil(t, group)
	require.Equal(t, "current", group.Name)
	require.NotNil(t, group.DailyLimitUSD)
	require.InDelta(t, currentLimit, *group.DailyLimitUSD, 0.000001)
	require.Same(t, sub.Group.DailyLimitUSD, sub.EffectiveDailyLimitUSD(nil))
}

func TestActiveSubscriptionEffectiveGroupReflectsRemovedLimits(t *testing.T) {
	now := time.Now().UTC()
	oldLimit := 100.0
	sub := &UserSubscription{
		ID:                    1,
		GroupID:               20,
		StartsAt:              now.Add(-time.Hour),
		ExpiresAt:             now.Add(time.Hour),
		Status:                SubscriptionStatusActive,
		DailyLimitUSDSnapshot: &oldLimit,
		Group: &Group{
			ID:               20,
			Name:             "current",
			SubscriptionType: SubscriptionTypeSubscription,
		},
	}

	group := sub.EffectiveGroup(nil)

	require.NotNil(t, group)
	require.Nil(t, group.DailyLimitUSD)
	require.Nil(t, sub.EffectiveDailyLimitUSD(nil))
}

func TestAggregateActiveSubscriptionsUsesCurrentGroupAfterLimitRemoval(t *testing.T) {
	now := time.Now().UTC()
	oldLimit := 100.0
	snapshotName := "historic"
	group := &Group{
		ID:               20,
		Name:             "current",
		SubscriptionType: SubscriptionTypeSubscription,
	}
	subs := []UserSubscription{
		{
			ID:                    1,
			UserID:                10,
			GroupID:               20,
			StartsAt:              now.Add(-2 * time.Hour),
			ExpiresAt:             now.Add(time.Hour),
			Status:                SubscriptionStatusActive,
			GroupNameSnapshot:     &snapshotName,
			DailyLimitUSDSnapshot: &oldLimit,
			Group:                 group,
		},
		{
			ID:                    2,
			UserID:                10,
			GroupID:               20,
			StartsAt:              now.Add(-time.Hour),
			ExpiresAt:             now.Add(time.Hour),
			Status:                SubscriptionStatusActive,
			GroupNameSnapshot:     &snapshotName,
			DailyLimitUSDSnapshot: &oldLimit,
			Group:                 group,
		},
	}

	agg := aggregateActiveSubscriptionsForDisplay(subs)

	require.NotNil(t, agg)
	require.True(t, agg.IsAggregate)
	require.Nil(t, agg.DailyLimitUSDSnapshot)
	require.NotNil(t, agg.Group)
	require.Equal(t, "current", agg.Group.Name)
	require.Nil(t, agg.Group.DailyLimitUSD)
}

func TestHistoricalSubscriptionEffectiveGroupUsesSnapshots(t *testing.T) {
	now := time.Now().UTC()
	snapshotLimit := 100.0
	currentLimit := 50.0
	snapshotName := "historic"
	deletedAt := now.Add(-time.Minute)
	tests := []struct {
		name string
		sub  UserSubscription
	}{
		{
			name: "status expired",
			sub: UserSubscription{
				Status:    SubscriptionStatusExpired,
				ExpiresAt: now.Add(time.Hour),
			},
		},
		{
			name: "active but expired by time",
			sub: UserSubscription{
				Status:    SubscriptionStatusActive,
				ExpiresAt: now.Add(-time.Minute),
			},
		},
		{
			name: "revoked",
			sub: UserSubscription{
				Status:    SubscriptionStatusRevoked,
				ExpiresAt: now.Add(time.Hour),
			},
		},
		{
			name: "soft deleted",
			sub: UserSubscription{
				Status:    SubscriptionStatusActive,
				ExpiresAt: now.Add(time.Hour),
				DeletedAt: &deletedAt,
			},
		},
		{
			name: "aggregate virtual card",
			sub: UserSubscription{
				Status:      SubscriptionStatusActive,
				ExpiresAt:   now.Add(time.Hour),
				IsAggregate: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := tt.sub
			sub.GroupID = 20
			sub.StartsAt = now.Add(-time.Hour)
			sub.GroupNameSnapshot = &snapshotName
			sub.DailyLimitUSDSnapshot = &snapshotLimit
			sub.Group = &Group{
				ID:               20,
				Name:             "current",
				SubscriptionType: SubscriptionTypeSubscription,
				DailyLimitUSD:    &currentLimit,
			}

			group := sub.EffectiveGroup(nil)

			require.NotNil(t, group)
			require.Equal(t, snapshotName, group.Name)
			require.NotNil(t, group.DailyLimitUSD)
			require.InDelta(t, snapshotLimit, *group.DailyLimitUSD, 0.000001)
		})
	}
}

func TestAggregateActiveByGroupKeepsExpiredRecordsSeparate(t *testing.T) {
	now := time.Now().UTC()
	active1 := UserSubscription{ID: 1, UserID: 10, GroupID: 20, StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour), Status: SubscriptionStatusActive}
	active2 := UserSubscription{ID: 2, UserID: 10, GroupID: 20, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour), Status: SubscriptionStatusActive}
	expired := UserSubscription{ID: 3, UserID: 10, GroupID: 20, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-24 * time.Hour), Status: SubscriptionStatusExpired}

	out := aggregateActiveByGroup([]UserSubscription{active1, expired, active2})

	require.Len(t, out, 2)
	require.True(t, out[0].IsAggregate)
	require.Equal(t, 2, out[0].SubscriptionCount)
	require.Equal(t, int64(3), out[1].ID)
	require.False(t, out[1].IsAggregate)
}

func TestUserVisibleSubscriptionsAggregateExpiredCardsByGroup(t *testing.T) {
	now := time.Now().UTC()
	active1 := UserSubscription{ID: 1, UserID: 10, GroupID: 20, StartsAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(time.Hour), Status: SubscriptionStatusActive, DailyUsageUSD: 10}
	active2 := UserSubscription{ID: 2, UserID: 10, GroupID: 20, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour), Status: SubscriptionStatusActive, DailyUsageUSD: 20}
	expired1 := UserSubscription{ID: 3, UserID: 10, GroupID: 20, StartsAt: now.Add(-72 * time.Hour), ExpiresAt: now.Add(-48 * time.Hour), Status: SubscriptionStatusExpired, DailyUsageUSD: 30}
	expired2 := UserSubscription{ID: 4, UserID: 10, GroupID: 20, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-24 * time.Hour), Status: SubscriptionStatusExpired, DailyUsageUSD: 40}
	otherGroup := UserSubscription{ID: 5, UserID: 10, GroupID: 21, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: SubscriptionStatusActive, DailyUsageUSD: 50}

	out := aggregateUserVisibleByGroupForDisplay([]UserSubscription{active1, expired1, active2, expired2, otherGroup})

	require.Len(t, out, 3)
	require.True(t, out[0].IsAggregate)
	require.Equal(t, SubscriptionStatusActive, out[0].Status)
	require.Equal(t, 2, out[0].SubscriptionCount)
	require.InDelta(t, 30, out[0].DailyUsageUSD, 0.000001)
	require.True(t, out[1].IsAggregate)
	require.Equal(t, SubscriptionStatusExpired, out[1].Status)
	require.Equal(t, 2, out[1].SubscriptionCount)
	require.InDelta(t, 70, out[1].DailyUsageUSD, 0.000001)
	require.False(t, out[2].IsAggregate)
	require.Equal(t, int64(5), out[2].ID)
}

func TestUserVisibleExpiredAggregationPreservesHistoricalWindowUsage(t *testing.T) {
	now := time.Now().UTC()
	expired1 := UserSubscription{
		ID:               3,
		UserID:           10,
		GroupID:          20,
		StartsAt:         now.Add(-10 * 24 * time.Hour),
		ExpiresAt:        now.Add(-5 * 24 * time.Hour),
		Status:           SubscriptionStatusExpired,
		DailyUsageUSD:    30,
		DailyWindowStart: subscriptionTimePtr(now.Add(-9 * 24 * time.Hour)),
	}
	expired2 := UserSubscription{
		ID:               4,
		UserID:           10,
		GroupID:          20,
		StartsAt:         now.Add(-8 * 24 * time.Hour),
		ExpiresAt:        now.Add(-4 * 24 * time.Hour),
		Status:           SubscriptionStatusExpired,
		DailyUsageUSD:    40,
		DailyWindowStart: subscriptionTimePtr(now.Add(-7 * 24 * time.Hour)),
	}

	out := aggregateUserVisibleByGroupForDisplay([]UserSubscription{expired1, expired2})

	require.Len(t, out, 1)
	require.True(t, out[0].IsAggregate)
	require.Equal(t, SubscriptionStatusExpired, out[0].Status)
	require.InDelta(t, 70, out[0].DailyUsageUSD, 0.000001)
}

func TestDeductSubscriptionDaysNewestReturnsSnapshotsAndRestoreExactCards(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	oldCard := UserSubscription{
		ID:        1,
		UserID:    10,
		GroupID:   20,
		StartsAt:  now.Add(-24 * time.Hour),
		ExpiresAt: now.Add(10 * 24 * time.Hour),
		Status:    SubscriptionStatusActive,
		Notes:     "old",
	}
	newCard := UserSubscription{
		ID:              2,
		UserID:          10,
		GroupID:         20,
		StartsAt:        now,
		ExpiresAt:       now.Add(48 * time.Hour),
		Status:          SubscriptionStatusActive,
		DailyUsageUSD:   7,
		WeeklyUsageUSD:  8,
		MonthlyUsageUSD: 9,
		CustomUsageUSD:  10,
		Notes:           "new",
	}
	repo.seed(&oldCard)
	repo.seed(&newCard)
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	snapshots, err := svc.DeductSubscriptionDaysNewestWithSnapshots(context.Background(), 10, 20, 1, "refund deduct")
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, int64(2), snapshots[0].ID)
	require.Equal(t, newCard.ExpiresAt, snapshots[0].ExpiresAt)

	updatedNew, err := repo.GetByID(context.Background(), 2)
	require.NoError(t, err)
	require.True(t, updatedNew.ExpiresAt.Before(newCard.ExpiresAt))
	require.Contains(t, updatedNew.Notes, "refund deduct")
	updatedOld, err := repo.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, oldCard.ExpiresAt, updatedOld.ExpiresAt)

	require.NoError(t, svc.RestoreSubscriptionSnapshots(context.Background(), snapshots))
	restoredNew, err := repo.GetByID(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, newCard.ExpiresAt, restoredNew.ExpiresAt)
	require.Equal(t, newCard.Status, restoredNew.Status)
	require.Equal(t, newCard.Notes, restoredNew.Notes)
	require.InDelta(t, newCard.DailyUsageUSD, restoredNew.DailyUsageUSD, 0.000001)
	require.InDelta(t, newCard.CustomUsageUSD, restoredNew.CustomUsageUSD, 0.000001)
}

func TestDeductSubscriptionDaysNewestSnapshotsCurrentGroupWhenCardExpires(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	oldGroupName := "old snapshot"
	oldDaily := 10.0
	oldWeekly := 20.0
	currentDaily := 100.0
	currentWeekly := 500.0
	currentMonthly := 1000.0
	currentCustom := 50.0
	card := UserSubscription{
		ID:                     3,
		UserID:                 10,
		GroupID:                20,
		StartsAt:               now.Add(-24 * time.Hour),
		ExpiresAt:              now.Add(12 * time.Hour),
		Status:                 SubscriptionStatusActive,
		Notes:                  "before",
		GroupNameSnapshot:      &oldGroupName,
		DailyLimitUSDSnapshot:  &oldDaily,
		WeeklyLimitUSDSnapshot: &oldWeekly,
	}
	repo.seed(&card)
	groupRepo := &subscriptionGroupRepoStub{group: &Group{
		ID:               20,
		Name:             "current snapshot",
		Platform:         PlatformAnthropic,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeSubscription,
		RateMultiplier:   2,
		DailyLimitUSD:    &currentDaily,
		WeeklyLimitUSD:   &currentWeekly,
		MonthlyLimitUSD:  &currentMonthly,
		CustomLimitHours: 6,
		CustomLimitUSD:   &currentCustom,
	}}
	svc := NewSubscriptionService(groupRepo, repo, nil, nil, nil)

	snapshots, err := svc.DeductSubscriptionDaysNewestWithSnapshots(context.Background(), 10, 20, 1, "refund expire")
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, oldGroupName, *snapshots[0].GroupNameSnapshot)

	expired, err := repo.GetByID(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, SubscriptionStatusExpired, expired.Status)
	require.NotNil(t, expired.GroupNameSnapshot)
	require.Equal(t, "current snapshot", *expired.GroupNameSnapshot)
	require.NotNil(t, expired.DailyLimitUSDSnapshot)
	require.InDelta(t, currentDaily, *expired.DailyLimitUSDSnapshot, 0.000001)
	require.NotNil(t, expired.WeeklyLimitUSDSnapshot)
	require.InDelta(t, currentWeekly, *expired.WeeklyLimitUSDSnapshot, 0.000001)
	require.NotNil(t, expired.MonthlyLimitUSDSnapshot)
	require.InDelta(t, currentMonthly, *expired.MonthlyLimitUSDSnapshot, 0.000001)
	require.NotNil(t, expired.CustomLimitHoursSnapshot)
	require.Equal(t, 6, *expired.CustomLimitHoursSnapshot)
	require.NotNil(t, expired.CustomLimitUSDSnapshot)
	require.InDelta(t, currentCustom, *expired.CustomLimitUSDSnapshot, 0.000001)

	require.NoError(t, svc.RestoreSubscriptionSnapshots(context.Background(), snapshots))
	restored, err := repo.GetByID(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, card.Status, restored.Status)
	require.Equal(t, card.ExpiresAt, restored.ExpiresAt)
	require.Equal(t, card.Notes, restored.Notes)
	require.NotNil(t, restored.GroupNameSnapshot)
	require.Equal(t, oldGroupName, *restored.GroupNameSnapshot)
	require.NotNil(t, restored.DailyLimitUSDSnapshot)
	require.InDelta(t, oldDaily, *restored.DailyLimitUSDSnapshot, 0.000001)
	require.NotNil(t, restored.WeeklyLimitUSDSnapshot)
	require.InDelta(t, oldWeekly, *restored.WeeklyLimitUSDSnapshot, 0.000001)
	require.Nil(t, restored.MonthlyLimitUSDSnapshot)
	require.Nil(t, restored.CustomLimitHoursSnapshot)
	require.Nil(t, restored.CustomLimitUSDSnapshot)
}

func TestGetActiveSubscriptionNormalizesStackedWindowsBeforePreflight(t *testing.T) {
	now := time.Now().UTC()
	dailyLimit := 100.0
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &dailyLimit}
	repo := &activeSubscriptionListRepoStub{
		subs: []UserSubscription{
			{
				ID:               1,
				UserID:           10,
				GroupID:          20,
				StartsAt:         now.Add(-48 * time.Hour),
				ExpiresAt:        now.Add(24 * time.Hour),
				Status:           SubscriptionStatusActive,
				DailyUsageUSD:    100,
				DailyWindowStart: subscriptionTimePtr(now.Add(-25 * time.Hour)),
				Group:            group,
			},
			{
				ID:               2,
				UserID:           10,
				GroupID:          20,
				StartsAt:         now.Add(-2 * time.Hour),
				ExpiresAt:        now.Add(24 * time.Hour),
				Status:           SubscriptionStatusActive,
				DailyUsageUSD:    50,
				DailyWindowStart: subscriptionTimePtr(now.Add(-2 * time.Hour)),
				Group:            group,
			},
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	sub, err := svc.GetActiveSubscription(context.Background(), 10, 20)
	require.NoError(t, err)
	require.True(t, sub.IsAggregate)
	require.InDelta(t, 50, sub.DailyUsageUSD, 0.000001)
	require.NotNil(t, sub.DailyLimitUSDSnapshot)
	require.InDelta(t, 200, *sub.DailyLimitUSDSnapshot, 0.000001)

	needsMaintenance, err := svc.ValidateAndCheckLimits(sub, group)
	require.NoError(t, err)
	require.False(t, needsMaintenance)
}

func TestBillingCacheInvalidationClearsSubscriptionL1(t *testing.T) {
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription}
	now := time.Now().UTC()
	repo := &activeSubscriptionListRepoStub{
		subs: []UserSubscription{{
			ID:        1,
			UserID:    10,
			GroupID:   20,
			StartsAt:  now.Add(-time.Hour),
			ExpiresAt: now.Add(time.Hour),
			Status:    SubscriptionStatusActive,
			Group:     group,
		}},
	}
	cfg := &config.Config{}
	cfg.SubscriptionCache.L1Size = 16
	cfg.SubscriptionCache.L1TTLSeconds = 60
	billingCache := NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	svc := NewSubscriptionService(groupRepoNoop{}, repo, billingCache, nil, cfg)
	t.Cleanup(svc.Stop)
	billingCache.subscriptionInvalidatorMu.RLock()
	hasInvalidator := billingCache.subscriptionL1Invalidator != nil
	billingCache.subscriptionInvalidatorMu.RUnlock()
	require.True(t, hasInvalidator)

	key := subCacheKey(10, 20)
	ok := svc.subCacheL1.SetWithTTL(key, &subCacheEntry{
		sub: &UserSubscription{
			ID:            1,
			UserID:        10,
			GroupID:       20,
			StartsAt:      now.Add(-time.Hour),
			ExpiresAt:     now.Add(time.Hour),
			Status:        SubscriptionStatusActive,
			DailyUsageUSD: 0,
			Group:         group,
		},
		version: svc.subCacheVersion(key),
	}, 1, time.Minute)
	require.True(t, ok)
	svc.subCacheL1.Wait()
	repo.subs[0].DailyUsageUSD = 9.5

	require.NoError(t, billingCache.InvalidateSubscription(context.Background(), 10, 20))
	require.Equal(t, uint64(1), svc.subCacheVersion(key))
	svc.subCacheL1.Wait()
	sub, err := svc.GetActiveSubscription(context.Background(), 10, 20)
	require.NoError(t, err)
	require.InDelta(t, 9.5, sub.DailyUsageUSD, 0.000001)
	require.Equal(t, 1, repo.calls)
}

func TestBillingCacheSubscriptionUsageUpdateCallsL1Updater(t *testing.T) {
	billingCache := NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(billingCache.Stop)
	var gotUserID, gotGroupID int64
	var gotCost float64
	billingCache.SetSubscriptionL1UsageUpdater(func(userID, groupID int64, costUSD float64) {
		gotUserID = userID
		gotGroupID = groupID
		gotCost = costUSD
	})

	billingCache.QueueUpdateSubscriptionUsage(10, 20, 2.5)

	require.Equal(t, int64(10), gotUserID)
	require.Equal(t, int64(20), gotGroupID)
	require.InDelta(t, 2.5, gotCost, 0.000001)
}

func TestGetActiveSubscriptionDoesNotCacheInFlightResultAfterInvalidation(t *testing.T) {
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription}
	now := time.Now().UTC()
	repo := &blockingActiveSubscriptionListRepoStub{
		subs: []UserSubscription{{
			ID:            1,
			UserID:        10,
			GroupID:       20,
			StartsAt:      now.Add(-time.Hour),
			ExpiresAt:     now.Add(time.Hour),
			Status:        SubscriptionStatusActive,
			DailyUsageUSD: 0,
			Group:         group,
		}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	cfg := &config.Config{}
	cfg.SubscriptionCache.L1Size = 16
	cfg.SubscriptionCache.L1TTLSeconds = 60
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, cfg)
	t.Cleanup(svc.Stop)

	firstResult := make(chan *UserSubscription, 1)
	firstErr := make(chan error, 1)
	go func() {
		sub, err := svc.GetActiveSubscription(context.Background(), 10, 20)
		firstResult <- sub
		firstErr <- err
	}()

	<-repo.started
	repo.setUsage(9.5)
	svc.InvalidateSubCache(10, 20)
	close(repo.release)

	require.NoError(t, <-firstErr)
	require.InDelta(t, 9.5, (<-firstResult).DailyUsageUSD, 0.000001)

	sub, err := svc.GetActiveSubscription(context.Background(), 10, 20)
	require.NoError(t, err)
	require.InDelta(t, 9.5, sub.DailyUsageUSD, 0.000001)
	require.GreaterOrEqual(t, repo.calls(), 2)
}

func TestValidateAndCheckLimitsRejectsAggregateWithoutPerCardCapacity(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit}
	subs := []UserSubscription{
		{
			ID:                     1,
			UserID:                 10,
			GroupID:                20,
			StartsAt:               now.Add(-time.Hour),
			ExpiresAt:              now.Add(time.Hour),
			Status:                 SubscriptionStatusActive,
			DailyUsageUSD:          10,
			WeeklyUsageUSD:         0,
			DailyWindowStart:       subscriptionTimePtr(now),
			WeeklyWindowStart:      subscriptionTimePtr(now),
			DailyLimitUSDSnapshot:  &limit,
			WeeklyLimitUSDSnapshot: &limit,
			Group:                  group,
		},
		{
			ID:                     2,
			UserID:                 10,
			GroupID:                20,
			StartsAt:               now.Add(-time.Hour),
			ExpiresAt:              now.Add(time.Hour),
			Status:                 SubscriptionStatusActive,
			DailyUsageUSD:          0,
			WeeklyUsageUSD:         10,
			DailyWindowStart:       subscriptionTimePtr(now),
			WeeklyWindowStart:      subscriptionTimePtr(now),
			DailyLimitUSDSnapshot:  &limit,
			WeeklyLimitUSDSnapshot: &limit,
			Group:                  group,
		},
	}
	agg := aggregateActiveSubscriptionsForDisplay(subs)
	require.NotNil(t, agg)
	require.NotNil(t, agg.StackedAvailableUSD)
	require.InDelta(t, 0, *agg.StackedAvailableUSD, 0.000001)
	require.True(t, agg.CheckDailyLimit(agg.Group, 0))
	require.True(t, agg.CheckWeeklyLimit(agg.Group, 0))

	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	_, err := svc.ValidateAndCheckLimits(agg, agg.Group)

	require.ErrorIs(t, err, ErrDailyLimitExceeded)
}

func TestValidateAndCheckLimitsDoesNotResetAggregateCustomUsage(t *testing.T) {
	now := time.Now().UTC()
	limit := 40.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		CustomLimitHours: 1,
		CustomLimitUSD:   &limit,
	}
	sub := &UserSubscription{
		IsAggregate:              true,
		UserID:                   10,
		GroupID:                  20,
		Status:                   SubscriptionStatusActive,
		StartsAt:                 now.Add(-2 * time.Hour),
		ExpiresAt:                now.Add(time.Hour),
		CustomUsageUSD:           20,
		CustomWindowStart:        subscriptionTimePtr(now.Add(-2 * time.Hour)),
		CustomLimitHoursSnapshot: &group.CustomLimitHours,
		CustomLimitUSDSnapshot:   &limit,
		Group:                    group,
	}
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)

	needsMaintenance, err := svc.ValidateAndCheckLimits(sub, group)

	require.NoError(t, err)
	require.False(t, needsMaintenance)
	require.InDelta(t, 20, sub.CustomUsageUSD, 0.000001)
}

func TestUserDisplayAggregateShowsEffectiveCapacityWhenWindowsBlockEachOther(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit}
	subs := []UserSubscription{
		{
			ID:                1,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-2 * time.Hour),
			ExpiresAt:         now.Add(24 * time.Hour),
			Status:            SubscriptionStatusActive,
			DailyUsageUSD:     10,
			WeeklyUsageUSD:    0,
			DailyWindowStart:  subscriptionTimePtr(now.Add(-2 * time.Hour)),
			WeeklyWindowStart: subscriptionTimePtr(now.Add(-2 * time.Hour)),
			Group:             group,
		},
		{
			ID:                2,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-1 * time.Hour),
			ExpiresAt:         now.Add(24 * time.Hour),
			Status:            SubscriptionStatusActive,
			DailyUsageUSD:     0,
			WeeklyUsageUSD:    10,
			DailyWindowStart:  subscriptionTimePtr(now.Add(-1 * time.Hour)),
			WeeklyWindowStart: subscriptionTimePtr(now.Add(-1 * time.Hour)),
			Group:             group,
		},
	}

	agg := aggregateActiveSubscriptionsForUserDisplay(subs)

	require.NotNil(t, agg)
	require.True(t, agg.IsAggregate)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.NotNil(t, agg.EffectiveWeeklyUsageUSD)
	require.InDelta(t, 20, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 10, *agg.EffectiveWeeklyUsageUSD, 0.000001)
	require.InDelta(t, 10, agg.DailyUsageUSD, 0.000001)
	require.InDelta(t, 10, agg.WeeklyUsageUSD, 0.000001)
}

func TestUserDisplayAggregateTreatsDailyAsBlockedWhenOlderCardWeeklyAndMonthlyAreFull(t *testing.T) {
	now := time.Now().UTC()
	limit := 100.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
		WeeklyLimitUSD:   &limit,
		MonthlyLimitUSD:  &limit,
	}
	subs := []UserSubscription{
		{
			ID:                 1,
			UserID:             10,
			GroupID:            20,
			StartsAt:           now.Add(-8 * 24 * time.Hour),
			ExpiresAt:          now.Add(24 * time.Hour),
			Status:             SubscriptionStatusActive,
			DailyUsageUSD:      0,
			WeeklyUsageUSD:     100,
			MonthlyUsageUSD:    100,
			DailyWindowStart:   subscriptionTimePtr(now.Add(-time.Hour)),
			WeeklyWindowStart:  subscriptionTimePtr(now.Add(-6 * 24 * time.Hour)),
			MonthlyWindowStart: subscriptionTimePtr(now.Add(-8 * 24 * time.Hour)),
			Group:              group,
		},
		{
			ID:                 2,
			UserID:             10,
			GroupID:            20,
			StartsAt:           now.Add(-time.Hour),
			ExpiresAt:          now.Add(24 * time.Hour),
			Status:             SubscriptionStatusActive,
			DailyUsageUSD:      0,
			WeeklyUsageUSD:     0,
			MonthlyUsageUSD:    0,
			DailyWindowStart:   subscriptionTimePtr(now.Add(-time.Hour)),
			WeeklyWindowStart:  subscriptionTimePtr(now.Add(-time.Hour)),
			MonthlyWindowStart: subscriptionTimePtr(now.Add(-time.Hour)),
			Group:              group,
		},
	}

	agg := aggregateActiveSubscriptionsForUserDisplay(subs)

	require.NotNil(t, agg)
	require.True(t, agg.IsAggregate)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 100, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.NotNil(t, agg.EffectiveWeeklyUsageUSD)
	require.NotNil(t, agg.EffectiveMonthlyUsageUSD)
	require.InDelta(t, 100, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 100, *agg.EffectiveWeeklyUsageUSD, 0.000001)
	require.InDelta(t, 100, *agg.EffectiveMonthlyUsageUSD, 0.000001)
	require.InDelta(t, 0, agg.DailyUsageUSD, 0.000001)
	require.InDelta(t, 100, agg.WeeklyUsageUSD, 0.000001)
	require.InDelta(t, 100, agg.MonthlyUsageUSD, 0.000001)
}

func TestUserDisplayAggregateResetTimeWaitsUntilCardCanActuallyRecover(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit}
	dailySoon := now.Add(-23 * time.Hour)
	weeklyMuchLater := now.Add(-24 * time.Hour)
	weeklyEarlier := now.Add(-6*24*time.Hour - time.Hour)
	dailyLater := now.Add(-2 * time.Hour)
	subs := []UserSubscription{
		{
			ID:                1,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-6 * 24 * time.Hour),
			ExpiresAt:         now.Add(24 * time.Hour),
			Status:            SubscriptionStatusActive,
			DailyUsageUSD:     10,
			WeeklyUsageUSD:    10,
			DailyWindowStart:  &dailySoon,
			WeeklyWindowStart: &weeklyMuchLater,
			Group:             group,
		},
		{
			ID:                2,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-6 * 24 * time.Hour),
			ExpiresAt:         now.Add(24 * time.Hour),
			Status:            SubscriptionStatusActive,
			DailyUsageUSD:     10,
			WeeklyUsageUSD:    10,
			DailyWindowStart:  &dailyLater,
			WeeklyWindowStart: &weeklyEarlier,
			Group:             group,
		},
	}

	agg := aggregateActiveSubscriptionsForUserDisplay(subs)

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveResetsAt)
	expected := weeklyEarlier.Add(subscriptionWeeklyWindow)
	require.WithinDuration(t, expected, *agg.EffectiveResetsAt, time.Second)
	require.Greater(t, agg.EffectiveResetsAt.Sub(dailySoon.Add(subscriptionDailyWindow)), time.Second)
	require.NotNil(t, agg.EffectiveDailyResetsAt)
	require.WithinDuration(t, expected, *agg.EffectiveDailyResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveWeeklyResetsAt)
	require.WithinDuration(t, expected, *agg.EffectiveWeeklyResetsAt, time.Second)
}

func TestUserDisplayAggregateWindowResetTimeUsesDailyCardInsteadOfWeeklyBlockedCard(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit, WeeklyLimitUSD: &limit}
	dailyFullStart := now.Add(-5 * time.Minute)
	weeklyUsableStart := now.Add(-time.Hour)
	dailyUsableStart := now.Add(-time.Hour)
	weeklyBlockedStart := now.Add(-time.Hour)
	subs := []UserSubscription{
		{
			ID:                1,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-time.Hour),
			ExpiresAt:         now.Add(48 * time.Hour),
			Status:            SubscriptionStatusActive,
			DailyUsageUSD:     10,
			WeeklyUsageUSD:    0,
			DailyWindowStart:  &dailyFullStart,
			WeeklyWindowStart: &weeklyUsableStart,
			Group:             group,
		},
		{
			ID:                2,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-time.Hour),
			ExpiresAt:         now.Add(48 * time.Hour),
			Status:            SubscriptionStatusActive,
			DailyUsageUSD:     0,
			WeeklyUsageUSD:    10,
			DailyWindowStart:  &dailyUsableStart,
			WeeklyWindowStart: &weeklyBlockedStart,
			Group:             group,
		},
	}

	agg := aggregateActiveSubscriptionsForUserDisplay(subs)

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyResetsAt)
	require.WithinDuration(t, dailyFullStart.Add(subscriptionDailyWindow), *agg.EffectiveDailyResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveWeeklyResetsAt)
	require.WithinDuration(t, weeklyBlockedStart.Add(subscriptionWeeklyWindow), *agg.EffectiveWeeklyResetsAt, time.Second)
}

func TestUserDisplaySingleCardUsesEffectiveBottleneck(t *testing.T) {
	now := time.Now().UTC()
	dailyLimit := 100.0
	weeklyLimit := 10.0
	group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &dailyLimit, WeeklyLimitUSD: &weeklyLimit}
	dailyStart := now.Add(-time.Hour)
	weeklyStart := now.Add(-2 * 24 * time.Hour)
	sub := UserSubscription{
		ID:                1,
		UserID:            10,
		GroupID:           20,
		StartsAt:          now.Add(-2 * 24 * time.Hour),
		ExpiresAt:         now.Add(24 * time.Hour),
		Status:            SubscriptionStatusActive,
		DailyUsageUSD:     0,
		WeeklyUsageUSD:    10,
		DailyWindowStart:  &dailyStart,
		WeeklyWindowStart: &weeklyStart,
		Group:             group,
	}

	agg := aggregateActiveSubscriptionsForUserDisplay([]UserSubscription{sub})

	require.NotNil(t, agg)
	require.False(t, agg.IsAggregate)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.InDelta(t, 100, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 0, agg.DailyUsageUSD, 0.000001)
	require.NotNil(t, agg.EffectiveResetsAt)
	require.WithinDuration(t, weeklyStart.Add(subscriptionWeeklyWindow), *agg.EffectiveResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveDailyResetsAt)
	require.WithinDuration(t, weeklyStart.Add(subscriptionWeeklyWindow), *agg.EffectiveDailyResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveWeeklyResetsAt)
	require.WithinDuration(t, weeklyStart.Add(subscriptionWeeklyWindow), *agg.EffectiveWeeklyResetsAt, time.Second)
}

func TestUserDisplaySingleCardMonthlyBottleneckFillsShorterWindows(t *testing.T) {
	now := time.Now().UTC()
	dailyLimit := 100.0
	weeklyLimit := 100.0
	monthlyLimit := 10.0
	customLimit := 100.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &dailyLimit,
		WeeklyLimitUSD:   &weeklyLimit,
		MonthlyLimitUSD:  &monthlyLimit,
		CustomLimitHours: 1,
		CustomLimitUSD:   &customLimit,
	}
	monthlyStart := now.Add(-3 * 24 * time.Hour)
	sub := UserSubscription{
		ID:                 1,
		UserID:             10,
		GroupID:            20,
		StartsAt:           now.Add(-3 * 24 * time.Hour),
		ExpiresAt:          now.Add(24 * time.Hour),
		Status:             SubscriptionStatusActive,
		DailyUsageUSD:      0,
		WeeklyUsageUSD:     0,
		MonthlyUsageUSD:    10,
		CustomUsageUSD:     0,
		DailyWindowStart:   subscriptionTimePtr(now.Add(-time.Hour)),
		WeeklyWindowStart:  subscriptionTimePtr(now.Add(-time.Hour)),
		MonthlyWindowStart: &monthlyStart,
		CustomWindowStart:  subscriptionTimePtr(now.Add(-30 * time.Minute)),
		Group:              group,
	}

	agg := aggregateActiveSubscriptionsForUserDisplay([]UserSubscription{sub})

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.NotNil(t, agg.EffectiveWeeklyUsageUSD)
	require.NotNil(t, agg.EffectiveMonthlyUsageUSD)
	require.NotNil(t, agg.EffectiveCustomUsageUSD)
	require.InDelta(t, 100, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 100, *agg.EffectiveWeeklyUsageUSD, 0.000001)
	require.InDelta(t, 10, *agg.EffectiveMonthlyUsageUSD, 0.000001)
	require.InDelta(t, 100, *agg.EffectiveCustomUsageUSD, 0.000001)
	require.InDelta(t, 0, agg.DailyUsageUSD, 0.000001)
	require.InDelta(t, 0, agg.WeeklyUsageUSD, 0.000001)
	require.InDelta(t, 10, agg.MonthlyUsageUSD, 0.000001)
	require.NotNil(t, agg.EffectiveResetsAt)
	require.WithinDuration(t, monthlyStart.Add(subscriptionMonthlyWindow), *agg.EffectiveResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveDailyResetsAt)
	require.WithinDuration(t, monthlyStart.Add(subscriptionMonthlyWindow), *agg.EffectiveDailyResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveWeeklyResetsAt)
	require.WithinDuration(t, monthlyStart.Add(subscriptionMonthlyWindow), *agg.EffectiveWeeklyResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveMonthlyResetsAt)
	require.WithinDuration(t, monthlyStart.Add(subscriptionMonthlyWindow), *agg.EffectiveMonthlyResetsAt, time.Second)
}

func TestUserDisplayEffectiveResetTimeUsesLaterBlockingCustomResetThanDaily(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
		CustomLimitHours: 48,
		CustomLimitUSD:   &limit,
	}
	dailyStart := now.Add(-23 * time.Hour)
	customStart := now.Add(-time.Hour)
	sub := UserSubscription{
		ID:                1,
		UserID:            10,
		GroupID:           20,
		StartsAt:          now.Add(-24 * time.Hour),
		ExpiresAt:         now.Add(72 * time.Hour),
		Status:            SubscriptionStatusActive,
		DailyUsageUSD:     10,
		CustomUsageUSD:    10,
		DailyWindowStart:  &dailyStart,
		CustomWindowStart: &customStart,
		Group:             group,
	}

	agg := aggregateActiveSubscriptionsForUserDisplay([]UserSubscription{sub})

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.NotNil(t, agg.EffectiveCustomUsageUSD)
	require.InDelta(t, 10, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 10, *agg.EffectiveCustomUsageUSD, 0.000001)
	require.InDelta(t, 10, agg.DailyUsageUSD, 0.000001)
	require.InDelta(t, 10, agg.CustomUsageUSD, 0.000001)
	require.NotNil(t, agg.EffectiveResetsAt)
	require.WithinDuration(t, customStart.Add(48*time.Hour), *agg.EffectiveResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveDailyResetsAt)
	require.WithinDuration(t, customStart.Add(48*time.Hour), *agg.EffectiveDailyResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveCustomResetsAt)
	require.WithinDuration(t, customStart.Add(48*time.Hour), *agg.EffectiveCustomResetsAt, time.Second)
}

func TestUserDisplayEffectiveResetTimeUsesLaterBlockingDailyResetThanCustom(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
		CustomLimitHours: 2,
		CustomLimitUSD:   &limit,
	}
	dailyStart := now.Add(-time.Hour)
	customStart := now.Add(-90 * time.Minute)
	sub := UserSubscription{
		ID:                1,
		UserID:            10,
		GroupID:           20,
		StartsAt:          now.Add(-2 * time.Hour),
		ExpiresAt:         now.Add(48 * time.Hour),
		Status:            SubscriptionStatusActive,
		DailyUsageUSD:     10,
		CustomUsageUSD:    10,
		DailyWindowStart:  &dailyStart,
		CustomWindowStart: &customStart,
		Group:             group,
	}

	agg := aggregateActiveSubscriptionsForUserDisplay([]UserSubscription{sub})

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveResetsAt)
	require.WithinDuration(t, dailyStart.Add(subscriptionDailyWindow), *agg.EffectiveResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveDailyResetsAt)
	require.WithinDuration(t, dailyStart.Add(subscriptionDailyWindow), *agg.EffectiveDailyResetsAt, time.Second)
	require.NotNil(t, agg.EffectiveCustomResetsAt)
	require.WithinDuration(t, dailyStart.Add(subscriptionDailyWindow), *agg.EffectiveCustomResetsAt, time.Second)
}

func TestUserDisplayShortCustomWindowDoesNotFillLongerWindows(t *testing.T) {
	now := time.Now().UTC()
	limit := 100.0
	customLimit := 10.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
		WeeklyLimitUSD:   &limit,
		MonthlyLimitUSD:  &limit,
		CustomLimitHours: 2,
		CustomLimitUSD:   &customLimit,
	}
	customStart := now.Add(-90 * time.Minute)
	sub := UserSubscription{
		ID:                 1,
		UserID:             10,
		GroupID:            20,
		StartsAt:           now.Add(-time.Hour),
		ExpiresAt:          now.Add(48 * time.Hour),
		Status:             SubscriptionStatusActive,
		DailyUsageUSD:      0,
		WeeklyUsageUSD:     0,
		MonthlyUsageUSD:    0,
		CustomUsageUSD:     10,
		DailyWindowStart:   subscriptionTimePtr(now.Add(-time.Hour)),
		WeeklyWindowStart:  subscriptionTimePtr(now.Add(-time.Hour)),
		MonthlyWindowStart: subscriptionTimePtr(now.Add(-time.Hour)),
		CustomWindowStart:  &customStart,
		Group:              group,
	}

	agg := aggregateActiveSubscriptionsForUserDisplay([]UserSubscription{sub})

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.NotNil(t, agg.EffectiveWeeklyUsageUSD)
	require.NotNil(t, agg.EffectiveMonthlyUsageUSD)
	require.NotNil(t, agg.EffectiveCustomUsageUSD)
	require.InDelta(t, 0, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 0, *agg.EffectiveWeeklyUsageUSD, 0.000001)
	require.InDelta(t, 0, *agg.EffectiveMonthlyUsageUSD, 0.000001)
	require.InDelta(t, 10, *agg.EffectiveCustomUsageUSD, 0.000001)
	require.NotNil(t, agg.EffectiveResetsAt)
	require.WithinDuration(t, customStart.Add(2*time.Hour), *agg.EffectiveResetsAt, time.Second)
}

func TestUserDisplayLongCustomWindowBlocksMonthlyAndShorterWindows(t *testing.T) {
	now := time.Now().UTC()
	limit := 100.0
	customLimit := 10.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
		WeeklyLimitUSD:   &limit,
		MonthlyLimitUSD:  &limit,
		CustomLimitHours: 1000,
		CustomLimitUSD:   &customLimit,
	}
	customStart := now.Add(-10 * 24 * time.Hour)
	sub := UserSubscription{
		ID:                 1,
		UserID:             10,
		GroupID:            20,
		StartsAt:           now.Add(-10 * 24 * time.Hour),
		ExpiresAt:          now.Add(60 * 24 * time.Hour),
		Status:             SubscriptionStatusActive,
		DailyUsageUSD:      0,
		WeeklyUsageUSD:     0,
		MonthlyUsageUSD:    0,
		CustomUsageUSD:     10,
		DailyWindowStart:   subscriptionTimePtr(now.Add(-time.Hour)),
		WeeklyWindowStart:  subscriptionTimePtr(now.Add(-time.Hour)),
		MonthlyWindowStart: subscriptionTimePtr(now.Add(-time.Hour)),
		CustomWindowStart:  &customStart,
		Group:              group,
	}

	agg := aggregateActiveSubscriptionsForUserDisplay([]UserSubscription{sub})

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.NotNil(t, agg.EffectiveWeeklyUsageUSD)
	require.NotNil(t, agg.EffectiveMonthlyUsageUSD)
	require.NotNil(t, agg.EffectiveCustomUsageUSD)
	require.InDelta(t, 100, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 100, *agg.EffectiveWeeklyUsageUSD, 0.000001)
	require.InDelta(t, 100, *agg.EffectiveMonthlyUsageUSD, 0.000001)
	require.InDelta(t, 10, *agg.EffectiveCustomUsageUSD, 0.000001)
	require.NotNil(t, agg.EffectiveMonthlyResetsAt)
	require.WithinDuration(t, customStart.Add(1000*time.Hour), *agg.EffectiveMonthlyResetsAt, time.Second)
}

func TestUserDisplayEffectiveResetTimeDoesNotTreatOneTimeDailyQuotaAsRecoverable(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
		CustomLimitHours: 1,
		CustomLimitUSD:   &limit,
	}
	dailyStart := now.Add(-30 * time.Minute)
	customStart := now.Add(-30 * time.Minute)
	sub := UserSubscription{
		ID:                1,
		UserID:            10,
		GroupID:           20,
		StartsAt:          now.Add(-30 * time.Minute),
		ExpiresAt:         now.Add(30 * time.Minute),
		Status:            SubscriptionStatusActive,
		DailyUsageUSD:     10,
		CustomUsageUSD:    10,
		DailyWindowStart:  &dailyStart,
		CustomWindowStart: &customStart,
		Group:             group,
	}

	agg := aggregateActiveSubscriptionsForUserDisplay([]UserSubscription{sub})

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 0, *agg.EffectiveAvailableUSD, 0.000001)
	require.Nil(t, agg.EffectiveResetsAt)
	require.Nil(t, agg.EffectiveDailyResetsAt)
}

func TestUserDisplayAggregateSumsCustomEffectiveCapacityAcrossCards(t *testing.T) {
	now := time.Now().UTC()
	customLimit := 20.0
	group := &Group{
		ID:               20,
		SubscriptionType: SubscriptionTypeSubscription,
		CustomLimitHours: 1,
		CustomLimitUSD:   &customLimit,
	}
	subs := []UserSubscription{
		{
			ID:                1,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-2 * time.Hour),
			ExpiresAt:         now.Add(24 * time.Hour),
			Status:            SubscriptionStatusActive,
			CustomUsageUSD:    5,
			CustomWindowStart: subscriptionTimePtr(now.Add(-30 * time.Minute)),
			Group:             group,
		},
		{
			ID:                2,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-time.Hour),
			ExpiresAt:         now.Add(24 * time.Hour),
			Status:            SubscriptionStatusActive,
			CustomUsageUSD:    20,
			CustomWindowStart: subscriptionTimePtr(now.Add(-10 * time.Minute)),
			Group:             group,
		},
	}

	agg := aggregateActiveSubscriptionsForUserDisplay(subs)

	require.NotNil(t, agg)
	require.True(t, agg.IsAggregate)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 15, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveCustomUsageUSD)
	require.InDelta(t, 25, *agg.EffectiveCustomUsageUSD, 0.000001)
	require.InDelta(t, 25, agg.CustomUsageUSD, 0.000001)
	require.NotNil(t, agg.EffectiveCustomLimitUSD(agg.Group))
	require.InDelta(t, 40, *agg.EffectiveCustomLimitUSD(agg.Group), 0.000001)
}

func TestUserDisplayAggregateDoesNotBorrowCapacityFromCardsWithoutWindow(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	dailyGroup := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	weeklyGroup := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, WeeklyLimitUSD: &limit}
	subs := []UserSubscription{
		{
			ID:               1,
			UserID:           10,
			GroupID:          20,
			StartsAt:         now.Add(-time.Hour),
			ExpiresAt:        now.Add(24 * time.Hour),
			Status:           SubscriptionStatusActive,
			DailyUsageUSD:    5,
			DailyWindowStart: subscriptionTimePtr(now.Add(-time.Hour)),
			Group:            dailyGroup,
		},
		{
			ID:                2,
			UserID:            10,
			GroupID:           20,
			StartsAt:          now.Add(-time.Hour),
			ExpiresAt:         now.Add(24 * time.Hour),
			Status:            SubscriptionStatusActive,
			WeeklyUsageUSD:    7,
			WeeklyWindowStart: subscriptionTimePtr(now.Add(-time.Hour)),
			Group:             weeklyGroup,
		},
	}

	agg := aggregateActiveSubscriptionsForUserDisplay(subs)

	require.NotNil(t, agg)
	require.NotNil(t, agg.EffectiveAvailableUSD)
	require.InDelta(t, 8, *agg.EffectiveAvailableUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyUsageUSD)
	require.NotNil(t, agg.EffectiveWeeklyUsageUSD)
	require.InDelta(t, 5, *agg.EffectiveDailyUsageUSD, 0.000001)
	require.InDelta(t, 7, *agg.EffectiveWeeklyUsageUSD, 0.000001)
	require.InDelta(t, 5, agg.DailyUsageUSD, 0.000001)
	require.InDelta(t, 7, agg.WeeklyUsageUSD, 0.000001)
	require.NotNil(t, agg.EffectiveDailyLimitUSD(agg.Group))
	require.NotNil(t, agg.EffectiveWeeklyLimitUSD(agg.Group))
	require.InDelta(t, 10, *agg.EffectiveDailyLimitUSD(agg.Group), 0.000001)
	require.InDelta(t, 10, *agg.EffectiveWeeklyLimitUSD(agg.Group), 0.000001)
}

func TestUserDisplayAggregateUnlimitedCardSuppressesFiniteWindowRows(t *testing.T) {
	now := time.Now().UTC()
	limit := 10.0
	limitedGroup := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	unlimitedGroup := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription}
	subs := []UserSubscription{
		{
			ID:               1,
			UserID:           10,
			GroupID:          20,
			StartsAt:         now.Add(-time.Hour),
			ExpiresAt:        now.Add(24 * time.Hour),
			Status:           SubscriptionStatusActive,
			DailyUsageUSD:    10,
			DailyWindowStart: subscriptionTimePtr(now.Add(-time.Hour)),
			Group:            limitedGroup,
		},
		{
			ID:        2,
			UserID:    10,
			GroupID:   20,
			StartsAt:  now.Add(-time.Hour),
			ExpiresAt: now.Add(24 * time.Hour),
			Status:    SubscriptionStatusActive,
			Group:     unlimitedGroup,
		},
	}

	agg := aggregateActiveSubscriptionsForUserDisplay(subs)

	require.NotNil(t, agg)
	require.Nil(t, agg.EffectiveAvailableUSD)
	require.Nil(t, agg.EffectiveResetsAt)
	require.Nil(t, agg.EffectiveDailyUsageUSD)
	require.Nil(t, agg.EffectiveDailyLimitUSD(agg.Group))
	require.NotNil(t, agg.Group)
	require.Nil(t, agg.Group.DailyLimitUSD)
}

func TestAdjustSubscriptionUpdatesSelectedUsageFields(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		ID:              10,
		UserID:          100,
		GroupID:         200,
		StartsAt:        now.Add(-time.Hour),
		ExpiresAt:       now.Add(24 * time.Hour),
		Status:          SubscriptionStatusActive,
		DailyUsageUSD:   1,
		WeeklyUsageUSD:  2,
		MonthlyUsageUSD: 3,
		CustomUsageUSD:  4,
	})
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	daily := 11.5
	custom := 99.9
	sub, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{
		DailyUsageUSD:  &daily,
		CustomUsageUSD: &custom,
	})

	require.NoError(t, err)
	require.InDelta(t, 11.5, sub.DailyUsageUSD, 0.000001)
	require.InDelta(t, 2, sub.WeeklyUsageUSD, 0.000001)
	require.InDelta(t, 3, sub.MonthlyUsageUSD, 0.000001)
	require.InDelta(t, 99.9, sub.CustomUsageUSD, 0.000001)
}

func TestAdjustSubscriptionRejectsNegativeUsage(t *testing.T) {
	repo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	negative := -0.1
	_, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{
		DailyUsageUSD: &negative,
	})

	require.Error(t, err)
}

func TestAdjustSubscriptionRejectsInvalidDays(t *testing.T) {
	repo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	zero := 0
	_, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{Days: &zero})
	require.Error(t, err)

	tooLarge := MaxValidityDays + 1
	_, err = svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{Days: &tooLarge})
	require.Error(t, err)

	tooSmall := -MaxValidityDays - 1
	_, err = svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{Days: &tooSmall})
	require.Error(t, err)
}

func TestAdjustSubscriptionRejectsExpiredReactivationWhenGroupMissing(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		ID:        10,
		UserID:    100,
		GroupID:   200,
		StartsAt:  now.Add(-48 * time.Hour),
		ExpiresAt: now.Add(-24 * time.Hour),
		Status:    SubscriptionStatusExpired,
	})
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{}, repo, nil, nil, nil)

	days := 1
	_, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{Days: &days})

	require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
	stored, getErr := repo.GetByID(context.Background(), 10)
	require.NoError(t, getErr)
	require.Equal(t, SubscriptionStatusExpired, stored.Status)
	require.True(t, stored.ExpiresAt.Before(now))
}

func TestAdjustSubscriptionRejectsExpiredReactivationWhenGroupInactive(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		ID:        10,
		UserID:    100,
		GroupID:   200,
		StartsAt:  now.Add(-48 * time.Hour),
		ExpiresAt: now.Add(-24 * time.Hour),
		Status:    SubscriptionStatusExpired,
	})
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{
		ID:               200,
		Status:           StatusDisabled,
		SubscriptionType: SubscriptionTypeSubscription,
	}}, repo, nil, nil, nil)

	days := 1
	_, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{Days: &days})

	require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
}

func TestAdjustSubscriptionRejectsExpiredReactivationWhenGroupNotSubscriptionType(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		ID:        10,
		UserID:    100,
		GroupID:   200,
		StartsAt:  now.Add(-48 * time.Hour),
		ExpiresAt: now.Add(-24 * time.Hour),
		Status:    SubscriptionStatusExpired,
	})
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{
		ID:               200,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeStandard,
	}}, repo, nil, nil, nil)

	days := 1
	_, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{Days: &days})

	require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
}

func TestAdjustSubscriptionRejectsOrphanHistoricalRecordUsageAdjustment(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		ID:              10,
		UserID:          100,
		GroupID:         200,
		StartsAt:        now.Add(-48 * time.Hour),
		ExpiresAt:       now.Add(-24 * time.Hour),
		Status:          SubscriptionStatusExpired,
		DailyUsageUSD:   1,
		WeeklyUsageUSD:  2,
		MonthlyUsageUSD: 3,
		CustomUsageUSD:  4,
	})
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{}, repo, nil, nil, nil)

	daily := 9.5
	_, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{DailyUsageUSD: &daily})

	require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
	stored, getErr := repo.GetByID(context.Background(), 10)
	require.NoError(t, getErr)
	require.InDelta(t, 1, stored.DailyUsageUSD, 0.000001)
	require.Equal(t, SubscriptionStatusExpired, stored.Status)
	require.True(t, stored.ExpiresAt.Before(now))
}

func TestExtendSubscriptionRejectsHistoricalWhenGroupInvalid(t *testing.T) {
	cases := []struct {
		name  string
		group *Group
	}{
		{name: "missing", group: nil},
		{name: "disabled", group: &Group{ID: 200, Status: StatusDisabled, SubscriptionType: SubscriptionTypeSubscription}},
		{name: "not subscription", group: &Group{ID: 200, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			repo := newSubscriptionUserSubRepoStub()
			repo.seed(&UserSubscription{
				ID:        10,
				UserID:    100,
				GroupID:   200,
				StartsAt:  now.Add(-48 * time.Hour),
				ExpiresAt: now.Add(-24 * time.Hour),
				Status:    SubscriptionStatusExpired,
			})
			svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: tc.group}, repo, nil, nil, nil)

			_, err := svc.ExtendSubscription(context.Background(), 10, 1)

			require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
			stored, getErr := repo.GetByID(context.Background(), 10)
			require.NoError(t, getErr)
			require.Equal(t, SubscriptionStatusExpired, stored.Status)
			require.True(t, stored.ExpiresAt.Before(now))
		})
	}
}

func TestAdjustSubscriptionAllowsActiveFutureWithoutGroupLookup(t *testing.T) {
	now := time.Now().UTC()
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		ID:        10,
		UserID:    100,
		GroupID:   200,
		StartsAt:  now.Add(-time.Hour),
		ExpiresAt: now.Add(24 * time.Hour),
		Status:    SubscriptionStatusActive,
	})
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	days := 1
	usage := 12.5
	sub, err := svc.AdjustSubscription(context.Background(), 10, AdjustSubscriptionInput{
		Days:          &days,
		DailyUsageUSD: &usage,
	})

	require.NoError(t, err)
	require.True(t, sub.ExpiresAt.After(now.Add(24*time.Hour)))
	require.InDelta(t, 12.5, sub.DailyUsageUSD, 0.000001)
}

func TestDoWindowMaintenanceSkipsAggregateVirtualSubscription(t *testing.T) {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)

	require.NotPanics(t, func() {
		svc.doWindowMaintenance(&UserSubscription{
			IsAggregate: true,
			UserID:      100,
			GroupID:     200,
		})
	})
}

func TestAdminSubscriptionHistoryIncludesSoftDeletedRevokedRecords(t *testing.T) {
	deletedAt := time.Now().UTC()
	repo := &subscriptionHistoryRepoStub{
		byID: &UserSubscription{
			ID:        10,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(time.Hour),
			DeletedAt: &deletedAt,
		},
		list: []UserSubscription{{
			ID:        10,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(time.Hour),
			DeletedAt: &deletedAt,
		}},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	sub, err := svc.GetByIDIncludeDeleted(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, SubscriptionStatusRevoked, sub.Status)

	subs, err := svc.ListUserSubscriptionRecords(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, SubscriptionStatusRevoked, subs[0].Status)
}

func subscriptionTimePtr(t time.Time) *time.Time {
	return &t
}

type activeSubscriptionListRepoStub struct {
	userSubRepoNoop

	calls int
	subs  []UserSubscription
}

type blockingActiveSubscriptionListRepoStub struct {
	userSubRepoNoop

	mu      sync.Mutex
	n       int
	subs    []UserSubscription
	started chan struct{}
	release chan struct{}
}

func (r *blockingActiveSubscriptionListRepoStub) ListActiveByUserIDAndGroupID(context.Context, int64, int64) ([]UserSubscription, error) {
	r.mu.Lock()
	r.n++
	shouldBlock := r.n == 1
	out := make([]UserSubscription, len(r.subs))
	copy(out, r.subs)
	r.mu.Unlock()

	if shouldBlock {
		close(r.started)
		<-r.release
	}
	return out, nil
}

func (r *blockingActiveSubscriptionListRepoStub) setUsage(usage float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs[0].DailyUsageUSD = usage
}

func (r *blockingActiveSubscriptionListRepoStub) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *activeSubscriptionListRepoStub) ListActiveByUserIDAndGroupID(context.Context, int64, int64) ([]UserSubscription, error) {
	r.calls++
	out := make([]UserSubscription, len(r.subs))
	copy(out, r.subs)
	return out, nil
}

type subscriptionHistoryRepoStub struct {
	userSubRepoNoop

	byID *UserSubscription
	list []UserSubscription
}

func (r *subscriptionHistoryRepoStub) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	if r.byID == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.byID
	return &cp, nil
}

func (r *subscriptionHistoryRepoStub) ListByUserIDIncludeDeleted(context.Context, int64) ([]UserSubscription, error) {
	out := make([]UserSubscription, len(r.list))
	copy(out, r.list)
	return out, nil
}
