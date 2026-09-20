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
