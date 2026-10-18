//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
	"testing"
)

type grokProxyRefreshClient struct {
	grokOAuthClientStub
	urls []string
}

func (c *grokProxyRefreshClient) RefreshToken(_ context.Context, _ string, proxyURL string, _ string) (*xai.TokenResponse, error) {
	c.urls = append(c.urls, proxyURL)
	return &xai.TokenResponse{AccessToken: "fixture-new", ExpiresIn: 3600}, nil
}

func TestGrokAutomaticProxyRefreshRoutesAndFailsClosed(t *testing.T) {
	previous := GetDefaultProxyAutoProbeService()
	t.Cleanup(func() { SetDefaultProxyAutoProbeService(previous) })
	for _, state := range []string{"disabled", "cold", "healthy"} {
		t.Run(state, func(t *testing.T) {
			auto := NewProxyAutoProbeService(nil, &proxyAutoProbeRepoStub{}, nil, nil)
			auto.config.Enabled = state != "disabled"
			auto.proxySnapshots[17] = Proxy{ID: 17, Status: StatusActive, Protocol: "http", Host: "proxy.fixture", Port: 3128}
			latency := int64(10)
			if state == "healthy" {
				auto.entries[17] = &proxyAutoProbeEntry{ProxyID: 17, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency}
			}
			SetDefaultProxyAutoProbeService(auto)
			client := &grokProxyRefreshClient{}
			svc := NewGrokOAuthService(nil, client)
			t.Cleanup(svc.Stop)
			account := &Account{ID: 42, Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: map[string]any{"auto_select_proxy": true}, Credentials: map[string]any{"refresh_token": "fixture-refresh"}}
			_, err := svc.RefreshAccountToken(context.Background(), account)
			if state == "healthy" {
				require.NoError(t, err)
				require.Equal(t, []string{"http://proxy.fixture:3128"}, client.urls)
			} else {
				require.Error(t, err)
				require.Empty(t, client.urls)
			}
			require.Nil(t, account.ProxyID, "runtime selection must not persist a fixed binding")
		})
	}
}
