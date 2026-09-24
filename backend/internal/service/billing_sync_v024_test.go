//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSyncV024ChannelContract(t *testing.T) {
	var pricing ChannelModelPricing
	require.NoError(t, json.Unmarshal([]byte(`{"cache_write_1h_price":0.2,"fast_multiplier":3,"intervals":[{"min_tokens":10,"input_multiplier":2}],"time_pricing":{"timezone":"UTC","periods":[{"start_time":"09:00","end_time":"10:00","multiplier":2}]}}`), &pricing))
	require.Equal(t, 0.2, *pricing.CacheWrite1hPrice)
	require.Equal(t, 2.0, *pricing.Intervals[0].InputMultiplier)
	clone := pricing.Clone()
	clone.TimePricing.Periods[0].Multiplier = 9
	require.Equal(t, 2.0, pricing.TimePricing.Periods[0].Multiplier)
	require.Error(t, ValidateIntervals([]PricingInterval{{CacheWrite1hPrice: testPtrFloat64(-1)}}, BillingModeToken))
	require.Error(t, ValidateIntervals([]PricingInterval{{InputMultiplier: testPtrFloat64(0)}}, BillingModeToken))
	for _, mode := range []BillingMode{BillingModeVideo, BillingModeDuration, BillingModeCharacter} {
		require.NoError(t, ValidateIntervals([]PricingInterval{{TierLabel: "a"}, {TierLabel: "b"}}, mode))
	}
}

func TestSyncV024IntervalTimeFastEffortContract(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformAnthropic, []ChannelModelPricing{{
		Platform: PlatformAnthropic, Models: []string{"custom-contract"}, BillingMode: BillingModeToken,
		InputPrice: testPtrFloat64(1), OutputPrice: testPtrFloat64(2), CacheWritePrice: testPtrFloat64(3), CacheWrite1hPrice: testPtrFloat64(4),
		FastMultiplier: testPtrFloat64(2), MaxReasoningEffortMultiplier: testPtrFloat64(3),
		Intervals:   []PricingInterval{{MinTokens: 10, MaxTokens: testPtrInt(20), InputMultiplier: testPtrFloat64(2), CacheWriteMultiplier: testPtrFloat64(2)}},
		TimePricing: &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "09:00", EndTime: "10:00", Multiplier: 2}}},
	}}, nil)
	group := &Group{ID: 100, Platform: PlatformAnthropic, LongContextPricingEnabled: true}
	for _, tc := range []struct {
		name   string
		tokens int
		want   float64
	}{{"boundary", 10, 10}, {"interval", 11, 22}, {"gap", 21, 21}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bs.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: "custom-contract", Group: group,
				Tokens: UsageTokens{InputTokens: tc.tokens}, RateMultiplier: 5, ServiceTier: "fast", ReasoningEffort: "max", Resolver: resolver,
				PricingAt: time.Date(2026, 9, 16, 9, 30, 0, 0, time.UTC)})
			require.NoError(t, err)
			require.InDelta(t, tc.want*2*2*3, got.TotalCost, 1e-9)
			require.InDelta(t, tc.want*2*2*3*5, got.ActualCost, 1e-9)
		})
	}
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "custom-contract", GroupID: &group.ID, Group: group})
	require.Equal(t, 8.0, resolver.GetIntervalPricing(resolved, 11).CacheCreation1hPrice)
	require.Equal(t, 4.0, resolver.GetIntervalPricing(resolved, 21).CacheCreation1hPrice)
}

func TestSyncV024UnmatchedGroupCardPreservesChannelAndUnits(t *testing.T) {
	for _, tc := range []struct {
		mode BillingMode
		want float64
	}{{BillingModeDuration, 6}, {BillingModeCharacter, 4}, {BillingModeVideo, 8}} {
		t.Run(string(tc.mode), func(t *testing.T) {
			bs, resolver := newTokenCostTestEnv(t, PlatformOpenAI, []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"custom-unit"}, BillingMode: tc.mode, PerRequestPrice: testPtrFloat64(2)}}, nil)
			group := &Group{ID: 100, ModelPricing: []ChannelModelPricing{{Models: []string{"unrelated-model"}, PerRequestPrice: testPtrFloat64(99)}}}
			resolved := resolver.Resolve(context.Background(), PricingInput{Model: "custom-unit", GroupID: &group.ID, Group: group})
			require.Equal(t, PricingSourceChannel, resolved.Source)
			got, err := bs.CalculateCostUnified(CostInput{Model: "custom-unit", Group: group, Resolved: resolved, Resolver: resolver, RateMultiplier: 3, DurationSeconds: 3, CharacterCount: 2000, RequestCount: 4})
			require.NoError(t, err)
			require.Equal(t, tc.want, got.TotalCost)
			require.Equal(t, tc.want*3, got.ActualCost)
		})
	}
}
