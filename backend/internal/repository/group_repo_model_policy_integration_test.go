//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupModelPolicyCanonicalProjections(t *testing.T) {
	for _, canonical := range []service.GroupModelsListConfig{
		{}, {Models: []string{"disabled-canonical"}}, {Enabled: true, Models: []string{"canonical"}},
	} {
		t.Run(fmt.Sprintf("%t/%v", canonical.Enabled, canonical.Models), func(t *testing.T) {
			ctx := context.Background()
			tx := testEntTx(t)
			client := tx.Client()
			group, err := client.Group.Create().SetName("canonical-projection").SetPlatform(service.PlatformOpenAI).
				SetModelsListConfig(canonical).
				SetModelAllowlist(service.DomainGroupModelAllowlist(service.GroupModelAllowlist{Enabled: true, Models: []string{"stale-mirror"}})).Save(ctx)
			require.NoError(t, err)
			user, err := client.User.Create().SetEmail("canonical-projection@test.invalid").SetPasswordHash("hash").Save(ctx)
			require.NoError(t, err)
			key, err := client.APIKey.Create().SetUserID(user.ID).SetGroupID(group.ID).SetKey("canonical-projection-key").SetName("projection").Save(ctx)
			require.NoError(t, err)
			repo := newGroupRepositoryWithSQL(client, tx)
			full, err := repo.GetByID(ctx, group.ID)
			require.NoError(t, err)
			lite, err := repo.GetByIDLite(ctx, group.ID)
			require.NoError(t, err)
			listed, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, service.PlatformOpenAI, "", "canonical-projection", nil)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			apiRepo := newAPIKeyRepositoryWithSQL(client, tx)
			auth, err := apiRepo.GetByKeyForAuth(ctx, key.Key)
			require.NoError(t, err)
			for _, got := range []*service.Group{full, lite, &listed[0], auth.Group} {
				require.NotNil(t, got)
				require.True(t, got.Hydrated)
				require.Equal(t, canonical, got.ModelsListConfig)
				require.Equal(t, canonical.Enabled, got.ModelAllowlist.Enabled)
				require.Equal(t, canonical.Models, got.ModelAllowlist.Models)
				require.Equal(t, !canonical.Enabled, got.AllowsModel("stale-mirror"))
			}
		})
	}
}

func TestGlobalModelOperationsReturnFreshGroupProjection(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	group := concurrentPolicyGroup("global-fresh-projection", "projection-test-platform")
	_, err := repo.CreateWithGlobalModelOperations(ctx, group, []service.GroupModelOperation{{Operation: "add", Model: "created"}})
	require.NoError(t, err)
	got, err := repo.GetByIDLite(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, got.UpdatedAt, group.UpdatedAt, "create result must reflect its final global write")
	_, err = repo.UpdateWithGlobalModelOperations(ctx, group, []service.GroupModelOperation{{Operation: "add", Model: "updated"}})
	require.NoError(t, err, "the returned create snapshot must be reusable")
	got, err = repo.GetByIDLite(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, got.UpdatedAt, group.UpdatedAt)
	require.Equal(t, []string{"base", "created", "updated"}, group.ModelsListConfig.Models)
	require.Equal(t, group.ModelsListConfig.Models, group.ModelAllowlist.Models)
	group.Name = "fresh-projection-next-save"
	require.NoError(t, repo.Update(ctx, group), "the returned update snapshot must be reusable")
}

func TestGlobalModelPolicyInvalidFinalStateRollsBackAllWrites(t *testing.T) {
	for _, method := range []string{"create", "update"} {
		for _, operation := range []service.GroupModelOperation{
			{Operation: "add", Model: "foo*bar"},
			{Operation: "remove", Model: "only"},
		} {
			t.Run(method+"/"+operation.Operation, func(t *testing.T) {
				ctx := context.Background()
				repo := newGroupRepositoryWithSQL(testEntClient(t), integrationDB)
				suffix := fmt.Sprintf("%d", time.Now().UnixNano())
				current := &service.Group{
					Name: "policy-current-" + suffix, Platform: service.PlatformOpenAI,
					RateMultiplier: 1, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard,
					ModelsListConfig: service.GroupModelsListConfig{Enabled: true, Models: []string{"only", "keep"}},
				}
				originalName := current.Name
				target := &service.Group{
					Name: "policy-target-" + suffix, Platform: service.PlatformOpenAI,
					RateMultiplier: 1, Status: "inactive", SubscriptionType: service.SubscriptionTypeStandard,
					ModelsListConfig: service.GroupModelsListConfig{Enabled: true, Models: []string{"only"}},
				}
				var userID int64
				keyValue := "policy-key-" + suffix
				cacheKey := fmt.Sprintf("%x", sha256.Sum256([]byte(keyValue)))
				t.Cleanup(func() {
					_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
					_, _ = integrationDB.ExecContext(ctx, `DELETE FROM groups WHERE name = $1 OR id IN ($2, $3)`, originalName, current.ID, target.ID)
					_, _ = integrationDB.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE group_id IN ($1, $2)`, current.ID, target.ID)
					_, _ = integrationDB.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = $1`, cacheKey)
				})
				if method == "update" {
					require.NoError(t, repo.Create(ctx, current))
				}
				require.NoError(t, repo.Create(ctx, target))
				require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'hash') RETURNING id`, suffix+"@policy.test").Scan(&userID))
				keyGroupID := target.ID
				if method == "update" {
					keyGroupID = current.ID
				}
				_, err := integrationDB.ExecContext(ctx, `INSERT INTO api_keys (user_id, group_id, key, name) VALUES ($1, $2, $3, 'policy')`, userID, keyGroupID, keyValue)
				require.NoError(t, err)
				_, err = integrationDB.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE group_id IN ($1, $2)`, current.ID, target.ID)
				require.NoError(t, err)
				if method == "update" {
					current.Name = "must-roll-back-" + suffix
					_, err = repo.UpdateWithGlobalModelOperations(ctx, current, []service.GroupModelOperation{operation})
				} else {
					_, err = repo.CreateWithGlobalModelOperations(ctx, current, []service.GroupModelOperation{operation})
				}
				require.ErrorContains(t, err, "INVALID_MODEL_ALLOWLIST")
				got, err := repo.GetByID(ctx, target.ID)
				require.NoError(t, err)
				require.Equal(t, []string{"only"}, got.ModelsListConfig.Models)
				require.True(t, got.ModelsListConfig.Enabled)
				require.Equal(t, got.ModelsListConfig.Models, got.ModelAllowlist.Models)
				if method == "update" {
					got, err = repo.GetByID(ctx, current.ID)
					require.NoError(t, err)
					require.Equal(t, originalName, got.Name)
					require.Equal(t, []string{"only", "keep"}, got.ModelsListConfig.Models)
					require.Equal(t, got.ModelsListConfig.Models, got.ModelAllowlist.Models)
				} else {
					var count int
					require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE name = $1`, originalName).Scan(&count))
					require.Zero(t, count, "new group must roll back with invalid global policy")
				}
				var schedulerEvents, authEvents int
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox WHERE group_id IN ($1, $2)`, current.ID, target.ID).Scan(&schedulerEvents))
				require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key = $1`, cacheKey).Scan(&authEvents))
				require.Zero(t, schedulerEvents)
				require.Zero(t, authEvents)
			})
		}
	}
}
