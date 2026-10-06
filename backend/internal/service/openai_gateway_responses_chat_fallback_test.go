//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardResponses_ForceChatCompletionsRoutesNonStreamingToChatCompletions(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false,"service_tier":"priority"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_resp_chat_json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_json","object":"chat.completion","model":"gpt-5.4","service_tier":"default","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_tokens_details":{"cached_tokens":1}}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}
	SetActualOpenAIUpstreamEndpoint(c, "/v1/responses")

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "/v1/chat/completions", GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "ok", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.NotNil(t, result.ServiceTier)
	require.Equal(t, "priority", *result.ServiceTier)
	require.Equal(t, "default", result.UpstreamResponseServiceTier)
	require.False(t, result.Stream)
}

// Scenario: 第三方无推理模型不收到兼容档位。
func TestForwardResponses_ForceChatCompletionsOmitsNoneReasoningEffort(t *testing.T) {

	body := []byte(`{"model":"company-coding-model","input":"hello","reasoning":{"effort":"none"},"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_none","object":"chat.completion","model":"company-coding-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "company-coding-model", gjson.GetBytes(upstream.lastBody, "model").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "reasoning_effort").Exists())
	require.Nil(t, result.ReasoningEffort)
}

func TestForwardResponses_PassthroughFlagWithUnsupportedResponsesUsesAccountMapping(t *testing.T) {

	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		path := path
		t.Run(path, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4-channel","input":"hello","stream":false}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"id":"chatcmpl_mapping","object":"chat.completion","model":"gpt-5.4-account","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				)),
			}}
			svc := &OpenAIGatewayService{
				cfg:          rawChatCompletionsTestConfig(),
				httpUpstream: upstream,
			}
			account := rawChatCompletionsTestAccount()
			account.Credentials["model_mapping"] = map[string]any{
				"gpt-5.4-channel": "gpt-5.4-account",
			}
			account.Credentials["compact_model_mapping"] = map[string]any{
				"gpt-5.4-account": "gpt-5.4-compact",
			}
			account.Extra = map[string]any{
				"openai_passthrough":                     true,
				openai_compat.ExtraKeyResponsesSupported: false,
			}

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
			require.Equal(t, "gpt-5.4-account", gjson.GetBytes(upstream.lastBody, "model").String())
		})
	}
}

func TestForwardResponses_ForceChatCompletionsRoutesStreamingToChatCompletions(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"he"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_resp_chat_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
	require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
	require.Contains(t, rec.Body.String(), `"delta":"he"`)
	require.Contains(t, rec.Body.String(), "event: response.completed")
	require.Contains(t, rec.Body.String(), `"input_tokens":4`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.True(t, result.Stream)
	require.NotNil(t, result.FirstTokenMs)
}

func TestForwardResponses_ChatFallbackRejectsInvalidToolArgumentsAtOutputLimit(t *testing.T) {

	body := []byte(`{"model":"deepseek-v4-flash","input":"run the command","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_length_tool","object":"chat.completion.chunk","model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_length","type":"function","function":{"name":"exec_command","arguments":"{\"cmd\":\"ssh root@HOST"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_length_tool","object":"chat.completion.chunk","model":"deepseek-v4-flash","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":4,"completion_tokens":6492,"total_tokens":6496}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_length_tool"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.ErrorContains(t, err, "invalid JSON")
	require.NotNil(t, result)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 6492, result.Usage.OutputTokens)
	require.NotContains(t, rec.Body.String(), "response.function_call_arguments.done")
	require.NotContains(t, rec.Body.String(), "response.output_item.done")
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
}

func TestForwardResponses_DeepSeekReasoningOnlyStreamProducesVisibleText(t *testing.T) {

	body := []byte(`{"model":"deepseek-reasoner","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"role":"assistant","content":null,"reasoning_content":""},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"reasoning_content":"visible fallback"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_deepseek_reasoning_responses_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
	require.Contains(t, rec.Body.String(), `"delta":"visible fallback"`)
	require.Contains(t, rec.Body.String(), `"status":"incomplete"`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestForwardResponses_AutoSupportedAccountStillUsesResponsesEndpoint(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_resp_native"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_native","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}],"status":"completed"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
		openai_compat.ExtraKeyResponsesSupported: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
	require.Equal(t, "ok", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}

func TestForwardResponses_AutoSupportedAPIKeyFallsBackToChatBeforeSemanticOutput(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	nativeBody := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\",\"status\":\"in_progress\"}}\n\n"
	chatBody := strings.Join([]string{
		`data: {"id":"chatcmpl_fallback","object":"chat.completion.chunk","model":"gpt-6-astra","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_fallback","object":"chat.completion.chunk","model":"gpt-6-astra","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_fallback","object":"chat.completion.chunk","model":"gpt-6-astra","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl_fallback","object":"chat.completion.chunk","model":"gpt-6-astra","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(nativeBody)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_chat_fallback"}},
			Body:       io.NopCloser(strings.NewReader(chatBody)),
		},
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesSupported: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
	require.Equal(t, "/v1/chat/completions", upstream.requests[1].URL.Path)
	require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
	require.Contains(t, rec.Body.String(), `"delta":"hello"`)
	require.Contains(t, rec.Body.String(), "event: response.completed")
	require.Equal(t, 2, result.Usage.InputTokens)
}

func TestForwardResponses_ForceResponsesDoesNotFallbackToChatAfterNativeStreamFailure(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\"}}\n\n")),
	}}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeForceResponses),
		openai_compat.ExtraKeyResponsesSupported: true,
	}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
}

