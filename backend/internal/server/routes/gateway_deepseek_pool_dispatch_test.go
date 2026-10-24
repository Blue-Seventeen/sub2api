package routes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type deepSeekPoolDispatchAccounts struct {
	service.AccountRepository
	mu         sync.Mutex
	accounts   []service.Account
	groupCalls []int64
	platforms  []string
}

func (r *deepSeekPoolDispatchAccounts) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platforms = append(r.platforms, platform)
	var accounts []service.Account
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (r *deepSeekPoolDispatchAccounts) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]service.Account, error) {
	r.mu.Lock()
	r.groupCalls = append(r.groupCalls, groupID)
	r.mu.Unlock()
	accounts, err := r.ListSchedulableByPlatform(ctx, platform)
	var filtered []service.Account
	for _, account := range accounts {
		for _, membership := range account.GroupIDs {
			if membership == groupID {
				filtered = append(filtered, account)
				break
			}
		}
	}
	return filtered, err
}

func (r *deepSeekPoolDispatchAccounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, account := range r.accounts {
		if account.ID == id {
			copy := account
			return &copy, nil
		}
	}
	return nil, nil
}

type deepSeekPoolDispatchUsage struct {
	service.UsageLogRepository
	mu   sync.Mutex
	logs []*service.UsageLog
}

func (r *deepSeekPoolDispatchUsage) Create(_ context.Context, entry *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *entry
	r.logs = append(r.logs, &copy)
	return true, nil
}

func (r *deepSeekPoolDispatchUsage) snapshot() []*service.UsageLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*service.UsageLog(nil), r.logs...)
}

type deepSeekPoolDispatchUpstream struct {
	service.HTTPUpstream
	mu               sync.Mutex
	hits             []int64
	paths            []string
	accept           []string
	recorder         *httptest.ResponseRecorder
	prematureWritten bool
}

func (u *deepSeekPoolDispatchUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.hits = append(u.hits, accountID)
	u.paths = append(u.paths, req.URL.Path)
	u.accept = append(u.accept, req.Header.Get("Accept"))
	if u.recorder.Body.Len() != 0 {
		u.prematureWritten = true
	}
	status, contentType := http.StatusServiceUnavailable, "application/json"
	body := `{"error":{"type":"server_error","message":"DEEPSEEK_FAILED_ATTEMPT_MUST_STAY_PRIVATE"}}`
	if accountID == 802 {
		status = http.StatusOK
		body = `{"id":"chatcmpl_deepseek_pool","object":"chat.completion","model":"deepseek-chat","choices":[{"index":0,"message":{"role":"assistant","content":"DEEPSEEK_POOL_OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`
		requestBody, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(requestBody), `"stream":true`) {
			contentType = "text/event-stream"
			body = "data: {\"id\":\"chatcmpl_deepseek_pool\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-chat\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"DEEPSEEK_POOL_OK\"},\"finish_reason\":null}]}\n\n" +
				"data: {\"id\":\"chatcmpl_deepseek_pool\",\"object\":\"chat.completion.chunk\",\"model\":\"deepseek-chat\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
				"data: [DONE]\n\n"
		}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType},
		"X-Request-Id": {"deepseek-pool-fixture"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (u *deepSeekPoolDispatchUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

// The two usage repositories deliberately differ. A successful production route
// must record through OpenAIGatewayHandler even when NewAPI performs forwarding;
// accidentally dispatching the DeepSeek route to CompatibleGateway is observable.
func runDeepSeekPoolDispatch(t *testing.T, groupNewAPI, accountNewAPI, stream, directCompatible bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	price := 1.0
	group := &service.Group{ID: 121, Platform: service.PlatformDeepseek, Status: service.StatusActive,
		Hydrated: true, RateMultiplier: 1, NewAPIStyleInterfaceEnabled: groupNewAPI,
		ModelPricing: []service.ChannelModelPricing{{Models: []string{"deepseek-chat"}, BillingMode: service.BillingModeToken,
			InputPrice: &price, OutputPrice: &price}}}
	accounts := &deepSeekPoolDispatchAccounts{}
	for i := 0; i < 2; i++ {
		accounts.accounts = append(accounts.accounts, service.Account{ID: int64(801 + i), Platform: service.PlatformDeepseek,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: i + 1,
			GroupIDs: []int64{group.ID}, AccountGroups: []service.AccountGroup{{AccountID: int64(801 + i), GroupID: group.ID}},
			Credentials: map[string]any{"api_key": "fixture-only", "base_url": "https://api.deepseek.com",
				"api_protocol": "chat_completions", "pool_mode": true, "pool_mode_retry_count": 1,
				"pool_mode_retry_status_codes": []any{float64(503)}},
			Extra: map[string]any{service.AccountExtraNewAPIStyleInterfaceEnabled: accountNewAPI}})
	}
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{
		MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20, MaxAccountSwitches: 3}}
	cfg.Default.RateMultiplier = 1
	openAIUsage, compatibleUsage := &deepSeekPoolDispatchUsage{}, &deepSeekPoolDispatchUsage{}
	recorder := httptest.NewRecorder()
	upstream := &deepSeekPoolDispatchUpstream{recorder: recorder}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	billing := service.NewBillingService(cfg, nil)
	resolver := service.NewModelPricingResolver(nil, billing)
	deferred := &service.DeferredService{}
	gateway := service.NewGatewayService(accounts, legacyCNRouteGroups{group: group}, compatibleUsage,
		nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, billingCache, nil, upstream, deferred,
		nil, nil, nil, nil, nil, nil, nil, resolver, nil, nil, nil, nil)
	newAPIService := service.NewNewAPIStyleGatewayService(gateway, upstream, cfg, nil, nil)
	openAIGateway := service.NewOpenAIGatewayService(accounts, openAIUsage, nil, nil, nil, nil, nil, cfg,
		nil, nil, billing, nil, billingCache, upstream, deferred, nil, nil, resolver, nil, nil, nil, nil)
	concurrency := service.NewConcurrencyService(nil)
	openAIHandler := handler.NewOpenAIGatewayHandler(openAIGateway, newAPIService, concurrency, billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, cfg)
	base := handler.NewGatewayHandler(gateway, newAPIService, nil, nil, nil, nil, concurrency, billingCache,
		nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil)
	compatible := handler.NewCompatibleGatewayHandler(service.NewCompatibleGatewayService(gateway, upstream, cfg, nil), base)
	key := &service.APIKey{ID: 321, UserID: 421, GroupID: &group.ID, Group: group, Status: service.StatusActive,
		User: &service.User{ID: 421, Status: service.StatusActive, Balance: 100}}
	auth := func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyAPIKey), key)
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: key.UserID})
		c.Next()
	}
	router := gin.New()
	if directCompatible {
		router.POST("/v1/chat/completions", auth, compatible.ChatCompletions)
	} else {
		RegisterGatewayRoutes(router, &handler.Handlers{Gateway: base, OpenAIGateway: openAIHandler,
			CompatibleGateway: compatible, NewAPIStyleGateway: handler.NewNewAPIStyleGatewayHandler(base),
			AsyncImage: handler.NewAsyncImageHandler(nil, nil)},
			servermiddleware.APIKeyAuthMiddleware(auth), nil, nil, nil, nil, nil, cfg)
	}
	body := fmt.Sprintf(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hello"}],"stream":%t,"stream_options":{"include_usage":true}}`, stream)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "DEEPSEEK_POOL_OK")
	require.NotContains(t, recorder.Body.String(), "DEEPSEEK_FAILED_ATTEMPT_MUST_STAY_PRIVATE")
	if stream {
		require.Contains(t, recorder.Body.String(), "[DONE]", "the successful stream must reach its protocol terminal")
	}
	upstream.mu.Lock()
	require.Equal(t, []int64{801, 801, 802}, upstream.hits, "count=1 means retry account 801 once before switching to account 802")
	wantUpstreamPath := "/v1/chat/completions"
	if directCompatible {
		// DeepSeek's direct native preset appends /chat/completions to its
		// versionless base URL. OpenAI/NewAPI routing uses the relay /v1 path.
		wantUpstreamPath = "/chat/completions"
	}
	require.Equal(t, []string{wantUpstreamPath, wantUpstreamPath, wantUpstreamPath}, upstream.paths)
	require.False(t, upstream.prematureWritten, "a 503 attempt must not write downstream before selection finishes")
	if !directCompatible {
		for _, accept := range upstream.accept {
			if groupNewAPI || accountNewAPI {
				require.Equal(t, "application/json", accept, "NewAPI must retain its default Accept header even for a streamed payload")
			}
		}
	}
	upstream.mu.Unlock()
	expectedUsage, wrongUsage := openAIUsage, compatibleUsage
	if directCompatible {
		expectedUsage, wrongUsage = compatibleUsage, openAIUsage
	}
	require.Eventually(t, func() bool { return len(expectedUsage.snapshot()) >= 1 }, time.Second, 5*time.Millisecond)
	rows := expectedUsage.snapshot()
	require.Len(t, rows, 1, "failed attempts must not record usage; only the final successful attempt records once")
	require.Empty(t, wrongUsage.snapshot(), "DeepSeek production dispatch and direct Compatible path must remain distinguishable")
	require.Equal(t, int64(802), rows[0].AccountID)
	require.NotNil(t, rows[0].GroupID)
	require.Equal(t, group.ID, *rows[0].GroupID)
	require.Equal(t, 5, rows[0].InputTokens)
	require.Equal(t, 2, rows[0].OutputTokens)
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	// Simple mode deliberately lists by platform, so group-scoped repository
	// calls are optional. Both candidate memberships and the billed group must
	// nevertheless identify the authenticated group used by this request.
	for _, account := range accounts.accounts {
		require.Equal(t, []int64{group.ID}, account.GroupIDs)
	}
	for _, groupID := range accounts.groupCalls {
		require.Equal(t, group.ID, groupID, "all candidates must stay inside the authenticated group")
	}
	for _, platform := range accounts.platforms {
		require.Equal(t, service.PlatformDeepseek, platform)
	}
}

func TestDeepSeekProductionChatPoolRetryDispatchesOpenAIGateway(t *testing.T) {
	for _, switches := range []struct {
		name           string
		group, account bool
	}{{"newapi-off", false, false}, {"newapi-group", true, false}, {"newapi-account", false, true}} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", switches.name, stream), func(t *testing.T) {
				runDeepSeekPoolDispatch(t, switches.group, switches.account, stream, false)
			})
		}
	}
}

func TestDeepSeekDirectCompatibleChatPoolRetryRecordsOnce(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			runDeepSeekPoolDispatch(t, false, false, stream, true)
		})
	}
}
