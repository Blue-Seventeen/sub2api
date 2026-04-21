//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPromotionRepositoryUpsertDailyPromotionCommissions_OnlyCountsActivatedUsage(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	promotionRepo := &promotionRepository{db: integrationDB}
	usageRepo := newUsageLogRepositoryWithSQL(client, integrationDB)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	parent := mustCreateUser(t, client, &service.User{Email: "promotion-parent-" + suffix + "@example.com"})
	activatedChild := mustCreateUser(t, client, &service.User{Email: "promotion-activated-child-" + suffix + "@example.com"})
	inactiveChild := mustCreateUser(t, client, &service.User{Email: "promotion-inactive-child-" + suffix + "@example.com"})

	activatedAccount := mustCreateAccount(t, client, &service.Account{Name: "promotion-activated-account-" + suffix})
	activatedAPIKey := mustCreateApiKey(t, client, &service.APIKey{UserID: activatedChild.ID, Key: "sk-promotion-activated-" + uuid.NewString(), Name: "k"})
	inactiveAccount := mustCreateAccount(t, client, &service.Account{Name: "promotion-inactive-account-" + suffix})
	inactiveAPIKey := mustCreateApiKey(t, client, &service.APIKey{UserID: inactiveChild.ID, Key: "sk-promotion-inactive-" + uuid.NewString(), Name: "k"})

	userIDs := []int64{parent.ID, activatedChild.ID, inactiveChild.ID}
	accountIDs := []int64{activatedAccount.ID, inactiveAccount.ID}
	apiKeyIDs := []int64{activatedAPIKey.ID, inactiveAPIKey.ID}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM promotion_commission_records WHERE beneficiary_user_id = ANY($1) OR source_user_id = ANY($1)`, pq.Array(userIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM promotion_activations WHERE user_id = ANY($1) OR promoter_user_id = ANY($1)`, pq.Array(userIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM promotion_users WHERE user_id = ANY($1)`, pq.Array(userIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM usage_logs WHERE user_id = ANY($1) OR api_key_id = ANY($2) OR account_id = ANY($3)`, pq.Array(userIDs), pq.Array(apiKeyIDs), pq.Array(accountIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ANY($1)`, pq.Array(apiKeyIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id = ANY($1)`, pq.Array(accountIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id = ANY($1)`, pq.Array(userIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM promotion_level_configs`)
	})

	_, err := promotionRepo.UpsertPromotionLevels(ctx, []service.PromotionLevelConfig{
		{
			LevelNo:                  1,
			LevelName:                "Integration Level 1",
			RequiredActivatedInvites: 0,
			DirectRate:               10,
			IndirectRate:             0,
			SortOrder:                1,
			Enabled:                  true,
		},
	})
	require.NoError(t, err)

	for _, userID := range userIDs {
		_, err := promotionRepo.EnsurePromotionUser(ctx, userID)
		require.NoError(t, err)
	}

	parentUserID := parent.ID
	_, err = promotionRepo.SetPromotionParent(ctx, activatedChild.ID, &parentUserID, service.PromotionBindingSourceAdmin, "integration setup", time.Now().UTC())
	require.NoError(t, err)
	_, err = promotionRepo.SetPromotionParent(ctx, inactiveChild.ID, &parentUserID, service.PromotionBindingSourceAdmin, "integration setup", time.Now().UTC())
	require.NoError(t, err)

	businessDate := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	preActivationUsageAt := businessDate.Add(9 * time.Hour)
	activatedAt := businessDate.Add(10 * time.Hour)
	postActivationUsageAt := businessDate.Add(11 * time.Hour)

	err = promotionRepo.CreatePromotionActivation(ctx, service.PromotionActivation{
		UserID:             activatedChild.ID,
		PromoterUserID:     parent.ID,
		ActivatedAt:        activatedAt,
		ThresholdAmount:    5,
		TriggerUsageAmount: 6,
	}, 0)
	require.NoError(t, err)

	createPromotionUsageLog(t, usageRepo, activatedChild.ID, activatedAPIKey.ID, activatedAccount.ID, 5, preActivationUsageAt)
	createPromotionUsageLog(t, usageRepo, activatedChild.ID, activatedAPIKey.ID, activatedAccount.ID, 7, postActivationUsageAt)
	createPromotionUsageLog(t, usageRepo, inactiveChild.ID, inactiveAPIKey.ID, inactiveAccount.ID, 9, postActivationUsageAt)

	err = promotionRepo.UpsertDailyPromotionCommissions(ctx, businessDate, businessDate, businessDate.Add(24*time.Hour))
	require.NoError(t, err)

	rows, err := integrationDB.QueryContext(ctx, `
		SELECT source_user_id, relation_depth, base_amount, amount
		FROM promotion_commission_records
		WHERE beneficiary_user_id = $1
		  AND commission_type = 'commission'
		  AND business_date = $2::date
		ORDER BY source_user_id, relation_depth
	`, parent.ID, businessDate.Format("2006-01-02"))
	require.NoError(t, err)
	defer rows.Close()

	type commissionRow struct {
		sourceUserID  int64
		relationDepth int
		baseAmount    float64
		amount        float64
	}

	var got []commissionRow
	for rows.Next() {
		var item commissionRow
		require.NoError(t, rows.Scan(&item.sourceUserID, &item.relationDepth, &item.baseAmount, &item.amount))
		got = append(got, item)
	}
	require.NoError(t, rows.Err())

	require.Len(t, got, 1, "未激活账户和激活前消费都不应产生返佣")
	require.Equal(t, activatedChild.ID, got[0].sourceUserID)
	require.Equal(t, 1, got[0].relationDepth)
	require.InDelta(t, 7.0, got[0].baseAmount, 1e-9)
	require.InDelta(t, 0.7, got[0].amount, 1e-9)
}

func createPromotionUsageLog(t *testing.T, repo *usageLogRepository, userID, apiKeyID, accountID int64, realActualCost float64, createdAt time.Time) {
	t.Helper()

	log := &service.UsageLog{
		UserID:                userID,
		APIKeyID:              apiKeyID,
		AccountID:             accountID,
		RequestID:             uuid.NewString(),
		Model:                 "claude-3",
		InputTokens:           10,
		OutputTokens:          20,
		TotalCost:             realActualCost,
		ActualCost:            realActualCost,
		RealActualCost:        realActualCost,
		CreatedAt:             createdAt,
		RateMultiplier:        1,
		UnifiedRateMultiplier: 1,
	}

	_, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
}
