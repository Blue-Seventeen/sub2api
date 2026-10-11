//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayProxyPoolRouteCompatibility(t *testing.T) {
	for _, mode := range []string{"idle", "preferred", "strict preferred", "busy queue"} {
		t.Run(mode, func(t *testing.T) {
			cfg := gatewayProxyWSConfig()
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			dialer := &gatewayProxyCaptureDialer{conns: []openAIWSClientConn{&openAIWSCaptureConn{}, &openAIWSCaptureConn{}}}
			pool.setClientDialerForTest(dialer)
			req := openAIWSAcquireRequest{Account: rawChatCompletionsTestAccount(), WSURL: "wss://upstream.example/v1/responses", ProxyURL: "socks5h://node-a:test-a@127.0.0.1:1080"}
			first, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			firstID := first.ConnID()
			req.ProxyURL = "socks5h://node-b:test-b@127.0.0.1:1080"
			if mode != "busy queue" {
				first.Release()
			}
			if mode == "preferred" || mode == "strict preferred" {
				req.PreferredConnID = firstID
				req.ForcePreferredConn = mode == "strict preferred"
			}
			type acquireResult struct {
				lease *openAIWSConnLease
				err   error
			}
			results := make(chan acquireResult, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			go func() {
				lease, err := pool.Acquire(ctx, req)
				results <- acquireResult{lease, err}
			}()
			if mode == "busy queue" {
				require.Eventually(t, func() bool { return pool.SnapshotMetrics().AcquireTotal >= 2 }, time.Second, time.Millisecond)
				first.Release()
			}
			result := <-results
			if mode == "strict preferred" {
				require.ErrorIs(t, result.err, errOpenAIWSPreferredConnUnavailable)
				require.Nil(t, result.lease)
				return
			}
			require.NoError(t, result.err)
			require.NotNil(t, result.lease)
			defer result.lease.Release()
			if result.lease.ConnID() == firstID || result.lease.Reused() {
				t.Fatal("a changed proxy route reused the old websocket connection")
			}
			dialer.mu.Lock()
			defer dialer.mu.Unlock()
			if len(dialer.proxyURLs) != 2 || dialer.proxyURLs[1] != req.ProxyURL {
				t.Fatal("a changed node identity did not reach a new dial")
			}
		})
	}
}

func TestOpenAIGatewayProxyPoolUnavailableRejectsOldDirectConnection(t *testing.T) {
	for _, forcePreferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary reuse", true: "preferred reuse"}[forcePreferred], func(t *testing.T) {
			pool := newOpenAIWSConnPool(gatewayProxyWSConfig())
			t.Cleanup(pool.Close)
			dialer := &gatewayProxyCaptureDialer{conn: &openAIWSCaptureConn{}}
			pool.setClientDialerForTest(dialer)
			req := openAIWSAcquireRequest{Account: rawChatCompletionsTestAccount(), WSURL: "wss://upstream.example/v1/responses"}
			first, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			first.Release()
			req.ProxyURL = "auto-proxy://unavailable"
			if forcePreferred {
				req.PreferredConnID, req.ForcePreferredConn = first.ConnID(), true
			}
			lease, err := pool.Acquire(context.Background(), req)
			if lease != nil {
				lease.Release()
				t.Fatal("unavailable automatic routing returned an old direct websocket lease")
			}
			require.Error(t, err)
			dialer.mu.Lock()
			defer dialer.mu.Unlock()
			require.Len(t, dialer.proxyURLs, 1, "invalid routes must fail before any dial")
		})
	}
}

type gatewayProxyIOConn struct {
	openAIWSCaptureConn
	readErr, writeErr error
	beforeRead        func()
}

func (c *gatewayProxyIOConn) ReadMessage(ctx context.Context) ([]byte, error) {
	if c.beforeRead != nil {
		c.beforeRead()
	}
	if c.readErr != nil {
		return nil, c.readErr
	}
	return c.openAIWSCaptureConn.ReadMessage(ctx)
}

func (c *gatewayProxyIOConn) WriteJSON(ctx context.Context, value any) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	return c.openAIWSCaptureConn.WriteJSON(ctx, value)
}

func gatewayProxyCompletedWSConn() *openAIWSCaptureConn {
	return &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"ws_proxy_io","model":"gpt-5.4","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}}
}

