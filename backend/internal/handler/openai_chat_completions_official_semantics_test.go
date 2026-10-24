//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIChatTerminalFailureUpstream struct {
	service.HTTPUpstream
	body  string
	paths []string
}

func (u *openAIChatTerminalFailureUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.paths = append(u.paths, req.URL.Path)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"chat-terminal-rid"}},
		Body:       io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func (u *openAIChatTerminalFailureUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func TestOpenAIChatCompletions_NonFailoverTerminalErrorBillsReturnedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxAccountSwitches = 1
	group := &service.Group{ID: 2301, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}
	key := &service.APIKey{
		ID: 3301, UserID: 4301, GroupID: &group.ID, Group: group,
		User: &service.User{ID: 4301, Status: service.StatusActive, Balance: 100},
	}
	accounts := []service.Account{{
		ID: 1301, Name: "openai-responses", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		GroupIDs:    []int64{group.ID},
		Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
	}}
	upstream := &openAIChatTerminalFailureUpstream{body: "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request_error\",\"message\":\"request rejected\"},\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}\n\n"}
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
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})

	h.ChatCompletions(c)

	require.Equal(t, []string{"/v1/responses"}, upstream.paths)
	require.Len(t, logs.logs, 1, "non-failover result+error is the final result delivered to billing")
	require.Equal(t, int64(1301), logs.logs[0].AccountID)
	require.NotNil(t, logs.logs[0].UpstreamEndpoint)
	require.Equal(t, "/v1/responses", *logs.logs[0].UpstreamEndpoint)
	require.Equal(t, 7, logs.logs[0].InputTokens)
	require.Equal(t, 2, logs.logs[0].OutputTokens)
	require.Len(t, billing.commands, 1)
	require.Equal(t, logs.logs[0].RequestID, billing.commands[0].RequestID)
	require.Greater(t, billing.commands[0].BalanceCost, 0.0)
}

func TestOpenAIChatCompletions_TerminalErrorHonorsBillableBoundary(t *testing.T) {
	for _, billable := range []bool{false, true} {
		name := "unaccepted-failover-does-not-bill"
		if billable {
			name = "accepted-usage-bills-once-without-replay"
		}
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.MaxAccountSwitches = 1
			group := &service.Group{ID: 2302, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}
			key := &service.APIKey{
				ID: 3302, UserID: 4302, GroupID: &group.ID, Group: group,
				User: &service.User{ID: 4302, Status: service.StatusActive, Balance: 100},
			}
			accounts := []service.Account{{
				ID: 1302, Name: "openai-responses", Platform: service.PlatformOpenAI,
				Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
				GroupIDs:    []int64{group.ID},
				Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"},
				Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
			}}
			upstream := &openAIChatTerminalFailureUpstream{body: "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp-failed\",\"status\":\"failed\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2},\"error\":{\"code\":\"server_error\",\"message\":\"temporary upstream failure\"}}}\n\n"}
			if billable {
				accounts[0].Credentials["pool_mode"] = true
				accounts[0].Credentials["pool_mode_retry_count"] = 10
				accounts[0].Credentials["pool_mode_retry_status_codes"] = []any{float64(502)}
				accounts[0].Priority = 1
				backup := accounts[0]
				backup.ID, backup.Name, backup.Priority = 1304, "backup-responses", 2
				accounts = append(accounts, backup)
			} else {
				// A failed attempt can only be replayed before upstream acceptance.
				upstream.body = strings.ReplaceAll(upstream.body, `"input_tokens":7,"output_tokens":2`, `"input_tokens":0,"output_tokens":0`)
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
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})

			h.ChatCompletions(c)

			require.Equal(t, []string{"/v1/responses"}, upstream.paths)
			if billable {
				require.Len(t, logs.logs, 1, "an accepted terminal attempt is settled, not replayed")
				require.Equal(t, 7, logs.logs[0].InputTokens)
				require.Equal(t, 2, logs.logs[0].OutputTokens)
				require.Len(t, billing.commands, 1)
				require.Equal(t, logs.logs[0].RequestID, billing.commands[0].RequestID)
				require.Greater(t, billing.commands[0].BalanceCost, 0.0)
				return
			}
			require.Empty(t, logs.logs, "usage from a failover attempt must not enter billing")
			require.Empty(t, billing.commands)
		})
	}
}

func TestOpenAIChatCompletions_NewAPIStyleUsageRecordsActualUpstreamEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	group := &service.Group{
		ID: 2303, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive,
		RateMultiplier: 1, NewAPIStyleInterfaceEnabled: true,
	}
	key := &service.APIKey{
		ID: 3303, UserID: 4303, GroupID: &group.ID, Group: group,
		User: &service.User{ID: 4303, Status: service.StatusActive, Balance: 100},
	}
	accounts := []service.Account{{
		ID: 1303, Name: "openai-newapi-chat", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		GroupIDs:    []int64{group.ID},
		Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
	}}
	upstream := &openAIChatTerminalFailureUpstream{body: "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\ndata: [DONE]\n\n"}
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
	newAPIStyle := service.NewNewAPIStyleGatewayService(nil, upstream, cfg, nil, nil)
	require.True(t, newAPIStyle.SupportsForGatewayHandler(&accounts[0], group, service.NewAPIStyleRouteChatCompletions))
	h := NewOpenAIGatewayHandler(gateway, newAPIStyle, service.NewConcurrencyService(nil), cache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, cfg)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})

	h.ChatCompletions(c)

	require.Equal(t, []string{"/v1/chat/completions"}, upstream.paths, "status=%d body=%s", rec.Code, rec.Body.String())
	require.Len(t, logs.logs, 1)
	require.NotNil(t, logs.logs[0].UpstreamEndpoint)
	require.Equal(t, "/v1/chat/completions", *logs.logs[0].UpstreamEndpoint)
}
