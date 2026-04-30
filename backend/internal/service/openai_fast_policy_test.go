package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIFastPolicyRepoStub struct {
	getValue func(ctx context.Context, key string) (string, error)
	setValue func(ctx context.Context, key, value string) error
}

func (s *openAIFastPolicyRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *openAIFastPolicyRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.getValue == nil {
		panic("unexpected GetValue call")
	}
	return s.getValue(ctx, key)
}

func (s *openAIFastPolicyRepoStub) Set(ctx context.Context, key, value string) error {
	if s.setValue == nil {
		panic("unexpected Set call")
	}
	return s.setValue(ctx, key, value)
}

func (s *openAIFastPolicyRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *openAIFastPolicyRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *openAIFastPolicyRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *openAIFastPolicyRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func resetOpenAIFastPolicySettingsCache(t *testing.T) {
	t.Helper()
	openAIFastPolicySettingsSF.Forget("openai_fast_policy")
	openAIFastPolicySettingsCache.Store((*cachedOpenAIFastPolicySettings)(nil))
	t.Cleanup(func() {
		openAIFastPolicySettingsSF.Forget("openai_fast_policy")
		openAIFastPolicySettingsCache.Store((*cachedOpenAIFastPolicySettings)(nil))
	})
}

func TestOpenAIFastPolicyDefaultsToEmptyAdminRules(t *testing.T) {
	settings := DefaultOpenAIFastPolicySettings()
	require.NotNil(t, settings)
	require.Empty(t, settings.Rules)
}

func TestOpenAIFastPolicyFlexAlwaysFiltersToStandard(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.1","service_tier":"flex"}`)

	updated, tier, err := svc.applyOpenAIFastPolicyToBody(context.Background(), nil, "gpt-5.1", body)

	require.NoError(t, err)
	require.Nil(t, tier)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

func TestOpenAIFastPolicyKnownNonFlexTierPassesByDefault(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.1","service_tier":"default"}`)

	updated, tier, err := svc.applyOpenAIFastPolicyToBody(context.Background(), nil, "gpt-5.1", body)

	require.NoError(t, err)
	require.NotNil(t, tier)
	require.Equal(t, "default", *tier)
	require.Equal(t, "default", gjson.GetBytes(updated, "service_tier").String())
}

func TestOpenAIFastPolicyAdminRuleCanBlockPriority(t *testing.T) {
	svc := &OpenAIGatewayService{settingService: &SettingService{}}
	ctx := withOpenAIFastPolicyContext(context.Background(), &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{
			{
				ServiceTier:  OpenAIFastTierPriority,
				Action:       BetaPolicyActionBlock,
				Scope:        BetaPolicyScopeAll,
				ErrorMessage: "priority disabled",
			},
		},
	})
	body := []byte(`{"model":"gpt-5.1","service_tier":"priority"}`)

	_, tier, err := svc.applyOpenAIFastPolicyToBody(ctx, nil, "gpt-5.1", body)

	require.Nil(t, tier)
	var blocked *OpenAIFastBlockedError
	require.True(t, errors.As(err, &blocked))
	require.Equal(t, "priority disabled", blocked.Message)
}

func TestOpenAIFastPolicyAdminRuleCanFilterKnownTiers(t *testing.T) {
	svc := &OpenAIGatewayService{settingService: &SettingService{}}
	for _, tier := range []string{OpenAIFastTierPriority, OpenAIFastTierAuto, OpenAIFastTierDefault, OpenAIFastTierScale} {
		t.Run(tier, func(t *testing.T) {
			ctx := withOpenAIFastPolicyContext(context.Background(), &OpenAIFastPolicySettings{
				Rules: []OpenAIFastPolicyRule{
					{
						ServiceTier: tier,
						Action:      BetaPolicyActionFilter,
						Scope:       BetaPolicyScopeAll,
					},
				},
			})
			body := []byte(`{"model":"gpt-5.1","service_tier":"` + tier + `"}`)

			updated, effectiveTier, err := svc.applyOpenAIFastPolicyToBody(ctx, nil, "gpt-5.1", body)

			require.NoError(t, err)
			require.Nil(t, effectiveTier)
			require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
		})
	}
}

func TestOpenAIFastPolicyAdminRuleDoesNotOverrideFlexDowngrade(t *testing.T) {
	svc := &OpenAIGatewayService{settingService: &SettingService{}}
	ctx := withOpenAIFastPolicyContext(context.Background(), &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{
			{
				ServiceTier:  OpenAIFastTierFlex,
				Action:       BetaPolicyActionBlock,
				Scope:        BetaPolicyScopeAll,
				ErrorMessage: "flex disabled",
			},
		},
	})
	body := []byte(`{"model":"gpt-5.1","service_tier":"flex"}`)

	updated, tier, err := svc.applyOpenAIFastPolicyToBody(ctx, nil, "gpt-5.1", body)

	require.NoError(t, err)
	require.Nil(t, tier)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

func TestOpenAIFastPolicyStripsUnknownTier(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.1","service_tier":"turbo"}`)

	updated, tier, err := svc.applyOpenAIFastPolicyToBody(context.Background(), nil, "gpt-5.1", body)

	require.NoError(t, err)
	require.Nil(t, tier)
	require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
}

func TestNormalizeOpenAIFastPolicySettingsRejectsFlexRules(t *testing.T) {
	_, err := normalizeOpenAIFastPolicySettings(&OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{
			{
				ServiceTier: OpenAIFastTierFlex,
				Action:      BetaPolicyActionFilter,
				Scope:       BetaPolicyScopeAll,
			},
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid service_tier")
}

func TestGetOpenAIFastPolicySettingsUsesStaleCacheOnRefreshError(t *testing.T) {
	resetOpenAIFastPolicySettingsCache(t)

	staleSettings := &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{
			{
				ServiceTier: OpenAIFastTierPriority,
				Action:      BetaPolicyActionBlock,
				Scope:       BetaPolicyScopeAll,
			},
		},
	}
	staleJSON, err := json.Marshal(staleSettings)
	require.NoError(t, err)

	calls := 0
	repo := &openAIFastPolicyRepoStub{
		getValue: func(ctx context.Context, key string) (string, error) {
			require.Equal(t, SettingKeyOpenAIFastPolicySettings, key)
			calls++
			if calls == 1 {
				return string(staleJSON), nil
			}
			return "", errors.New("database unavailable")
		},
	}
	svc := &SettingService{settingRepo: repo}

	first, err := svc.GetOpenAIFastPolicySettings(context.Background())
	require.NoError(t, err)
	require.Len(t, first.Rules, 1)
	require.Equal(t, OpenAIFastTierPriority, first.Rules[0].ServiceTier)

	openAIFastPolicySettingsCache.Store(&cachedOpenAIFastPolicySettings{
		settings:  first,
		expiresAt: time.Now().Add(-time.Second).UnixNano(),
	})

	second, err := svc.GetOpenAIFastPolicySettings(context.Background())
	require.NoError(t, err)
	require.Len(t, second.Rules, 1)
	require.Equal(t, OpenAIFastTierPriority, second.Rules[0].ServiceTier)
	require.Equal(t, 2, calls)
}
