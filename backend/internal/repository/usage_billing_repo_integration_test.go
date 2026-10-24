//go:build integration

package repository

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryApply_DeduplicatesBalanceBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-" + uuid.NewString(),
		Name:   "billing",
		Quota:  1,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:           requestID,
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		AccountID:           account.ID,
		AccountType:         service.AccountTypeAPIKey,
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result1)
	require.True(t, result1.Applied)
	require.True(t, result1.APIKeyQuotaExhausted)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, result2)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used FROM api_keys WHERE id = $1", apiKey.ID).Scan(&quotaUsed))
	require.InDelta(t, 1.25, quotaUsed, 0.000001)

	var usage5h float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT usage_5h FROM api_keys WHERE id = $1", apiKey.ID).Scan(&usage5h))
	require.InDelta(t, 1.25, usage5h, 0.000001)

	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT status FROM api_keys WHERE id = $1", apiKey.ID).Scan(&status))
	require.Equal(t, service.StatusAPIKeyQuotaExhausted, status)

	var dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryApply_DeduplicatesSubscriptionBilling(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-sub-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-sub-" + uuid.NewString(),
		Name:    "billing-sub",
	})
	subscription := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:  user.ID,
		GroupID: group.ID,
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:        requestID,
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        0,
		SubscriptionID:   &subscription.ID,
		SubscriptionCost: 2.5,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var dailyUsage, customUsage float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd, custom_usage_usd FROM user_subscriptions WHERE id = $1", subscription.ID).Scan(&dailyUsage, &customUsage))
	require.InDelta(t, 2.5, dailyUsage, 0.000001)
	require.InDelta(t, 2.5, customUsage, 0.000001)
}

func TestUsageBillingRepositoryApply_StackedSubscriptionChargesEarliestAvailableCard(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	dailyLimit := 100.0
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-stacked-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-stacked-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
		DailyLimitUSD:    &dailyLimit,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-stacked-" + uuid.NewString(),
		Name:    "billing-stacked",
	})

	now := time.Now().UTC()
	exhausted := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:        user.ID,
		GroupID:       group.ID,
		StartsAt:      now.Add(-2 * time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		DailyUsageUSD: dailyLimit,
	})
	available := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:        user.ID,
		GroupID:       group.ID,
		StartsAt:      now.Add(-1 * time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		DailyUsageUSD: 10,
	})

	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:           uuid.NewString(),
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		SubscriptionUserID:  user.ID,
		SubscriptionGroupID: group.ID,
		SubscriptionCost:    15,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.SubscriptionID)
	require.Equal(t, available.ID, *result.SubscriptionID)

	var exhaustedDaily, availableDaily float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", exhausted.ID).Scan(&exhaustedDaily))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", available.ID).Scan(&availableDaily))
	require.InDelta(t, dailyLimit, exhaustedDaily, 0.000001)
	require.InDelta(t, 25, availableDaily, 0.000001)
}

