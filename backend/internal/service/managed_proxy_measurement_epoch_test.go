package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestManagedProxyNodeAdminSameSecondOldQualityIsNotRevived(t *testing.T) {
	svc, states := managedProxyNodeAdminFixture()
	epoch := time.Unix(1800000000, 800000000)
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: epoch}
	latency, score, checked := int64(20), 100, epoch.Unix()
	svc.proxyLatencyCache = &managedProxyNodeLatencyCache{values: map[int64]*ProxyLatencyInfo{1: {
		Success: true, LatencyMs: &latency, QualityScore: &score, QualityGrade: "A", QualityCheckedAt: &checked, UpdatedAt: epoch.Add(time.Second),
	}}}
	out, err := svc.GetProxySnapshots(context.Background(), []int64{1})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Nil(t, out[0].QualityScore, "second precision cannot prove that quality used the new configuration")
	require.Empty(t, out[0].QualityGrade)
}

func TestManagedProxyNodeAdminFreshLatencyMergeKeepsOldQualityEpoch(t *testing.T) {
	svc, states := managedProxyNodeAdminFixture()
	epoch := time.Unix(1800000000, 800000000)
	previous := epoch.Add(-500 * time.Millisecond)
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: epoch}
	latency, score, checked := int64(20), 100, previous.Unix()
	cache := &managedProxyNodeLatencyCache{values: map[int64]*ProxyLatencyInfo{1: {Success: true, QualityScore: &score, QualityGrade: "A", QualityCheckedAt: &checked, NodeEpoch: &previous, QualityNodeEpoch: &previous, UpdatedAt: epoch}}}
	svc.proxyLatencyCache = cache
	svc.saveProxyLatency(context.Background(), 1, &ProxyLatencyInfo{Success: true, LatencyMs: &latency, NodeEpoch: &epoch, UpdatedAt: epoch.Add(time.Second)})
	require.True(t, cache.values[1].QualityNodeEpoch.Equal(previous), "latency refresh cannot replace the quality check's configuration source")
	out, err := svc.GetProxySnapshots(context.Background(), []int64{1})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Equal(t, "success", out[0].LatencyStatus)
	require.Nil(t, out[0].QualityScore)
	require.Empty(t, out[0].QualityGrade)
}

func TestManagedProxyNodeAdminUnchangedUnavailableProbeKeepsUnavailableResult(t *testing.T) {
	for _, status := range []string{ProxySubscriptionNodeStatusInactive, ProxySubscriptionNodeStatusSourceMissing} {
		t.Run(status, func(t *testing.T) {
			svc, states := managedProxyNodeAdminFixture()
			states.states[1] = ProxySubscriptionNode{Status: status, UpdatedAt: time.Now()}
			svc.managedProxyResolver = measurementEpochResolver{}
			prober := &measurementEpochProber{started: make(chan struct{}), release: make(chan struct{})}
			svc.proxyProber = prober
			result, err := svc.TestProxy(context.Background(), 1)
			require.NoError(t, err, "an unchanged unavailable node did not change configuration during a probe")
			require.False(t, result.Success)
			require.Contains(t, result.Message, "unavailable")
			quality, err := svc.CheckProxyQuality(context.Background(), 1)
			require.NoError(t, err)
			require.Greater(t, quality.FailedCount, 0)
			require.Contains(t, quality.Items[0].Message, "unavailable")
			select {
			case <-prober.started:
				t.Fatal("an unavailable node reached the network prober")
			default:
			}
		})
	}
}

type pausedNodePickerResolver struct {
	managedProxyNodeRunningResolver
	status string
}

func (r pausedNodePickerResolver) GetStatus(id int64) ManagedProxyRuntimeStatus {
	return ManagedProxyRuntimeStatus{Enabled: true, Status: r.status, SubscriptionID: id}
}

func TestManagedProxyNodeAdminPickerExcludesPausedNodesKeepsLegacy(t *testing.T) {
	for _, status := range []string{ManagedProxyRuntimeStatusStopped, ManagedProxyRuntimeStatusDisabled, ManagedProxyRuntimeStatusError} {
		t.Run(status, func(t *testing.T) {
			svc, _ := managedProxyNodeAdminFixture()
			svc.managedProxyResolver = pausedNodePickerResolver{status: status}
			proxies, err := svc.GetAllProxies(context.Background())
			require.NoError(t, err)
			var ids []int64
			for _, p := range proxies {
				ids = append(ids, p.ID)
			}
			require.Equal(t, []int64{4, 5}, ids, "paused node-bound subscriptions cannot be offered as usable proxies")
			counted, err := svc.GetAllProxiesWithAccountCount(context.Background())
			require.NoError(t, err)
			ids = nil
			for _, p := range counted {
				ids = append(ids, p.ID)
			}
			require.Equal(t, []int64{4, 5}, ids)
		})
	}
}

func TestManagedProxyNodeResolverRejectsOperatorDisableBeforeReload(t *testing.T) {
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{Enabled: true, MihomoBinaryPath: "mihomo", BindHost: "127.0.0.1", WorkDir: t.TempDir()}, nil, &capturingManagedProxyRunner{})
	sub := managedProxyRuntimeTestSubscription(7, 1)
	id := int64(1)
	sub.Nodes[0].ProxyID = &id
	require.NoError(t, runtime.startEntry(context.Background(), sub))
	t.Cleanup(runtime.Stop)
	for _, status := range []string{"inactive", "expired"} {
		proxy := &Proxy{ID: id, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &sub.ID, Username: "mpu_hk", Password: "mpp_hk", Status: status}
		value, err := runtime.ResolveProxyURLForProxy(context.Background(), proxy)
		require.ErrorIs(t, err, ErrManagedProxyNodeUnavailable, "operator disable must be enforced while old installed auth still exists")
		require.Empty(t, value)
	}
}

