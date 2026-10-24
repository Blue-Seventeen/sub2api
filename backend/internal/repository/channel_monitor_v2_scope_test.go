package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func closeChannelMonitorV2TestDB(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) {
	t.Helper()
	t.Cleanup(func() {
		mock.ExpectClose()
		if err := db.Close(); err != nil {
			t.Errorf("close sqlmock database: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("sqlmock cleanup expectations: %v", err)
		}
	})
}

func TestChannelMonitorV2CanonicalPolicyOverridesLegacyInventory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	mock.ExpectQuery(`SELECT g.id`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).AddRow(3, "A", "openai", `{"enabled":true,"models":["GPT-5","gpt-5","gpt-*"]}`, `[]`))
	// An endpoint must derive eligibility from the canonical group policy, even
	// when the legacy per-platform toggle is false and its model list is stale.
	mock.ExpectQuery(`SELECT usage_coverage_start`).WillReturnRows(sqlmock.NewRows([]string{"usage_coverage_start", "error_coverage_start", "data_through", "last_successful_at", "backfill_cursor"}))
	mock.ExpectQuery(`SELECT .*m.platform`).WillReturnRows(sqlmock.NewRows([]string{"bucket", "platform", "group_id", "name", "model", "success", "errors", "affected", "attempts", "input", "output", "create", "read", "ttft_sum", "ttft_count", "duration_sum", "duration_count"}))
	mock.ExpectQuery(`SELECT .*h.platform`).WillReturnRows(sqlmock.NewRows([]string{"bucket", "platform", "group_id", "model", "user_id", "metric", "bound", "count"}))
	repo := &channelMonitorV2Repository{db: db}
	result, err := repo.GetModels(context.Background(), service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{Platforms: []service.ChannelMonitorV2PlatformConfig{{Platform: "openai", Enabled: false, Models: []string{"stale"}}}}, true)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "GPT-5", result.Items[0].Model)
	require.Zero(t, result.Items[0].Metrics.RequestCount)
	require.Equal(t, "unknown", result.Items[0].Health.Overall)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalScopeTuplesAndCatalog(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	mock.ExpectQuery(`SELECT g.id`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).
		AddRow(3, "A", "openai", `{"enabled":true,"models":[" GPT-5 ","gpt-5","gpt-*","no-traffic"]}`, `[]`).
		AddRow(4, "B", "openai", `{"enabled":true,"models":["gpt-4"]}`, `[]`).
		AddRow(5, "disabled", "openai", `{"enabled":false,"models":["gpt-5"]}`, `[]`).
		AddRow(6, "missing", "openai", `{}`, `[]`).
		AddRow(7, "empty", "openai", `{"enabled":true,"models":[" "]}`, `[]`).
		AddRow(8, "composite", "composite", `{"enabled":true,"models":["alias","unrouted"]}`, `[{"public_model":"alias","match_type":"exact","target_platform":"anthropic","priority":1}]`))
	repo := &channelMonitorV2Repository{db: db}
	ctx, filter, cfg, err := repo.resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{})
	require.NoError(t, err)
	require.True(t, channelMonitorV2TupleSelected(ctx, filter, cfg, "openai", 3, "gpt-5"))
	require.False(t, channelMonitorV2TupleSelected(ctx, filter, cfg, "openai", 4, "gpt-5"))
	for _, id := range []int64{0, 5, 6, 7, 99} {
		require.False(t, channelMonitorV2TupleSelected(ctx, filter, cfg, "openai", id, "gpt-5"))
	}
	require.Equal(t, "GPT-5", channelMonitorV2ScopedDisplayModel(ctx, cfg, "openai", 3, "gpt-5"))
	require.Equal(t, "gpt-new", channelMonitorV2ScopedDisplayModel(ctx, cfg, "openai", 3, "gpt-new"))
	scope := channelMonitorV2ScopeFromContext(ctx)
	require.Len(t, scope.groups, 3)
	require.Equal(t, []string{"GPT-5", "no-traffic"}, scope.groups[3].modelsByPlatform["openai"])
	require.Equal(t, []string{"alias"}, scope.groups[8].modelsByPlatform["anthropic"])
	require.Equal(t, []string{"unrouted"}, scope.groups[8].modelsByPlatform["composite"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalScopeIntersectionsNeverWiden(t *testing.T) {
	tests := []struct {
		name   string
		filter service.ChannelMonitorV2Filter
		cfg    service.ChannelMonitorV2Config
		want   []int64
	}{
		{"all", service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{}, []int64{3, 4}},
		{"selected", service.ChannelMonitorV2Filter{GroupIDs: []int64{3}}, service.ChannelMonitorV2Config{}, []int64{3}},
		{"configured", service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{GroupIDs: []int64{4}}, []int64{4}},
		{"user", service.ChannelMonitorV2Filter{RestrictGroups: true, AllowedGroupIDs: []int64{3}}, service.ChannelMonitorV2Config{}, []int64{3}},
		{"empty-user", service.ChannelMonitorV2Filter{RestrictGroups: true}, service.ChannelMonitorV2Config{}, nil},
		{"disjoint", service.ChannelMonitorV2Filter{GroupIDs: []int64{3}}, service.ChannelMonitorV2Config{GroupIDs: []int64{4}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			closeChannelMonitorV2TestDB(t, db, mock)
			if tt.name != "empty-user" && tt.name != "disjoint" {
				mock.ExpectQuery(`SELECT g.id`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).AddRow(3, "A", "openai", `{"enabled":true,"models":["gpt-5"]}`, `[]`).AddRow(4, "B", "openai", `{"enabled":true,"models":["gpt-4"]}`, `[]`))
			}
			repo := &channelMonitorV2Repository{db: db}
			ctx, f, cfg, err := repo.resolveChannelMonitorV2Scope(context.Background(), tt.filter, tt.cfg)
			require.NoError(t, err)
			scope := channelMonitorV2ScopeFromContext(ctx)
			require.Len(t, scope.groups, len(tt.want))
			for _, id := range tt.want {
				require.Contains(t, scope.groups, id)
			}
			if len(tt.want) == 0 {
				where, _ := channelMonitorV2Where(f, cfg, "m")
				require.Contains(t, where, "FALSE")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func canonicalMonitorTestFilter() service.ChannelMonitorV2Filter {
	start := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	return service.ChannelMonitorV2Filter{Start: start, End: start.Add(5 * time.Minute), Bucket: 5 * time.Minute}
}

func expectMonitorCanonicalPolicies(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).
		AddRow(3, "A", "openai", `{"enabled":true,"models":["GPT-5","gpt-*","no-traffic"]}`, `[]`).
		AddRow(4, "B", "openai", `{"enabled":true,"models":["gpt-4"]}`, `[]`).
		AddRow(5, "disabled", "openai", `{"enabled":false,"models":["gpt-5"]}`, `[]`))
}

func expectMonitorCoverage(mock sqlmock.Sqlmock, f service.ChannelMonitorV2Filter) {
	mock.ExpectQuery(`SELECT usage_coverage_start, error_coverage_start, data_through, last_successful_at, backfill_cursor`).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"usage", "errors", "through", "computed", "backfill"}).AddRow(f.Start, f.Start, f.End, f.End, f.Start))
}

func expectMonitorFacts(mock sqlmock.Sqlmock, f service.ChannelMonitorV2Filter) {
	mock.ExpectQuery(`SELECT .*m.platform,m.group_id,COALESCE\(g.name,''\),m.model.*FROM channel_monitor_v2_metrics_rollup`).WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300).WillReturnRows(sqlmock.NewRows([]string{"bucket", "platform", "group", "name", "model", "success", "error", "affected", "attempts", "input", "output", "create", "read", "ttft_sum", "ttft_count", "duration_sum", "duration_count"}).
		AddRow(f.Start, "openai", 3, "A", "gpt-5", 1, 1, 0, 0, 10, 5, 0, 0, 100, 1, 1000, 1).
		AddRow(f.Start, "openai", 4, "B", "gpt-5", 0, 100, 100, 100, 9999, 9999, 999, 999, 900000, 100, 999999, 100).
		AddRow(f.Start, "openai", 4, "B", "gpt-4", 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0).
		AddRow(f.Start, "openai", 3, "A", "gpt-new", 1, 0, 0, 0, 20, 10, 0, 0, 1000, 1, 2000, 1).
		AddRow(f.Start, "openai", 5, "disabled", "gpt-5", 100, 0, 0, 0, 999, 999, 0, 0, 100, 100, 100, 100))
}

