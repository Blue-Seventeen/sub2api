package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type billingCacheWorkerStub struct {
	balanceUpdates                  int64
	subscriptionUpdates             int64
	subscriptionInvalidations       int64
	publishedInvalidationCacheKey   string
	subscriptionInvalidationHandler func(cacheKey string)
}

type billingCacheUserGroupRateRepoStub struct {
	UserGroupRateRepository
	rate *float64
}

type billingSubscriptionCacheStub struct {
	billingCacheWorkerStub
	data *SubscriptionCacheData
}

func (b *billingSubscriptionCacheStub) GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error) {
	return b.data, nil
}

type billingCacheSubRepoStub struct {
	UserSubscriptionRepository
	calls int
	subs  []UserSubscription
}

type generationBillingCacheStub struct {
	billingCacheWorkerStub
	mu         sync.Mutex
	generation int64
	sets       []int64
	setStarted chan struct{}
	setRelease chan struct{}
	setDone    chan bool
}

func (c *generationBillingCacheStub) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return nil, errors.New("cache miss")
}

func (c *generationBillingCacheStub) GetSubscriptionCacheGeneration(context.Context, int64, int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation, nil
}

func (c *generationBillingCacheStub) InvalidateSubscriptionCache(context.Context, int64, int64) error {
	c.mu.Lock()
	c.generation++
	c.mu.Unlock()
	return nil
}

func (c *generationBillingCacheStub) SetSubscriptionCacheIfGeneration(_ context.Context, _ int64, _ int64, generation int64, _ *SubscriptionCacheData) (bool, error) {
	if c.setStarted != nil {
		select {
		case c.setStarted <- struct{}{}:
		default:
		}
	}
	if c.setRelease != nil {
		<-c.setRelease
	}
	c.mu.Lock()
	c.sets = append(c.sets, generation)
	written := generation == c.generation
	c.mu.Unlock()
	if c.setDone != nil {
		c.setDone <- written
	}
	return written, nil
}

func (c *generationBillingCacheStub) setGenerations() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.sets...)
}

type blockingSubscriptionStatusRepo struct {
	billingCacheSubRepoStub
	firstStarted chan struct{}
	firstRelease chan struct{}
	mu           sync.Mutex
	loads        int
}

