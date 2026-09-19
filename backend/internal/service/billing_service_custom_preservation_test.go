package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type customPricingChannelRepo struct {
	ChannelRepository
	channels []Channel
	platform string
}

func (r *customPricingChannelRepo) ListAll(context.Context) ([]Channel, error) {
	return r.channels, nil
}

func (r *customPricingChannelRepo) GetGroupPlatforms(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{1: r.platform}, nil
}

func TestCustomChannelPriorityPricingPreservation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		model      string
		tier       string
		pricing    ChannelModelPricing
		wantInput  float64
		wantOutput float64
	}{
		{name: "flat priority", model: "gpt-5.4", tier: "priority", pricing: ChannelModelPricing{InputPrice: pricingMultiplier(1e-6), OutputPrice: pricingMultiplier(2e-6)}, wantInput: .001, wantOutput: .002},
		{name: "flat fast", model: "gpt-5.4", tier: "fast", pricing: ChannelModelPricing{InputPrice: pricingMultiplier(1e-6), OutputPrice: pricingMultiplier(2e-6)}, wantInput: .001, wantOutput: .002},
		{name: "partial override keeps catalog output", model: "gpt-5.4", tier: "priority", pricing: ChannelModelPricing{InputPrice: pricingMultiplier(1e-6)}, wantInput: .001, wantOutput: .030},
		{name: "interval explicit prices", model: "gpt-5.4", tier: "priority", pricing: ChannelModelPricing{Intervals: []PricingInterval{{MinTokens: 100, InputPrice: pricingMultiplier(3e-6), OutputPrice: pricingMultiplier(4e-6)}}}, wantInput: .003, wantOutput: .004},
		{name: "zero override", model: "gpt-5.4", tier: "priority", pricing: ChannelModelPricing{InputPrice: pricingMultiplier(0), OutputPrice: pricingMultiplier(2e-6)}, wantInput: 0, wantOutput: .002},
		{name: "explicit fast multiplier", model: "gpt-5.4", tier: "fast", pricing: ChannelModelPricing{InputPrice: pricingMultiplier(1e-6), OutputPrice: pricingMultiplier(2e-6), FastMultiplier: pricingMultiplier(3)}, wantInput: .003, wantOutput: .006},
		{name: "explicit priority multiplier", model: "gpt-5.4", tier: "priority", pricing: ChannelModelPricing{InputPrice: pricingMultiplier(1e-6), OutputPrice: pricingMultiplier(2e-6), FastMultiplier: pricingMultiplier(3)}, wantInput: .003, wantOutput: .006},
		{name: "catalog only", model: "gpt-5.4", tier: "priority", wantInput: .005, wantOutput: .030},
		{name: "non OpenAI explicit prices", model: "claude-sonnet-4", tier: "priority", pricing: ChannelModelPricing{InputPrice: pricingMultiplier(1e-6), OutputPrice: pricingMultiplier(2e-6)}, wantInput: .001, wantOutput: .002},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card := tc.pricing.Clone()
			card.Platform, card.Models, card.BillingMode = PlatformOpenAI, []string{tc.model}, BillingModeToken
			repo := &customPricingChannelRepo{platform: PlatformOpenAI, channels: []Channel{{
				ID: 1, Name: "custom", Status: StatusActive, GroupIDs: []int64{1}, ModelPricing: []ChannelModelPricing{card},
			}}}
			channels := NewChannelService(repo, nil, nil, nil, nil)
			billing := NewBillingService(&config.Config{}, nil)
			resolver := NewModelPricingResolver(channels, billing)
			group := &Group{ID: 1, Platform: PlatformOpenAI}
			resolved := resolver.Resolve(context.Background(), PricingInput{Model: tc.model, GroupID: &group.ID, Group: group})
			before := *resolved.BasePricing
			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: context.Background(), Model: tc.model, GroupID: &group.ID, Group: group,
				Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 1000}, RateMultiplier: 1.5,
				ServiceTier: tc.tier, Resolver: resolver, Resolved: resolved,
			})
			require.NoError(t, err)
			require.InDelta(t, tc.wantInput, cost.InputCost, 1e-12)
			require.InDelta(t, tc.wantOutput, cost.OutputCost, 1e-12)
			require.InDelta(t, (tc.wantInput+tc.wantOutput)*1.5, cost.ActualCost, 1e-12)
			require.Equal(t, before, *resolved.BasePricing, "billing must not mutate cached pricing")
		})
	}
}

