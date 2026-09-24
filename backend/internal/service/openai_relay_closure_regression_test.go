//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const closurePrivateError = `invalid request from https://relay.private.example at 10.23.45.67 Bearer private-bearer-123456 sk-private-secret-123456`

func closureRelayContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return c, rec
}

func closureRelayResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func requireClosureErrorRedacted(t *testing.T, text string) {
	t.Helper()
	for _, private := range []string{"relay.private.example", "10.23.45.67", "private-bearer-123456", "sk-private-secret-123456"} {
		require.NotContains(t, text, private)
	}
}

func TestRelayClosureMessagesChatErrorRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, partial := range []bool{false, true} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("partial=%v/custom=%v", partial, custom), func(t *testing.T) {
				c, rec := closureRelayContext("/v1/messages")
				if custom {
					bindPassthroughRule(c, PlatformOpenAI, []string{"invalid request"}, http.StatusTeapot)
				}
				stream := ""
				if partial {
					stream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Visit public.example\"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\n"
				}
				stream += fmt.Sprintf("data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":%q}}\n\ndata: [DONE]\n\n", closurePrivateError)
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
				result, err := svc.streamChatCompletionsAsAnthropic(c, closureRelayResponse(stream), rawChatCompletionsTestAccount(), "gpt-5.5", "gpt-5.5", "gpt-5.5", nil, nil, time.Now())
				require.Error(t, err)
				require.NotNil(t, result)
				requireClosureErrorRedacted(t, rec.Body.String())
				requireClosureErrorRedacted(t, err.Error())
				wantType := "api_error"
				if custom {
					wantType = "upstream_error"
				}
				if partial {
					require.Equal(t, http.StatusOK, rec.Code)
					require.Contains(t, rec.Body.String(), "Visit public.example")
					require.Contains(t, rec.Body.String(), `"type":"`+wantType+`"`)
					require.Equal(t, 1, strings.Count(rec.Body.String(), "event: error\n"))
					require.Equal(t, 5, result.Usage.InputTokens)
				} else {
					wantStatus := http.StatusBadGateway
					if custom {
						wantStatus = http.StatusTeapot
					}
					require.Equal(t, wantStatus, rec.Code)
					require.Equal(t, wantType, gjson.Get(rec.Body.String(), "error.type").String())
				}
				require.NotContains(t, rec.Body.String(), "message_stop")
				require.NotContains(t, rec.Body.String(), "[DONE]")
			})
		}
	}
}

func TestRelayClosureResponsesChatStreamCustomErrorRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, upstreamType := range []string{"invalid_request_error", "rate_limit_error"} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/partial=%v", upstreamType, partial), func(t *testing.T) {
				c, rec := closureRelayContext("/v1/responses")
				c.Set("api_key", &APIKey{ID: 11, UserID: 1})
				account := forceChatResponsesFallbackAccount()
				bindPassthroughRule(c, account.Platform, []string{"invalid request"}, http.StatusTeapot)
				stream := ""
				if partial {
					stream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"unfinished reasoning\",\"content\":\"Visit public.example\"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\n"
				}
				stream += fmt.Sprintf("data: {\"type\":\"error\",\"error\":{\"type\":%q,\"message\":%q}}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"must-not-emit\"}}],\"usage\":{\"prompt_tokens\":999}}\n\ndata: [DONE]\n\n", upstreamType, closurePrivateError)
				cache := &reasoningRecordingCache{}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cache: cache, httpUpstream: &httpUpstreamRecorder{resp: closureRelayResponse(stream)}}
				result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":true,"input":"hello"}`))
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover, "custom rule must take precedence over upstream retry classification")
				require.NotNil(t, result)
				requireClosureErrorRedacted(t, rec.Body.String())
				requireClosureErrorRedacted(t, err.Error())
				if partial {
					require.Equal(t, http.StatusOK, rec.Code, "already committed stream status must not change")
					require.Contains(t, rec.Body.String(), "Visit public.example")
					require.Equal(t, 5, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.failed\n"))
					var failed gjson.Result
					for _, line := range strings.Split(rec.Body.String(), "\n") {
						if payload, ok := extractOpenAISSEDataLine(line); ok && gjson.Get(payload, "type").String() == "response.failed" {
							failed = gjson.Parse(payload)
						}
					}
					require.Equal(t, "failed", failed.Get("response.status").String())
					require.Equal(t, "upstream_error", failed.Get("response.error.type").String())
					require.Contains(t, failed.Get("response.error.message").String(), "invalid request")
				} else {
					require.Equal(t, http.StatusTeapot, rec.Code)
					require.Equal(t, "upstream_error", gjson.Get(rec.Body.String(), "error.type").String())
					require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "invalid request")
					require.Zero(t, result.Usage.InputTokens)
				}
				for _, forbidden := range []string{"response.completed", "response.output_text.done", "[DONE]", "must-not-emit"} {
					require.NotContains(t, rec.Body.String(), forbidden)
				}
				require.Empty(t, cache.snapshotSets(), "failed reasoning must not be cached")
			})
		}
	}
}

func TestRelayClosureRawChatErrorIsTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, shape := range []struct{ name, prefix, payload string }{
		{"typed", "", fmt.Sprintf(`{"type":"error","error":{"type":"invalid_request_error","message":%q}}`, closurePrivateError)},
		{"untyped", "", fmt.Sprintf(`{"error":{"type":"invalid_request_error","message":%q}}`, closurePrivateError)},
		{"failed", "", fmt.Sprintf(`{"type":"response.failed","response":{"error":{"type":"invalid_request_error","message":%q}}}`, closurePrivateError)},
		{"named", "event: error\n", fmt.Sprintf(`{"error":{"type":"invalid_request_error","message":%q}}`, closurePrivateError)},
	} {
		for _, partial := range []bool{false, true} {
			for _, custom := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/partial=%v/custom=%v", shape.name, partial, custom), func(t *testing.T) {
					c, rec := closureRelayContext("/v1/chat/completions")
					if custom {
						bindPassthroughRule(c, PlatformOpenAI, []string{"invalid request"}, http.StatusTeapot)
					}
					prefix := ""
					if partial {
						prefix = "data: {\"id\":\"chat_partial\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Visit public.example\"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\n"
					}
					stream := prefix + shape.prefix + "data: " + shape.payload + "\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"must-not-emit\"}}],\"usage\":{\"prompt_tokens\":999}}\n\ndata: [DONE]\n\n"
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
					result, err := svc.streamRawChatCompletions(c, closureRelayResponse(stream), rawChatCompletionsTestAccount(), "gpt-5.5", "gpt-5.5", "gpt-5.5", nil, nil, time.Now(), 0)
					require.Error(t, err, "terminal upstream errors must not enter success billing")
					requireClosureErrorRedacted(t, rec.Body.String())
					requireClosureErrorRedacted(t, err.Error())
					require.NotContains(t, rec.Body.String(), "must-not-emit")
					require.NotContains(t, rec.Body.String(), "[DONE]")
					wantType := "invalid_request_error"
					if custom {
						wantType = "upstream_error"
					}
					if partial {
						require.NotNil(t, result)
						require.Equal(t, 5, result.Usage.InputTokens)
						require.Equal(t, 2, result.Usage.OutputTokens)
						require.True(t, strings.HasPrefix(rec.Body.String(), prefix), "successful stream body must remain intact")
						require.Contains(t, rec.Body.String(), `"type":"`+wantType+`"`)
					} else {
						wantStatus := http.StatusBadRequest
						if custom {
							wantStatus = http.StatusTeapot
							var failover *UpstreamFailoverError
							require.NotErrorAs(t, err, &failover)
						}
						require.Equal(t, wantStatus, rec.Code)
						require.Equal(t, wantType, gjson.Get(rec.Body.String(), "error.type").String())
					}
				})
			}
		}
	}
}

func TestRelayClosureRawChatNullErrorPreservesSuccessfulBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := "event: chunk\ndata: {\"error\":null,\"choices\":[{\"delta\":{\"content\":\"Visit public.example\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"
	c, rec := closureRelayContext("/v1/chat/completions")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	result, err := svc.streamRawChatCompletions(c, closureRelayResponse(stream), rawChatCompletionsTestAccount(), "gpt-5.5", "gpt-5.5", "gpt-5.5", nil, nil, time.Now(), 0)
	require.NoError(t, err)
	require.Equal(t, stream, rec.Body.String())
	require.Equal(t, 3, result.Usage.InputTokens)
}