func (r *blockingSubscriptionStatusRepo) ListActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]UserSubscription, error) {
	r.mu.Lock()
	r.loads++
	load := r.loads
	subs := append([]UserSubscription(nil), r.subs...)
	r.mu.Unlock()
	if load == 1 {
		close(r.firstStarted)
		select {
		case <-r.firstRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return subs, nil
}

func (r *blockingSubscriptionStatusRepo) loadCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loads
}

func (r *billingCacheSubRepoStub) ListActiveByUserIDAndGroupID(context.Context, int64, int64) ([]UserSubscription, error) {
	r.calls++
	out := make([]UserSubscription, len(r.subs))
	copy(out, r.subs)
	return out, nil
}

type billingRateLimitResetRepoStub struct {
	APIKeyRepository
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
}

func (s *billingRateLimitResetRepoStub) ResetRateLimitWindows(ctx context.Context, id int64) error {
	s.calls.Add(1)
	select {
	case s.started <- struct{}{}:
	default:
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *billingCacheUserGroupRateRepoStub) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	return s.rate, nil
}

func (b *billingCacheWorkerStub) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	return 0, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetUserBalance(ctx context.Context, userID int64, balance float64) error {
	atomic.AddInt64(&b.balanceUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) DeductUserBalance(ctx context.Context, userID int64, amount float64) error {
	atomic.AddInt64(&b.balanceUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	return nil
}

func (b *billingCacheWorkerStub) GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error) {
	return nil, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetSubscriptionCache(ctx context.Context, userID, groupID int64, data *SubscriptionCacheData) error {
	atomic.AddInt64(&b.subscriptionUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, cost float64) error {
	atomic.AddInt64(&b.subscriptionUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) InvalidateSubscriptionCache(ctx context.Context, userID, groupID int64) error {
	atomic.AddInt64(&b.subscriptionInvalidations, 1)
	return nil
}

func (b *billingCacheWorkerStub) PublishSubscriptionCacheInvalidation(ctx context.Context, cacheKey string) error {
	b.publishedInvalidationCacheKey = cacheKey
	if b.subscriptionInvalidationHandler != nil {
		b.subscriptionInvalidationHandler(cacheKey)
	}
	return nil
}

func (b *billingCacheWorkerStub) SubscribeSubscriptionCacheInvalidation(ctx context.Context, handler func(cacheKey string)) error {
	b.subscriptionInvalidationHandler = handler
	return nil
}

func (b *billingCacheWorkerStub) GetAPIKeyRateLimit(ctx context.Context, keyID int64) (*APIKeyRateLimitCacheData, error) {
	return nil, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetAPIKeyRateLimit(ctx context.Context, keyID int64, data *APIKeyRateLimitCacheData) error {
	return nil
}

func (b *billingCacheWorkerStub) UpdateAPIKeyRateLimitUsage(ctx context.Context, keyID int64, cost float64) error {
	return nil
}

func (b *billingCacheWorkerStub) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	return nil
}

func (b *billingCacheWorkerStub) GetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func (b *billingCacheWorkerStub) SetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string, entry *UserPlatformQuotaCacheEntry, ttl time.Duration) error {
	return nil
}

func (b *billingCacheWorkerStub) DeleteUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) error {
	return nil
}

func (b *billingCacheWorkerStub) IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration, markDirty bool) error {
	return nil
}

func (b *billingCacheWorkerStub) PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]UserPlatformQuotaKey, error) {
	return nil, nil
}

func (b *billingCacheWorkerStub) ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []UserPlatformQuotaKey) error {
	return nil
}

func (b *billingCacheWorkerStub) BatchGetUserPlatformQuotaCache(ctx context.Context, keys []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error) {
	return nil, nil
}

func TestBillingCacheServiceQueueHighLoad(t *testing.T) {
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	start := time.Now()
	for i := 0; i < cacheWriteBufferSize*2; i++ {
		svc.QueueDeductBalance(1, 1)
	}
	require.Less(t, time.Since(start), 2*time.Second)

	svc.QueueUpdateSubscriptionUsage(1, 2, 1.5)

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&cache.balanceUpdates) > 0
	}, 2*time.Second, 10*time.Millisecond)

	require.Equal(t, int64(1), atomic.LoadInt64(&cache.subscriptionInvalidations))
}

func TestBillingCacheServiceRefillRetriesAfterSecondInvalidation(t *testing.T) {
	now := time.Now().UTC()
	group := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription}
	repo := &blockingSubscriptionStatusRepo{
		billingCacheSubRepoStub: billingCacheSubRepoStub{subs: []UserSubscription{{
			ID: 1, UserID: 1, GroupID: 2, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Status: SubscriptionStatusActive, Group: group, DailyUsageUSD: 1,
		}}},
		firstStarted: make(chan struct{}),
		firstRelease: make(chan struct{}),
	}
	cache := &generationBillingCacheStub{
		setStarted: make(chan struct{}, 1),
		setRelease: make(chan struct{}),
		setDone:    make(chan bool, 1),
	}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	resultCh := make(chan *subscriptionCacheData, 1)
	errCh := make(chan error, 1)
	go func() {
		data, err := svc.GetSubscriptionStatus(context.Background(), 1, 2)
		resultCh <- data
		errCh <- err
	}()
	<-repo.firstStarted
	err := svc.InvalidateSubscription(context.Background(), 1, 2)
	require.NoError(t, err)
	close(repo.firstRelease)

	require.NoError(t, <-errCh)
	require.NotNil(t, <-resultCh)
	require.GreaterOrEqual(t, repo.loadCount(), 2)
	<-cache.setStarted
	require.NoError(t, svc.InvalidateSubscription(context.Background(), 1, 2))
	close(cache.setRelease)
	require.False(t, <-cache.setDone)
	sets := cache.setGenerations()
	require.NotEmpty(t, sets)
	require.NotContains(t, sets, int64(0))
	require.NotContains(t, sets, int64(2))
}

