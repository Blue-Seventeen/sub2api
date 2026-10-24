package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
	"time"
)

func TestWebSearchActualProxyRouteAndLease(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
	defer tracker.Stop()
	ctx, o := NewProxyRouteObservation(t.Context(), tracker)
	defer o.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	id := int64(73)
	account := &Account{ID: 52, ProxyID: &id, Proxy: &Proxy{ID: 73, Name: "search-node", Protocol: "http", Host: "127.0.0.1", Port: port}}
	previous := getWebSearchManager()
	SetWebSearchManager(websearch.NewManager([]websearch.ProviderConfig{{Type: "brave", APIKey: "fixture"}}, nil))
	defer SetWebSearchManager(previous)
	_, _, err = doWebSearch(ctx, account, "fixture query")
	require.Error(t, err, "the only outbound target is an intentionally closed loopback proxy")
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 73, Name: "search-node", Host: "127.0.0.1", Port: port}}, ProxyRoutesFromContext(ctx))
	waitForProxyActiveUsageCount(t, tracker, 73, 0)
}

func TestWebSearchProviderUsesLogicalRouteMetadata(t *testing.T) {
	ctx, o := NewProxyRouteObservation(t.Context(), nil)
	defer o.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	listenerURL := (&Proxy{Protocol: "http", Host: "127.0.0.1", Port: port}).URL()
	previous := getWebSearchManager()
	SetWebSearchManager(websearch.NewManager([]websearch.ProviderConfig{{Type: "brave", APIKey: "fixture", ProxyID: 74, ProxyURL: listenerURL, ProxyRoute: &websearch.ProxyRouteMetadata{ID: 74, Name: "managed-search", Host: "2001:db8::74", Port: 1080}}}, nil))
	defer SetWebSearchManager(previous)
	_, _, err = doWebSearch(ctx, &Account{ID: 52}, "fixture query")
	require.Error(t, err)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 74, Name: "managed-search", Host: "2001:db8::74", Port: 1080}}, ProxyRoutesFromContext(ctx))
}
