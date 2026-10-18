//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const gatewayProxyResponsesBody = `{"model":"gpt-5.4","input":"hello","stream":false}`
const gatewayProxyChatBody = `{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`
const gatewayProxyMessagesBody = `{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`
const gatewayProxyCompleted = `{"id":"resp_proxy","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}],"status":"completed"}],"usage":{"input_tokens":3,"output_tokens":1}}`
const gatewayProxyChatCompleted = `{"id":"chat_proxy","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`

func gatewayProxyFixture(t *testing.T) (*ProxyAutoProbeService, Proxy) {
	t.Helper()
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{Enabled: true, MihomoBinaryPath: "mihomo", BindHost: "127.0.0.1", WorkDir: t.TempDir()}, nil, &capturingManagedProxyRunner{})
	sub := managedProxyRuntimeTestSubscription(77, 1)
	chosenID := int64(625)
	epoch := time.Now().Add(-time.Hour)
	sub.Nodes[0].ProxyID = &chosenID
	sub.Nodes[0].Username, sub.Nodes[0].Password = "gateway-node-user", "gateway-node-password"
	sub.Nodes[0].UpdatedAt = epoch
	require.NoError(t, runtime.startEntry(context.Background(), sub))
	status := runtime.GetStatus(sub.ID)
	chosen := Proxy{ID: chosenID, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &sub.ID, Username: sub.Nodes[0].Username, Password: sub.Nodes[0].Password, Status: StatusActive, NodeStatus: ProxySubscriptionNodeStatusActive, nodeUpdatedAt: epoch, UpdatedAt: epoch, RuntimeStatus: &status}
	backup := Proxy{ID: 626, Protocol: "http", Host: "127.0.0.1", Port: 1080, Status: StatusActive}
	failed := Proxy{ID: 624, Protocol: "http", Host: "127.0.0.1", Port: 1081, Status: StatusActive}
	auto := NewProxyAutoProbeService(nil, &proxyAutoProbeRepoStub{}, &proxyAutoProbeSettingRepoStub{}, nil, &proxyStickyStoreStub{})
	auto.config = ProxyAutoProbeConfig{Enabled: true, DefaultIntervalSec: 60, RetryIntervalSec: 5, StickyEnabled: false, StickyTTLSeconds: 60}
	selectedLatency, backupLatency, failedLatency := int64(20), int64(80), int64(1)
	auto.entries[625] = &proxyAutoProbeEntry{ProxyID: 625, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &selectedLatency}
	auto.entries[626] = &proxyAutoProbeEntry{ProxyID: 626, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &backupLatency}
	auto.entries[624] = &proxyAutoProbeEntry{ProxyID: 624, Queue: ProxyAutoProbeQueueFailed, LastLatencyMs: &failedLatency}
	seedProxyAutoProbeSnapshots(auto, chosen, backup, failed)
	previousAuto, previousRuntime := GetDefaultProxyAutoProbeService(), GetDefaultManagedProxyResolver()
	SetDefaultProxyAutoProbeService(auto)
	SetDefaultManagedProxyResolver(runtime)
	t.Cleanup(func() {
		auto.Stop()
		runtime.Stop()
		SetDefaultProxyAutoProbeService(previousAuto)
		SetDefaultManagedProxyResolver(previousRuntime)
	})
	return auto, chosen
}

func gatewayProxyAccount(binding string, chosen Proxy) *Account {
	account := rawChatCompletionsTestAccount()
	account.Status, account.Schedulable = StatusActive, true
	account.Credentials["api_protocol"] = APIProtocolResponses
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)}
	switch binding {
	case "auto":
		account.Extra["auto_select_proxy"] = true
	case "auto with hydrated proxy":
		account.Extra["auto_select_proxy"] = true
		account.Proxy = &chosen
	case "manual managed":
		account.ProxyID = &chosen.ID
		account.Proxy = &chosen
	}
	return account
}

