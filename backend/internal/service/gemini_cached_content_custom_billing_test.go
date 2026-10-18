//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/stretchr/testify/require"
)

func TestGeminiCacheStoragePreservesCustomMultipliers(t *testing.T) {
	svc, logs := newGeminiCacheBillingGatewayForTest(t)
	user := &User{ID: 621, UnifiedRateEnabled: true, UnifiedRateMultiplier: 0.25}
	groupID := int64(91)
	group := &Group{ID: groupID, Platform: PlatformGemini, RateMultiplier: 2, Hydrated: true,
		PeakRateEnabled: true, PeakRateWindows: []PeakRateWindow{{Start: "00:00", End: "23:59", Multiplier: 3}}}
	apiKey := &APIKey{ID: 821, GroupID: &groupID, Group: group, User: user}
	input := &RecordUsageInput{Result: &ForwardResult{RequestID: "gemini-cache-custom-cost", Model: "gemini-3.8-flash", CacheStorageTokenHours: 20000},
		APIKey: apiKey, User: user, Account: &Account{ID: 713, Platform: PlatformGemini},
		PricingAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	quote := svc.QuoteUsageCost(context.Background(), input)
	require.InDelta(t, 0.01*2*3*0.25, quote, 1e-12)
	require.NoError(t, svc.RecordUsage(context.Background(), input))
	require.InDelta(t, quote, logs.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 0.01*2*3, logs.lastLog.RealActualCost, 1e-12)
	require.InDelta(t, 0.01, logs.lastLog.TotalCost, 1e-12)
}

type geminiManagedProxyCapture struct {
	geminiCompatHTTPUpstreamStub
	proxyURL string
}

func (s *geminiManagedProxyCapture) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	s.proxyURL = proxyURL
	return s.geminiCompatHTTPUpstreamStub.Do(req, proxyURL, accountID, concurrency)
}

func TestGeminiCacheAndModelsResolveManagedProxy(t *testing.T) {
	previous := GetDefaultManagedProxyResolver()
	SetDefaultManagedProxyResolver(managedProxyLegacyResolver{})
	t.Cleanup(func() { SetDefaultManagedProxyResolver(previous) })
	subscriptionID, proxyID := int64(5), int64(6)
	account := &Account{ID: 9, Type: AccountTypeAPIKey, Platform: PlatformGemini,
		Credentials: map[string]any{"api_key": "test-key"}, ProxyID: &proxyID,
		Proxy: &Proxy{ID: proxyID, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subscriptionID}}
	upstream := &geminiManagedProxyCapture{}
	compat := &GeminiMessagesCompatService{httpUpstream: upstream}
	response := func() {
		upstream.response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}
	}
	response()
	svc := NewGeminiCachedContentService(nil, compat, nil)
	_, err := svc.doUpstream(context.Background(), account, http.MethodGet, "https://generativelanguage.googleapis.com/v1beta/cachedContents/test", nil, time.Second)
	require.NoError(t, err)
	require.Equal(t, "socks5h://127.0.0.1:1080", upstream.proxyURL)
	response()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://generativelanguage.googleapis.com/v1beta/models", nil)
	require.NoError(t, err)
	_, err = compat.doGeminiModelsGET(req, account)
	require.NoError(t, err)
	require.Equal(t, "socks5h://127.0.0.1:1080", upstream.proxyURL)
	require.Equal(t, upstream.proxyURL, vertexServiceAccountProxyURL(account))
}

func TestVertexCacheTokenExchangeKeepsColdAutomaticProxyClosed(t *testing.T) {
	previous := GetDefaultProxyAutoProbeService()
	SetDefaultProxyAutoProbeService(nil)
	t.Cleanup(func() { SetDefaultProxyAutoProbeService(previous) })
	account := &Account{ID: 91, Platform: PlatformGemini, Type: AccountTypeServiceAccount, Extra: map[string]any{"auto_select_proxy": true}}
	require.Equal(t, proxyurl.AutoProxyUnavailableURL, vertexServiceAccountProxyURL(account, context.Background()))
}

func TestFixedCacheChargePreservesRealBalanceAndStackedCustomHeadroom(t *testing.T) {
	ctx := context.Background()
	user := &User{ID: 7, UnifiedRateEnabled: true, UnifiedRateMultiplier: 0.25}
	newService := func(cache *fixedChargeCacheStub) *BillingCacheService {
		cfg := &config.Config{}
		cfg.Billing.InflightReservation.Enabled = true
		svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
		t.Cleanup(svc.Stop)
		return svc
	}
	t.Run("display rate does not discount real balance admission", func(t *testing.T) {
		svc := newService(&fixedChargeCacheStub{balance: 1})
		_, err := svc.ReserveFixedCharge(ctx, user, nil, nil, nil, 0.5)
		require.ErrorIs(t, err, ErrInsufficientBalance)
	})
	t.Run("custom window rejects a charge exceeding its headroom", func(t *testing.T) {
		limit := 5.0
		group := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, CustomLimitHours: 2, CustomLimitUSD: &limit}
		svc := newService(&fixedChargeCacheStub{sub: &SubscriptionCacheData{Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour), CustomUsage: 4}})
		res, err := svc.ReserveFixedCharge(ctx, user, nil, group, &UserSubscription{ID: 1}, 1)
		require.NoError(t, err)
		res.Release()
		_, err = svc.ReserveFixedCharge(ctx, user, nil, group, &UserSubscription{ID: 1}, 1.01)
		require.ErrorIs(t, err, ErrCustomLimitExceeded)
	})
	t.Run("different cards reserve the same aggregate headroom", func(t *testing.T) {
		limit, available := 5.0, 3.0
		group := &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, CustomLimitHours: 2, CustomLimitUSD: &limit}
		svc := newService(&fixedChargeCacheStub{sub: &SubscriptionCacheData{Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour), StackedAvailableUSD: &available}})
		first, err := svc.ReserveFixedCharge(ctx, user, nil, group, &UserSubscription{ID: 1}, 2)
		require.NoError(t, err)
		defer first.Release()
		_, err = svc.ReserveFixedCharge(ctx, user, nil, group, &UserSubscription{ID: 99}, 2)
		require.ErrorIs(t, err, ErrDailyLimitExceeded)
	})
}
