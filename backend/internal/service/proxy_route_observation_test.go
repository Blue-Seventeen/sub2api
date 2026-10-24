package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"io"
	"strings"
	"testing"
	"time"
)

func TestProxyRouteObservationRecordsOnlyActualAttempts(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	proxy := &Proxy{ID: 17, Name: "IPv6 node", Host: "2001:db8::1", Port: 1080, Protocol: "socks5", Username: "secret-user", Password: "secret-password"}
	raw := ResolveProxyURL(ctx, proxy)
	require.Empty(t, ProxyRoutesFromContext(ctx), "resolving a node is not an actual attempt")
	attempt := BeginProxyRouteAttempt(ctx, 52, raw, false)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 17, Name: "IPv6 node", Host: "2001:db8::1", Port: 1080}}, ProxyRoutesFromContext(ctx))
	proxy.Name = "renamed"
	proxy.Host = "changed"
	attempt.End()
	routes := ProxyRoutesFromContext(ctx)
	data, err := json.Marshal(routes)
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret")
	require.NotContains(t, string(data), "changed")
	require.Equal(t, "[2001:db8::1]:1080", routes[0].Address())
}

func TestProxyRouteObservationRetriesAreOrderedAndFrozen(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	for _, id := range []int64{1, 2, 1} {
		raw := ResolveProxyURL(ctx, &Proxy{ID: id, Name: "node", Host: "proxy.test", Port: int(id), Protocol: "http"})
		BeginProxyRouteAttempt(ctx, 9, raw, false).End()
	}
	routes := ProxyRoutesFromContext(ctx)
	require.Len(t, routes, 2)
	require.Equal(t, int64(1), routes[0].ProxyID)
	require.Equal(t, int64(2), routes[1].ProxyID)
	frozen := WithProxyRoutesSnapshot(context.Background(), routes)
	ResetProxyRouteUsage(ctx)
	BeginProxyRouteAttempt(ctx, 9, "", false).End()
	require.Equal(t, routes, ProxyRoutesFromContext(frozen))
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "direct"}}, ProxyRoutesFromContext(ctx))
}

func TestProxyRouteObservationActiveLeaseBodyAndCancellation(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
	defer tracker.Stop()
	base, cancel := context.WithCancel(context.Background())
	ctx, observation := NewProxyRouteObservation(base, tracker)
	defer observation.Close()
	raw := ResolveProxyURL(ctx, &Proxy{ID: 17, Host: "proxy.test", Port: 1080, Protocol: "socks5"})
	attempt := BeginProxyRouteAttempt(ctx, 52, raw, false)
	waitForProxyActiveUsageCount(t, tracker, 17, 1)
	body := attempt.WrapBody(io.NopCloser(strings.NewReader("ok")))
	_, err := io.ReadAll(body)
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 17, 0)
	require.NoError(t, body.Close())
	BeginProxyRouteAttempt(ctx, 52, raw, false)
	waitForProxyActiveUsageCount(t, tracker, 17, 1)
	cancel()
	waitForProxyActiveUsageCount(t, tracker, 17, 0)
}

func TestProxyRouteObservationNilAndFailedResolutionRemainUnknown(t *testing.T) {
	require.Nil(t, ProxyRoutesFromContext(context.Background()))
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	BeginProxyRouteAttempt(ctx, 1, "auto-proxy://unavailable", false).End()
	require.Empty(t, ProxyRoutesFromContext(ctx))
	BeginProxyRouteAttempt(ctx, 1, "socks5h://127.0.0.1:10001", false).End()
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "unknown"}}, ProxyRoutesFromContext(ctx))
}

func TestProxyRouteObservationSeedsResumedUsageWithoutLease(t *testing.T) {
	ctx, o := NewProxyRouteObservation(t.Context(), nil)
	defer o.Close()
	prior := []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 7, Name: "first", Host: "logical.test", Port: 1080}}
	SeedProxyRouteUsage(ctx, prior)
	prior[0].Name = "mutated"
	SeedProxyRouteUsage(ctx, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 7, Name: "renamed"}, {Kind: "direct"}})
	got := ProxyRoutesFromContext(ctx)
	require.Len(t, got, 2)
	require.Equal(t, "first", got[0].Name)
	require.Equal(t, "direct", got[1].Kind)
}

func TestProxyRouteObservationCancellationKeepsOnlyDetachedAttempts(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: 10 * time.Millisecond})
	t.Cleanup(tracker.Stop)
	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctx, observation := NewProxyRouteObservation(parent, tracker)
	t.Cleanup(observation.Close)
	raw := ResolveProxyURL(ctx, &Proxy{ID: 17, Name: "logical-node", Host: "proxy.test", Port: 1080, Protocol: "socks5"})
	BeginProxyRouteAttempt(ctx, 52, raw, false)
	// Use the real streaming-context helper, including an independent deadline:
	// detecting detached requests by a nil Done channel alone is insufficient.
	detached, release := detachStreamUpstreamContext(ctx, true)
	defer release()
	detached, timeout := context.WithTimeout(detached, time.Minute)
	defer timeout()
	attempt := BeginProxyRouteAttempt(detached, 52, raw, false)
	body := attempt.WrapBody(io.NopCloser(strings.NewReader("usage")))
	requestCount := func() int64 {
		count := tracker.GetActiveUsageDetails(context.Background(), []int64{17})[17].RequestCount
		if count == nil {
			return -1
		}
		return *count
	}
	require.Eventually(t, func() bool { return requestCount() == 2 }, time.Second, 10*time.Millisecond)
	routes := ProxyRoutesFromContext(ctx)
	cancel()
	require.Eventually(t, func() bool { return requestCount() == 1 }, time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return requestCount() != 1 }, 100*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, routes, ProxyRoutesFromContext(ctx))
	require.NoError(t, body.Close())
	require.NoError(t, body.Close())
	waitForProxyActiveUsageCount(t, tracker, 17, 0)
}