func TestUsageBillingRepositoryApply_StackedSubscriptionConcurrentChargesDoNotDoubleSpend(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	dailyLimit := 10.0
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-stacked-concurrent-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-stacked-concurrent-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
		DailyLimitUSD:    &dailyLimit,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-stacked-concurrent-" + uuid.NewString(),
		Name:    "billing-stacked-concurrent",
	})

	now := time.Now().UTC()
	first := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:    user.ID,
		GroupID:   group.ID,
		StartsAt:  now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(24 * time.Hour),
	})
	second := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:    user.ID,
		GroupID:   group.ID,
		StartsAt:  now.Add(-1 * time.Hour),
		ExpiresAt: now.Add(24 * time.Hour),
	})

	const requests = 15
	start := make(chan struct{})
	errs := make(chan error, requests)
	chargedIDs := make(chan int64, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			result, err := repo.Apply(ctx, &service.UsageBillingCommand{
				RequestID:           fmt.Sprintf("%s-%d", uuid.NewString(), i),
				APIKeyID:            apiKey.ID,
				UserID:              user.ID,
				SubscriptionUserID:  user.ID,
				SubscriptionGroupID: group.ID,
				SubscriptionCost:    1,
			})
			if err != nil {
				errs <- err
				return
			}
			if result == nil || !result.Applied || result.SubscriptionID == nil {
				errs <- fmt.Errorf("unexpected billing result: %+v", result)
				return
			}
			chargedIDs <- *result.SubscriptionID
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	close(chargedIDs)

	for err := range errs {
		require.NoError(t, err)
	}

	chargedByCard := map[int64]int{}
	for id := range chargedIDs {
		chargedByCard[id]++
	}
	require.Equal(t, 10, chargedByCard[first.ID])
	require.Equal(t, 5, chargedByCard[second.ID])

	var firstDaily, secondDaily float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", first.ID).Scan(&firstDaily))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", second.ID).Scan(&secondDaily))
	require.InDelta(t, dailyLimit, firstDaily, 0.000001)
	require.InDelta(t, 5, secondDaily, 0.000001)
}

func TestUsageBillingRepositoryApply_StackedSubscriptionExpiredWindowReEnablesEarliestCard(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	dailyLimit := 10.0
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-stacked-reset-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-stacked-reset-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
		DailyLimitUSD:    &dailyLimit,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-stacked-reset-" + uuid.NewString(),
		Name:    "billing-stacked-reset",
	})

	now := time.Now().UTC()
	expiredDailyWindow := now.Add(-25 * time.Hour)
	first := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:           user.ID,
		GroupID:          group.ID,
		StartsAt:         now.Add(-48 * time.Hour),
		ExpiresAt:        now.Add(24 * time.Hour),
		DailyUsageUSD:    dailyLimit,
		DailyWindowStart: &expiredDailyWindow,
	})
	second := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:        user.ID,
		GroupID:       group.ID,
		StartsAt:      now.Add(-1 * time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		DailyUsageUSD: 0,
	})

	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:           uuid.NewString(),
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		SubscriptionUserID:  user.ID,
		SubscriptionGroupID: group.ID,
		SubscriptionCost:    3,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.SubscriptionID)
	require.Equal(t, first.ID, *result.SubscriptionID)

	var firstDaily, secondDaily float64
	var firstDailyWindow time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd, daily_window_start FROM user_subscriptions WHERE id = $1", first.ID).Scan(&firstDaily, &firstDailyWindow))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", second.ID).Scan(&secondDaily))
	require.InDelta(t, 3, firstDaily, 0.000001)
	require.InDelta(t, 0, secondDaily, 0.000001)
	require.True(t, firstDailyWindow.After(expiredDailyWindow))
}

func TestUsageBillingRepositoryApply_StackedSubscriptionUsesCurrentGroupLimit(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	currentDailyLimit := 10.0
	snapshotDailyLimit := 100.0
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-current-group-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-current-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
		DailyLimitUSD:    &currentDailyLimit,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-current-group-" + uuid.NewString(),
		Name:    "billing-current-group",
	})

	now := time.Now().UTC()
	exhaustedByCurrentGroup := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:                user.ID,
		GroupID:               group.ID,
		StartsAt:              now.Add(-2 * time.Hour),
		ExpiresAt:             now.Add(24 * time.Hour),
		DailyUsageUSD:         currentDailyLimit,
		DailyLimitUSDSnapshot: &snapshotDailyLimit,
	})
	available := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:                user.ID,
		GroupID:               group.ID,
		StartsAt:              now.Add(-1 * time.Hour),
		ExpiresAt:             now.Add(24 * time.Hour),
		DailyUsageUSD:         0,
		DailyLimitUSDSnapshot: &snapshotDailyLimit,
	})

	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:           uuid.NewString(),
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		SubscriptionUserID:  user.ID,
		SubscriptionGroupID: group.ID,
		SubscriptionCost:    5,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.SubscriptionID)
	require.Equal(t, available.ID, *result.SubscriptionID)

	var exhaustedDaily, availableDaily float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", exhaustedByCurrentGroup.ID).Scan(&exhaustedDaily))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", available.ID).Scan(&availableDaily))
	require.InDelta(t, currentDailyLimit, exhaustedDaily, 0.000001)
	require.InDelta(t, 5, availableDaily, 0.000001)
}

