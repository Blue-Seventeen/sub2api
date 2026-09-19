package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type grokMediaBillingUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *grokMediaBillingUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

type grokMediaBillingRepo struct {
	service.UsageBillingRepository
	commands []*service.UsageBillingCommand
	failNext bool
}

func (r *grokMediaBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.commands = append(r.commands, cmd)
	if r.failNext {
		r.failNext = false
		return nil, errors.New("billing unavailable")
	}
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

type grokMediaBillingLogRepo struct {
	service.UsageLogRepository
	logs []*service.UsageLog
}

func (r *grokMediaBillingLogRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.logs = append(r.logs, log)
	return true, nil
}

type grokMediaBillingUpstream struct {
	service.HTTPUpstream
	body    string
	respond func(*http.Request) *http.Response
}

func (u *grokMediaBillingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if u.respond != nil {
		return u.respond(req), nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"},
			"X-Request-Id": {"grok-media-image-request"},
		},
		Body: io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func newGrokMediaBillingHandler(t *testing.T) (*OpenAIGatewayHandler, *service.APIKey, *service.Account, *grokMediaBillingRepo, *grokMediaBillingLogRepo) {
	return newGrokMediaBillingHandlerWithCache(t, nil, &grokMediaBillingUpstream{body: `{"data":[{"url":"https://example.test/image.png"}]}`})
}

func newGrokMediaBillingHandlerWithCache(t *testing.T, gatewayCache service.GatewayCache, upstream service.HTTPUpstream) (*OpenAIGatewayHandler, *service.APIKey, *service.Account, *grokMediaBillingRepo, *grokMediaBillingLogRepo) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	price := 0.25
	group := &service.Group{
		ID: 42, Platform: service.PlatformGrok, Status: service.StatusActive,
		AllowImageGeneration: true, RateMultiplier: 2, ImagePrice2K: &price,
	}
	key := &service.APIKey{
		ID: 24, GroupID: &group.ID, Group: group,
		User: &service.User{
			ID: 12, Status: service.StatusActive, Balance: 100,
			UnifiedRateEnabled: true, UnifiedRateMultiplier: 3,
		},
	}
	account := &service.Account{
		ID: 7, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		GroupIDs:    []int64{group.ID},
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://api.x.ai"},
	}
	users := &grokMediaBillingUserRepo{user: key.User}
	billing := &grokMediaBillingRepo{}
	logs := &grokMediaBillingLogRepo{}
	cache := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	gateway := service.NewOpenAIGatewayService(
		&openAIWSUsageHandlerAccountRepoStub{account: *account}, logs, billing, users, nil, nil, gatewayCache,
		cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache,
		upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	h := NewOpenAIGatewayHandler(
		gateway, nil, service.NewConcurrencyService(nil), cache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, nil, nil, cfg,
	)
	return h, key, account, billing, logs
}

type grokMediaBillingCache struct {
	service.GatewayCache
	mu            sync.Mutex
	sessions      map[string]int64
	pending       map[string][]byte
	claims        map[string]bool
	storeFailures int
	claimErr      error
}

func newGrokMediaBillingCache() *grokMediaBillingCache {
	return &grokMediaBillingCache{
		sessions: make(map[string]int64), pending: make(map[string][]byte), claims: make(map[string]bool),
	}
}

func (s *grokMediaBillingCache) SetSessionAccountID(_ context.Context, _ int64, key string, accountID int64, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[key] = accountID
	return nil
}

func (s *grokMediaBillingCache) GetSessionAccountID(_ context.Context, _ int64, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[key], nil
}

func (s *grokMediaBillingCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *grokMediaBillingCache) DeleteSessionAccountID(_ context.Context, _ int64, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
	return nil
}

func (s *grokMediaBillingCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storeFailures > 0 {
		s.storeFailures--
		return errors.New("pending store unavailable")
	}
	s.pending[key] = append([]byte(nil), payload...)
	return nil
}

func (s *grokMediaBillingCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.pending[key]...), nil
}

func (s *grokMediaBillingCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return false, s.claimErr
	}
	if s.claims[key] {
		return false, nil
	}
	s.claims[key] = true
	return true, nil
}

func (s *grokMediaBillingCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claims, key)
	return nil
}

func grokMediaBillingContext(key *service.APIKey, method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyAPIKey), key)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: key.User.ID})
	return c, rec
}

