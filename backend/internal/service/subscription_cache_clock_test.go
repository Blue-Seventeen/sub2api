//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionCacheSnapshotAndDeadlineUseOneClock(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		name := "singleflight"
		if fresh {
			name = "fresh"
		}
		t.Run(name, func(t *testing.T) {
			// Keep the real clock before the reset while the service clock has
			// crossed it; both the aggregate and its deadline must use the latter.
			boundary := time.Now().Add(12 * time.Hour).Truncate(time.Second)
			anchor := boundary.Add(-24 * time.Hour)
			now := boundary.Add(time.Second)
			limit := 10.0
			group := &Group{ID: 20, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
			repo := &activeSubscriptionListRepoStub{subs: []UserSubscription{{
				ID: 1, UserID: 10, GroupID: 20, Status: SubscriptionStatusActive,
				StartsAt: anchor, ExpiresAt: boundary.Add(72 * time.Hour),
				DailyWindowStart: &anchor, DailyUsageUSD: 10, Group: group,
			}}}
			svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
			t.Cleanup(svc.Stop)
			svc.now = func() time.Time { return now }
			var sub *UserSubscription
			var err error
			if fresh {
				sub, err = svc.loadActiveSubscriptionFresh(context.Background(), 10, 20, subCacheKey(10, 20))
			} else {
				sub, err = svc.GetActiveSubscription(context.Background(), 10, 20)
			}
			require.NoError(t, err)
			require.Zero(t, sub.DailyUsageUSD, "a future cache deadline must not hide pre-reset exhaustion")
			require.Equal(t, boundary, *sub.DailyWindowStart)
			require.Equal(t, 10.0, repo.subs[0].DailyUsageUSD, "normalization must not mutate repository data")
		})
	}
}
