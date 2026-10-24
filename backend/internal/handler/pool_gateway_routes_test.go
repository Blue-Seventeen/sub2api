//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type poolGatewayRouteUpstream struct {
	service.HTTPUpstream
	mu        sync.Mutex
	hits      []int64
	response  string
	recorder  *httptest.ResponseRecorder
	earlyBody bool
}

func (u *poolGatewayRouteUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.hits = append(u.hits, accountID)
	if u.recorder.Body.Len() != 0 {
		u.earlyBody = true
	}
	status, body := http.StatusServiceUnavailable, `{"error":{"type":"server_error","message":"fixture unavailable"}}`
	contentType := "application/json"
	if accountID == 806 {
		status, body = http.StatusOK, u.response
		if strings.HasPrefix(body, "event:") || strings.Contains(req.URL.Path, ":streamGenerateContent") {
			contentType = "text/event-stream"
			if strings.Contains(req.URL.Path, ":streamGenerateContent") {
				body = "data: " + body + "\n\n"
			}
		}
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": {contentType}, "X-Request-Id": {"pool-route-fixture"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (u *poolGatewayRouteUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

type poolGatewayUsageLogRepo struct {
	service.UsageLogRepository
	mu   sync.Mutex
	logs []*service.UsageLog
}

func (r *poolGatewayUsageLogRepo) Create(_ context.Context, entry *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, entry)
	return true, nil
}

func newPoolGatewayRouteHandler(t *testing.T, platform, model string, upstream *poolGatewayRouteUpstream, useNewAPI bool, slotCaches ...service.ConcurrencyCache) (*GatewayHandler, *service.APIKey, *poolGatewayUsageLogRepo) {
	t.Helper()
	price := 1.0
	group := &service.Group{
		ID: 901, Platform: platform, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1,
		ModelPricing: []service.ChannelModelPricing{{Models: []string{model}, BillingMode: service.BillingModeToken, InputPrice: &price, OutputPrice: &price}},
	}
	accounts := make([]*service.Account, 6)
	repo := &grokCredentialHandlerRepo{}
	for i := range accounts {
		account := service.Account{
			ID: int64(801 + i), Name: "gateway-pool-fixture", Platform: platform, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: i + 1,
			GroupIDs: []int64{group.ID}, AccountGroups: []service.AccountGroup{{AccountID: int64(801 + i), GroupID: group.ID}},
			Credentials: map[string]any{
				"api_key": "fixture-only", "pool_mode": true, "pool_mode_retry_count": 1,
				"pool_mode_retry_status_codes": []any{float64(503)},
			},
		}
		if useNewAPI {
			account.Extra = map[string]any{service.AccountExtraNewAPIStyleInterfaceEnabled: true}
		}
		accounts[i] = &account
		repo.accounts = append(repo.accounts, account)
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 3
	var schedulerConcurrency *service.ConcurrencyService
	if len(slotCaches) > 0 {
		schedulerConcurrency = service.NewConcurrencyService(slotCaches[0])
		cfg.Gateway.Scheduling.FallbackWaitTimeout = 5 * time.Millisecond
		cfg.Gateway.Scheduling.FallbackMaxWaiting = 1
	}
	logs := &poolGatewayUsageLogRepo{}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, nil)
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewGatewayService(
		repo, &fakeGroupRepo{group: group}, logs, nil, nil, nil, nil, nil, cfg,
		snapshot, schedulerConcurrency, billing, nil, billingCache, nil, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
		service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil,
	)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	handlerConcurrency := schedulerConcurrency
	if handlerConcurrency == nil {
		handlerConcurrency = service.NewConcurrencyService(cache)
	}
	h := &GatewayHandler{
		gatewayService: gateway, billingCacheService: billingCache, cfg: cfg,
		concurrencyHelper:  NewConcurrencyHelper(handlerConcurrency, SSEPingFormatNone, 0),
		maxAccountSwitches: 3, maxAccountSwitchesGemini: 3,
	}
	h.geminiCompatService = service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
	h.newAPIStyleService = service.NewNewAPIStyleGatewayService(gateway, upstream, cfg, nil, nil)
	key := &service.APIKey{ID: 902, UserID: 903, GroupID: &group.ID, Group: group, Status: service.StatusActive, User: &service.User{ID: 903, Status: service.StatusActive, Balance: 100}}
	return h, key, logs
}

const poolGatewayGeminiSuccess = `{"candidates":[{"content":{"role":"model","parts":[{"text":"healthy-last-account"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`

func TestPoolGatewayAdmissionZeroSwitchDoesNotRoam(t *testing.T) {
	for _, kind := range []string{"compatible", "newapi", "gemini"} {
		for _, queueFull := range []bool{true, false} {
			t.Run(kind+"/queue_full="+strconv.FormatBool(queueFull), func(t *testing.T) {
				cache := &poolTraversalSlotCache{queueFull: queueFull}
				cache.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) { return true, nil }
				cache.acquireAccountSlotFn = func(_ context.Context, id int64, _ int, _ string) (bool, error) { return id == 806, nil }
				rec := httptest.NewRecorder()
				upstream := &poolGatewayRouteUpstream{response: poolGatewayChatSuccess, recorder: rec}
				platform, model := service.PlatformZhipu, "glm-4-flash"
				if kind == "gemini" {
					platform, model = service.PlatformGemini, "gemini-2.5-flash"
				}
				h, key, logs := newPoolGatewayRouteHandler(t, platform, model, upstream, kind == "newapi", cache)
				h.maxAccountSwitches, h.maxAccountSwitchesGemini = 0, 0
				var route gin.HandlerFunc = NewCompatibleGatewayHandler(service.NewCompatibleGatewayService(h.gatewayService, upstream, h.cfg, nil), h).ChatCompletions
				if kind == "newapi" {
					route = NewNewAPIStyleGatewayHandler(h).Embeddings
				}
				if kind == "gemini" {
					route = h.Messages
				}
				router := gin.New()
				router.POST("/v1/messages", func(c *gin.Context) {
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
					route(c)
				})
				body := `{"model":"` + model + `","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
				req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
				upstream.mu.Lock()
				require.Empty(t, upstream.hits, "zero-switch admission must not roam to the healthy sixth account")
				upstream.mu.Unlock()
				cache.mu.Lock()
				require.Zero(t, cache.waiting)
				cache.mu.Unlock()
				logs.mu.Lock()
				require.Empty(t, logs.logs)
				logs.mu.Unlock()
			})
		}
	}
}

const poolGatewayChatSuccess = `{"id":"chatcmpl_pool_route","object":"chat.completion","model":"glm-4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"healthy-last-account"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
const poolGatewayAnthropicResponsesSuccess = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_pool_route\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-5\",\"stop_reason\":\"\",\"usage\":{\"input_tokens\":1}}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"healthy-last-account\"}}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func TestPoolGatewayRoutesTraverseAfterConfiguredSameAccountRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path, body, platform, model, response, want string
		newAPI                                            bool
	}{
		{"gateway_messages", "/v1/messages", `{"model":"gemini-2.5-flash","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`, service.PlatformGemini, "gemini-2.5-flash", poolGatewayGeminiSuccess, "healthy-last-account", false},
		{"gateway_chat", "/v1/chat/completions", `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hello"}]}`, service.PlatformGemini, "gemini-2.5-flash", poolGatewayGeminiSuccess, "healthy-last-account", false},
		{"gateway_responses", "/v1/responses", `{"model":"claude-sonnet-4-5","input":"hello"}`, service.PlatformAnthropic, "claude-sonnet-4-5", poolGatewayAnthropicResponsesSuccess, "healthy-last-account", false},
		{"systemone", "/v1/systemone", validSystemOneHandlerBody, service.PlatformTypeSafe, "jev-latest", `{"answers":{"q":{"type":"noul","answer":"healthy-last-account"}},"usage":{"input_tokens":1,"output_tokens":1}}`, "healthy-last-account", false},
		{"compatible_messages", "/v1/messages", `{"model":"glm-4-flash","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`, service.PlatformZhipu, "glm-4-flash", poolGatewayChatSuccess, "healthy-last-account", false},
		{"compatible_chat", "/v1/chat/completions", `{"model":"glm-4-flash","messages":[{"role":"user","content":"hello"}]}`, service.PlatformZhipu, "glm-4-flash", poolGatewayChatSuccess, "healthy-last-account", false},
		{"compatible_responses", "/v1/responses", `{"model":"glm-4-flash","input":"hello"}`, service.PlatformZhipu, "glm-4-flash", poolGatewayChatSuccess, "healthy-last-account", false},
		{"newapi_embeddings", "/v1/embeddings", `{"model":"embedding-3","input":"hello"}`, service.PlatformZhipu, "embedding-3", `{"object":"list","model":"embedding-3","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`, `"embedding":[0.1,0.2]`, true},
		{"newapi_rerank", "/v1/rerank", `{"model":"BAAI/bge-reranker-v2-m3","query":"hello","documents":["world"]}`, service.PlatformSiliconFlow, "BAAI/bge-reranker-v2-m3", `{"id":"rerank_pool","results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":1}}`, `"relevance_score":0.9`, true},
		{"newapi_audio", "/v1/audio/transcriptions", `{"model":"glm-asr-2512","file":"fixture-audio"}`, service.PlatformZhipu, "glm-asr-2512", `{"text":"healthy-last-account","usage":{"prompt_tokens":1,"total_tokens":1}}`, "healthy-last-account", true},
		{"web_search", "/v1/web_search", `{"query":"fixture search"}`, service.PlatformGrok, "grok-web-search", `{"output":[{"type":"web_search_call","action":{"sources":[{"url":"https://example.test/source","title":"healthy-last-account","snippet":"fixture search"}]}}]}`, "example.test/source", false},
		{"x_search", "/v1/x_search", `{"query":"fixture search"}`, service.PlatformGrok, "grok-x-search", `{"output":[{"type":"x_search_call","action":{"sources":[{"url":"https://example.test/source","title":"healthy-last-account","snippet":"fixture search"}]}}]}`, "example.test/source", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			upstream := &poolGatewayRouteUpstream{response: tc.response, recorder: recorder}
			h, key, logs := newPoolGatewayRouteHandler(t, tc.platform, tc.model, upstream, tc.newAPI)
			compatible := NewCompatibleGatewayHandler(service.NewCompatibleGatewayService(h.gatewayService, upstream, h.cfg, nil), h)
			newAPI := NewNewAPIStyleGatewayHandler(h)
			var route gin.HandlerFunc
			switch tc.name {
			case "gateway_messages":
				route = h.Messages
			case "gateway_chat":
				route = h.ChatCompletions
			case "gateway_responses":
				route = h.Responses
			case "systemone":
				route = h.SystemOne
			case "compatible_messages":
				route = compatible.Messages
			case "compatible_chat":
				route = compatible.ChatCompletions
			case "compatible_responses":
				route = compatible.Responses
			case "newapi_embeddings":
				route = newAPI.Embeddings
			case "newapi_rerank":
				route = newAPI.Rerank
			case "newapi_audio":
				route = newAPI.Audio
			case "web_search":
				route = h.WebSearch
			case "x_search":
				route = h.XSearch
			}
			router := gin.New()
			router.POST(tc.path, func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
				route(c)
			})
			request := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), tc.want)
			upstream.mu.Lock()
			require.Equal(t, []int64{801, 801, 802, 802, 803, 803, 804, 804, 805, 805, 806}, upstream.hits)
			require.False(t, upstream.earlyBody, "a retryable failure must not become a premature client response")
			upstream.mu.Unlock()
			logs.mu.Lock()
			defer logs.mu.Unlock()
			require.Len(t, logs.logs, 1, "failed attempts must not create usage records; the successful attempt must record once")
			require.Equal(t, int64(806), logs.logs[0].AccountID)
		})
	}
}

