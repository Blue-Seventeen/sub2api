//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

func preflightPoolAccount(platform string) *Account {
	return &Account{
		ID: 9301, Name: "sse-preflight-fixture", Platform: platform, Type: AccountTypeAPIKey,
		Extra: map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
		Credentials: map[string]any{
			"api_key": "test-key", "base_url": "https://relay.example.com",
			"pool_mode": true, "pool_mode_retry_count": 2,
			"pool_mode_retry_status_codes": []any{float64(502), float64(503), float64(529)},
		},
	}
}

func assertPreflightFailover(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder, result *ForwardResult, err error, status int) {
	t.Helper()
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("error = %T (%v), want *UpstreamFailoverError before downstream output", err, err)
	}
	if failoverErr.StatusCode != status {
		t.Fatalf("failover status = %d, want %d", failoverErr.StatusCode, status)
	}
	if !failoverErr.RetryableOnSameAccount {
		t.Fatal("configured pool failure did not retain same-account retry disposition")
	}
	if result != nil || c.Writer.Written() || rec.Body.Len() != 0 {
		t.Fatalf("failed attempt leaked output/result: result=%+v written=%v body=%q", result, c.Writer.Written(), rec.Body.String())
	}
	if got := rec.Header().Get("X-Request-Id"); got != "" {
		t.Fatalf("failed attempt leaked upstream request ID %q", got)
	}
}

func TestGatewaySSEPreflightCompatibleRecoverableError(t *testing.T) {
	for _, tc := range []struct {
		name, platform, request, upstream string
		route                             CompatibleRequestRoute
	}{
		{"chat", PlatformZhipu, `{"model":"glm-4.6","messages":[],"stream":true}`, "data: {\"error\":{\"code\":503,\"message\":\"busy\"}}\n\n", CompatibleRouteChatCompletions},
		{"chat_to_responses", PlatformZhipu, `{"model":"glm-4.6","input":"hi","stream":true}`, "data: {\"error\":{\"code\":503,\"message\":\"busy\"}}\n\n", CompatibleRouteResponses},
		{"chat_to_messages", PlatformZhipu, `{"model":"glm-4.6","messages":[],"max_tokens":16,"stream":true}`, "data: {\"error\":{\"code\":503,\"message\":\"busy\"}}\n\n", CompatibleRouteMessages},
		{"native_responses_metadata", PlatformAli, `{"model":"qwen-max","input":"hi","stream":true}`, "event: response.created\ndata: {\"type\":\"response.created\"}\n\nevent: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":503,\"message\":\"busy\"}}}\n\n", CompatibleRouteResponses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			resp := newCompatibleGatewayEventStreamResponse(http.StatusOK, tc.upstream)
			resp.Header.Set("X-Request-Id", "failed-attempt")
			svc := newCompatibleGatewayServiceForTest(&compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{resp}})
			result, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(tc.platform), tc.route, []byte(tc.request))
			assertPreflightFailover(t, c, rec, result, err, http.StatusServiceUnavailable)
		})
	}
}

func TestGatewaySSEPreflightNewAPIRecoverableError(t *testing.T) {
	for _, tc := range []struct {
		name, upstream string
		route          NewAPIStyleRoute
		status         int
	}{
		{"chat_numeric_error", "data: {\"error\":{\"code\":503,\"message\":\"busy\"}}\n\n", NewAPIStyleRouteChatCompletions, 503},
		{"chat_compact_string_error", ": keepalive\r\n\r\ndata:{\"error\":{\"code\":\"503\",\"message\":\"busy\"}}\r\n\r\n", NewAPIStyleRouteChatCompletions, 503},
		{"responses_metadata_then_error", "event: response.created\ndata: {\"type\":\"response.created\"}\n\nevent: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":503,\"message\":\"busy\"}}}\n\n", NewAPIStyleRouteResponses, 503},
		{"messages_overloaded", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"busy\"}}\n\n", NewAPIStyleRouteMessages, 529},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			resp := newCompatibleGatewayEventStreamResponse(http.StatusOK, tc.upstream)
			resp.Header.Set("X-Request-Id", "failed-attempt")
			svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: resp}}
			platform := PlatformOpenRouter
			switch tc.route {
			case NewAPIStyleRouteResponses:
				platform = PlatformOpenAI
			case NewAPIStyleRouteMessages:
				platform = PlatformAnthropic
			}
			result, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(platform), NewAPIStyleForwardOptions{
				Route: tc.route, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","stream":true,"messages":[]}`), Stream: true,
			})
			assertPreflightFailover(t, c, rec, result, err, tc.status)
		})
	}
}

