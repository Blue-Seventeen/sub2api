package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
)

func TestProxyAutoProbeColdStartDoesNotSelectUnmeasuredProxy(t *testing.T) {
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{{ID: 1, Status: StatusActive}}}
	svc := NewProxyAutoProbeService(nil, repo, nil, nil)
	svc.config.Enabled = true
	now := time.Now()
	require.NoError(t, svc.initializeEntries(context.Background(), now))
	require.Nil(t, svc.getBestProxy(context.Background()), "unmeasured is not healthy")
	id, due := svc.acquireDueProxy(now)
	require.True(t, due, "probe a new candidate immediately")
	require.Equal(t, int64(1), id)
}

func TestProxyAutoProbeUsesCurrentNodeStateAndMeasurement(t *testing.T) {
	ctx := context.Background()
	epoch, old := time.Now(), time.Now().Add(-time.Hour)
	subID, latency := int64(12), int64(1)
	repo := &proxyAutoProbeRepoStub{}
	states := &managedProxyNodeStateRepo{states: map[int64]ProxySubscriptionNode{}}
	cache := &proxyAutoProbeLatencyCacheStub{items: map[int64]*ProxyLatencyInfo{}}
	for id := int64(1); id <= 5; id++ {
		repo.proxies = append(repo.proxies, Proxy{ID: id, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID})
		states.states[id] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: epoch}
		cache.items[id] = &ProxyLatencyInfo{Success: true, QualityStatus: "healthy", LatencyMs: &latency, NodeEpoch: &epoch, QualityNodeEpoch: &epoch}
	}
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusSourceMissing, UpdatedAt: epoch}
	states.states[2] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusInactive, UpdatedAt: epoch}
	cache.items[3].NodeEpoch, cache.items[3].QualityNodeEpoch = &old, &old
	cache.items[5].QualityNodeEpoch = &old
	admin := &adminServiceImpl{proxyRepo: repo, proxySubRepo: states, managedProxyResolver: managedProxyNodeRunningResolver{}}
	svc := NewProxyAutoProbeService(admin, repo, nil, cache)
	svc.config.Enabled = true
	require.NoError(t, svc.initializeEntries(ctx, epoch))
	best := svc.getBestProxy(ctx)
	require.NotNil(t, best)
	require.Equal(t, int64(4), best.ID, "missing, disabled and stale measurements must not win")
	require.Nil(t, svc.getUsableSuccessProxy(ctx, 1))
	require.Nil(t, svc.getUsableSuccessProxy(ctx, 2))
	require.Nil(t, svc.getUsableSuccessProxy(ctx, 3))
	require.Nil(t, svc.getUsableSuccessProxy(ctx, 5))
}

func TestProxyAutoProbeReconcileInvalidatesChangedNode(t *testing.T) {
	ctx := context.Background()
	epoch, latency := time.Now(), int64(5)
	subID := int64(12)
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{{ID: 1, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID, nodeUpdatedAt: epoch}}}
	cache := &proxyAutoProbeLatencyCacheStub{items: map[int64]*ProxyLatencyInfo{1: {Success: true, QualityStatus: "healthy", LatencyMs: &latency, NodeEpoch: &epoch, QualityNodeEpoch: &epoch}}}
	svc := NewProxyAutoProbeService(nil, repo, nil, cache)
	svc.config.Enabled = true
	require.NoError(t, svc.initializeEntries(ctx, epoch))
	require.NotNil(t, svc.getBestProxy(ctx))
	repo.proxies[0].nodeUpdatedAt = epoch.Add(time.Second)
	require.NoError(t, svc.reconcileEntries(ctx, epoch.Add(time.Second)))
	require.Nil(t, svc.getBestProxy(ctx), "the previous generation's success cannot authorize a changed node")
	id, due := svc.acquireDueProxy(epoch.Add(time.Second))
	require.True(t, due)
	require.Equal(t, int64(1), id)
}

func TestProxyAutoProbeDoesNotSelectUnavailableOrExpiredSnapshot(t *testing.T) {
	ctx := context.Background()
	subID, latency := int64(12), int64(5)
	expired := time.Now().Add(-time.Minute)
	for _, proxy := range []Proxy{
		{ID: 1, Status: StatusActive, ExpiresAt: &expired},
		{ID: 1, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID, NodeStatus: ProxySubscriptionNodeStatusSourceMissing},
		{ID: 1, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID, NodeStatus: ProxySubscriptionNodeStatusActive, RuntimeStatus: &ManagedProxyRuntimeStatus{Status: ManagedProxyRuntimeStatusError}},
	} {
		svc := NewProxyAutoProbeService(nil, nil, nil, nil)
		svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &latency}
		seedProxyAutoProbeSnapshots(svc, proxy)
		require.Nil(t, svc.getBestProxy(ctx))
		require.Nil(t, svc.getUsableSuccessProxy(ctx, 1))
	}
}

type autoProbeHealthAdmin struct {
	AdminService
	qualityEpoch *time.Time
	qualityHook  func()
}