func TestUsageBillingRepositoryApply_StackedSubscriptionRecordsPostSuccessOverage(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	dailyLimit := 10.0
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-overage-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-overage-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
		DailyLimitUSD:    &dailyLimit,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-overage-" + uuid.NewString(),
		Name:    "billing-overage",
	})

	now := time.Now().UTC()
	first := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:        user.ID,
		GroupID:       group.ID,
		StartsAt:      now.Add(-2 * time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		DailyUsageUSD: 9,
	})
	second := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:        user.ID,
		GroupID:       group.ID,
		StartsAt:      now.Add(-1 * time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		DailyUsageUSD: 9,
	})

	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:           uuid.NewString(),
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		SubscriptionUserID:  user.ID,
		SubscriptionGroupID: group.ID,
		SubscriptionCost:    5,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.SubscriptionID)
	require.Equal(t, first.ID, *result.SubscriptionID)

	var firstDaily, secondDaily float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", first.ID).Scan(&firstDaily))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", second.ID).Scan(&secondDaily))
	require.InDelta(t, 10, firstDaily, 0.000001)
	require.InDelta(t, 13, secondDaily, 0.000001)
}

func TestUsageBillingRepositoryApply_StackedSubscriptionAllExhaustedRecordsPostSuccessOverage(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	dailyLimit := 10.0
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-all-exhausted-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:             "usage-billing-all-exhausted-group-" + uuid.NewString(),
		Platform:         service.PlatformAnthropic,
		SubscriptionType: service.SubscriptionTypeSubscription,
		DailyLimitUSD:    &dailyLimit,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-usage-billing-all-exhausted-" + uuid.NewString(),
		Name:    "billing-all-exhausted",
	})

	now := time.Now().UTC()
	first := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:        user.ID,
		GroupID:       group.ID,
		StartsAt:      now.Add(-2 * time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		DailyUsageUSD: dailyLimit,
	})
	second := mustCreateSubscription(t, client, &service.UserSubscription{
		UserID:        user.ID,
		GroupID:       group.ID,
		StartsAt:      now.Add(-1 * time.Hour),
		ExpiresAt:     now.Add(24 * time.Hour),
		DailyUsageUSD: dailyLimit,
	})

	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:           uuid.NewString(),
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		SubscriptionUserID:  user.ID,
		SubscriptionGroupID: group.ID,
		SubscriptionCost:    1,
	})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.SubscriptionID)
	require.Equal(t, first.ID, *result.SubscriptionID)

	var firstDaily, secondDaily float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", first.ID).Scan(&firstDaily))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT daily_usage_usd FROM user_subscriptions WHERE id = $1", second.ID).Scan(&secondDaily))
	require.InDelta(t, dailyLimit+1, firstDaily, 0.000001)
	require.InDelta(t, dailyLimit, secondDaily, 0.000001)
}

func TestUsageBillingRepositoryApply_RequestFingerprintConflict(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-conflict-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-conflict-" + uuid.NewString(),
		Name:   "billing-conflict",
	})

	requestID := uuid.NewString()
	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	})
	require.NoError(t, err)

	_, err = repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 2.50,
	})
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
}

