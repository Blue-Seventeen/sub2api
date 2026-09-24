//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLog_UpstreamModelMismatchFilterAndPartialIndex(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "model-audit@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-model-audit", Name: "model-audit"})
	account := mustCreateAccount(t, client, &service.Account{Name: "model-audit-account"})
	now := time.Now().UTC()
	trueValue, falseValue := true, false
	for _, row := range []struct {
		mismatch               *bool
		requested, upstream    string
		response               string
		inbound, outbound      string
		inputTokens, outTokens int
	}{
		{&trueValue, " gpt-5.5 ", " gpt-5.5 ", "gpt-5.4", " /v1/messages ", " /v1/responses ", 1, 1},
		{&falseValue, " ", " ", "gpt-5.5", "/v1/responses", "/v1/chat/completions", 2, 3},
		{nil, "", "", "", " ", "", 4, 5},
	} {
		var upstreamModel, responseModel, inboundEndpoint, upstreamEndpoint *string
		if row.upstream != "" {
			upstreamModel = &row.upstream
		}
		if row.response != "" {
			responseModel = &row.response
		}
		if row.inbound != "" {
			inboundEndpoint = &row.inbound
		}
		if row.outbound != "" {
			upstreamEndpoint = &row.outbound
		}
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			Model: "gpt-5.5", RequestedModel: row.requested, UpstreamModel: upstreamModel,
			InputTokens: row.inputTokens, OutputTokens: row.outTokens,
			UpstreamResponseModel: responseModel, UpstreamModelMismatch: row.mismatch,
			InboundEndpoint: inboundEndpoint, UpstreamEndpoint: upstreamEndpoint,
			CreatedAt: now,
		})
		require.NoError(t, err)
	}

	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	for _, tc := range []struct {
		name                                string
		mismatch                            *bool
		requests, tokens                    int64
		endpoints, upstreamEndpoints, paths []usagestats.EndpointStat
	}{
		{
			name: "mismatched", mismatch: &trueValue, requests: 1, tokens: 2,
			endpoints:         []usagestats.EndpointStat{{Endpoint: "/v1/messages", Requests: 1, TotalTokens: 2}},
			upstreamEndpoints: []usagestats.EndpointStat{{Endpoint: "/v1/responses", Requests: 1, TotalTokens: 2}},
			paths:             []usagestats.EndpointStat{{Endpoint: "/v1/messages -> /v1/responses", Requests: 1, TotalTokens: 2}},
		},
		{
			name: "matched_excludes_null", mismatch: &falseValue, requests: 1, tokens: 5,
			endpoints:         []usagestats.EndpointStat{{Endpoint: "/v1/responses", Requests: 1, TotalTokens: 5}},
			upstreamEndpoints: []usagestats.EndpointStat{{Endpoint: "/v1/chat/completions", Requests: 1, TotalTokens: 5}},
			paths:             []usagestats.EndpointStat{{Endpoint: "/v1/responses -> /v1/chat/completions", Requests: 1, TotalTokens: 5}},
		},
		{
			name: "unfiltered_includes_null", requests: 3, tokens: 16,
			endpoints: []usagestats.EndpointStat{
				{Endpoint: "/v1/messages", Requests: 1, TotalTokens: 2},
				{Endpoint: "/v1/responses", Requests: 1, TotalTokens: 5},
				{Endpoint: "unknown", Requests: 1, TotalTokens: 9},
			},
			upstreamEndpoints: []usagestats.EndpointStat{
				{Endpoint: "/v1/responses", Requests: 1, TotalTokens: 2},
				{Endpoint: "/v1/chat/completions", Requests: 1, TotalTokens: 5},
				{Endpoint: "unknown", Requests: 1, TotalTokens: 9},
			},
			paths: []usagestats.EndpointStat{
				{Endpoint: "/v1/messages -> /v1/responses", Requests: 1, TotalTokens: 2},
				{Endpoint: "/v1/responses -> /v1/chat/completions", Requests: 1, TotalTokens: 5},
				{Endpoint: "unknown -> unknown", Requests: 1, TotalTokens: 9},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Requested/upstream filters retain their TRIM and empty-field fallback
			// semantics while the same mismatch predicate limits every aggregate.
			for _, source := range []string{"", usagestats.ModelSourceRequested, usagestats.ModelSourceUpstream} {
				filters := usagestats.UsageLogFilters{
					UserID: user.ID, StartTime: &start, EndTime: &end,
					Model: "gpt-5.5", ModelFilterSource: source, UpstreamModelMismatch: tc.mismatch,
				}
				stats, err := repo.GetStatsWithFilters(ctx, filters)
				require.NoError(t, err, "model source %q", source)
				require.Equal(t, tc.requests, stats.TotalRequests, "model source %q", source)
				require.Equal(t, tc.tokens, stats.TotalTokens, "model source %q", source)
				require.ElementsMatch(t, tc.endpoints, stats.Endpoints, "model source %q", source)
				require.ElementsMatch(t, tc.upstreamEndpoints, stats.UpstreamEndpoints, "model source %q", source)
				require.ElementsMatch(t, tc.paths, stats.EndpointPaths, "model source %q", source)

				trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "hour", filters)
				require.NoError(t, err, "model source %q", source)
				require.Len(t, trend, 1)
				require.Equal(t, tc.requests, trend[0].Requests, "model source %q", source)
				require.Equal(t, tc.tokens, trend[0].TotalTokens, "model source %q", source)
			}
		})
	}

	_, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off")
	require.NoError(t, err)
	assertPlanUsesIndex := func(query, indexName string, args ...any) {
		rows, queryErr := tx.QueryContext(ctx, query, args...)
		require.NoError(t, queryErr)
		var planLines []string
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			planLines = append(planLines, line)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Contains(t, strings.Join(planLines, "\n"), indexName)
	}
	assertPlanUsesIndex(`
EXPLAIN (COSTS OFF)
SELECT id
FROM usage_logs
WHERE upstream_model_mismatch IS TRUE
ORDER BY created_at DESC, id DESC
LIMIT 100
`, usageLogsUpstreamModelMismatchIndex)
	assertPlanUsesIndex(`
EXPLAIN (COSTS OFF)
SELECT id
FROM usage_logs
WHERE COALESCE(NULLIF(TRIM(requested_model), ''), model) = $1
  AND created_at >= $2 AND created_at < $3
ORDER BY created_at DESC, id DESC
LIMIT 100
`, usageLogsEffectiveRequestedModelIndex, "gpt-5.5", start, end)
	assertPlanUsesIndex(`
EXPLAIN (COSTS OFF)
SELECT id
FROM usage_logs
WHERE COALESCE(NULLIF(TRIM(upstream_model), ''), model) = $1
  AND created_at >= $2 AND created_at < $3
ORDER BY created_at DESC, id DESC
LIMIT 100
`, usageLogsEffectiveUpstreamModelIndex, "gpt-5.5", start, end)
}

