//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type poolTraversalUpstream struct {
	*grokCredentialHandlerUpstream
	onFirst func()
	once    sync.Once
}

func (u *poolTraversalUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	response, err := u.grokCredentialHandlerUpstream.Do(req, proxyURL, accountID, concurrency)
	if u.onFirst != nil {
		u.once.Do(u.onFirst)
	}
	return response, err
}

func newPoolTraversalAccounts(count int) []service.Account {
	accounts := make([]service.Account, count)
	for i := range accounts {
		accounts[i] = service.Account{
			ID: int64(801 + i), Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: i + 1,
			Credentials: map[string]any{
				"api_key": "test-pool-key", "pool_mode": true, "pool_mode_retry_count": 0,
			},
		}
	}
	return accounts
}

func newPoolTraversalHandler(t *testing.T, accounts []service.Account, upstream service.HTTPUpstream, cfg *config.Config, slotCaches ...service.ConcurrencyCache) (*OpenAIGatewayHandler, *grokCredentialHandlerRepo, *gin.Engine) {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{RunMode: config.RunModeSimple}
	}
	repo := &grokCredentialHandlerRepo{accounts: accounts}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	var schedulerConcurrency *service.ConcurrencyService
	var handlerConcurrency *service.ConcurrencyService
	if len(slotCaches) > 0 {
		schedulerConcurrency = service.NewConcurrencyService(slotCaches[0])
		handlerConcurrency = schedulerConcurrency
	}
	gateway := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg, nil, schedulerConcurrency,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	if handlerConcurrency == nil {
		handlerConcurrency = service.NewConcurrencyService(cache)
	}
	h := NewOpenAIGatewayHandler(gateway, nil, handlerConcurrency, billingCache, &service.APIKeyService{}, nil, nil, nil, nil, nil, nil, cfg)
	h.maxAccountSwitches = 3
	groupID := int64(901)
	key := &service.APIKey{
		ID: 902, GroupID: &groupID, User: &service.User{ID: 903, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: accounts[0].Platform, Status: service.StatusActive, AllowMessagesDispatch: true},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.User.ID, Concurrency: 1})
		c.Next()
	})
	router.POST("/openai/v1/responses", h.Responses)
	router.POST("/openai/v1/messages", h.Messages)
	router.POST("/openai/v1/chat/completions", h.ChatCompletions)
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	return h, repo, router
}

func TestOpenAIPoolTraversalBeyondLegacySwitchLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ name, path, body string }{
		{"responses", "/openai/v1/responses", `{"model":"grok","input":"hello","stream":false}`},
		{"messages", "/openai/v1/messages", `{"model":"grok","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			upstream := &grokCredentialHandlerUpstream{failureStatus: map[int64]int{801: 520, 802: 520, 803: 520, 804: 520, 805: 520}}
			_, _, router := newPoolTraversalHandler(t, newPoolTraversalAccounts(6), upstream, nil)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, endpoint.path, bytes.NewBufferString(endpoint.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, []int64{801, 802, 803, 804, 805, 806}, upstream.accountHits())
		})
	}
}

func TestOpenAIPoolRetryPinsAccountWithoutStickyBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ name, path, body string }{
		{"responses", "/openai/v1/responses", `{"model":"grok","input":"hello","stream":false}`},
		{"messages", "/openai/v1/messages", `{"model":"grok","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			accounts := newPoolTraversalAccounts(2)
			accounts[0].Credentials["pool_mode_retry_count"] = 1
			upstream := &poolTraversalUpstream{grokCredentialHandlerUpstream: &grokCredentialHandlerUpstream{rateLimitIDs: map[int64]bool{801: true}}}
			_, repo, router := newPoolTraversalHandler(t, accounts, upstream, nil)
			upstream.onFirst = func() {
				repo.mu.Lock()
				defer repo.mu.Unlock()
				repo.accounts[1].Priority = 0
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, endpoint.path, bytes.NewBufferString(endpoint.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, []int64{801, 801, 802}, upstream.accountHits(), "ranking changes must not consume a same-account retry on another account")
		})
	}
}

