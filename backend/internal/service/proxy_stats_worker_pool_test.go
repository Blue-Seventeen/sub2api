package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyStatsWorkerPoolSubmitExecutesTask(t *testing.T) {
	pool := NewProxyStatsWorkerPoolWithOptions(ProxyStatsWorkerPoolOptions{
		WorkerCount: 1,
		QueueSize:   4,
		TaskTimeout: time.Second,
	})
	t.Cleanup(pool.Stop)

	done := make(chan struct{})
	mode := pool.Submit(func(ctx context.Context) {
		close(done)
	})

	require.Equal(t, proxyStatsSubmitModeEnqueued, mode)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy stats task not executed")
	}
	require.Eventually(t, func() bool {
		stats := pool.Stats()
		return stats.SubmittedTasks == 1 && stats.CompletedTasks == 1 && stats.SuccessfulTasks == 1
	}, time.Second, 10*time.Millisecond)
}

func TestProxyStatsWorkerPoolSubmitQueueFullDropsWithoutSyncFallback(t *testing.T) {
	pool := NewProxyStatsWorkerPoolWithOptions(ProxyStatsWorkerPoolOptions{
		WorkerCount: 1,
		QueueSize:   1,
		TaskTimeout: 5 * time.Second,
	})
	t.Cleanup(pool.Stop)

	release := make(chan struct{})
	blockingStarted := make(chan struct{})
	require.Equal(t, proxyStatsSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(blockingStarted)
		<-release
	}))
	select {
	case <-blockingStarted:
	case <-time.After(time.Second):
		t.Fatal("blocking task not started")
	}

	queuedRelease := make(chan struct{})
	require.Equal(t, proxyStatsSubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		<-queuedRelease
	}))

	var droppedRan atomic.Bool
	mode := pool.Submit(func(ctx context.Context) {
		droppedRan.Store(true)
	})
	require.Equal(t, proxyStatsSubmitModeDropped, mode)
	require.False(t, droppedRan.Load(), "queue-full submit must not run synchronously")
	require.GreaterOrEqual(t, pool.Stats().DroppedQueueFull, uint64(1))

	close(release)
	close(queuedRelease)
}

func TestProxyStatsWorkerPoolSubmitAfterStopDropsWithoutExecuting(t *testing.T) {
	pool := NewProxyStatsWorkerPoolWithOptions(ProxyStatsWorkerPoolOptions{
		WorkerCount: 1,
		QueueSize:   1,
		TaskTimeout: time.Second,
	})
	pool.Stop()

	var ran atomic.Bool
	mode := pool.Submit(func(ctx context.Context) {
		ran.Store(true)
	})

	require.Equal(t, proxyStatsSubmitModeDropped, mode)
	require.False(t, ran.Load())
	require.Equal(t, uint64(1), pool.Stats().DroppedStopped)
}
