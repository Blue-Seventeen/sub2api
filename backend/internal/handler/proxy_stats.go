package handler

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func gatewayProxyStatsRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if requestID, ok := ctx.Value(ctxkey.ClientRequestID).(string); ok && strings.TrimSpace(requestID) != "" {
		return "client:" + strings.TrimSpace(requestID)
	}
	if requestID, ok := ctx.Value(ctxkey.RequestID).(string); ok && strings.TrimSpace(requestID) != "" {
		return "local:" + strings.TrimSpace(requestID)
	}
	return ""
}

func recordGatewayProxyFailureStat(
	ctx context.Context,
	gatewayService *service.GatewayService,
	proxyStatsWorkerPool *service.ProxyStatsWorkerPool,
	account *service.Account,
	apiKey *service.APIKey,
	durationMs int64,
	logKey string,
) {
	if gatewayService == nil || proxyStatsWorkerPool == nil {
		return
	}
	accountSnapshot := proxyStatsAccountSnapshot(account)
	if accountSnapshot == nil {
		return
	}
	apiKeySnapshot := proxyStatsAPIKeySnapshot(apiKey)
	requestID := gatewayProxyStatsRequestID(ctx)
	if durationMs < 0 {
		durationMs = 0
	}
	proxyStatsWorkerPool.Submit(func(taskCtx context.Context) {
		gatewayService.RecordProxyRequestStat(taskCtx, accountSnapshot, apiKeySnapshot, false, durationMs, requestID, logKey)
	})
}

func recordOpenAIProxyFailureStat(
	ctx context.Context,
	gatewayService *service.OpenAIGatewayService,
	proxyStatsWorkerPool *service.ProxyStatsWorkerPool,
	account *service.Account,
	apiKey *service.APIKey,
	durationMs int64,
	logKey string,
) {
	if gatewayService == nil || proxyStatsWorkerPool == nil {
		return
	}
	accountSnapshot := proxyStatsAccountSnapshot(account)
	if accountSnapshot == nil {
		return
	}
	apiKeySnapshot := proxyStatsAPIKeySnapshot(apiKey)
	requestID := gatewayProxyStatsRequestID(ctx)
	if durationMs < 0 {
		durationMs = 0
	}
	proxyStatsWorkerPool.Submit(func(taskCtx context.Context) {
		gatewayService.RecordProxyRequestStat(taskCtx, accountSnapshot, apiKeySnapshot, false, durationMs, requestID, logKey)
	})
}

func proxyStatsAccountSnapshot(account *service.Account) *service.Account {
	if account == nil || account.ID <= 0 {
		return nil
	}
	effectiveProxyID := account.EffectiveProxyID()
	if effectiveProxyID == nil || *effectiveProxyID <= 0 {
		return nil
	}
	proxyID := *effectiveProxyID
	return &service.Account{
		ID:      account.ID,
		ProxyID: &proxyID,
	}
}

func proxyStatsAPIKeySnapshot(apiKey *service.APIKey) *service.APIKey {
	if apiKey == nil || apiKey.ID <= 0 {
		return nil
	}
	return &service.APIKey{ID: apiKey.ID}
}

func elapsedMillisSince(start time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	durationMs := time.Since(start).Milliseconds()
	if durationMs < 0 {
		return 0
	}
	return durationMs
}

func beginProxyActiveUsage(tracker *service.ProxyActiveUsageTracker, account *service.Account) *service.ProxyActiveUsageHandle {
	if tracker == nil || account == nil {
		return nil
	}
	return tracker.Begin(account)
}

func endProxyActiveUsage(handle *service.ProxyActiveUsageHandle) {
	if handle != nil {
		handle.End()
	}
}