func expectMonitorHistograms(mock sqlmock.Sqlmock, f service.ChannelMonitorV2Filter, user bool) {
	q := mock.ExpectQuery(`SELECT .*h.platform,h.group_id,h.model,h.user_id,h.metric,h.upper_bound_ms,SUM\(h.sample_count\) FROM channel_monitor_v2_latency_histograms_rollup`)
	if user {
		q.WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300)
	} else {
		q.WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300, int64(0))
	}
	uid := int64(0)
	if user {
		uid = 1
	}
	q.WillReturnRows(sqlmock.NewRows([]string{"bucket", "platform", "group", "model", "user", "metric", "bound", "count"}).
		AddRow(f.Start, "openai", 3, "gpt-5", uid, "ttft", 100, 1).
		AddRow(f.Start, "openai", 3, "gpt-new", uid, "ttft", 1000, 1).
		AddRow(f.Start, "openai", 4, "gpt-5", int64(2), "ttft", 9000, 100).
		AddRow(f.Start, "openai", 5, "gpt-5", uid, "ttft", 1, 100))
}

func expectMonitorIgnored(mock sqlmock.Sqlmock, f service.ChannelMonitorV2Filter, bucket bool) {
	prefix := `SELECT e.platform, e.group_id, e.model, SUM\(e.error_requests\)`
	cols := []string{"platform", "group", "model", "count"}
	if bucket {
		prefix = `SELECT e.bucket_start, e.platform, e.group_id, e.model, SUM\(e.error_requests\)`
		cols = append([]string{"bucket"}, cols...)
	}
	rows := sqlmock.NewRows(cols)
	if bucket {
		rows.AddRow(f.Start, "openai", 3, "gpt-5", 1).AddRow(f.Start, "openai", 4, "gpt-5", 100)
	} else {
		rows.AddRow("openai", 3, "gpt-5", 1).AddRow("openai", 4, "gpt-5", 100)
	}
	mock.ExpectQuery(prefix).WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300, pq.Array([]string{"authentication"}), service.ChannelMonitorV2TaxonomyVersion).WillReturnRows(rows)
}

func canonicalMonitorTestConfig() service.ChannelMonitorV2Config {
	return service.ChannelMonitorV2Config{Enabled: true, IgnoredErrorCategories: []string{"authentication"}, HealthThresholds: service.DefaultChannelMonitorV2HealthThresholds(), Platforms: []service.ChannelMonitorV2PlatformConfig{{Platform: "openai", Enabled: false, Models: []string{"obsolete"}}}}
}

