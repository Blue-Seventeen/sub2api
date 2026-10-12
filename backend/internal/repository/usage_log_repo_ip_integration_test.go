//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageIPFilterAllAggregates(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "ip-own@test.com"})
	other := mustCreateUser(t, client, &service.User{Email: "ip-other@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-ip-own", Name: "ip-own"})
	account := mustCreateAccount(t, client, &service.Account{Name: "ip-account"})
	now := time.Now().UTC()
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	for i, ip := range []string{"192.0.2.1", "2001:0DB8::1", "192.0.2.2", "", "198.51.100.1", "192.0.2.1", "192.0.2.1"} {
		uid := user.ID
		if i == 4 {
			uid = other.ID
		}
		created := now
		if i == 5 {
			created = now.Add(-2 * time.Hour)
		}
		model := "ip-model"
		if i == 6 {
			model = "other-model"
		}
		_, err := repo.Create(ctx, &service.UsageLog{UserID: uid, APIKeyID: key.ID, AccountID: account.ID, RequestID: string(rune('a' + i)), Model: model, RequestedModel: model, IPAddress: &ip, CreatedAt: created, InputTokens: 10, OutputTokens: 2, ActualCost: 1, TotalCost: 2, InboundEndpoint: ptrString("/v1/messages"), UpstreamEndpoint: ptrString("/v1/responses")})
		require.NoError(t, err)
	}
	filters := usagestats.UsageLogFilters{UserID: user.ID, StartTime: &start, EndTime: &end, Model: "ip-model", IPAddresses: []string{"192.0.2.1", "2001:0DB8::1"}}
	rows, page, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 1}, filters)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 2, page.Total)
	stats, err := repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.EqualValues(t, 2, stats.TotalRequests)
	require.EqualValues(t, 24, stats.TotalTokens)
	for _, points := range [][]usagestats.EndpointStat{stats.Endpoints, stats.UpstreamEndpoints, stats.EndpointPaths} {
		require.Len(t, points, 1)
		require.EqualValues(t, 2, points[0].Requests)
	}
	trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "day", filters)
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.EqualValues(t, 2, trend[0].Requests)
	for _, source := range []string{"requested", "upstream", "mapping"} {
		modelFilters := filters
		if source == usagestats.ModelSourceMapping {
			modelFilters.Model = "ip-model -> ip-model"
		}
		models, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, modelFilters, source)
		require.NoError(t, err)
		require.Len(t, models, 1)
		require.EqualValues(t, 2, models[0].Requests)
	}
	groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, filters)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.EqualValues(t, 2, groups[0].Requests)
	breakdown, err := repo.GetUserBreakdownStats(ctx, start, end, usagestats.UserBreakdownDimension{UserID: user.ID, Model: "ip-model", ModelType: usagestats.ModelSourceRequested, IPAddresses: filters.IPAddresses}, 50)
	require.NoError(t, err)
	require.Len(t, breakdown, 1)
	require.EqualValues(t, 2, breakdown[0].Requests)
	type optionsRepo interface {
		GetIPOptionsWithFilters(context.Context, usagestats.UsageLogFilters, string, int, int) (*usagestats.UsageIPOptions, error)
	}
	optionRepo, ok := any(repo).(optionsRepo)
	require.True(t, ok, "candidate capability missing")
	options, err := optionRepo.GetIPOptionsWithFilters(ctx, filters, "", 0, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"192.0.2.1"}, options.IPs)
	require.True(t, options.HasMore)
	options, err = optionRepo.GetIPOptionsWithFilters(ctx, filters, "", 1, 100)
	require.NoError(t, err)
	require.Equal(t, []string{"192.0.2.2", "2001:0DB8::1"}, options.IPs)
	require.False(t, options.HasMore)
	options, err = optionRepo.GetIPOptionsWithFilters(ctx, filters, "%", 0, 100)
	require.NoError(t, err)
	require.Empty(t, options.IPs)
	options, err = optionRepo.GetIPOptionsWithFilters(ctx, filters, "2001:", 0, 100)
	require.NoError(t, err)
	require.Equal(t, []string{"2001:0DB8::1"}, options.IPs)

	baseFilters := filters
	trueValue := true
	requestType := int16(service.RequestTypeWSV2)
	billingType := int8(service.BillingTypeSubscription)
	for _, tc := range []struct {
		name   string
		change func(*usagestats.UsageLogFilters)
	}{
		{"user", func(f *usagestats.UsageLogFilters) { f.UserID = other.ID }},
		{"api_key", func(f *usagestats.UsageLogFilters) { f.APIKeyID = key.ID + 100 }},
		{"account", func(f *usagestats.UsageLogFilters) { f.AccountID = account.ID + 100 }},
		{"group", func(f *usagestats.UsageLogFilters) { f.GroupID = 100 }},
		{"model", func(f *usagestats.UsageLogFilters) { f.Model = "no-model" }},
		{"request_type", func(f *usagestats.UsageLogFilters) { f.RequestType = &requestType }},
		{"stream", func(f *usagestats.UsageLogFilters) { f.Stream = &trueValue }},
		{"native_compaction", func(f *usagestats.UsageLogFilters) { f.NativeCompactionV2 = &trueValue }},
		{"billing_type", func(f *usagestats.UsageLogFilters) { f.BillingType = &billingType }},
		{"billing_mode", func(f *usagestats.UsageLogFilters) { f.BillingMode = "per_request" }},
		{"mismatch", func(f *usagestats.UsageLogFilters) { f.UpstreamModelMismatch = &trueValue }},
	} {
		t.Run("and_"+tc.name, func(t *testing.T) {
			f := baseFilters
			tc.change(&f)
			logs, p, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, f)
			require.NoError(t, err)
			require.Empty(t, logs)
			require.Zero(t, p.Total)
			total, err := repo.GetStatsWithFilters(ctx, f)
			require.NoError(t, err)
			require.Zero(t, total.TotalRequests)
			for _, points := range [][]usagestats.EndpointStat{total.Endpoints, total.UpstreamEndpoints, total.EndpointPaths} {
				require.Empty(t, points)
			}
			trend, err := repo.GetUsageTrendWithUsageFilters(ctx, start, end, "day", f)
			require.NoError(t, err)
			require.Empty(t, trend)
			models, err := repo.GetModelStatsWithUsageFiltersBySource(ctx, start, end, f, "requested")
			require.NoError(t, err)
			require.Empty(t, models)
			groups, err := repo.GetGroupStatsWithUsageFilters(ctx, start, end, f)
			require.NoError(t, err)
			require.Empty(t, groups)
			options, err := optionRepo.GetIPOptionsWithFilters(ctx, f, "", 0, 100)
			require.NoError(t, err)
			// The other user has its own distinct IP; candidate lookup excludes only the current IP selection.
			if tc.name == "user" {
				require.Equal(t, []string{"198.51.100.1"}, options.IPs)
			} else {
				require.Empty(t, options.IPs)
			}
		})
	}
	// An IP-only query must bypass aggregate tables that cannot represent the IP dimension.
	_, err = tx.ExecContext(ctx, "INSERT INTO usage_dashboard_daily (bucket_date,total_requests) VALUES ($1::date,999)", now)
	require.NoError(t, err)
	ipOnly := usagestats.UsageLogFilters{IPAddresses: []string{"2001:0DB8::1"}}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	trend, err = repo.GetUsageTrendWithUsageFilters(ctx, dayStart, dayStart.AddDate(0, 0, 1), "day", ipOnly)
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.EqualValues(t, 1, trend[0].Requests)
	filters.IPAddresses = []string{"2001:db8::1"}
	stats, err = repo.GetStatsWithFilters(ctx, filters)
	require.NoError(t, err)
	require.Zero(t, stats.TotalRequests, "must preserve exact stored spelling")
}
func ptrString(v string) *string { return &v }
