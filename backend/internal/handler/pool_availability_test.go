//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The real handlers and scheduler must reach a healthy account beyond the old
// switch cap, without exposing any of the preceding failures to the client.
type poolAvailabilityUpstream struct {
	service.HTTPUpstream
	goodID         int64
	hits           []int64
	recorder       *httptest.ResponseRecorder
	prematureWrite bool
}

func (u *poolAvailabilityUpstream) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	if u.recorder.Body.Len() != 0 {
		u.prematureWrite = true
	}
	u.hits = append(u.hits, id)
	status := http.StatusServiceUnavailable
	body := `{"error":{"type":"server_error","message":"fixture unavailable"}}`
	if id == u.goodID {
		status = http.StatusOK
		switch {
		case strings.Contains(req.URL.Path, "input_tokens"):
			body = `{"object":"response.input_tokens","input_tokens":7}`
		case strings.HasSuffix(req.URL.Path, "/stt"):
			body = `{"text":"healthy-voice"}`
		case strings.Contains(req.URL.Path, "/embeddings"):
			body = `{"object":"list","model":"text-embedding-3-small","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`
		case strings.Contains(req.URL.Path, "/images/"):
			body = `{"created":1,"data":[{"b64_json":"aGVhbHRoeQ=="}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
		default:
			body = `{"id":"chatcmpl_pool","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"healthy-last-account"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		}
	}
	contentType := "application/json"
	if status == http.StatusOK && strings.Contains(req.URL.Path, "/responses") && !strings.Contains(req.URL.Path, "input_tokens") {
		contentType = "text/event-stream"
		body = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"healthy-last-account\",\"item_id\":\"msg_pool\",\"output_index\":0,\"content_index\":0}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_pool\",\"status\":\"completed\",\"model\":\"gpt-4o-mini\",\"output\":[{\"type\":\"message\",\"id\":\"msg_pool\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"healthy-last-account\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}, "X-Request-Id": []string{"fixture-pool"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestPoolAvailabilityCountersAndVoice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, path, body, platform, want string }{
		{"responses_input_tokens", "/v1/responses/input_tokens", `{"model":"gpt-4o-mini","input":"hello"}`, service.PlatformOpenAI, `"input_tokens":7`},
		{"messages_count_tokens", "/v1/messages/count_tokens", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`, service.PlatformOpenAI, `"input_tokens":7`},
		{"voice_stt", "/v1/stt", `{"audio":"fixture"}`, service.PlatformGrok, "healthy-voice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &grokCredentialHandlerRepo{}
			for i := 0; i < 6; i++ {
				repo.accounts = append(repo.accounts, service.Account{ID: int64(801 + i), Name: "safe-read-fixture", Platform: tc.platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: i + 1, Credentials: map[string]any{"api_key": "fixture-only", "pool_mode": true, "pool_mode_retry_count": float64(1), "pool_mode_retry_status_codes": []any{float64(503)}}})
			}
			recorder := httptest.NewRecorder()
			upstream := &poolAvailabilityUpstream{goodID: 806, recorder: recorder}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.MaxAccountSwitches = 3
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			slots := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }}
			h := NewOpenAIGatewayHandler(gateway, nil, service.NewConcurrencyService(slots), billing, &service.APIKeyService{}, nil, nil, nil, nil, nil, nil, cfg)
			gid := int64(901)
			key := &service.APIKey{ID: 902, GroupID: &gid, User: &service.User{ID: 903, Status: service.StatusActive}, Group: &service.Group{ID: gid, Platform: tc.platform, Status: service.StatusActive, AllowMessagesDispatch: true}}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 903, Concurrency: 1})
				c.Next()
			})
			switch tc.name {
			case "responses_input_tokens":
				router.POST(tc.path, h.ResponsesInputTokens)
			case "messages_count_tokens":
				router.POST(tc.path, h.CountTokens)
			default:
				router.POST(tc.path, func(c *gin.Context) { h.GrokVoice(c, "stt") })
			}
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, []int64{801, 801, 802, 802, 803, 803, 804, 804, 805, 805, 806}, upstream.hits)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), tc.want)
			require.False(t, upstream.prematureWrite)
		})
	}
}

func (u *poolAvailabilityUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestPoolAvailabilityIndependentHTTPHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, path, body, wantText string
		retries                    int
		allFail                    bool
	}{
		{"chat_last_candidate", "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`, "healthy-last-account", 0, false},
		{"images_last_candidate", "/v1/images/generations", `{"model":"gpt-image-1","prompt":"cat"}`, "aGVhbHRoeQ==", 0, false},
		{"embeddings_retry_then_last_candidate", "/v1/embeddings", `{"model":"text-embedding-3-small","input":"hello"}`, `"embedding":[0.1,0.2]`, 1, false},
		{"all_failed_stops_after_each_budget", "/v1/embeddings", `{"model":"text-embedding-3-small","input":"hello"}`, "", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &grokCredentialHandlerRepo{}
			for i := 0; i < 6; i++ {
				repo.accounts = append(repo.accounts, service.Account{ID: int64(801 + i), Name: "availability-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: i + 1,
					Credentials: map[string]any{"api_key": "fixture-only", "pool_mode": true, "pool_mode_retry_count": float64(tc.retries), "pool_mode_retry_status_codes": []any{float64(503)}}})
			}
			recorder := httptest.NewRecorder()
			upstream := &poolAvailabilityUpstream{goodID: 806, recorder: recorder}
			if tc.allFail {
				upstream.goodID = 0
			}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Gateway.MaxAccountSwitches = 3
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			slots := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }}
			h := NewOpenAIGatewayHandler(gateway, nil, service.NewConcurrencyService(slots), billing, &service.APIKeyService{}, nil, nil, nil, nil, nil, nil, cfg)
			gid := int64(901)
			key := &service.APIKey{ID: 902, GroupID: &gid, User: &service.User{ID: 903, Status: service.StatusActive}, Group: &service.Group{ID: gid, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowImageGeneration: true, AllowMessagesDispatch: true}}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 903, Concurrency: 1})
				c.Next()
			})
			switch tc.path {
			case "/v1/embeddings":
				router.POST(tc.path, h.Embeddings)
			case "/v1/images/generations":
				router.POST(tc.path, h.Images)
			default:
				router.POST(tc.path, h.ChatCompletions)
			}
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			want := []int64{}
			for id := int64(801); id <= 806; id++ {
				n := tc.retries + 1
				if id == 806 && !tc.allFail {
					n = 1
				}
				for i := 0; i < n; i++ {
					want = append(want, id)
				}
			}
			require.Equal(t, want, upstream.hits, "every eligible candidate must be attempted with its configured budget")
			if tc.allFail {
				require.Equal(t, http.StatusBadGateway, recorder.Code)
			} else {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), tc.wantText)
			}
			require.False(t, upstream.prematureWrite, "intermediate errors must stay server-side")
		})
	}
}

