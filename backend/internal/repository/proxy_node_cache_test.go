package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newProxyNodeRedisClient(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return client, func() {
		_ = client.Close()
		mr.Close()
	}
}

func TestProxyLatencyCacheUsesNodeScopedKeys(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newProxyNodeRedisClient(t)
	defer cleanup()

	nodeALatency := int64(20)
	nodeBLatency := int64(90)
	nodeA := NewProxyLatencyCacheWithNodeID(client, "node-a")
	nodeB := NewProxyLatencyCacheWithNodeID(client, "node-b")

	require.NoError(t, nodeA.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: true, LatencyMs: &nodeALatency}))
	require.NoError(t, nodeB.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: true, LatencyMs: &nodeBLatency}))

	aItems, err := nodeA.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, nodeALatency, *aItems[1].LatencyMs)

	bItems, err := nodeB.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, nodeBLatency, *bItems[1].LatencyMs)

	require.False(t, client.Exists(ctx, proxyLatencyKey(1)).Val() > 0)
}

func TestProxyLatencyCacheFallsBackToLegacyKey(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newProxyNodeRedisClient(t)
	defer cleanup()

	legacyLatency := int64(55)
	payload, err := json.Marshal(&service.ProxyLatencyInfo{Success: true, LatencyMs: &legacyLatency})
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, proxyLatencyKey(2), payload, 0).Err())

	cache := NewProxyLatencyCacheWithNodeID(client, "node-a")
	items, err := cache.GetProxyLatencies(ctx, []int64{2})
	require.NoError(t, err)
	require.Equal(t, legacyLatency, *items[2].LatencyMs)
}

func TestProxyLatencyCachePrefersNodeKeyOverLegacyKey(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newProxyNodeRedisClient(t)
	defer cleanup()

	legacyLatency := int64(99)
	nodeLatency := int64(11)
	legacyPayload, err := json.Marshal(&service.ProxyLatencyInfo{Success: true, LatencyMs: &legacyLatency})
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, proxyLatencyKey(3), legacyPayload, 0).Err())

	cache := NewProxyLatencyCacheWithNodeID(client, "node-a")
	require.NoError(t, cache.SetProxyLatency(ctx, 3, &service.ProxyLatencyInfo{Success: true, LatencyMs: &nodeLatency}))

	items, err := cache.GetProxyLatencies(ctx, []int64{3})
	require.NoError(t, err)
	require.Equal(t, nodeLatency, *items[3].LatencyMs)
}

func TestProxyStickyStoreUsesNodeScopedKeys(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newProxyNodeRedisClient(t)
	defer cleanup()

	nodeA := NewProxyStickyStoreWithNodeID(client, "node-a")
	nodeB := NewProxyStickyStoreWithNodeID(client, "node-b")
	require.NoError(t, nodeA.Set(ctx, 100, 1, time.Minute))
	require.NoError(t, nodeB.Set(ctx, 100, 2, time.Minute))

	aProxy, ok, err := nodeA.Get(ctx, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(1), aProxy)

	bProxy, ok, err := nodeB.Get(ctx, 100)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(2), bProxy)

	require.False(t, client.Exists(ctx, proxyStickyAccountKey(100)).Val() > 0)
}

func TestProxyStickyStoreFallsBackToLegacyKeyAndDeleteRemovesIt(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newProxyNodeRedisClient(t)
	defer cleanup()

	require.NoError(t, client.Set(ctx, proxyStickyAccountKey(101), int64(7), time.Minute).Err())
	store := NewProxyStickyStoreWithNodeID(client, "node-a")

	proxyID, ok, err := store.Get(ctx, 101)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(7), proxyID)

	require.NoError(t, store.DeleteIfMatch(ctx, 101, 7))
	_, ok, err = store.Get(ctx, 101)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProxyStickyStorePrefersNodeKeyOverLegacyKey(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newProxyNodeRedisClient(t)
	defer cleanup()

	require.NoError(t, client.Set(ctx, proxyStickyAccountKey(102), int64(7), time.Minute).Err())
	store := NewProxyStickyStoreWithNodeID(client, "node-a")
	require.NoError(t, store.Set(ctx, 102, 8, time.Minute))

	proxyID, ok, err := store.Get(ctx, 102)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(8), proxyID)
}

func TestProxyStickyStoreDeleteIfMatchDoesNotDeleteOtherNodeKey(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newProxyNodeRedisClient(t)
	defer cleanup()

	nodeA := NewProxyStickyStoreWithNodeID(client, "node-a")
	nodeB := NewProxyStickyStoreWithNodeID(client, "node-b")
	require.NoError(t, nodeA.Set(ctx, 103, 1, time.Minute))
	require.NoError(t, nodeB.Set(ctx, 103, 2, time.Minute))

	require.NoError(t, nodeA.DeleteIfMatch(ctx, 103, 1))

	_, ok, err := nodeA.Get(ctx, 103)
	require.NoError(t, err)
	require.False(t, ok)

	proxyID, ok, err := nodeB.Get(ctx, 103)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(2), proxyID)
}