func TestGatewaySSEPreflightEmptyStreamFailsOverBeforeCommit(t *testing.T) {
	for _, body := range []string{"", ": keepalive\n\n", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"} {
		for _, provider := range []string{"compatible", "newapi"} {
			t.Run(provider+"/"+body, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				resp := newCompatibleGatewayEventStreamResponse(http.StatusOK, body)
				account := preflightPoolAccount(PlatformZhipu)
				var result *ForwardResult
				var err error
				if provider == "compatible" {
					svc := newCompatibleGatewayServiceForTest(&compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{resp}})
					result, _, err = svc.Forward(context.Background(), c, account, CompatibleRouteChatCompletions, []byte(`{"model":"glm-4.6","messages":[],"stream":true}`))
				} else {
					svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: resp}}
					account.Platform = PlatformOpenRouter
					result, _, err = svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","stream":true}`), Stream: true})
				}
				assertPreflightFailover(t, c, rec, result, err, http.StatusBadGateway)
			})
		}
	}
}

func TestGatewaySSEPreflightDoesNotReplayConsumedOutput(t *testing.T) {
	for _, prefix := range []string{
		`{"choices":[{"delta":{"content":"hello"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"lookup","arguments":"{"}}]}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":0}}`,
	} {
		t.Run(prefix, func(t *testing.T) {
			body := "data: " + prefix + "\n\ndata: {\"error\":{\"code\":503,\"message\":\"busy\"}}\n\n"
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: newCompatibleGatewayEventStreamResponse(http.StatusOK, body)}}
			_, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(PlatformOpenRouter), NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","stream":true}`), Stream: true})
			var failoverErr *UpstreamFailoverError
			if errors.As(err, &failoverErr) || !strings.Contains(rec.Body.String(), prefix) {
				t.Fatalf("consumed semantic/usage event must not replay: error=%v body=%q", err, rec.Body.String())
			}
		})
	}
}

func TestGatewaySSEPreflightGeminiBeforeSyntheticStart(t *testing.T) {
	for _, input := range []string{"", ": keepalive\n\n", "data: {\"candidates\":[]}\n\n", "data: {\"modelVersion\":\"gemini-2.5-flash\",\"responseId\":\"metadata-only\"}\n\n", "data: {\"error\":{\"code\":503,\"status\":\"UNAVAILABLE\",\"message\":\"busy\"}}\n\n"} {
		for _, route := range []string{"native", "messages", "chat"} {
			t.Run(route+"/"+input, func(t *testing.T) {
				svc := newGeminiSignalService("text/event-stream", input)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				account := preflightPoolAccount(PlatformGemini)
				var result *ForwardResult
				var err error
				switch route {
				case "native":
					result, err = svc.ForwardNative(context.Background(), c, account, "gemini-2.5-flash", "streamGenerateContent", true, geminiSignalTestRequest())
				case "messages":
					result, err = svc.Forward(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
				case "chat":
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
				}
				status := http.StatusBadGateway
				if strings.Contains(input, "503") {
					status = http.StatusServiceUnavailable
				}
				assertPreflightFailover(t, c, rec, result, err, status)
			})
		}
	}
}

func TestGatewaySSEPreflightCompatiblePoolDelegatesRetryBudget(t *testing.T) {
	upstream := &compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{
		newCompatibleGatewayHTTPResponse(503, `{"error":{"message":"busy"}}`),
		newCompatibleGatewayHTTPResponse(200, `{"choices":[{"message":{"content":"wrong hidden retry"}}]}`),
	}}
	svc := newCompatibleGatewayServiceForTest(upstream)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(PlatformZhipu), CompatibleRouteChatCompletions, []byte(`{"model":"glm-4.6","messages":[]}`))
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) || !failoverErr.RetryableOnSameAccount || len(upstream.urls) != 1 {
		t.Fatalf("pool budget must be consumed by handler only: error=%v calls=%d", err, len(upstream.urls))
	}
}