func TestOpenAIPoolTraversalAllFailedStopsAfterOnePass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &grokCredentialHandlerUpstream{failureStatus: map[int64]int{801: 520, 802: 520, 803: 520, 804: 520, 805: 520, 806: 520}}
	_, _, router := newPoolTraversalHandler(t, newPoolTraversalAccounts(6), upstream, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{801, 802, 803, 804, 805, 806}, upstream.accountHits(), "each failed account must be tried once before pool exhaustion")
}

func TestOpenAIWSPoolTraversalBeyondLegacySwitchLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &grokCredentialHandlerUpstream{failureStatus: map[int64]int{801: 520, 802: 520, 803: 520, 804: 520, 805: 520}}
	_, _, router := newPoolTraversalHandler(t, newPoolTraversalAccounts(6), upstream, nil)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
	require.NoError(t, err)
	defer func() { _ = connection.CloseNow() }()
	require.NoError(t, connection.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"grok","input":"hello","stream":false}`)))

	_, payload, err := connection.Read(ctx)

	require.NoError(t, err)
	require.Contains(t, string(payload), "resp_healthy")
	require.Equal(t, []int64{801, 802, 803, 804, 805, 806}, upstream.accountHits())
}

func TestOpenAIWSPoolRetryUsesConfiguredSameAccountBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accounts := newPoolTraversalAccounts(2)
	accounts[0].Credentials["pool_mode_retry_count"] = 1
	accounts[0].Credentials["pool_mode_retry_status_codes"] = []any{500}
	upstream := &grokCredentialHandlerUpstream{failureStatus: map[int64]int{801: http.StatusInternalServerError}}
	_, _, router := newPoolTraversalHandler(t, accounts, upstream, nil)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
	require.NoError(t, err)
	defer func() { _ = connection.CloseNow() }()
	require.NoError(t, connection.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"grok","input":"hello","stream":false}`)))

	_, payload, err := connection.Read(ctx)

	require.NoError(t, err)
	require.Contains(t, string(payload), "resp_healthy")
	require.Equal(t, []int64{801, 801, 802}, upstream.accountHits(), "WebSocket must honor configured non-429 pool retries before switching")
}

type poolTraversalSlotCache struct {
	concurrencyCacheMock
	queueFull  bool
	mu         sync.Mutex
	waiting    int
	accountIDs []int64
}

func (c *poolTraversalSlotCache) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	c.mu.Lock()
	c.accountIDs = append(c.accountIDs, accountID)
	c.mu.Unlock()
	return c.concurrencyCacheMock.AcquireAccountSlot(ctx, accountID, maxConcurrency, requestID)
}

func (c *poolTraversalSlotCache) IncrementAccountWaitCount(_ context.Context, _ int64, _ int) (bool, error) {
	if c.queueFull {
		return false, nil
	}
	c.mu.Lock()
	c.waiting++
	c.mu.Unlock()
	return true, nil
}

func (c *poolTraversalSlotCache) DecrementAccountWaitCount(context.Context, int64) error {
	c.mu.Lock()
	c.waiting--
	c.mu.Unlock()
	return nil
}

