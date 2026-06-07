package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func unavailableRedisClient(t *testing.T) *redis.Client {
	t.Helper()

	server, err := miniredis.Run()
	require.NoError(t, err)
	addr := server.Addr()
	server.Close()

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  10 * time.Millisecond,
		ReadTimeout:  10 * time.Millisecond,
		WriteTimeout: 10 * time.Millisecond,
		MaxRetries:   0,
	})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func advisoryLockDBExpectedIfFallbackRuns(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	mock.ExpectQuery("SELECT pg_try_advisory_lock").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func requireAdvisoryLockFallbackNotUsed(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	require.Error(t, mock.ExpectationsWereMet(), "DB advisory fallback should remain unused when Redis is configured but unavailable")
}

func TestOpsCleanupLeaderLockSkipsOnRedisErrorWithoutDBFallback(t *testing.T) {
	db, mock := advisoryLockDBExpectedIfFallbackRuns(t)
	svc := &OpsCleanupService{
		db:          db,
		redisClient: unavailableRedisClient(t),
		instanceID:  "test-cleanup",
	}

	release, ok := svc.tryAcquireLeaderLock(context.Background())
	require.False(t, ok)
	require.Nil(t, release)
	requireAdvisoryLockFallbackNotUsed(t, mock)
}

func TestOpsAggregationLeaderLockSkipsOnRedisErrorWithoutDBFallback(t *testing.T) {
	db, mock := advisoryLockDBExpectedIfFallbackRuns(t)
	svc := &OpsAggregationService{
		db:          db,
		redisClient: unavailableRedisClient(t),
		instanceID:  "test-aggregation",
	}

	release, ok := svc.tryAcquireLeaderLock(context.Background(), "ops:test:aggregation:leader", time.Minute, "[OpsAggregation][test]")
	require.False(t, ok)
	require.Nil(t, release)
	requireAdvisoryLockFallbackNotUsed(t, mock)
}

func TestOpsMetricsCollectorLeaderLockSkipsOnRedisErrorWithoutDBFallback(t *testing.T) {
	db, mock := advisoryLockDBExpectedIfFallbackRuns(t)
	collector := &OpsMetricsCollector{
		db:          db,
		redisClient: unavailableRedisClient(t),
		instanceID:  "test-metrics",
	}

	release, ok := collector.tryAcquireLeaderLock(context.Background())
	require.False(t, ok)
	require.Nil(t, release)
	requireAdvisoryLockFallbackNotUsed(t, mock)
}
