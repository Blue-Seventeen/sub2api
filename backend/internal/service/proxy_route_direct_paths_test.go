package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Keep the service's real request building and routing, replacing only the
// network boundary. It records attempts exactly where HTTPUpstream does.
type proxyRouteDirectUpstream struct{ recorder httpUpstreamRecorder }

func (u *proxyRouteDirectUpstream) Do(req *http.Request, raw string, accountID int64, concurrency int) (*http.Response, error) {
	attempt := BeginProxyRouteAttempt(req.Context(), accountID, raw, false)
	resp, err := u.recorder.Do(req, raw, accountID, concurrency)
	if err != nil {
		attempt.End()
		return nil, err
	}
	resp.Body = attempt.WrapBody(resp.Body)
	return resp, nil
}

func (u *proxyRouteDirectUpstream) DoWithTLS(req *http.Request, raw string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, raw, accountID, concurrency)
}

func TestProxyRouteDirectHTTPServicePaths(t *testing.T) {
	cases := []struct {
		name, response string
		run            func(context.Context, *gin.Context, *OpenAIGatewayService, *Account) error
	}{
		{
			name: "embeddings", response: `{"data":[],"usage":{"prompt_tokens":1,"total_tokens":1}}`,
			run: func(ctx context.Context, c *gin.Context, svc *OpenAIGatewayService, a *Account) error {
				_, err := svc.ForwardEmbeddings(ctx, c, a, []byte(`{"model":"embed","input":"hello"}`), "")
				return err
			},
		},
		{
			name: "count tokens", response: `{"object":"response.input_tokens","input_tokens":3}`,
			run: func(ctx context.Context, c *gin.Context, svc *OpenAIGatewayService, a *Account) error {
				return svc.ForwardCountTokensAsAnthropic(ctx, c, a, []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`), "gpt-5.4")
			},
		},
		{
			name: "seedance", response: `{"id":"fixture-task"}`,
			run: func(ctx context.Context, c *gin.Context, svc *OpenAIGatewayService, a *Account) error {
				a.Credentials["openai_capabilities"] = []string{"seedance"}
				_, err := svc.ForwardSeedance(ctx, c, a, SeedanceEndpointCreate, "", []byte(`{"model":"video","content":[{"type":"text","text":"waves"}]}`))
				return err
			},
		},
		{
			name: "image download", response: string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}),
			run: func(ctx context.Context, _ *gin.Context, svc *OpenAIGatewayService, a *Account) error {
				_, err := svc.fetchOpenAIImageURLBase64(ctx, a, "https://cdn.example.com/a.png")
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, observation := NewProxyRouteObservation(t.Context(), nil)
			defer observation.Close()
			proxyID := int64(71)
			account := &Account{ID: 52, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, ProxyID: &proxyID,
				Proxy:       &Proxy{ID: 71, Name: "fixed-node", Protocol: "socks5", Host: "2001:db8::7", Port: 1080, Username: "private-user", Password: "private-password"},
				Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://upstream.example"}}
			upstream := &proxyRouteDirectUpstream{recorder: httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.response)),
			}}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/fixture", bytes.NewReader(nil)).WithContext(ctx)
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			require.NoError(t, tc.run(ctx, c, svc, account))
			require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 71, Name: "fixed-node", Host: "2001:db8::7", Port: 1080}}, ProxyRoutesFromContext(ctx))
			require.Equal(t, "socks5://private-user:private-password@[2001:db8::7]:1080", upstream.recorder.lastProxyURL, "observation must preserve the existing route and credentials sent to transport")
		})
	}
}

func TestProxyRouteDirectAnthropicServicePaths(t *testing.T) {
	for _, name := range []string{"messages passthrough", "count tokens passthrough", "count tokens standard", "antigravity upstream"} {
		t.Run(name, func(t *testing.T) {
			ctx, observation := NewProxyRouteObservation(t.Context(), nil)
			defer observation.Close()
			proxyID := int64(72)
			account := &Account{ID: 53, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, ProxyID: &proxyID,
				Proxy:       &Proxy{ID: 72, Name: "anthropic-node", Protocol: "http", Host: "proxy.example", Port: 3128},
				Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://upstream.example"},
				Extra:       map[string]any{"anthropic_passthrough": true}}
			body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
			payload := `{"id":"fixture","type":"message","model":"claude-sonnet-4-5","role":"assistant","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
			if strings.HasPrefix(name, "count tokens") {
				payload = `{"input_tokens":3}`
			}
			upstream := &proxyRouteDirectUpstream{recorder: httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload)),
			}}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body)).WithContext(ctx)
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
			svc := &GatewayService{cfg: cfg, httpUpstream: upstream, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
			var err error
			switch name {
			case "messages passthrough":
				_, err = svc.forwardAnthropicAPIKeyPassthrough(ctx, c, account, body, "claude-sonnet-4-5", "claude-sonnet-4-5", false, time.Now())
			case "count tokens passthrough", "count tokens standard":
				account.Extra["anthropic_passthrough"] = name == "count tokens passthrough"
				err = svc.ForwardCountTokens(ctx, c, account, &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-5"})
			case "antigravity upstream":
				antigravity := newAntigravityTestService(cfg)
				antigravity.httpUpstream = upstream
				_, err = antigravity.ForwardUpstream(ctx, c, account, body)
			}
			require.NoError(t, err)
			require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 72, Name: "anthropic-node", Host: "proxy.example", Port: 3128}}, ProxyRoutesFromContext(ctx))
		})
	}
}

func TestProxyRouteDirectCustomRelayDoesNotAttributeQueryProxy(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(t.Context(), nil)
	defer observation.Close()
	proxyID := int64(73)
	account := &Account{ID: 54, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, ProxyID: &proxyID,
		Proxy:       &Proxy{ID: 73, Name: "remote-relay-node", Protocol: "http", Host: "proxy.example", Port: 3128},
		Credentials: map[string]any{"access_token": "fixture-token"},
		Extra:       map[string]any{"custom_base_url_enabled": true, "custom_base_url": "https://relay.example"}}
	upstream := &proxyRouteDirectUpstream{recorder: httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"input_tokens":3}`)),
	}}}
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body)).WithContext(ctx)
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	err := svc.ForwardCountTokens(ctx, c, account, &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-5"})
	require.NoError(t, err)
	require.Equal(t, "http://proxy.example:3128", upstream.recorder.lastReq.URL.Query().Get("proxy"))
	require.Empty(t, upstream.recorder.lastProxyURL, "the query configures the remote relay, not this process's transport")
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "direct"}}, ProxyRoutesFromContext(ctx))
}
