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
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIUsageHasTokensIncludesAllBillableClasses(t *testing.T) {
	fields := []func(*OpenAIUsage){
		func(u *OpenAIUsage) { u.InputTokens = 1 },
		func(u *OpenAIUsage) { u.ImageInputTokens = 1 },
		func(u *OpenAIUsage) { u.ImageCacheReadTokens = 1 },
		func(u *OpenAIUsage) { u.OutputTokens = 1 },
		func(u *OpenAIUsage) { u.CacheCreationInputTokens = 1 },
		func(u *OpenAIUsage) { u.CacheCreation5mTokens = 1 },
		func(u *OpenAIUsage) { u.CacheCreation1hTokens = 1 },
		func(u *OpenAIUsage) { u.CacheReadInputTokens = 1 },
		func(u *OpenAIUsage) { u.ImageOutputTokens = 1 },
	}
	for _, set := range fields {
		usage := OpenAIUsage{}
		set(&usage)
		require.True(t, OpenAIUsageHasTokens(&usage))
	}
	require.False(t, OpenAIUsageHasTokens(&OpenAIUsage{}))
}

func buildResponsesFailedSSEStream(errType, errorMessage string) string {
	failed := fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp_err","object":"response","status":"failed","error":{"type":"%s","message":"%s"},"output":[],"usage":{"input_tokens":10,"output_tokens":0,"total_tokens":10}}}`, errType, errorMessage)
	return fmt.Sprintf("data: %s\n\n", failed)
}

func TestForwardAsAnthropic_BufferedResponseFailed_ReturnsError(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ssePayload := buildResponsesFailedSSEStream("invalid_request_error", "Content policy violation")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(ssePayload)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := rawChatCompletionsTestAccount()
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err, "non-cyber response.failed must return an error, not swallow as 200")
	require.Contains(t, err.Error(), "upstream response failed")
	require.Equal(t, http.StatusBadGateway, rec.Code, "should write 502 for non-failover failed response")
}

func TestForwardAsAnthropic_StreamingResponseFailed_ReturnsError(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ssePayload := buildResponsesFailedSSEStream("invalid_request_error", "Content policy violation")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(ssePayload)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := rawChatCompletionsTestAccount()
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err, "streaming response.failed must return an error")
	require.Contains(t, err.Error(), "upstream response failed")
}

func TestForwardAsAnthropic_TerminalFailureReplayDependsOnAcceptance(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, billable := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/billable=%v", stream, billable), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":%v}`, stream))
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
				payload := buildResponsesFailedSSEStream("server_error", "temporary upstream failure")
				if !billable {
					payload = strings.ReplaceAll(payload, `"input_tokens":10`, `"input_tokens":0`)
					payload = strings.ReplaceAll(payload, `"total_tokens":10`, `"total_tokens":0`)
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				result, err := svc.ForwardAsAnthropic(context.Background(), c, rawChatCompletionsTestAccount(), body, "", "")
				require.Error(t, err)
				var failoverErr *UpstreamFailoverError
				if billable {
					require.NotErrorAs(t, err, &failoverErr)
					require.NotNil(t, result)
					require.Equal(t, 10, result.Usage.InputTokens)
					require.True(t, c.Writer.Written())
					require.Contains(t, rec.Body.String(), "error")
					require.NotContains(t, rec.Body.String(), "message_stop")
				} else {
					require.ErrorAs(t, err, &failoverErr)
					require.False(t, c.Writer.Written())
				}
				require.Len(t, upstream.requests, 1)
			})
		}
	}
}

func TestForwardAsAnthropic_StreamingBareErrorAfterOutputIsVisible(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ssePayload := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_bare_error","object":"response","model":"gpt-5.4","status":"in_progress","output":[]}}`,
		"",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}`,
		"",
		`event: error`,
		`data: {"type":"error","error":{"type":"server_error","code":"upstream_error","message":"mixed tools failed"}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(ssePayload)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := rawChatCompletionsTestAccount()
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream response failed: mixed tools failed")
	clientStream := rec.Body.String()
	require.Contains(t, clientStream, `"text":"partial"`)
	require.Contains(t, clientStream, "event: error")
	require.Contains(t, clientStream, "mixed tools failed")
	require.NotContains(t, clientStream, "event: message_stop")
	require.NotContains(t, err.Error(), "missing terminal event")
}

func TestForwardAsAnthropic_StreamingBareErrorBeforeOutputFailsOver(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ssePayload := strings.Join([]string{
		`event: error`,
		`data: {"type":"error","error":{"type":"server_error","code":"upstream_error","message":"temporary upstream failure"}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(ssePayload)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := rawChatCompletionsTestAccount()
	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "pre-output retryable error must remain failover-safe: %T: %v", err, err)
	require.Empty(t, rec.Body.String(), "failover path must not commit downstream output")
}

func TestForwardAsAnthropic_StreamingGenericBareErrorBeforeOutputIsNotHiddenByFailover(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ssePayload := "event: error\n" +
		`data: {"type":"error","error":{"type":"server_error","code":"upstream_error","message":"mixed tools failed"}}` + "\n\n" +
		"data: [DONE]\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(ssePayload)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.ForwardAsAnthropic(context.Background(), c, rawChatCompletionsTestAccount(), body, "", "")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Contains(t, rec.Body.String(), "mixed tools failed")
}

func TestForwardAsAnthropic_BufferedResponseFailed_Failover(t *testing.T) {

	body := []byte(`{"model":"gpt-5.4","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	ssePayload := buildResponsesFailedSSEStream("rate_limit_error", "Rate limit reached")
	ssePayload = strings.ReplaceAll(ssePayload, `"input_tokens":10`, `"input_tokens":0`)
	ssePayload = strings.ReplaceAll(ssePayload, `"total_tokens":10`, `"total_tokens":0`)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(ssePayload)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	account := rawChatCompletionsTestAccount()
	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "buffered retryable failures stay eligible for official failover")
	require.Nil(t, result)
	require.Len(t, upstream.requests, 1)
}
