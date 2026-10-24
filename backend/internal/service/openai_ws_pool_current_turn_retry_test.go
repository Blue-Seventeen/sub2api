package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIWSPoolReconnectFailureDialer struct {
	first openAIWSClientConn
	calls atomic.Int32
}

func (d *openAIWSPoolReconnectFailureDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	if d.calls.Add(1) == 1 {
		return d.first, 0, nil, nil
	}
	return nil, http.StatusServiceUnavailable, nil, errors.New("fixture reconnect 503")
}

func TestOpenAIWSPoolLaterTurnAcquireFailureCarriesCurrentTurn(t *testing.T) {
	for _, withReplayInput := range []bool{true, false} {
		t.Run(map[bool]string{true: "complete-input", false: "missing-input"}[withReplayInput], func(t *testing.T) {

			prevPreflightPingIdle := openAIWSIngressPreflightPingIdle
			openAIWSIngressPreflightPingIdle = 0
			defer func() {
				openAIWSIngressPreflightPingIdle = prevPreflightPingIdle
			}()

			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.Enabled = true
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

			firstConn := &openAIWSPreflightFailConn{
				events: [][]byte{
					[]byte(`{"type":"response.completed","response":{"id":"resp_turn_ping_1","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
				},
			}
			dialer := &openAIWSPoolReconnectFailureDialer{first: firstConn}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(dialer)

			svc := &OpenAIGatewayService{
				cfg:              cfg,
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}

			account := &Account{
				ID:          116,
				Name:        "openai-ingress-preflight-ping",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key": "sk-test", "pool_mode": true, "pool_mode_retry_status_codes": []any{float64(503)},
				},
				Extra: map[string]any{
					"responses_websockets_v2_enabled": true,
				},
			}

			serverErrCh := make(chan error, 1)
			wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{
					CompressionMode: coderws.CompressionContextTakeover,
				})
				if err != nil {
					serverErrCh <- err
					return
				}
				defer func() {
					_ = conn.CloseNow()
				}()

				rec := httptest.NewRecorder()
				ginCtx, _ := gin.CreateTestContext(rec)
				req := r.Clone(r.Context())
				req.Header = req.Header.Clone()
				req.Header.Set("User-Agent", "unit-test-agent/1.0")
				ginCtx.Request = req

				readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				msgType, firstMessage, readErr := conn.Read(readCtx)
				cancel()
				if readErr != nil {
					serverErrCh <- readErr
					return
				}
				if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
					serverErrCh <- errors.New("unsupported websocket client message type")
					return
				}

				serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, nil)
			}))
			defer wsServer.Close()

			dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
			clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
			cancelDial()
			require.NoError(t, err)
			defer func() {
				_ = clientConn.CloseNow()
			}()

			writeMessage := func(payload string) {
				writeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(payload)))
			}
			readMessage := func() []byte {
				readCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				msgType, message, readErr := clientConn.Read(readCtx)
				require.NoError(t, readErr)
				require.Equal(t, coderws.MessageText, msgType)
				return message
			}

			firstPayload := `{"type":"response.create","model":"gpt-5.1","stream":false}`
			secondPayload := `{"type":"response.create","model":"gpt-5.1","stream":false}`
			if withReplayInput {
				firstPayload = `{"type":"response.create","model":"gpt-5.1","stream":false,"input":[{"role":"user","content":"first-turn"}]}`
				secondPayload = `{"type":"response.create","model":"gpt-5.1","stream":false,"previous_response_id":"resp_turn_ping_1","input":[{"role":"user","content":"second-turn"}]}`
			}
			writeMessage(firstPayload)
			firstTurn := readMessage()
			require.Equal(t, "resp_turn_ping_1", gjson.GetBytes(firstTurn, "response.id").String())

			writeMessage(secondPayload)
			select {
			case serverErr := <-serverErrCh:
				var failoverErr *UpstreamFailoverError
				require.ErrorAs(t, serverErr, &failoverErr)
				require.Equal(t, 503, failoverErr.StatusCode)
				payload, currentTurn := OpenAIWSCurrentTurnRetryPayload(serverErr)
				require.True(t, currentTurn, "later-turn handshake failure must never replay the retained first request")
				if withReplayInput {
					require.Contains(t, string(payload), "second-turn")
					require.Contains(t, string(payload), "first-turn", "complete conversation input must survive the account switch")
					require.False(t, gjson.GetBytes(payload, "previous_response_id").Exists())
				} else {
					require.Empty(t, payload, "missing replay history must stop recovery rather than fall back to the first request")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("later-turn handshake failure was not returned")
			}
			require.Equal(t, 1, firstConn.WriteCount(), "failed reconnect must not resend either business turn to the dead connection")
		})
	}
}
