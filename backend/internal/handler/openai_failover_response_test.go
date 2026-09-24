//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIFailoverCustomRulePrecedesCapacityAndModelFallback(t *testing.T) {
	status := http.StatusConflict
	message := "Configured maintenance at https://private.example token=private-token"
	rule := &model.ErrorPassthroughRule{
		ID: 1, Enabled: true, Priority: 1, ErrorCodes: []int{http.StatusBadRequest},
		MatchMode: model.MatchModeAny, Platforms: []string{service.PlatformOpenAI},
		ResponseCode: &status, CustomMessage: &message, SkipMonitoring: true,
	}
	h := &OpenAIGatewayHandler{errorPassthroughService: service.NewErrorPassthroughService(
		&compatibleErrorPassthroughRepo{rules: []*model.ErrorPassthroughRule{rule}}, nil,
	)}
	for _, body := range []string{
		`{"error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`,
		`{"error":{"code":"model_not_found","message":"Model not found"}}`,
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		h.handleFailoverExhausted(c, &service.UpstreamFailoverError{
			StatusCode: http.StatusBadRequest, RequestScopedTransient: true,
			ResponseBody: []byte(body), ClientStatusCode: http.StatusServiceUnavailable,
			ClientMessage: "upstream fallback must not replace configured message",
		}, false)
		require.Equal(t, http.StatusConflict, recorder.Code)
		require.Contains(t, recorder.Body.String(), "Configured maintenance")
		require.NotContains(t, recorder.Body.String(), "private.example")
		require.NotContains(t, recorder.Body.String(), "private-token")
		require.NotContains(t, recorder.Body.String(), "upstream fallback")
		require.True(t, c.GetBool(service.OpsSkipPassthroughKey))
	}
}

func TestManagedModelNotFoundStreamEmitsOnlySanitizedFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := c.Writer.Write([]byte(": keepalive\n\n"))
	require.NoError(t, err)
	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadRequest,
		ResponseBody: []byte(`{"error":{"code":"model_not_found","message":"Model not found at https://private.example/debug?access_token=super-secret-value","token":"private-token"}}`),
	}, true)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed"))
	require.NotContains(t, recorder.Body.String(), "response.completed")
	require.NotContains(t, recorder.Body.String(), "private.example")
	require.NotContains(t, recorder.Body.String(), "super-secret-value")
	require.NotContains(t, recorder.Body.String(), "private-token")
	streamErr, ok := service.GetOpsStreamError(c)
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, streamErr.IntendedStatus)
	require.True(t, streamErr.CountTowardsSLA)
}

func TestResponsesFailoverTerminalFailureCountsOnce(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	h := &GatewayHandler{}
	h.handleResponsesFailoverExhausted(c, &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, true)
	h.handleResponsesFailoverExhausted(c, &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}, true)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed"))
	errors := service.GetOpsStreamErrors(c)
	require.Len(t, errors, 1)
	require.Equal(t, http.StatusTooManyRequests, errors[0].IntendedStatus)
	require.True(t, errors[0].CountTowardsSLA)
}

func TestOpenAIFailoverAfterOrdinaryKeepaliveEmitsResponseFailed(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	_, err := c.Writer.Write([]byte(":\n\n"))
	require.NoError(t, err)
	c.Writer.Flush()
	c.Set("openai_stream_keepalive_bytes", 3)

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: []byte(`{"error":{"type":"upstream_error","message":"upstream failed"}}`),
	}, false)

	body := recorder.Body.String()
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, strings.Count(body, "event: response.failed\n"))
	require.NotContains(t, body, ":\n\n{\"error\":")
	require.NotContains(t, body, "response.completed")
}
