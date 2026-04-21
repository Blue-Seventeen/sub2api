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

func TestPromotionRepositoryTeamContributionSemantics_UserVsAdmin(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	promotionRepo := &promotionRepository{db: integrationDB}
	usageRepo := newUsageLogRepositoryWithSQL(client, integrationDB)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	rootUser := mustCreateUser(t, client, &service.User{Email: "promotion-root-" + suffix + "@example.com"})
	childUser := mustCreateUser(t, client, &service.User{Email: "promotion-child-" + suffix + "@example.com"})

	childAccount := mustCreateAccount(t, client, &service.Account{Name: "promotion-team-account-" + suffix})
	childAPIKey := mustCreateApiKey(t, client, &service.APIKey{UserID: childUser.ID, Key: "sk-promotion-team-" + uuid.NewString(), Name: "k"})

	userIDs := []int64{rootUser.ID, childUser.ID}
	accountIDs := []int64{childAccount.ID}
	apiKeyIDs := []int64{childAPIKey.ID}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM promotion_commission_records WHERE beneficiary_user_id = ANY($1) OR source_user_id = ANY($1)`, pq.Array(userIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM promotion_activations WHERE user_id = ANY($1) OR promoter_user_id = ANY($1)`, pq.Array(userIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM promotion_users WHERE user_id = ANY($1)`, pq.Array(userIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM usage_logs WHERE user_id = ANY($1) OR api_key_id = ANY($2) OR account_id = ANY($3)`, pq.Array(userIDs), pq.Array(apiKeyIDs), pq.Array(accountIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ANY($1)`, pq.Array(apiKeyIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id = ANY($1)`, pq.Array(accountIDs))
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id = ANY($1)`, pq.Array(userIDs))
	})

	for _, userID := range userIDs {
		_, err := promotionRepo.EnsurePromotionUser(ctx, userID)
		require.NoError(t, err)
	}
	rootUserID := rootUser.ID
	_, err := promotionRepo.SetPromotionParent(ctx, childUser.ID, &rootUserID, service.PromotionBindingSourceAdmin, "integration setup", time.Now().UTC())
	require.NoError(t, err)

	businessDate := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	activatedAt := businessDate.Add(8 * time.Hour)
	err = promotionRepo.CreatePromotionActivation(ctx, service.PromotionActivation{
		UserID:             childUser.ID,
		PromoterUserID:     rootUser.ID,
		ActivatedAt:        activatedAt,
		ThresholdAmount:    5,
		TriggerUsageAmount: 6,
	}, 0)
	require.NoError(t, err)

	createPromotionUsageLog(t, usageRepo, childUser.ID, childAPIKey.ID, childAccount.ID, 100, businessDate.Add(10*time.Hour))
	createPromotionUsageLog(t, usageRepo, childUser.ID, childAPIKey.ID, childAccount.ID, 200, businessDate.AddDate(0, 0, -1).Add(10*time.Hour))

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO promotion_commission_records (
			beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
			base_amount, amount, status, note, created_at, updated_at
		) VALUES
			($1, $2, $3::date, 'commission', 1, 50, 1.20, 'pending', 'today commission', NOW(), NOW()),
			($1, $2, $3::date, 'activation', 1, 0, 0.80, 'settled', 'today activation', NOW(), NOW()),
			($1, $2, $4::date, 'commission', 1, 80, 2.50, 'settled', 'historical commission', NOW(), NOW()),
			($1, $2, $3::date, 'commission', 2, 99, 9.90, 'cancelled', 'cancelled should be excluded', NOW(), NOW()),
			($1, $2, $3::date, 'promotion', 1, 0, 7.70, 'settled', 'non B semantics should be excluded', NOW(), NOW())
	`, rootUser.ID, childUser.ID, businessDate.Format("2006-01-02"), businessDate.AddDate(0, 0, -1).Format("2006-01-02"))
	require.NoError(t, err)

	userItems, total, err := promotionRepo.ListPromotionTeam(ctx, rootUser.ID, service.PromotionTeamFilter{
		Page:      1,
		PageSize:  10,
		SortBy:    "today_contribution",
		SortOrder: "desc",
	}, businessDate)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, userItems, 1)
	require.Equal(t, childUser.ID, userItems[0].UserID)
	require.InDelta(t, 2.0, userItems[0].TodayContribution, 1e-9, "用户页应展示该成员今日给我贡献的佣金")
	require.InDelta(t, 4.5, userItems[0].TotalContribution, 1e-9, "用户页应展示该成员累计给我贡献的佣金")

	adminItems, adminTotal, err := promotionRepo.ListPromotionDownlines(ctx, rootUser.ID, service.PromotionTeamFilter{
		Page:      1,
		PageSize:  10,
		SortBy:    "today_contribution",
		SortOrder: "desc",
	}, businessDate, businessDate.Add(24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, adminTotal)
	require.Len(t, adminItems, 1)
	require.Equal(t, childUser.ID, adminItems[0].UserID)
	require.InDelta(t, 100.0, adminItems[0].TodayContribution, 1e-9, "管理员页仍应展示该成员今日真实消费")
	require.InDelta(t, 300.0, adminItems[0].TotalContribution, 1e-9, "管理员页仍应展示该成员累计真实消费")
}