func TestOpenAIPoolTraversalSkipsUnavailableAccountSlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ name, path, body string }{
		{"responses", "/openai/v1/responses", `{"model":"grok","input":"hello","stream":false}`},
		{"messages", "/openai/v1/messages", `{"model":"grok","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
	} {
		for _, queueFull := range []bool{true, false} {
			name := "wait timeout"
			if queueFull {
				name = "queue full"
			}
			t.Run(endpoint.name+"/"+name, func(t *testing.T) {
				cache := &poolTraversalSlotCache{queueFull: queueFull}
				cache.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) { return true, nil }
				cache.acquireAccountSlotFn = func(_ context.Context, id int64, _ int, _ string) (bool, error) { return id != 801, nil }
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Gateway.Scheduling.FallbackWaitTimeout = 5 * time.Millisecond
				cfg.Gateway.Scheduling.FallbackMaxWaiting = 1
				upstream := &grokCredentialHandlerUpstream{}
				_, _, router := newPoolTraversalHandler(t, newPoolTraversalAccounts(2), upstream, cfg, cache)
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, endpoint.path, bytes.NewBufferString(endpoint.body))
				request.Header.Set("Content-Type", "application/json")

				router.ServeHTTP(recorder, request)

				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Equal(t, []int64{802}, upstream.accountHits(), "an unavailable account slot must not hide another eligible account")
				cache.mu.Lock()
				defer cache.mu.Unlock()
				require.Zero(t, cache.waiting, "wait count must be released before trying the next account")
			})
		}
	}
}

func TestOpenAIPoolTraversalAllBusyReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ name, path, body string }{
		{"responses", "/openai/v1/responses", `{"model":"grok","input":"hello","stream":false}`},
		{"messages", "/openai/v1/messages", `{"model":"grok","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			cache := &poolTraversalSlotCache{queueFull: true}
			cache.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) { return true, nil }
			cache.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) { return false, nil }
			upstream := &grokCredentialHandlerUpstream{}
			_, _, router := newPoolTraversalHandler(t, newPoolTraversalAccounts(2), upstream, nil, cache)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, endpoint.path, bytes.NewBufferString(endpoint.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
			require.Empty(t, upstream.accountHits())
			cache.mu.Lock()
			defer cache.mu.Unlock()
			require.Contains(t, cache.accountIDs, int64(801))
			require.Contains(t, cache.accountIDs, int64(802), "both accounts must be checked before pool exhaustion")
		})
	}
}

func TestOpenAIPoolTraversalKeepsLastFailureWhenRemainingAccountsBusy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &poolTraversalSlotCache{queueFull: true}
	cache.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) { return true, nil }
	cache.acquireAccountSlotFn = func(_ context.Context, id int64, _ int, _ string) (bool, error) { return id == 801, nil }
	upstream := &grokCredentialHandlerUpstream{failureStatus: map[int64]int{801: 520}}
	_, _, router := newPoolTraversalHandler(t, newPoolTraversalAccounts(2), upstream, nil, cache)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"grok","input":"hello","stream":false}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
	require.Equal(t, []int64{801}, upstream.accountHits())
}

type poolFirstOutputUpstream struct {
	service.HTTPUpstream
	mu      sync.Mutex
	hits    []int64
	onFirst func()
}

func (u *poolFirstOutputUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.hits = append(u.hits, accountID)
	u.mu.Unlock()
	if accountID == 801 {
		if u.onFirst != nil {
			u.onFirst()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: &poolFirstOutputBody{closed: make(chan struct{})}}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"recovered\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_recovered\",\"model\":\"gpt-5.1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")),
	}, nil
}

type poolFirstOutputBody struct {
	closed chan struct{}
	once   sync.Once
}

func (b *poolFirstOutputBody) Read([]byte) (int, error) { <-b.closed; return 0, io.EOF }
func (b *poolFirstOutputBody) Close() error             { b.once.Do(func() { close(b.closed) }); return nil }

func TestOpenAIResponsesFirstOutputTimeoutTraversesRemainingPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accounts := newPoolTraversalAccounts(2)
	for i := range accounts {
		accounts[i].Platform = service.PlatformOpenAI
		accounts[i].Type = service.AccountTypeOAuth
		accounts[i].Credentials = map[string]any{"access_token": "test-oauth-token"}
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	upstream := &poolFirstOutputUpstream{}
	_, _, router := newPoolTraversalHandler(t, accounts, upstream, cfg)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.1","input":"hello","stream":true}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "recovered")
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	require.Equal(t, []int64{801, 802}, upstream.hits)
}

