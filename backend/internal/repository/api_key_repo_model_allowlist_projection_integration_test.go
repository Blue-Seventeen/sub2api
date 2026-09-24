//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetByKeyForAuthCarriesGroupModelAllowlist(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	canonical := service.GroupModelsListConfig{
		Enabled: true,
		Models:  []string{"gpt-5.4", "gpt-5.5-*"},
	}
	// Persist conflicting fields so auth must project the canonical policy,
	// rather than accidentally enforcing the legacy compatibility mirror.
	group, err := integrationEntClient.Group.Create().
		SetName(fmt.Sprintf("model-allowlist-proj-group-%d", suffix)).
		SetPlatform(service.PlatformOpenAI).
		SetRateMultiplier(1).
		SetModelsListConfig(canonical).
		SetModelAllowlist(service.DomainGroupModelAllowlist(service.GroupModelAllowlist{
			Enabled: true,
			Models:  []string{"stale-mirror"},
		})).Save(ctx)
	require.NoError(t, err)
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("model-allowlist-proj-%d@example.com", suffix), Concurrency: 5,
	})
	groupID := group.ID
	keyValue := fmt.Sprintf("sk-model-allowlist-proj-%d", suffix)
	apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	key := &service.APIKey{UserID: user.ID, GroupID: &groupID, Key: keyValue, Name: "model-allowlist-proj", Status: service.StatusActive}
	require.NoError(t, apiKeyRepo.Create(ctx, key))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')", keyValue)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id = $1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = $1", group.ID)
		require.NoError(t, err)
	})

	got, err := apiKeyRepo.GetByKeyForAuth(ctx, keyValue)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	require.True(t, got.Group.Hydrated)
	require.Equal(t, canonical, got.Group.ModelsListConfig, "auth projection must include the canonical models_list_config")
	require.True(t, got.Group.ModelAllowlist.Enabled, "the compatibility allowlist must reflect the canonical policy")
	require.Equal(t, []string{"gpt-5.4", "gpt-5.5-*"}, got.Group.ModelAllowlist.Models)
	require.True(t, got.Group.AllowsModel("gpt-5.4"))
	require.True(t, got.Group.AllowsModel("gpt-5.5-test"))
	require.False(t, got.Group.AllowsModel("stale-mirror"))
	require.False(t, got.Group.AllowsModel("unlisted-model"))
}
