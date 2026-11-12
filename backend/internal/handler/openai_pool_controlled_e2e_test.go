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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIPoolControlledUpstream struct {
	mu        sync.Mutex
	hits      []int64
	statuses  map[int64]int
	markers   map[int64]string
	recorder  *httptest.ResponseRecorder
	earlyBody bool
}

func (u *openAIPoolControlledUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.hits = append(u.hits, accountID)
	if u.recorder != nil && u.recorder.Body.Len() != 0 {
		u.earlyBody = true
	}
	status := u.statuses[accountID]
	marker := u.markers[accountID]
	u.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	if status != http.StatusOK {
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"server_error","code":"fixture_transient","message":"` + marker + `"}}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_pool_controlled","object":"response","status":"completed","model":"gpt-4o-mini","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + marker + `"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)),
	}, nil
}

func (u *openAIPoolControlledUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func newOpenAIPoolControlledFixture(t *testing.T, upstream *openAIPoolControlledUpstream, withBilling bool) (*OpenAIGatewayHandler, *service.APIKey, *grokCredentialHandlerRepo, *grokMediaBillingLogRepo, *grokMediaBillingRepo, *config.Config) {
	t.Helper()
	price := 1.0
	group := &service.Group{ID: 901, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 1, ModelPricing: []service.ChannelModelPricing{{Models: []string{"gpt-4o-mini"}, BillingMode: service.BillingModeToken, InputPrice: &price, OutputPrice: &price}}}
	accounts := make([]service.Account, 13)
	statuses := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable}
	for i := range accounts {
		id := int64(801 + i)
		accounts[i] = service.Account{ID: id, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: i + 1, GroupIDs: []int64{group.ID}, Credentials: map[string]any{
			"api_key": "fixture-only", "pool_mode": true, "pool_mode_retry_count": 0,
			"pool_mode_retry_status_codes": []any{float64(http.StatusUnauthorized), float64(http.StatusForbidden), float64(http.StatusTooManyRequests), float64(http.StatusServiceUnavailable)},
			"custom_error_codes_enabled":   true, "custom_error_codes": []any{float64(http.StatusTeapot)},
		}, Extra: map[string]any{"openai_responses_mode": "force_responses"}}
		if upstream.statuses == nil {
			upstream.statuses = make(map[int64]int)
		}
		if upstream.markers == nil {
			upstream.markers = make(map[int64]string)
		}
		if i < 12 {
			upstream.statuses[id] = statuses[i%len(statuses)]
			upstream.markers[id] = "FIXTURE_" + strconv.Itoa(i+1)
		} else {
			upstream.markers[id] = "POOL_FINAL_ACCOUNT_813"
		}
	}
	repo := &grokCredentialHandlerRepo{accounts: accounts}
	logs := &grokMediaBillingLogRepo{}
	billing := &grokMediaBillingRepo{}
	user := &service.User{ID: 903, Status: service.StatusActive, Balance: 100}
	users := &grokMediaBillingUserRepo{user: user}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billingCache := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	billingService := service.NewBillingService(cfg, nil)
	var usageRepo service.UsageLogRepository
	var billingRepo service.UsageBillingRepository
	if withBilling {
		usageRepo, billingRepo = logs, billing
	}
	gateway := service.NewOpenAIGatewayService(repo, usageRepo, billingRepo, users, nil, nil, nil, cfg, nil, nil, billingService, nil, billingCache, upstream, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(nil, billingService), nil, nil, nil, nil)
	cache := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }}
	h := NewOpenAIGatewayHandler(gateway, nil, service.NewConcurrencyService(cache), billingCache, &service.APIKeyService{}, nil, nil, nil, nil, nil, nil, cfg)
	h.maxAccountSwitches = 3
	key := &service.APIKey{ID: 902, UserID: user.ID, GroupID: &group.ID, Group: group, User: user}
	return h, key, repo, logs, billing, cfg
}

func TestOpenAIPoolControlledStatusTraversalReachesThirteenthAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	upstream := &openAIPoolControlledUpstream{recorder: recorder}
	h, key, repo, logs, billing, _ := newOpenAIPoolControlledFixture(t, upstream, true)
	router := gin.New()
	router.POST("/openai/v1/responses", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
		h.Responses(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-4o-mini","input":"hello","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "POOL_FINAL_ACCOUNT_813")
	for i := 1; i <= 12; i++ {
		require.NotContains(t, recorder.Body.String(), "FIXTURE_"+strconv.Itoa(i))
	}
	upstream.mu.Lock()
	require.Equal(t, []int64{801, 802, 803, 804, 805, 806, 807, 808, 809, 810, 811, 812, 813}, upstream.hits)
	require.False(t, upstream.earlyBody)
	upstream.mu.Unlock()
	repo.mu.Lock()
	require.Empty(t, repo.setErrorIDs)
	require.Empty(t, repo.setTempIDs)
	require.Empty(t, repo.rateLimitIDs)
	for _, account := range repo.accounts {
		require.Equal(t, service.StatusActive, account.Status)
		require.True(t, account.Schedulable)
	}
	repo.mu.Unlock()
	require.Len(t, logs.logs, 1)
	require.Equal(t, int64(813), logs.logs[0].AccountID)
	require.Len(t, billing.commands, 1)
	require.Equal(t, int64(813), billing.commands[0].AccountID)
}

func TestOpenAIPoolControlledStatusTraversalExhaustsCandidatesWithoutUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	upstream := &openAIPoolControlledUpstream{recorder: recorder}
	for i := 0; i < 13; i++ {
		if upstream.statuses == nil {
			upstream.statuses = make(map[int64]int)
		}
		upstream.statuses[int64(801+i)] = []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable}[i%4]
	}
	h, key, _, logs, billing, _ := newOpenAIPoolControlledFixture(t, upstream, true)
	router := gin.New()
	router.POST("/openai/v1/responses", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
		h.Responses(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-4o-mini","input":"hello","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.NotEqual(t, http.StatusOK, recorder.Code, recorder.Body.String())
	upstream.mu.Lock()
	require.Equal(t, []int64{801, 802, 803, 804, 805, 806, 807, 808, 809, 810, 811, 812, 813}, upstream.hits)
	require.False(t, upstream.earlyBody)
	upstream.mu.Unlock()
	for i := 1; i <= 13; i++ {
		require.NotContains(t, recorder.Body.String(), "FIXTURE_"+strconv.Itoa(i))
	}
	require.Empty(t, logs.logs)
	require.Empty(t, billing.commands)
}
