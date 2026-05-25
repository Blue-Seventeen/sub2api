package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type proxyStatsRepository struct {
	sql sqlExecutor
}

func NewProxyStatsRepository(sqlDB *sql.DB) service.ProxyStatsRepository {
	return newProxyStatsRepositoryWithSQL(sqlDB)
}

func newProxyStatsRepositoryWithSQL(sqlq sqlExecutor) *proxyStatsRepository {
	return &proxyStatsRepository{sql: sqlq}
}

func (r *proxyStatsRepository) Record(ctx context.Context, stat *service.ProxyRequestStat) error {
	if r == nil || r.sql == nil || stat == nil || stat.ProxyID <= 0 || stat.AccountID <= 0 {
		return nil
	}
	createdAt := stat.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	requestID := strings.TrimSpace(stat.RequestID)
	if requestID == "" || stat.APIKeyID == nil || *stat.APIKeyID <= 0 {
		_, err := r.sql.ExecContext(ctx, `
			INSERT INTO proxy_request_stats (
				proxy_id, account_id, api_key_id, request_id, success, duration_ms, created_at
			) VALUES ($1, $2, $3, NULL, $4, $5, $6)
		`, stat.ProxyID, stat.AccountID, stat.APIKeyID, stat.Success, stat.DurationMs, createdAt)
		return err
	}
	_, err := r.sql.ExecContext(ctx, `
		INSERT INTO proxy_request_stats (
			proxy_id, account_id, api_key_id, request_id, success, duration_ms, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, stat.ProxyID, stat.AccountID, stat.APIKeyID, requestID, stat.Success, stat.DurationMs, createdAt)
	return err
}

func (r *proxyStatsRepository) GetStats(ctx context.Context, proxyID int64) (*service.ProxyStats, error) {
	out := &service.ProxyStats{}
	if r == nil || r.sql == nil || proxyID <= 0 {
		return out, nil
	}
	if err := scanSingleRow(ctx, r.sql, `
		SELECT
			COUNT(*) AS total_accounts,
			COUNT(*) FILTER (WHERE status = 'active') AS active_accounts
		FROM accounts
		WHERE proxy_id = $1 AND deleted_at IS NULL
	`, []any{proxyID}, &out.TotalAccounts, &out.ActiveAccounts); err != nil {
		return nil, err
	}

	var successCount int64
	var avgLatency sql.NullFloat64
	if err := scanSingleRow(ctx, r.sql, `
		SELECT
			COUNT(*) AS total_requests,
			COUNT(*) FILTER (WHERE success) AS success_count,
			AVG(duration_ms)::float8 AS average_latency
		FROM proxy_request_stats
		WHERE proxy_id = $1
	`, []any{proxyID}, &out.TotalRequests, &successCount, &avgLatency); err != nil {
		return nil, err
	}
	if out.TotalRequests > 0 {
		out.SuccessRate = float64(successCount) * 100 / float64(out.TotalRequests)
	}
	if avgLatency.Valid {
		out.AverageLatency = int64(avgLatency.Float64 + 0.5)
	}
	return out, nil
}
