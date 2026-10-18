//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewAutomaticAccountPrivacyUsesProxyAndWaitsForHealth(t *testing.T) {
	previous := GetDefaultProxyAutoProbeService()
	t.Cleanup(func() { SetDefaultProxyAutoProbeService(previous) })
	for _, healthy := range []bool{false, true} {
		for _, force := range []bool{false, true} {
			t.Run(func() string {
				if healthy {
					return "healthy"
				}
				return "cold"
			}()+func() string {
				if force {
					return "_force"
				}
				return "_ensure"
			}(), func(t *testing.T) {
				auto := NewProxyAutoProbeService(nil, &proxyAutoProbeRepoStub{}, nil, nil)
				auto.config.Enabled = true
				auto.proxySnapshots[17] = Proxy{ID: 17, Status: StatusActive, Protocol: "http", Host: "proxy.fixture", Port: 3128}
				latency := int64(10)
				if healthy {
					auto.entries[17] = &proxyAutoProbeEntry{ProxyID: 17, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency}
				}
				SetDefaultProxyAutoProbeService(auto)
				var urls []string
				svc := &adminServiceImpl{accountRepo: &mockAccountRepoForGemini{}, privacyClientFactory: func(u string) (*req.Client, error) {
					urls = append(urls, u)
					return nil, errors.New("fixture factory failure")
				}}
				account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"auto_select_proxy": true}, Credentials: map[string]any{"access_token": "fixture"}}
				if force {
					svc.ForceOpenAIPrivacy(context.Background(), account)
				} else {
					svc.EnsureOpenAIPrivacy(context.Background(), account)
				}
				if healthy {
					require.Equal(t, []string{"http://proxy.fixture:3128"}, urls)
				} else {
					require.Empty(t, urls)
				}
				require.Nil(t, account.ProxyID)
			})
		}
	}
}