func TestUsageBillingRepositoryApply_DeletedAPIKeyStillBillsBalance(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-deleted-key-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:      user.ID,
		Key:         "sk-usage-billing-deleted-key-" + uuid.NewString(),
		Name:        "billing-deleted-key",
		Quota:       50,
		RateLimit5h: 50,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-deleted-key-account-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})

	_, err := integrationDB.ExecContext(ctx, "UPDATE api_keys SET deleted_at = NOW() WHERE id = $1", apiKey.ID)
	require.NoError(t, err)

	requestID := uuid.NewString()
	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:           requestID,
		APIKeyID:            apiKey.ID,
		UserID:              user.ID,
		AccountID:           account.ID,
		AccountType:         service.AccountTypeAPIKey,
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.False(t, result.APIKeyQuotaExhausted)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)

	var quotaUsed, usage5h float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used, usage_5h FROM api_keys WHERE id = $1", apiKey.ID).Scan(&quotaUsed, &usage5h))
	require.InDelta(t, 0, quotaUsed, 0.000001)
	require.InDelta(t, 0, usage5h, 0.000001)

	var dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryApply_DeletedAccountStillBillsBalance(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-deleted-account-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-deleted-account-" + uuid.NewString(),
		Name:   "billing-deleted-account",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name:     "usage-billing-deleted-account-" + uuid.NewString(),
		Platform: service.PlatformGemini,
		Type:     service.AccountTypeServiceAccount,
	})

	_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET deleted_at = NOW() WHERE id = $1", account.ID)
	require.NoError(t, err)

	requestID := uuid.NewString()
	result, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:        requestID,
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        account.ID,
		AccountType:      service.AccountTypeServiceAccount,
		BalanceCost:      1.25,
		AccountQuotaCost: 1.25,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.Nil(t, result.QuotaState)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)

	var dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&dedupCount))
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryApply_UpdatesAccountQuota(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-account-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-account-" + uuid.NewString(),
		Name:   "billing-account",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-billing-account-quota-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
		Extra: map[string]any{
			"quota_limit": 100.0,
		},
	})

	_, err := repo.Apply(ctx, &service.UsageBillingCommand{
		RequestID:        uuid.NewString(),
		APIKeyID:         apiKey.ID,
		UserID:           user.ID,
		AccountID:        account.ID,
		AccountType:      service.AccountTypeAPIKey,
		AccountQuotaCost: 3.5,
	})
	require.NoError(t, err)

	var quotaUsed float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE((extra->>'quota_used')::numeric, 0) FROM accounts WHERE id = $1", account.ID).Scan(&quotaUsed))
	require.InDelta(t, 3.5, quotaUsed, 0.000001)
}

// service_account 只设总上限：累加 quota_used；日/周用量只在设置了对应 limit 时累加，未跨越上限不入队调度 outbox。
func TestUsageBillingRepositoryApply_UpdatesServiceAccountQuota(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-sa-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-sa-" + uuid.NewString(),
		Name:   "billing-sa",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name:     "usage-billing-sa-quota-" + uuid.NewString(),
		Platform: service.PlatformGemini,
		Type:     service.AccountTypeServiceAccount,
		Extra: map[string]any{
			"quota_limit": 100.0,
		},
	})

	for _, cost := range []float64{2.25, 1.5} {
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKey.ID,
			UserID:           user.ID,
			AccountID:        account.ID,
			AccountType:      service.AccountTypeServiceAccount,
			AccountQuotaCost: cost,
		})
		require.NoError(t, err)
	}

	var quotaUsed float64
	var hasDailyUsed, hasWeeklyUsed bool
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COALESCE((extra->>'quota_used')::numeric, 0), extra ? 'quota_daily_used', extra ? 'quota_weekly_used' FROM accounts WHERE id = $1",
		account.ID,
	).Scan(&quotaUsed, &hasDailyUsed, &hasWeeklyUsed))
	require.InDelta(t, 3.75, quotaUsed, 0.000001)
	require.False(t, hasDailyUsed)
	require.False(t, hasWeeklyUsed)

	var outboxCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = $1 AND account_id = $2",
		service.SchedulerOutboxEventAccountChanged, account.ID,
	).Scan(&outboxCount))
	require.Zero(t, outboxCount, "service_account under its limit must not enqueue scheduler outbox")
}

