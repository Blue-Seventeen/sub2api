package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type ProxyRequestStat struct {
	ProxyID    int64
	AccountID  int64
	APIKeyID   *int64
	RequestID  string
	Success    bool
	DurationMs int64
	CreatedAt  time.Time
}

type ProxyStats struct {
	TotalAccounts  int64
	ActiveAccounts int64
	TotalRequests  int64
	SuccessRate    float64
	AverageLatency int64
}

type ProxyStatsRepository interface {
	Record(ctx context.Context, stat *ProxyRequestStat) error
	GetStats(ctx context.Context, proxyID int64) (*ProxyStats, error)
}

func (s *GatewayService) RecordProxyRequestStat(ctx context.Context, account *Account, apiKey *APIKey, success bool, durationMs int64, requestID string, logKey string) {
	if s == nil {
		return
	}
	recordProxyRequestStat(ctx, s.proxyStatsRepo, account, apiKey, success, durationMs, requestID, logKey)
}

func (s *OpenAIGatewayService) RecordProxyRequestStat(ctx context.Context, account *Account, apiKey *APIKey, success bool, durationMs int64, requestID string, logKey string) {
	if s == nil {
		return
	}
	recordProxyRequestStat(ctx, s.proxyStatsRepo, account, apiKey, success, durationMs, requestID, logKey)
}

func recordProxyRequestStat(ctx context.Context, repo ProxyStatsRepository, account *Account, apiKey *APIKey, success bool, durationMs int64, requestID string, logKey string) {
	if repo == nil || account == nil || account.ID <= 0 {
		return
	}
	proxyID := account.EffectiveProxyID()
	if proxyID == nil || *proxyID <= 0 {
		return
	}
	if durationMs < 0 {
		durationMs = 0
	}
	var apiKeyID *int64
	if apiKey != nil && apiKey.ID > 0 {
		id := apiKey.ID
		apiKeyID = &id
	}
	statCtx, cancel := proxyStatsWriteContext(ctx)
	defer cancel()
	if err := repo.Record(statCtx, &ProxyRequestStat{
		ProxyID:    *proxyID,
		AccountID:  account.ID,
		APIKeyID:   apiKeyID,
		RequestID:  strings.TrimSpace(requestID),
		Success:    success,
		DurationMs: durationMs,
		CreatedAt:  time.Now(),
	}); err != nil {
		if logKey == "" {
			logKey = "service.proxy_stats"
		}
		logger.LegacyPrintf(logKey, "Record proxy request stat failed: %v", err)
	}
}

func proxyStatsWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultProxyStatsTaskTimeout)
}