func TestManagedProxyNodeAdminRejectsEpochNotInstalledInRunningProcess(t *testing.T) {
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{Enabled: true, MihomoBinaryPath: "mihomo", BindHost: "127.0.0.1", WorkDir: t.TempDir()}, nil, &capturingManagedProxyRunner{})
	sub := managedProxyRuntimeTestSubscription(7, 1)
	id := int64(1)
	sub.Nodes[0].ProxyID = &id
	sub.Nodes[0].UpdatedAt = time.Now().Add(-time.Minute)
	require.NoError(t, runtime.startEntry(context.Background(), sub))
	t.Cleanup(runtime.Stop)
	svc, states := managedProxyNodeAdminFixture()
	repo, ok := svc.proxyRepo.(*managedProxyNodeAdminRepo)
	require.True(t, ok, "fixture must use the managed proxy node admin repository")
	repo.proxies[0].Username, repo.proxies[0].Password = "mpu_hk", "mpp_hk"
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: time.Now()}
	svc.managedProxyResolver = runtime
	prober := &measurementEpochProber{started: make(chan struct{}), release: make(chan struct{})}
	close(prober.release)
	svc.proxyProber = prober
	_, err := svc.TestProxy(context.Background(), 1)
	require.ErrorContains(t, err, "configuration changed", "a running old generation cannot measure the new DB epoch")
	select {
	case <-prober.started:
		t.Fatal("probe was sent through a stale process")
	default:
	}
}

type qualityEpochResolver struct {
	managedProxyNodeRunningResolver
	url string
}

func (r qualityEpochResolver) ResolveProxyURL(context.Context, int64) (string, error) {
	return r.url, nil
}

func TestManagedProxyNodeAdminInFlightQualityCannotDescribeRotatedNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	previous := proxyQualityTargets
	proxyQualityTargets = []proxyQualityTarget{{Target: "local", URL: "http://quality.example/check", Method: http.MethodGet, AllowedStatuses: map[int]struct{}{http.StatusOK: {}}}}
	defer func() { proxyQualityTargets = previous }()
	svc, states := managedProxyNodeAdminFixture()
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: time.Now().Add(-time.Minute)}
	prober := &measurementEpochProber{started: make(chan struct{}), release: make(chan struct{})}
	svc.proxyProber = prober
	svc.managedProxyResolver = qualityEpochResolver{url: server.URL}
	svc.proxyLatencyCache = &managedProxyNodeLatencyCache{values: make(map[int64]*ProxyLatencyInfo)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := svc.CheckProxyQuality(ctx, 1); done <- err }()
	select {
	case <-prober.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: time.Now()}
	close(prober.release)
	require.ErrorContains(t, <-done, "configuration changed")
	out, err := svc.GetProxySnapshots(ctx, []int64{1})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Nil(t, out[0].QualityScore)
	require.Empty(t, out[0].QualityGrade)
}

func TestManagedProxyNodeAdminFreshQualityHasPreciseEpoch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	previous := proxyQualityTargets
	proxyQualityTargets = []proxyQualityTarget{{Target: "local", URL: "http://quality.example/check", Method: http.MethodGet, AllowedStatuses: map[int]struct{}{http.StatusOK: {}}}}
	defer func() { proxyQualityTargets = previous }()
	svc, states := managedProxyNodeAdminFixture()
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: time.Now()}
	prober := &measurementEpochProber{started: make(chan struct{}), release: make(chan struct{})}
	close(prober.release)
	svc.proxyProber = prober
	svc.managedProxyResolver = qualityEpochResolver{url: server.URL}
	svc.proxyLatencyCache = &managedProxyNodeLatencyCache{values: make(map[int64]*ProxyLatencyInfo)}
	result, err := svc.CheckProxyQuality(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "A", result.Grade)
	out, err := svc.GetProxySnapshots(context.Background(), []int64{1})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Equal(t, "A", out[0].QualityGrade, "fresh same-second quality must retain its independently captured epoch")
	require.Equal(t, "success", out[0].LatencyStatus)
}

type measurementEpochResolver struct {
	managedProxyNodeRunningResolver
}

func (measurementEpochResolver) ResolveProxyURL(context.Context, int64) (string, error) {
	return "socks5h://127.0.0.1:1080", nil
}

type measurementEpochProber struct{ started, release chan struct{} }

func (p *measurementEpochProber) ProbeProxy(ctx context.Context, _ string) (*ProxyExitInfo, int64, error) {
	close(p.started)
	select {
	case <-p.release:
		return &ProxyExitInfo{IP: "198.51.100.9"}, 25, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

func TestManagedProxyNodeAdminInFlightLatencyCannotDescribeRotatedNode(t *testing.T) {
	svc, states := managedProxyNodeAdminFixture()
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: time.Now().Add(-time.Minute)}
	prober := &measurementEpochProber{started: make(chan struct{}), release: make(chan struct{})}
	svc.proxyProber = prober
	svc.managedProxyResolver = measurementEpochResolver{}
	svc.proxyLatencyCache = &managedProxyNodeLatencyCache{values: make(map[int64]*ProxyLatencyInfo)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := svc.TestProxy(ctx, 1); done <- err }()
	select {
	case <-prober.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: time.Now()}
	close(prober.release)
	require.ErrorContains(t, <-done, "configuration changed", "an old successful result must not be returned to the caller")
	out, err := svc.GetProxySnapshots(ctx, []int64{1})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Empty(t, out[0].LatencyStatus, "completion after rotation does not make an old probe current")
	require.Empty(t, out[0].IPAddress)
}