func TestPoolAvailabilityStateTraversesCandidatesWithoutReopeningFailures(t *testing.T) {
	fs := NewFailoverState(1, false)
	for id := int64(1); id <= 6; id++ {
		require.Equal(t, FailoverContinue, fs.HandleFailoverError(context.Background(), &mockTempUnscheduler{}, id, service.PlatformOpenAI, 0, newTestFailoverErr(503, false, false)))
	}
	require.Len(t, fs.FailedAccountIDs, 6)
	require.Equal(t, FailoverExhausted, fs.HandleSelectionExhausted(context.Background()))
	require.Len(t, fs.FailedAccountIDs, 6, "exhaustion cannot reset failures and create another full sweep")
}

func TestPoolAvailabilityRealtimePreacceptExhaustsCandidates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &grokCredentialHandlerRepo{}
	for i := 0; i < 6; i++ {
		repo.accounts = append(repo.accounts, service.Account{ID: int64(801 + i), Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: i + 1, Credentials: map[string]any{"api_key": "fixture-only", "base_url": "https://127.0.0.1:1", "pool_mode": true, "pool_mode_retry_count": 0}})
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 3
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	var acquiredIDs []int64
	cache := &concurrencyCacheMock{acquireAccountSlotFn: func(_ context.Context, id int64, _ int, _ string) (bool, error) {
		acquiredIDs = append(acquiredIDs, id)
		return true, nil
	}}
	concurrency := service.NewConcurrencyService(cache)
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billing, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, nil, service.NewConcurrencyService(cache), billing, &service.APIKeyService{}, nil, nil, nil, nil, nil, nil, cfg)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	c.Request.Header.Set("Upgrade", "websocket")
	c.Request.Header.Set("Connection", "Upgrade")
	gid := int64(901)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 902, GroupID: &gid, User: &service.User{ID: 903}, Group: &service.Group{ID: gid, Platform: service.PlatformGrok}})
	h.GrokRealtime(c)
	require.Equal(t, []int64{801, 802, 803, 804, 805, 806}, acquiredIDs)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.NotContains(t, recorder.Header(), "Sec-WebSocket-Accept", "no upstream handshake succeeded")
}