func TestOpenAIGatewayProxyWebsocketActiveIOFailureReselects(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			auto, chosen := gatewayProxyFixture(t)
			auto.config.StickyEnabled = true
			cfg := gatewayProxyWSConfig()
			cfg.Gateway.OpenAIWS.RetryBackoffInitialMS, cfg.Gateway.OpenAIWS.RetryBackoffMaxMS = 1, 1
			failed := &gatewayProxyIOConn{}
			if operation == "read" {
				failed.readErr = errors.New("connection reset by peer")
			} else {
				failed.writeErr = errors.New("connection reset by peer")
			}
			dialer := &gatewayProxyCaptureDialer{conns: []openAIWSClientConn{failed, gatewayProxyCompletedWSConn()}}
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
			if dialer.proxyURLs[1] != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
				t.Fatal("an active websocket IO failure redialed through the failed automatic node")
			}
		})
	}
}

func TestOpenAIGatewayProxyWebsocketIngressIOFailureReselects(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			auto, chosen := gatewayProxyFixture(t)
			auto.config.StickyEnabled = true
			cfg := gatewayProxyWSConfig()
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
			failed := &gatewayProxyIOConn{}
			if operation == "read" {
				failed.readErr = errors.New("connection reset by peer")
			} else {
				failed.writeErr = errors.New("connection reset by peer")
			}
			dialer := &gatewayProxyCaptureDialer{conns: []openAIWSClientConn{failed, gatewayProxyCompletedWSConn()}}
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(dialer)
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
			account := gatewayProxyAccount("auto", chosen)
			account.Extra["responses_websockets_v2_enabled"] = true
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
			gatewayProxyRunIngress(t, svc, account)
			dialer.mu.Lock()
			defer dialer.mu.Unlock()
			require.Len(t, dialer.proxyURLs, 2)
			if dialer.proxyURLs[1] != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
				t.Fatal("an unfinished ingress turn redialed through the failed automatic node")
			}
		})
	}
}

func TestOpenAIGatewayProxyWebsocketIngressApplicationEOFPreservesSticky(t *testing.T) {
	auto, chosen := gatewayProxyFixture(t)
	auto.config.StickyEnabled = true
	cfg := gatewayProxyWSConfig()
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
	dialer := &gatewayProxyCaptureDialer{conn: &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"error","error":{"code":"invalid_request_error","type":"invalid_request_error","message":"invalid test request"}}`)}}}
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(dialer)
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
	account := gatewayProxyAccount("auto", chosen)
	account.Extra["responses_websockets_v2_enabled"] = true
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	gatewayProxyRunIngress(t, svc, account)
	fresh := gatewayProxyAccount("auto", chosen)
	if selected := auto.selectProxyForAccount(context.Background(), fresh); selected == nil || selected.ID != 625 {
		t.Fatal("EOF after a valid application error cleared healthy automatic routing")
	}
}

func TestOpenAIGatewayProxyWebsocketNonTransportOutcomesPreserveSticky(t *testing.T) {
	for _, outcome := range []string{"completed terminal before EOF", "application error", "client cancellation with EOF"} {
		t.Run(outcome, func(t *testing.T) {
			auto, chosen := gatewayProxyFixture(t)
			auto.config.StickyEnabled = true
			cfg := gatewayProxyWSConfig()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := &gatewayProxyIOConn{}
			first.events = gatewayProxyCompletedWSConn().events
			if outcome == "application error" {
				first.events = [][]byte{[]byte(`{"type":"error","error":{"code":"invalid_request_error","type":"invalid_request_error","message":"invalid test request"}}`)}
			} else if outcome == "client cancellation with EOF" {
				first.readErr, first.beforeRead = io.EOF, cancel
			}
			dialer := &gatewayProxyCaptureDialer{conn: first}
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(dialer)
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
			account := gatewayProxyAccount("auto", chosen)
			account.Extra["responses_websockets_v2_enabled"] = true
			c, _ := newTestContext()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(gatewayProxyResponsesBody)).WithContext(ctx)
			_, err := svc.Forward(ctx, c, account, []byte(gatewayProxyResponsesBody))
			if outcome == "completed terminal before EOF" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			fresh := gatewayProxyAccount("auto", chosen)
			selected := auto.selectProxyForAccount(context.Background(), fresh)
			if selected == nil || selected.ID != 625 {
				t.Fatal("a terminal event, application error, or client cancellation cleared healthy proxy sticky")
			}
		})
	}
}

func gatewayProxyRunIngress(t *testing.T, svc *OpenAIGatewayService, account *Account) {
	t.Helper()
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.CloseNow()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
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
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello","stream":true}`)))
	_, _, err = client.Read(ctx)
	require.NoError(t, err)
	_ = client.CloseNow()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("websocket ingress did not terminate")
	}
}
