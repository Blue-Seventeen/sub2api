package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type closureSubscriptionCache struct {
	billingCacheWorkerStub
	get        func() (*SubscriptionCacheData, error)
	generation func() (int64, error)
	invalidate func(context.Context) error
	publish    func(context.Context) error
	cas        func(context.Context, int64, *SubscriptionCacheData) (bool, error)
	sets       atomic.Int64
	casSets    atomic.Int64
}

func (c *closureSubscriptionCache) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	if c.get != nil {
		return c.get()
	}
	return nil, errors.New("cache miss")
}

func (c *closureSubscriptionCache) GetSubscriptionCacheGeneration(context.Context, int64, int64) (int64, error) {
	if c.generation != nil {
		return c.generation()
	}
	return 0, nil
}

func (c *closureSubscriptionCache) SetSubscriptionCache(context.Context, int64, int64, *SubscriptionCacheData) error {
	c.sets.Add(1)
	return nil
}

func (c *closureSubscriptionCache) SetSubscriptionCacheIfGeneration(ctx context.Context, _, _ int64, generation int64, data *SubscriptionCacheData) (bool, error) {
	c.casSets.Add(1)
	if c.cas != nil {
		return c.cas(ctx, generation, data)
	}
	return true, nil
}

func (c *closureSubscriptionCache) InvalidateSubscriptionCache(ctx context.Context, _, _ int64) error {
	if c.invalidate != nil {
		return c.invalidate(ctx)
	}
	return nil
}

func (c *closureSubscriptionCache) PublishSubscriptionCacheInvalidation(ctx context.Context, _ string) error {
	if c.publish != nil {
		return c.publish(ctx)
	}
	return nil
}

func closureSubscriptionFixture() (*Group, []UserSubscription) {
	now := time.Now()
	limit := 10.0
	group := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	return group, []UserSubscription{{
		ID: 1, UserID: 1, GroupID: 2, Group: group, Status: SubscriptionStatusActive,
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now,
	}}
}

func TestBillingCacheClosureGenerationOutageUsesDBWithoutRefill(t *testing.T) {
	for _, when := range []struct {
		name string
		fail int
	}{{"before_load", 1}, {"after_load", 2}} {
		t.Run(when.name, func(t *testing.T) {
			group, subs := closureSubscriptionFixture()
			calls := 0
			cache := &closureSubscriptionCache{generation: func() (int64, error) {
				calls++
				if calls == when.fail {
					return 0, errors.New("generation unavailable")
				}
				return 7, nil
			}}
			repo := &billingCacheSubRepoStub{subs: subs}
			svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
			t.Cleanup(svc.Stop)
			err := svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, "")
			svc.Stop()
			require.NoError(t, err)
			require.Equal(t, 1, repo.calls)
			require.Zero(t, cache.sets.Load(), "outage must not trigger an unfenced fill")
			require.Zero(t, cache.casSets.Load(), "outage must not enqueue a fill")
		})
	}
}

func TestBillingCacheClosureSuppliedStackedCapacity(t *testing.T) {
	for _, available := range []float64{5, 0} {
		group, subs := closureSubscriptionFixture()
		sub := subs[0]
		sub.IsAggregate = true
		sub.SubscriptionCount = 2
		sub.DailyUsageUSD = 35
		limit := 20.0
		sub.DailyLimitUSDSnapshot = &limit
		sub.StackedAvailableUSD = &available
		svc := NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
		err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, group, &sub, "")
		svc.Stop()
		if available > 0 {
			require.NoError(t, err, "another card's overspend must not consume this card's capacity")
		} else {
			require.ErrorIs(t, err, ErrDailyLimitExceeded)
		}
	}
}

func TestBillingCacheClosureFreshLoadDropsExpiredCard(t *testing.T) {
	group, subs := closureSubscriptionFixture()
	expired := subs[0]
	expired.ID = 2
	expired.ExpiresAt = time.Now().Add(-time.Second)
	subs[0].DailyUsageUSD = 10
	repo := &billingCacheSubRepoStub{subs: append(subs, expired)}
	svc := NewBillingCacheService(nil, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	err := svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, "")
	require.ErrorIs(t, err, ErrDailyLimitExceeded, "an expired card cannot contribute capacity to a fresh result")
}

