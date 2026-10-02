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
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardResponses_ChatFallbackRequiresTerminal(t *testing.T) {
	textChunk := "data: {\"id\":\"chat_1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n"
	usageChunk := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\n"
	finishChunk := "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	for _, forceChat := range []bool{false, true} {
		mode := "auto"
		if forceChat {
			mode = "force_chat"
		}
		for _, tc := range []struct {
			name string
			body string
			ok   bool
		}{
			{"text_eof", textChunk, false},
			{"usage_eof", textChunk + usageChunk, false},
			{"empty_eof", "", false},
			{"finish_reason_eof", textChunk + finishChunk + usageChunk, true},
			{"done", textChunk + usageChunk + "data: [DONE]\n\n", true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				upstream := &httpUpstreamRecorder{}
				account := rawChatCompletionsTestAccount()
				account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
				if forceChat {
					account = forceChatResponsesFallbackAccount()
				} else {
					upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))})
				}
				upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.body))})
				result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)
				if tc.ok {
					require.NoError(t, err)
					require.Contains(t, rec.Body.String(), "response.completed")
				} else {
					require.Error(t, err)
					require.NotContains(t, rec.Body.String(), "response.completed")
					require.NotContains(t, rec.Body.String(), "data: [DONE]")
					var failoverErr *UpstreamFailoverError
					require.False(t, errors.As(err, &failoverErr))
				}
				require.NotNil(t, result)
				if strings.Contains(tc.body, "prompt_tokens") {
					require.Equal(t, 7, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
				}
			})
		}
	}
}

func TestOpenAIResponsesAutoFallbackRejectsToolOutputMedia(t *testing.T) {
	for _, output := range []string{
		`[{"type":"input_file","file_id":"file_test"}]`,
		`[{"type":"input_image","file_id":"file_test"}]`,
		`[{"type":"input_text","text":"hello"},{"type":"input_file","file_data":"data:application/pdf;base64,AA=="}]`,
	} {
		t.Run(output, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4","input":[{"type":"function_call","call_id":"c1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":` + output + `}],"stream":true}`)
			require.False(t, openAIResponsesRequestCanAutoFallbackToChat(body))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))}}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
			_, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.Len(t, upstream.requests, 1, "Responses-only tool outputs must not be downgraded")
		})
	}
	require.True(t, openAIResponsesRequestCanAutoFallbackToChat([]byte(`{"model":"gpt-5.4","input":[{"type":"function_call_output","call_id":"c1","output":"plain tool output"}]}`)))
	require.True(t, openAIResponsesRequestCanAutoFallbackToChat([]byte(`{"model":"gpt-5.4","input":[{"type":"function_call_output","call_id":"c1","output":[{"type":"input_text","text":"plain tool output"}]}]}`)))
}

func TestForwardResponses_NativeImagePartialPreservesBillingMetadata(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"draw","stream":true,"tools":[{"type":"image_generation","model":"gpt-image-2","size":"3840x2160"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"result\":\"final-image\",\"size\":\"3840x2160\"}}\n\n" +
			"data: {\"type\":\"response.failed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":3,\"output_tokens_details\":{\"image_tokens\":2}},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))}}
	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, rawChatCompletionsTestAccount(), body)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "gpt-image-2", result.BillingModel)
	require.Equal(t, "3840x2160", result.ImageInputSize)
	require.NotEmpty(t, result.ImageSize)
	require.Equal(t, []string{"3840x2160"}, result.ImageOutputSizes)
	require.Equal(t, 2, result.Usage.ImageOutputTokens)
	require.Len(t, upstream.requests, 1)
}

func TestForwardResponses_FailedChatFallbackIncludesTerminalUsage(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{}
	for _, usage := range []string{`{"input_tokens":5,"output_tokens":1}`, `{"input_tokens":7,"output_tokens":2}`} {
		upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"usage\":" + usage + ",\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))})
	}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Len(t, upstream.requests, 2)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.NotContains(t, rec.Body.String(), "response.completed")
}

func TestChatFallback_BillableTerminalErrorWithoutNativeUsageDoesNotReplay(t *testing.T) {
	for _, mode := range []string{"auto", "force_chat", "messages_force_chat"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{}
			account := forceChatResponsesFallbackAccount()
			if mode == "auto" {
				account = rawChatCompletionsTestAccount()
				account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
				upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))})
			}
			upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":2},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))})
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			var result *OpenAIForwardResult
			var err error
			if mode == "messages_force_chat" {
				body = []byte(`{"model":"gpt-5.4","stream":true,"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
				result, err = svc.ForwardAsAnthropic(context.Background(), c, forceChatMessagesFallbackAccount(), body, "", "")
			} else {
				result, err = svc.Forward(context.Background(), c, account, body)
			}
			require.Error(t, err)
			require.NotNil(t, result)
			require.Equal(t, 7, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr), "confirmed usage must not be discarded by account replay")
			require.NotContains(t, rec.Body.String(), "response.completed")
		})
	}
}
