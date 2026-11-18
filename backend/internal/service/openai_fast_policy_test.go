package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
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
	if s.values == nil {
		panic("unexpected GetMultiple call")
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
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

func TestEvaluateOpenAIFastPolicy_ScopeFiltersOAuth(t *testing.T) {
	settings := &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier: OpenAIFastTierAny,
			Action:      BetaPolicyActionFilter,
			Scope:       BetaPolicyScopeOAuth,
		}},
	}
	svc := newOpenAIGatewayServiceWithSettings(t, settings)

	oauthAccount := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	action, _ := svc.evaluateOpenAIFastPolicy(context.Background(), oauthAccount, "gpt-4", OpenAIFastTierPriority)
	require.Equal(t, BetaPolicyActionFilter, action)

	apiKeyAccount := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	action, _ = svc.evaluateOpenAIFastPolicy(context.Background(), apiKeyAccount, "gpt-4", OpenAIFastTierPriority)
	require.Equal(t, BetaPolicyActionPass, action)
}

func TestEvaluateOpenAIFastPolicy_UserScopedRuleOverridesGlobalRule(t *testing.T) {
	settings := &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{
			{
				ServiceTier: OpenAIFastTierPriority,
				Action:      BetaPolicyActionFilter,
				Scope:       BetaPolicyScopeAll,
			},
			{
				ServiceTier: OpenAIFastTierPriority,
				Action:      BetaPolicyActionPass,
				Scope:       BetaPolicyScopeAll,
				UserIDs:     []int64{42},
			},
		},
	}
	svc := newOpenAIGatewayServiceWithSettings(t, settings)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	allowedUserCtx := context.WithValue(context.Background(), ctxkey.UserID, int64(42))
	action, _ := svc.evaluateOpenAIFastPolicy(allowedUserCtx, account, "gpt-5.5", OpenAIFastTierPriority)
	require.Equal(t, BetaPolicyActionPass, action)

	otherUserCtx := context.WithValue(context.Background(), ctxkey.UserID, int64(43))
	action, _ = svc.evaluateOpenAIFastPolicy(otherUserCtx, account, "gpt-5.5", OpenAIFastTierPriority)
	require.Equal(t, BetaPolicyActionFilter, action)

	action, _ = svc.evaluateOpenAIFastPolicy(context.Background(), account, "gpt-5.5", OpenAIFastTierPriority)
	require.Equal(t, BetaPolicyActionFilter, action)
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

func TestApplyOpenAIFastPolicyToBody_UserScopedRuleOverridesGlobalRule(t *testing.T) {
	settings := &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{
			{
				ServiceTier: OpenAIFastTierPriority,
				Action:      BetaPolicyActionFilter,
				Scope:       BetaPolicyScopeAll,
			},
			{
				ServiceTier: OpenAIFastTierPriority,
				Action:      BetaPolicyActionPass,
				Scope:       BetaPolicyScopeAll,
				UserIDs:     []int64{42},
			},
		},
	}
	svc := newOpenAIGatewayServiceWithSettings(t, settings)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	body := []byte(`{"model":"gpt-5.5","service_tier":"priority"}`)

	allowedUserCtx := context.WithValue(context.Background(), ctxkey.UserID, int64(42))
	updated, _, err := svc.applyOpenAIFastPolicyToBody(allowedUserCtx, account, "gpt-5.5", body)
	require.NoError(t, err)
	require.Equal(t, "priority", gjson.GetBytes(updated, "service_tier").String())

	otherUserCtx := context.WithValue(context.Background(), ctxkey.UserID, int64(43))
	updated, _, err = svc.applyOpenAIFastPolicyToBody(otherUserCtx, account, "gpt-5.5", body)
	require.NoError(t, err)
	require.NotContains(t, string(updated), `"service_tier"`)
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

	// Non-positive and duplicate user IDs are rejected.
	err = svc.SetOpenAIFastPolicySettings(context.Background(), &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier: OpenAIFastTierPriority,
			Action:      BetaPolicyActionPass,
			Scope:       BetaPolicyScopeAll,
			UserIDs:     []int64{0},
		}},
	})
	require.Error(t, err)

	err = svc.SetOpenAIFastPolicySettings(context.Background(), &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier: OpenAIFastTierPriority,
			Action:      BetaPolicyActionPass,
			Scope:       BetaPolicyScopeAll,
			UserIDs:     []int64{42, 42},
		}},
	})
	require.Error(t, err)

	// Valid settings persisted
	err = svc.SetOpenAIFastPolicySettings(context.Background(), &OpenAIFastPolicySettings{
		Rules: []OpenAIFastPolicyRule{{
			ServiceTier: OpenAIFastTierPriority,
			Action:      OpenAIFastPolicyActionForcePriority,
			Scope:       BetaPolicyScopeAll,
			UserIDs:     []int64{42, 43},
		}},
	})
	require.NoError(t, err)

	got, err := svc.GetOpenAIFastPolicySettings(context.Background())
	require.NoError(t, err)
	require.Len(t, got.Rules, 1)
	require.Equal(t, OpenAIFastTierPriority, got.Rules[0].ServiceTier)
	require.Equal(t, OpenAIFastPolicyActionForcePriority, got.Rules[0].Action)
	require.Equal(t, []int64{42, 43}, got.Rules[0].UserIDs)
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