func TestBillingCacheClosureLoadRechecksDeadlineAfterGeneration(t *testing.T) {
	group, subs := closureSubscriptionFixture()
	// Only the first post-load generation lookup crosses this card's expiry.
	expired := subs[0]
	expired.ID = 2
	expired.ExpiresAt = time.Now().Add(150 * time.Millisecond)
	subs[0].DailyUsageUSD = 10
	calls := 0
	cache := &closureSubscriptionCache{generation: func() (int64, error) {
		calls++
		if calls == 2 {
			<-time.After(time.Until(expired.ExpiresAt))
		}
		return 0, nil
	}}
	repo := &billingCacheSubRepoStub{subs: append(subs, expired)}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	err := svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, "")
	require.ErrorIs(t, err, ErrDailyLimitExceeded)
	require.GreaterOrEqual(t, repo.calls, 2)
}

func TestBillingCacheClosureInvalidationRetriesAndBypassesDirtyCache(t *testing.T) {
	group, subs := closureSubscriptionFixture()
	subs[0].DailyUsageUSD = 10
	available := 10.0
	stale := &SubscriptionCacheData{
		Status: SubscriptionStatusActive, ExpiresAt: subs[0].ExpiresAt, RefreshAt: subs[0].ExpiresAt,
		StackedAvailableUSD: &available,
	}
	var attempts atomic.Int64
	var fail atomic.Bool
	fail.Store(true)
	cache := &closureSubscriptionCache{
		get: func() (*SubscriptionCacheData, error) { return stale, nil },
		invalidate: func(context.Context) error {
			attempts.Add(1)
			if fail.Load() {
				return errors.New("invalidate unavailable")
			}
			return nil
		},
	}
	repo := &billingCacheSubRepoStub{subs: subs}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	svc.QueueUpdateSubscriptionUsage(1, 2, 1)
	require.Greater(t, attempts.Load(), int64(1), "a transient failure must be retried")
	require.LessOrEqual(t, attempts.Load(), int64(3), "retries must be bounded")
	err := svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, "")
	require.ErrorIs(t, err, ErrDailyLimitExceeded, "dirty Redis hits must be ignored")
	// An already queued refill must also be suppressed while invalidation failed.
	svc.setSubscriptionCache(context.Background(), 1, 2, svc.convertFromPortsData(stale), 0, true, 0)
	require.Zero(t, cache.casSets.Load())
	require.Zero(t, cache.sets.Load())
	require.Eventually(t, func() bool {
		state := svc.subscriptionCacheState(1, 2)
		state.mu.Lock()
		defer state.mu.Unlock()
		return !state.repairQueued && !state.repairing
	}, time.Second, time.Millisecond)
	fail.Store(false)
	before := attempts.Load()
	err = svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, "")
	require.ErrorIs(t, err, ErrDailyLimitExceeded, "repairing a dirty key still returns canonical DB data")
	require.Eventually(t, func() bool { return attempts.Load() > before }, time.Second, time.Millisecond,
		"a later read must repair the failed invalidation")
}

func TestBillingCacheClosureInvalidationRetriesPublish(t *testing.T) {
	var publishes atomic.Int64
	cache := &closureSubscriptionCache{publish: func(context.Context) error {
		if publishes.Add(1) < 3 {
			return errors.New("publish unavailable")
		}
		return nil
	}}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	svc.QueueUpdateSubscriptionUsage(1, 2, 1)
	require.Equal(t, int64(3), publishes.Load(), "publication failure also needs bounded retry")
}

