package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type plazaCanonicalChannelRepo struct {
	ChannelRepository
	channels []Channel
}

func (r plazaCanonicalChannelRepo) ListAll(context.Context) ([]Channel, error) {
	return r.channels, nil
}

type plazaCanonicalGroupRepo struct {
	GroupRepository
	groups []Group
}

func (r plazaCanonicalGroupRepo) ListActive(context.Context) ([]Group, error) {
	return r.groups, nil
}

func TestModelPlazaServiceUsesCanonicalPolicyOverLegacyMirror(t *testing.T) {
	channels := []Channel{{
		ID: 1, Name: "canonical", Status: StatusActive, GroupIDs: []int64{10},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"canonical-model"}}},
	}}
	groups := []Group{{
		ID: 10, Name: "canonical", Platform: PlatformOpenAI, RateMultiplier: 1,
		ModelsListConfig: GroupModelsListConfig{Enabled: true, Models: []string{"canonical-model"}},
		ModelAllowlist:   GroupModelAllowlist{Enabled: true, Models: []string{"legacy-model"}},
	}}

	svc := NewModelPlazaService(
		plazaCanonicalChannelRepo{channels: channels},
		plazaCanonicalGroupRepo{groups: groups},
		nil,
		nil,
		nil,
	)
	out, err := svc.ListGroups(context.Background())

	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Models, 1)
	require.Equal(t, "canonical-model", out[0].Models[0].Name)
}