func assertGatewayNodeProxyURL(t *testing.T, raw string, account *Account) {
	t.Helper()
	if raw == "" {
		t.Fatal("actual upstream boundary received a direct route for a selected proxy")
	}
	_, parsed, err := proxyurl.Parse(raw)
	if err != nil {
		t.Fatalf("actual upstream boundary received an unresolved proxy scheme: %v", err)
	}
	if parsed == nil || parsed.Scheme != "socks5h" || parsed.Hostname() != "127.0.0.1" || parsed.User == nil {
		t.Fatal("actual upstream boundary did not receive a local authenticated SOCKS route")
	}
	password, ok := parsed.User.Password()
	if parsed.User.Username() != "gateway-node-user" || !ok || password != "gateway-node-password" {
		t.Fatal("actual upstream boundary lost the selected node authentication")
	}
	if account.Proxy == nil || account.Proxy.ID != 625 {
		t.Fatal("forward did not retain the chosen node for request attribution")
	}
	if account.IsAutoSelectProxyEnabled() && account.ProxyID != nil {
		t.Fatal("automatic choice changed the persisted proxy binding")
	}
}

func TestOpenAIGatewayProxyHTTPBoundaries(t *testing.T) {
	_, chosen := gatewayProxyFixture(t)
	cases := []struct {
		name, path, body, protocol string
		passthrough, bridge        bool
	}{
		{name: "native responses", path: "/v1/responses", body: gatewayProxyResponsesBody, protocol: APIProtocolResponses},
		{name: "responses passthrough", path: "/v1/responses", body: gatewayProxyResponsesBody, protocol: APIProtocolResponses, passthrough: true},
		{name: "responses to chat", path: "/v1/responses", body: gatewayProxyResponsesBody, protocol: APIProtocolChatCompletions},
		{name: "chat to responses", path: "/v1/chat/completions", body: gatewayProxyChatBody, protocol: APIProtocolResponses},
		{name: "native chat", path: "/v1/chat/completions", body: gatewayProxyChatBody, protocol: APIProtocolChatCompletions},
		{name: "messages to responses", path: "/v1/messages", body: gatewayProxyMessagesBody, protocol: APIProtocolResponses},
		{name: "messages to chat", path: "/v1/messages", body: gatewayProxyMessagesBody, protocol: APIProtocolChatCompletions},
		{name: "responses to anthropic", path: "/v1/responses", body: gatewayProxyResponsesBody, protocol: APIProtocolAnthropic},
		{name: "chat to anthropic", path: "/v1/chat/completions", body: gatewayProxyChatBody, protocol: APIProtocolAnthropic},
		{name: "native anthropic", path: "/v1/messages", body: gatewayProxyMessagesBody, protocol: APIProtocolAnthropic},
		{name: "websocket HTTP bridge", path: "/v1/responses", body: `{"type":"response.create","model":"gpt-5.4","input":"hello","stream":true}`, protocol: APIProtocolResponses, bridge: true},
	}
	for _, binding := range []string{"auto", "auto with hydrated proxy", "manual managed"} {
		for _, tc := range cases {
			t.Run(binding+"/"+tc.name, func(t *testing.T) {
				account := gatewayProxyAccount(binding, chosen)
				account.Credentials["api_protocol"] = tc.protocol
				account.Credentials["api_base_urls"] = map[string]any{tc.protocol: "http://upstream.example"}
				account.Extra["openai_passthrough"] = tc.passthrough
				if tc.protocol == APIProtocolChatCompletions {
					account.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceChatCompletions)
				}
				if tc.protocol == APIProtocolAnthropic {
					account.Platform = PlatformKimi
				}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(gatewayProxyCompleted))}
				if tc.protocol == APIProtocolResponses && tc.path != "/v1/responses" {
					response = gatewayProxyResponsesSSE()
				}
				if tc.protocol == APIProtocolChatCompletions {
					response.Body = io.NopCloser(strings.NewReader(gatewayProxyChatCompleted))
				}
				if tc.protocol == APIProtocolAnthropic {
					response = nativeAnthropicBufferedResponse()
					if tc.path != "/v1/messages" {
						response = nativeAnthropicStreamResponse()
					}
				}
				if tc.bridge {
					response = gatewayProxyResponsesSSE()
				}
				upstream := &httpUpstreamRecorder{resp: response}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader([]byte(tc.body)))
				c.Request.Header.Set("Content-Type", "application/json")
				var result *OpenAIForwardResult
				var err error
				if tc.bridge {
					result, err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", []byte(tc.body), len(tc.body), "gpt-5.4", "", "", "", "", 1, func([]byte) error { return nil })
				} else if tc.path == "/v1/messages" {
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, []byte(tc.body), "", "")
				} else if tc.path == "/v1/chat/completions" {
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(tc.body), "", "")
				} else {
					result, err = svc.Forward(context.Background(), c, account, []byte(tc.body))
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				expectedPath := "/v1/responses"
				switch tc.protocol {
				case APIProtocolChatCompletions:
					expectedPath = "/v1/chat/completions"
				case APIProtocolAnthropic:
					expectedPath = "/v1/messages"
				}
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, expectedPath, upstream.lastReq.URL.Path)
				assertGatewayNodeProxyURL(t, upstream.lastProxyURL, account)
			})
		}
	}
}