func TestBillingCacheServiceEnqueueAfterStopReturnsFalse(t *testing.T) {
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	svc.Stop()

	enqueued := svc.enqueueCacheWrite(cacheWriteTask{
		kind:   cacheWriteDeductBalance,
		userID: 1,
		amount: 1,
	})
	require.False(t, enqueued)
}

func TestBillingCacheServiceCheckBillingEligibility_FreeGroupSkipsBalanceCheck(t *testing.T) {
	svc := NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		&Group{ID: 2, RateMultiplier: 0},
		nil,
		"",
	)
	require.NoError(t, err)
}

func TestBillingCacheServiceCheckBillingEligibility_FreeUserRateSkipsBalanceCheck(t *testing.T) {
	svc := NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{}, nil)
	svc.SetUserGroupRateRepository(&billingCacheUserGroupRateRepoStub{rate: func() *float64 {
		v := 0.0
		return &v
	}()})
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		&Group{ID: 2, RateMultiplier: 1.5},
		nil,
		"",
	)
	require.NoError(t, err)
}

func TestBillingCacheServiceCheckBillingEligibility_AllowsStaleCustomCacheWhenWindowExpired(t *testing.T) {
	now := time.Now()
	customLimit := 100.0
	customStart := now.Add(-73 * time.Hour)
	cache := &billingSubscriptionCacheStub{
		data: &SubscriptionCacheData{
			Status:      SubscriptionStatusActive,
			ExpiresAt:   now.Add(24 * time.Hour),
			CustomUsage: 100,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		&Group{
			ID:               2,
			SubscriptionType: SubscriptionTypeSubscription,
			CustomLimitHours: 72,
			CustomLimitUSD:   &customLimit,
		},
		&UserSubscription{
			Status:            SubscriptionStatusActive,
			ExpiresAt:         now.Add(24 * time.Hour),
			CustomWindowStart: &customStart,
			CustomUsageUSD:    100,
		},
		"",
	)
	require.NoError(t, err)
}

func TestBillingCacheServiceCheckBillingEligibility_BlocksActiveCustomLimit(t *testing.T) {
	now := time.Now()
	customLimit := 100.0
	customStart := now.Add(-2 * time.Hour)
	cache := &billingSubscriptionCacheStub{
		data: &SubscriptionCacheData{
			Status:      SubscriptionStatusActive,
			ExpiresAt:   now.Add(24 * time.Hour),
			CustomUsage: 100,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		&Group{
			ID:               2,
			SubscriptionType: SubscriptionTypeSubscription,
			CustomLimitHours: 72,
			CustomLimitUSD:   &customLimit,
		},
		&UserSubscription{
			Status:            SubscriptionStatusActive,
			ExpiresAt:         now.Add(24 * time.Hour),
			CustomWindowStart: &customStart,
			CustomUsageUSD:    100,
		},
		"",
	)
	require.ErrorIs(t, err, ErrCustomLimitExceeded)
}

func TestBillingCacheServiceCheckBillingEligibility_UsesStackedSubscriptionCacheLimits(t *testing.T) {
	now := time.Now()
	groupDailyLimit := 100.0
	stackedDailyLimit := 200.0
	stackedAvailable := 100.0
	cache := &billingSubscriptionCacheStub{
		data: &SubscriptionCacheData{
			Status:              SubscriptionStatusActive,
			ExpiresAt:           now.Add(24 * time.Hour),
			RefreshAt:           now.Add(24 * time.Hour),
			DailyUsage:          100,
			DailyLimitUSD:       &stackedDailyLimit,
			DailyWindowStart:    &now,
			StackedAvailableUSD: &stackedAvailable,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		&Group{
			ID:               2,
			SubscriptionType: SubscriptionTypeSubscription,
			DailyLimitUSD:    &groupDailyLimit,
		},
		nil,
		"",
	)
	require.NoError(t, err)
}

func TestBillingCacheServiceFreshSubscriptionCheckUsesCurrentCacheInsteadOfSnapshot(t *testing.T) {
	now := time.Now()
	limit := 100.0
	cache := &billingSubscriptionCacheStub{
		data: &SubscriptionCacheData{
			Status:           SubscriptionStatusActive,
			ExpiresAt:        now.Add(24 * time.Hour),
			RefreshAt:        now.Add(24 * time.Hour),
			DailyUsage:       100,
			DailyLimitUSD:    &limit,
			DailyWindowStart: &now,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibilityFreshSubscription(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		&Group{
			ID:               2,
			SubscriptionType: SubscriptionTypeSubscription,
			DailyLimitUSD:    &limit,
		},
		"",
	)

	require.ErrorIs(t, err, ErrDailyLimitExceeded)
}

func TestBillingCacheServiceFreshSubscriptionCheckAllowsCurrentCacheWhenSnapshotWouldBlock(t *testing.T) {
	now := time.Now()
	limit := 100.0
	cache := &billingSubscriptionCacheStub{
		data: &SubscriptionCacheData{
			Status:           SubscriptionStatusActive,
			ExpiresAt:        now.Add(24 * time.Hour),
			RefreshAt:        now.Add(24 * time.Hour),
			DailyUsage:       0,
			DailyLimitUSD:    &limit,
			DailyWindowStart: &now,
		},
	}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibilityFreshSubscription(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		&Group{
			ID:               2,
			SubscriptionType: SubscriptionTypeSubscription,
			DailyLimitUSD:    &limit,
		},
		"",
	)

	require.NoError(t, err)
}

func TestBillingCacheServiceFreshSubscriptionCheckTreatsLegacyCacheWithoutWindowStartAsStale(t *testing.T) {
	now := time.Now()
	limit := 100.0
	windowStart := now.Add(-25 * time.Hour)
	group := &Group{
		ID:               2,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
	}
	cache := &billingSubscriptionCacheStub{
		data: &SubscriptionCacheData{
			Status:        SubscriptionStatusActive,
			ExpiresAt:     now.Add(24 * time.Hour),
			DailyUsage:    limit,
			DailyLimitUSD: &limit,
		},
	}
	repo := &billingCacheSubRepoStub{
		subs: []UserSubscription{
			{
				ID:               10,
				UserID:           1,
				GroupID:          group.ID,
				Group:            group,
				Status:           SubscriptionStatusActive,
				StartsAt:         now.Add(-48 * time.Hour),
				ExpiresAt:        now.Add(48 * time.Hour),
				DailyWindowStart: &windowStart,
				DailyUsageUSD:    limit,
			},
		},
	}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibilityFreshSubscription(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		group,
		"",
	)

	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
}

func TestBillingCacheServiceFreshSubscriptionCheckNormalizesStackedDBWindows(t *testing.T) {
	now := time.Now()
	limit := 100.0
	windowStart := now.Add(-25 * time.Hour)
	group := &Group{
		ID:               2,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
	}
	repo := &billingCacheSubRepoStub{
		subs: []UserSubscription{
			{
				ID:               10,
				UserID:           1,
				GroupID:          group.ID,
				Group:            group,
				Status:           SubscriptionStatusActive,
				StartsAt:         now.Add(-48 * time.Hour),
				ExpiresAt:        now.Add(48 * time.Hour),
				DailyWindowStart: &windowStart,
				DailyUsageUSD:    limit,
			},
		},
	}
	svc := NewBillingCacheService(nil, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibilityFreshSubscription(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		group,
		"",
	)

	require.NoError(t, err)
}

func TestBillingCacheServiceSubscriptionCacheUsesCurrentGroupLimits(t *testing.T) {
	now := time.Now()
	snapshotLimit := 100.0
	currentLimit := 50.0
	group := &Group{
		ID:               2,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &currentLimit,
	}
	repo := &billingCacheSubRepoStub{
		subs: []UserSubscription{
			{
				ID:                    10,
				UserID:                1,
				GroupID:               group.ID,
				Group:                 group,
				Status:                SubscriptionStatusActive,
				StartsAt:              now.Add(-time.Hour),
				ExpiresAt:             now.Add(time.Hour),
				DailyLimitUSDSnapshot: &snapshotLimit,
			},
		},
	}
	svc := NewBillingCacheService(nil, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	data, err := svc.getSubscriptionFromDB(context.Background(), 1, group.ID)

	require.NoError(t, err)
	require.NotNil(t, data.DailyLimitUSD)
	require.InDelta(t, currentLimit, *data.DailyLimitUSD, 0.000001)
	require.NotNil(t, data.StackedAvailableUSD)
	require.InDelta(t, currentLimit, *data.StackedAvailableUSD, 0.000001)
}

func TestBillingCacheServiceFreshSubscriptionCheckRefreshesExpiredStackedCacheWindow(t *testing.T) {
	now := time.Now()
	limit := 100.0
	windowStart := now.Add(-25 * time.Hour)
	stackedAvailable := 0.0
	group := &Group{
		ID:               2,
		SubscriptionType: SubscriptionTypeSubscription,
		DailyLimitUSD:    &limit,
	}
	cache := &billingSubscriptionCacheStub{
		data: &SubscriptionCacheData{
			Status:              SubscriptionStatusActive,
			ExpiresAt:           now.Add(48 * time.Hour),
			DailyUsage:          limit,
			DailyLimitUSD:       &limit,
			StackedAvailableUSD: &stackedAvailable,
			DailyWindowStart:    &windowStart,
		},
	}
	repo := &billingCacheSubRepoStub{
		subs: []UserSubscription{
			{
				ID:               10,
				UserID:           1,
				GroupID:          group.ID,
				Group:            group,
				Status:           SubscriptionStatusActive,
				StartsAt:         now.Add(-48 * time.Hour),
				ExpiresAt:        now.Add(48 * time.Hour),
				DailyWindowStart: &windowStart,
				DailyUsageUSD:    limit,
			},
		},
	}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibilityFreshSubscription(
		context.Background(),
		&User{ID: 1},
		&APIKey{},
		group,
		"",
	)

	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
}

func TestBillingCacheServiceQueueSubscriptionUsageFallsBackWhenWorkerUnavailable(t *testing.T) {
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	svc.Stop()

	svc.QueueUpdateSubscriptionUsage(1, 2, 1.5)

	require.Equal(t, int64(1), atomic.LoadInt64(&cache.subscriptionInvalidations))
}

func TestBillingCacheServiceRateLimitReset_DeduplicatesConcurrentExpiredWindow(t *testing.T) {
	repo := &billingRateLimitResetRepoStub{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	svc := NewBillingCacheService(&billingCacheWorkerStub{}, nil, nil, repo, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	expiredWindow := time.Now().Add(-RateLimitWindow5h - time.Minute)
	apiKey := &APIKey{
		ID:          123,
		RateLimit5h: 100,
	}

	const goroutines = 32
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errCh <- svc.evaluateRateLimits(context.Background(), apiKey, 99, 0, 0, &expiredWindow, nil, nil)
		}()
	}
	close(start)

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("reset did not start")
	}

	require.Eventually(t, func() bool {
		return repo.calls.Load() == 1
	}, time.Second, 10*time.Millisecond)
	close(repo.release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), repo.calls.Load())
}
