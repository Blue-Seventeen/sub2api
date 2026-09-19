//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"math"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func groupContractFixture() *service.Group {
	price := 0.75
	return &service.Group{
		ID: 7, Name: "contract", Platform: service.PlatformOpenAI,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeSubscription,
		ModelAllowlist:   service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5*"}},
		ModelsListConfig: service.GroupModelsListConfig{Enabled: true, Models: []string{"gpt-5"}},
		CodexModelsManifestConfig: domain.GroupCodexModelsManifestConfig{
			Enabled: true, AccountIDs: []int64{9, 3}, FallbackToScheduler: true,
		},
		ForceOpenAIFast: true, FreeOpenAIFast: true,
		MaxReasoningEffort: "high", MaxReasoningEffortOverLimit: "reject",
		LongContextPricingEnabled: true,
		ModelPricing:              []service.ChannelModelPricing{{Models: []string{"gpt-5"}, InputPrice: &price}},
		VideoModelPrices:          map[string]map[string]float64{"grok-imagine-video": {"720p": price}},
		SearchPricePer1k:          &price, AudioRealtimePricePerMin: &price,
		AudioTTSPricePerMillionChars: &price, AudioSTTPricePerHour: &price,
		NewAPIStyleInterfaceEnabled: true, CustomLimitHours: 6, CustomLimitUSD: &price,
		PeakRateEnabled: true, PeakStart: "09:00", PeakEnd: "12:00", PeakRateMultiplier: 1.5,
		PeakRateWindows: []service.PeakRateWindow{{Start: "09:00", End: "12:00", Multiplier: 1.5}},
	}
}

// Observe the real Ent builder at the database boundary, before issuing SQL.
func captureGroupMutation(t *testing.T, input *service.Group, update bool) *dbent.GroupMutation {
	t.Helper()
	client := dbent.NewClient()
	stop := errors.New("captured group mutation")
	var captured *dbent.GroupMutation
	client.Group.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
			captured = m.(*dbent.GroupMutation)
			return nil, stop
		})
	})
	var err error
	if update {
		err = (&groupRepository{client: client}).Update(context.Background(), input)
	} else {
		err = createGroupRecord(context.Background(), client, input)
	}
	require.ErrorIs(t, err, stop)
	require.NotNil(t, captured)
	return captured
}

func TestGroupPersistenceContractWrites(t *testing.T) {
	for _, update := range []bool{false, true} {
		name := "create"
		if update {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			g := groupContractFixture()
			m := captureGroupMutation(t, g, update)
			pricing, err := json.Marshal(g.ModelPricing)
			require.NoError(t, err)
			fields := map[string]any{
				group.FieldModelAllowlist:            service.DomainGroupModelAllowlist(g.ModelAllowlist),
				group.FieldModelsListConfig:          g.ModelsListConfig,
				group.FieldCodexModelsManifestConfig: g.CodexModelsManifestConfig,
				group.FieldForceOpenaiFast:           true, group.FieldFreeOpenaiFast: true,
				group.FieldMaxReasoningEffortOverLimit: g.MaxReasoningEffortOverLimit,
				group.FieldLongContextPricingEnabled:   true, group.FieldModelPricing: jsontext.Value(pricing),
				group.FieldVideoModelPrices:             g.VideoModelPrices,
				group.FieldSearchPricePer1k:             *g.SearchPricePer1k,
				group.FieldAudioRealtimePricePerMin:     *g.AudioRealtimePricePerMin,
				group.FieldAudioTtsPricePerMillionChars: *g.AudioTTSPricePerMillionChars,
				group.FieldAudioSttPricePerHour:         *g.AudioSTTPricePerHour,
				group.FieldNewapiStyleInterfaceEnabled:  true,
				group.FieldCustomLimitHours:             g.CustomLimitHours, group.FieldCustomLimitUsd: *g.CustomLimitUSD,
				group.FieldPeakRateWindows: service.PeakRateWindowsForStorage(g.PeakRateWindows),
			}
			for field, want := range fields {
				got, ok := m.Field(field)
				require.True(t, ok, "missing %s", field)
				require.Equal(t, want, got, field)
			}
		})
	}
}