func gatewayProxyResponsesSSE() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":" + gatewayProxyCompleted + "}\n\n"))}
}

func TestOpenAIGatewayProxyAdminTransportFailureReselects(t *testing.T) {
	for _, protocol := range []string{APIProtocolResponses, APIProtocolChatCompletions} {
		for _, cause := range []error{errors.New("temporary connection reset by peer"), context.Canceled} {
			t.Run(protocol+"/"+cause.Error(), func(t *testing.T) {
				auto, chosen := gatewayProxyFixture(t)
				auto.config.StickyEnabled = true
				account := gatewayProxyAccount("auto", chosen)
				if protocol == APIProtocolChatCompletions {
					account.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceChatCompletions)
				}
				upstream := &httpUpstreamRecorder{err: cause}
				svc := &AccountTestService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				c, _ := newTestContext()
				require.Error(t, svc.testOpenAIAccountConnection(c, account, "gpt-5.4", "", ""))
				assertGatewayNodeProxyURL(t, upstream.lastProxyURL, account)
				upstream.err, upstream.resp = nil, gatewayProxyResponsesSSE()
				if protocol == APIProtocolChatCompletions {
					upstream.resp.Body = io.NopCloser(strings.NewReader("data: " + gatewayProxyChatCompleted + "\n\ndata: [DONE]\n\n"))
				}
				c, _ = newTestContext()
				require.NoError(t, svc.testOpenAIAccountConnection(c, account, "gpt-5.4", "", ""))
				if errors.Is(cause, context.Canceled) {
					assertGatewayNodeProxyURL(t, upstream.lastProxyURL, account)
				} else if upstream.lastProxyURL != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
					t.Fatal("administrator test reused the failed automatic proxy on the same account")
				}
			})
		}
	}
}

func TestOpenAIGatewayProxyHTTPBridgeLaterTurnReselects(t *testing.T) {
	auto, chosen := gatewayProxyFixture(t)
	auto.config.StickyEnabled = true
	account := gatewayProxyAccount("auto", chosen)
	account.Proxy = auto.selectProxyForAccount(context.Background(), account)
	upstream := &httpUpstreamRecorder{err: errors.New("temporary connection reset by peer")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	body := []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello","stream":true}`)
	c, _ := newTestContext()
	_, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", body, len(body), "gpt-5.4", "", "", "", "", 2, func([]byte) error { return nil })
	require.Error(t, err)
	upstream.err, upstream.resp = nil, gatewayProxyResponsesSSE()
	c, _ = newTestContext()
	_, err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", body, len(body), "gpt-5.4", "", "", "", "", 2, func([]byte) error { return nil })
	require.NoError(t, err)
	if upstream.lastProxyURL != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
		t.Fatal("later websocket HTTP bridge turn reused the failed automatic proxy")
	}
}

