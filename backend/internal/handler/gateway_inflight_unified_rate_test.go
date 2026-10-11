//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReserveInflightBalance_ZeroChargeUnifiedRateBypassesStrictPricingAndOverlap(t *testing.T) {
	for _, balance := range []float64{0, -1} {
		for _, priced := range []bool{false, true} {
			cache := newHandlerInflightCache(balance)
			cache.res["existing-paid-request"] = 0.9
			cfg := &config.Config{}
			cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, FailClosedOnUnpriced: true, TTLSeconds: 60}
			billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			key := &service.APIKey{User: &service.User{ID: 1, Balance: balance, UnifiedRateEnabled: true, UnifiedRateMultiplier: 0}}
			est := &countingEstimator{cost: 10, priced: priced}
			for _, kind := range []service.InflightEstimateKind{service.InflightEstimateToken, service.InflightEstimateImage, service.InflightEstimatePerRequest, service.InflightEstimateVideo, service.InflightEstimateAudio} {
				ctx := context.WithValue(context.Background(), struct{}{}, "retained")
				newCtx, done, err := reserveInflightBalanceCtx(ctx, billing, est, key, nil, service.InflightEstimateRequest{Model: "unknown", Kind: kind, AudioUnits: 1})
				require.NoError(t, err)
				require.Same(t, ctx, newCtx)
				require.Nil(t, service.InflightReservationFromContext(newCtx))
				t.Cleanup(done)
			}
			c := newInflightTestGinContext()
			done, err := reserveInflightBalance(c, billing, est, key, nil, tokenInflightEstimate("unknown", nil))
			require.NoError(t, err)
			done()
			require.Zero(t, est.calls, "free requests must bypass estimation and strict unpriced rejection")
			require.Equal(t, map[string]float64{"existing-paid-request": 0.9}, cache.res)
		}
	}
}

func TestReserveInflightBalance_NonFreeUnifiedRateStillRejectsUnpriced(t *testing.T) {
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, FailClosedOnUnpriced: true}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	for _, user := range []*service.User{
		{ID: 1, UnifiedRateEnabled: false, UnifiedRateMultiplier: 0},
		{ID: 1, UnifiedRateEnabled: true, UnifiedRateMultiplier: -1},
		{ID: 1, UnifiedRateEnabled: true, UnifiedRateMultiplier: 1},
	} {
		_, done, err := reserveInflightBalanceCtx(context.Background(), billing, &countingEstimator{}, &service.APIKey{User: user}, nil, tokenInflightEstimate("unknown", nil))
		require.ErrorIs(t, err, service.ErrInsufficientBalance)
		done()
	}
}
