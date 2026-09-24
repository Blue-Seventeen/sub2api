package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayRecordUsage_DeepSeekDefaultPricingAt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		model     string
		pricingAt time.Time
		want      float64
	}{
		{"flash_off_peak", "deepseek-v4-flash", time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), 0.000557},
		{"flash_peak", "deepseek-v4-flash", time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), 0.001114},
		{"flash_weekend", "deepseek-v4-flash", time.Date(2026, 8, 29, 2, 0, 0, 0, time.UTC), 0.000557},
		{"pro_peak", "deepseek-v4-pro", time.Date(2026, 8, 24, 6, 30, 0, 0, time.UTC), 0.003344},
		{"alias_peak", "deepseek-chat", time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), 0.001114},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, logs, billing, input := newGatewayDeepSeekPricingCase(t)
			input.Result.Model = tc.model
			input.Result.UpstreamModel = tc.model
			input.PricingAt = tc.pricingAt

			require.NoError(t, svc.RecordUsage(context.Background(), input))
			require.NotNil(t, logs.lastLog)
			require.InDelta(t, tc.want, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tc.want, logs.lastLog.ActualCost, 1e-12)
			require.NotNil(t, billing.lastCmd)
			require.InDelta(t, tc.want, billing.lastCmd.SubscriptionCost, 1e-12)
			require.InDelta(t, tc.want, billing.lastCmd.APIKeyQuotaCost, 1e-12)
		})
	}
}

func TestGatewayRecordUsage_DeepSeekPeakAppliesUserMultipliersOnce(t *testing.T) {
	svc, logs, billing, input := newGatewayDeepSeekPricingCase(t)
	input.APIKey.Group.RateMultiplier = 1.5
	input.APIKey.Group.PeakRateEnabled = true
	input.APIKey.Group.PeakStart = "00:00"
	input.APIKey.Group.PeakEnd = "23:59"
	input.APIKey.Group.PeakRateMultiplier = 3
	input.User.UnifiedRateEnabled = true
	input.User.UnifiedRateMultiplier = 2

	require.NoError(t, svc.RecordUsage(context.Background(), input))
	require.NotNil(t, logs.lastLog)
	require.InDelta(t, 0.001114, logs.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 0.010026, logs.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 0.005013, logs.lastLog.RealActualCost, 1e-12)
	require.NotNil(t, billing.lastCmd)
	require.InDelta(t, 0.010026, billing.lastCmd.SubscriptionCost, 1e-12)
	require.InDelta(t, 0.010026, billing.lastCmd.APIKeyQuotaCost, 1e-12)
}