func TestOpenAIGatewayProxyTransportFailureReselectsOnSameAccount(t *testing.T) {
	auto, chosen := gatewayProxyFixture(t)
	auto.config.StickyEnabled = true
	account := gatewayProxyAccount("auto", chosen)
	account.Proxy = auto.selectProxyForAccount(context.Background(), account)
	require.NotNil(t, account.Proxy)
	upstream := &httpUpstreamRecorder{err: errors.New("temporary connection reset by peer")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(gatewayProxyResponsesBody))
	_, err := svc.Forward(context.Background(), c, account, []byte(gatewayProxyResponsesBody))
	require.Error(t, err)
	rawEvents, exists := c.Get(OpsUpstreamErrorsKey)
	require.True(t, exists)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].ProxyID)
	require.Equal(t, chosen.ID, *events[0].ProxyID, "failed node attribution must survive sticky invalidation")
	require.NotNil(t, account.Proxy)
	require.Equal(t, chosen.ID, account.Proxy.ID)
	upstream.err = nil
	upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(gatewayProxyCompleted))}
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(gatewayProxyResponsesBody))
	_, err = svc.Forward(context.Background(), c, account, []byte(gatewayProxyResponsesBody))
	require.NoError(t, err)
	if upstream.lastProxyURL != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
		t.Fatal("same-account retry reused the failed automatic proxy after clearing sticky")
	}
}

func TestOpenAIGatewayProxyCancellationPreservesSelection(t *testing.T) {
	auto, chosen := gatewayProxyFixture(t)
	auto.config.StickyEnabled = true
	account := gatewayProxyAccount("auto", chosen)
	account.Proxy = auto.selectProxyForAccount(context.Background(), account)
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpUpstreamRecorder{err: context.Canceled}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(gatewayProxyResponsesBody))
	_, err := svc.Forward(context.Background(), c, account, []byte(gatewayProxyResponsesBody))
	require.ErrorIs(t, err, context.Canceled)
	if account.Proxy == nil || account.Proxy.ID != 625 {
		t.Fatal("client cancellation cleared the selected proxy")
	}
	fresh := gatewayProxyAccount("auto", chosen)
	if selected := auto.selectProxyForAccount(context.Background(), fresh); selected == nil || selected.ID != 625 {
		t.Fatal("client cancellation cleared healthy sticky routing")
	}
}

type gatewayProxyCaptureDialer struct {
	mu         sync.Mutex
	conn       openAIWSClientConn
	conns      []openAIWSClientConn
	proxyURLs  []string
	err        error
	dialErrors []error
}