func TestGatewaySSEPreflightRequestPolicyErrorDoesNotRetry(t *testing.T) {
	for _, code := range []string{"cyber_policy", "content_policy_violation"} {
		for _, envelope := range []string{"chat", "responses"} {
			t.Run(code+"/"+envelope, func(t *testing.T) {
				payload := `{"error":{"code":"` + code + `","status":503,"message":"Request blocked by content policy"}}`
				protocol := upstreamSSEChat
				if envelope == "responses" {
					payload = `{"type":"response.failed","response":` + payload + `}`
					protocol = upstreamSSEResponses
				}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\n"))}
				failure := inspectUpstreamSSEPrefix(context.Background(), response, protocol, 0)
				if failure == nil {
					t.Fatal("expected the structured terminal upstream error to be recognized")
				}
				account := preflightPoolAccount(PlatformAli)
				account.Credentials["custom_error_codes_enabled"] = true
				account.Credentials["custom_error_codes"] = []any{float64(503)}
				repo := &errorPolicyRepoStub{}
				rateLimit := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
				if failover := failure.failover(context.Background(), rateLimit, account, nil, "qwen-max"); failover != nil {
					t.Fatalf("request policy rejection must not rotate credentials despite wrapped 503: %+v", failover)
				}
				if repo.setErrCalls != 0 {
					t.Fatal("request policy rejection must not disable the selected credential")
				}
				replayed, err := io.ReadAll(response.Body)
				if err != nil || string(replayed) != "data: "+payload+"\n\n" {
					t.Fatalf("terminal event must remain available to the protocol relay: bytes=%q err=%v", replayed, err)
				}
				_ = response.Body.Close()
			})
		}
	}
}

