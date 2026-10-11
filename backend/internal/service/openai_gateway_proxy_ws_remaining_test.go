//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewayProxyPrewarmConn struct {
	gatewayProxyIOConn
	generateFalseObserved bool
}

func (c *gatewayProxyPrewarmConn) WriteJSON(ctx context.Context, value any) error {
	if payload, ok := value.(map[string]any); ok {
		if generate, exists := payload["generate"].(bool); exists && !generate {
			c.generateFalseObserved = true
		}
	}
	return c.gatewayProxyIOConn.WriteJSON(ctx, value)
}

func gatewayProxyPrewarmCompletedConn() *openAIWSCaptureConn {
	event := gatewayProxyCompletedWSConn().events[0]
	return &openAIWSCaptureConn{events: [][]byte{event, event}}
}

func TestOpenAIGatewayProxyGeneratePrewarmTransportAndControls(t *testing.T) {
	for _, outcome := range []string{"read reset", "write reset", "completed terminal before EOF", "application error", "client cancellation with EOF"} {
		t.Run(outcome, func(t *testing.T) {
			auto, chosen := gatewayProxyFixture(t)
			auto.config.StickyEnabled = true
			cfg := gatewayProxyWSConfig()
			cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
			cfg.Gateway.OpenAIWS.RetryBackoffInitialMS, cfg.Gateway.OpenAIWS.RetryBackoffMaxMS = 1, 1
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := &gatewayProxyPrewarmConn{}
			switch outcome {
			case "read reset":
				first.readErr = errors.New("connection reset by peer")
			case "write reset":
				first.writeErr = errors.New("connection reset by peer")
			case "completed terminal before EOF":
				first.events = gatewayProxyPrewarmCompletedConn().events
			case "application error":
				first.events = [][]byte{[]byte(`{"type":"error","error":{"code":"invalid_request_error","type":"invalid_request_error","message":"invalid test request"}}`)}
			case "client cancellation with EOF":
				first.readErr, first.beforeRead = io.EOF, cancel
			}
			second := gatewayProxyPrewarmCompletedConn()
			dialer := &gatewayProxyCaptureDialer{conns: []openAIWSClientConn{first, second}}
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(dialer)
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
			account := gatewayProxyAccount("auto", chosen)
			account.Extra["responses_websockets_v2_enabled"] = true
			c, _ := newTestContext()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(gatewayProxyResponsesBody)).WithContext(ctx)
			result, err := svc.Forward(ctx, c, account, []byte(gatewayProxyResponsesBody))
			require.True(t, first.generateFalseObserved, "failure must happen in generate=false prewarm")
			if outcome == "client cancellation with EOF" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.OpenAIWSMode)
			}
			if outcome == "read reset" || outcome == "write reset" {
				require.Equal(t, 2, len(dialer.proxyURLs))
				if dialer.proxyURLs[1] != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
					t.Fatal("a generate=false prewarm IO failure redialed through the failed automatic node")
				}
				require.Len(t, second.writes, 2, "replacement connection must finish prewarm and main turn")
			} else {
				fresh := gatewayProxyAccount("auto", chosen)
				if selected := auto.selectProxyForAccount(context.Background(), fresh); selected == nil || selected.ID != 625 {
					t.Fatal("prewarm terminal, application error, or cancellation cleared healthy sticky")
				}
			}
		})
	}
}

type gatewayProxyPassthroughConn struct {
	openAIWSCaptureConn
	readErr, writeErr error
	failWriteAt       int32
	writeCount        atomic.Int32
	writeResetSeen    atomic.Bool
	eofAfterEvents    bool
	cancelOnRead      bool
	beforeRead        func()
}

func (c *gatewayProxyPassthroughConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c.beforeRead != nil {
		c.beforeRead()
	}
	payload, err := c.openAIWSCaptureConn.ReadMessage(ctx)
	if err == nil {
		return coderws.MessageText, payload, nil
	}
	if c.readErr != nil {
		return coderws.MessageText, nil, c.readErr
	}
	if c.eofAfterEvents {
		return coderws.MessageText, nil, io.EOF
	}
	<-ctx.Done()
	return coderws.MessageText, nil, ctx.Err()
}

func (c *gatewayProxyPassthroughConn) WriteFrame(ctx context.Context, _ coderws.MessageType, payload []byte) error {
	if c.writeCount.Add(1) == c.failWriteAt && c.writeErr != nil {
		c.writeResetSeen.Store(true)
		return c.writeErr
	}
	return c.openAIWSCaptureConn.WriteJSON(ctx, json.RawMessage(payload))
}

