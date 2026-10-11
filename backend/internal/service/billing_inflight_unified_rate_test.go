//go:build unit

package service

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHasZeroChargeUnifiedRate(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *User
		want bool
	}{
		{name: "nil"},
		{name: "disabled_zero", user: &User{UnifiedRateMultiplier: 0}},
		{name: "enabled_zero", user: &User{UnifiedRateEnabled: true, UnifiedRateMultiplier: 0}, want: true},
		{name: "negative", user: &User{UnifiedRateEnabled: true, UnifiedRateMultiplier: -1}},
		{name: "positive", user: &User{UnifiedRateEnabled: true, UnifiedRateMultiplier: 0.5}},
		{name: "nan", user: &User{UnifiedRateEnabled: true, UnifiedRateMultiplier: math.NaN()}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, HasZeroChargeUnifiedRate(tc.user)) })
	}
}

func TestInflightEstimate_ZeroChargeUnifiedRateAllKinds(t *testing.T) {
	groupID := int64(7)
	price := 0.5
	key := &APIKey{
		User:    &User{ID: 1, UnifiedRateEnabled: true, UnifiedRateMultiplier: 0},
		GroupID: &groupID,
		Group: &Group{ID: groupID, RateMultiplier: 2, ModelPricing: []ChannelModelPricing{
			{Models: []string{"priced-model"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price},
		}},
	}
	gw := newInflightEstimateGateway(t, nil)
	openai := &OpenAIGatewayService{cfg: gw.cfg, billingService: gw.billingService, resolver: gw.resolver}
	for name, estimate := range map[string]func(context.Context, *APIKey, InflightEstimateRequest) (float64, bool){
		"gateway": gw.EstimateInflightReservation,
		"openai":  openai.EstimateInflightReservation,
	} {
		t.Run(name, func(t *testing.T) {
			for _, model := range []string{"priced-model", "totally-unknown-model-xyz"} {
				for _, kind := range []InflightEstimateKind{InflightEstimateToken, InflightEstimateImage, InflightEstimatePerRequest, InflightEstimateVideo, InflightEstimateAudio} {
					t.Run(model+"/"+[]string{"token", "image", "per_request", "video", "audio"}[kind], func(t *testing.T) {
						cost, priced := estimate(context.Background(), key, InflightEstimateRequest{Model: model, Kind: kind, Units: 2, MaxTokens: 1000, BodyBytes: 4000, SearchCalls: 1, VideoDurationSeconds: 10, AudioMode: "realtime", AudioUnits: 1})
						require.True(t, priced, "explicitly free usage is priced at zero, even for unknown models")
						require.Zero(t, cost)
					})
				}
			}
		})
	}
}

func TestInflightEstimate_ZeroChargeUnifiedRateSkipsModelAndRateLookups(t *testing.T) {
	calls := 0
	deps := inflightEstimateDeps{
		resolveMapping:      func(context.Context, int64, string) ChannelMappingResult { calls++; return ChannelMappingResult{} },
		userGroupRate:       func(context.Context, int64, int64, float64) float64 { calls++; return 1 },
		accountMappedModels: func(context.Context, *APIKey, string) []string { calls++; return nil },
	}
	groupID := int64(7)
	key := &APIKey{User: &User{ID: 1, UnifiedRateEnabled: true}, GroupID: &groupID, Group: &Group{ID: groupID, RateMultiplier: 1}}
	cost, priced := deps.estimate(context.Background(), key, InflightEstimateRequest{Model: "unknown"})
	require.True(t, priced)
	require.Zero(t, cost)
	require.Zero(t, calls, "free estimation must not access mapping, rate, or account dependencies")
}

type observedUnifiedInflightCache struct {
	*memInflightCache
	reads    atomic.Int32
	reserves atomic.Int32
}

func (c *observedUnifiedInflightCache) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	c.reads.Add(1)
	return c.memInflightCache.GetUserBalance(ctx, userID)
}

func (c *observedUnifiedInflightCache) ReserveInflightBalance(ctx context.Context, userID int64, id string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	c.reserves.Add(1)
	return c.memInflightCache.ReserveInflightBalance(ctx, userID, id, amount, balance, ttl)
}

func TestReserveInflight_ZeroChargeUnifiedRatePositiveEstimateDoesNotTouchCache(t *testing.T) {
	for _, balance := range []float64{0, -1} {
		cache := &observedUnifiedInflightCache{memInflightCache: newMemInflightCache(balance)}
		expires := time.Now().Add(time.Hour)
		cache.res["existing-paid-request"] = 0.9
		cache.exp["existing-paid-request"] = expires
		svc := newInflightSvc(t, cache, 1)
		user := &User{ID: 1, Balance: balance, UnifiedRateEnabled: true, UnifiedRateMultiplier: 0}
		// Leave the first request active while admitting the second; a positive
		// estimate must not create a handle or reject either free request.
		for i := 0; i < 2; i++ {
			res, err := svc.ReserveInflight(context.Background(), user, nil, nil, 10)
			require.NoError(t, err)
			if res != nil {
				t.Cleanup(res.HandlerDone)
			}
			require.Nil(t, res)
		}
		release, err := svc.ReserveInflightBalance(context.Background(), user, nil, nil, 10)
		require.NoError(t, err)
		release()
		require.Zero(t, cache.reads.Load())
		require.Zero(t, cache.reserves.Load())
		require.Zero(t, cache.renews.Load())
		require.Zero(t, cache.releaseCt.Load())
		require.Equal(t, map[string]float64{"existing-paid-request": 0.9}, cache.res)
		require.Equal(t, expires, cache.exp["existing-paid-request"])
	}
}

func TestInflightUnifiedRate_NonFreeUsersPreservePaidBehavior(t *testing.T) {
	for _, user := range []*User{
		{ID: 1, UnifiedRateEnabled: false, UnifiedRateMultiplier: 0},
		{ID: 1, UnifiedRateEnabled: true, UnifiedRateMultiplier: -1},
		{ID: 1, UnifiedRateEnabled: true, UnifiedRateMultiplier: 2},
	} {
		gw := newInflightEstimateGateway(t, nil)
		cost, priced := gw.EstimateInflightReservation(context.Background(), &APIKey{User: user}, InflightEstimateRequest{Model: "claude-sonnet-4-5", MaxTokens: 1000})
		require.True(t, priced)
		require.InDelta(t, 0.015, cost, 1e-12)
		_, priced = gw.EstimateInflightReservation(context.Background(), &APIKey{User: user}, InflightEstimateRequest{Model: "totally-unknown-model-xyz"})
		require.False(t, priced)
		cache := newMemInflightCache(1)
		svc := newInflightSvc(t, cache, 60)
		res, err := svc.ReserveInflight(context.Background(), user, nil, nil, 0.9)
		require.NoError(t, err)
		require.NotNil(t, res)
		t.Cleanup(res.HandlerDone)
		_, err = svc.ReserveInflight(context.Background(), user, nil, nil, 0.9)
		require.ErrorIs(t, err, ErrInsufficientBalance)
	}
}