func TestGatewayRecordUsage_DeepSeekPreservesCustomAndFreePricing(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		channelCard           bool
		groupCard             bool
		inputPrice            float64
		outputPrice           float64
		cacheReadPrice        float64
		groupRate             float64
		unifiedRate           float64
		peakRate              float64
		wantTotal, wantActual float64
	}{
		{name: "custom_channel", channelCard: true, inputPrice: 1e-6, outputPrice: 2e-6, cacheReadPrice: 0.5e-6, groupRate: 1, unifiedRate: 1, peakRate: 1, wantTotal: 0.0025, wantActual: 0.0025},
		{name: "free_channel", channelCard: true, groupRate: 1, unifiedRate: 1, peakRate: 1},
		{name: "custom_group", groupCard: true, inputPrice: 1e-6, outputPrice: 2e-6, cacheReadPrice: 0.5e-6, groupRate: 1, unifiedRate: 1, peakRate: 1, wantTotal: 0.0025, wantActual: 0.0025},
		{name: "free_group_card", groupCard: true, groupRate: 1, unifiedRate: 1, peakRate: 1},
		{name: "free_group_rate", groupRate: 0, unifiedRate: 1, peakRate: 1, wantTotal: 0.001114},
		{name: "free_unified_rate", groupRate: 1, unifiedRate: 0, peakRate: 1, wantTotal: 0.001114},
		{name: "free_peak_rate", groupRate: 1, unifiedRate: 1, peakRate: 0, wantTotal: 0.001114},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, logs, billing, input := newGatewayDeepSeekPricingCase(t)
			group := input.APIKey.Group
			group.RateMultiplier = tc.groupRate
			group.PeakRateEnabled, group.PeakStart, group.PeakEnd, group.PeakRateMultiplier = true, "00:00", "23:59", tc.peakRate
			input.User.UnifiedRateEnabled, input.User.UnifiedRateMultiplier = true, tc.unifiedRate
			card := ChannelModelPricing{
				Models: []string{input.Result.Model}, BillingMode: BillingModeToken,
				InputPrice: &tc.inputPrice, OutputPrice: &tc.outputPrice, CacheReadPrice: &tc.cacheReadPrice,
			}
			if tc.channelCard {
				cache := newEmptyChannelCache()
				cache.pricingByGroupModel[channelModelKey{groupID: group.ID, model: input.Result.Model}] = &card
				cache.channelByGroupID[group.ID] = &Channel{ID: group.ID, Status: StatusActive}
				cache.groupPlatform[group.ID] = ""
				cache.loadedAt = time.Now()
				channels := &ChannelService{}
				channels.cache.Store(cache)
				svc.resolver = NewModelPricingResolver(channels, svc.billingService)
			}
			if tc.groupCard {
				group.ModelPricing = []ChannelModelPricing{card}
			}

			require.NoError(t, svc.RecordUsage(context.Background(), input))
			require.NotNil(t, logs.lastLog)
			require.InDelta(t, tc.wantTotal, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tc.wantActual, logs.lastLog.ActualCost, 1e-12)
			require.NotNil(t, billing.lastCmd)
			require.InDelta(t, tc.wantActual, billing.lastCmd.SubscriptionCost, 1e-12)
			require.InDelta(t, tc.wantActual, billing.lastCmd.APIKeyQuotaCost, 1e-12)
		})
	}
}

func TestGatewayRecordUsageWithLongContext_DeepSeekPreservesExplicitThreshold(t *testing.T) {
	for _, tc := range []struct {
		name       string
		threshold  int
		customCard bool
		groupCard  bool
		freeCard   bool
		wantTotal  float64
		wantActual float64
	}{
		{name: "default_input_overflow", threshold: 1500, wantTotal: 0.001114, wantActual: 0.001334},
		{name: "default_cache_overflow", threshold: 500, wantTotal: 0.001114, wantActual: 0.001561},
		{name: "default_at_threshold", threshold: 2000, wantTotal: 0.001114, wantActual: 0.001114},
		{name: "custom_channel_no_extra_context", threshold: 1500, customCard: true, wantTotal: 0.0025, wantActual: 0.0025},
		{name: "custom_group_no_extra_context", threshold: 1500, customCard: true, groupCard: true, wantTotal: 0.0025, wantActual: 0.0025},
		{name: "free_channel", threshold: 1500, customCard: true, freeCard: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, logs, billing, input := newGatewayDeepSeekPricingCase(t)
			if tc.customCard {
				inputPrice, outputPrice, cacheReadPrice := 1e-6, 2e-6, 0.5e-6
				if tc.freeCard {
					inputPrice, outputPrice, cacheReadPrice = 0, 0, 0
				}
				card := ChannelModelPricing{
					Models: []string{input.Result.Model}, BillingMode: BillingModeToken,
					InputPrice: &inputPrice, OutputPrice: &outputPrice, CacheReadPrice: &cacheReadPrice,
				}
				if tc.groupCard {
					input.APIKey.Group.ModelPricing = []ChannelModelPricing{card}
				} else {
					setGatewayPricingTestChannel(svc, input, &card)
				}
			}

			require.NoError(t, svc.RecordUsageWithLongContext(context.Background(), &RecordUsageLongContextInput{
				Result: input.Result, APIKey: input.APIKey, User: input.User, Account: input.Account,
				Subscription: input.Subscription, PricingAt: input.PricingAt, APIKeyService: input.APIKeyService,
				LongContextThreshold: tc.threshold, LongContextMultiplier: 2,
			}))
			require.NotNil(t, logs.lastLog)
			require.InDelta(t, tc.wantTotal, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tc.wantActual, logs.lastLog.ActualCost, 1e-12)
			require.InDelta(t, tc.wantActual, logs.lastLog.RealActualCost, 1e-12)
			require.NotNil(t, billing.lastCmd)
			require.InDelta(t, tc.wantActual, billing.lastCmd.SubscriptionCost, 1e-12)
			require.InDelta(t, tc.wantActual, billing.lastCmd.APIKeyQuotaCost, 1e-12)
		})
	}
}

