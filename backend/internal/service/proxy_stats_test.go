//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type proxyStatsRepoStub struct {
	err        error
	stats      *ProxyStats
	calls      int
	last       *ProxyRequestStat
	lastCtxErr error
}

func (s *proxyStatsRepoStub) Record(ctx context.Context, stat *ProxyRequestStat) error {
	s.calls++
	if ctx != nil {
		s.lastCtxErr = ctx.Err()
	}
	copied := *stat
	s.last = &copied
	return s.err
}

func (s *proxyStatsRepoStub) GetStats(ctx context.Context, proxyID int64) (*ProxyStats, error) {
	if s.stats != nil {
		return s.stats, nil
	}
	return &ProxyStats{}, nil
}

func TestRecordProxyRequestStat_SkipsAccountsWithoutProxy(t *testing.T) {
	repo := &proxyStatsRepoStub{}

	recordProxyRequestStat(context.Background(), repo, &Account{ID: 10}, &APIKey{ID: 20}, true, 123, "req-1", "test")

	require.Zero(t, repo.calls)
}

func TestRecordProxyRequestStat_UsesRuntimeProxySnapshot(t *testing.T) {
	repo := &proxyStatsRepoStub{}

	recordProxyRequestStat(context.Background(), repo, &Account{ID: 10, Proxy: &Proxy{ID: 77}}, nil, true, 123, "req-1", "test")

	require.Equal(t, 1, repo.calls)
	require.NotNil(t, repo.last)
	require.Equal(t, int64(77), repo.last.ProxyID)
}

func TestRecordProxyRequestStat_BestEffortErrorDoesNotPanic(t *testing.T) {
	proxyID := int64(99)
	repo := &proxyStatsRepoStub{err: errors.New("write failed")}

	require.NotPanics(t, func() {
		recordProxyRequestStat(context.Background(), repo, &Account{ID: 10, ProxyID: &proxyID}, &APIKey{ID: 20}, false, -1, " req-1 ", "test")
	})

	require.Equal(t, 1, repo.calls)
	require.NotNil(t, repo.last)
	require.Equal(t, proxyID, repo.last.ProxyID)
	require.Equal(t, int64(10), repo.last.AccountID)
	require.NotNil(t, repo.last.APIKeyID)
	require.Equal(t, int64(20), *repo.last.APIKeyID)
	require.Equal(t, "req-1", repo.last.RequestID)
	require.False(t, repo.last.Success)
	require.Zero(t, repo.last.DurationMs)
}

func TestRecordProxyRequestStat_RespectsCallerCancellation(t *testing.T) {
	proxyID := int64(99)
	repo := &proxyStatsRepoStub{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	recordProxyRequestStat(ctx, repo, &Account{ID: 10, ProxyID: &proxyID}, nil, false, 1, "req-1", "test")

	require.Equal(t, 1, repo.calls)
	require.ErrorIs(t, repo.lastCtxErr, context.Canceled)
}

func TestGatewayServiceRecordUsage_RecordsProxyStatsBeforeBillingError(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{err: errors.New("billing unavailable")}
	proxyStatsRepo := &proxyStatsRepoStub{}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	svc.proxyStatsRepo = proxyStatsRepo
	proxyID := int64(88)

	err := svc.RecordUsage(context.Background(), &RecordUsageInput{
		Result: &ForwardResult{
			RequestID: "gateway_proxy_stat_billing_error",
			Usage:     ClaudeUsage{InputTokens: 10, OutputTokens: 5},
			Model:     "claude-sonnet-4",
		},
		APIKey:  &APIKey{ID: 501},
		User:    &User{ID: 601},
		Account: &Account{ID: 701, ProxyID: &proxyID},
	})

	require.Error(t, err)
	require.Equal(t, 1, proxyStatsRepo.calls)
	require.True(t, proxyStatsRepo.last.Success)
	require.Equal(t, proxyID, proxyStatsRepo.last.ProxyID)
	require.Equal(t, "gateway_proxy_stat_billing_error", proxyStatsRepo.last.RequestID)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, "gateway_proxy_stat_billing_error:billing_failed", usageRepo.lastLog.RequestID)
	require.Zero(t, usageRepo.lastLog.InputTokens)
	require.Zero(t, usageRepo.lastLog.OutputTokens)
}