func TestGatewaySSEPreflightPoolConfiguredStatusAndCustomPriority(t *testing.T) {
	for _, provider := range []string{"compatible", "newapi"} {
		for _, tc := range []struct {
			name       string
			status     int
			configured []any
			custom     bool
			wantRetry  bool
		}{
			{"configured_503", 503, []any{float64(503)}, false, true},
			{"unconfigured_503", 503, []any{float64(502)}, false, false},
			{"configured_404_after_endpoint_fallback", 404, []any{float64(404)}, false, true},
			{"custom_disable_precedes_retry", 503, []any{float64(503)}, true, false},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				account := preflightPoolAccount(PlatformZhipu)
				account.Credentials["pool_mode_retry_status_codes"] = tc.configured
				if tc.custom {
					account.Credentials["custom_error_codes_enabled"] = true
					account.Credentials["custom_error_codes"] = []any{float64(tc.status)}
				}
				repo := &errorPolicyRepoStub{}
				gateway := &GatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				var err error
				if provider == "compatible" {
					upstream := &compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{
						newCompatibleGatewayHTTPResponse(tc.status, `{"error":{"message":"busy"}}`),
						newCompatibleGatewayHTTPResponse(tc.status, `{"error":{"message":"busy"}}`),
					}}
					svc := newCompatibleGatewayServiceForTest(upstream)
					svc.gatewayService.rateLimitService = gateway.rateLimitService
					_, _, err = svc.Forward(context.Background(), c, account, CompatibleRouteChatCompletions, []byte(`{"model":"glm-4.6","messages":[]}`))
				} else {
					account.Platform = PlatformOpenRouter
					svc := &NewAPIStyleGatewayService{gatewayService: gateway, httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: newCompatibleGatewayHTTPResponse(tc.status, `{"error":{"message":"busy"}}`)}}
					_, _, err = svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[]}`)})
				}
				var failoverErr *UpstreamFailoverError
				if !errors.As(err, &failoverErr) {
					t.Fatalf("error=%T (%v), want failover after endpoint alternatives", err, err)
				}
				if failoverErr.RetryableOnSameAccount != tc.wantRetry {
					t.Fatalf("same-account retry=%v, want %v", failoverErr.RetryableOnSameAccount, tc.wantRetry)
				}
				if tc.custom && repo.setErrCalls != 1 {
					t.Fatalf("explicit custom policy mutations=%d, want 1 before switching accounts", repo.setErrCalls)
				}
			})
		}
	}
}

type preflightSemanticSignalWriter struct {
	gin.ResponseWriter
	once     sync.Once
	semantic chan struct{}
}

func (w *preflightSemanticSignalWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n > 0 && strings.Contains(string(p[:n]), "first") {
		w.once.Do(func() { close(w.semantic) })
	}
	return n, err
}

func (w *preflightSemanticSignalWriter) WriteString(p string) (int, error) {
	return w.Write([]byte(p))
}

func TestGatewaySSEPreflightReleasesFirstSemanticBeforeTerminal(t *testing.T) {
	for _, provider := range []string{"compatible", "newapi"} {
		t.Run(provider, func(t *testing.T) {
			prefix := ": preserved-comment\n\ndata: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n"
			first := "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n"
			terminal := "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			releaseTerminal := make(chan struct{})
			go func() {
				_, _ = io.WriteString(writer, prefix+first)
				<-releaseTerminal
				_, _ = io.WriteString(writer, terminal)
				_ = writer.Close()
			}()
			resp := newCompatibleGatewayEventStreamResponse(http.StatusOK, "")
			resp.Body = reader
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			semantic := make(chan struct{})
			c.Writer = &preflightSemanticSignalWriter{ResponseWriter: c.Writer, semantic: semantic}
			finished := make(chan error, 1)
			go func() {
				var err error
				if provider == "compatible" {
					svc := newCompatibleGatewayServiceForTest(&compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{resp}})
					_, _, err = svc.Forward(context.Background(), c, preflightPoolAccount(PlatformZhipu), CompatibleRouteChatCompletions, []byte(`{"model":"glm-4.6","messages":[],"stream":true}`))
				} else {
					svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: resp}}
					_, _, err = svc.Forward(context.Background(), c, preflightPoolAccount(PlatformOpenRouter), NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","stream":true}`), Stream: true})
				}
				finished <- err
			}()
			releasedEarly := false
			select {
			case <-semantic:
				releasedEarly = true
			case <-time.After(2 * time.Second):
			}
			close(releaseTerminal)
			if err := <-finished; err != nil {
				t.Fatalf("stream forwarding error=%v", err)
			}
			if !releasedEarly {
				t.Fatal("first semantic event was held until terminal/EOF")
			}
			if got := rec.Body.String(); got != prefix+first+terminal {
				t.Fatalf("stream prefix replay changed bytes: got=%q", got)
			}
		})
	}
}

func TestGatewaySSEPreflightTaskTransportFailureDoesNotResubmit(t *testing.T) {
	for _, tc := range []struct {
		name, platform, method, path string
		route                        NewAPIStyleRoute
	}{
		{"create_video", PlatformOpenAI, http.MethodPost, "/v1/videos", NewAPIStyleRouteVideo},
		{"existing_task", PlatformSuno, http.MethodGet, "/suno/task/task_accepted", NewAPIStyleRouteTask},
		{"create_image_tool", PlatformOpenAI, http.MethodPost, "/v1/responses", NewAPIStyleRouteResponses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{err: io.ErrUnexpectedEOF}}
			_, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(tc.platform), NewAPIStyleForwardOptions{
				Route: tc.route, Method: tc.method, InboundPath: tc.path,
				RequestBody: []byte(`{"model":"gpt-5.1","tools":[{"type":"image_generation"}]}`),
			})
			var failoverErr *UpstreamFailoverError
			if err == nil || errors.As(err, &failoverErr) {
				t.Fatalf("ambiguous accepted task must not request cross-account resubmission: error=%T %v", err, err)
			}
		})
	}
}

