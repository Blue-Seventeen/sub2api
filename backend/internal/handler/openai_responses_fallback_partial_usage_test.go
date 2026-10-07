//go:build unit

package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responsesPartialUsageUpstream struct {
	service.HTTPUpstream
	failure    string
	nativeBody string
	chatBody   string
	wantInput  int
	wantOutput int
	forceChat  bool
	paths      []string
}

func (u *responsesPartialUsageUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.paths = append(u.paths, req.URL.Path)
	if req.URL.Path == "/v1/chat/completions" {
		if u.chatBody != "" {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"fallback-billed"}}, Body: io.NopCloser(strings.NewReader(u.chatBody))}, nil
		}
		if u.failure == "transport" {
			return nil, errors.New("connection reset by peer")
		}
		status := http.StatusBadGateway
		if u.failure == "http_400" {
			status = http.StatusBadRequest
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"chat fallback failed"}}`))}, nil
	}
	body := "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary upstream failure\"}}}\n\n"
	if u.nativeBody != "" {
		body = u.nativeBody
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"native-billed"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestOpenAIResponses_OfficialRetryAndExplicitChatModeBilling(t *testing.T) {
	for _, forceChat := range []bool{false, true} {
		name := "official_responses_retry_does_not_bill_failed_attempts"
		paths := []string{"/v1/responses", "/v1/responses"}
		wantBilling := false
		if forceChat {
			name = "force_chat"
			paths = []string{"/v1/chat/completions"}
			// Explicit force-chat is a non-failover bridge result in the official
			// handler and its terminal result+error is recorded once.
			wantBilling = true
		}
		t.Run(name, func(t *testing.T) {
			assertResponsesFailureUsageBehavior(t, &responsesPartialUsageUpstream{
				forceChat: forceChat, wantInput: 7, wantOutput: 2,
				nativeBody: "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n",
				chatBody:   "data: {\"type\":\"response.failed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":2},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n",
			}, `{"model":"gpt-5.4","input":"hello","stream":true}`, false, paths, wantBilling)
		})
	}
}

func TestOpenAIResponses_OfficialRetriesDoNotBillFailedAttempts(t *testing.T) {
	for _, failure := range []string{"transport", "http_400", "http_502"} {
		t.Run(failure, func(t *testing.T) {
			assertResponsesFailureUsageBehavior(t, &responsesPartialUsageUpstream{failure: failure},
				`{"model":"gpt-5.4","input":"hello","stream":true}`, false, []string{"/v1/responses", "/v1/responses"}, false)
		})
	}
}

func TestOpenAIResponses_NativeTerminalErrorDoesNotRecordFailedAttemptUsage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		requestBody    string
		nativeBody     string
		forceResponses bool
		wantPaths      []string
	}{
		{
			name: "after_visible_output", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true}`,
			nativeBody: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary upstream failure\"}}}\n\n",
			wantPaths:  []string{"/v1/responses"},
		},
		{
			name: "force_responses", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true}`, forceResponses: true,
			nativeBody: "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"invalid_request_error\",\"message\":\"native terminal failure\"}}}\n\n",
			wantPaths:  []string{"/v1/responses"},
		},
		{
			name: "responses_only_tool", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true,"tools":[{"type":"web_search"}]}`,
			nativeBody: "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"invalid_request_error\",\"message\":\"native terminal failure\"}}}\n\n",
			wantPaths:  []string{"/v1/responses"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertResponsesFailureUsageBehavior(t, &responsesPartialUsageUpstream{nativeBody: tc.nativeBody}, tc.requestBody, tc.forceResponses, tc.wantPaths, false)
		})
	}
}

func assertResponsesFailureUsageBehavior(t *testing.T, upstream *responsesPartialUsageUpstream, body string, forceResponses bool, wantPaths []string, wantBilling bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxAccountSwitches = 1
	group := &service.Group{ID: 2001, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}
	key := &service.APIKey{
		ID: 3001, UserID: 4001, GroupID: &group.ID, Group: group,
		User: &service.User{ID: 4001, Status: service.StatusActive, Balance: 100},
	}
	accounts := []service.Account{
		{ID: 1001, Name: "native", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: 1, GroupIDs: []int64{group.ID}, Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"}, Extra: map[string]any{openai_compat.ExtraKeyResponsesSupported: true}},
		{ID: 1002, Name: "must-not-replay", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: 2, GroupIDs: []int64{group.ID}, Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"}, Extra: map[string]any{openai_compat.ExtraKeyResponsesSupported: true}},
	}
	if forceResponses {
		for i := range accounts {
			accounts[i].Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceResponses)
		}
	}
	if upstream.forceChat {
		for i := range accounts {
			accounts[i].Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceChatCompletions)
		}
	}
	users := &grokMediaBillingUserRepo{user: key.User}
	billing := &grokMediaBillingRepo{}
	logs := &grokMediaBillingLogRepo{}
	cache := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	gateway := service.NewOpenAIGatewayService(
		&openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}, logs, billing, users, nil, nil, nil,
		cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	h := NewOpenAIGatewayHandler(gateway, nil, service.NewConcurrencyService(nil), cache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, cfg)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})

	h.Responses(c)

	require.Equal(t, wantPaths, upstream.paths, "unexpected upstream attempts: status=%d body=%s", rec.Code, rec.Body.String())
	if !wantBilling {
		require.Empty(t, logs.logs, "failed retry attempts do not enter the billing chain")
		require.Empty(t, billing.commands, "failed retry attempts do not affect balance")
		return
	}
	require.Len(t, logs.logs, 1)
	requestID := "fallback-billed"
	require.Equal(t, requestID, logs.logs[0].RequestID)
	require.Equal(t, int64(1001), logs.logs[0].AccountID)
	require.Equal(t, upstream.wantInput, logs.logs[0].InputTokens)
	require.Equal(t, upstream.wantOutput, logs.logs[0].OutputTokens)
	require.Len(t, billing.commands, 1)
	require.Equal(t, requestID, billing.commands[0].RequestID)
	require.Greater(t, billing.commands[0].BalanceCost, 0.0)
	require.NotContains(t, rec.Body.String(), "response.completed")
}