func TestChannelMonitorV2CanonicalScopeSnapshotTrendAndHistograms(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	f := canonicalMonitorTestFilter()
	expectMonitorCanonicalPolicies(mock)
	expectMonitorCoverage(mock, f)
	expectMonitorFacts(mock, f)
	expectMonitorHistograms(mock, f, false)
	expectMonitorIgnored(mock, f, true)
	result, err := (&channelMonitorV2Repository{db: db}).GetSnapshot(context.Background(), f, canonicalMonitorTestConfig(), true)
	require.NoError(t, err)
	require.Equal(t, int64(4), result.Metrics.RequestCount)
	require.Equal(t, int64(45), result.Metrics.TokenCount)
	require.InDelta(t, 0.25, result.Metrics.ErrorRate, 0.00001) // ignored errors retain the absolute request denominator
	require.Equal(t, int64(2), result.Metrics.TTFT.SampleCount)
	require.Equal(t, int64(100), *result.Metrics.TTFT.P50Ms)
	require.Equal(t, int64(1000), *result.Metrics.TTFT.P95Ms)
	require.Equal(t, "unknown", result.Health.Overall)
	require.Len(t, result.Trend, 1)
	require.True(t, result.Trend[0].Health.Provisional)
	require.Equal(t, int64(50), result.Trend[0].Health.MinimumSample)
	require.NotNil(t, result.Trend[0].Health.Score)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalScopeModelsAndMatrixDoNotShareGroups(t *testing.T) {
	for _, matrix := range []bool{false, true} {
		t.Run(fmt.Sprint(matrix), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			closeChannelMonitorV2TestDB(t, db, mock)
			f := canonicalMonitorTestFilter()
			expectMonitorCanonicalPolicies(mock)
			expectMonitorCoverage(mock, f)
			expectMonitorFacts(mock, f)
			expectMonitorHistograms(mock, f, false)
			expectMonitorIgnored(mock, f, matrix)
			repo := &channelMonitorV2Repository{db: db}
			if matrix {
				result, err := repo.GetMatrix(context.Background(), f, canonicalMonitorTestConfig(), service.ChannelMonitorV2GroupByPlatformGroupModel, true)
				require.NoError(t, err)
				require.Len(t, result.Items, 4)
				for _, r := range result.Items {
					require.NotNil(t, r.GroupID)
					require.Equal(t, "unknown", r.Health.Overall)
					if *r.GroupID == 4 {
						require.Equal(t, "gpt-4", r.Model)
						require.Equal(t, int64(1), r.Metrics.RequestCount)
					}
					if r.Model == "GPT-5" {
						require.Equal(t, int64(3), *r.GroupID)
						require.Equal(t, int64(2), r.Metrics.RequestCount)
						require.Zero(t, r.Metrics.ErrorRate)
					}
					if r.Model == "no-traffic" {
						require.Empty(t, r.Buckets)
						require.Zero(t, r.Metrics.RequestCount)
					} else {
						require.Len(t, r.Buckets, 1)
						require.True(t, r.Buckets[0].Health.Provisional)
					}
				}
			} else {
				result, err := repo.GetModels(context.Background(), f, canonicalMonitorTestConfig(), true)
				require.NoError(t, err)
				require.Len(t, result.Items, 4)
				for _, r := range result.Items {
					require.NotContains(t, r.Model, "*")
					require.NotEqual(t, "obsolete", r.Model)
					if r.Model == "GPT-5" {
						require.Equal(t, int64(2), r.Metrics.RequestCount)
						require.Zero(t, r.Metrics.ErrorRate)
						require.Equal(t, int64(100), *r.Metrics.TTFT.P95Ms)
					}
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelMonitorV2CanonicalConfigWritesIgnoreLegacyPlatforms(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	thresholds, _ := json.Marshal(service.DefaultChannelMonitorV2HealthThresholds())
	query := `UPDATE channel_monitor_v2_config SET version = version \+ 1, enabled = \$1, refresh_interval_seconds = \$2, group_ids = \$3, ignored_error_categories = \$4, health_thresholds = \$5, updated_by = \$6, updated_at = NOW\(\) WHERE id = 1 AND version = \$7 RETURNING`
	mock.ExpectQuery(query).WithArgs(true, 300, pq.Array([]int64{}), pq.Array([]string{}), thresholds, sqlmock.AnyArg(), 1).WillReturnRows(sqlmock.NewRows([]string{"version", "enabled", "refresh", "platforms", "group_ids", "ignored", "health", "updated_at", "updated_by"}).AddRow(2, true, 300, `[{"platform":"openai","enabled":false,"models":["stale"]}]`, `{}`, `{}`, thresholds, now, nil))
	expectMonitorCanonicalPolicies(mock)
	end := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	start := end.Add(-30 * 24 * time.Hour)
	through := end.Add(-23 * time.Hour)
	mock.ExpectQuery(`SELECT usage_coverage_start, error_coverage_start, data_through, last_successful_at, backfill_cursor`).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"usage", "errors", "through", "computed", "backfill"}).AddRow(start, start, through, through, start))
	mock.ExpectQuery(`SELECT DISTINCT m.platform,m.group_id,m.model FROM channel_monitor_v2_metrics_rollup.*m.bucket_seconds IN \(300,3600,43200,86400\)`).WithArgs(start, through, pq.Array([]int64{3, 4})).WillReturnRows(sqlmock.NewRows([]string{"platform", "group", "model"}).AddRow("openai", 3, "gpt-new"))
	result, err := (&channelMonitorV2Repository{db: db}).UpdateConfig(context.Background(), service.ChannelMonitorV2Config{Enabled: true, RefreshIntervalSeconds: 300, GroupIDs: []int64{}, Platforms: []service.ChannelMonitorV2PlatformConfig{{Platform: "openai", Enabled: false, Models: []string{"injected"}}}}, 1)
	require.NoError(t, err)
	require.Len(t, result.Platforms, 1)
	require.True(t, result.Platforms[0].Enabled)
	require.Equal(t, []string{"GPT-5", "no-traffic", "gpt-4", "gpt-new"}, result.Platforms[0].Models)
	require.Empty(t, result.GroupIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalScopeErrorDenominatorsAndAdminSamples(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(fmt.Sprint(admin), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			closeChannelMonitorV2TestDB(t, db, mock)
			f := canonicalMonitorTestFilter()
			expectMonitorCanonicalPolicies(mock)
			expectMonitorCoverage(mock, f)
			mock.ExpectQuery(`SELECT e.platform,e.group_id,e.model,e.error_category,SUM\(e.error_requests\).*GROUP BY e.platform,e.group_id,e.model,e.error_category`).WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300).WillReturnRows(sqlmock.NewRows([]string{"platform", "group", "model", "category", "count"}).AddRow("openai", 3, "gpt-5", "authentication", 1).AddRow("openai", 4, "gpt-4", "timeout", 1).AddRow("openai", 4, "gpt-5", "rate_or_capacity", 100).AddRow("openai", 5, "gpt-5", "internal", 100))
			if admin {
				mock.ExpectQuery(`SELECT .*FROM ops_error_logs current_error.*GROUP BY 1,2,3,4,5,6,7,8,9.*LIMIT 400`).WithArgs(f.Start, f.End, pq.Array([]int64{3, 4})).WillReturnRows(sqlmock.NewRows([]string{"platform", "group", "model", "type", "owner", "source", "status", "upstream", "message", "count"}).
					AddRow("openai", 3, "gpt-5", "upstream_error", "provider", "upstream", 401, 401, "invalid api key", 1).
					AddRow("openai", 4, "gpt-5", "upstream_error", "provider", "upstream", 504, 504, "unauthorized group sample", 100).
					AddRow("openai", 4, "gpt-4", "timeout", "provider", "upstream", 504, 504, "upstream timeout", 1))
			}
			result, err := (&channelMonitorV2Repository{db: db}).GetErrors(context.Background(), f, canonicalMonitorTestConfig(), admin)
			require.NoError(t, err)
			require.Len(t, result.Items, 2)
			for _, row := range result.Items {
				require.Equal(t, int64(1), row.Count)
				require.Equal(t, .5, row.Rate)
				if admin {
					require.Len(t, row.Details, 1)
					require.Equal(t, int64(1), row.Details[0].Count)
					require.NotContains(t, row.Details[0].Message, "unauthorized")
				} else {
					require.Empty(t, row.Details)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestChannelMonitorV2CanonicalScopeRankingKeepsGroupUntilFiltered(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	f := canonicalMonitorTestFilter()
	expectMonitorCanonicalPolicies(mock)
	expectMonitorCoverage(mock, f)
	mock.ExpectQuery(`SELECT m.user_id,COALESCE\(u.email,''\),COALESCE\(u.username,''\),m.platform,m.group_id,m.model,.*GROUP BY m.user_id,u.email,u.username,m.platform,m.group_id,m.model`).WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300).WillReturnRows(sqlmock.NewRows([]string{"user", "email", "name", "platform", "group", "model", "success", "errors", "input", "output", "create", "read", "ttftsum", "ttftcount", "dursum", "durcount"}).
		AddRow(1, "a@example.test", "a", "openai", 3, "gpt-5", 1, 1, 10, 5, 0, 0, 100, 1, 1000, 1).
		AddRow(1, "a@example.test", "a", "openai", 3, "gpt-new", 1, 0, 20, 10, 0, 0, 1000, 1, 2000, 1).
		AddRow(2, "b@example.test", "b", "openai", 4, "gpt-5", 0, 100, 9999, 9999, 999, 999, 900000, 100, 999999, 100).
		AddRow(3, "c@example.test", "c", "openai", 4, "gpt-4", 0, 1, 0, 0, 0, 0, 0, 0, 0, 0))
	expectMonitorHistograms(mock, f, true)
	expectMonitorIgnored(mock, f, true)
	result, err := (&channelMonitorV2Repository{db: db}).GetUsers(context.Background(), f, canonicalMonitorTestConfig(), true)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	require.Equal(t, int64(1), *result.Items[0].UserID)
	require.Equal(t, int64(3), result.Items[0].Metrics.RequestCount)
	require.Equal(t, int64(2), result.Items[0].Metrics.TTFT.SampleCount)
	require.Equal(t, int64(1000), *result.Items[0].Metrics.TTFT.P95Ms)
	require.Equal(t, int64(3), *result.Items[1].UserID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalScopeDimensionsCountOnlyEligibleTuples(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	f := canonicalMonitorTestFilter()
	expectMonitorCanonicalPolicies(mock)
	expectMonitorCoverage(mock, f)
	mock.ExpectQuery(`SELECT m.platform, COALESCE\(g.name, ''\).*GROUP BY m.platform, g.name, g.platform, m.group_id, m.model`).WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300).WillReturnRows(sqlmock.NewRows([]string{"platform", "group_name", "group_platform", "group_id", "model", "count"}).
		AddRow("openai", "A", "openai", 3, "gpt-5", 2).AddRow("openai", "A", "openai", 3, "gpt-new", 1).
		AddRow("openai", "B", "openai", 4, "gpt-4", 1).AddRow("openai", "B", "openai", 4, "gpt-5", 100).
		AddRow("openai", "disabled", "openai", 5, "gpt-5", 100))
	result, err := (&channelMonitorV2Repository{db: db}).GetDimensions(context.Background(), f, canonicalMonitorTestConfig())
	require.NoError(t, err)
	require.Len(t, result.Platforms, 1)
	require.Equal(t, int64(4), result.Platforms[0].RequestCount)
	require.Len(t, result.Groups, 2)
	require.Len(t, result.Models, 4)
	for _, m := range result.Models {
		require.NotContains(t, m.Value, "*")
		if m.Value == "GPT-5" {
			require.Equal(t, int64(2), m.RequestCount)
		}
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalScopeEmptyUsersAndErrorsDoNotQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	f := service.ChannelMonitorV2Filter{RestrictGroups: true}
	repo := &channelMonitorV2Repository{db: db}
	users, err := repo.GetUsers(context.Background(), f, canonicalMonitorTestConfig(), true)
	require.NoError(t, err)
	require.Empty(t, users.Items)
	require.NotNil(t, users.Items)
	errors, err := repo.GetErrors(context.Background(), f, canonicalMonitorTestConfig(), true)
	require.NoError(t, err)
	require.Empty(t, errors.Items)
	require.NotNil(t, errors.Items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalWildcardCaseDedupeAndExactAliasPrecedence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	expectMonitorCanonicalPolicies(mock)
	ctx, f, cfg, err := (&channelMonitorV2Repository{db: db}).resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{})
	require.NoError(t, err)
	require.Equal(t, "gpt-new", channelMonitorV2ScopedDisplayModel(ctx, cfg, "openai", 3, "GPT-NEW"))
	require.Equal(t, "gpt-5-high", channelMonitorV2ScopedDisplayModel(ctx, cfg, "openai", 3, "gpt-5-high")) // not a gateway equivalence for GPT-5
	require.True(t, channelMonitorV2TupleSelected(ctx, f, cfg, "openai", 3, "gpt-5-high"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2MetricHasDataWithoutAbsoluteVolume(t *testing.T) {
	acc := newMetricAccumulator()
	acc.success = 1
	m := acc.metric(5, false)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(b, &wire))
	require.Equal(t, true, wire["has_data"])
	empty := newMetricAccumulator().metric(5, false)
	b, err = json.Marshal(empty)
	require.NoError(t, err)
	wire = nil
	require.NoError(t, json.Unmarshal(b, &wire))
	require.Equal(t, false, wire["has_data"])
}

func TestChannelMonitorV2CanonicalModelFilterDoesNotInventWildcardRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).AddRow(3, "A", "openai", `{"enabled":true,"models":["gpt-*"]}`, `[]`))
	_, f, cfg, err := (&channelMonitorV2Repository{db: db}).resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{Models: []string{"llama"}}, service.ChannelMonitorV2Config{})
	require.NoError(t, err)
	require.Empty(t, configuredChannelMonitorV2Models(cfg, "openai", f))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalDimensionValuesArePublicModelIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	expectMonitorCanonicalPolicies(mock)
	f := canonicalMonitorTestFilter()
	expectMonitorCoverage(mock, f)
	mock.ExpectQuery(`SELECT m.platform, COALESCE\(g.name, ''\)`).WithArgs(f.Start, f.End, pq.Array([]int64{3, 4}), 300).WillReturnRows(sqlmock.NewRows([]string{"platform", "name", "groupplatform", "group", "model", "count"}))
	d, err := (&channelMonitorV2Repository{db: db}).GetDimensions(context.Background(), f, canonicalMonitorTestConfig())
	require.NoError(t, err)
	require.Len(t, d.Models, 3)
	for _, m := range d.Models {
		require.NotContains(t, m.Value, "\x00")
		require.Equal(t, m.Value, m.Label)
		require.Equal(t, "openai", m.Platform)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalAdminDetailsUseEffectiveCompositePlatform(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).AddRow(8, "C", "composite", `{"enabled":true,"models":["alias"]}`, `[{"public_model":"alias","match_type":"exact","target_platform":"openai"}]`))
	f := canonicalMonitorTestFilter()
	f.Platforms = []string{"openai"}
	expectMonitorCoverage(mock, f)
	mock.ExpectQuery(`SELECT e.platform,e.group_id,e.model,e.error_category`).WithArgs(f.Start, f.End, pq.Array([]string{"openai"}), pq.Array([]int64{8}), 300).WillReturnRows(sqlmock.NewRows([]string{"platform", "group", "model", "category", "count"}).AddRow("openai", 8, "alias", "timeout", 1))
	mock.ExpectQuery(`SELECT .*CASE WHEN g.platform = 'composite'.*FROM ops_error_logs current_error LEFT JOIN groups g ON g.id = current_error.group_id LEFT JOIN accounts a ON a.id = current_error.account_id.*CASE WHEN g.platform = 'composite'.*LIMIT 400`).WithArgs(f.Start, f.End, pq.Array([]string{"openai"}), pq.Array([]int64{8})).WillReturnRows(sqlmock.NewRows([]string{"platform", "group", "model", "type", "owner", "source", "status", "upstream", "message", "count"}).AddRow("openai", 8, "alias", "timeout", "provider", "upstream", 504, 504, "timeout", 1))
	result, err := (&channelMonitorV2Repository{db: db}).GetErrors(context.Background(), f, canonicalMonitorTestConfig(), true)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Len(t, result.Items[0].Details, 1)
	require.Equal(t, "openai", result.Items[0].Details[0].Platform)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalPolicyChangesApplyNextRequestWithoutLegacyFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	repo := &channelMonitorV2Repository{db: db}
	for _, policy := range []string{`{"enabled":true,"models":["old"]}`, `{"enabled":true,"models":["new"]}`, `{"enabled":false,"models":["new"]}`, `{}`} {
		mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).AddRow(3, "A", "openai", policy, `[]`))
	}
	legacy := service.ChannelMonitorV2Config{Platforms: []service.ChannelMonitorV2PlatformConfig{{Platform: "openai", Enabled: true, Models: []string{"old", "new"}}}}
	oldCtx, oldFilter, oldCfg, err := repo.resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{}, legacy)
	require.NoError(t, err)
	require.True(t, channelMonitorV2TupleSelected(oldCtx, oldFilter, oldCfg, "openai", 3, "old"))
	require.False(t, channelMonitorV2TupleSelected(oldCtx, oldFilter, oldCfg, "openai", 3, "new"))
	newCtx, newFilter, newCfg, err := repo.resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{}, legacy)
	require.NoError(t, err)
	require.False(t, channelMonitorV2TupleSelected(newCtx, newFilter, newCfg, "openai", 3, "old"))
	require.True(t, channelMonitorV2TupleSelected(newCtx, newFilter, newCfg, "openai", 3, "new"))
	require.Equal(t, []string{"new"}, newCfg.Platforms[0].Models)
	// An in-flight request keeps one consistent snapshot; next requests refresh it.
	require.True(t, channelMonitorV2TupleSelected(oldCtx, oldFilter, oldCfg, "openai", 3, "old"))
	for i := 0; i < 2; i++ {
		ctx, f, cfg, err := repo.resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{}, legacy)
		require.NoError(t, err)
		require.Empty(t, cfg.Platforms)
		require.False(t, channelMonitorV2TupleSelected(ctx, f, cfg, "openai", 3, "new"))
		require.True(t, channelMonitorV2RestrictedGroupScopeEmpty(f, cfg))
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CanonicalCandidatesUseGatewayAllowlistEquivalences(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "models_list_config", "routes"}).AddRow(3, "A", "gemini", `{"enabled":true,"models":["gemini-*","gemini-2.5-pro"]}`, `[]`).AddRow(4, "B", "anthropic", `{"enabled":true,"models":["claude-sonnet-4"]}`, `[]`).AddRow(5, "C", "openai", `{"enabled":true,"models":["gpt-*","gpt-5.5"]}`, `[]`))
	ctx, f, cfg, err := (&channelMonitorV2Repository{db: db}).resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{})
	require.NoError(t, err)
	require.True(t, channelMonitorV2TupleSelected(ctx, f, cfg, "gemini", 3, "models/gemini-2.5-pro"))
	require.Equal(t, "gemini-2.5-pro", channelMonitorV2ScopedDisplayModel(ctx, cfg, "gemini", 3, "models/gemini-2.5-pro"))
	require.True(t, channelMonitorV2TupleSelected(ctx, f, cfg, "anthropic", 4, "claude-sonnet-4-thinking"))
	require.Equal(t, "claude-sonnet-4", channelMonitorV2ScopedDisplayModel(ctx, cfg, "anthropic", 4, "claude-sonnet-4-thinking"))
	require.False(t, channelMonitorV2TupleSelected(ctx, f, cfg, "gemini", 4, "models/gemini-2.5-pro"))
	require.True(t, channelMonitorV2TupleSelected(ctx, f, cfg, "openai", 5, "gpt-5.5-codex-high"))
	require.Equal(t, "gpt-5.5", channelMonitorV2ScopedDisplayModel(ctx, cfg, "openai", 5, "gpt-5.5-codex-high"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2ReviewExactAliasesStayOwnedAcrossEndpoints(t *testing.T) {
	for _, selected := range []bool{false, true} {
		for _, endpoint := range []string{"models", "dimensions", "matrix"} {
			t.Run(fmt.Sprintf("%s/selected=%v", endpoint, selected), func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				closeChannelMonitorV2TestDB(t, db, mock)
				f := canonicalMonitorTestFilter()
				ids := []int64{3, 4}
				if selected {
					f.GroupIDs = []int64{4}
					ids = []int64{4}
				}
				mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "policy", "routes"}).AddRow(3, "A", "gemini", `{"enabled":true,"models":["gemini-2.5-pro"]}`, `[]`).AddRow(4, "B", "gemini", `{"enabled":true,"models":["models/gemini-2.5-pro"]}`, `[]`))
				expectMonitorCoverage(mock, f)
				repo := &channelMonitorV2Repository{db: db}
				cfg := service.ChannelMonitorV2Config{HealthThresholds: service.DefaultChannelMonitorV2HealthThresholds()}
				if endpoint == "dimensions" {
					mock.ExpectQuery(`SELECT m.platform, COALESCE\(g.name, ''\)`).WithArgs(f.Start, f.End, pq.Array(ids), 300).WillReturnRows(sqlmock.NewRows([]string{"platform", "name", "groupplatform", "group", "model", "count"}).AddRow("gemini", "B", "gemini", 4, "models/gemini-2.5-pro", 1))
					result, err := repo.GetDimensions(context.Background(), f, cfg)
					require.NoError(t, err)
					counts := map[string]int64{}
					for _, m := range result.Models {
						counts[m.Value] = m.RequestCount
					}
					require.Equal(t, int64(1), counts["models/gemini-2.5-pro"])
					if selected {
						require.Len(t, counts, 1)
					} else {
						require.Len(t, counts, 2)
						require.Zero(t, counts["gemini-2.5-pro"])
					}
				} else {
					mock.ExpectQuery(`SELECT .*m.platform,m.group_id,COALESCE\(g.name,''\),m.model`).WithArgs(f.Start, f.End, pq.Array(ids), 300).WillReturnRows(sqlmock.NewRows([]string{"bucket", "platform", "group", "name", "model", "success", "error", "affected", "attempts", "input", "output", "create", "read", "ttftsum", "ttftcount", "dursum", "durcount"}).AddRow(f.Start, "gemini", 4, "B", "models/gemini-2.5-pro", 1, 0, 0, 0, 10, 5, 0, 0, 100, 1, 1000, 1))
					mock.ExpectQuery(`SELECT .*h.platform,h.group_id,h.model,h.user_id,h.metric,h.upper_bound_ms`).WithArgs(f.Start, f.End, pq.Array(ids), 300, int64(0)).WillReturnRows(sqlmock.NewRows([]string{"bucket", "platform", "group", "model", "user", "metric", "bound", "count"}))
					if endpoint == "models" {
						result, err := repo.GetModels(context.Background(), f, cfg, true)
						require.NoError(t, err)
						counts := map[string]int64{}
						for _, m := range result.Items {
							counts[m.Model] = m.Metrics.RequestCount
						}
						require.Equal(t, int64(1), counts["models/gemini-2.5-pro"])
						if selected {
							require.Len(t, counts, 1)
						} else {
							require.Len(t, counts, 2)
							require.Zero(t, counts["gemini-2.5-pro"])
						}
					} else {
						result, err := repo.GetMatrix(context.Background(), f, cfg, service.ChannelMonitorV2GroupByPlatformGroupModel, true)
						require.NoError(t, err)
						want := 2
						if selected {
							want = 1
						}
						require.Len(t, result.Items, want)
						for _, row := range result.Items {
							if *row.GroupID == 4 {
								require.Equal(t, "models/gemini-2.5-pro", row.Model)
								require.Equal(t, int64(1), row.Metrics.RequestCount)
							} else {
								require.Equal(t, "gemini-2.5-pro", row.Model)
								require.Zero(t, row.Metrics.RequestCount)
							}
						}
					}
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestChannelMonitorV2ReviewExactIdentityBeforeSameGroupCandidateAlias(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	closeChannelMonitorV2TestDB(t, db, mock)
	mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "policy", "routes"}).AddRow(3, "A", "gemini", `{"enabled":true,"models":["gemini-2.5-pro","models/gemini-2.5-pro"]}`, `[]`))
	ctx, _, cfg, err := (&channelMonitorV2Repository{db: db}).resolveChannelMonitorV2Scope(context.Background(), service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{})
	require.NoError(t, err)
	require.Equal(t, "models/gemini-2.5-pro", channelMonitorV2ScopedDisplayModel(ctx, cfg, "gemini", 3, "models/gemini-2.5-pro"))
	require.Equal(t, "gemini-2.5-pro", channelMonitorV2ScopedDisplayModel(ctx, cfg, "gemini", 3, "gemini-2.5-pro"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2ReviewInventoryAndMatrixSeedsUseExactRequestIntersection(t *testing.T) {
	cases := []struct {
		name, policy      string
		platforms, models []string
		observed          bool
		wantRows          int
		wantCount         int64
	}{
		{name: "exact model miss", policy: `{"enabled":true,"models":["gpt-5"]}`, models: []string{"llama"}},
		{name: "exact platform miss", policy: `{"enabled":true,"models":["gpt-5"]}`, platforms: []string{"anthropic"}},
		{name: "wildcard without observed concrete", policy: `{"enabled":true,"models":["gpt-*"]}`},
		{name: "wildcard matching observed tuple", policy: `{"enabled":true,"models":["gpt-*"]}`, platforms: []string{"openai"}, models: []string{"gpt-new"}, observed: true, wantRows: 1, wantCount: 1},
		{name: "matching exact waiting row", policy: `{"enabled":true,"models":["gpt-5"]}`, wantRows: 1},
	}
	for _, tt := range cases {
		for _, endpoint := range []string{"dimensions", "models", "matrix-platform", "matrix-group"} {
			t.Run(tt.name+"/"+endpoint, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				require.NoError(t, err)
				closeChannelMonitorV2TestDB(t, db, mock)
				f := canonicalMonitorTestFilter()
				f.Platforms = tt.platforms
				f.Models = tt.models
				mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "policy", "routes"}).AddRow(3, "A", "openai", tt.policy, `[]`))
				expectMonitorCoverage(mock, f)
				expectArgs := func(q *sqlmock.ExpectedQuery) {
					if len(f.Platforms) > 0 {
						q.WithArgs(f.Start, f.End, pq.Array(f.Platforms), pq.Array([]int64{3}), 300)
					} else {
						q.WithArgs(f.Start, f.End, pq.Array([]int64{3}), 300)
					}
				}
				repo := &channelMonitorV2Repository{db: db}
				cfg := service.ChannelMonitorV2Config{HealthThresholds: service.DefaultChannelMonitorV2HealthThresholds()}
				if endpoint == "dimensions" {
					rows := sqlmock.NewRows([]string{"platform", "name", "groupplatform", "group", "model", "count"})
					if tt.observed {
						rows.AddRow("openai", "A", "openai", 3, "gpt-new", 1)
					}
					q := mock.ExpectQuery(`SELECT m.platform, COALESCE\(g.name, ''\)`)
					expectArgs(q)
					q.WillReturnRows(rows)
					result, err := repo.GetDimensions(context.Background(), f, cfg)
					require.NoError(t, err)
					require.Len(t, result.Platforms, tt.wantRows)
					require.Len(t, result.Groups, tt.wantRows)
					require.Len(t, result.Models, tt.wantRows)
					for _, g := range result.Groups {
						require.Equal(t, int64(3), g.ID)
						require.Equal(t, tt.wantCount, g.RequestCount)
					}
				} else {
					rows := sqlmock.NewRows([]string{"bucket", "platform", "group", "name", "model", "success", "error", "affected", "attempts", "input", "output", "create", "read", "ttftsum", "ttftcount", "dursum", "durcount"})
					if tt.observed {
						rows.AddRow(f.Start, "openai", 3, "A", "gpt-new", 1, 0, 0, 0, 10, 5, 0, 0, 100, 1, 1000, 1)
					}
					q := mock.ExpectQuery(`SELECT .*m.platform,m.group_id,COALESCE\(g.name,''\),m.model`)
					expectArgs(q)
					q.WillReturnRows(rows)
					h := mock.ExpectQuery(`SELECT .*h.platform,h.group_id,h.model,h.user_id,h.metric,h.upper_bound_ms`)
					if len(f.Platforms) > 0 {
						h.WithArgs(f.Start, f.End, pq.Array(f.Platforms), pq.Array([]int64{3}), 300, int64(0))
					} else {
						h.WithArgs(f.Start, f.End, pq.Array([]int64{3}), 300, int64(0))
					}
					h.WillReturnRows(sqlmock.NewRows([]string{"bucket", "platform", "group", "model", "user", "metric", "bound", "count"}))
					if endpoint == "models" {
						result, err := repo.GetModels(context.Background(), f, cfg, true)
						require.NoError(t, err)
						require.Len(t, result.Items, tt.wantRows)
						for _, m := range result.Items {
							require.Equal(t, tt.wantCount, m.Metrics.RequestCount)
						}
					} else {
						groupBy := service.ChannelMonitorV2GroupByPlatform
						if endpoint == "matrix-group" {
							groupBy = service.ChannelMonitorV2GroupByPlatformGroup
						}
						result, err := repo.GetMatrix(context.Background(), f, cfg, groupBy, true)
						require.NoError(t, err)
						require.Len(t, result.Items, tt.wantRows)
						for _, m := range result.Items {
							require.Equal(t, tt.wantCount, m.Metrics.RequestCount)
						}
					}
				}
				require.NoError(t, mock.ExpectationsWereMet())
			})
		}
	}
}

func TestChannelMonitorV2ReviewConfigCatalogMaterializesObservedWildcards(t *testing.T) {
	tests := []struct {
		name, policy       string
		observed, selected bool
		partialCoverage    bool
		want               []string
	}{
		{name: "wildcard observed despite disabled legacy platform", policy: `{"enabled":true,"models":["gpt-*"]}`, observed: true, want: []string{"gpt-new"}},
		{name: "wildcard unobserved has no invented model", policy: `{"enabled":true,"models":["gpt-*"]}`},
		{name: "exact waiting plus observed wildcard", policy: `{"enabled":true,"models":["GPT-5","gpt-*"]}`, observed: true, want: []string{"GPT-5", "gpt-new"}},
		{name: "selected group policy stays selected", policy: `{"enabled":true,"models":["gpt-*"]}`, observed: true, selected: true, want: []string{"gpt-new"}},
		{name: "partial first coverage bucket uses recent short rollups", policy: `{"enabled":true,"models":["gpt-*"]}`, observed: true, partialCoverage: true, want: []string{"gpt-new"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			closeChannelMonitorV2TestDB(t, db, mock)
			thresholds, _ := json.Marshal(service.DefaultChannelMonitorV2HealthThresholds())
			rawGroups := "{}"
			validID := int64(3)
			invalidID := int64(4)
			secondPolicy := `{"enabled":false,"models":["gpt-*"]}`
			if tt.selected {
				rawGroups = "{4}"
				validID = 4
				invalidID = 3
				secondPolicy = tt.policy
			}
			mock.ExpectQuery(`SELECT version, enabled, refresh_interval_seconds, platforms, group_ids`).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"version", "enabled", "refresh", "platforms", "groups", "ignored", "thresholds", "updated", "actor"}).AddRow(1, true, 300, `[{"platform":"openai","enabled":false,"models":["stale","*"]}]`, rawGroups, `{}`, thresholds, time.Now().UTC(), nil))
			mock.ExpectQuery(regexp.QuoteMeta(channelMonitorV2CanonicalPoliciesSQL)).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "policy", "routes"}).AddRow(3, "A", "openai", tt.policy, `[]`).AddRow(4, "B", "openai", secondPolicy, `[]`))
			end := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			start := end.Add(-30 * 24 * time.Hour)
			through := end.Add(-23 * time.Hour)
			coverageStart := start
			if tt.partialCoverage {
				coverageStart = through.Add(-17 * time.Minute)
			}
			mock.ExpectQuery(`SELECT usage_coverage_start, error_coverage_start, data_through, last_successful_at, backfill_cursor`).WithArgs().WillReturnRows(sqlmock.NewRows([]string{"usage", "errors", "through", "computed", "backfill"}).AddRow(coverageStart, coverageStart, through, through, coverageStart))
			rows := sqlmock.NewRows([]string{"platform", "group", "model"})
			if tt.observed {
				rows.AddRow("openai", validID, "gpt-new").AddRow("openai", validID, "GPT-NEW").AddRow("openai", validID, "llama").AddRow("openai", validID, "gpt-*").AddRow("openai", invalidID, "gpt-rogue")
			}
			// Fresh observations can exist only in the short rollups while the
			// daily tier is still waiting for its next rebuild.
			mock.ExpectQuery(`SELECT DISTINCT m.platform,m.group_id,m.model FROM channel_monitor_v2_metrics_rollup.*m.bucket_start >= \$1::timestamptz - INTERVAL '1 day'.*m.bucket_start \+ m.bucket_seconds \* INTERVAL '1 second' > \$1::timestamptz.*m.bucket_start < \$2.*m.bucket_seconds IN \(300,3600,43200,86400\).*\(m.success_requests \+ m.error_requests\) > 0`).WithArgs(coverageStart, through, pq.Array([]int64{validID})).WillReturnRows(rows)
			cfg, err := service.NewChannelMonitorV2Service(&channelMonitorV2Repository{db: db}).GetConfig(context.Background())
			require.NoError(t, err)
			if tt.selected {
				require.Equal(t, []int64{4}, cfg.GroupIDs)
			} else {
				require.Empty(t, cfg.GroupIDs)
			}
			if len(tt.want) == 0 {
				require.Empty(t, cfg.Platforms)
			} else {
				require.Len(t, cfg.Platforms, 1)
				require.Equal(t, "openai", cfg.Platforms[0].Platform)
				require.True(t, cfg.Platforms[0].Enabled)
				require.Equal(t, tt.want, cfg.Platforms[0].Models)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