func TestBillingCacheClosureInvalidationDoesNotClearNewerCommit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int64
	var fail atomic.Bool
	cache := &closureSubscriptionCache{invalidate: func(context.Context) error {
		if attempts.Add(1) == 1 {
			close(started)
			<-release
			return nil
		}
		if fail.Load() {
			return errors.New("unavailable")
		}
		return nil
	}}
	group, subs := closureSubscriptionFixture()
	svc := NewBillingCacheService(cache, nil, &billingCacheSubRepoStub{subs: subs}, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	done := make(chan error, 1)
	go func() { done <- svc.UpdateSubscriptionUsage(context.Background(), 1, 2, 1) }()
	<-started
	require.Error(t, svc.UpdateSubscriptionUsage(context.Background(), 1, 2, 1))
	fail.Store(true)
	close(release)
	require.Error(t, <-done)
	_, dirty := svc.subscriptionCacheState(1, 2).snapshot()
	require.True(t, dirty, "a previous repair must not clear a later commit's barrier")
	require.NoError(t, svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, ""))
	svc.Stop()
	require.Zero(t, cache.casSets.Load())
}

func TestBillingCacheClosureLateRefillKeepsBarrierUntilRepair(t *testing.T) {
	group, subs := closureSubscriptionFixture()
	started, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int64
	var available atomic.Bool
	cache := &closureSubscriptionCache{
		invalidate: func(context.Context) error {
			attempts.Add(1)
			if !available.Load() {
				return errors.New("unavailable")
			}
			return nil
		},
		cas: func(context.Context, int64, *SubscriptionCacheData) (bool, error) {
			close(started)
			<-release
			return true, nil
		},
	}
	repo := &billingCacheSubRepoStub{subs: subs}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	// Release any blocked worker before Stop, even if an assertion fails.
	t.Cleanup(svc.Stop)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	require.NoError(t, svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, ""))
	<-started
	svc.QueueUpdateSubscriptionUsage(1, 2, 1)
	available.Store(true)
	require.NoError(t, svc.InvalidateSubscription(context.Background(), 1, 2))
	_, dirty := svc.subscriptionCacheState(1, 2).snapshot()
	require.True(t, dirty, "an in-flight pre-commit fill still exists")
	before := attempts.Load()
	close(release)
	svc.Stop()
	require.Greater(t, attempts.Load(), before, "late fill must be invalidated again")
	_, dirty = svc.subscriptionCacheState(1, 2).snapshot()
	require.False(t, dirty)
	beforeSets := cache.casSets.Load()
	data := &subscriptionCacheData{ExpiresAt: time.Now().Add(time.Hour)}
	svc.setSubscriptionCache(context.Background(), 1, 2, data, 0, true, 0)
	require.Equal(t, beforeSets, cache.casSets.Load(), "a queued old local version remains fenced after repair")
}

func TestBillingCacheClosureDirtySuppliedSnapshotUsesDB(t *testing.T) {
	group, subs := closureSubscriptionFixture()
	stale := subs[0]
	subs[0].DailyUsageUSD = 10
	cache := &closureSubscriptionCache{invalidate: func(context.Context) error { return errors.New("unavailable") }}
	svc := NewBillingCacheService(cache, nil, &billingCacheSubRepoStub{subs: subs}, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	svc.QueueUpdateSubscriptionUsage(1, 2, 1)
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, group, &stale, ""), ErrDailyLimitExceeded)
}