func TestCustomChannelPriorityCachePricing(t *testing.T) {
	for _, interval := range []bool{false, true} {
		for _, optIn := range []bool{false, true} {
			card := ChannelModelPricing{CacheWritePrice: pricingMultiplier(3e-6), CacheReadPrice: pricingMultiplier(4e-6)}
			if interval {
				card.Intervals = []PricingInterval{{MinTokens: 100, CacheWritePrice: card.CacheWritePrice, CacheReadPrice: card.CacheReadPrice}}
				card.CacheWritePrice, card.CacheReadPrice = nil, nil
			}
			factor := 1.0
			if optIn {
				factor = 3
				card.FastMultiplier = &factor
			}
			for _, rate := range []float64{0, 1.5} {
				billing := NewBillingService(&config.Config{}, nil)
				resolver := NewModelPricingResolver(nil, billing)
				resolved := resolver.resolveConfiguredPricing(&card, "gpt-5.4", PricingSourceChannel)
				cost, err := billing.CalculateCostUnified(CostInput{
					Model: "gpt-5.4", Resolver: resolver, Resolved: resolved, ServiceTier: "priority", RateMultiplier: rate,
					Tokens: UsageTokens{CacheCreationTokens: 1000, CacheReadTokens: 1000},
				})
				require.NoError(t, err)
				require.InDelta(t, .003*factor, cost.CacheCreationCost, 1e-12)
				require.InDelta(t, .004*factor, cost.CacheReadCost, 1e-12)
				require.InDelta(t, .007*factor*rate, cost.ActualCost, 1e-12)
			}
		}
	}
}

type customPricingGroupRepo struct {
	GroupRepository
	groups  []Group
	created *Group
}

func (r *customPricingGroupRepo) ListActive(context.Context) ([]Group, error) {
	return r.groups, nil
}

func (r *customPricingGroupRepo) Create(_ context.Context, group *Group) error {
	r.created = group
	return nil
}

func TestCustomModelPlazaWiredChannelPricingSource(t *testing.T) {
	for _, withCatalog := range []bool{false, true} {
		name := "fallback pricing wired"
		if withCatalog {
			name = "catalog pricing wired"
		}
		t.Run(name, func(t *testing.T) {
			var catalog *PricingService
			if withCatalog {
				catalog = &PricingService{pricingData: map[string]*LiteLLMModelPricing{
					"gpt-5.4":      {Mode: "chat", InputCostPerToken: 9e-6, OutputCostPerToken: 90e-6},
					"catalog-only": {Mode: "chat", InputCostPerToken: 8e-6},
				}}
			}
			card := ChannelModelPricing{
				Platform: PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: BillingModeToken,
				InputPrice: pricingMultiplier(1e-6), OutputPrice: pricingMultiplier(2e-6),
				CacheWritePrice: pricingMultiplier(0), CacheWrite1hPrice: pricingMultiplier(4e-6),
				Intervals: []PricingInterval{{MinTokens: 10000, InputPrice: pricingMultiplier(3e-6)}},
			}
			repo := &customPricingChannelRepo{platform: PlatformOpenAI, channels: []Channel{{
				ID: 1, Name: "custom", Status: StatusActive, GroupIDs: []int64{1}, ModelPricing: []ChannelModelPricing{card},
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {"catalog-only": "catalog-only"}},
			}}}
			groups := &customPricingGroupRepo{groups: []Group{{ID: 1, Platform: PlatformOpenAI, RateMultiplier: .5}}}
			channels := NewChannelService(repo, groups, nil, catalog, nil)
			billing := NewBillingService(&config.Config{}, catalog)
			plaza := NewModelPlazaService(repo, groups, catalog, billing, NewModelPricingResolver(channels, billing))
			out, err := plaza.ListGroups(context.Background())
			require.NoError(t, err)
			require.Len(t, out, 1)
			models := make(map[string]PlazaModel)
			for _, model := range out[0].Models {
				models[model.Name] = model
			}
			require.Equal(t, officialPricingFromChannelPricingV1(&card), models["gpt-5.4"].OfficialPricing)
			require.Nil(t, models["catalog-only"].OfficialPricing, "catalog prices must not masquerade as configured channel prices")
			require.Equal(t, .5, out[0].RateMultiplier)
			require.Equal(t, card, repo.channels[0].ModelPricing[0])
		})
	}
}

func TestCustomGroupCreateAllowsZeroRate(t *testing.T) {
	for _, subscriptionType := range []string{SubscriptionTypeStandard, SubscriptionTypeSubscription} {
		t.Run(subscriptionType, func(t *testing.T) {
			repo := &customPricingGroupRepo{}
			admin := &adminServiceImpl{groupRepo: repo}
			group, err := admin.CreateGroup(context.Background(), &CreateGroupInput{
				Name: "free", Platform: PlatformAnthropic, SubscriptionType: subscriptionType, RateMultiplier: 0,
			})
			require.NoError(t, err)
			require.NotNil(t, group)
			require.Same(t, group, repo.created)
			require.Zero(t, group.RateMultiplier)
			require.Equal(t, subscriptionType, group.SubscriptionType)
		})
	}
	t.Run("negative rejected before persistence", func(t *testing.T) {
		repo := &customPricingGroupRepo{}
		admin := &adminServiceImpl{groupRepo: repo}
		_, err := admin.CreateGroup(context.Background(), &CreateGroupInput{Name: "invalid", RateMultiplier: -.1})
		require.Error(t, err)
		require.Nil(t, repo.created)
	})
}
