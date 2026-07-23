package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type proxyStatsHandlerRepoStub struct {
	mu    sync.Mutex
	calls int
	last  *service.ProxyRequestStat
}

func (s *proxyStatsHandlerRepoStub) Record(ctx context.Context, stat *service.ProxyRequestStat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	copied := *stat
	s.last = &copied
	return nil
}

func (s *proxyStatsHandlerRepoStub) GetStats(ctx context.Context, proxyID int64) (*service.ProxyStats, error) {
	return &service.ProxyStats{}, nil
}

func (s *proxyStatsHandlerRepoStub) snapshot() (int, *service.ProxyRequestStat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.last
}

func TestGatewayProxyStatsRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, " local-1 ")
	ctx = context.WithValue(ctx, ctxkey.ClientRequestID, " client-1 ")
	require.Equal(t, "client:client-1", gatewayProxyStatsRequestID(ctx))

	localOnly := context.WithValue(context.Background(), ctxkey.RequestID, " local-2 ")
	require.Equal(t, "local:local-2", gatewayProxyStatsRequestID(localOnly))
	require.Empty(t, gatewayProxyStatsRequestID(context.Background()))
	require.Empty(t, gatewayProxyStatsRequestID(context.TODO()))
}

func TestRecordGatewayProxyFailureStatSubmitsAsyncTask(t *testing.T) {
	repo := &proxyStatsHandlerRepoStub{}
	svc := service.NewGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, repo, nil,
	)
	pool := service.NewProxyStatsWorkerPoolWithOptions(service.ProxyStatsWorkerPoolOptions{
		WorkerCount: 1,
		QueueSize:   2,
		TaskTimeout: time.Second,
	})
	t.Cleanup(pool.Stop)
	proxyID := int64(42)
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, " req-42 ")

	recordGatewayProxyFailureStat(ctx, svc, pool, &service.Account{ID: 7, ProxyID: &proxyID}, &service.APIKey{ID: 9}, -10, "test")

	require.Eventually(t, func() bool {
		calls, _ := repo.snapshot()
		return calls == 1
	}, time.Second, 10*time.Millisecond)
	_, stat := repo.snapshot()
	require.NotNil(t, stat)
	require.Equal(t, proxyID, stat.ProxyID)
	require.Equal(t, int64(7), stat.AccountID)
	require.NotNil(t, stat.APIKeyID)
	require.Equal(t, int64(9), *stat.APIKeyID)
	require.Equal(t, "client:req-42", stat.RequestID)
	require.False(t, stat.Success)
	require.Zero(t, stat.DurationMs)
}

func TestRecordOpenAIProxyFailureStatSubmitsAsyncTask(t *testing.T) {
	repo := &proxyStatsHandlerRepoStub{}
	svc := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, repo, nil,
	)
	pool := service.NewProxyStatsWorkerPoolWithOptions(service.ProxyStatsWorkerPoolOptions{
		WorkerCount: 1,
		QueueSize:   2,
		TaskTimeout: time.Second,
	})
	t.Cleanup(pool.Stop)
	proxyID := int64(43)
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, " local-43 ")

	recordOpenAIProxyFailureStat(ctx, svc, pool, &service.Account{ID: 8, ProxyID: &proxyID}, &service.APIKey{ID: 10}, 25, "test")

	require.Eventually(t, func() bool {
		calls, _ := repo.snapshot()
		return calls == 1
	}, time.Second, 10*time.Millisecond)
	_, stat := repo.snapshot()
	require.NotNil(t, stat)
	require.Equal(t, proxyID, stat.ProxyID)
	require.Equal(t, int64(8), stat.AccountID)
	require.NotNil(t, stat.APIKeyID)
	require.Equal(t, int64(10), *stat.APIKeyID)
	require.Equal(t, "local:local-43", stat.RequestID)
	require.False(t, stat.Success)
	require.Equal(t, int64(25), stat.DurationMs)
}

func TestRecordGatewayProxyFailureStatQueueFullDoesNotRunSynchronously(t *testing.T) {
	repo := &proxyStatsHandlerRepoStub{}
	svc := service.NewGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, repo, nil,
	)
	pool := service.NewProxyStatsWorkerPoolWithOptions(service.ProxyStatsWorkerPoolOptions{
		WorkerCount: 1,
		QueueSize:   1,
		TaskTimeout: 5 * time.Second,
	})
	t.Cleanup(pool.Stop)

	release := make(chan struct{})
	started := make(chan struct{})
	require.Equal(t, "enqueued", pool.Submit(func(ctx context.Context) {
		close(started)
		<-release
	}))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocking task not started")
	}
	queuedRelease := make(chan struct{})
	require.Equal(t, "enqueued", pool.Submit(func(ctx context.Context) {
		<-queuedRelease
	}))

	proxyID := int64(42)
	recordGatewayProxyFailureStat(context.Background(), svc, pool, &service.Account{ID: 7, ProxyID: &proxyID}, &service.APIKey{ID: 9}, 15, "test")
	calls, _ := repo.snapshot()
	require.Zero(t, calls, "queue-full failure stat must be dropped instead of sync fallback")
	require.GreaterOrEqual(t, pool.Stats().DroppedQueueFull, uint64(1))

	close(release)
	close(queuedRelease)
}