func TestOpenAIResponsesFirstOutputTimeoutAfterHeartbeatStillTraversesPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accounts := newPoolTraversalAccounts(2)
	for i := range accounts {
		accounts[i].Platform = service.PlatformOpenAI
		accounts[i].Type = service.AccountTypeOAuth
		accounts[i].Credentials = map[string]any{"access_token": "test-oauth-token"}
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	upstream := &poolFirstOutputUpstream{}
	handler, _, _ := newPoolTraversalHandler(t, accounts, upstream, cfg)
	c, recorder := newAstraProFailoverContext(t, `{"model":"gpt-5.1","input":"hello","stream":true}`)
	upstream.onFirst = func() {
		_, err := fmt.Fprint(c.Writer, ": ping\n\n")
		require.NoError(t, err)
		c.Writer.Flush()
	}

	handler.Responses(c)

	require.Contains(t, recorder.Body.String(), "recovered", "a service-certified pre-output timeout must survive a non-semantic heartbeat")
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	require.Equal(t, []int64{801, 802}, upstream.hits)
}

type poolCustomStatusUpstream struct {
	service.HTTPUpstream
	mu      sync.Mutex
	hits    []int64
	status  int
	message string
}

func (u *poolCustomStatusUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.hits = append(u.hits, accountID)
	u.mu.Unlock()
	status, body := u.status, `{"error":{"type":"invalid_request_error","code":"fixture_rejected","message":"`+u.message+`"}}`
	contentType := "application/json"
	if accountID == 806 {
		status = http.StatusOK
		body = `{"id":"resp_custom_pool","status":"completed","model":"gpt-4o-mini","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"custom-status-recovered"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`
		if strings.Contains(req.URL.Path, "chat/completions") {
			body = `{"id":"chatcmpl_custom_pool","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"custom-status-recovered"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		} else {
			var payload struct{ Stream bool }
			if requestBody, err := io.ReadAll(req.Body); err == nil {
				_ = json.Unmarshal(requestBody, &payload)
			}
			if payload.Stream {
				contentType = "text/event-stream"
				body = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
			}
		}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestOpenAIPoolConsumesConfiguredNonDefault404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ name, path, body string }{
		{"responses", "/openai/v1/responses", `{"model":"gpt-4o-mini","input":"hello","stream":false}`},
		{"messages", "/openai/v1/messages", `{"model":"gpt-4o-mini","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`},
		{"chat", "/openai/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"stream":false}`},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			accounts := newPoolTraversalAccounts(6)
			for i := range accounts {
				accounts[i].Platform = service.PlatformOpenAI
				accounts[i].Extra = map[string]any{"openai_responses_mode": "force_responses"}
				accounts[i].Credentials["pool_mode_retry_count"] = 1
				accounts[i].Credentials["pool_mode_retry_status_codes"] = []any{float64(404)}
			}
			upstream := &poolCustomStatusUpstream{status: http.StatusNotFound, message: "fixture capacity route unavailable"}
			_, _, router := newPoolTraversalHandler(t, accounts, upstream, nil)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "custom-status-recovered")
			upstream.mu.Lock()
			defer upstream.mu.Unlock()
			require.Equal(t, []int64{801, 801, 802, 802, 803, 803, 804, 804, 805, 805, 806}, upstream.hits)
		})
	}
}

func TestOpenAIPoolMalformed400RemainsTerminalDespiteConfiguredCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accounts := newPoolTraversalAccounts(2)
	for i := range accounts {
		accounts[i].Platform = service.PlatformOpenAI
		accounts[i].Extra = map[string]any{"openai_responses_mode": "force_responses"}
		accounts[i].Credentials["pool_mode_retry_count"] = 2
		accounts[i].Credentials["pool_mode_retry_status_codes"] = []any{float64(400)}
	}
	upstream := &poolCustomStatusUpstream{status: http.StatusBadRequest, message: "Invalid value for messages: expected an array"}
	_, _, router := newPoolTraversalHandler(t, accounts, upstream, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	require.Equal(t, []int64{801}, upstream.hits, "request-shape errors must not be amplified across account retries")
}
