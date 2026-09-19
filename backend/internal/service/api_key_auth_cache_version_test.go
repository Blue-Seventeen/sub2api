package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotPreservesPricingAndAccessPolicy(t *testing.T) {
	groupID := int64(10)
	search := 0.02
	apiKey := &APIKey{
		ID: 1, UserID: 2, GroupID: &groupID, Key: "k", Status: StatusActive,
		User: &User{ID: 2, Status: StatusActive, RestrictPublicGroups: true, UnifiedRateEnabled: true, UnifiedRateMultiplier: 0.5},
		Group: &Group{
			ID: groupID, Name: "g", Platform: PlatformOpenAI, Status: StatusActive,
			VideoModelPrices: map[string]map[string]float64{"model": {"720p": 0.3}},
			SearchPricePer1k: &search, AudioRealtimePricePerMin: &search,
			AudioTTSPricePerMillionChars: &search, AudioSTTPricePerHour: &search,
			LongContextPricingEnabled: true, ForceOpenAIFast: true, FreeOpenAIFast: true,
			ModelAllowlist:              GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5"}},
			CodexModelsManifestConfig:   GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{3}, FallbackToScheduler: true},
			MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDeny,
		},
	}
	svc := &APIKeyService{}
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))
	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.True(t, materialized.User.RestrictPublicGroups)
	require.Equal(t, 0.5, materialized.User.UnifiedRateMultiplier)
	require.Equal(t, apiKey.Group.VideoModelPrices, materialized.Group.VideoModelPrices)
	require.True(t, materialized.Group.LongContextPricingEnabled)
	require.True(t, materialized.Group.ForceOpenAIFast)
	require.True(t, materialized.Group.FreeOpenAIFast)
	require.Equal(t, apiKey.Group.ModelAllowlist, materialized.Group.ModelAllowlist)
	require.Equal(t, apiKey.Group.CodexModelsManifestConfig, materialized.Group.CodexModelsManifestConfig)
	require.Equal(t, ReasoningEffortOverLimitDeny, materialized.Group.MaxReasoningEffortOverLimit)
	require.Equal(t, apiKeyAuthSnapshotVersion, cached.Snapshot.Version)
}

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelAllowlist(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after model_allowlist was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV15AuthSnapshotWithoutReasoningEffortPolicy(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-reasoning-mappings", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 15},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v15 auth snapshot to be rejected after reasoning effort policy was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}