func TestPoolGatewayRoutesSkipBusyCandidatesWithoutLeakingWaitCounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path, body, platform, model, response string
		newAPI                                      bool
	}{
		{"gateway_messages", "/v1/messages", `{"model":"gemini-2.5-flash","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`, service.PlatformGemini, "gemini-2.5-flash", poolGatewayGeminiSuccess, false},
		{"gateway_chat", "/v1/chat/completions", `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hello"}]}`, service.PlatformGemini, "gemini-2.5-flash", poolGatewayGeminiSuccess, false},
		{"gateway_responses", "/v1/responses", `{"model":"claude-sonnet-4-5","input":"hello"}`, service.PlatformAnthropic, "claude-sonnet-4-5", poolGatewayAnthropicResponsesSuccess, false},
		{"gemini_native", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, service.PlatformGemini, "gemini-2.5-flash", poolGatewayGeminiSuccess, false},
		{"compatible_messages", "/v1/messages", `{"model":"glm-4-flash","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`, service.PlatformZhipu, "glm-4-flash", poolGatewayChatSuccess, false},
		{"compatible_chat", "/v1/chat/completions", `{"model":"glm-4-flash","messages":[{"role":"user","content":"hello"}]}`, service.PlatformZhipu, "glm-4-flash", poolGatewayChatSuccess, false},
		{"compatible_responses", "/v1/responses", `{"model":"glm-4-flash","input":"hello"}`, service.PlatformZhipu, "glm-4-flash", poolGatewayChatSuccess, false},
		{"newapi_embeddings", "/v1/embeddings", `{"model":"embedding-3","input":"hello"}`, service.PlatformZhipu, "embedding-3", `{"object":"list","data":[{"index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`, true},
		{"newapi_rerank", "/v1/rerank", `{"model":"BAAI/bge-reranker-v2-m3","query":"hello","documents":["world"]}`, service.PlatformSiliconFlow, "BAAI/bge-reranker-v2-m3", `{"results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":1}}`, true},
		{"newapi_audio", "/v1/audio/transcriptions", `{"model":"glm-asr-2512","file":"fixture-audio"}`, service.PlatformZhipu, "glm-asr-2512", `{"text":"healthy-last-account","usage":{"prompt_tokens":1,"total_tokens":1}}`, true},
	} {
		for _, queueFull := range []bool{true, false} {
			kind := "timeout"
			if queueFull {
				kind = "queue_full"
			}
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				cache := &poolTraversalSlotCache{queueFull: queueFull}
				cache.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) { return true, nil }
				cache.acquireAccountSlotFn = func(_ context.Context, id int64, _ int, _ string) (bool, error) { return id == 806, nil }
				recorder := httptest.NewRecorder()
				upstream := &poolGatewayRouteUpstream{response: tc.response, recorder: recorder}
				h, key, logs := newPoolGatewayRouteHandler(t, tc.platform, tc.model, upstream, tc.newAPI, cache)
				compatible := NewCompatibleGatewayHandler(service.NewCompatibleGatewayService(h.gatewayService, upstream, h.cfg, nil), h)
				newAPI := NewNewAPIStyleGatewayHandler(h)
				var route gin.HandlerFunc
				switch tc.name {
				case "gateway_messages":
					route = h.Messages
				case "gateway_chat":
					route = h.ChatCompletions
				case "gateway_responses":
					route = h.Responses
				case "gemini_native":
					route = h.GeminiV1BetaModels
				case "compatible_messages":
					route = compatible.Messages
				case "compatible_chat":
					route = compatible.ChatCompletions
				case "compatible_responses":
					route = compatible.Responses
				case "newapi_embeddings":
					route = newAPI.Embeddings
				case "newapi_rerank":
					route = newAPI.Rerank
				case "newapi_audio":
					route = newAPI.Audio
				}
				router := gin.New()
				registeredPath := tc.path
				if tc.name == "gemini_native" {
					registeredPath = "/v1beta/models/*modelAction"
				}
				router.POST(registeredPath, func(c *gin.Context) {
					c.Set(string(middleware.ContextKeyAPIKey), key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
					route(c)
				})
				request := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
				request.Header.Set("Content-Type", "application/json")

				router.ServeHTTP(recorder, request)

				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				upstream.mu.Lock()
				require.Equal(t, []int64{806}, upstream.hits, "only an account with a slot may reach the upstream")
				require.False(t, upstream.earlyBody)
				upstream.mu.Unlock()
				cache.mu.Lock()
				require.Zero(t, cache.waiting, "every unsuccessful account wait must release its counter")
				cache.mu.Unlock()
				logs.mu.Lock()
				defer logs.mu.Unlock()
				require.Len(t, logs.logs, 1)
				require.Equal(t, int64(806), logs.logs[0].AccountID)
			})
		}
	}
}

func TestPoolGatewayAnthropicCountTokensTraversesWithoutUsageOrConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	upstream := &poolGatewayRouteUpstream{response: `{"input_tokens":7}`, recorder: recorder}
	h, key, logs := newPoolGatewayRouteHandler(t, service.PlatformAnthropic, "claude-sonnet-4-5", upstream, false)
	var slotCalls int
	slots := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			slotCalls++
			return false, nil
		},
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			slotCalls++
			return false, nil
		},
	}
	h.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(slots), SSEPingFormatNone, 0)
	router := gin.New()
	router.POST("/v1/messages/count_tokens", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
		h.CountTokens(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"input_tokens":7`)
	upstream.mu.Lock()
	require.Equal(t, []int64{801, 801, 802, 802, 803, 803, 804, 804, 805, 805, 806}, upstream.hits)
	require.False(t, upstream.earlyBody)
	upstream.mu.Unlock()
	require.Zero(t, slotCalls, "token counting must not acquire user or account concurrency slots")
	logs.mu.Lock()
	defer logs.mu.Unlock()
	require.Empty(t, logs.logs, "token counting is not billable model output")
}