func (a autoProbeHealthAdmin) TestProxy(context.Context, int64) (*ProxyTestResult, error) {
	return &ProxyTestResult{Success: true, LatencyMs: 5}, nil
}

func (a autoProbeHealthAdmin) CheckProxyQuality(context.Context, int64) (*ProxyQualityCheckResult, error) {
	if a.qualityHook != nil {
		a.qualityHook()
	}
	return &ProxyQualityCheckResult{BaseLatencyMs: 5, Items: []ProxyQualityCheckItem{{Target: "openai", Status: "pass"}}, nodeEpoch: a.qualityEpoch}, nil
}

func TestProxyAutoProbeLatencyOnlyCacheIsNotHealthy(t *testing.T) {
	epoch, latency, subID := time.Now(), int64(5), int64(12)
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{{ID: 1, Status: StatusActive, nodeUpdatedAt: epoch, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID}}}
	cache := &proxyAutoProbeLatencyCacheStub{items: map[int64]*ProxyLatencyInfo{1: {Success: true, LatencyMs: &latency, NodeEpoch: &epoch}}}
	svc := NewProxyAutoProbeService(nil, repo, nil, cache)
	svc.config.Enabled = true
	require.NoError(t, svc.initializeEntries(context.Background(), epoch))
	require.Nil(t, svc.getBestProxy(context.Background()))
	require.Nil(t, svc.getUsableSuccessProxy(context.Background(), 1))
	_, due := svc.acquireDueProxy(epoch)
	require.True(t, due, "a connectivity-only cache still needs a quality probe")
}

func TestProxyAutoProbeManualLateMeasurementDoesNotBlessChangedAddress(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	repo := &proxyAutoProbeRepoStub{proxies: []Proxy{{ID: 1, Status: StatusActive, Host: "before.example", UpdatedAt: now}}}
	svc := NewProxyAutoProbeService(nil, repo, nil, nil)
	svc.config.Enabled = true
	require.NoError(t, svc.initializeEntries(ctx, now))
	svc.adminService = autoProbeHealthAdmin{qualityHook: func() {
		repo.proxies[0].Host = "after.example"
		repo.proxies[0].UpdatedAt = now.Add(time.Second)
		admin := svc.adminService
		svc.adminService = nil
		require.NoError(t, svc.reconcileEntries(ctx, now.Add(time.Second)))
		svc.adminService = admin
	}}
	svc.finishProbe(1, svc.probeProxy(ctx, 1), now.Add(time.Second))
	require.Nil(t, svc.getBestProxy(ctx))
	require.False(t, svc.GetStatus().RecentCompletions[0].Success)
}

func TestProxyAutoProbeStickyReadsQueueAndSnapshotTogether(t *testing.T) {
	svc := NewProxyAutoProbeService(nil, nil, nil, nil)
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess}
	seedProxyAutoProbeSnapshots(svc, Proxy{ID: 1, Status: StatusActive, Host: "healthy.example"})
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 50000; i++ {
			svc.mu.Lock()
			svc.entries[1].Queue = ProxyAutoProbeQueueFailed
			svc.proxySnapshots[1] = Proxy{ID: 1, Status: StatusActive, Host: "unmeasured.example"}
			svc.mu.Unlock()
			runtime.Gosched()
			svc.mu.Lock()
			svc.entries[1].Queue = ProxyAutoProbeQueueSuccess
			svc.proxySnapshots[1] = Proxy{ID: 1, Status: StatusActive, Host: "healthy.example"}
			svc.mu.Unlock()
		}
	}()
	close(start)
	bad := false
	for i := 0; i < 100000; i++ {
		if p := svc.getUsableSuccessProxy(context.Background(), 1); p != nil && p.Host != "healthy.example" {
			bad = true
			break
		}
	}
	wg.Wait()
	require.False(t, bad, "old success must never authorize the new unmeasured snapshot")
}

