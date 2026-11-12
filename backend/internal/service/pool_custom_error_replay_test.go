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
	"github.com/stretchr/testify/require"
)

func poolCustomExcludedAccount(platform string, pool bool) *Account {
	account := preflightPoolAccount(platform)
	account.Credentials["pool_mode"] = pool
	account.Credentials["custom_error_codes_enabled"] = true
	account.Credentials["custom_error_codes"] = []any{float64(http.StatusUnauthorized)}
	return account
}

func TestPoolCustomErrorReplayNativeHTTPKeepsNonPoolTerminalContract(t *testing.T) {
	for _, pool := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-pool", true: "pool"}[pool], func(t *testing.T) {
			account := poolCustomExcludedAccount(PlatformOpenAI, pool)
			repo := &errorPolicyRepoStub{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
			body := []byte(`{"error":{"message":"upstream temporarily unavailable"}}`)
			message := extractUpstreamErrorMessage(body)
			require.Equal(t, pool, svc.shouldFailoverOpenAIUpstreamResponse(account, 503, message, body))
			require.Equal(t, pool, shouldFailoverOpenAIPassthroughResponse(account, 503, body))
			c, rec := newOpenAIUpstreamErrorTestContext(t)
			failure := svc.failoverOpenAIUpstreamHTTPError(context.Background(), c, account, newOpenAIUpstreamErrorResponse(503, string(body)), body, message, "fixture-model")
			if pool {
				require.NotNil(t, failure)
				require.True(t, failure.RetryableOnSameAccount)
			} else {
				require.Nil(t, failure)
				_, err := svc.handleErrorResponse(context.Background(), newOpenAIUpstreamErrorResponse(503, string(body)), c, account, nil)
				require.Error(t, err)
				require.Equal(t, 500, rec.Code)
				require.Contains(t, rec.Body.String(), "Upstream gateway error")
			}
			require.Zero(t, repo.setErrCalls)
		})
	}
}

func TestPoolCustomErrorReplayDoesNotBroadenSameAccountBudget(t *testing.T) {
	account := poolCustomExcludedAccount(PlatformZhipu, true)
	account.Credentials["pool_mode_retry_status_codes"] = []any{float64(502)}
	failure := poolModeHTTPFailover(context.Background(), nil, account, 503, nil, []byte(`{"error":{"message":"busy"}}`), "fixture-model")
	require.NotNil(t, failure, "safe account failover remains available outside the same-account status list")
	require.False(t, failure.RetryableOnSameAccount)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Nil(t, poolModeHTTPFailover(ctx, nil, account, 503, nil, nil, "fixture-model"))
}

func TestPoolCustomErrorReplayStillStopsRequestRejections(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		status int
		body   string
	}{
		{"schema", 400, `{"error":{"type":"invalid_request_error","message":"Invalid value for messages"}}`},
		{"context", 400, `{"error":{"code":"context_length_exceeded","message":"Maximum context length exceeded"}}`},
		{"cyber", 503, `{"error":{"code":"cyber_policy","message":"blocked"}}`},
		{"content", 503, `{"error":{"code":"content_policy_violation","message":"blocked"}}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			account := poolCustomExcludedAccount(PlatformOpenAI, true)
			account.Credentials["pool_mode_retry_status_codes"] = []any{float64(fixture.status)}
			body := []byte(fixture.body)
			require.Nil(t, poolModeHTTPFailover(context.Background(), nil, account, fixture.status, nil, body, "fixture-model"))
			require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(account, fixture.status, extractUpstreamErrorMessage(body), body))
			require.False(t, shouldFailoverOpenAIPassthroughResponse(account, fixture.status, body))
		})
	}
}

func TestPoolCustomErrorReplayCCEnvelopeHonorsOutputBoundary(t *testing.T) {
	for _, fixture := range []struct {
		name, extra string
		allow, want bool
	}{
		{"pre-output", "", true, true},
		{"prior-output", "", false, false},
		{"billable", `,"usage":{"input_tokens":1}`, true, false},
		{"tool-output", `,"output":[{"type":"function_call","name":"lookup"}]`, true, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			account := poolCustomExcludedAccount(PlatformOpenAI, true)
			payload := []byte(`{"type":"response.failed","response":{"error":{"code":503,"message":"busy"}` + fixture.extra + `}}`)
			c, _ := newOpenAIUpstreamErrorTestContext(t)
			err := (&OpenAIGatewayService{cfg: &config.Config{}}).handleCCUpstreamErrorPayload(c, &http.Response{Header: http.Header{}}, account, payload, fixture.allow, writeChatCompletionsError)
			var failure *UpstreamFailoverError
			require.Equal(t, fixture.want, errors.As(err, &failure))
			require.Equal(t, !fixture.want, c.Writer.Written())
		})
	}
}

func TestPoolCustomErrorReplayAlphaSearchEndpointFallback(t *testing.T) {
	account := poolCustomExcludedAccount(PlatformOpenAI, true)
	upstream := &httpUpstreamRecorder{resp: newOpenAIUpstreamErrorResponse(404, `{"error":{"message":"route unavailable"}}`)}
	c, _ := newOpenAIUpstreamErrorTestContext(t)
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	_, err := svc.ForwardAlphaSearch(context.Background(), c, account, []byte(`{"model":"gpt-5.5","input":"hello"}`))
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.False(t, c.Writer.Written())
}

func TestPoolCustomErrorReplayCompactRecoveryKeepsExplicitBoundary(t *testing.T) {
	payload := []byte(`{"type":"response.failed","response":{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}}`)
	for _, pool := range []bool{false, true} {
		account := poolCustomExcludedAccount(PlatformOpenAI, pool)
		c := newOpenAICompactFallbackTestContext(t, "/v1/responses/compact")
		signal := newOpenAICompactFallbackSignalForAccount(c, account, payload, "context window exceeded")
		require.Equal(t, pool, signal != nil, "only explicit compact recovery, not ordinary account replay, may recover context failure")
		MarkResponseCommitted(c)
		require.Nil(t, newOpenAICompactFallbackSignalForAccount(c, account, payload, "context window exceeded"))
	}
}

func TestPoolCustomErrorReplayDoesNotMutateExcludedAccountHealth(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		status int
		body   string
	}{
		{"access-state", 401, `{"error":{"code":"account_deactivated","message":"account unavailable"}}`},
		{"transient-model-cooldown", 503, `{"error":{"message":"busy"}}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			account := poolCustomExcludedAccount(PlatformOpenAI, true)
			account.Credentials["custom_error_codes"] = []any{float64(403)}
			account.Credentials["pool_mode_retry_status_codes"] = []any{float64(502)}
			repo := &errorPolicyRepoStub{}
			svc := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
			for attempt := 0; attempt < 3; attempt++ {
				require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, fixture.status, nil, []byte(fixture.body), "fixture-model"))
			}
			require.Zero(t, repo.setErrCalls)
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "fixture-model"))
		})
	}
}