func openAIUltrafastGroupContext() context.Context {
	return context.WithValue(context.Background(), ctxkey.Group, &Group{
		ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, ForceOpenAIUltrafast: true,
	})
}

func TestApplyOpenAIFastPolicyToBody_GroupForceUltrafastInjectsAndOverridesTier(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	for _, body := range [][]byte{
		[]byte(`{"model":"gpt-6.1-sol","input":"hi"}`),
		[]byte(`{"model":"gpt-6.1-sol","service_tier":"default"}`),
		[]byte(`{"model":"gpt-6.1-sol","service_tier":"fast"}`),
		[]byte(`{"model":"gpt-6.1-sol","service_tier":"priority"}`),
		[]byte(`{"model":"gpt-6.1-sol","service_tier":"flex"}`),
		[]byte(`{"model":"gpt-6.1-sol","service_tier":"ultrafast"}`),
		[]byte(`{"model":"gpt-6.1-sol","service_tier":"client-unknown"}`),
	} {
		updated, _, err := svc.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, "gpt-6.1-sol", body)
		require.NoError(t, err)
		require.Equal(t, OpenAIFastTierUltrafast, gjson.GetBytes(updated, "service_tier").String(), "body %s", body)
	}
}

// 全局策略在分组强制之后裁决：针对 ultrafast 的 filter / block / force_priority
// 规则必须作用在分组写入的 ultrafast 上，而只针对 priority 的规则不应误伤。
func TestApplyOpenAIFastPolicyToBody_GroupForceUltrafastStillHonorsGlobalPolicy(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	body := []byte(`{"model":"gpt-6.1-sol"}`)
	rule := func(tier, action string) *OpenAIFastPolicySettings {
		return &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
			ServiceTier: tier, Action: action, Scope: BetaPolicyScopeAll, ErrorMessage: "ultrafast disabled",
		}}}
	}

	t.Run("ultrafast filter strips the forced tier", func(t *testing.T) {
		svc := newOpenAIGatewayServiceWithSettings(t, rule(OpenAIFastTierUltrafast, BetaPolicyActionFilter))
		updated, _, err := svc.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, "gpt-6.1-sol", body)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(updated, "service_tier").Exists())
	})

	t.Run("ultrafast block rejects the request", func(t *testing.T) {
		svc := newOpenAIGatewayServiceWithSettings(t, rule(OpenAIFastTierUltrafast, BetaPolicyActionBlock))
		_, _, err := svc.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, "gpt-6.1-sol", body)
		var blocked *OpenAIFastBlockedError
		require.ErrorAs(t, err, &blocked)
		require.Equal(t, "ultrafast disabled", blocked.Message)
	})

	t.Run("ultrafast force_priority downgrades to priority", func(t *testing.T) {
		svc := newOpenAIGatewayServiceWithSettings(t, rule(OpenAIFastTierUltrafast, OpenAIFastPolicyActionForcePriority))
		updated, _, err := svc.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, "gpt-6.1-sol", body)
		require.NoError(t, err)
		require.Equal(t, OpenAIFastTierPriority, gjson.GetBytes(updated, "service_tier").String())
	})

	t.Run("priority-only filter leaves ultrafast alone", func(t *testing.T) {
		svc := newOpenAIGatewayServiceWithSettings(t, openAIFastFilterPriorityPolicy())
		updated, _, err := svc.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, "gpt-6.1-sol", body)
		require.NoError(t, err)
		require.Equal(t, OpenAIFastTierUltrafast, gjson.GetBytes(updated, "service_tier").String())
	})

	t.Run("model whitelist fallback filters unsupported models", func(t *testing.T) {
		svc := newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{
			ServiceTier: OpenAIFastTierUltrafast, Action: BetaPolicyActionPass, Scope: BetaPolicyScopeAll,
			ModelWhitelist: []string{"gpt-6.1-sol"}, FallbackAction: BetaPolicyActionFilter,
		}}})
		supported, _, err := svc.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, "gpt-6.1-sol", body)
		require.NoError(t, err)
		require.Equal(t, OpenAIFastTierUltrafast, gjson.GetBytes(supported, "service_tier").String())

		// Astra 支持 Ultrafast，分组会写入；不在白名单内，由 fallback 过滤掉。
		notWhitelisted, _, err := svc.applyOpenAIFastPolicyToBody(
			openAIUltrafastGroupContext(), account, "gpt-6-astra", []byte(`{"model":"gpt-6-astra"}`))
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(notWhitelisted, "service_tier").Exists())
	})
}

