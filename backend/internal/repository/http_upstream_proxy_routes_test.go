package repository

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// This uses the real HTTP transport and Redis Lua store against only loopback
// fixtures. The account intentionally has no preselected/bound proxy.
func TestHTTPUpstreamActualProxyRoutesAndConcurrentStreams(t *testing.T) {
	store, _ := newProxyActiveUsageRedisStore(t)
	tracker := service.NewProxyActiveUsageTrackerWithOptions(store, service.ProxyActiveUsageTrackerOptions{WorkerCount: 1, HeartbeatInterval: 10 * time.Millisecond})
	t.Cleanup(tracker.Stop)
	release := make(chan struct{})
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: fixture\n\n")
		require.NoError(t, http.NewResponseController(w).Flush())
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(proxy.Close)
	u, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	host, portRaw, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)
	port, err := strconv.Atoi(portRaw)
	require.NoError(t, err)
	node := &service.Proxy{ID: 71, Name: "isolated-proxy", Protocol: "http", Host: host, Port: port}
	account := &service.Account{ID: 52}
	require.Nil(t, account.Proxy)
	upstream := NewHTTPUpstream(nil)
	var bodies []io.ReadCloser
	for i := 0; i < 3; i++ {
		ctx, observation := service.NewProxyRouteObservation(t.Context(), tracker)
		t.Cleanup(observation.Close)
		raw := service.ResolveProxyURL(ctx, node)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://fixture.invalid/responses", nil)
		require.NoError(t, err)
		resp, err := upstream.DoWithTLS(req, raw, account.ID, 3, &tlsfingerprint.Profile{Name: "plain-http"})
		require.NoError(t, err)
		bodies = append(bodies, resp.Body)
		require.Equal(t, []service.ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 71, Name: "isolated-proxy", Host: host, Port: port}}, service.ProxyRoutesFromContext(ctx))
	}
	require.Eventually(t, func() bool {
		v := tracker.GetActiveUsageDetails(context.Background(), []int64{71})[71]
		return v.AccountCount != nil && *v.AccountCount == 1 && v.RequestCount != nil && *v.RequestCount == 3
	}, 3*time.Second, 10*time.Millisecond)
	close(release)
	for _, body := range bodies {
		_, err := io.ReadAll(body)
		require.NoError(t, err)
		require.NoError(t, body.Close())
	}
	require.Eventually(t, func() bool {
		v := tracker.GetActiveUsageDetails(context.Background(), []int64{71})[71]
		return v.RequestCount != nil && *v.RequestCount == 0 && v.AccountCount != nil && *v.AccountCount == 0
	}, 3*time.Second, 10*time.Millisecond)
}

func TestHTTPUpstreamDirectAndFailedAttemptAttribution(t *testing.T) {
	ctx, observation := service.NewProxyRouteObservation(t.Context(), nil)
	defer observation.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	upstream := NewHTTPUpstream(nil)
	resp, err := upstream.Do(req, "", 52, 1)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, []service.ProxyRouteSnapshot{{Kind: "direct"}}, service.ProxyRoutesFromContext(ctx))
	service.ResetProxyRouteUsage(ctx)
	raw := service.ResolveProxyURL(ctx, &service.Proxy{ID: 72, Protocol: "http", Host: "127.0.0.1", Port: 1})
	_, err = upstream.Do(req, raw, 52, 1)
	require.Error(t, err)
	routes := service.ProxyRoutesFromContext(ctx)
	require.Len(t, routes, 1)
	require.Equal(t, int64(72), routes[0].ProxyID, "a failed actual dial is still an attempted proxy route")
}

func TestHTTPUpstreamProxyLeaseFollowsActualTransportLifetime(t *testing.T) {
	for _, mode := range []string{"detached-close", "detached-eof", "ordinary-cancel"} {
		t.Run(mode, func(t *testing.T) {
			store, _ := newProxyActiveUsageRedisStore(t)
			tracker := service.NewProxyActiveUsageTrackerWithOptions(store, service.ProxyActiveUsageTrackerOptions{WorkerCount: 1, HeartbeatInterval: 10 * time.Millisecond})
			t.Cleanup(tracker.Stop)
			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseStream := func() { releaseOnce.Do(func() { close(release) }) }
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: opening\n\n")
				require.NoError(t, http.NewResponseController(w).Flush())
				select {
				case <-release:
					_, _ = io.WriteString(w, "data: final-usage\n\n")
				case <-r.Context().Done():
				}
			}))
			t.Cleanup(proxy.Close)
			t.Cleanup(releaseStream)
			u, err := url.Parse(proxy.URL)
			require.NoError(t, err)
			host, portRaw, err := net.SplitHostPort(u.Host)
			require.NoError(t, err)
			port, err := strconv.Atoi(portRaw)
			require.NoError(t, err)
			parent, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			ctx, observation := service.NewProxyRouteObservation(parent, tracker)
			t.Cleanup(observation.Close)
			raw := service.ResolveProxyURL(ctx, &service.Proxy{ID: 73, Name: "drain-proxy", Protocol: "http", Host: host, Port: port})
			transportCtx := ctx
			if mode != "ordinary-cancel" {
				transportCtx = context.WithoutCancel(ctx)
			}
			req, err := http.NewRequestWithContext(transportCtx, http.MethodGet, "http://fixture.invalid/responses", nil)
			require.NoError(t, err)
			resp, err := NewHTTPUpstream(nil).DoWithTLS(req, raw, 52, 1, &tlsfingerprint.Profile{Name: "plain-http"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = resp.Body.Close() })
			requestCount := func() int64 {
				details := tracker.GetActiveUsageDetails(context.Background(), []int64{73})[73]
				if details.RequestCount == nil {
					return -1
				}
				return *details.RequestCount
			}
			require.Eventually(t, func() bool { return requestCount() == 1 }, 3*time.Second, 10*time.Millisecond)
			routes := service.ProxyRoutesFromContext(ctx)
			cancel()
			if mode == "ordinary-cancel" {
				// A normal cancelled transport must release without requiring the
				// caller to consume or close its response body first.
				require.Eventually(t, func() bool { return requestCount() == 0 }, 3*time.Second, 10*time.Millisecond)
			} else {
				require.Never(t, func() bool { return requestCount() != 1 }, 150*time.Millisecond, 10*time.Millisecond)
				if mode == "detached-eof" {
					releaseStream()
					body, err := io.ReadAll(resp.Body)
					require.NoError(t, err)
					require.Contains(t, string(body), "final-usage")
				} else {
					require.NoError(t, resp.Body.Close())
				}
				require.Eventually(t, func() bool { return requestCount() == 0 }, 3*time.Second, 10*time.Millisecond)
			}
			require.Equal(t, routes, service.ProxyRoutesFromContext(ctx))
			require.NoError(t, resp.Body.Close())
		})
	}
}