func TestForwardResponses_DoesNotFallbackAfterSemanticOutputWasCommitted(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	nativeBody := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(nativeBody)),
	}}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
	require.Contains(t, rec.Body.String(), `"delta":"hello"`)
}

func TestForwardResponses_GPT61SolToolsDoesNotDowngradeAfterNativeStreamFailure(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":"run the tool","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\"}}\n\n")),
	}}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
}

func TestForwardResponses_BillableNativeFailedEventDoesNotReplayThroughChat(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	nativeBody := "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary upstream failure\"}}}\n\n"
	chatBody := strings.Join([]string{
		`data: {"id":"chatcmpl_fallback","object":"chat.completion.chunk","model":"gpt-6-astra","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_fallback","object":"chat.completion.chunk","model":"gpt-6-astra","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl_fallback","object":"chat.completion.chunk","model":"gpt-6-astra","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(nativeBody))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(chatBody))},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
}

func TestForwardResponses_TransportErrorDoesNotFallbackToChat(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{err: errors.New("dial tcp: connection refused")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
}

func TestForwardResponses_StreamReadErrorDoesNotFallbackToChat(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%v", async), func(t *testing.T) {
			body := []byte(`{"model":"gpt-6-astra","input":"hello","stream":true}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			cfg := rawChatCompletionsTestConfig()
			if async {
				cfg.Gateway.StreamDataIntervalTimeout = 30
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: &openAIResponseFlushReadError{
					payload: []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\"}}\n\n"),
					err:     errors.New("connection reset by peer"),
				},
			}}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
			_, err := (&OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}).Forward(context.Background(), c, account, body)

			require.Error(t, err)
			require.Len(t, upstream.requests, 1, "an ambiguous body transport failure must not replay through Chat")
		})
	}
}

func TestForwardResponses_NativeFallbackPreservesModelMapping(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4-channel","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\"}}\n\n")),
		},
		{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("data: {\"id\":\"chatcmpl_fallback\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")),
		},
	}}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
	account.Credentials["model_mapping"] = map[string]any{
		"gpt-5.4-channel": "gpt-5.4",
		"gpt-5.4":         "gpt-4o",
	}
	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.bodies[0], "model").String())
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.bodies[1], "model").String())
	require.Equal(t, "gpt-5.4-channel", result.Model)
	require.Equal(t, "gpt-5.4", result.BillingModel)
	require.Equal(t, "gpt-5.4", result.UpstreamModel)
	require.Contains(t, rec.Body.String(), `"model":"gpt-5.4-channel"`)
}