func TestGatewaySSEPreflightExistingTaskHTTPErrorKeepsOwner(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: newCompatibleGatewayHTTPResponse(503, `{"error":{"message":"busy"}}`)}}
	_, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(PlatformSuno), NewAPIStyleForwardOptions{
		Route: NewAPIStyleRouteTask, Method: http.MethodGet, InboundPath: "/suno/task/task_accepted",
	})
	var failoverErr *UpstreamFailoverError
	if err == nil || errors.As(err, &failoverErr) {
		t.Fatalf("existing task must retain its selected owner: error=%T %v", err, err)
	}
}

func TestGatewaySSEPreflightAcceptedMediaStreamReadErrorDoesNotResubmit(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body: io.NopCloser(&newAPIStyleFailingReader{}),
	}}}
	_, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(PlatformOpenAI), NewAPIStyleForwardOptions{
		Route: NewAPIStyleRouteAudio, Method: http.MethodPost, InboundPath: "/v1/audio/speech", Model: "tts-1", Stream: true,
		RequestBody: []byte(`{"model":"tts-1","input":"hello","stream":true}`),
	})
	var failoverErr *UpstreamFailoverError
	if err == nil || errors.As(err, &failoverErr) {
		t.Fatalf("HTTP200 media acceptance must not resubmit after an empty read failure: error=%T %v", err, err)
	}
}

func TestGatewaySSEPreflightNonStreamErrorEnvelopeBeforeCommit(t *testing.T) {
	for _, provider := range []string{"compatible", "newapi"} {
		t.Run(provider, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			resp := newCompatibleGatewayHTTPResponse(http.StatusOK, `{"error":{"code":503,"message":"busy"}}`)
			resp.Header.Set("X-Request-Id", "failed-attempt")
			var result *ForwardResult
			var err error
			if provider == "compatible" {
				svc := newCompatibleGatewayServiceForTest(&compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{resp}})
				result, _, err = svc.Forward(context.Background(), c, preflightPoolAccount(PlatformZhipu), CompatibleRouteChatCompletions, []byte(`{"model":"glm-4.6","messages":[]}`))
			} else {
				svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: resp}}
				result, _, err = svc.Forward(context.Background(), c, preflightPoolAccount(PlatformOpenRouter), NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[]}`)})
			}
			assertPreflightFailover(t, c, rec, result, err, http.StatusServiceUnavailable)
		})
	}
}

func TestGatewaySSEPreflightHeartbeatThenErrorRemainsReplayable(t *testing.T) {
	for _, heartbeat := range []string{
		"event: ping\ndata: {\"type\":\"ping\"}\n\n",
		"event: keepalive\ndata: {\"type\":\"keepalive\"}\n\n",
	} {
		t.Run(heartbeat, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			resp := newCompatibleGatewayEventStreamResponse(200, heartbeat+"data: {\"error\":{\"code\":503,\"message\":\"busy\"}}\n\n")
			svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: resp}}
			result, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(PlatformOpenRouter), NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, Stream: true, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","stream":true}`)})
			assertPreflightFailover(t, c, rec, result, err, http.StatusServiceUnavailable)
		})
	}
}