func TestUsageLog_GetStatsWithFilters_AggregatesAndEndpoints(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)

	user := mustCreateUser(t, client, &service.User{Email: "stats@test.com"})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-stats-1", Name: "k"})
	account := mustCreateAccount(t, client, &service.Account{Name: "acc-stats"})

	now := time.Now().UTC()
	inboundEndpoint := "/v1/messages"
	upstreamEndpoint := "/v1/responses"
	for i := 0; i < 3; i++ {
		_, err := repo.Create(ctx, &service.UsageLog{
			UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
			Model: "claude-3", InputTokens: 2, OutputTokens: 3,
			CacheCreationTokens: 4, CacheReadTokens: 5,
			TotalCost: 0.5, ActualCost: 0.4, CreatedAt: now,
			InboundEndpoint: &inboundEndpoint, UpstreamEndpoint: &upstreamEndpoint,
		})
		require.NoError(t, err)
	}

	start := now.Add(-1 * time.Hour)
	end := now.Add(1 * time.Hour)
	// 按本测试创建的 user 维度过滤:集成库为共享实例,其它用 testEntClient 的兄弟测试会留下
	// 已提交的 usage_log 行(含零 token 的失败请求),不限定 user 会把它们计入 TotalRequests。
	stats, err := repo.GetStatsWithFilters(ctx, usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end})
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.TotalRequests)
	require.Equal(t, int64(6), stats.TotalInputTokens)
	require.Equal(t, int64(9), stats.TotalOutputTokens)
	require.Equal(t, int64(27), stats.TotalCacheTokens)
	require.Equal(t, int64(12), stats.TotalCacheCreationTokens)
	require.Equal(t, int64(15), stats.TotalCacheReadTokens)
	require.InDelta(t, 1.2, stats.TotalActualCost, 1e-9)
	require.NotEmpty(t, stats.Endpoints)
	require.NotEmpty(t, stats.UpstreamEndpoints)
	require.NotEmpty(t, stats.EndpointPaths)
}