func TestShouldFallbackOpenAIResponsesStreamToChatRejectsHeaderTimeoutAndAllowsSemanticTimeout(t *testing.T) {
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	headerTimeout := &UpstreamFailoverError{
		PreSemanticOutput:             false,
		ResponsesChatFallbackEligible: false,
	}
	require.False(t, shouldFallbackOpenAIResponsesStreamToChat(account, c, nil, "gpt-6-astra", headerTimeout))

	semanticTimeout := &UpstreamFailoverError{
		PreSemanticOutput:             true,
		ResponsesChatFallbackEligible: true,
	}
	require.True(t, shouldFallbackOpenAIResponsesStreamToChat(account, c, nil, "gpt-6-astra", semanticTimeout))
}

func TestForwardResponses_AutoFallbackPreservesResponsesOnlyCapabilities(t *testing.T) {
	for _, field := range []string{
		`"previous_response_id":"resp_previous"`,
		`"conversation":"conv_previous"`,
		`"conversation":{"id":"conv_previous"}`,
		`"background":true`,
		`"tools":[{"type":"web_search"}]`,
		`"tools":[{"type":"web_search_preview"}]`,
		`"tools":[{"type":"image_generation"}]`,
		`"tools":[{"type":"file_search","vector_store_ids":["vs_test"]}]`,
		`"tools":[{"type":"computer_use_preview"}]`,
		`"tools":[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"lark","definition":"start: WORD"}}]`,
		`"include":["reasoning.encrypted_content"]`,
		`"input":[{"type":"item_reference","id":"msg_previous"}]`,
		`"input":[{"type":"reasoning","encrypted_content":"opaque_previous_reasoning"}]`,
		`"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Summarize this PDF"},{"type":"input_file","file_id":"file_test"}]}]`,
		`"input":[{"role":"user","content":[{"type":"input_text","text":"Read this"},{"type":"unknown_part","payload":"opaque"}]}]`,
		`"input":[{"type":"message","role":"user","content":{"type":"input_file","file_data":"data:application/pdf;base64,AAAA"}}]`,
		`"input":[{"type":"message","role":"user","content":{"type":"unknown_part","text":"must not downgrade"}}]`,
		`"input":[{"type":"message","role":"user","content":[{"type":"input_image","file_id":"file_test"}]}]`,
		`"input":[{"type":"message","role":"assistant","content":[{"type":"input_image","image_url":"https://images.example/test.png"}]}]`,
	} {
		t.Run(field, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4","stream":true,"input":"hello",` + field + `}`)
			// Input-specific cases replace the normal text input instead of adding a duplicate JSON key.
			if strings.HasPrefix(field, `"input":`) {
				body = []byte(`{"model":"gpt-5.4","stream":true,` + field + `}`)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\"}}\n\n"))},
				{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"id\":\"chatcmpl_wrong\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"plain answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))},
			}}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}

			result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)

			require.Error(t, err, "a plain Chat answer cannot replace Responses-only capabilities")
			require.Nil(t, result)
			require.Len(t, upstream.requests, 1, "clean pre-semantic EOF must not drop tools or context")
			require.NotContains(t, rec.Body.String(), "plain answer")
		})
	}
}

func TestForwardResponses_AutoFallbackPreservesCompatibleMessageContent(t *testing.T) {
	for _, content := range []string{
		`"hello"`,
		`[{"type":"input_text","text":"hello"}]`,
		`{"type":"input_text","text":"hello"}`,
		`[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://images.example/test.png"}]`,
		`[{"type":"input_text","text":"hello"},{"type":"image_url","image_url":{"url":"https://images.example/test.png"}}]`,
	} {
		t.Run(content, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4","stream":true,"input":[{"role":"user","content":` + content + `}]}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\"}}\n\n"))},
				{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"id\":\"chatcmpl_fallback\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))},
			}}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}

			result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 2)
			if strings.Contains(content, "image_url") {
				require.Equal(t, "hello", gjson.GetBytes(upstream.bodies[1], "messages.0.content.0.text").String())
				require.Equal(t, "https://images.example/test.png", gjson.GetBytes(upstream.bodies[1], "messages.0.content.1.image_url.url").String())
			} else {
				require.Equal(t, "hello", gjson.GetBytes(upstream.bodies[1], "messages.0.content").String())
			}
		})
	}
}