func TestRelayClosureNullFieldsDoNotHideEmptyCompletion(t *testing.T) {
	for _, field := range []string{"error", "usage"} {
		t.Run(field, func(t *testing.T) {
			body := fmt.Sprintf(`{"id":"resp_empty","status":"completed","output":[],%q:null}`, field)
			event := `{"type":"response.completed","response":` + body + `}`
			require.True(t, openAIResponsesCompletedEventIsEmpty([]byte(event), &OpenAIUsage{}))
			require.True(t, openAIResponsesFinalResponseIsEmpty([]byte(body), &OpenAIUsage{}))
		})
	}
}

func TestRelayClosureNullFieldsEmptyCompletionFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: closureRelayResponse("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_empty\",\"status\":\"completed\",\"output\":[],\"error\":null,\"usage\":null}}\n\n")}
			svc := newOpenAIImageGenerationControlTestService(upstream)
			c, rec := newOpenAIImageGenerationControlTestContext(stream, "codex_cli_rs/0.144.1")
			account := newOpenAIImageGenerationControlTestAccount()
			account.Extra = map[string]any{"openai_passthrough": true}
			_, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%v,"input":"hello"}`, stream)))
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Empty(t, rec.Body.String())
		})
	}
}

func TestRelayClosureNullErrorDoesNotHideSilentRefusal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, rec := closureRelayContext("/v1/chat/completions")
	stream := "data: {\"error\":null,\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	_, err := svc.streamRawChatCompletions(c, closureRelayResponse(stream), rawChatCompletionsTestAccount(), "gpt-5.5", "gpt-5.5", "gpt-5.5", nil, nil, time.Now(), openAISilentRefusalMinRequestBodyBytes)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.True(t, IsOpenAISilentRefusalErrorBody(failover.ResponseBody))
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestRelayClosureBufferedHTTP200Errors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"responses", "messages", "chat"} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/custom=%v", route, custom), func(t *testing.T) {
				c, rec := closureRelayContext("/v1/" + route)
				if custom {
					bindPassthroughRule(c, PlatformOpenAI, []string{"invalid request"}, http.StatusTeapot)
				}
				upstream := &httpUpstreamRecorder{resp: closureRelayResponse(fmt.Sprintf(`{"error":{"type":"invalid_request_error","message":%q}}`, closurePrivateError))}
				upstream.resp.Header.Set("Content-Type", "application/json")
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				var result *OpenAIForwardResult
				var err error
				switch route {
				case "responses":
					result, err = svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`))
				case "messages":
					result, err = svc.ForwardAsAnthropic(context.Background(), c, forceChatMessagesFallbackAccount(), []byte(`{"model":"gpt-5.5","stream":false,"max_tokens":8,"messages":[{"role":"user","content":"hello"}]}`), "", "")
				case "chat":
					result, err = svc.forwardAsRawChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), []byte(`{"model":"gpt-5.5","stream":false,"messages":[{"role":"user","content":"hello"}]}`), "")
				}
				require.Error(t, err)
				require.Nil(t, result, "HTTP 200 error envelopes must not become completed responses")
				wantStatus, wantType := http.StatusBadRequest, "invalid_request_error"
				if custom {
					wantStatus, wantType = http.StatusTeapot, "upstream_error"
				}
				require.Equal(t, wantStatus, rec.Code)
				require.Equal(t, wantType, gjson.Get(rec.Body.String(), "error.type").String())
				requireClosureErrorRedacted(t, rec.Body.String())
				requireClosureErrorRedacted(t, err.Error())
				require.NotContains(t, rec.Body.String(), `"status":"completed"`)
			})
		}
	}
}

func TestRelayClosureBufferedNullErrorRemainsSuccessful(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"id":"chat_ok","error":null,"choices":[{"index":0,"message":{"role":"assistant","content":"Visit public.example"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`
	c, rec := closureRelayContext("/v1/responses")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpUpstreamRecorder{resp: closureRelayResponse(body)}}
	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "Visit public.example")
	require.Equal(t, 3, result.Usage.InputTokens)
}
