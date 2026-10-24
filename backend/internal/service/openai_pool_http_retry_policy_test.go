//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIPoolHTTPRetryClassificationPreservesRequestAndCustomPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		excludeCode bool
		wantRetry   bool
	}{
		{"configured 404", http.StatusNotFound, `{"error":{"type":"invalid_request_error","message":"upstream route unavailable"}}`, false, true},
		{"custom policy excludes configured 404", http.StatusNotFound, `{"error":{"message":"upstream route unavailable"}}`, true, false},
		{"schema 400", http.StatusBadRequest, openAIInvalidFunctionParametersBody, false, false},
		{"input 400", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"Invalid value for messages: expected an array"}}`, false, false},
		{"invalid json 400", http.StatusBadRequest, `{"error":{"code":"invalid_json","message":"Cannot parse the request body as JSON"}}`, false, false},
		{"context 400", http.StatusBadRequest, `{"error":{"code":"context_length_exceeded","message":"Maximum context length exceeded"}}`, false, false},
		{"processing 400", http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"An error occurred while processing your request"}}`, false, true},
		{"cyber wrapped 502", http.StatusBadGateway, `{"error":{"code":"cyber_policy","message":"Request blocked by cyber policy"}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{
					"pool_mode": true, "pool_mode_retry_count": 1,
					"pool_mode_retry_status_codes": []any{float64(tc.status)},
				},
			}
			if tc.excludeCode {
				account.Credentials["custom_error_codes_enabled"] = true
				account.Credentials["custom_error_codes"] = []any{float64(http.StatusServiceUnavailable)}
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: &modelNotFoundManagedAccountRepo{}}
			body := []byte(tc.body)
			message := extractUpstreamErrorMessage(body)

			require.Equal(t, tc.wantRetry, svc.shouldFailoverOpenAIUpstreamResponse(account, tc.status, message, body), "native HTTP gate")
			require.Equal(t, tc.wantRetry, shouldFailoverOpenAIPassthroughResponse(account, tc.status, body), "passthrough HTTP gate")
			c, recorder := newOpenAIUpstreamErrorTestContext(t)
			failoverErr := svc.failoverOpenAIUpstreamHTTPError(context.Background(), c, account,
				newOpenAIUpstreamErrorResponse(tc.status, tc.body), body, message, "gpt-4o-mini")
			if tc.wantRetry {
				require.NotNil(t, failoverErr)
				require.True(t, failoverErr.RetryableOnSameAccount)
			} else {
				require.Nil(t, failoverErr)
			}
			require.Empty(t, recorder.Body.String(), "classification must leave the response unwritten")
		})
	}
}

func TestOpenAIGrokQuota400PreservesAccountFailoverDespiteRequestErrorEnvelope(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"free usage", `{"error":{"type":"invalid_request_error","code":"subscription:free-usage-exhausted","message":"You've used all the included free usage for model grok-4.5"}}`},
		{"billing quota", `{"error":{"type":"invalid_request_error","message":"Your team has run out of credits"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				ID: 42, Platform: PlatformGrok, Type: AccountTypeAPIKey,
				Credentials: map[string]any{
					"pool_mode": true, "pool_mode_retry_count": 1,
					"pool_mode_retry_status_codes": []any{float64(http.StatusBadRequest)},
				},
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			body := []byte(tc.body)
			decision := classifyGrokUpstreamFailure(http.StatusBadRequest, body, "grok-4.5")
			require.True(t, decision.ShouldFailover, "fixture must represent an existing typed account quota failure")
			c, recorder := newOpenAIUpstreamErrorTestContext(t)
			failoverErr := svc.failoverOpenAIUpstreamHTTPError(context.Background(), c, account,
				newOpenAIUpstreamErrorResponse(http.StatusBadRequest, tc.body), body, extractUpstreamErrorMessage(body), "grok-4.5")

			require.NotNil(t, failoverErr, "an account quota rejection must not become a client schema failure")
			require.True(t, failoverErr.RetryableOnSameAccount)
			require.Empty(t, recorder.Body.String())

			// The same generic envelope still terminates a genuine Grok schema error.
			schemaBody := []byte(`{"error":{"type":"invalid_request_error","message":"Invalid value for messages: expected an array"}}`)
			c, recorder = newOpenAIUpstreamErrorTestContext(t)
			require.Nil(t, svc.failoverOpenAIUpstreamHTTPError(context.Background(), c, account,
				newOpenAIUpstreamErrorResponse(http.StatusBadRequest, string(schemaBody)), schemaBody, extractUpstreamErrorMessage(schemaBody), "grok-4.5"))
			require.Empty(t, recorder.Body.String())
		})
	}
}
