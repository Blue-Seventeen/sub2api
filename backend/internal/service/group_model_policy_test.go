package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupEffectiveModelPolicyUsesModelsListConfigAsCanonical(t *testing.T) {
	group := &Group{
		ModelsListConfig: GroupModelsListConfig{Enabled: true, Models: []string{" gpt-5.4 ", "GPT-5.4"}},
		ModelAllowlist:   GroupModelAllowlist{Enabled: true, Models: []string{"legacy-only"}},
	}

	policy := group.EffectiveModelPolicy()
	require.Equal(t, GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}, policy)
	require.True(t, group.ModelAllowlistEnabled())
	require.True(t, group.AllowsModel("GPT-5.4"))
	require.False(t, group.AllowsModel("legacy-only"))
}

func TestGroupEffectiveGroupModelPolicyMatchesCompatibilityWrapper(t *testing.T) {
	group := &Group{
		ModelsListConfig: GroupModelsListConfig{Enabled: true, Models: []string{" gpt-5.4 ", "GPT-5.4"}},
		ModelAllowlist:   GroupModelAllowlist{Enabled: true, Models: []string{"legacy-only"}},
	}

	require.Equal(t, GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}, group.EffectiveGroupModelPolicy())
	require.Equal(t, group.EffectiveGroupModelPolicy(), group.EffectiveModelPolicy())
}

func TestGroupEffectiveModelPolicyFallsBackToLegacyMirrorOnlyWhenCanonicalIsAbsent(t *testing.T) {
	group := &Group{ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"legacy-only"}}}

	require.Equal(t, group.ModelAllowlist, group.EffectiveModelPolicy())
	require.True(t, group.AllowsModel("legacy-only"))
}

func TestGroupEffectiveModelPolicyHydratedEmptyCanonicalDoesNotFallBackToMirror(t *testing.T) {
	group := &Group{
		Hydrated:         true,
		ModelsListConfig: GroupModelsListConfig{},
		ModelAllowlist:   GroupModelAllowlist{Enabled: true, Models: []string{"stale-legacy"}},
	}

	require.Equal(t, GroupModelAllowlist{}, group.EffectiveModelPolicy())
	require.False(t, group.ModelAllowlistEnabled())
	require.True(t, group.AllowsModel("stale-legacy"))
}

func TestGroupModelPolicyAliasNormalization(t *testing.T) {
	canonical := GroupModelsListConfig{Enabled: true, Models: []string{" gpt-5.4 ", "GPT-5.4", "gpt-*"}}
	got, err := NormalizeGroupModelPolicy(canonical)
	require.NoError(t, err)
	require.Equal(t, GroupModelsListConfig{Enabled: true, Models: []string{"gpt-5.4", "gpt-*"}}, got.ModelsListConfig)
	require.Equal(t, GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4", "gpt-*"}}, got.ModelAllowlist)
}

func TestNormalizeGroupModelPolicyAcceptsInteriorAndLeadingWildcards(t *testing.T) {
	policy, err := NormalizeGroupModelPolicy(GroupModelsListConfig{
		Enabled: true,
		Models:  []string{"foo*bar", "*codex"},
	})

	require.NoError(t, err)
	require.Equal(t, []string{"foo*bar", "*codex"}, policy.ModelsListConfig.Models)
	require.Equal(t, policy.ModelsListConfig.Models, policy.ModelAllowlist.Models)
	for _, model := range []string{"foo-middle-bar", "gpt-5-codex"} {
		require.True(t, ModelsListAllowsModel(policy.ModelsListConfig.Models, model))
		require.True(t, policy.ModelAllowlist.Allows(model))
	}
	for _, model := range []string{"prefix-foo-middle-bar", "gpt-5-codex-extra"} {
		require.False(t, ModelsListAllowsModel(policy.ModelsListConfig.Models, model))
		require.False(t, policy.ModelAllowlist.Allows(model))
	}
}

func TestNormalizeGroupModelPolicyRejectsExplicitEnabledEmptyCanonicalConfig(t *testing.T) {
	_, err := NormalizeGroupModelPolicy(GroupModelsListConfig{Enabled: true})

	require.Error(t, err)
	require.Contains(t, err.Error(), "INVALID_MODEL_ALLOWLIST")
}

func TestAPIKeyAuthSnapshotUsesCanonicalModelPolicy(t *testing.T) {
	groupID := int64(10)
	apiKey := &APIKey{
		ID: 1, UserID: 2, GroupID: &groupID, Key: "k", Status: StatusActive,
		User: &User{ID: 2, Status: StatusActive},
		Group: &Group{
			ID:               groupID,
			ModelsListConfig: GroupModelsListConfig{Enabled: true, Models: []string{"gpt-5.4"}},
			ModelAllowlist:   GroupModelAllowlist{Enabled: true, Models: []string{"legacy-only"}},
		},
	}

	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}, snapshot.Group.ModelAllowlist)
	require.Equal(t, apiKey.Group.ModelsListConfig, snapshot.Group.ModelsListConfig)
	require.True(t, snapshot.Group.ModelsListConfigPresent)
	payload, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var decoded APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.True(t, decoded.Group.ModelsListConfigPresent)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
}

func TestAPIKeyAuthSnapshotHydratedEmptyCanonicalDoesNotRestoreLegacyMirror(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := svc.snapshotToAPIKey("key", &APIKeyAuthSnapshot{
		Version: apiKeyAuthSnapshotVersion,
		Group: &APIKeyAuthGroupSnapshot{
			ID:                      10,
			ModelsListConfig:        GroupModelsListConfig{},
			ModelsListConfigPresent: true,
			ModelAllowlist:          GroupModelAllowlist{Enabled: true, Models: []string{"stale-legacy"}},
		},
	})

	require.NotNil(t, apiKey.Group)
	require.Equal(t, GroupModelAllowlist{}, apiKey.Group.ModelAllowlist)
	require.False(t, apiKey.Group.ModelAllowlistEnabled())
}

func TestAPIKeyAuthSnapshotLegacyMirrorRemainsEffectiveWhenCanonicalIsAbsent(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := svc.snapshotToAPIKey("key", &APIKeyAuthSnapshot{
		Version: apiKeyAuthSnapshotVersion,
		Group: &APIKeyAuthGroupSnapshot{
			ID:               10,
			ModelsListConfig: GroupModelsListConfig{},
			ModelAllowlist:   GroupModelAllowlist{Enabled: true, Models: []string{"legacy-only"}},
		},
	})

	require.NotNil(t, apiKey.Group)
	require.Equal(t, GroupModelAllowlist{Enabled: true, Models: []string{"legacy-only"}}, apiKey.Group.EffectiveModelPolicy())
}