func TestForwardResponses_ForceChatKeepsExistingMessageContentConversion(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","stream":false,"input":[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_file","file_id":"file_test"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_forced","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`)),
	}}

	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/v1/chat/completions", upstream.requests[0].URL.Path)
	require.Equal(t, "hello", gjson.GetBytes(upstream.bodies[0], "messages.0.content").String())
}

type responsesFallbackChatTransportError struct {
	httpUpstreamRecorder
}

func (u *responsesFallbackChatTransportError) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	if req.URL.Path == "/v1/chat/completions" {
		u.err = errors.New("connection reset by peer")
	}
	return u.httpUpstreamRecorder.Do(req, proxyURL, accountID, concurrency)
}

func TestForwardResponses_BillableNativeFailureDoesNotReplayThroughChat(t *testing.T) {
	for _, failure := range []string{"transport", "http_400", "http_502"} {
		t.Run(failure, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			nativeBody := "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary upstream failure\"}}}\n\n"
			recorder := httpUpstreamRecorder{responses: []*http.Response{{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"native-billed"}}, Body: io.NopCloser(strings.NewReader(nativeBody)),
			}}}
			var upstream HTTPUpstream = &recorder
			requests := &recorder.requests
			if failure == "transport" {
				transport := &responsesFallbackChatTransportError{httpUpstreamRecorder: recorder}
				upstream, requests = transport, &transport.requests
			} else {
				status := http.StatusBadRequest
				if failure == "http_502" {
					status = http.StatusBadGateway
				}
				recorder.responses = append(recorder.responses, &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"chat fallback failed"}}`))})
			}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}

			result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)

			require.Error(t, err)
			require.NotNil(t, result, "failed fallback must not discard the native attempt's billable usage")
			require.Equal(t, "native-billed", result.RequestID)
			require.Equal(t, "gpt-5.4", result.BillingModel)
			require.Equal(t, 5, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
			require.Len(t, *requests, 1)
			require.Equal(t, "/v1/responses", (*requests)[0].URL.Path)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr), "billable partial results must not trigger another replay")
		})
	}
}

func TestForwardResponses_NativeTerminalErrorPreservesUsageWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name           string
		requestBody    string
		prefix         string
		errorCode      string
		forceResponses bool
	}{
		{
			name: "after_visible_output", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true}`,
			prefix: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n", errorCode: "server_error",
		},
		{
			name: "force_responses", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true}`,
			errorCode: "invalid_request_error", forceResponses: true,
		},
		{
			name: "responses_only_tool", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true,"tools":[{"type":"web_search"}]}`,
			errorCode: "invalid_request_error",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.requestBody)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			nativeBody := tc.prefix + fmt.Sprintf("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":%q,\"message\":\"native terminal failure\"}}}\n\n", tc.errorCode)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"native-billed"}},
				Body: io.NopCloser(strings.NewReader(nativeBody)),
			}}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
			if tc.forceResponses {
				account.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceResponses)
			}

			result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)

			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr))
			require.NotNil(t, result, "a non-replayable native error must preserve confirmed billable usage")
			require.Equal(t, "native-billed", result.RequestID)
			require.Equal(t, "gpt-5.4", result.BillingModel)
			require.Equal(t, openAIResponsesUpstreamEndpoint, result.UpstreamEndpoint)
			require.Equal(t, 5, result.Usage.InputTokens)
			require.Equal(t, 1, result.Usage.OutputTokens)
			require.Len(t, upstream.requests, 1, "native terminal errors must not replay through Chat")
			require.Contains(t, rec.Body.String(), "response.failed")
			require.NotContains(t, rec.Body.String(), "response.completed")
		})
	}
}

func TestForwardResponses_NativeBillableFailurePreservesUsageWithoutReplay(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary upstream failure\"}}}\n\n")),
	}}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)}

	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.NotNil(t, result, "a non-replayable terminal error must preserve billable usage")
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Len(t, upstream.requests, 1)
}

func forceChatResponsesFallbackAccount() *Account {
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
	}
	return account
}

// reasoningRecordingCache 记录 reasoning 缓存写入、并按需响应回查。
type reasoningRecordingCache struct {
	stubGatewayCache
	mu      sync.Mutex
	sets    map[string]string
	getResp map[string]string
}

func (c *reasoningRecordingCache) SetReasoningContent(_ context.Context, itemID string, content string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sets == nil {
		c.sets = make(map[string]string)
	}
	c.sets[itemID] = content
	return nil
}

func (c *reasoningRecordingCache) GetReasoningContent(_ context.Context, itemID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.getResp[itemID]; ok {
		return v, nil
	}
	if v, ok := c.sets[itemID]; ok {
		return v, nil
	}
	return "", ErrReasoningContentNotFound
}

func (c *reasoningRecordingCache) SetScopedReasoningContent(ctx context.Context, userID, apiKeyID int64, itemID, content string, ttl time.Duration) error {
	return c.SetReasoningContent(ctx, fmt.Sprintf("%d:%d:%s", userID, apiKeyID, itemID), content, ttl)
}

func (c *reasoningRecordingCache) GetScopedReasoningContent(ctx context.Context, userID, apiKeyID int64, itemID string) (string, error) {
	return c.GetReasoningContent(ctx, fmt.Sprintf("%d:%d:%s", userID, apiKeyID, itemID))
}

func (c *reasoningRecordingCache) snapshotSets() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.sets))
	for k, v := range c.sets {
		out[k] = v
	}
	return out
}

// 流式响应里的 reasoning_content 应按 reasoning item id 写入缓存，供后续轮次
// 客户端不回传明文 summary 时回注（DeepSeek thinking mode 400 修复的写入侧）。
func TestForwardResponses_ChatFallbackCachesStreamedReasoning(t *testing.T) {

	body := []byte(`{"model":"deepseek-reasoner","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key", &APIKey{ID: 11, UserID: 1})

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_rc","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_rc","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"reasoning_content":"think "},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_rc","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"reasoning_content":"first"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_rc","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl_rc","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_reasoning_cache_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	cache := &reasoningRecordingCache{}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
		cache:        cache,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)

	sets := cache.snapshotSets()
	require.Len(t, sets, 1, "应恰好缓存一个 reasoning item")
	for itemID, content := range sets {
		require.True(t, strings.HasPrefix(itemID, "1:11:"))
		require.Equal(t, "think first", content)
	}
}

