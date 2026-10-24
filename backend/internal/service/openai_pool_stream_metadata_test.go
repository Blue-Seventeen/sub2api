//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIPoolStreamMetadataDoesNotCommitBeforeRetryableError(t *testing.T) {
	for _, route := range poolCompatBoundaryRoutes() {
		t.Run(route, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			account := preflightPoolAccount(PlatformOpenAI)
			account.Credentials["pool_mode_retry_status_codes"] = []any{float64(503)}
			prefix := "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"
			if strings.HasPrefix(route, "responses-to-") {
				prefix = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_metadata\",\"status\":\"in_progress\"}}\n\n"
			}
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(prefix + "data: {\"type\":\"error\",\"error\":{\"code\":503,\"type\":\"server_error\",\"message\":\"temporary upstream failure\"}}\n\n"))}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			var err error
			switch route {
			case "responses-to-chat":
				_, err = svc.handleChatStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now(), 0)
			case "raw-chat":
				_, err = svc.streamRawChatCompletions(c, resp, account, "fixture", "fixture", "fixture", nil, nil, time.Now(), 0)
			case "chat-to-responses":
				_, err = svc.streamChatCompletionsAsResponses(c, resp, account, "fixture", nil, nil, false, nil, "fixture", "fixture", nil, nil, time.Now())
			case "chat-to-messages":
				_, err = svc.streamChatCompletionsAsAnthropic(c, resp, account, "fixture", "fixture", "fixture", nil, nil, time.Now())
			case "responses-to-messages":
				_, err = svc.handleAnthropicStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
			case "responses-to-messages-buffered":
				_, err = svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
			case "responses-to-chat-buffered":
				_, err = svc.handleChatBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
			}
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, 503, failure.StatusCode)
			require.True(t, failure.RetryableOnSameAccount)
			require.False(t, c.Writer.Written(), "role/start metadata must not expose an intermediate failed attempt")
		})
	}
}

func TestOpenAIPoolCCPolicyRejectionIsNotOverriddenByConfiguredStatus(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	account := preflightPoolAccount(PlatformOpenAI)
	account.Credentials["pool_mode_retry_status_codes"] = []any{float64(503)}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	err := svc.handleCCUpstreamErrorPayload(c, &http.Response{Header: http.Header{}}, account,
		[]byte(`{"error":{"code":503,"type":"content_policy_violation","message":"Request blocked by content policy"}}`),
		true, writeChatCompletionsError)
	var failure *UpstreamFailoverError
	require.NotErrorAs(t, err, &failure)
	require.True(t, c.Writer.Written())
}

func TestOpenAIPoolCompatBillableErrorDoesNotReplay(t *testing.T) {
	for _, route := range poolCompatBoundaryRoutes() {
		for _, fixture := range []struct {
			name, prefix, usage string
			wantTokens          bool
			numericCode         bool
		}{
			{name: "terminal-tokens", usage: `,"usage":{"input_tokens":9,"output_tokens":1}`, wantTokens: true},
			{name: "terminal-tokens-numeric-code", usage: `,"usage":{"input_tokens":9,"output_tokens":1}`, wantTokens: true, numericCode: true},
			{name: "terminal-provider-alias", usage: `,"usage":{"billable_seconds":2}`},
			{name: "prior-provider-alias", prefix: "data: {\"usage\":{\"billable_seconds\":2},\"choices\":[]}\n\n"},
		} {
			t.Run(route+"/"+fixture.name, func(t *testing.T) {
				code := `"503"`
				if fixture.numericCode {
					code = `503`
				}
				data := fixture.prefix + `data: {"type":"response.failed","response":{"status":"failed","error":{"code":` + code + `,"type":"server_error","message":"temporary upstream failure"}` + fixture.usage + "}}\n\n"
				result, c, err := runPoolCompatStreamBoundary(t, route, data)
				var failure *UpstreamFailoverError
				require.Error(t, err)
				require.NotErrorAs(t, err, &failure, "positive usage is an acceptance boundary, even without client output")
				require.True(t, c.Writer.Written(), "the accepted attempt must terminate rather than silently replay")
				require.NotNil(t, result)
				if fixture.wantTokens {
					require.Equal(t, 9, result.Usage.InputTokens)
					require.Equal(t, 1, result.Usage.OutputTokens)
				}
			})
		}
	}
}