func TestGatewaySSEPreflightSafeInferencePOSTUsesHandlerRetryBudget(t *testing.T) {
	for _, tc := range []struct {
		name, platform, model, path string
		route                       NewAPIStyleRoute
	}{
		{"embeddings", PlatformOpenAI, "text-embedding-3-small", "/v1/embeddings", NewAPIStyleRouteEmbeddings},
		{"rerank", PlatformSiliconFlow, "BAAI/bge-reranker-v2-m3", "/v1/rerank", NewAPIStyleRouteRerank},
		{"speech", PlatformOpenAI, "tts-1", "/v1/audio/speech", NewAPIStyleRouteAudio},
		{"transcription", PlatformOpenAI, "whisper-1", "/v1/audio/transcriptions", NewAPIStyleRouteAudio},
	} {
		for _, transport := range []bool{true, false} {
			t.Run(tc.name+"/transport="+strconv.FormatBool(transport), func(t *testing.T) {
				account := preflightPoolAccount(tc.platform)
				upstream := &newAPIStyleOpsHTTPUpstream{}
				if transport {
					upstream.err = io.ErrUnexpectedEOF
				} else {
					upstream.resp = newCompatibleGatewayHTTPResponse(503, `{"error":{"message":"busy"}}`)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
				_, _, err := svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{Route: tc.route, Method: http.MethodPost, Model: tc.model, InboundPath: tc.path, RequestBody: []byte(`{"input":"hello"}`)})
				var failoverErr *UpstreamFailoverError
				if !errors.As(err, &failoverErr) {
					t.Fatalf("safe stateless inference must retain failover: error=%T %v", err, err)
				}
				if !transport && !failoverErr.RetryableOnSameAccount {
					t.Fatal("configured HTTP503 must retain handler-owned same-account budget")
				}
			})
		}
	}
}

