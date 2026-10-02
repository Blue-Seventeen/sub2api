//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

const ccPartialGeminiSSE = `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":2,"candidatesTokenCount":4,"thoughtsTokenCount":2}}` + "\n\n"

type ccPartialUsageUpstream struct {
	service.HTTPUpstream
	respond    func(int64) (*http.Response, error)
	accountIDs []int64
}

func (u *ccPartialUsageUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	return u.respond(accountID)
}

type ccPartialUsageProxyStatsRepo struct {
	service.ProxyStatsRepository
	mu       sync.Mutex
	failures int
}

func (r *ccPartialUsageProxyStatsRepo) Record(_ context.Context, stat *service.ProxyRequestStat) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !stat.Success {
		r.failures++
	}
	return nil
}

func (r *ccPartialUsageProxyStatsRepo) failureCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures
}

func newCCPartialUsageHandler(t *testing.T, upstream *ccPartialUsageUpstream, platforms ...string) (*GatewayHandler, *gin.Context, *httptest.ResponseRecorder, *grokMediaBillingRepo, *grokMediaBillingLogRepo, *ccPartialUsageProxyStatsRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	group := &service.Group{ID: 2001, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive, RateMultiplier: 1}
	if len(platforms) > 0 {
		group.Platform = platforms[0]
	}
	inputPrice, outputPrice, cacheReadPrice := 1.0, 2.0, 0.1
	group.ModelPricing = []service.ChannelModelPricing{{
		Models: []string{"gemini-2.5-flash"}, BillingMode: service.BillingModeToken,
		InputPrice: &inputPrice, OutputPrice: &outputPrice, CacheReadPrice: &cacheReadPrice,
	}}
	accounts := make([]*service.Account, 0, 2)
	for i := int64(0); i < 2; i++ {
		proxyID := int64(5001)
		accounts = append(accounts, &service.Account{
			ID: 1001 + i, Name: fmt.Sprintf("gemini-%d", i), Platform: service.PlatformGemini,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
			Concurrency: 1, Priority: int(i + 1), ProxyID: &proxyID,
			GroupIDs: []int64{group.ID}, Credentials: map[string]any{"api_key": "test-key"},
			AccountGroups: []service.AccountGroup{{AccountID: 1001 + i, GroupID: group.ID}},
		})
		if group.Platform == service.PlatformAntigravity {
			accounts[len(accounts)-1].Platform = service.PlatformAntigravity
			accounts[len(accounts)-1].Type = service.AccountTypeOAuth
			accounts[len(accounts)-1].Credentials = map[string]any{
				"access_token": "test-oauth-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
				"project_id": "test-project", "model_mapping": map[string]any{"gemini-2.5-flash": "gemini-2.5-flash"},
			}
		}
	}
	h, cleanup := newTestGatewayHandler(t, group, accounts)
	t.Cleanup(cleanup)
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	key := &service.APIKey{
		ID: 3001, UserID: 4001, GroupID: &group.ID, Group: group, Status: service.StatusActive,
		User: &service.User{ID: 4001, Status: service.StatusActive, Balance: 100, Concurrency: 1},
	}
	users := &grokMediaBillingUserRepo{user: key.User}
	billing := &grokMediaBillingRepo{}
	logs := &grokMediaBillingLogRepo{}
	stats := &ccPartialUsageProxyStatsRepo{}
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := repository.NewGatewayCache(rdb)
	billingCache := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
	billingService := service.NewBillingService(cfg, nil)
	h.gatewayService = service.NewGatewayService(
		nil, &fakeGroupRepo{group: group}, logs, billing, users, nil, nil, cache, cfg,
		snapshot, nil, billingService, nil, billingCache, nil, nil,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
		service.NewModelPricingResolver(nil, billingService), nil, nil, stats, nil,
	)
	h.billingCacheService = billingCache
	h.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
	h.antigravityGatewayService = service.NewAntigravityGatewayService(nil, nil, nil,
		service.NewAntigravityTokenProvider(nil, nil, nil), nil, upstream,
		service.NewSettingService(&settingHandlerPublicRepoStub{}, cfg), nil)
	h.proxyStatsWorkerPool = service.NewProxyStatsWorkerPoolWithOptions(service.ProxyStatsWorkerPoolOptions{
		WorkerCount: 1, QueueSize: 8, TaskTimeout: time.Second,
	})
	t.Cleanup(h.proxyStatsWorkerPool.Stop)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
	return h, c, rec, billing, logs, stats
}

