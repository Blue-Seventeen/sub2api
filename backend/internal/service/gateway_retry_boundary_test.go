//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type preflightOneShotReadError struct{ data []byte }

func (r *preflightOneShotReadError) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func TestGatewayPreflightPreservesConsumedReadError(t *testing.T) {
	for _, sse := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[sse], func(t *testing.T) {
			body := `{"data":[{"embedding":[0.1]}]}`
			if sse {
				body = `data: {"choices":[{"delta":{"content":"hello"}}]}`
			}
			resp := &http.Response{Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(&preflightOneShotReadError{data: []byte(body)})}
			if sse {
				_ = inspectUpstreamSSEPrefix(context.Background(), resp, upstreamSSEChat, 0)
			} else {
				_ = inspectUpstreamJSONError(resp, nil)
			}
			got, err := io.ReadAll(resp.Body)
			require.Equal(t, body, string(got))
			require.ErrorIs(t, err, io.ErrUnexpectedEOF, "preflight must not turn a partial read into a successful response")
		})
	}
}

func TestGatewayPreflightBillableAliasesAreReplayBoundaries(t *testing.T) {
	for _, usage := range []string{
		`{"cache_write_input_tokens":8}`, `{"cache_write_tokens":8}`, `{"cache_creation_tokens":8}`,
		`{"cache_creation":{"ephemeral_5m_input_tokens":8}}`, `{"input_tokens_details":{"cache_write_tokens":8}}`,
		`{"image_output_tokens":8}`, `{"duration":2}`, `{"characters":8}`,
	} {
		t.Run(usage, func(t *testing.T) {
			boundary, failure := classifyUpstreamSSEEvent([]byte(`data: {"error":{"code":503},"usage":`+usage+"}\n\n"), upstreamSSEChat)
			require.True(t, boundary, "observed billable usage must prevent replay")
			require.Nil(t, failure)
		})
	}
}

func TestGatewayPreflightLargeFirstSemanticEventIsNotRetryable(t *testing.T) {
	body := `data: {"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"` + strings.Repeat("x", upstreamSSEPreflightMaxBytes) + `"}}]}}]}` + "\n\n"
	resp := &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	failure := inspectUpstreamSSEPrefix(context.Background(), resp, upstreamSSEChat, 0)
	require.Nil(t, failure, "unknown acceptance at the prefix guard must fall back to the existing relay, never replay")
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(got), "falling back must preserve both consumed and unread bytes")
}

func TestGrokPoolConfigured404ReachesVoiceRetryOwner(t *testing.T) {
	account := preflightPoolAccount(PlatformGrok)
	account.Credentials["pool_mode_retry_status_codes"] = []any{float64(404)}
	c, rec := gin.CreateTestContext(httptest.NewRecorder())
	_ = rec
	result, err := (&OpenAIGatewayService{cfg: &config.Config{}}).handleGrokMediaErrorResponse(context.Background(), newOpenAIUpstreamErrorResponse(404, `{"error":{"message":"route unavailable"}}`), c, account, "fixture", "grok")
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.RetryableOnSameAccount)
	require.Nil(t, result)
	require.False(t, c.Writer.Written())
}

func TestGrokPoolRequestTerminalWinsOverAccountClassifier(t *testing.T) {
	account := preflightPoolAccount(PlatformGrok)
	for _, body := range []string{
		`{"error":{"code":"cyber_policy","message":"Request blocked by cyber policy"}}`,
		`{"error":{"code":"context_length_exceeded","message":"Maximum context length exceeded"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			require.False(t, svc.shouldFailoverOpenAIUpstreamResponse(account, 502, extractUpstreamErrorMessage([]byte(body)), []byte(body)))
		})
	}
}

func TestAlphaSearchCustomErrorExclusionWinsOverEndpointFallback(t *testing.T) {
	account := preflightPoolAccount(PlatformOpenAI)
	account.Credentials["custom_error_codes_enabled"] = true
	account.Credentials["custom_error_codes"] = []any{float64(503)}
	upstream := &httpUpstreamRecorder{resp: newOpenAIUpstreamErrorResponse(404, `{"error":{"message":"route unavailable"}}`)}
	c, _ := newOpenAIUpstreamErrorTestContext(t)
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	_, err := svc.ForwardAlphaSearch(context.Background(), c, account, []byte(`{"model":"gpt-5.5","input":"hello"}`))
	var failure *UpstreamFailoverError
	require.False(t, errors.As(err, &failure), "custom exclusion must not be bypassed by endpoint unsupported fallback")
}