func TestUsageBillingRepositoryApply_EnqueuesSchedulerOutboxOnQuotaCrossing(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)

	newFixture := func(t *testing.T, extra map[string]any) (int64, int64) {
		t.Helper()
		user := mustCreateUser(t, client, &service.User{
			Email:        fmt.Sprintf("usage-billing-outbox-user-%d-%s@example.com", time.Now().UnixNano(), uuid.NewString()),
			PasswordHash: "hash",
		})
		apiKey := mustCreateApiKey(t, client, &service.APIKey{
			UserID: user.ID,
			Key:    "sk-usage-billing-outbox-" + uuid.NewString(),
			Name:   "billing-outbox",
		})
		account := mustCreateAccount(t, client, &service.Account{
			Name:  "usage-billing-outbox-" + uuid.NewString(),
			Type:  service.AccountTypeAPIKey,
			Extra: extra,
		})
		return apiKey.ID, account.ID
	}

	outboxCountFor := func(t *testing.T, accountID int64) int {
		t.Helper()
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM scheduler_outbox WHERE event_type = $1 AND account_id = $2",
			service.SchedulerOutboxEventAccountChanged, accountID,
		).Scan(&count))
		return count
	}

	t.Run("daily_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_daily_limit": 10.0,
		})
		// 第一次低于日限额：不应入队 outbox
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 4,
		})
		require.NoError(t, err)
		require.Equal(t, 0, outboxCountFor(t, accountID), "below limit should not enqueue")

		// 第二次跨越日限额：应入队一次 outbox
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 8,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "crossing daily limit should enqueue once")

		// 再次递增（已超）：不应重复入队
		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 2,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "subsequent increments beyond limit should not re-enqueue")
	})

	t.Run("weekly_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_weekly_limit": 10.0,
		})
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 15, // 单次即跨越
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "single-shot crossing weekly limit should enqueue once")
	})

	t.Run("total_first_crossing_enqueues", func(t *testing.T) {
		apiKeyID, accountID := newFixture(t, map[string]any{
			"quota_limit": 10.0,
		})
		_, err := repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 4,
		})
		require.NoError(t, err)
		require.Equal(t, 0, outboxCountFor(t, accountID), "below limit should not enqueue")

		_, err = repo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:        uuid.NewString(),
			APIKeyID:         apiKeyID,
			AccountID:        accountID,
			AccountType:      service.AccountTypeAPIKey,
			AccountQuotaCost: 8,
		})
		require.NoError(t, err)
		require.Equal(t, 1, outboxCountFor(t, accountID), "crossing total limit should enqueue once")
	})
}

