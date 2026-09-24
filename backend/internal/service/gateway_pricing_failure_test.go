package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayRecordUsage_UnpricedTokensRejectSettlement(t *testing.T) {
	for _, searchCount := range []int{0, 2} {
		t.Run(map[bool]string{true: "with_search", false: "tokens_only"}[searchCount > 0], func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{}
			svc := &GatewayService{
				billingService:   NewBillingService(&config.Config{}, nil),
				usageLogRepo:     logs,
				usageBillingRepo: billing,
				deferredService:  &DeferredService{},
			}
			err := svc.RecordUsage(context.Background(), &RecordUsageInput{
				Result: &ForwardResult{
					RequestID: "unpriced-gateway", Model: "unknown-unpriced-model",
					Usage:       ClaudeUsage{InputTokens: 20, OutputTokens: 10},
					SearchCount: searchCount, Duration: time.Second,
				},
				APIKey: &APIKey{ID: 2}, User: &User{ID: 1}, Account: &Account{ID: 3},
			})
			require.ErrorContains(t, err, "pricing")
			require.Zero(t, billing.calls, "pricing failure must not claim the billing dedup key")
			require.Equal(t, 1, logs.calls)
			require.Equal(t, "unpriced-gateway:billing_failed", logs.lastLog.RequestID)
			require.Zero(t, logs.lastLog.ActualCost)
		})
	}
}

func TestOpenAIRecordUsage_UnpricedTokensRejectSettlement(t *testing.T) {
	for _, searchCount := range []int{0, 2} {
		t.Run(map[bool]string{true: "with_search", false: "tokens_only"}[searchCount > 0], func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing,
				&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "unpriced-openai", Model: "unknown-unpriced-model",
					Usage:       OpenAIUsage{InputTokens: 20, OutputTokens: 10},
					SearchCount: searchCount, Duration: time.Second,
				},
				APIKey: &APIKey{ID: 2}, User: &User{ID: 1}, Account: &Account{ID: 3},
			})
			require.ErrorContains(t, err, "pricing")
			require.Zero(t, billing.calls)
			require.Equal(t, 1, logs.calls)
			require.Equal(t, "unpriced-openai:billing_failed", logs.lastLog.RequestID)
			require.Zero(t, logs.lastLog.ActualCost)
		})
	}
}

func TestGatewayPricing_ExplicitFreeRemainsValid(t *testing.T) {
	groupID := int64(88)
	for _, tc := range []struct {
		name       string
		price      float64
		multiplier float64
	}{
		{name: "zero_channel_price", price: 0, multiplier: 1},
		{name: "zero_multiplier", price: 0.01, multiplier: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &GatewayService{
				billingService: NewBillingService(&config.Config{}, nil),
				resolver:       newOpenAITokenChannelPricingResolverForTest(t, groupID, "custom-free-model", tc.price, tc.price, tc.price),
			}
			cost, err := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
				Model: "custom-free-model", Usage: ClaudeUsage{InputTokens: 100, OutputTokens: 50},
			}, &APIKey{GroupID: &groupID}, "custom-free-model", tc.multiplier, 1, nil)
			require.NoError(t, err)
			require.NotNil(t, cost)
			require.Zero(t, cost.ActualCost)
		})
	}
}

func TestGatewayPricing_UnpricedRequestUnitsFail(t *testing.T) {
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	cost, err := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
		Model: "custom-unpriced-task", TaskCount: 1,
	}, &APIKey{}, "custom-unpriced-task", 1, 1, nil)
	require.ErrorContains(t, err, "pricing")
	require.Nil(t, cost)
}

func TestOpenAIRecordUsage_UnpricedUnitsRejectSettlement(t *testing.T) {
	for _, unit := range []string{"request", "task"} {
		t.Run(unit, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing,
				&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			result := &OpenAIForwardResult{RequestID: "unpriced-unit", Model: "custom-unpriced-task"}
			if unit == "request" {
				result.RequestCount = 1
			} else {
				result.TaskCount = 1
			}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: result, APIKey: &APIKey{ID: 2}, User: &User{ID: 1}, Account: &Account{ID: 3},
			})
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Zero(t, billing.calls)
			require.Equal(t, 1, logs.calls)
			require.Equal(t, "unpriced-unit:billing_failed", logs.lastLog.RequestID)
		})
	}
}

func TestOpenAIRecordUsage_SearchUsesFinalMultiplier(t *testing.T) {
	for _, tc := range []struct {
		name          string
		unified, peak float64
	}{
		{"unified_and_peak", 2, 3},
		{"free_unified", 0, 3},
		{"free_peak", 2, 0},
	} {
		for _, webSearch := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{true: "/web_search", false: "/search"}[webSearch], func(t *testing.T) {
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				billing := &openAIRecordUsageBillingRepoStub{}
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing,
					&openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				price := 10.0
				group := &Group{ID: 4, Platform: PlatformOpenAI, RateMultiplier: 1.5,
					SubscriptionType: SubscriptionTypeSubscription, SearchPricePer1k: &price,
					PeakRateEnabled: true, PeakStart: "00:00", PeakEnd: "23:59", PeakRateMultiplier: tc.peak}
				result := &OpenAIForwardResult{RequestID: "search-rates", Model: "gpt-5.1", SearchCount: 100}
				if webSearch {
					result.SearchCount, result.WebSearchCalls = 0, 100
				}
				err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
					Result: result, APIKey: &APIKey{ID: 2, GroupID: &group.ID, Group: group, Quota: 100},
					User:    &User{ID: 1, UnifiedRateEnabled: true, UnifiedRateMultiplier: tc.unified},
					Account: &Account{ID: 3}, Subscription: &UserSubscription{ID: 5, UserID: 1, GroupID: 4},
					PricingAt: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), APIKeyService: &openAIRecordUsageAPIKeyQuotaStub{},
				})
				require.NoError(t, err)
				require.NotNil(t, logs.lastLog)
				want := 1.5 * tc.unified * tc.peak
				require.InDelta(t, want, logs.lastLog.ActualCost, 1e-9)
				require.NotNil(t, billing.lastCmd)
				require.InDelta(t, want, billing.lastCmd.SubscriptionCost, 1e-9)
				require.InDelta(t, want, billing.lastCmd.APIKeyQuotaCost, 1e-9)
			})
		}
	}
}