func TestGatewayRecordUsage_ChannelTokenPricingUsesRequestTime(t *testing.T) {
	for _, tc := range []struct {
		name string
		hour int
		want float64
	}{
		{"scheduled", 2, 0.0075},
		{"outside_schedule", 12, 0.0025},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, logs, billing, input := newGatewayDeepSeekPricingCase(t)
			input.Result.Model, input.Result.UpstreamModel = "custom-token-model", "custom-token-model"
			input.PricingAt = time.Date(2026, 8, 24, tc.hour, 0, 0, 0, time.UTC)
			inputPrice, outputPrice, cacheReadPrice := 1e-6, 2e-6, 0.5e-6
			setGatewayPricingTestChannel(svc, input, &ChannelModelPricing{
				BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice, CacheReadPrice: &cacheReadPrice,
				TimePricing: &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "01:00", EndTime: "04:00", Multiplier: 3}}},
			})

			require.NoError(t, svc.RecordUsage(context.Background(), input))
			require.NotNil(t, logs.lastLog)
			require.InDelta(t, tc.want, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, tc.want, logs.lastLog.ActualCost, 1e-12)
			require.NotNil(t, billing.lastCmd)
			require.InDelta(t, tc.want, billing.lastCmd.SubscriptionCost, 1e-12)
			require.InDelta(t, tc.want, billing.lastCmd.APIKeyQuotaCost, 1e-12)
		})
	}
}

func setGatewayPricingTestChannel(svc *GatewayService, input *RecordUsageInput, card *ChannelModelPricing) {
	groupID := input.APIKey.Group.ID
	cache := newEmptyChannelCache()
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: input.Result.Model}] = card
	cache.channelByGroupID[groupID] = &Channel{ID: groupID, Status: StatusActive}
	cache.groupPlatform[groupID] = ""
	cache.loadedAt = time.Now()
	channels := &ChannelService{}
	channels.cache.Store(cache)
	svc.resolver = NewModelPricingResolver(channels, svc.billingService)
}

func newGatewayDeepSeekPricingCase(t *testing.T) (*GatewayService, *openAIRecordUsageLogRepoStub, *openAIRecordUsageBillingRepoStub, *RecordUsageInput) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	billingService := NewBillingService(cfg, NewPricingService(cfg, nil))
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	billing := &openAIRecordUsageBillingRepoStub{}
	svc := &GatewayService{
		cfg:                 cfg,
		billingService:      billingService,
		resolver:            NewModelPricingResolver(nil, billingService),
		usageLogRepo:        logs,
		usageBillingRepo:    billing,
		billingCacheService: &BillingCacheService{},
		deferredService:     &DeferredService{},
	}
	group := &Group{ID: 4, Platform: PlatformDeepseek, RateMultiplier: 1, SubscriptionType: SubscriptionTypeSubscription}
	input := &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "deepseek-pricing", Model: "deepseek-v4-flash", UpstreamModel: "deepseek-v4-flash",
			Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 500, CacheReadInputTokens: 1000},
		},
		APIKey:        &APIKey{ID: 2, GroupID: &group.ID, Group: group, Quota: 100},
		User:          &User{ID: 1},
		Account:       &Account{ID: 3, Platform: PlatformDeepseek},
		Subscription:  &UserSubscription{ID: 5, UserID: 1, GroupID: 4},
		PricingAt:     time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC),
		APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{},
	}
	return svc, logs, billing, input
}