func (d *gatewayProxyCaptureDialer) Dial(_ context.Context, _ string, _ http.Header, proxyURL string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.proxyURLs = append(d.proxyURLs, proxyURL)
	if len(d.dialErrors) > 0 {
		err := d.dialErrors[0]
		d.dialErrors = d.dialErrors[1:]
		if err != nil {
			return nil, 0, nil, err
		}
	}
	if d.err != nil {
		return nil, 0, nil, d.err
	}
	if len(d.conns) > 0 {
		conn := d.conns[0]
		d.conns = d.conns[1:]
		return conn, 0, nil, nil
	}
	return d.conn, 0, nil, nil
}
func gatewayProxyWSConfig() *config.Config {
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.APIKeyEnabled, cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true, true, true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount, cfg.Gateway.OpenAIWS.MaxIdlePerAccount, cfg.Gateway.OpenAIWS.QueueLimitPerConn = 1, 1, 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds, cfg.Gateway.OpenAIWS.ReadTimeoutSeconds, cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3, 3, 3
	return cfg
}
func TestOpenAIGatewayProxyWebsocketV2Boundary(t *testing.T) {
	_, chosen := gatewayProxyFixture(t)
	for _, binding := range []string{"auto", "manual managed"} {
		t.Run(binding, func(t *testing.T) {
			cfg := gatewayProxyWSConfig()
			dialer := &gatewayProxyCaptureDialer{conn: &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"ws_proxy","model":"gpt-5.4","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}}}
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(dialer)
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
			account := gatewayProxyAccount(binding, chosen)
			account.Extra["responses_websockets_v2_enabled"] = true
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(gatewayProxyResponsesBody))
			result, err := svc.Forward(context.Background(), c, account, []byte(gatewayProxyResponsesBody))
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.OpenAIWSMode)
			require.NotEmpty(t, dialer.proxyURLs)
			assertGatewayNodeProxyURL(t, dialer.proxyURLs[0], account)
		})
	}
}

func TestOpenAIGatewayProxyWebsocketDialFailureReselects(t *testing.T) {
	auto, chosen := gatewayProxyFixture(t)
	auto.config.StickyEnabled = true
	cfg := gatewayProxyWSConfig()
	cfg.Gateway.OpenAIWS.RetryBackoffInitialMS, cfg.Gateway.OpenAIWS.RetryBackoffMaxMS = 1, 1
	dialer := &gatewayProxyCaptureDialer{
		conn:       &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"ws_proxy_retry","model":"gpt-5.4","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}},
		dialErrors: []error{errors.New("temporary connection reset by peer")},
	}
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(dialer)
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
	account := gatewayProxyAccount("auto", chosen)
	account.Extra["responses_websockets_v2_enabled"] = true
	c, _ := newTestContext()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(gatewayProxyResponsesBody))
	result, err := svc.Forward(context.Background(), c, account, []byte(gatewayProxyResponsesBody))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.OpenAIWSMode)
	require.Len(t, dialer.proxyURLs, 2)
	assertGatewayNodeProxyURL(t, dialer.proxyURLs[0], gatewayProxyAccount("auto with hydrated proxy", chosen))
	if dialer.proxyURLs[1] != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
		t.Fatal("websocket reconnect reused the failed automatic proxy on the same account")
	}
}

func TestOpenAIGatewayProxyWebsocketIngressBoundary(t *testing.T) {
	_, chosen := gatewayProxyFixture(t)
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			cfg := gatewayProxyWSConfig()
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = mode
			dialer := &gatewayProxyCaptureDialer{conn: &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"ws_ingress_proxy","model":"gpt-5.4","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}}}
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(dialer)
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool, openaiWSPassthroughDialer: dialer}
			account := gatewayProxyAccount("auto", chosen)
			account.Extra["responses_websockets_v2_enabled"] = true
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = mode
			done := make(chan error, 1)
			wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					done <- err
					return
				}
				defer func() {
					// Best-effort teardown: the session may have closed the socket already.
					_ = conn.CloseNow()
				}()
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = r
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				defer cancel()
				_, payload, err := conn.Read(ctx)
				if err != nil {
					done <- err
					return
				}
				done <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "test-token", payload, nil)
			}))
			defer wsServer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
			require.NoError(t, err)
			defer func() {
				// Best-effort cleanup also covers assertion failures before the explicit close.
				_ = client.CloseNow()
			}()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello","stream":true}`)))
			_, _, _ = client.Read(ctx)
			_ = client.CloseNow()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("websocket ingress did not terminate")
			}
			dialer.mu.Lock()
			urls := append([]string(nil), dialer.proxyURLs...)
			dialer.mu.Unlock()
			require.NotEmpty(t, urls)
			assertGatewayNodeProxyURL(t, urls[0], account)
		})
	}
}
