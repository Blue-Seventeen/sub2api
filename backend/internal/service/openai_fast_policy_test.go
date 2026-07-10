package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIFastPolicyRepoStub struct {
	values   map[string]string
	getValue func(ctx context.Context, key string) (string, error)
	setValue func(ctx context.Context, key, value string) error
}

func (s *openAIFastPolicyRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *openAIFastPolicyRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.values != nil {
		value, ok := s.values[key]
		if !ok {
			return "", ErrSettingNotFound
		}
		return value, nil
	}
	if s.getValue == nil {
		panic("unexpected GetValue call")
	}
	return s.getValue(ctx, key)
}

func (s *openAIFastPolicyRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values != nil {
		s.values[key] = value
		return nil
	}
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

func newOpenAIGatewayServiceWithSettings(t *testing.T, settings *OpenAIFastPolicySettings) *OpenAIGatewayService {
	t.Helper()
	resetOpenAIFastPolicySettingsCache(t)
	repo := &openAIFastPolicyRepoStub{values: map[string]string{}}
	if settings != nil {
		raw, err := json.Marshal(settings)
		require.NoError(t, err)
		repo.values[SettingKeyOpenAIFastPolicySettings] = string(raw)
	}
	return &OpenAIGatewayService{
		settingService: NewSettingService(repo, &config.Config{}),
	}
}

func openAIFastFilterPriorityPolicy() *OpenAIFastPolicySettings {
	return &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier:    OpenAIFastTierPriority,
			Action:         BetaPolicyActionFilter,
			Scope:          BetaPolicyScopeAll,
			ModelWhitelist: []string{},
			FallbackAction: BetaPolicyActionPass,
		}},
	}
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

func TestApplyOpenAIFastPolicyToBody_OfficialTiersBypassDefaultRule(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for _, tt := range []struct {
		in   string
		want string
	}{
		{in: "flex", want: OpenAIFastTierFlex},
		{in: "auto", want: OpenAIFastTierAuto},
		{in: "default", want: OpenAIFastTierDefault},
		{in: "scale", want: OpenAIFastTierScale},
		{in: "priority", want: OpenAIFastTierPriority},
		{in: "fast", want: OpenAIFastTierPriority},
	} {
		body := []byte(`{"model":"gpt-5.5","service_tier":"` + tt.in + `"}`)
		updated, _, err := svc.applyOpenAIFastPolicyToBody(context.Background(), account, "gpt-5.5", body)
		require.NoError(t, err)
		require.Equal(t, tt.want, gjson.GetBytes(updated, "service_tier").String())
	}
}

func TestOpenAIFastPolicyPreservesUnknownTier(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	body := []byte(`{"model":"gpt-5.1","service_tier":"turbo"}`)

	updated, _, err := svc.applyOpenAIFastPolicyToBody(context.Background(), nil, "gpt-5.1", body)

	require.NoError(t, err)
	require.Equal(t, string(body), string(updated))
}

func TestOpenAIFastPolicyAdminRuleCanBlockPriority(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
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

	_, _, err := svc.applyOpenAIFastPolicyToBody(ctx, nil, "gpt-5.1", body)

	var blocked *OpenAIFastBlockedError
	require.True(t, errors.As(err, &blocked))
	require.Equal(t, "priority disabled", blocked.Message)
}

func TestOpenAIFastPolicyAdminRuleCanFilterKnownTiers(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	for _, tier := range []string{OpenAIFastTierPriority, OpenAIFastTierFlex, OpenAIFastTierAuto, OpenAIFastTierDefault, OpenAIFastTierScale} {
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

			updated, _, err := svc.applyOpenAIFastPolicyToBody(ctx, nil, "gpt-5.1", body)

			require.NoError(t, err)
			require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
		})
	}
}

func TestApplyOpenAIFastPolicyToBody_ForcePriorityRewritesKnownTier(t *testing.T) {
	settings := &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier: OpenAIFastTierAny,
			Action:      OpenAIFastPolicyActionForcePriority,
			Scope:       BetaPolicyScopeAll,
		}},
	}
	svc := newOpenAIGatewayServiceWithSettings(t, settings)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	for _, tier := range []string{"flex", "auto", "default", "scale", "fast", "priority"} {
		body := []byte(`{"model":"gpt-5.5","service_tier":"` + tier + `"}`)
		updated, _, err := svc.applyOpenAIFastPolicyToBody(context.Background(), account, "gpt-5.5", body)
		require.NoError(t, err)
		require.Equal(t, OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String(),
			"tier %q should be forced to priority", tier)
	}
}

func TestNormalizeOpenAIFastPolicySettingsRejectsInvalidTier(t *testing.T) {
	_, err := normalizeOpenAIFastPolicySettings(&OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{
			{
				ServiceTier: "turbo",
				Action:      BetaPolicyActionFilter,
				Scope:       BetaPolicyScopeAll,
			},
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid service_tier")
}

func TestSettingServiceSetOpenAIFastPolicySettingsValidatesAndPersistsForcePriority(t *testing.T) {
	resetOpenAIFastPolicySettingsCache(t)
	repo := &openAIFastPolicyRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.SetOpenAIFastPolicySettings(context.Background(), &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier: "turbo",
			Action:      BetaPolicyActionPass,
			Scope:       BetaPolicyScopeAll,
		}},
	})
	require.Error(t, err)

	err = svc.SetOpenAIFastPolicySettings(context.Background(), &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier: OpenAIFastTierPriority,
			Action:      OpenAIFastPolicyActionForcePriority,
			Scope:       BetaPolicyScopeAll,
		}},
	})
	require.NoError(t, err)

	got, err := svc.GetOpenAIFastPolicySettings(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Rules, 1)
	require.Equal(t, OpenAIFastTierPriority, got.Rules[0].ServiceTier)
	require.Equal(t, OpenAIFastPolicyActionForcePriority, got.Rules[0].Action)
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
