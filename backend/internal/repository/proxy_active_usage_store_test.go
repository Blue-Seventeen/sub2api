package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newProxyActiveUsageRedisStore(t *testing.T) (*RedisProxyActiveUsageStore, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisProxyActiveUsageStore(client), server
}

func TestRedisProxyActiveUsageShortLeaseDoesNotExpireLongSibling(t *testing.T) {
	store, _ := newProxyActiveUsageRedisStore(t)
	ctx := context.Background()
	long := service.ProxyActiveUsageEntry{Token: "long", ProxyID: 10, AccountID: 100}
	short := service.ProxyActiveUsageEntry{Token: "short", ProxyID: 10, AccountID: 100}
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, long, time.Minute))
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, short, 20*time.Millisecond))
	time.Sleep(40 * time.Millisecond)
	counts, err := store.CountProxyActiveAccounts(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, int64(1), counts[10], "short sibling expiration must not hide a live request")
}

func TestRedisProxyActiveUsageCountsRequestsAndAccountsAcrossInstances(t *testing.T) {
	first, server := newProxyActiveUsageRedisStore(t)
	otherClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = otherClient.Close() })
	second := NewRedisProxyActiveUsageStore(otherClient)
	ctx := context.Background()
	entries := []service.ProxyActiveUsageEntry{
		{Token: "a", ProxyID: 10, AccountID: 100},
		{Token: "b", ProxyID: 10, AccountID: 100},
		{Token: "c", ProxyID: 10, AccountID: 100},
		{Token: "d", ProxyID: 10, AccountID: 101},
	}
	for _, entry := range entries[:3] {
		require.NoError(t, first.UpsertProxyActiveUsage(ctx, entry, time.Minute))
	}
	counts, err := second.CountProxyActiveUsage(ctx, []int64{10, 20})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{AccountCount: 1, RequestCount: 3}, counts[10])
	require.Equal(t, service.ProxyActiveUsageCounts{}, counts[20])
	require.NoError(t, second.UpsertProxyActiveUsage(ctx, entries[3], time.Minute))
	require.NoError(t, first.RemoveProxyActiveUsage(ctx, entries[0], time.Minute))
	// Repeated End and a late heartbeat cannot revive the ended token.
	require.NoError(t, first.RemoveProxyActiveUsage(ctx, entries[0], time.Minute))
	require.NoError(t, second.UpsertProxyActiveUsage(ctx, entries[0], time.Minute))
	counts, err = first.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{AccountCount: 2, RequestCount: 3}, counts[10])
	for _, entry := range entries[1:] {
		require.NoError(t, second.RemoveProxyActiveUsage(ctx, entry, time.Minute))
	}
	counts, err = first.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{}, counts[10])
}

func TestRedisProxyActiveUsageHeartbeatKeepsOnlyLiveTokens(t *testing.T) {
	store, server := newProxyActiveUsageRedisStore(t)
	ctx := context.Background()
	start := time.Now()
	server.SetTime(start)
	expired := service.ProxyActiveUsageEntry{Token: "expired", ProxyID: 10, AccountID: 100}
	live := service.ProxyActiveUsageEntry{Token: "live", ProxyID: 10, AccountID: 100}
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, expired, time.Minute))
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, live, time.Minute))
	server.SetTime(start.Add(40 * time.Second))
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, live, time.Minute))
	server.SetTime(start.Add(70 * time.Second))
	counts, err := store.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{AccountCount: 1, RequestCount: 1}, counts[10])
	require.NoError(t, store.RemoveProxyActiveUsage(ctx, live, time.Minute))
	counts, err = store.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{}, counts[10])
}

func TestRedisProxyActiveUsageExpiredProcessClearsCounts(t *testing.T) {
	store, server := newProxyActiveUsageRedisStore(t)
	ctx := context.Background()
	start := time.Now()
	server.SetTime(start)
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, service.ProxyActiveUsageEntry{Token: "dead", ProxyID: 10, AccountID: 100}, time.Minute))
	server.SetTime(start.Add(2 * time.Minute))
	counts, err := store.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{}, counts[10])
	server.FastForward(3 * time.Minute)
	require.Empty(t, server.Keys())
}

