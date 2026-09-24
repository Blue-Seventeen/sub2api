//go:build unit

package service

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIStreamErrorCodeTopLevelRateLimit(t *testing.T) {
	for name, payload := range map[string]string{
		"top_level":       `{"type":"error","code":"rate_limit_exceeded","message":"Upstream rate limit exceeded, please retry later"}`,
		"nested_error":    `{"type":"error","error":{"code":"rate_limit_exceeded","message":"Upstream rate limit exceeded, please retry later"}}`,
		"nested_response": `{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","message":"Upstream rate limit exceeded, please retry later"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := []byte(payload)
			message := extractOpenAISSEErrorMessage(body)
			require.Equal(t, "rate_limit_exceeded", openAIStreamFailedEventErrorCode(body))
			require.Equal(t, http.StatusTooManyRequests, openAIStreamFailedEventSemanticStatus(body, message))
			require.Equal(t, http.StatusTooManyRequests, openAIStreamFailureStatus(body, message))
			require.True(t, openAIStreamErrorEventShouldFailover(body, message))
		})
	}
}

func TestOpenAIStreamErrorCodeRetainsNestedPrecedence(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"code":" RATE_LIMIT_EXCEEDED "}`, "rate_limit_exceeded"},
		{`{"code":"rate_limit_exceeded","error":{"code":"invalid_request"}}`, "invalid_request"},
		{`{"code":"rate_limit_exceeded","error":{"code":"invalid_request"},"response":{"error":{"code":"context_length_exceeded"}}}`, "context_length_exceeded"},
		{`{"code":null}`, ""},
	} {
		require.Equal(t, tc.want, openAIStreamFailedEventErrorCode([]byte(tc.body)))
	}
}

func TestOpenAIStreamTopLevelRateLimitCustomRuleStillWins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, rec := closureRelayContext("/v1/chat/completions")
	bindPassthroughRule(c, PlatformOpenAI, []string{"rate limit"}, http.StatusTeapot)
	payload := []byte(fmt.Sprintf(`{"type":"error","code":"rate_limit_exceeded","message":%q}`, "rate limit: "+closurePrivateError))
	svc := &OpenAIGatewayService{}
	err := svc.handleCCUpstreamErrorPayload(c, closureRelayResponse(""), rawChatCompletionsTestAccount(), payload, true, writeChatCompletionsError)
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.NotErrorAs(t, err, &failover)
	require.Equal(t, http.StatusTeapot, rec.Code)
	require.Contains(t, rec.Body.String(), `"type":"upstream_error"`)
	requireClosureErrorRedacted(t, rec.Body.String())
	requireClosureErrorRedacted(t, err.Error())
}