func TestGatewaySSEPreflightGeminiPoolHTTPBudgetIsHandlerOwned(t *testing.T) {
	for _, route := range []string{"native", "messages", "chat"} {
		for _, policyBackend := range []bool{false, true} {
			t.Run(route+"/policy="+strconv.FormatBool(policyBackend), func(t *testing.T) {
				upstream := &compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{
					newCompatibleGatewayHTTPResponse(503, `{"error":{"code":503,"status":"UNAVAILABLE","message":"busy"}}`),
					newCompatibleGatewayHTTPResponse(200, `{"candidates":[{"content":{"parts":[{"text":"unexpected internal retry"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`),
				}}
				svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
				if policyBackend {
					svc.rateLimitService = NewRateLimitService(&errorPolicyRepoStub{}, nil, &config.Config{}, nil, nil)
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				account := preflightPoolAccount(PlatformGemini)
				account.Credentials["pool_mode_retry_count"] = 1
				var result *ForwardResult
				var err error
				switch route {
				case "native":
					result, err = svc.ForwardNative(context.Background(), c, account, "gemini-2.5-flash", "generateContent", false, geminiSignalTestRequest())
				case "messages":
					result, err = svc.Forward(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
				case "chat":
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`))
				}
				var failoverErr *UpstreamFailoverError
				if !errors.As(err, &failoverErr) || !failoverErr.RetryableOnSameAccount || len(upstream.urls) != 1 || result != nil || c.Writer.Written() {
					t.Fatalf("Gemini service consumed handler retry budget: err=%v calls=%d result=%+v written=%v", err, len(upstream.urls), result, c.Writer.Written())
				}
			})
		}
	}
}

func TestGatewaySSEPreflightConfigured400OnlyRetriesRecoverableFailures(t *testing.T) {
	for _, body := range []string{
		`{"error":{"type":"invalid_request_error","message":"missing messages"}}`,
		`{"error":{"code":"invalid_json","message":"Invalid JSON body"}}`,
		`{"error":{"code":"context_length_exceeded","message":"maximum context length exceeded"}}`,
		`{"error":{"message":"invalid JSON in request body"}}`,
	} {
		for _, provider := range []string{"compatible", "newapi"} {
			t.Run(provider+"/"+body, func(t *testing.T) {
				account := preflightPoolAccount(PlatformZhipu)
				account.Credentials["pool_mode_retry_status_codes"] = []any{float64(400)}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				var err error
				if provider == "compatible" {
					svc := newCompatibleGatewayServiceForTest(&compatibleGatewayHTTPUpstreamRecorder{responses: []*http.Response{newCompatibleGatewayHTTPResponse(400, body)}})
					_, _, err = svc.Forward(context.Background(), c, account, CompatibleRouteChatCompletions, []byte(`{"model":"glm-4.6","messages":[]}`))
				} else {
					account.Platform = PlatformOpenRouter
					svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: newCompatibleGatewayHTTPResponse(400, body)}}
					_, _, err = svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[]}`)})
				}
				var failoverErr *UpstreamFailoverError
				if err == nil || errors.As(err, &failoverErr) {
					t.Fatalf("definitive bad request must not be replayed despite configured400: error=%T %v", err, err)
				}
			})
		}
	}
	t.Run("temporary_processing_remains_configurable", func(t *testing.T) {
		account := preflightPoolAccount(PlatformOpenRouter)
		account.Credentials["pool_mode_retry_status_codes"] = []any{float64(400)}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: newCompatibleGatewayHTTPResponse(400, `{"error":{"type":"processing_error","message":"Temporary processing failure, retry"}}`)}}
		_, _, err := svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions, RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[]}`)})
		var failoverErr *UpstreamFailoverError
		if !errors.As(err, &failoverErr) || !failoverErr.RetryableOnSameAccount {
			t.Fatalf("recoverable400 must consume configured budget: error=%v", err)
		}
	})
}

func TestGatewaySSEPreflightAudioErrorBeforeMediaIsNotCommitted(t *testing.T) {
	for _, route := range []NewAPIStyleRoute{NewAPIStyleRouteAudio, NewAPIStyleRouteQwenTTS} {
		t.Run(string(route), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			platform, model, path := PlatformZhipu, "glm-tts", "/v1/audio/speech"
			if route == NewAPIStyleRouteQwenTTS {
				platform, model, path = PlatformAli, "qwen3-tts-flash", aliQwenTTSGenerationPath
			}
			svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: newCompatibleGatewayEventStreamResponse(200, "event: error\ndata: {\"error\":{\"code\":503,\"message\":\"busy\"}}\n\n")}}
			result, _, err := svc.Forward(context.Background(), c, preflightPoolAccount(platform), NewAPIStyleForwardOptions{
				Route: route, Method: http.MethodPost, InboundPath: path, Model: model, Stream: true,
				RequestBody: []byte(`{"model":"` + model + `","input":"hello","stream":true}`),
			})
			assertPreflightFailover(t, c, rec, result, err, 503)
		})
	}
}

func TestGatewaySSEPreflightGuardsBillableUsageAliasesBeforeError(t *testing.T) {
	// These aliases are already accepted by the existing NewAPI usage parser.
	// Any one of them makes an otherwise retryable error unsafe to replay.
	for _, usage := range []string{
		`{"cached_tokens":8}`,
		`{"cache_write_input_tokens":8}`,
		`{"cache_write_tokens":8}`,
		`{"cache_creation_tokens":8}`,
		`{"cache_creation":{"ephemeral_5m_input_tokens":8}}`,
		`{"cache_creation":{"ephemeral_1h_input_tokens":8}}`,
		`{"input_tokens_details":{"cached_tokens":8}}`,
		`{"prompt_tokens_details":{"cached_tokens":8}}`,
		`{"input_tokens_details":{"cache_write_tokens":8}}`,
		`{"input_tokens_details":{"cache_creation_tokens":8}}`,
		`{"input_tokens_details":{"cache_creation_input_tokens":8}}`,
		`{"prompt_tokens_details":{"cache_write_tokens":8}}`,
		`{"prompt_tokens_details":{"cache_creation_tokens":8}}`,
		`{"prompt_tokens_details":{"cache_creation_input_tokens":8}}`,
		`{"image_output_tokens":8}`,
		`{"output_tokens_details":{"image_tokens":8}}`,
		`{"completion_tokens_details":{"image_tokens":8}}`,
	} {
		for _, nesting := range []string{"usage", "response", "message"} {
			t.Run(nesting+"/"+usage, func(t *testing.T) {
				payload := `{"error":{"code":503,"message":"busy"},"usage":` + usage + `}`
				if nesting != "usage" {
					payload = `{"error":{"code":503,"message":"busy"},"` + nesting + `":{"usage":` + usage + `}}`
				}
				boundary, failure := classifyUpstreamSSEEvent([]byte("data: "+payload+"\n\n"), upstreamSSEChat)
				if !boundary || failure != nil {
					t.Fatalf("already billable usage must prevent replay: boundary=%v failure=%v", boundary, failure)
				}
			})
		}
	}
}

func TestGatewaySSEPreflightGuardsDisabledIntervalTimeout(t *testing.T) {
	cfg := &config.Config{}
	if got := upstreamSSEIdleTimeout(cfg); got != 0 {
		t.Fatalf("explicitly disabled stream interval timeout must stay disabled, got %v", got)
	}
	cfg.Gateway.StreamDataIntervalTimeout = 30
	if got := upstreamSSEIdleTimeout(cfg); got != 30*time.Second {
		t.Fatalf("enabled stream interval timeout = %v, want 30s", got)
	}
}

// A Reader may return useful bytes and a one-time error together, then EOF.
// The preflight must restore both, not just the useful bytes.
type preflightOneShotErrorReader struct {
	payload []byte
	err     error
}

func (r *preflightOneShotErrorReader) Read(p []byte) (int, error) {
	if len(r.payload) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.payload)
	r.payload = r.payload[n:]
	if len(r.payload) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestGatewaySSEPreflightGuardsOneShotReadErrorOnReplay(t *testing.T) {
	for _, protocol := range []string{"json", "sse"} {
		t.Run(protocol, func(t *testing.T) {
			payload := `{"choices":[{"message":{"content":"partial"}}]}`
			contentType := "application/json"
			if protocol == "sse" {
				payload = `data: {"choices":[{"delta":{"content":"partial"}}]}`
				contentType = "text/event-stream"
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
				Body: io.NopCloser(&preflightOneShotErrorReader{payload: []byte(payload), err: io.ErrUnexpectedEOF})}
			var failure *upstreamSSEPreflightFailure
			if protocol == "json" {
				failure = inspectUpstreamJSONError(resp, nil)
			} else {
				failure = inspectUpstreamSSEPrefix(context.Background(), resp, upstreamSSEChat, 0)
			}
			if failure != nil {
				t.Fatalf("semantic payload must retain the existing non-replay relay path: %v", failure)
			}
			data, err := io.ReadAll(resp.Body)
			if string(data) != payload || !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("replay lost bytes or one-shot error: bytes=%q error=%v", data, err)
			}
			if n, err := resp.Body.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
				t.Fatalf("one-shot error must not repeat: n=%d error=%v", n, err)
			}
		})
	}
}

func TestGatewaySSEPreflightGuardsLargePrefixFallsBackWithoutReplay(t *testing.T) {
	for _, eventPrefix := range []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"id":"accepted-tool","function":{"arguments":"`,
		`: `,
	} {
		t.Run(eventPrefix, func(t *testing.T) {
			payload := eventPrefix + strings.Repeat("x", upstreamSSEPreflightMaxBytes+1)
			if strings.HasPrefix(eventPrefix, "data:") {
				payload += `"}}]}}]}` + "\n\n"
			} else {
				payload += "\n\ndata: {\"error\":{\"code\":503}}\n\n"
			}
			resp := newCompatibleGatewayEventStreamResponse(http.StatusOK, payload)
			if failure := inspectUpstreamSSEPrefix(context.Background(), resp, upstreamSSEChat, 0); failure != nil {
				t.Fatalf("oversized unknown prefix must fall back to the existing relay, never request replay: %v", failure)
			}
			data, err := io.ReadAll(resp.Body)
			if err != nil || string(data) != payload {
				t.Fatalf("fallback must preserve all bytes: got=%d want=%d error=%v", len(data), len(payload), err)
			}
		})
	}
}