func TestIncrementUsageBillingAccountQuota_StatementVariantsMatchFullStatement(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	now := time.Now().UTC()
	at := func(offset time.Duration) string { return now.Add(offset).Format(time.RFC3339) }

	// dailyUnset/weeklyUnset 为库中该维度视为未设置上限：省略该维度的语句在这些配置上命中，其余配置命中 0 行。
	configs := []struct {
		name        string
		extra       map[string]any
		rawExtra    string
		dailyUnset  bool
		weeklyUnset bool
		wantErr     bool
	}{
		{name: "no_limits", extra: map[string]any{}, dailyUnset: true, weeklyUnset: true},
		{name: "total_only", extra: map[string]any{"quota_limit": 100.0, "quota_used": 3.0}, dailyUnset: true, weeklyUnset: true},
		{name: "zero_and_negative_period_limits", extra: map[string]any{"quota_limit": 5.0, "quota_daily_limit": 0.0, "quota_weekly_limit": -1.0}, dailyUnset: true, weeklyUnset: true},
		{name: "null_period_limits", extra: map[string]any{"quota_daily_limit": nil, "quota_weekly_limit": nil}, dailyUnset: true, weeklyUnset: true},
		{name: "string_period_limits", extra: map[string]any{"quota_daily_limit": "10", "quota_weekly_limit": "0"}},
		{name: "daily_rolling_active", extra: map[string]any{"quota_daily_limit": 10.0, "quota_daily_used": 4.0, "quota_daily_start": at(-time.Hour)}, weeklyUnset: true},
		{name: "daily_rolling_expired", extra: map[string]any{"quota_daily_limit": 10.0, "quota_daily_used": 9.0, "quota_daily_start": at(-30 * time.Hour)}, weeklyUnset: true},
		{name: "daily_fixed_expired", extra: map[string]any{
			"quota_daily_limit": 10.0, "quota_daily_used": 7.0, "quota_daily_reset_mode": "fixed",
			"quota_daily_reset_hour": 3, "quota_reset_timezone": "Asia/Shanghai", "quota_daily_reset_at": at(-2 * time.Hour),
		}, weeklyUnset: true},
		{name: "weekly_rolling_active", extra: map[string]any{"quota_weekly_limit": 50.0, "quota_weekly_used": 20.0, "quota_weekly_start": at(-48 * time.Hour)}, dailyUnset: true},
		{name: "weekly_fixed_expired", extra: map[string]any{
			"quota_weekly_limit": 50.0, "quota_weekly_used": 49.0, "quota_weekly_reset_mode": "fixed",
			"quota_weekly_reset_day": 1, "quota_weekly_reset_hour": 0, "quota_reset_timezone": "UTC", "quota_weekly_reset_at": at(-time.Hour),
		}, dailyUnset: true},
		{name: "daily_and_weekly", extra: map[string]any{
			"quota_limit": 100.0, "quota_daily_limit": 10.0, "quota_weekly_limit": 50.0,
			"quota_daily_used": 1.0, "quota_weekly_used": 2.0, "quota_daily_start": at(-time.Hour), "quota_weekly_start": at(-200 * time.Hour),
		}},
		{name: "array_extra", rawExtra: `[]`},
		{name: "scalar_extra", rawExtra: `"value"`},
		{name: "non_numeric_daily_limit", extra: map[string]any{"quota_daily_limit": "abc"}, weeklyUnset: true, wantErr: true},
		{name: "boolean_weekly_limit", extra: map[string]any{"quota_weekly_limit": true}, dailyUnset: true, wantErr: true},
	}

	for _, cfg := range configs {
		for _, hints := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
			t.Run(fmt.Sprintf("%s/daily=%v/weekly=%v", cfg.name, hints[0], hints[1]), func(t *testing.T) {
				variant := mustCreateAccount(t, client, &service.Account{
					Name:  "usage-billing-quota-variant-" + uuid.NewString(),
					Type:  service.AccountTypeAPIKey,
					Extra: maps.Clone(cfg.extra),
				})
				reference := mustCreateAccount(t, client, &service.Account{
					Name:  "usage-billing-quota-reference-" + uuid.NewString(),
					Type:  service.AccountTypeAPIKey,
					Extra: maps.Clone(cfg.extra),
				})
				if cfg.rawExtra != "" {
					_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET extra = $1::jsonb WHERE id IN ($2, $3)", cfg.rawExtra, variant.ID, reference.ID)
					require.NoError(t, err)
					t.Cleanup(func() {
						_, _ = integrationDB.ExecContext(ctx, "UPDATE accounts SET extra = '{}'::jsonb WHERE id IN ($1, $2)", variant.ID, reference.ID)
					})
				}

				if cfg.wantErr {
					sqlState := func(accountID int64, daily, weekly bool) pq.ErrorCode {
						tx, err := integrationDB.BeginTx(ctx, nil)
						require.NoError(t, err)
						defer func() { _ = tx.Rollback() }()
						_, err = incrementUsageBillingAccountQuota(ctx, tx, accountID, 2.5, daily, weekly)
						var pqErr *pq.Error
						require.ErrorAs(t, err, &pqErr)
						return pqErr.Code
					}
					require.Equal(t, sqlState(reference.ID, true, true), sqlState(variant.ID, hints[0], hints[1]))
					return
				}

				tx, err := integrationDB.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback() }()

				isObject := cfg.rawExtra == ""
				wantHit := (hints[0] && hints[1]) || (isObject && (hints[0] || cfg.dailyUnset) && (hints[1] || cfg.weeklyUnset))
				_, err = tx.ExecContext(ctx, "SAVEPOINT quota_variant_probe")
				require.NoError(t, err)
				probe, err := execAccountQuotaIncrement(ctx, tx, accountQuotaIncrementSQLs[accountQuotaIncrementSQLIndex(hints[0], hints[1])], variant.ID, 2.5)
				require.NoError(t, err)
				require.Equal(t, wantHit, probe != nil, "variant statement hit")
				_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT quota_variant_probe")
				require.NoError(t, err)

				got, err := incrementUsageBillingAccountQuota(ctx, tx, variant.ID, 2.5, hints[0], hints[1])
				require.NoError(t, err)
				want, err := incrementUsageBillingAccountQuota(ctx, tx, reference.ID, 2.5, true, true)
				require.NoError(t, err)
				require.Equal(t, *want, *got)

				var gotExtra, wantExtra string
				var sameExtra, sameUpdatedAt bool
				require.NoError(t, tx.QueryRowContext(ctx,
					"SELECT v.extra::text, r.extra::text, v.extra = r.extra, v.updated_at = r.updated_at FROM accounts v, accounts r WHERE v.id = $1 AND r.id = $2",
					variant.ID, reference.ID,
				).Scan(&gotExtra, &wantExtra, &sameExtra, &sameUpdatedAt))
				require.True(t, sameExtra, "variant extra %s != reference extra %s", gotExtra, wantExtra)
				require.True(t, sameUpdatedAt)
			})
		}
	}
}

