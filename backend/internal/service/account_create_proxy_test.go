package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

type createProxyAccountRepo struct {
	AccountRepository
	created []*Account
	binds   int
}

func TestOAuthCreationDoesNotDropMissingProxy(t *testing.T) {
	repo := &proxyAutoProbeRepoStub{}
	id := int64(17)
	for _, platform := range []string{"anthropic", "gemini", "antigravity"} {
		t.Run(platform, func(t *testing.T) {
			var err error
			switch platform {
			case "anthropic":
				_, err = NewOAuthService(repo, nil).GenerateAuthURL(context.Background(), &id)
			case "gemini":
				svc := NewGeminiOAuthService(repo, nil, nil, nil, &config.Config{})
				_, err = svc.GenerateAuthURL(context.Background(), &id, "", "", "code_assist", "")
			case "antigravity":
				_, err = NewAntigravityOAuthService(repo).GenerateAuthURL(context.Background(), &id)
			}
			require.Error(t, err, "a selected proxy disappearing must not authorize a direct connection")
		})
	}
}

func TestResolveAccountCreationAuthProxy(t *testing.T) {
	previous := GetDefaultProxyAutoProbeService()
	t.Cleanup(func() { SetDefaultProxyAutoProbeService(previous) })
	for _, state := range []string{"missing_service", "disabled", "cold", "no_healthy", "healthy"} {
		t.Run(state, func(t *testing.T) {
			svc := NewProxyAutoProbeService(nil, &proxyAutoProbeRepoStub{}, nil, nil)
			svc.config.Enabled = state != "disabled"
			latency := int64(10)
			svc.proxySnapshots[17] = Proxy{ID: 17, Status: StatusActive}
			if state == "healthy" {
				svc.entries[17] = &proxyAutoProbeEntry{ProxyID: 17, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency}
			}
			if state == "no_healthy" {
				svc.entries[17] = &proxyAutoProbeEntry{ProxyID: 17, Queue: ProxyAutoProbeQueueFailed}
			}
			if state == "missing_service" {
				SetDefaultProxyAutoProbeService(nil)
			} else {
				SetDefaultProxyAutoProbeService(svc)
			}
			id, err := ResolveAccountCreationAuthProxyID(context.Background(), nil, map[string]any{"auto_select_proxy": true})
			if state == "healthy" {
				require.NoError(t, err)
				require.Equal(t, int64(17), *id)
			} else {
				require.Error(t, err)
				require.Equal(t, http.StatusServiceUnavailable, infraerrors.Code(err))
				require.Nil(t, id)
			}
		})
	}
}

func TestAccountAuthorizationRejectsUnavailableManagedRuntime(t *testing.T) {
	previous := GetDefaultManagedProxyResolver()
	SetDefaultManagedProxyResolver(managedProxyNoopResolver{})
	t.Cleanup(func() { SetDefaultManagedProxyResolver(previous) })
	id, subID := int64(17), int64(7)
	account := &Account{ProxyID: &id, Proxy: &Proxy{ID: id, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID}}
	url, err := resolveAccountAuthorizationProxyURL(context.Background(), account, nil)
	require.Error(t, err)
	require.Empty(t, url)
}

func (r *createProxyAccountRepo) Create(_ context.Context, a *Account) error {
	a.ID = 42
	r.created = append(r.created, a)
	return nil
}
func (r *createProxyAccountRepo) BindGroups(context.Context, int64, []int64) error {
	r.binds++
	return nil
}

func TestCreateAccountProxyConfiguration(t *testing.T) {
	previous := GetDefaultProxyAutoProbeService()
	SetDefaultProxyAutoProbeService(nil)
	t.Cleanup(func() { SetDefaultProxyAutoProbeService(previous) })
	for _, tc := range []struct {
		name   string
		id     *int64
		extra  map[string]any
		reason string
	}{
		{name: "none"},
		{name: "manual", id: func() *int64 { v := int64(17); return &v }()},
		{name: "auto_cold_start", extra: map[string]any{"auto_select_proxy": true, "quota_limit": 42.0}},
		{name: "zero", id: func() *int64 { v := int64(0); return &v }(), reason: "INVALID_PROXY_ID"},
		{name: "sentinel", id: func() *int64 { v := int64(-1); return &v }(), reason: "INVALID_PROXY_ID"},
		{name: "malformed_auto", extra: map[string]any{"auto_select_proxy": "true"}, reason: "INVALID_AUTO_SELECT_PROXY"},
		{name: "null_auto", extra: map[string]any{"auto_select_proxy": nil}, reason: "INVALID_AUTO_SELECT_PROXY"},
		{name: "conflicting_modes", id: func() *int64 { v := int64(17); return &v }(), extra: map[string]any{"auto_select_proxy": true}, reason: "PROXY_MODE_CONFLICT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &createProxyAccountRepo{}
			svc := &adminServiceImpl{accountRepo: repo}
			account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{Name: "fixture", Platform: PlatformDeepSeek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}, ProxyID: tc.id, Extra: tc.extra, SkipDefaultGroupBind: true, SkipMixedChannelCheck: true})
			if tc.reason != "" {
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				require.Equal(t, tc.reason, infraerrors.Reason(err))
				require.Nil(t, account)
				require.Empty(t, repo.created)
				require.Zero(t, repo.binds)
				return
			}
			require.NoError(t, err)
			require.Len(t, repo.created, 1)
			require.Equal(t, tc.id, account.ProxyID)
			if tc.name == "auto_cold_start" {
				require.True(t, account.IsAutoSelectProxyEnabled())
				require.Equal(t, 42.0, account.Extra["quota_limit"])
			}
		})
	}
}