func TestOpenAIModelSupportsUltrafast(t *testing.T) {
	for _, model := range []string{
		"gpt-6-astra", "gpt-6", "GPT-6-Astra", "openai/gpt-6-astra",
		"gpt-6.1-sol", "gpt-6.1-sol-high", "openai/gpt-6.1-sol", "gpt-6.1-sol-openai-compact",
	} {
		require.True(t, openAIModelSupportsUltrafast(model), model)
	}
	for _, model := range []string{
		"", "gpt-6.1-terra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.5", "gpt-5.4-mini",
		"gpt-6.1-solar", "grok-4", "claude-opus-5-5",
	} {
		require.False(t, openAIModelSupportsUltrafast(model), model)
	}
}

func TestOpenAIGroupForcedServiceTier(t *testing.T) {
	openAIAccount := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	const sol, astra, unsupported = "gpt-6.1-sol", "gpt-6-astra", "gpt-5.6-sol"
	group := func(platform string, fast, ultrafast bool) context.Context {
		return context.WithValue(context.Background(), ctxkey.Group, &Group{
			ID: 7, Platform: platform, Status: StatusActive, Hydrated: true,
			ForceOpenAIFast: fast, ForceOpenAIUltrafast: ultrafast,
		})
	}
	for _, tt := range []struct {
		name    string
		ctx     context.Context
		account *Account
		model   string
		want    string
	}{
		{name: "no group", ctx: context.Background(), account: openAIAccount, model: sol, want: ""},
		{name: "nil account", ctx: group(PlatformOpenAI, true, true), account: nil, model: sol, want: ""},
		{name: "follow client", ctx: group(PlatformOpenAI, false, false), account: openAIAccount, model: sol, want: ""},

		{name: "fast only, ultrafast-capable model", ctx: group(PlatformOpenAI, true, false), account: openAIAccount, model: sol, want: OpenAIFastTierPriority},
		{name: "fast only, other model", ctx: group(PlatformOpenAI, true, false), account: openAIAccount, model: unsupported, want: OpenAIFastTierPriority},
		{name: "fast only, empty model", ctx: group(PlatformOpenAI, true, false), account: openAIAccount, model: "", want: OpenAIFastTierPriority},

		{name: "ultrafast only, sol", ctx: group(PlatformOpenAI, false, true), account: openAIAccount, model: sol, want: OpenAIFastTierUltrafast},
		{name: "ultrafast only, astra", ctx: group(PlatformOpenAI, false, true), account: openAIAccount, model: astra, want: OpenAIFastTierUltrafast},
		{name: "ultrafast only, sol effort alias", ctx: group(PlatformOpenAI, false, true), account: openAIAccount, model: "gpt-6.1-sol-high", want: OpenAIFastTierUltrafast},
		{name: "ultrafast only, unsupported model follows client", ctx: group(PlatformOpenAI, false, true), account: openAIAccount, model: unsupported, want: ""},
		{name: "ultrafast only, empty model follows client", ctx: group(PlatformOpenAI, false, true), account: openAIAccount, model: "", want: ""},

		{name: "both, supported model gets ultrafast", ctx: group(PlatformOpenAI, true, true), account: openAIAccount, model: sol, want: OpenAIFastTierUltrafast},
		{name: "both, unsupported model falls back to fast", ctx: group(PlatformOpenAI, true, true), account: openAIAccount, model: unsupported, want: OpenAIFastTierPriority},

		{name: "composite group with openai account", ctx: group(PlatformComposite, false, true), account: openAIAccount, model: sol, want: OpenAIFastTierUltrafast},
		{name: "composite group with grok account", ctx: group(PlatformComposite, true, true), account: &Account{Platform: PlatformGrok}, model: sol, want: ""},
		{name: "unsupported group platform", ctx: group(PlatformAnthropic, true, true), account: openAIAccount, model: sol, want: ""},
		{name: "unhydrated group", ctx: context.WithValue(context.Background(), ctxkey.Group, &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, ForceOpenAIFast: true, ForceOpenAIUltrafast: true}), account: openAIAccount, model: sol, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, openAIGroupForcedServiceTier(tt.ctx, tt.account, tt.model))
		})
	}
}