func TestDashboardAggregationRepositoryCleanupUsageBillingDedup_BatchDeletesOldRows(t *testing.T) {
	ctx := context.Background()
	repo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	oldRequestID := "dedup-old-" + uuid.NewString()
	newRequestID := "dedup-new-" + uuid.NewString()
	oldCreatedAt := time.Now().UTC().AddDate(0, 0, -400)
	newCreatedAt := time.Now().UTC().Add(-time.Hour)

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint, created_at)
		VALUES ($1, 1, $2, $3), ($4, 1, $5, $6)
	`,
		oldRequestID, strings.Repeat("a", 64), oldCreatedAt,
		newRequestID, strings.Repeat("b", 64), newCreatedAt,
	)
	require.NoError(t, err)

	require.NoError(t, repo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	var oldCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", oldRequestID).Scan(&oldCount))
	require.Equal(t, 0, oldCount)

	var newCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1", newRequestID).Scan(&newCount))
	require.Equal(t, 1, newCount)

	var archivedCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup_archive WHERE request_id = $1", oldRequestID).Scan(&archivedCount))
	require.Equal(t, 1, archivedCount)
}

func TestUsageBillingRepositoryApply_DeduplicatesAgainstArchivedKey(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB)
	aggRepo := newDashboardAggregationRepositoryWithSQL(integrationDB)

	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-billing-archive-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-billing-archive-" + uuid.NewString(),
		Name:   "billing-archive",
	})

	requestID := uuid.NewString()
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		BalanceCost: 1.25,
	}

	result1, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result1.Applied)

	_, err = integrationDB.ExecContext(ctx, `
		UPDATE usage_billing_dedup
		SET created_at = $1
		WHERE request_id = $2 AND api_key_id = $3
	`, time.Now().UTC().AddDate(0, 0, -400), requestID, apiKey.ID)
	require.NoError(t, err)
	require.NoError(t, aggRepo.CleanupUsageBillingDedup(ctx, time.Now().UTC().AddDate(0, 0, -365)))

	result2, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result2.Applied)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.InDelta(t, 98.75, balance, 0.000001)
}
