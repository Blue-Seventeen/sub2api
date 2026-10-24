package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheLiveProxyRoutesMergeWithoutChangingControllerOrTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache, ok := NewGatewayCache(client).(*gatewayCache)
	require.True(t, ok)
	first := service.ProxyRouteSnapshot{Kind: "proxy", ProxyID: 1, Name: "first", Host: "proxy.test", Port: 8080}
	record := &service.LiveCallRecord{CallID: "call_routes", CallHash: "routes", Controller: service.LiveControllerProxy, ControllerOwner: "owner", ProxyRoutes: []service.ProxyRouteSnapshot{first}}
	require.NoError(t, cache.SaveLiveCall(context.Background(), record, time.Hour))
	ttl := server.TTL(liveCallKey(record.CallHash))
	var wg sync.WaitGroup
	for _, id := range []int64{2, 3, 2, 3} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			err := cache.AppendLiveCallProxyRoutes(context.Background(), record.CallHash, []service.ProxyRouteSnapshot{{Kind: "proxy", ProxyID: id, Host: "next.test", Port: 1080}})
			require.NoError(t, err)
		}(id)
	}
	wg.Wait()
	loaded, err := cache.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Len(t, loaded.ProxyRoutes, 3)
	require.Equal(t, first, loaded.ProxyRoutes[0])
	require.Equal(t, service.LiveControllerProxy, loaded.Controller)
	require.Equal(t, "owner", loaded.ControllerOwner)
	require.Equal(t, ttl, server.TTL(liveCallKey(record.CallHash)))
	err = cache.AppendLiveCallProxyRoutes(context.Background(), "missing", []service.ProxyRouteSnapshot{first})
	require.ErrorIs(t, err, service.ErrLiveCallNotFound)
	require.False(t, server.Exists(liveCallKey("missing")))
}
