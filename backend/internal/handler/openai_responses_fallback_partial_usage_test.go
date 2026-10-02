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

func TestOpenAIResponses_ChatFallbackPartialUsageRecordedOnce(t *testing.T) {
	for _, body := range []string{
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\n",
		"data: {\"type\":\"response.failed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":2},\"error\":{\"code\":\"server_error\",\"message\":\"temporary failure\"}}}\n\n",
	} {
		t.Run(body, func(t *testing.T) {
			assertResponsesPartialUsageRecordedOnce(t, &responsesPartialUsageUpstream{chatBody: body, wantInput: 12, wantOutput: 3},
				`{"model":"gpt-5.4","input":"hello","stream":true}`, false, []string{"/v1/responses", "/v1/chat/completions"})
		})
	}
}

func TestOpenAIResponses_FailedChatFallbackRecordsNativeUsageOnce(t *testing.T) {
	for _, failure := range []string{"transport", "http_400", "http_502"} {
		t.Run(failure, func(t *testing.T) {
			assertResponsesPartialUsageRecordedOnce(t, &responsesPartialUsageUpstream{failure: failure},
				`{"model":"gpt-5.4","input":"hello","stream":true}`, false, []string{"/v1/responses", "/v1/chat/completions"})
		})
	}
}

func TestOpenAIResponses_NativeTerminalErrorRecordsUsageOnce(t *testing.T) {
	for _, tc := range []struct {
		name           string
		requestBody    string
		nativeBody     string
		forceResponses bool
	}{
		{
			name: "after_visible_output", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true}`,
			nativeBody: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"server_error\",\"message\":\"temporary upstream failure\"}}}\n\n",
		},
		{
			name: "force_responses", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true}`, forceResponses: true,
			nativeBody: "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"invalid_request_error\",\"message\":\"native terminal failure\"}}}\n\n",
		},
		{
			name: "responses_only_tool", requestBody: `{"model":"gpt-5.4","input":"hello","stream":true,"tools":[{"type":"web_search"}]}`,
			nativeBody: "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_native\",\"status\":\"failed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1},\"error\":{\"code\":\"invalid_request_error\",\"message\":\"native terminal failure\"}}}\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertResponsesPartialUsageRecordedOnce(t, &responsesPartialUsageUpstream{nativeBody: tc.nativeBody}, tc.requestBody, tc.forceResponses, []string{"/v1/responses"})
		})
	}
}

func assertResponsesPartialUsageRecordedOnce(t *testing.T, upstream *responsesPartialUsageUpstream, body string, forceResponses bool, wantPaths []string) {
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

	require.Equal(t, wantPaths, upstream.paths, "partial usage must not trigger account replay: status=%d body=%s", rec.Code, rec.Body.String())
	require.Len(t, logs.logs, 1, "native partial usage must survive both the fallback and handler error paths")
	requestID, input, output := "native-billed", 5, 1
	if upstream.chatBody != "" {
		requestID, input, output = "fallback-billed", upstream.wantInput, upstream.wantOutput
	}
	require.Equal(t, requestID, logs.logs[0].RequestID)
	require.Equal(t, int64(1001), logs.logs[0].AccountID)
	require.Equal(t, input, logs.logs[0].InputTokens)
	require.Equal(t, output, logs.logs[0].OutputTokens)
	require.Len(t, billing.commands, 1)
	require.Equal(t, requestID, billing.commands[0].RequestID)
	require.Greater(t, billing.commands[0].BalanceCost, 0.0)
	require.NotContains(t, rec.Body.String(), "response.completed")
}