func TestOpenAIPoolCompatEmptyStreamCanSwitchWithoutSameAccountRetry(t *testing.T) {
	for _, route := range poolCompatBoundaryRoutes() {
		for _, kind := range []string{"empty", "comments-only", "metadata-only", "neutral-500"} {
			name := route + "/" + kind
			data, wantStatus := "", 502
			switch kind {
			case "comments-only":
				data = ": keepalive\n\n"
			case "metadata-only":
				data = "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"
				if strings.HasPrefix(route, "responses-to-") {
					data = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_metadata\",\"status\":\"in_progress\"}}\n\n"
				}
			case "neutral-500":
				data = "data: {\"error\":{\"code\":500,\"type\":\"server_error\",\"message\":\"Unavailable\"}}\n\n"
				wantStatus = 500
			}
			t.Run(name, func(t *testing.T) {
				_, c, err := runPoolCompatStreamBoundary(t, route, data)
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, wantStatus, failure.StatusCode)
				require.False(t, failure.RetryableOnSameAccount, "only configured 503 may retry the same account")
				require.False(t, c.Writer.Written(), "a pre-output empty stream must leave account failover possible")
			})
		}
	}
}

func TestOpenAIPoolCompatPreflightReleasesAtFirstSemanticEvent(t *testing.T) {
	for _, protocol := range []upstreamSSEProtocol{upstreamSSEResponses, upstreamSSEChat} {
		t.Run(map[upstreamSSEProtocol]string{upstreamSSEResponses: "responses", upstreamSSEChat: "chat"}[protocol], func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			reader, writer := io.Pipe()
			t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}
			done := make(chan error, 1)
			go func() {
				done <- (&OpenAIGatewayService{}).preflightOpenAIPoolCompatStream(c, resp, preflightPoolAccount(PlatformOpenAI), protocol, writeChatCompletionsError)
			}()
			frame := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"
			if protocol == upstreamSSEResponses {
				frame = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
			}
			_, err := io.WriteString(writer, frame)
			require.NoError(t, err)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("preflight waited for terminal/EOF instead of releasing the first semantic event")
			}
			require.False(t, c.Writer.Written())
			prefix := make([]byte, len(frame))
			_, err = io.ReadFull(resp.Body, prefix)
			require.NoError(t, err)
			require.Equal(t, frame, string(prefix))
		})
	}
}

func TestOpenAIPoolCompatPreflightCancellationDoesNotRetry(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: reader}
	done := make(chan error, 1)
	go func() {
		done <- (&OpenAIGatewayService{}).preflightOpenAIPoolCompatStream(c, resp, preflightPoolAccount(PlatformOpenAI), upstreamSSEChat, writeChatCompletionsError)
	}()
	_, err := io.WriteString(writer, ": keepalive\n\n")
	require.NoError(t, err)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		var failure *UpstreamFailoverError
		require.NotErrorAs(t, err, &failure)
	case <-time.After(3 * time.Second):
		t.Fatal("preflight did not stop after request cancellation")
	}
	require.False(t, c.Writer.Written())
}

func runPoolCompatStreamBoundary(t *testing.T, route, data string) (*OpenAIForwardResult, *gin.Context, error) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	account := preflightPoolAccount(PlatformOpenAI)
	account.Credentials["pool_mode_retry_status_codes"] = []any{float64(503)}
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(data))}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	var result *OpenAIForwardResult
	var err error
	switch route {
	case "responses-to-chat":
		result, err = svc.handleChatStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now(), 0)
	case "raw-chat":
		result, err = svc.streamRawChatCompletions(c, resp, account, "fixture", "fixture", "fixture", nil, nil, time.Now(), 0)
	case "chat-to-responses":
		result, err = svc.streamChatCompletionsAsResponses(c, resp, account, "fixture", nil, nil, false, nil, "fixture", "fixture", nil, nil, time.Now())
	case "chat-to-messages":
		result, err = svc.streamChatCompletionsAsAnthropic(c, resp, account, "fixture", "fixture", "fixture", nil, nil, time.Now())
	case "responses-to-messages":
		result, err = svc.handleAnthropicStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
	case "responses-to-messages-buffered":
		result, err = svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
	case "responses-to-chat-buffered":
		result, err = svc.handleChatBufferedStreamingResponse(resp, c, account, "fixture", "fixture", "fixture", time.Now())
	default:
		t.Fatalf("unknown route %q", route)
	}
	return result, c, err
}

func poolCompatBoundaryRoutes() []string {
	return []string{"responses-to-chat", "raw-chat", "chat-to-responses", "chat-to-messages", "responses-to-messages", "responses-to-messages-buffered", "responses-to-chat-buffered"}
}
