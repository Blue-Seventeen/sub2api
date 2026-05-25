//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyStatsRepository_GetStatsAggregatesCurrentAccountsAndRequestSnapshots(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	proxyRepo := newProxyRepositoryWithSQL(tx.Client(), tx)
	statsRepo := newProxyStatsRepositoryWithSQL(tx)

	proxy1 := &service.Proxy{Name: "stats-p1", Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: service.StatusActive}
	proxy2 := &service.Proxy{Name: "stats-p2", Protocol: "socks5", Host: "127.0.0.1", Port: 8080, Status: service.StatusActive}
	proxy3 := &service.Proxy{Name: "stats-p3", Protocol: "https", Host: "127.0.0.1", Port: 8443, Status: service.StatusActive}
	require.NoError(t, proxyRepo.Create(ctx, proxy1))
	require.NoError(t, proxyRepo.Create(ctx, proxy2))
	require.NoError(t, proxyRepo.Create(ctx, proxy3))

	account1 := insertProxyStatsAccount(t, ctx, tx, "stats-a1", proxy1.ID, service.StatusActive)
	_ = insertProxyStatsAccount(t, ctx, tx, "stats-a2", proxy1.ID, service.StatusDisabled)
	_ = insertProxyStatsAccount(t, ctx, tx, "stats-a3", proxy2.ID, service.StatusActive)

	require.NoError(t, statsRepo.Record(ctx, &service.ProxyRequestStat{
		ProxyID:    proxy1.ID,
		AccountID:  account1,
		Success:    true,
		DurationMs: 100,
		CreatedAt:  time.Now(),
	}))
	require.NoError(t, statsRepo.Record(ctx, &service.ProxyRequestStat{
		ProxyID:    proxy1.ID,
		AccountID:  account1,
		Success:    false,
		DurationMs: 300,
		CreatedAt:  time.Now(),
	}))

	stats, err := statsRepo.GetStats(ctx, proxy1.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.TotalAccounts)
	require.Equal(t, int64(1), stats.ActiveAccounts)
	require.Equal(t, int64(2), stats.TotalRequests)
	require.Equal(t, 50.0, stats.SuccessRate)
	require.Equal(t, int64(200), stats.AverageLatency)

	_, err = tx.ExecContext(ctx, "UPDATE accounts SET proxy_id = $1 WHERE id = $2", proxy2.ID, account1)
	require.NoError(t, err)

	stats, err = statsRepo.GetStats(ctx, proxy1.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalAccounts)
	require.Equal(t, int64(0), stats.ActiveAccounts)
	require.Equal(t, int64(2), stats.TotalRequests, "request history remains attributed to the proxy snapshot")

	proxy2Stats, err := statsRepo.GetStats(ctx, proxy2.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), proxy2Stats.TotalAccounts)
	require.Equal(t, int64(2), proxy2Stats.ActiveAccounts)
	require.Equal(t, int64(0), proxy2Stats.TotalRequests, "current account assignment must not backfill request history")

	emptyStats, err := statsRepo.GetStats(ctx, proxy3.ID)
	require.NoError(t, err)
	require.Equal(t, &service.ProxyStats{}, emptyStats)
}

func TestProxyStatsRepository_RecordAllowsRepeatedRequestIDs(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	statsRepo := newProxyStatsRepositoryWithSQL(tx)
	apiKeyID := int64(7)

	stat := &service.ProxyRequestStat{
		ProxyID:    1,
		AccountID:  2,
		APIKeyID:   &apiKeyID,
		RequestID:  "same-request-id",
		Success:    true,
		DurationMs: 10,
		CreatedAt:  time.Now(),
	}
	require.NoError(t, statsRepo.Record(ctx, stat))
	stat.AccountID = 3
	stat.Success = false
	require.NoError(t, statsRepo.Record(ctx, stat))

	var count int64
	require.NoError(t, scanSingleRow(ctx, tx, "SELECT COUNT(*) FROM proxy_request_stats WHERE request_id = $1 AND api_key_id = $2", []any{stat.RequestID, apiKeyID}, &count))
	require.Equal(t, int64(2), count)
}

func insertProxyStatsAccount(t *testing.T, ctx context.Context, tx sqlExecutor, name string, proxyID int64, status string) int64 {
	t.Helper()

	var id int64
	err := scanSingleRow(
		ctx,
		tx,
		"INSERT INTO accounts (name, platform, type, proxy_id, status) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		[]any{name, service.PlatformAnthropic, service.AccountTypeOAuth, proxyID, status},
		&id,
	)
	require.NoError(t, err)
	return id
}