// 请求侧：encrypted-only reasoning item（无明文 summary）经缓存回查补回
// reasoning_content；带明文 summary 的 item 顺手回写缓存（自愈）。
func TestForwardResponses_ChatFallbackRestoresReasoningFromCache(t *testing.T) {

	body := []byte(`{
		"model":"deepseek-reasoner",
		"stream":false,
		"input":[
			{"type":"reasoning","id":"item_plain","summary":[{"type":"summary_text","text":"plain thinking"}]},
			{"type":"function_call","call_id":"call_0","name":"get_value","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_0","output":"ok"},
			{"type":"reasoning","id":"item_enc1","summary":[],"encrypted_content":"opaque"},
			{"type":"function_call","call_id":"call_1","name":"get_value","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"go on"}]}
		]
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key", &APIKey{ID: 11, UserID: 1})

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_reasoning_cache_restore"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_restore","object":"chat.completion","model":"deepseek-reasoner","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		)),
	}}
	cache := &reasoningRecordingCache{
		getResp: map[string]string{"1:11:item_enc1": "cached thinking"},
	}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
		cache:        cache,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)

	// 明文 summary 的 assistant 工具调用消息：reasoning_content 来自 summary 本身。
	require.Equal(t, "plain thinking", gjson.GetBytes(upstream.lastBody, "messages.0.reasoning_content").String())
	require.Equal(t, "call_0", gjson.GetBytes(upstream.lastBody, "messages.0.tool_calls.0.id").String())
	require.Equal(t, "tool", gjson.GetBytes(upstream.lastBody, "messages.1.role").String())
	// encrypted-only 的 assistant 工具调用消息：reasoning_content 来自缓存回查。
	require.Equal(t, "cached thinking", gjson.GetBytes(upstream.lastBody, "messages.2.reasoning_content").String())
	require.Equal(t, "call_1", gjson.GetBytes(upstream.lastBody, "messages.2.tool_calls.0.id").String())
	require.Equal(t, "tool", gjson.GetBytes(upstream.lastBody, "messages.3.role").String())

	// 明文 summary 的 item 被回写进缓存（自愈）。
	require.Equal(t, "plain thinking", cache.snapshotSets()["1:11:item_plain"])
}