func TestGrokMediaVideoHandlerDefersAndBillsOnce(t *testing.T) {
	for _, endpoint := range []service.GrokMediaEndpoint{service.GrokMediaEndpointVideosGenerations, service.GrokMediaEndpointVideosEdits, service.GrokMediaEndpointVideosExtensions} {
		for _, firstLookup := range []service.GrokMediaEndpoint{service.GrokMediaEndpointVideoStatus, service.GrokMediaEndpointVideoContent} {
			t.Run(string(endpoint)+"/"+string(firstLookup), func(t *testing.T) {
				cache := newGrokMediaBillingCache()
				cache.storeFailures = 1
				status := `{"status":"pending"}`
				upstream := &grokMediaBillingUpstream{respond: func(req *http.Request) *http.Response {
					body, contentType := status, "application/json"
					if req.Method == http.MethodPost {
						body = `{"request_id":"task-1"}`
					} else if req.URL.Path == "/video.mp4" {
						body, contentType = "video-data", "video/mp4"
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
				}}
				h, key, _, billing, logs := newGrokMediaBillingHandlerWithCache(t, cache, upstream)
				c, rec := grokMediaBillingContext(key, http.MethodPost, "/v1/videos/generations", `{"model":"grok-imagine-video","prompt":"test","duration":6,"resolution":"720p"}`)
				h.handleGrokMedia(c, endpoint, "")
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Empty(t, billing.commands)
				require.Empty(t, logs.logs)
				pending, err := h.gatewayService.LoadGrokVideoPendingBilling(context.Background(), "task-1", key.User.ID, key.ID)
				require.NoError(t, err)
				require.NotNil(t, pending, "create snapshot must survive a transient cache failure")
				require.Equal(t, "720p", pending.VideoResolution)
				require.Equal(t, 6, pending.VideoDurationSeconds)
				require.NotEmpty(t, pending.CreatedAt)

				c, rec = grokMediaBillingContext(key, http.MethodGet, "/v1/videos/task-1", "")
				h.handleGrokMedia(c, service.GrokMediaEndpointVideoStatus, "task-1")
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Empty(t, billing.commands)
				status = `{"status":"done","model":"grok-imagine-video","video":{"url":"https://vidgen.x.ai/video.mp4","duration":9}}`
				for _, lookup := range []service.GrokMediaEndpoint{firstLookup, service.GrokMediaEndpointVideoStatus, service.GrokMediaEndpointVideoContent} {
					c, rec = grokMediaBillingContext(key, http.MethodGet, "/v1/videos/task-1", "")
					h.handleGrokMedia(c, lookup, "task-1")
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				}
				require.Len(t, billing.commands, 1)
				require.Len(t, logs.logs, 1)
				require.Equal(t, "grok-video:task-1", billing.commands[0].RequestID)
				require.Equal(t, 1, logs.logs[0].VideoCount)
				require.NotNil(t, logs.logs[0].VideoResolution)
				require.Equal(t, "720p", *logs.logs[0].VideoResolution)
				require.NotNil(t, logs.logs[0].VideoDurationSeconds)
				require.Equal(t, 9, *logs.logs[0].VideoDurationSeconds)
				require.InDelta(t, logs.logs[0].ActualCost/3, billing.commands[0].BalanceCost, 1e-9)
			})
		}
	}
}

func TestGrokMediaVideoCompletionClaimAndRetry(t *testing.T) {
	cache := newGrokMediaBillingCache()
	h, key, account, billing, _ := newGrokMediaBillingHandlerWithCache(t, cache, nil)
	subject := middleware2.AuthSubject{UserID: key.User.ID}
	ctx := context.Background()
	status := &service.OpenAIForwardResult{VideoCount: 1, ImageCount: 1}
	require.Nil(t, prepareGrokVideoCompletionBilling(ctx, h, zap.NewNop(), key, subject, "task-2", status), "missing duration and snapshot must not consume a claim")
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(ctx, "task-2", subject.UserID, key.ID, service.GrokVideoPendingBilling{
		Model: "grok-imagine-video", VideoResolution: "1080p", VideoDurationSeconds: 6,
		CreatedAt: time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano),
	}))
	cache.claimErr = errors.New("claim unavailable")
	require.Nil(t, prepareGrokVideoCompletionBilling(ctx, h, zap.NewNop(), key, subject, "task-2", status))
	cache.claimErr = nil
	result := prepareGrokVideoCompletionBilling(ctx, h, zap.NewNop(), key, subject, "task-2", status)
	require.NotNil(t, result)
	require.Equal(t, "grok-video:task-2", result.RequestID)
	require.Zero(t, result.ImageCount)
	require.Equal(t, "1080p", result.VideoResolution)
	require.Equal(t, 6, result.VideoDurationSeconds)
	require.GreaterOrEqual(t, result.Duration, time.Minute)
	require.Empty(t, status.RequestID, "completion must not mutate the forward result")
	require.Nil(t, prepareGrokVideoCompletionBilling(ctx, h, zap.NewNop(), key, subject, "task-2", status))

	billing.failNext = true
	c, _ := grokMediaBillingContext(key, http.MethodGet, "/v1/videos/task-2", "")
	recordGrokMediaUsage(c, h, zap.NewNop(), key, subject, nil, account, result, result.Model, nil, "task-2", service.ChannelMappingResult{}, time.Now())
	retry := prepareGrokVideoCompletionBilling(ctx, h, zap.NewNop(), key, subject, "task-2", status)
	require.NotNil(t, retry, "failed durable billing must release the claim")
	recordGrokMediaUsage(c, h, zap.NewNop(), key, subject, nil, account, retry, retry.Model, nil, "task-2", service.ChannelMappingResult{}, time.Now())
	require.Len(t, billing.commands, 2)
	require.Equal(t, billing.commands[0].RequestID, billing.commands[1].RequestID)
	require.Equal(t, billing.commands[0].RequestFingerprint, billing.commands[1].RequestFingerprint)
	require.Nil(t, prepareGrokVideoCompletionBilling(ctx, h, zap.NewNop(), key, subject, "task-2", status))
}

