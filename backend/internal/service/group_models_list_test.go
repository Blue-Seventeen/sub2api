package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupAllowsRequestedModel_CaseInsensitiveAndWildcard(t *testing.T) {
	group := &Group{
		ModelsListConfig: GroupModelsListConfig{
			Enabled: true,
			Models:  []string{"kimi", "KIMI-*"},
		},
	}

	require.True(t, GroupAllowsRequestedModel(group, "kimi"))
	require.True(t, GroupAllowsRequestedModel(group, "KIMI"))
	require.True(t, GroupAllowsRequestedModel(group, "Kimi"))
	require.True(t, GroupAllowsRequestedModel(group, "kimi-k2.6"))
	require.True(t, GroupAllowsRequestedModel(group, "KIMI-K2.6"))
	require.False(t, GroupAllowsRequestedModel(group, "gpt-5.5"))
}

func TestGroupAllowsRequestedModel_EnabledEmptyDeniesAll(t *testing.T) {
	group := &Group{ModelsListConfig: GroupModelsListConfig{Enabled: true}}

	require.False(t, GroupAllowsRequestedModel(group, "kimi-k2.6"))
}

func TestNormalizeGroupModelsListConfig_DedupesCaseInsensitive(t *testing.T) {
	cfg := normalizeGroupModelsListConfig(GroupModelsListConfig{
		Enabled: true,
		Models:  []string{" kimi ", "KIMI", "Kimi-*", "kimi-*", ""},
	})

	require.True(t, cfg.Enabled)
	require.Equal(t, []string{"kimi", "Kimi-*"}, cfg.Models)
}

func TestDefaultModelsListCandidateIDs_MoonshotUsesKimiModels(t *testing.T) {
	models := defaultModelsListCandidateIDs(PlatformMoonshot)

	require.Contains(t, models, "kimi-k2.6")
	require.Contains(t, models, "kimi-k2.5")
	require.NotContains(t, models, "claude-sonnet-4-6")
}

func TestDefaultModelsListCandidateIDs_CompatiblePlatformsUseProviderPresets(t *testing.T) {
	for _, platform := range CompatiblePlatforms() {
		t.Run(platform, func(t *testing.T) {
			defaultModels := CompatibleDefaultModels(platform)
			require.NotEmpty(t, defaultModels)

			want := make([]string, 0, len(defaultModels))
			for _, model := range defaultModels {
				want = append(want, model.ID)
			}

			require.Equal(t, want, defaultModelsListCandidateIDs(platform))
		})
	}
}