func TestGatewayCompatHandlers_PartialStreamUsageRecordedOnce(t *testing.T) {
	for _, route := range []string{"messages_gemini", "responses_antigravity"} {
		for _, readErr := range []error{nil, errors.New("upstream read failed")} {
			name := "eof"
			if readErr != nil {
				name = "read_error"
			}
			t.Run(route+"/"+name, func(t *testing.T) {
				platform := service.PlatformGemini
				streamBody := ccPartialGeminiSSE
				if route == "responses_antigravity" {
					platform = service.PlatformAntigravity
					streamBody = "data: {\"response\":" + strings.TrimSpace(strings.TrimPrefix(ccPartialGeminiSSE, "data: ")) + "}\n\n"
				}
				upstream := &ccPartialUsageUpstream{respond: func(accountID int64) (*http.Response, error) {
					reader := io.Reader(strings.NewReader(streamBody))
					if readErr != nil {
						reader = io.MultiReader(reader, iotest.ErrReader(readErr))
					}
					return ccPartialUsageResponse(accountID, http.StatusOK, reader), nil
				}}
				h, c, rec, billing, logs, stats := newCCPartialUsageHandler(t, upstream, platform)
				path := "/v1/messages"
				body := `{"model":"gemini-2.5-flash","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`
				if route == "responses_antigravity" {
					path = "/v1/responses"
					body = `{"model":"gemini-2.5-flash","stream":true,"input":"hello"}`
				}
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(c.Request.Context())
				c.Request.Header.Set("Content-Type", "application/json")
				if route == "messages_gemini" {
					h.Messages(c)
				} else {
					h.Responses(c)
				}
				require.Equal(t, []int64{1001}, upstream.accountIDs, "partial output must not replay: %s", rec.Body.String())
				require.Contains(t, rec.Body.String(), "partial")
				require.NotContains(t, rec.Body.String(), "response.completed")
				require.NotContains(t, rec.Body.String(), "message_stop")
				require.Len(t, logs.logs, 1)
				require.Equal(t, 8, logs.logs[0].InputTokens)
				require.Equal(t, 6, logs.logs[0].OutputTokens)
				require.Equal(t, 2, logs.logs[0].CacheReadTokens)
				require.Len(t, billing.commands, 1)
				require.Greater(t, billing.commands[0].BalanceCost, 0.0)
				require.Eventually(t, func() bool { return stats.failureCount() == 1 }, time.Second, time.Millisecond)
			})
		}
	}
}

func TestGatewayGeminiNative_AntigravityPartialWritesErrorAndBillsOnce(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			upstream := &ccPartialUsageUpstream{respond: func(accountID int64) (*http.Response, error) {
				body := "data: {\"response\":" + strings.TrimSpace(strings.TrimPrefix(ccPartialGeminiSSE, "data: ")) + "}\n\n"
				return ccPartialUsageResponse(accountID, http.StatusOK, strings.NewReader(body)), nil
			}}
			h, c, rec, billing, logs, _ := newCCPartialUsageHandler(t, upstream, service.PlatformAntigravity)
			action := "generateContent"
			if stream {
				action = "streamGenerateContent"
			}
			path := "/antigravity/v1beta/models/gemini-2.5-flash:" + action
			ctx := context.WithValue(c.Request.Context(), ctxkey.ForcePlatform, service.PlatformAntigravity)
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)).WithContext(ctx)
			c.Params = gin.Params{{Key: "modelAction", Value: "gemini-2.5-flash:" + action}}
			c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)
			h.GeminiV1BetaModels(c)
			require.Equal(t, []int64{1001}, upstream.accountIDs, "%s", rec.Body.String())
			require.Contains(t, rec.Body.String(), `"error"`)
			if !stream {
				require.Equal(t, http.StatusBadGateway, rec.Code)
			} else {
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, 1, strings.Count(rec.Body.String(), `"error"`))
			}
			require.Len(t, logs.logs, 1)
			require.Equal(t, 8, logs.logs[0].InputTokens)
			require.Equal(t, 6, logs.logs[0].OutputTokens)
			require.Len(t, billing.commands, 1)
			require.Greater(t, billing.commands[0].BalanceCost, 0.0)
		})
	}
}

