//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewGrokAutomaticAccountRoutesAllTransports(t *testing.T) {
	for _, state := range []string{"healthy", "cold", "disabled"} {
		for _, route := range []string{"responses", "media", "voice", "search", "models", "quota", "realtime", "realtime_probe"} {
			t.Run(state+"/"+route, func(t *testing.T) {
				auto, chosen := gatewayProxyFixture(t)
				if state == "cold" {
					auto.entries = map[int64]*proxyAutoProbeEntry{}
				}
				if state == "disabled" {
					auto.config.Enabled = false
				}
				account := &Account{ID: 42, Platform: PlatformGrok, Type: AccountTypeAPIKey, Concurrency: 1, Extra: map[string]any{"auto_select_proxy": true}, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://api.x.ai/v1"}}
				upstream := &httpUpstreamRecorder{err: errors.New("fixture stop after capturing route")}
				cfg := &config.Config{}
				svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: cfg}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
				var captured string
				switch route {
				case "responses":
					_, _ = svc.doOpenAIUpstream(c.Request, "", account)
					captured = upstream.lastProxyURL
				case "media":
					_, _ = svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointImagesGenerations, "", []byte(`{"model":"grok-imagine-image","prompt":"fixture"}`), "application/json", "")
					require.NotNil(t, upstream.lastReq)
					captured = upstream.lastProxyURL
				case "voice":
					_, _ = svc.ForwardGrokVoice(context.Background(), c, account, "tts", []byte(`{"text":"fixture"}`), "application/json")
					require.NotNil(t, upstream.lastReq)
					captured = upstream.lastProxyURL
				case "search":
					search := &GatewayService{httpUpstream: upstream, cfg: cfg}
					_, _ = search.DoGrokNativeResponsesJSON(context.Background(), account, []byte(`{"model":"grok-4.3","input":"fixture"}`))
					require.NotNil(t, upstream.lastReq)
					captured = upstream.lastProxyURL
				case "models":
					account.Credentials["access_token"] = "fixture"
					quota := &GrokQuotaService{httpUpstream: upstream, cfg: cfg}
					_ = quota.syncGrokObservedModels(context.Background(), account)
					require.NotNil(t, upstream.lastReq)
					captured = upstream.lastProxyURL
				case "quota":
					quota := &GrokQuotaService{}
					captured = quota.resolveProxyURL(context.Background(), account)
				case "realtime", "realtime_probe":
					dialer := &grokRealtimeTestDialer{}
					svc.openaiWSPassthroughDialer = dialer
					var err error
					if route == "realtime" {
						_, err = svc.OpenGrokRealtime(context.Background(), account, "fixture", "grok-voice-latest")
					} else {
						err = svc.ProbeGrokRealtime(context.Background(), account, "fixture", "grok-voice-latest")
					}
					if state != "healthy" {
						require.Error(t, err)
						require.Empty(t, dialer.lastURL)
						require.Nil(t, account.ProxyID)
						return
					}
					require.NoError(t, err)
					captured = dialer.lastProxy
				}
				switch state {
				case "healthy":
					require.Equal(t, ResolveProxyURL(context.Background(), &chosen), captured)
				case "disabled":
					require.Equal(t, proxyurl.AutoProxyDisabledURL, captured)
				default:
					require.Equal(t, proxyurl.AutoProxyUnavailableURL, captured)
				}
				require.Nil(t, account.ProxyID)
			})
		}
	}
}
