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
	balanceUpdates      int64
	subscriptionUpdates int64
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

func (b *billingCacheWorkerStub) IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration) error {
	return nil
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

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&cache.subscriptionUpdates) > 0
	}, 2*time.Second, 10*time.Millisecond)
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
