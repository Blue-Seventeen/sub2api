//go:build unit

package service

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// These cases catch replay after upstream acceptance, not only after output.
func TestNativeResponsesAcceptedFailureBoundary(t *testing.T) {
	for _, route := range []string{"stream", "passthrough-stream", "buffered", "passthrough-buffered"} {
		for _, fixture := range []struct {
			name, prefix, usage, code string
			tokens                    bool
		}{
			{"unaccepted", "", "", `"503"`, false},
			{"terminal-tokens", "", `,"usage":{"input_tokens":9,"output_tokens":1}`, `"503"`, true},
			{"numeric-code-tokens", "", `,"usage":{"input_tokens":9,"output_tokens":1}`, `503`, true},
			{"terminal-alias", "", `,"usage":{"billable_seconds":2}`, `503`, false},
			{"prior-alias", "data: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"billable_seconds\":2}}}\n\n", "", `503`, false},
			{"prior-tool", "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"call_1\",\"name\":\"lookup\"}}\n\n", "", `503`, false},
			{"terminal-tool", "", `,"output":[{"type":"function_call","id":"call_1","name":"lookup","arguments":"{}"}]`, `503`, false},
			{"terminal-text", "", `,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"accepted output"}]}]`, `503`, false},
		} {
			t.Run(route+"/"+fixture.name, func(t *testing.T) {
				data := fixture.prefix + `data: {"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"code":` + fixture.code + `,"type":"server_error","message":"temporary upstream failure"}` + fixture.usage + "}}\n\n"
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				account := preflightPoolAccount(PlatformOpenAI)
				account.Credentials["pool_mode_retry_status_codes"] = []any{float64(503)}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), toolCorrector: NewCodexToolCorrector()}
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))}
				var usage *OpenAIUsage
				var err error
				switch route {
				case "stream":
					r, e := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "fixture", "fixture")
					err = e
					if r != nil {
						usage = r.usage
					}
				case "passthrough-stream":
					r, e := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "fixture", "fixture")
					err = e
					if r != nil {
						usage = r.usage
					}
				case "buffered":
					r, e := svc.handleSSEToJSON(resp, c, account, []byte(data), "fixture", "fixture")
					err = e
					if r != nil {
						usage = r.usage
					}
				case "passthrough-buffered":
					r, e := svc.handlePassthroughSSEToJSON(resp, c, account, []byte(data), "fixture", "fixture")
					err = e
					if r != nil {
						usage = r.usage
					}
				}
				var failure *UpstreamFailoverError
				require.Error(t, err)
				if fixture.name == "unaccepted" {
					require.ErrorAs(t, err, &failure)
					require.False(t, c.Writer.Written())
					return
				}
				require.NotErrorAs(t, err, &failure)
				require.NotNil(t, usage)
				require.True(t, c.Writer.Written())
				require.NotContains(t, rec.Body.String(), "response.completed")
				if fixture.tokens {
					require.Equal(t, 9, usage.InputTokens)
					require.Equal(t, 1, usage.OutputTokens)
				}
			})
		}
	}
}

