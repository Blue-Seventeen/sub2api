//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthUnifiedRate_ZeroChargeKeepsKeyAndSubscriptionLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name         string
		enabled      bool
		rate         float64
		balance      float64
		quotaUsed    float64
		expired      bool
		subscription bool
		wantStatus   int
		wantCode     string
	}{
		{name: "free_zero_balance", enabled: true, wantStatus: http.StatusOK},
		{name: "free_negative_balance", enabled: true, balance: -1, wantStatus: http.StatusOK},
		{name: "disabled_zero_is_paid", wantStatus: http.StatusForbidden, wantCode: "INSUFFICIENT_BALANCE"},
		{name: "negative_rate_is_paid", enabled: true, rate: -1, wantStatus: http.StatusForbidden, wantCode: "INSUFFICIENT_BALANCE"},
		{name: "positive_rate_is_paid", enabled: true, rate: 1, wantStatus: http.StatusForbidden, wantCode: "INSUFFICIENT_BALANCE"},
		{name: "free_key_quota_exhausted", enabled: true, quotaUsed: 1, wantStatus: http.StatusTooManyRequests, wantCode: "API_KEY_QUOTA_EXHAUSTED"},
		{name: "free_key_expired", enabled: true, expired: true, wantStatus: http.StatusForbidden, wantCode: "API_KEY_EXPIRED"},
		{name: "free_subscription_daily_limit", enabled: true, subscription: true, wantStatus: http.StatusTooManyRequests, wantCode: "USAGE_LIMIT_EXCEEDED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard}
			user := &service.User{ID: 1, Role: service.RoleUser, Status: service.StatusActive, Balance: tc.balance, UnifiedRateEnabled: tc.enabled, UnifiedRateMultiplier: tc.rate}
			key := &service.APIKey{ID: 1, UserID: 1, Key: "unified-rate-test-key", User: user, Status: service.StatusActive, Quota: 1, QuotaUsed: tc.quotaUsed}
			if tc.expired {
				past := time.Now().Add(-time.Hour)
				key.ExpiresAt = &past
			}
			var subSvc *service.SubscriptionService
			if tc.subscription {
				limit := 1.0
				key.Group = &service.Group{ID: 7, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeSubscription, DailyLimitUSD: &limit}
				key.GroupID = &key.Group.ID
				now := time.Now()
				sub := &service.UserSubscription{ID: 1, UserID: 1, GroupID: 7, Status: service.SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, DailyUsageUSD: 1.1}
				subRepo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) {
					clone := *sub
					return &clone, nil
				}}
				subSvc = service.NewSubscriptionService(nil, subRepo, nil, nil, cfg)
				t.Cleanup(subSvc.Stop)
			}
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { clone := *key; return &clone, nil }}
			svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			router := newAuthTestRouter(svc, subSvc, cfg)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			req.Header.Set("x-api-key", key.Key)
			router.ServeHTTP(w, req)
			require.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			if tc.wantCode != "" {
				require.Contains(t, w.Body.String(), tc.wantCode)
			}
		})
	}
}