// 仅强制 Ultra Fast 的分组：不支持的模型保留客户端传入的档位（含省略）。
func TestApplyOpenAIFastPolicyToBody_GroupForceUltrafastSkipsUnsupportedModels(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	for _, tt := range []struct {
		body string
		want string
	}{
		{body: `{"model":"gpt-5.6-sol","input":"hi"}`, want: ""},
		{body: `{"model":"gpt-5.6-sol","service_tier":"fast"}`, want: OpenAIFastTierPriority},
		{body: `{"model":"gpt-5.6-sol","service_tier":"flex"}`, want: OpenAIFastTierFlex},
		{body: `{"model":"gpt-5.6-sol","service_tier":"ultrafast"}`, want: OpenAIFastTierUltrafast},
	} {
		updated, _, err := svc.applyOpenAIFastPolicyToBody(openAIUltrafastGroupContext(), account, "gpt-5.6-sol", []byte(tt.body))
		require.NoError(t, err)
		require.Equal(t, tt.want, gjson.GetBytes(updated, "service_tier").String(), "body %s", tt.body)
	}
}

// 两个开关同时开启：支持的模型写 ultrafast，其余模型写 priority。
func TestApplyOpenAIFastPolicyToBody_GroupForceFastAndUltrafastTogether(t *testing.T) {
	svc := newOpenAIGatewayServiceWithSettings(t, DefaultOpenAIFastPolicySettings())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	ctx := context.WithValue(context.Background(), ctxkey.Group, &Group{
		ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, ForceOpenAIFast: true, ForceOpenAIUltrafast: true,
	})

	for _, tt := range []struct {
		model string
		want  string
	}{
		{model: "gpt-6.1-sol", want: OpenAIFastTierUltrafast},
		{model: "gpt-6-astra", want: OpenAIFastTierUltrafast},
		{model: "gpt-5.6-sol", want: OpenAIFastTierPriority},
		{model: "gpt-6.1-terra", want: OpenAIFastTierPriority},
	} {
		for _, body := range []string{
			`{"model":"` + tt.model + `"}`,
			`{"model":"` + tt.model + `","service_tier":"flex"}`,
			`{"model":"` + tt.model + `","service_tier":"ultrafast"}`,
		} {
			updated, _, err := svc.applyOpenAIFastPolicyToBody(ctx, account, tt.model, []byte(body))
			require.NoError(t, err)
			require.Equal(t, tt.want, gjson.GetBytes(updated, "service_tier").String(), "body %s", body)
		}
	}
}