func TestGroupPersistenceContractClears(t *testing.T) {
	m := captureGroupMutation(t, &service.Group{ID: 7}, true)
	for _, field := range []string{
		group.FieldSearchPricePer1k, group.FieldAudioRealtimePricePerMin,
		group.FieldAudioTtsPricePerMillionChars, group.FieldAudioSttPricePerHour,
		group.FieldCustomLimitUsd,
	} {
		require.True(t, m.FieldCleared(field), "%s must reset to database NULL", field)
	}
	for _, field := range []string{group.FieldForceOpenaiFast, group.FieldFreeOpenaiFast, group.FieldLongContextPricingEnabled} {
		got, ok := m.Field(field)
		require.True(t, ok)
		require.Equal(t, false, got, field)
	}
	pricing, ok := m.Field(group.FieldModelPricing)
	require.True(t, ok)
	require.JSONEq(t, "null", string(pricing.(jsontext.Value)))
}

func TestGroupPersistenceContractRejectsInvalidPricing(t *testing.T) {
	bad := math.NaN()
	g := &service.Group{ID: 7, ModelPricing: []service.ChannelModelPricing{{InputPrice: &bad}}}
	client := dbent.NewClient()
	require.ErrorContains(t, createGroupRecord(context.Background(), client, g), "marshal group model pricing")
	require.ErrorContains(t, (&groupRepository{client: client}).Update(context.Background(), g), "marshal group model pricing")
}

func TestGroupPersistenceContractMapper(t *testing.T) {
	want := groupContractFixture()
	pricing, err := json.Marshal(want.ModelPricing)
	require.NoError(t, err)
	got := groupEntityToService(&dbent.Group{
		ModelAllowlist:   service.DomainGroupModelAllowlist(want.ModelAllowlist),
		ModelsListConfig: want.ModelsListConfig, CodexModelsManifestConfig: want.CodexModelsManifestConfig,
		ForceOpenaiFast: true, FreeOpenaiFast: true, MaxReasoningEffortOverLimit: want.MaxReasoningEffortOverLimit,
		LongContextPricingEnabled: true, ModelPricing: pricing, VideoModelPrices: want.VideoModelPrices,
		SearchPricePer1k: want.SearchPricePer1k, AudioRealtimePricePerMin: want.AudioRealtimePricePerMin,
		AudioTtsPricePerMillionChars: want.AudioTTSPricePerMillionChars, AudioSttPricePerHour: want.AudioSTTPricePerHour,
		NewapiStyleInterfaceEnabled: true, CustomLimitHours: want.CustomLimitHours, CustomLimitUsd: want.CustomLimitUSD,
		PeakStart: want.PeakStart, PeakEnd: want.PeakEnd, PeakRateMultiplier: want.PeakRateMultiplier,
		PeakRateWindows: service.PeakRateWindowsForStorage(want.PeakRateWindows),
	})
	require.Equal(t, want.ModelsListConfig, got.ModelsListConfig)
	require.Equal(t, want.ModelAllowlist, got.ModelAllowlist)
	require.Equal(t, want.CodexModelsManifestConfig, got.CodexModelsManifestConfig)
	require.True(t, got.ForceOpenAIFast && got.FreeOpenAIFast && got.LongContextPricingEnabled)
	require.Equal(t, want.MaxReasoningEffortOverLimit, got.MaxReasoningEffortOverLimit)
	require.Equal(t, want.ModelPricing, got.ModelPricing)
	require.Equal(t, want.VideoModelPrices, got.VideoModelPrices)
	require.Equal(t, want.SearchPricePer1k, got.SearchPricePer1k)
	require.Equal(t, want.AudioRealtimePricePerMin, got.AudioRealtimePricePerMin)
	require.Equal(t, want.AudioTTSPricePerMillionChars, got.AudioTTSPricePerMillionChars)
	require.Equal(t, want.AudioSTTPricePerHour, got.AudioSTTPricePerHour)
	require.True(t, got.NewAPIStyleInterfaceEnabled)
	require.Equal(t, want.CustomLimitHours, got.CustomLimitHours)
	require.Equal(t, want.CustomLimitUSD, got.CustomLimitUSD)
	require.Equal(t, want.PeakRateWindows, got.PeakRateWindows)
	require.Nil(t, groupEntityToService(&dbent.Group{ModelPricing: []byte("invalid")}).ModelPricing)
}
