//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayServiceRecordUsage_AggregateSubscriptionHasNoVirtualForeignKey(t *testing.T) {
	billingErr := errors.New("subscription transaction failed")
	for _, tc := range []struct {
		name       string
		simpleMode bool
		free       bool
		billingErr error
	}{
		{name: "no committed card returned"},
		{name: "simple mode", simpleMode: true},
		{name: "free subscription", free: true},
		{name: "failed billing", billingErr: billingErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{err: tc.billingErr}
			svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
			if tc.simpleMode {
				svc.cfg.RunMode = config.RunModeSimple
			}
			group := &Group{ID: 91, Platform: PlatformGemini, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1}
			if tc.free {
				group.RateMultiplier = 0
			}
			err := svc.RecordUsage(context.Background(), &RecordUsageInput{
				Result:       &ForwardResult{RequestID: "aggregate-no-card", Model: "gemini-3.8-flash", CacheStorageTokenHours: 10000},
				APIKey:       &APIKey{ID: 821, GroupID: &group.ID, Group: group},
				User:         &User{ID: 621},
				Account:      &Account{ID: 713, Platform: PlatformGemini},
				Subscription: &UserSubscription{ID: 0, UserID: 621, GroupID: group.ID, IsAggregate: true},
			})
			if tc.billingErr != nil {
				require.ErrorIs(t, err, tc.billingErr)
				require.Equal(t, "aggregate-no-card:billing_failed", logs.lastLog.RequestID)
				require.Zero(t, logs.lastLog.ActualCost)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, logs.lastLog)
			require.Nil(t, logs.lastLog.SubscriptionID, "virtual aggregate IDs must never reach a usage foreign key")
			if tc.simpleMode {
				require.Zero(t, billing.calls)
			}
		})
	}
}

func TestGatewayServiceRecordUsage_SubscriptionLogUsesCommittedCard(t *testing.T) {
	for _, tc := range []struct {
		name         string
		subscription UserSubscription
	}{
		{name: "aggregate across cards", subscription: UserSubscription{ID: 0, IsAggregate: true}},
		{name: "single-card snapshot", subscription: UserSubscription{ID: 77}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cardID := int64(61)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true, SubscriptionID: &cardID}}
			svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, billing, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
			user := &User{ID: 621, UnifiedRateEnabled: true, UnifiedRateMultiplier: 0.25}
			group := &Group{ID: 91, Platform: PlatformGemini, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 2, Hydrated: true,
				PeakRateEnabled: true, PeakRateWindows: []PeakRateWindow{{Start: "00:00", End: "12:00", Multiplier: 3}, {Start: "12:00", End: "23:59", Multiplier: 4}}}
			apiKey := &APIKey{ID: 821, GroupID: &group.ID, Group: group, User: user}
			tc.subscription.UserID, tc.subscription.GroupID = user.ID, group.ID
			originalID := tc.subscription.ID
			input := &RecordUsageInput{
				Result: &ForwardResult{RequestID: "committed-subscription-card", Model: "gemini-3.8-flash", Usage: ClaudeUsage{InputTokens: 10000}, CacheStorageTokenHours: 10000},
				APIKey: apiKey, User: user, Account: &Account{ID: 713, Platform: PlatformGemini}, Subscription: &tc.subscription,
				PricingAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
			}
			require.InDelta(t, 0.025, svc.QuoteUsageCost(context.Background(), input), 1e-12)
			require.NoError(t, svc.RecordUsage(context.Background(), input))
			require.NotNil(t, logs.lastLog)
			require.Equal(t, &cardID, logs.lastLog.SubscriptionID, "attribute usage to the committed card instead of the preflight snapshot")
			require.Equal(t, originalID, tc.subscription.ID, "do not mutate the aggregate or cached preflight subscription")
			require.Equal(t, BillingTypeSubscription, logs.lastLog.BillingType)
			require.InDelta(t, 0.0125, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, 0.025, logs.lastLog.ActualCost, 1e-12)
			require.InDelta(t, 0.1, logs.lastLog.RealActualCost, 1e-12)
			require.InDelta(t, 0.025, billing.lastCmd.SubscriptionCost, 1e-12)
			require.Zero(t, billing.lastCmd.BalanceCost)
			require.Nil(t, billing.lastCmd.SubscriptionID, "the repository must still select and split across current active cards")
			require.Equal(t, user.ID, billing.lastCmd.SubscriptionUserID)
			require.Equal(t, group.ID, billing.lastCmd.SubscriptionGroupID)
		})
	}
}

func TestApplyUsageBillingSubscriptionAttributionRequiresCommittedID(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *UsageBillingApplyResult
		err    error
	}{
		{name: "deduplicated", result: &UsageBillingApplyResult{Applied: false, SubscriptionID: i64p(61)}},
		{name: "zero ID", result: &UsageBillingApplyResult{Applied: true, SubscriptionID: i64p(0)}},
		{name: "negative ID", result: &UsageBillingApplyResult{Applied: true, SubscriptionID: i64p(-1)}},
		{name: "no card", result: &UsageBillingApplyResult{Applied: true}},
		{name: "transaction failure", err: errors.New("rollback")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &UsageLog{}
			repo := &openAIRecordUsageBillingRepoStub{result: tc.result, err: tc.err}
			_, err := applyUsageBilling(context.Background(), "subscription-invalid-result", logs, &postUsageBillingParams{
				Cost: &CostBreakdown{TotalCost: 0.0125, ActualCost: 0.025, RealActualCost: 0.1},
				User: &User{ID: 621}, APIKey: &APIKey{ID: 821, GroupID: i64p(91)}, Account: &Account{ID: 713},
				Subscription: &UserSubscription{ID: 0, UserID: 621, GroupID: 91, IsAggregate: true}, IsSubscriptionBill: true,
			}, &billingDeps{billingCacheService: &BillingCacheService{}, deferredService: &DeferredService{}}, repo)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, logs.SubscriptionID, "uncommitted or invalid card IDs must not become log attribution")
		})
	}
}