func TestNativeAnthropicConvertersDoNotSynthesizeSuccessAfterFailure(t *testing.T) {
	for _, route := range []string{"chat-stream", "responses-stream", "chat-buffered", "responses-buffered"} {
		for _, fixture := range []struct {
			name, usage, tail string
			tokens            bool
		}{
			{"metadata-error", `{}`, "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":503,\"type\":\"overloaded_error\",\"message\":\"temporary failure\"}}\n\n", false},
			{"metadata-eof", `{}`, "", false},
			{"billable-error", `{"input_tokens":9,"output_tokens":1}`, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"temporary failure\"}}\n\n", true},
			{"billable-eof", `{"input_tokens":9,"output_tokens":1}`, "", true},
			{"semantic-eof", `{}`, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"accepted\"}}\n\n", false},
			{"success", `{"input_tokens":9,"output_tokens":1}`, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", true},
		} {
			t.Run(route+"/"+fixture.name, func(t *testing.T) {
				data := `event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"fixture","content":[],"usage":` + fixture.usage + "}}\n\n" + fixture.tail
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
				var result *OpenAIForwardResult
				var err error
				switch route {
				case "chat-stream":
					result, err = svc.handleCCStreamingFromNativeAnthropic(resp, c, "fixture", "fixture", "fixture", nil, time.Now())
				case "responses-stream":
					result, err = svc.handleResponsesStreamingFromNativeAnthropic(resp, c, "fixture", "fixture", "fixture", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				case "chat-buffered":
					result, err = svc.handleCCBufferedFromNativeAnthropic(resp, c, "fixture", "fixture", "fixture", nil, time.Now())
				case "responses-buffered":
					result, err = svc.handleResponsesBufferedFromNativeAnthropic(resp, c, "fixture", "fixture", "fixture", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				}
				if fixture.name == "success" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					var failure *UpstreamFailoverError
					require.NotErrorAs(t, err, &failure)
					require.NotContains(t, rec.Body.String(), "[DONE]")
					require.NotContains(t, rec.Body.String(), "response.completed")
					require.Contains(t, rec.Body.String(), "error")
				}
				if fixture.tokens {
					require.NotNil(t, result)
					require.Equal(t, 9, result.Usage.InputTokens)
					require.Equal(t, 1, result.Usage.OutputTokens)
				}
			})
		}
	}
}

func TestCCBufferedErrorRetainsBillableUsage(t *testing.T) {
	for _, route := range []string{"raw", "responses", "messages"} {
		t.Run(route, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":503,"type":"server_error","message":"temporary failure"},"usage":{"prompt_tokens":9,"completion_tokens":1}}`))}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			account := preflightPoolAccount(PlatformOpenAI)
			var result *OpenAIForwardResult
			var err error
			switch route {
			case "raw":
				result, err = svc.bufferRawChatCompletions(c, resp, account, "fixture", "fixture", "fixture", nil, nil, time.Now())
			case "responses":
				result, err = svc.bufferChatCompletionsAsResponses(c, resp, account, "fixture", nil, nil, false, nil, "fixture", "fixture", nil, nil, time.Now())
			case "messages":
				result, err = svc.bufferChatCompletionsAsAnthropic(c, resp, account, "fixture", "fixture", "fixture", nil, nil, time.Now())
			}
			require.Error(t, err)
			var failure *UpstreamFailoverError
			require.NotErrorAs(t, err, &failure)
			require.NotNil(t, result)
			require.Equal(t, 9, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
		})
	}
}

func TestNativeAnthropicPoolPreflightBeforeConversion(t *testing.T) {
	for _, route := range []string{"chat", "responses", "messages"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", route, stream), func(t *testing.T) {
				account := rawChatCompletionsTestAccount()
				account.Platform = PlatformZhipu
				account.Credentials["pool_mode"] = true
				account.Credentials["pool_mode_retry_status_codes"] = []any{float64(503)}
				account.Credentials["api_protocol"] = "anthropic"
				data := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"role\":\"assistant\",\"content\":[],\"usage\":{}}}\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{\"code\":503,\"type\":\"overloaded_error\",\"message\":\"temporary failure\"}}\n\n"
				ct := "text/event-stream"
				if route == "messages" && !stream {
					data = `{"type":"error","error":{"code":503,"type":"overloaded_error","message":"temporary failure"}}`
					ct = "application/json"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(data))}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				body := []byte(fmt.Sprintf(`{"model":"fixture","max_tokens":10,"messages":[{"role":"user","content":"hello"}],"input":"hello","stream":%v}`, stream))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				var err error
				switch route {
				case "chat":
					_, err = svc.forwardChatCompletionsViaNativeAnthropic(c.Request.Context(), c, account, body, "")
				case "responses":
					_, err = svc.forwardResponsesViaNativeAnthropic(c.Request.Context(), c, account, body, "")
				case "messages":
					_, err = svc.forwardAnthropicViaNativeAnthropicEndpoint(c.Request.Context(), c, account, body, "")
				}
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, 503, failure.StatusCode)
				require.False(t, c.Writer.Written())
			})
		}
	}
}

type relayBoundaryReadFailure struct{}