func TestGrokMediaUsageGateRequiresTypedResult(t *testing.T) {
	gate := reflect.TypeOf(shouldRecordGrokMediaUsage)
	require.False(t, gate.IsVariadic(), "omitting the forward result must be a compile error")
	require.Equal(t, reflect.TypeOf((*service.OpenAIForwardResult)(nil)), gate.In(2))
}

func TestGrokMediaImagesRecordUsageThroughHandler(t *testing.T) {
	for _, path := range []string{"/v1/images/generations", "/v1/images/edits"} {
		t.Run(path, func(t *testing.T) {
			h, key, _, billing, logs := newGrokMediaBillingHandler(t)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"grok-imagine-image","prompt":"test","size":"2K"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware2.ContextKeyAPIKey), key)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: key.User.ID})

			h.GrokImages(c)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "image.png")
			require.Len(t, billing.commands, 1, "a successful image must reach billing, not only pass the helper test")
			require.Len(t, logs.logs, 1)
			require.Equal(t, 1, logs.logs[0].ImageCount)
			require.InDelta(t, 0.25, logs.logs[0].TotalCost, 1e-9)
			require.InDelta(t, 1.5, logs.logs[0].ActualCost, 1e-9)
			require.InDelta(t, 0.5, logs.logs[0].RealActualCost, 1e-9)
			require.InDelta(t, 0.5, billing.commands[0].BalanceCost, 1e-9)
		})
	}
}

func TestGrokMediaRecordUsagePreservesPricingAndSubscription(t *testing.T) {
	for _, tc := range []struct {
		name         string
		subscription bool
		hour         int
		actual       float64
		real         float64
	}{
		{name: "balance peak", hour: 10, actual: 6, real: 2},
		{name: "balance off peak", hour: 13, actual: 1.5, real: 0.5},
		{name: "subscription peak", subscription: true, hour: 10, actual: 6, real: 2},
		{name: "subscription off peak", subscription: true, hour: 13, actual: 1.5, real: 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, key, account, billing, logs := newGrokMediaBillingHandler(t)
			key.Group.PeakRateEnabled = true
			key.Group.PeakStart, key.Group.PeakEnd, key.Group.PeakRateMultiplier = "09:00", "12:00", 4
			var subscription *service.UserSubscription
			if tc.subscription {
				key.Group.SubscriptionType = service.SubscriptionTypeSubscription
				subscription = &service.UserSubscription{ID: 9, UserID: key.User.ID, GroupID: key.Group.ID}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			result := &service.OpenAIForwardResult{RequestID: tc.name, Model: "grok-imagine-image", ImageCount: 1, ImageSize: "2K"}
			pricingAt := time.Date(2026, 9, 1, tc.hour, 0, 0, 0, timezone.Location())

			recordGrokMediaUsage(c, h, zap.NewNop(), key, middleware2.AuthSubject{UserID: key.User.ID}, subscription,
				account, result, result.Model, []byte(`{"model":"grok-imagine-image"}`), "", service.ChannelMappingResult{}, pricingAt)

			require.Len(t, billing.commands, 1)
			require.Len(t, logs.logs, 1)
			require.InDelta(t, tc.actual, logs.logs[0].ActualCost, 1e-9)
			require.InDelta(t, tc.real, logs.logs[0].RealActualCost, 1e-9)
			require.Equal(t, 3.0, logs.logs[0].UnifiedRateMultiplier)
			if tc.subscription {
				require.Equal(t, service.BillingTypeSubscription, logs.logs[0].BillingType)
				require.InDelta(t, tc.actual, billing.commands[0].SubscriptionCost, 1e-9)
				require.Equal(t, key.User.ID, billing.commands[0].SubscriptionUserID)
				require.Equal(t, key.Group.ID, billing.commands[0].SubscriptionGroupID)
				require.Zero(t, billing.commands[0].BalanceCost)
			} else {
				require.InDelta(t, tc.real, billing.commands[0].BalanceCost, 1e-9)
				require.Zero(t, billing.commands[0].SubscriptionCost)
			}
		})
	}
}