func TestProxyAutoProbeUnavailableDoesNotConnectDirectly(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	proxyAddress, err := url.Parse(server.URL)
	require.NoError(t, err)
	proxyPort, err := strconv.Atoi(proxyAddress.Port())
	require.NoError(t, err)
	previous := GetDefaultProxyAutoProbeService()
	t.Cleanup(func() { SetDefaultProxyAutoProbeService(previous) })
	for _, tc := range []struct {
		name  string
		svc   *ProxyAutoProbeService
		proxy *Proxy
	}{
		{name: "service absent"},
		{name: "cold unknown", svc: NewProxyAutoProbeService(nil, nil, nil, nil)},
		{name: "disabled with stale hydrated proxy", svc: NewProxyAutoProbeService(nil, nil, nil, nil), proxy: &Proxy{ID: 1, Status: StatusActive, Protocol: "http", Host: proxyAddress.Hostname(), Port: proxyPort}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits.Store(0)
			if tc.svc != nil && tc.name == "cold unknown" {
				tc.svc.config.Enabled = true
			}
			SetDefaultProxyAutoProbeService(tc.svc)
			a := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}, Proxy: tc.proxy}
			client, err := httpclient.GetClient(httpclient.Options{ProxyURL: resolveAccountProxyURL(context.Background(), a, nil)})
			if err == nil {
				res, requestErr := client.Get(server.URL)
				if res != nil {
					res.Body.Close()
				}
				err = requestErr
			}
			require.Error(t, err, "an unavailable automatic proxy must fail before making a direct request")
			require.Equal(t, int64(0), hits.Load())
		})
	}
	client, err := httpclient.GetClient(httpclient.Options{ProxyURL: resolveAccountProxyURL(context.Background(), &Account{ID: 100}, nil)})
	require.NoError(t, err)
	res, err := client.Get(server.URL)
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, int64(1), hits.Load(), "explicit no-proxy accounts retain direct access")
	var proxyHits atomic.Int64
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.IsAbs() && r.URL.Host == "upstream.example" {
			proxyHits.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(proxyURL.Port())
	require.NoError(t, err)
	svc := NewProxyAutoProbeService(nil, nil, nil, nil)
	svc.config.Enabled = true
	svc.entries[9] = &proxyAutoProbeEntry{ProxyID: 9, Queue: ProxyAutoProbeQueueSuccess}
	seedProxyAutoProbeSnapshots(svc, Proxy{ID: 9, Status: StatusActive, Protocol: "http", Host: proxyURL.Hostname(), Port: port})
	SetDefaultProxyAutoProbeService(svc)
	a := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}}
	client, err = httpclient.GetClient(httpclient.Options{ProxyURL: resolveAccountProxyURL(context.Background(), a, nil)})
	require.NoError(t, err)
	res, err = client.Get("http://upstream.example/model")
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, int64(1), proxyHits.Load(), "a healthy automatic candidate still receives the actual request")
	require.Equal(t, int64(1), hits.Load())
	require.Nil(t, a.ProxyID, "selection does not persist an automatic binding")
}

func TestProxyAutoProbeLateMeasurementDoesNotBlessNewGeneration(t *testing.T) {
	ctx := context.Background()
	old := time.Now().Add(-time.Minute)
	current := time.Now()
	svc := NewProxyAutoProbeService(autoProbeHealthAdmin{qualityEpoch: &old}, nil, nil, nil)
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueFailed}
	seedProxyAutoProbeSnapshots(svc, Proxy{ID: 1, Status: StatusActive, nodeUpdatedAt: current})
	outcome := svc.probeProxy(ctx, 1)
	svc.finishProbe(1, outcome, current)
	require.Nil(t, svc.getBestProxy(ctx), "a probe of the old installed node cannot authorize the new generation")
	status := svc.GetStatus()
	require.False(t, status.RecentCompletions[0].Success, "do not advertise an obsolete probe as healthy")
	svc.adminService = autoProbeHealthAdmin{qualityEpoch: &current}
	svc.finishProbe(1, svc.probeProxy(ctx, 1), current)
	require.NotNil(t, svc.getBestProxy(ctx), "a successful probe of the current generation is usable")
}

func TestClearAutoSelectedProxyCanceledEOFDoesNotRejectHealthyNode(t *testing.T) {
	previous := GetDefaultProxyAutoProbeService()
	svc := NewProxyAutoProbeService(nil, nil, nil, nil, &proxyStickyStoreStub{})
	svc.config = ProxyAutoProbeConfig{Enabled: true, StickyEnabled: true, StickyTTLSeconds: 60}
	fast, slow := int64(5), int64(20)
	svc.entries[1] = &proxyAutoProbeEntry{ProxyID: 1, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &fast}
	svc.entries[2] = &proxyAutoProbeEntry{ProxyID: 2, Queue: ProxyAutoProbeQueueSuccess, LastLatencyMs: &slow}
	seedProxyAutoProbeSnapshots(svc, Proxy{ID: 1, Status: StatusActive}, Proxy{ID: 2, Status: StatusActive})
	SetDefaultProxyAutoProbeService(svc)
	t.Cleanup(func() { svc.Stop(); SetDefaultProxyAutoProbeService(previous) })
	a := &Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}}
	require.Equal(t, int64(1), resolveAccountProxy(context.Background(), a, nil).ID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ClearAutoSelectedProxyStickyOnTransportError(ctx, a, io.EOF)
	require.Equal(t, int64(1), resolveAccountProxy(context.Background(), a, nil).ID, "a canceled request's EOF must not evict its healthy proxy")
	ClearAutoSelectedProxyStickyOnTransportError(context.Background(), a, io.EOF)
	require.Equal(t, int64(2), resolveAccountProxy(context.Background(), a, nil).ID, "a live request's transport failure still switches away from the failed proxy")
}

func TestProxyAutoProbeUnavailableOpsDoesNotClaimDirectConnection(t *testing.T) {
	id, name := opsUpstreamProxyAttribution(&Account{ID: 100, Extra: map[string]any{"auto_select_proxy": true}})
	require.Nil(t, id)
	require.NotEqual(t, opsProxyNameDirect, name, "automatic routing without a candidate does not use the direct egress")
	_, directName := opsUpstreamProxyAttribution(&Account{ID: 100})
	require.Equal(t, opsProxyNameDirect, directName)
}