func (relayBoundaryReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestResponsesBufferedReadFailurePreservesAcceptedUsage(t *testing.T) {
	for _, route := range []string{"native", "passthrough", "chat", "messages"} {
		for _, separator := range []string{"\n\n", ""} {
			t.Run(fmt.Sprintf("%s/separator=%q", route, separator), func(t *testing.T) {
				data := `data: {"type":"response.in_progress","response":{"usage":{"input_tokens":9,"output_tokens":1}}}` + separator
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(io.MultiReader(strings.NewReader(data), relayBoundaryReadFailure{}))}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
				account := preflightPoolAccount(PlatformOpenAI)
				var usage *OpenAIUsage
				var err error
				switch route {
				case "native":
					r, e := svc.handleNonStreamingResponse(c.Request.Context(), resp, c, account, "fixture", "fixture")
					err = e
					if r != nil {
						usage = r.usage
					}
				case "passthrough":
					r, e := svc.handleNonStreamingResponsePassthrough(c.Request.Context(), resp, c, account, "fixture", "fixture")
					err = e
					if r != nil {
						usage = r.usage
					}
				case "chat":
					r, e := svc.handleChatBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
					err = e
					if r != nil {
						usage = &r.Usage
					}
				case "messages":
					r, e := svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
					err = e
					if r != nil {
						usage = &r.Usage
					}
				}
				require.Error(t, err)
				var failure *UpstreamFailoverError
				require.NotErrorAs(t, err, &failure)
				require.NotNil(t, usage)
				require.Equal(t, 9, usage.InputTokens)
				require.Equal(t, 1, usage.OutputTokens)
				require.True(t, c.Writer.Written())
			})
		}
	}
}

func TestResponsesTerminalRulePrecedesPoolRetry(t *testing.T) {
	for _, route := range []string{"native", "passthrough", "native-buffered", "passthrough-buffered", "chat-buffered", "messages-buffered"} {
		for _, bare := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bare=%v", route, bare), func(t *testing.T) {
				data := "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":503,\"type\":\"server_error\",\"message\":\"temporary ruled failure\"}}}\n\n"
				if bare {
					data = "data: {\"type\":\"error\",\"error\":{\"code\":503,\"type\":\"server_error\",\"message\":\"temporary ruled failure\"}}\n\n"
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				bindPassthroughRule(c, PlatformOpenAI, []string{"ruled failure"}, http.StatusTeapot)
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), toolCorrector: NewCodexToolCorrector()}
				account := preflightPoolAccount(PlatformOpenAI)
				var err error
				switch route {
				case "native":
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "fixture", "fixture")
				case "passthrough":
					_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "fixture", "fixture")
				case "native-buffered":
					_, err = svc.handleSSEToJSON(resp, c, account, []byte(data), "fixture", "fixture")
				case "passthrough-buffered":
					_, err = svc.handlePassthroughSSEToJSON(resp, c, account, []byte(data), "fixture", "fixture")
				case "chat-buffered":
					_, err = svc.handleChatBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
				case "messages-buffered":
					_, err = svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
				}
				require.Error(t, err)
				var failure *UpstreamFailoverError
				require.NotErrorAs(t, err, &failure)
				require.Equal(t, http.StatusTeapot, rec.Code)
			})
		}
	}
}

func TestNativeAnthropicPassthroughAcceptedErrorRetainsUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			account := preflightPoolAccount(PlatformZhipu)
			data := `{"type":"error","error":{"code":503,"type":"overloaded_error","message":"temporary failure"},"usage":{"input_tokens":9,"output_tokens":1}}`
			ct := "application/json"
			if stream {
				data = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"usage\":{\"input_tokens\":9,\"output_tokens\":1}}}\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"temporary failure\"}}\n\n"
				ct = "text/event-stream"
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(data))}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			var result *OpenAIForwardResult
			var err error
			if stream {
				result, err = svc.handleNativeAnthropicStreamingResponse(c.Request.Context(), resp, c, account, "fixture", "fixture", "fixture", nil, time.Now())
			} else {
				result, err = svc.handleNativeAnthropicBufferedResponse(c.Request.Context(), resp, c, account, "fixture", "fixture", "fixture", nil, time.Now())
			}
			require.Error(t, err)
			var failure *UpstreamFailoverError
			require.NotErrorAs(t, err, &failure)
			require.NotNil(t, result)
			require.Equal(t, 9, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
			require.Contains(t, rec.Body.String(), "error")
		})
	}
}