func TestRedisProxyActiveUsageEndBeforeBeginDoesNotResurrect(t *testing.T) {
	store, _ := newProxyActiveUsageRedisStore(t)
	ctx := context.Background()
	entry := service.ProxyActiveUsageEntry{Token: "ended-first", ProxyID: 10, AccountID: 100}
	require.NoError(t, store.RemoveProxyActiveUsage(ctx, entry, time.Minute))
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, entry, time.Minute))
	counts, err := store.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{}, counts[10])
}

func TestRedisProxyActiveUsageUnavailableReturnsError(t *testing.T) {
	store, server := newProxyActiveUsageRedisStore(t)
	server.Close()
	_, err := store.CountProxyActiveUsage(context.Background(), []int64{10})
	require.Error(t, err)
}

func TestRedisProxyActiveUsageConcurrentHeartbeatAndEndDoNotResurrect(t *testing.T) {
	store, _ := newProxyActiveUsageRedisStore(t)
	ctx := context.Background()
	var tasks sync.WaitGroup
	errors := make(chan error, 60)
	for i := 0; i < 30; i++ {
		entry := service.ProxyActiveUsageEntry{Token: fmt.Sprintf("request-%d", i), ProxyID: 10, AccountID: int64(100 + i%3)}
		require.NoError(t, store.UpsertProxyActiveUsage(ctx, entry, time.Minute))
		tasks.Add(2)
		go func() {
			defer tasks.Done()
			errors <- store.UpsertProxyActiveUsage(ctx, entry, time.Minute)
		}()
		go func() {
			defer tasks.Done()
			errors <- store.RemoveProxyActiveUsage(ctx, entry, time.Minute)
		}()
	}
	tasks.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	counts, err := store.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{}, counts[10])
}

func TestRedisProxyActiveUsageCoexistsWithLegacySetKeys(t *testing.T) {
	store, server := newProxyActiveUsageRedisStore(t)
	ctx := context.Background()
	_, err := server.SAdd("proxy_active_usage:proxy:10:account:100:requests", "old-request")
	require.NoError(t, err)
	entry := service.ProxyActiveUsageEntry{Token: "new-request", ProxyID: 10, AccountID: 100}
	require.NoError(t, store.UpsertProxyActiveUsage(ctx, entry, time.Minute))
	counts, err := store.CountProxyActiveUsage(ctx, []int64{10})
	require.NoError(t, err)
	require.Equal(t, service.ProxyActiveUsageCounts{AccountCount: 1, RequestCount: 1}, counts[10])
	members, err := server.Members("proxy_active_usage:proxy:10:account:100:requests")
	require.NoError(t, err)
	require.Equal(t, []string{"old-request"}, members)
}

func TestRedisProxyActiveUsageTrackerHeartbeatsKeepLongRequestVisible(t *testing.T) {
	store, _ := newProxyActiveUsageRedisStore(t)
	tracker := service.NewProxyActiveUsageTrackerWithOptions(store, service.ProxyActiveUsageTrackerOptions{
		TTL: 200 * time.Millisecond, HeartbeatInterval: 40 * time.Millisecond,
	})
	defer tracker.Stop()
	handle := tracker.BeginProxy(10, 100)
	require.NotNil(t, handle)
	defer handle.End()
	require.Eventually(t, func() bool {
		counts := tracker.GetActiveUsageDetails(context.Background(), []int64{10})[10]
		return counts.RequestCount != nil && *counts.RequestCount == 1
	}, time.Second, time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	counts := tracker.GetActiveUsageDetails(context.Background(), []int64{10})[10]
	require.Equal(t, "ok", counts.Status)
	require.NotNil(t, counts.AccountCount)
	require.NotNil(t, counts.RequestCount)
	require.Equal(t, int64(1), *counts.AccountCount)
	require.Equal(t, int64(1), *counts.RequestCount)
	handle.End()
	require.Eventually(t, func() bool {
		counts := tracker.GetActiveUsageDetails(context.Background(), []int64{10})[10]
		return counts.RequestCount != nil && *counts.RequestCount == 0 && *counts.AccountCount == 0
	}, time.Second, time.Millisecond)
}
