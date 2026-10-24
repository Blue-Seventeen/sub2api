//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responsesPartialErrorBillingUpstream struct {
	service.HTTPUpstream
	body     string
	accounts []int64
}

func TestOpenAIResponsesOrdinaryErrorPerRequestBillingRequiresAcceptance(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, outcome := range []string{"request-error", "ruled-error", "semantic-error", "billable-error", "success"} {
			t.Run(fmt.Sprintf("passthrough=%v/%s", passthrough, outcome), func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Default.RateMultiplier = 1
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				group := &service.Group{ID: 2402, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}
				key := &service.APIKey{ID: 3402, UserID: 4402, GroupID: &group.ID, Group: group, User: &service.User{ID: 4402, Status: service.StatusActive, Balance: 100}}
				account := service.Account{ID: 1403, Name: "per-request-source", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: 1, GroupIDs: []int64{group.ID}, Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"}, Extra: map[string]any{"openai_responses_mode": "force_responses", "openai_passthrough": passthrough}}
				usage, output := `{}`, `[]`
				status, event := "failed", "response.failed"
				failure := `,"error":{"code":503,"type":"server_error","message":"temporary ruled failure"}`
				switch outcome {
				case "request-error":
					failure = `,"error":{"code":400,"type":"invalid_request_error","message":"missing required parameter"}`
				case "semantic-error":
					output = `[{"type":"function_call","id":"call_1","call_id":"call_1","name":"lookup","arguments":"{}"}]`
				case "billable-error":
					usage = `{"input_tokens":9,"output_tokens":1}`
				case "success":
					status, event, failure = "completed", "response.completed", ""
					output = `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]`
				}
				upstream := &responsesPartialErrorBillingUpstream{body: fmt.Sprintf("data: {\"type\":%q,\"response\":{\"id\":\"resp_request_billing\",\"status\":%q,\"output\":%s,\"usage\":%s%s}}\n\n", event, status, output, usage, failure)}
				users := &grokMediaBillingUserRepo{user: key.User}
				billing, logs := &grokMediaBillingRepo{}, &grokMediaBillingLogRepo{}
				cache := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(cache.Stop)
				price := 0.25
				channel := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
					channels:       []service.Channel{{ID: 7702, Status: service.StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"gpt-5.4"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price}}}},
					groupPlatforms: map[int64]string{group.ID: service.PlatformOpenAI},
				}, nil, nil, nil, nil)
				billingService := service.NewBillingService(cfg, nil)
				resolver := service.NewModelPricingResolver(channel, billingService)
				gateway := service.NewOpenAIGatewayService(&openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}, logs, billing, users, nil, nil, nil, cfg, nil, nil, billingService, nil, cache, upstream, &service.DeferredService{}, nil, nil, resolver, channel, nil, nil, nil)
				h := NewOpenAIGatewayHandler(gateway, nil, service.NewConcurrencyService(nil), cache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, cfg)
				if outcome == "ruled-error" {
					code := http.StatusTeapot
					h.errorPassthroughService = service.NewErrorPassthroughService(&compatibleErrorPassthroughRepo{rules: []*model.ErrorPassthroughRule{{ID: 1, Enabled: true, MatchMode: model.MatchModeAny, Platforms: []string{service.PlatformOpenAI}, Keywords: []string{"ruled failure"}, ResponseCode: &code, PassthroughBody: true}}}, nil)
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hello","stream":true}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
				h.Responses(c)
				require.Equal(t, []int64{account.ID}, upstream.accounts)
				if outcome != "success" {
					require.Contains(t, rec.Body.String(), "error")
					require.NotContains(t, rec.Body.String(), "response.completed")
				}
				if outcome == "request-error" || outcome == "ruled-error" {
					require.Empty(t, logs.logs, "unaccepted ordinary errors must not create a per-request usage row")
					require.Empty(t, billing.commands, "unaccepted ordinary errors must not charge per-request pricing")
					return
				}
				require.Len(t, logs.logs, 1)
				require.Len(t, billing.commands, 1)
				require.Equal(t, string(service.BillingModePerRequest), *logs.logs[0].BillingMode)
				require.InDelta(t, price, billing.commands[0].BalanceCost, 1e-12)
			})
		}
	}
}

func (u *responsesPartialErrorBillingUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accounts = append(u.accounts, accountID)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"responses-partial-rid"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

func (u *responsesPartialErrorBillingUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func TestOpenAIResponsesAcceptedOuterErrorBillsExactlyOnce(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, billable := range []bool{false, true} {
				t.Run(fmt.Sprintf("passthrough=%v/stream=%v/billable=%v", passthrough, stream, billable), func(t *testing.T) {
					gin.SetMode(gin.TestMode)
					cfg := &config.Config{}
					cfg.Default.RateMultiplier = 1
					cfg.Security.URLAllowlist.AllowInsecureHTTP = true
					group := &service.Group{ID: 2401, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}
					key := &service.APIKey{ID: 3401, UserID: 4401, GroupID: &group.ID, Group: group, User: &service.User{ID: 4401, Status: service.StatusActive, Balance: 100}}
					accounts := []service.Account{{ID: 1401, Name: "accepted-source-A", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: 1, GroupIDs: []int64{group.ID}, Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"}, Extra: map[string]any{"openai_responses_mode": "force_responses", "openai_passthrough": passthrough}}}
					usage := `{"input_tokens":0,"output_tokens":0}`
					if billable {
						usage = `{"input_tokens":9,"output_tokens":1}`
						cfg.Gateway.MaxAccountSwitches = 1
						accounts[0].Credentials["pool_mode"] = true
						accounts[0].Credentials["pool_mode_retry_count"] = 10
						accounts[0].Credentials["pool_mode_retry_status_codes"] = []any{float64(503)}
						backup := accounts[0]
						backup.ID, backup.Name, backup.Priority = 1402, "unused-backup-B", 2
						accounts = append(accounts, backup)
					}
					upstream := &responsesPartialErrorBillingUpstream{body: `data: {"type":"response.failed","response":{"id":"resp_partial_error","status":"failed","usage":` + usage + `,"error":{"code":503,"type":"server_error","message":"temporary upstream failure"}}}` + "\n\n"}
					users := &grokMediaBillingUserRepo{user: key.User}
					billing := &grokMediaBillingRepo{}
					logs := &grokMediaBillingLogRepo{}
					cache := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
					t.Cleanup(cache.Stop)
					gateway := service.NewOpenAIGatewayService(&openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}, logs, billing, users, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
					h := NewOpenAIGatewayHandler(gateway, nil, service.NewConcurrencyService(nil), cache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, cfg)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-5.4","input":"hello","stream":%v}`, stream)))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
					h.Responses(c)
					require.Equal(t, []int64{1401}, upstream.accounts)
					require.Contains(t, rec.Body.String(), "error")
					require.NotContains(t, rec.Body.String(), "response.completed")
					if !billable {
						require.Empty(t, logs.logs)
						require.Empty(t, billing.commands)
						return
					}
					require.Len(t, logs.logs, 1, "accepted ordinary error from Forward is settled exactly once")
					require.Len(t, billing.commands, 1)
					require.Equal(t, int64(1401), logs.logs[0].AccountID)
					require.Equal(t, 9, logs.logs[0].InputTokens)
					require.Equal(t, 1, logs.logs[0].OutputTokens)
					require.NotNil(t, logs.logs[0].UpstreamEndpoint)
					require.Equal(t, "/v1/responses", *logs.logs[0].UpstreamEndpoint)
					require.Equal(t, logs.logs[0].RequestID, billing.commands[0].RequestID)
					require.Greater(t, billing.commands[0].BalanceCost, 0.0)
				})
			}
		}
	}
}