func TestResponsesMessagesAcceptedReadErrorWritesErrorEvent(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%v", async), func(t *testing.T) {
			data := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r\",\"status\":\"in_progress\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"accepted\"}\n\ndata: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"input_tokens\":9,\"output_tokens\":1}}}\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(io.MultiReader(strings.NewReader(data), relayBoundaryReadFailure{}))}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			cfg := rawChatCompletionsTestConfig()
			if async {
				cfg.Gateway.StreamKeepaliveInterval = 1
			}
			result, err := (&OpenAIGatewayService{cfg: cfg}).handleAnthropicStreamingResponse(resp, c, preflightPoolAccount(PlatformOpenAI), "fixture", "fixture", "fixture", time.Now())
			require.Error(t, err)
			var failure *UpstreamFailoverError
			require.NotErrorAs(t, err, &failure)
			require.NotNil(t, result)
			require.Equal(t, 9, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
			require.Contains(t, rec.Body.String(), "event: error")
			require.NotContains(t, rec.Body.String(), "event: message_stop")
		})
	}
}

func TestNativeResponsesPriorProviderUsageKeepsCompletedTerminal(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			data := "data: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"billable_seconds\":2}}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"output\":[]}}\n\n"
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(data))}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), toolCorrector: NewCodexToolCorrector()}
			account := preflightPoolAccount(PlatformOpenAI)
			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "fixture", "fixture")
			} else {
				_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "fixture", "fixture")
			}
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), "response.completed")
			require.Contains(t, rec.Body.String(), "billable_seconds")
		})
	}
}

func TestResponsesFailedTerminalOutputPreventsCompatReplay(t *testing.T) {
	for _, route := range poolCompatBoundaryRoutes() {
		for _, fixture := range []struct {
			name, output string
			accepted     bool
		}{
			{"empty", `[]`, false},
			{"tool", `[{"type":"function_call","id":"call_1","name":"lookup","arguments":"{}"}]`, true},
			{"text", `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"accepted output"}]}]`, true},
		} {
			t.Run(route+"/"+fixture.name, func(t *testing.T) {
				data := `data: {"type":"response.failed","response":{"status":"failed","output":` + fixture.output + `,"error":{"code":503,"type":"server_error","message":"temporary upstream failure"}}}` + "\n\n"
				result, c, err := runPoolCompatStreamBoundary(t, route, data)
				require.Error(t, err)
				var failure *UpstreamFailoverError
				if !fixture.accepted {
					require.ErrorAs(t, err, &failure)
					require.False(t, c.Writer.Written())
					return
				}
				require.NotErrorAs(t, err, &failure)
				require.NotNil(t, result)
				require.Equal(t, OpenAIUsage{}, result.Usage)
				require.True(t, c.Writer.Written())
				require.GreaterOrEqual(t, c.Writer.Status(), http.StatusBadRequest, "accepted failure must emit an error, not successful conversion")
			})
		}
	}
}

func TestNativeAnthropicTerminalErrorUsageIsNotLost(t *testing.T) {
	for _, billable := range []bool{false, true} {
		t.Run(fmt.Sprintf("billable=%v", billable), func(t *testing.T) {
			usage := `{}`
			if billable {
				usage = `{"input_tokens":9,"output_tokens":1}`
			}
			data := "event: error\ndata: " + `{"type":"error","error":{"code":503,"type":"overloaded_error","message":"temporary failure"},"usage":` + usage + "}\n\n"
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))}}
			account := preflightPoolAccount(PlatformZhipu)
			account.Credentials["api_protocol"] = "anthropic"
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			result, err := svc.forwardAnthropicViaNativeAnthropicEndpoint(c.Request.Context(), c, account, []byte(`{"model":"glm-4.6","max_tokens":10,"messages":[{"role":"user","content":"hello"}],"stream":true}`), "")
			require.Error(t, err)
			var failure *UpstreamFailoverError
			if !billable {
				require.ErrorAs(t, err, &failure)
				require.False(t, c.Writer.Written())
				return
			}
			require.NotErrorAs(t, err, &failure)
			require.NotNil(t, result)
			require.Equal(t, 9, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
			require.Contains(t, rec.Body.String(), "event: error")
			require.NotContains(t, rec.Body.String(), "message_stop")
			require.Len(t, upstream.requests, 1)
		})
	}
}