func TestOpenAIGatewayProxyPassthroughActiveIOFailureReselects(t *testing.T) {
	for _, operation := range []string{"read", "first write", "relay write", "auxiliary write"} {
		t.Run(operation, func(t *testing.T) {
			auto, chosen := gatewayProxyFixture(t)
			auto.config.StickyEnabled = true
			cfg := gatewayProxyWSConfig()
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModePassthrough
			first := &gatewayProxyPassthroughConn{}
			if operation == "read" {
				first.readErr = errors.New("connection reset by peer")
			} else {
				first.writeErr, first.failWriteAt = errors.New("connection reset by peer"), 1
				if operation == "relay write" || operation == "auxiliary write" {
					first.events = gatewayProxyCompletedWSConn().events
					first.failWriteAt = 2
					if operation == "auxiliary write" {
						first.events = [][]byte{[]byte(`{"type":"response.created","response":{"id":"ws_proxy_pending","model":"gpt-5.4","status":"in_progress"}}`)}
					}
				}
			}
			second := &gatewayProxyPassthroughConn{eofAfterEvents: true}
			second.events = gatewayProxyCompletedWSConn().events
			dialer := &gatewayProxyCaptureDialer{conns: []openAIWSClientConn{first, second}}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPassthroughDialer: dialer}
			account := gatewayProxyAccount("auto", chosen)
			account.Extra["responses_websockets_v2_enabled"] = true
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
			followUp := ""
			if operation == "relay write" {
				followUp = "response.create"
			} else if operation == "auxiliary write" {
				followUp = "conversation.item.create"
			}
			firstErr := gatewayProxyRunPassthroughSession(t, svc, account, first, followUp)
			if operation != "read" {
				require.True(t, first.writeResetSeen.Load(), "the actual upstream WriteFrame must fail")
			}
			if operation == "auxiliary write" {
				// Auxiliary frames are written inside ReadClientFrame. The relay
				// classifies the returned reset as a graceful client disconnect.
				// Keep that existing return contract, but verify the real upstream
				// failure still invalidates routing for the next session.
				require.NoError(t, firstErr)
				require.Equal(t, int32(2), first.writeCount.Load())
			} else {
				require.ErrorContains(t, firstErr, "connection reset by peer")
			}
			_ = gatewayProxyRunPassthroughSession(t, svc, account, second, "")
			dialer.mu.Lock()
			defer dialer.mu.Unlock()
			require.Equal(t, 2, len(dialer.proxyURLs))
			if dialer.proxyURLs[1] != "http://127.0.0.1:1080" || account.Proxy == nil || account.Proxy.ID != 626 {
				t.Fatal("an established passthrough IO failure reused the failed node on the next dial")
			}
		})
	}
}

func TestOpenAIGatewayProxyPassthroughNonTransportControls(t *testing.T) {
	for _, outcome := range []string{"completed EOF", "completed then reset", "application EOF", "application then reset", "client cancellation with EOF"} {
		t.Run(outcome, func(t *testing.T) {
			auto, chosen := gatewayProxyFixture(t)
			auto.config.StickyEnabled = true
			cfg := gatewayProxyWSConfig()
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModePassthrough
			first := &gatewayProxyPassthroughConn{eofAfterEvents: true}
			if strings.HasPrefix(outcome, "completed") {
				first.events = gatewayProxyCompletedWSConn().events
			} else if strings.HasPrefix(outcome, "application") {
				first.events = [][]byte{[]byte(`{"type":"error","error":{"code":"invalid_request_error","type":"invalid_request_error","message":"invalid test request"}}`)}
			} else {
				first.cancelOnRead = true
			}
			if strings.HasSuffix(outcome, "then reset") {
				first.readErr = errors.New("connection reset by peer")
			}
			dialer := &gatewayProxyCaptureDialer{conn: first}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPassthroughDialer: dialer}
			account := gatewayProxyAccount("auto", chosen)
			account.Extra["responses_websockets_v2_enabled"] = true
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
			_ = gatewayProxyRunPassthroughSession(t, svc, account, first, "")
			fresh := gatewayProxyAccount("auto", chosen)
			if selected := auto.selectProxyForAccount(context.Background(), fresh); selected == nil || selected.ID != 625 {
				t.Fatal("completed, application error, or canceled passthrough cleared healthy proxy sticky")
			}
		})
	}
}

func gatewayProxyRunPassthroughSession(t *testing.T, svc *OpenAIGatewayService, account *Account, upstream *gatewayProxyPassthroughConn, followUp string) error {
	t.Helper()
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		if upstream.cancelOnRead {
			upstream.beforeRead = cancel
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.WithContext(ctx)
		_, payload, err := conn.Read(ctx)
		if err != nil {
			done <- err
			return
		}
		done <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "test-token", payload, nil)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer client.CloseNow()
	payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello","stream":true}`)
	require.NoError(t, client.Write(ctx, coderws.MessageText, payload))
	_, _, readErr := client.Read(ctx)
	if followUp != "" {
		require.NoError(t, readErr, "first upstream frame must arrive before testing the next write")
		if followUp == "conversation.item.create" {
			payload = []byte(`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[]}}`)
		}
		require.NoError(t, client.Write(ctx, coderws.MessageText, payload))
		_, _, readErr = client.Read(ctx)
	} else if readErr == nil {
		_, _, readErr = client.Read(ctx)
	}
	_ = client.CloseNow()
	select {
	case serverErr := <-done:
		return serverErr
	case <-time.After(5 * time.Second):
		t.Fatal("passthrough session did not terminate")
		return nil
	}
}