func TestBillingCacheClosureInvalidationPublishHasIndependentBudget(t *testing.T) {
	var publishes atomic.Int64
	var expiredPublishContext atomic.Bool
	cache := &closureSubscriptionCache{
		invalidate: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		publish: func(ctx context.Context) error {
			publishes.Add(1)
			expiredPublishContext.Store(ctx.Err() != nil)
			return nil
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	svc.QueueUpdateSubscriptionUsage(1, 2, 1)
	require.Equal(t, int64(3), publishes.Load())
	require.False(t, expiredPublishContext.Load())
}

func TestBillingCacheClosureNormalizationPreservesZeroExpiryAndCustomAnchor(t *testing.T) {
	now := time.Now()
	customStart := now.Add(-2 * time.Hour)
	limit := 10.0
	group := &Group{CustomLimitHours: 72, CustomLimitUSD: &limit}
	subs := []UserSubscription{{Group: group, CustomWindowStart: &customStart, CustomUsageUSD: 7}}
	normalized := normalizeSubscriptionsForCacheAt(subs, now)
	require.Len(t, normalized, 1)
	require.Equal(t, customStart, *normalized[0].CustomWindowStart)
	require.Equal(t, 7.0, normalized[0].CustomUsageUSD)
	require.True(t, normalized[0].ExpiresAt.IsZero())
	require.Equal(t, 7.0, subs[0].CustomUsageUSD)
}

func TestBillingCacheClosureDirtyRepairDoesNotConsumeDBDeadline(t *testing.T) {
	group, subs := closureSubscriptionFixture()
	release := make(chan struct{})
	cache := &closureSubscriptionCache{invalidate: func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}}
	repo := &billingCacheSubRepoStub{subs: subs}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, svc.InvalidateSubscription(canceled, 1, 2))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := svc.CheckBillingEligibilityFreshSubscription(ctx, &User{ID: 1}, nil, group, "")
	close(release)
	svc.Stop()
	require.NoError(t, err, "retrying Redis must not spend the healthy DB fallback's deadline")
	require.Equal(t, 1, repo.calls)
	require.Zero(t, cache.casSets.Load())
	require.Zero(t, cache.sets.Load())
}

func TestBillingCacheClosureStaleRepairDoesNotConsumeDBDeadline(t *testing.T) {
	for _, kind := range []string{"expired", "schema_stale"} {
		t.Run(kind, func(t *testing.T) {
			group, subs := closureSubscriptionFixture()
			stale := &SubscriptionCacheData{
				Status: SubscriptionStatusActive, ExpiresAt: subs[0].ExpiresAt,
			}
			if kind == "expired" {
				stale.RefreshAt = time.Now().Add(-time.Second)
			}
			var attempts, localInvalidations atomic.Int64
			cache := &closureSubscriptionCache{
				get: func() (*SubscriptionCacheData, error) { return stale, nil },
				invalidate: func(ctx context.Context) error {
					switch attempts.Add(1) {
					case 1:
						return errors.New("transient invalidation failure")
					case 2:
						timer := time.NewTimer(150 * time.Millisecond)
						defer timer.Stop()
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-timer.C:
							return context.DeadlineExceeded
						}
					default:
						return nil
					}
				},
			}
			repo := &billingCacheSubRepoStub{subs: subs}
			svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
			t.Cleanup(svc.Stop)
			svc.SetSubscriptionL1Invalidator(func(int64, int64) { localInvalidations.Add(1) })
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err := svc.CheckBillingEligibilityFreshSubscription(ctx, &User{ID: 1}, nil, group, "")
			requestErr := ctx.Err()
			svc.Stop()
			require.NoError(t, err, "stale-cache repair must not consume the DB fallback deadline")
			require.NoError(t, requestErr)
			require.Equal(t, 1, repo.calls)
			require.Equal(t, int64(1), localInvalidations.Load())
			require.Equal(t, int64(3), attempts.Load(), "repair must still run on the worker")
			require.Zero(t, cache.casSets.Load())
			require.Zero(t, cache.sets.Load())
		})
	}
}

func TestBillingCacheClosureDirtyRepairsCoalesce(t *testing.T) {
	group, subs := closureSubscriptionFixture()
	started, release := make(chan struct{}), make(chan struct{})
	var attempts atomic.Int64
	cache := &closureSubscriptionCache{invalidate: func(context.Context) error {
		if attempts.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	}}
	svc := NewBillingCacheService(cache, nil, &billingCacheSubRepoStub{subs: subs}, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, svc.InvalidateSubscription(canceled, 1, 2))
	for i := 0; i < 20; i++ {
		require.NoError(t, svc.CheckBillingEligibilityFreshSubscription(context.Background(), &User{ID: 1}, nil, group, ""))
	}
	<-started
	require.Equal(t, int64(1), attempts.Load())
	require.Empty(t, svc.cacheWriteChan, "coalesced repair must not fill the worker queue")
	close(release)
	svc.Stop()
	_, dirty := svc.subscriptionCacheState(1, 2).snapshot()
	require.False(t, dirty)
	require.Zero(t, cache.casSets.Load())
}