func ccPartialUsageResponse(accountID int64, status int, body io.Reader) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Goog-Request-Id": {fmt.Sprintf("cc-stream-%d", accountID)}},
		Body:       io.NopCloser(body),
	}
}

func TestGatewayChatCompletions_PartialStreamUsageRecordedOnce(t *testing.T) {
	for _, readErr := range []error{nil, errors.New("upstream read failed")} {
		name := "eof"
		if readErr != nil {
			name = "read_error"
		}
		t.Run(name, func(t *testing.T) {
			upstream := &ccPartialUsageUpstream{respond: func(accountID int64) (*http.Response, error) {
				reader := io.Reader(strings.NewReader(ccPartialGeminiSSE))
				if readErr != nil {
					reader = io.MultiReader(reader, iotest.ErrReader(readErr))
				}
				return ccPartialUsageResponse(accountID, http.StatusOK, reader), nil
			}}
			h, c, rec, billing, logs, stats := newCCPartialUsageHandler(t, upstream)

			h.ChatCompletions(c)

			require.Equal(t, []int64{1001}, upstream.accountIDs, "partial output must not be retried")
			require.Contains(t, rec.Body.String(), "partial")
			require.NotContains(t, rec.Body.String(), "data: [DONE]")
			require.Len(t, logs.logs, 1, "observed partial usage must reach the usage repository")
			log := logs.logs[0]
			require.Equal(t, int64(1001), log.AccountID)
			require.Equal(t, "cc-stream-1001", log.RequestID)
			require.Equal(t, 8, log.InputTokens)
			require.Equal(t, 6, log.OutputTokens)
			require.Equal(t, 2, log.CacheReadTokens)
			require.Len(t, billing.commands, 1)
			require.Greater(t, billing.commands[0].BalanceCost, 0.0)
			require.Eventually(t, func() bool { return stats.failureCount() == 1 }, time.Second, time.Millisecond)
		})
	}
}

func TestGatewayChatCompletions_NilErrorResultDoesNotRecordUsage(t *testing.T) {
	upstream := &ccPartialUsageUpstream{respond: func(accountID int64) (*http.Response, error) {
		return ccPartialUsageResponse(accountID, http.StatusBadRequest, strings.NewReader(`{"error":{"message":"invalid request"}}`)), nil
	}}
	h, c, _, billing, logs, stats := newCCPartialUsageHandler(t, upstream)

	h.ChatCompletions(c)

	require.Equal(t, []int64{1001}, upstream.accountIDs)
	require.Empty(t, logs.logs)
	require.Empty(t, billing.commands)
	require.Eventually(t, func() bool { return stats.failureCount() == 1 }, time.Second, time.Millisecond)
}

func TestGatewayChatCompletions_FailoverRecordsOnlySuccessfulUsage(t *testing.T) {
	upstream := &ccPartialUsageUpstream{respond: func(accountID int64) (*http.Response, error) {
		if accountID == 1001 {
			return nil, errors.New("connection reset by peer")
		}
		body := ccPartialGeminiSSE + `data: {"candidates":[{"finishReason":"STOP"}]}` + "\n\n"
		return ccPartialUsageResponse(accountID, http.StatusOK, strings.NewReader(body)), nil
	}}
	h, c, rec, billing, logs, _ := newCCPartialUsageHandler(t, upstream)

	h.ChatCompletions(c)

	require.Equal(t, []int64{1001, 1002}, upstream.accountIDs)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
	require.Len(t, logs.logs, 1)
	require.Equal(t, int64(1002), logs.logs[0].AccountID)
	require.Equal(t, "cc-stream-1002", logs.logs[0].RequestID)
	require.Len(t, billing.commands, 1)
	require.Equal(t, "cc-stream-1002", billing.commands[0].RequestID)
}