func TestPoolCustomErrorReplayWSHTTPBridgeKeepsTurnBoundary(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		for _, fixture := range []struct {
			name         string
			status, turn int
			body         string
			want         bool
		}{
			{"http-first-turn", 503, 1, `{"error":{"message":"temporary upstream failure"}}`, true},
			{"http-later-turn", 503, 2, `{"error":{"message":"temporary upstream failure"}}`, false},
			{"sse-first-turn", 200, 1, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"limited\"}}}\n\n", true},
		} {
			t.Run(platform+"/"+fixture.name, func(t *testing.T) {
				account := poolCustomExcludedAccount(platform, true)
				account.Concurrency = 1
				resp := &http.Response{StatusCode: fixture.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fixture.body))}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &httpUpstreamRecorder{resp: resp}}
				c, _ := newOpenAIUpstreamErrorTestContext(t)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
				payload := []byte(`{"type":"response.create","model":"fixture-model","input":"hi"}`)
				writes := 0
				_, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "fixture-token", payload, len(payload), "fixture-model", "", "", "", "", fixture.turn, func([]byte) error { writes++; return nil })
				var failure *UpstreamFailoverError
				require.Equal(t, fixture.want, errors.As(err, &failure))
				if fixture.want {
					require.Zero(t, writes)
				}
			})
		}
	}
}

func TestPoolCustomErrorReplayGrokMediaExplicitRejection(t *testing.T) {
	account := poolCustomExcludedAccount(PlatformGrok, true)
	c, _ := newOpenAIUpstreamErrorTestContext(t)
	_, err := (&OpenAIGatewayService{cfg: &config.Config{}}).handleGrokMediaErrorResponse(context.Background(),
		newOpenAIUpstreamErrorResponse(503, `{"error":{"message":"temporary outage"}}`), c, account, "fixture-id", "grok-imagine")
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.True(t, failure.RetryableOnSameAccount)
	require.False(t, c.Writer.Written())
}

func TestPoolCustomErrorReplayGrokExclusionDoesNotApplyForbiddenStateRule(t *testing.T) {
	account := poolCustomExcludedAccount(PlatformGrok, true)
	account.Credentials["temp_unschedulable_enabled"] = true
	account.Credentials["temp_unschedulable_rules"] = []any{map[string]any{
		"error_code": float64(403), "keywords": []any{"entitlement unavailable"}, "duration_minutes": float64(1),
	}}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
	c, _ := newOpenAIUpstreamErrorTestContext(t)
	body := []byte(`{"error":{"message":"entitlement unavailable"}}`)
	failure := svc.failoverOpenAIUpstreamHTTPError(context.Background(), c, account,
		newOpenAIUpstreamErrorResponse(403, string(body)), body, "entitlement unavailable", "grok-4.5")
	require.NotNil(t, failure, "account availability still allows safe pool failover")
	require.Zero(t, repo.tempUnschedCalls, "custom-missed statuses must not newly activate an account state rule")
	require.Zero(t, repo.rateLimitedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.False(t, c.Writer.Written())
}

func TestPoolCustomErrorReplayImagesRetainsDedicatedReplayPolicy(t *testing.T) {
	for _, pool := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-pool", true: "pool"}[pool], func(t *testing.T) {
			account := poolCustomExcludedAccount(PlatformOpenAI, pool)
			c, rec := newOpenAIUpstreamErrorTestContext(t)
			_, err := (&OpenAIGatewayService{cfg: &config.Config{}}).handleOpenAIImagesErrorResponse(context.Background(),
				newOpenAIUpstreamErrorResponse(503, `{"error":{"message":"temporary outage"}}`), c, account, "gpt-image-1")
			var failure *UpstreamFailoverError
			require.NotErrorAs(t, err, &failure, "custom-code splitting must not invent replay of specialized image work")
			if pool {
				require.Equal(t, 503, rec.Code)
			} else {
				require.Equal(t, 500, rec.Code)
			}
		})
	}
}
