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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCCScanner_EmptyTerminalUsagePreservesObservedUsage(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	body := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"usage\":{},\"error\":{\"code\":\"server_error\"}}}\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	scan := svc.scanCCStream(nil, resp, "test", "", time.Now(), func(*apicompat.ChatCompletionsChunk) {})
	require.Equal(t, 7, scan.Usage.InputTokens)
	require.Equal(t, 2, scan.Usage.OutputTokens)
	require.NotEmpty(t, scan.ErrorPayload)
}

func TestOpenAIResponsesOnlyToolOutputDoesNotAutoRetryAsChat(t *testing.T) {
	for _, output := range []string{
		`[{"type":"input_file","file_id":"file_test"}]`,
		`[{"type":"input_image","file_id":"file_test"}]`,
		`[{"type":"input_text","text":"hello"},{"type":"input_file","file_data":"data:application/pdf;base64,AA=="}]`,
	} {
		t.Run(output, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4","input":[{"type":"function_call","call_id":"c1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":` + output + `}],"stream":true}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_native\"}}\n\n")),
			}}
			account := rawChatCompletionsTestAccount()
			account.Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
			_, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
		})
	}
}

func TestForwardResponses_NativeImageFailureDropsFailedAttemptUsage(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"draw","stream":true,"tools":[{"type":"image_generation","model":"gpt-image-2","size":"3840x2160"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"result\":\"final-image\",\"size\":\"3840x2160\"}}\n\n" +
			"data: {\"type\":\"response.failed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":3,\"output_tokens_details\":{\"image_tokens\":2}},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))}}
	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, rawChatCompletionsTestAccount(), body)
	require.Error(t, err)
	require.Nil(t, result, "failed Responses attempt usage is not billed")
	require.Len(t, upstream.requests, 1)
}

func TestForwardResponses_BillableNativeFailureDoesNotChatFallback(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n"))}}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesSupported: true,
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeForceResponses),
	}
	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 1)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.NotContains(t, rec.Body.String(), "response.completed")
}

func TestForwardResponses_TerminalFailureDropsAttemptUsageForOfficialRetry(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-ID": {"native-failed-1"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.failed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":2},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n",
		)),
	}}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesSupported: true,
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeForceResponses),
	}

	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).Forward(
		context.Background(), c, account, body,
	)

	require.Error(t, err)
	require.Nil(t, result, "failed-attempt usage is not part of the official final result")
	require.Len(t, upstream.requests, 1, "account retry is owned by the handler, not the service")
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "official retryable terminal failure must remain eligible for handler failover")
}

func TestForwardChatCompletions_BillableTerminalFailureDoesNotRemainReplayable(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-ID": {"chat-failed-1"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"output\":[],\"usage\":{\"input_tokens\":9,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n",
		)),
	}}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesSupported: true,
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeForceResponses),
	}

	result, err := (&OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}).ForwardAsChatCompletions(
		context.Background(), c, account, body, "", "",
	)

	require.Error(t, err)
	require.Nil(t, result, "buffered failed attempt usage is not billed")
	require.Len(t, upstream.requests, 1, "account retry is owned by the handler")
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
}
