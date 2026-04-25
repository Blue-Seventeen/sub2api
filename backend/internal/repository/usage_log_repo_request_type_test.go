package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-1",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		InputTokens:    10,
		OutputTokens:   20,
		TotalCost:      1,
		ActualCost:     1,
		RealActualCost: 1,
		UnifiedRateMultiplier: 1,
		BillingType:    service.BillingTypeBalance,
		RequestType:    service.RequestTypeWSV2,
		Stream:         false,
		OpenAIWSMode:   false,
		CreatedAt:      createdAt,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(usageLogInsertExpectationArgs(log)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(99), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, int64(99), log.ID)
	require.Nil(t, log.ServiceTier)
	require.Equal(t, service.RequestTypeWSV2, log.RequestType)
	require.True(t, log.Stream)
	require.True(t, log.OpenAIWSMode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryCreate_PersistsServiceTier(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	serviceTier := "priority"
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-service-tier",
		Model:          "gpt-5.4",
		RequestedModel: "gpt-5.4",
		ServiceTier:    &serviceTier,
		CreatedAt:      createdAt,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(usageLogInsertExpectationArgs(log)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(100), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildUsageLogBestEffortInsertQuery_IncludesRequestedModelColumn(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-query",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC),
	})

	query, args := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	require.Contains(t, query, "INSERT INTO usage_logs (")
	require.Contains(t, query, "\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,")
	require.Contains(t, query, "\n\t\t\trequest_id,\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,")
	require.Len(t, args, len(prepared.args))
	require.Equal(t, prepared.args[5], args[5])
}

func TestExecUsageLogInsertNoResult_PersistsRequestedModel(t *testing.T) {
	db, mock := newSQLMock(t)
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-exec",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 4, 12, 0, 0, 0, time.UTC),
	})

	mock.ExpectExec("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := execUsageLogInsertNoResult(context.Background(), db, prepared)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareUsageLogInsert_ArgCountMatchesTypes(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-arg-count",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 5, 12, 0, 0, 0, time.UTC),
	})

	require.Len(t, prepared.args, len(usageLogInsertArgTypes))
}

func TestCoalesceTrimmedString(t *testing.T) {
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{}, "fallback"))
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{Valid: true, String: "   "}, "fallback"))
	require.Equal(t, "value", coalesceTrimmedString(sql.NullString{Valid: true, String: "value"}, "fallback"))
}

func anySliceToDriverValues(values []any) []driver.Value {
	out := make([]driver.Value, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func TestUsageLogRepositoryListWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeWSV2)
	stream := false
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
		ExactTotal:  true,
	}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(requestType).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\) ORDER BY id DESC LIMIT \\$2 OFFSET \\$3").
		WithArgs(requestType, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.Equal(t, int64(0), page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUsageTrendWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeStream)
	stream := true

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "real_actual_cost"}))

	trend, err := repo.GetUsageTrendWithFilters(context.Background(), start, end, "day", 0, 0, 0, 0, "", &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeWSV2)
	stream := false

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"model", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "real_actual_cost", "account_cost"}))

	stats, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeSync)
	stream := true
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
	}

	mock.ExpectQuery("FROM usage_logs\\s+WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE\\)\\)").
		WithArgs(requestType).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_tokens",
			"total_cost",
			"total_actual_cost",
			"real_total_actual_cost",
			"total_account_cost",
			"avg_duration_ms",
		}).AddRow(int64(1), int64(2), int64(3), int64(4), 1.2, 1.0, 0.8, 1.2, 20.0))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(inbound_endpoint\\), ''\\), 'unknown'\\) AS endpoint").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost", "real_actual_cost"}))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(upstream_endpoint\\), ''\\), 'unknown'\\) AS endpoint").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost", "real_actual_cost"}))
	mock.ExpectQuery("SELECT CONCAT\\(").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), requestType).
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost", "real_actual_cost"}))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, int64(9), stats.TotalTokens)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost should always be returned")
	require.Equal(t, 1.2, *stats.TotalAccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_logs").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"model", "requests", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "total_tokens",
			"cost", "actual_cost", "real_actual_cost", "account_cost",
		}).
			AddRow("claude-opus-4-6", int64(10), int64(100), int64(200), int64(5), int64(3), int64(308), 2.5, 2.0, 2.0, 1.8).
			AddRow("claude-sonnet-4-6", int64(5), int64(50), int64(100), int64(0), int64(0), int64(150), 1.0, 0.8, 0.8, 0.7))

	results, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "claude-opus-4-6", results[0].Model)
	require.Equal(t, 2.5, results[0].Cost)
	require.Equal(t, 2.0, results[0].ActualCost)
	require.Equal(t, 1.8, results[0].AccountCost)
	require.Equal(t, "claude-sonnet-4-6", results[1].Model)
	require.Equal(t, 0.7, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetGroupStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_logs").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"group_id", "group_name", "requests", "total_tokens",
			"cost", "actual_cost", "real_actual_cost", "account_cost",
		}).
			AddRow(int64(1), "azure-cc", int64(100), int64(5000), 10.0, 8.5, 8.5, 7.2).
			AddRow(int64(2), "max", int64(50), int64(2000), 5.0, 4.0, 4.0, 3.5))

	results, err := repo.GetGroupStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, int64(1), results[0].GroupID)
	require.Equal(t, "azure-cc", results[0].GroupName)
	require.Equal(t, 10.0, results[0].Cost)
	require.Equal(t, 8.5, results[0].ActualCost)
	require.Equal(t, 7.2, results[0].AccountCost)
	require.Equal(t, int64(2), results[1].GroupID)
	require.Equal(t, 3.5, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersAlwaysReturnsAccountCost(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	// No AccountID filter set - TotalAccountCost should still be returned for UI summaries
	filters := usagestats.UsageLogFilters{}

	mock.ExpectQuery("FROM usage_logs").
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens",
			"total_cache_tokens", "total_cost", "total_actual_cost",
			"real_total_actual_cost", "total_account_cost", "avg_duration_ms",
		}).AddRow(int64(50), int64(1000), int64(2000), int64(100), 15.0, 12.5, 12.5, 11.0, 100.0))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(inbound_endpoint\\)").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT COALESCE\\(NULLIF\\(TRIM\\(upstream_endpoint\\)").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))
	mock.ExpectQuery("SELECT CONCAT\\(").
		WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost must always be returned, even without AccountID filter")
	require.Equal(t, 11.0, *stats.TotalAccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUserSpendingRanking(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	rows := sqlmock.NewRows([]string{"user_id", "email", "actual_cost", "real_actual_cost", "requests", "tokens", "total_actual_cost", "real_total_actual_cost", "total_requests", "total_tokens"}).
		AddRow(int64(2), "beta@example.com", 12.5, 8.5, int64(9), int64(900), 40.0, 28.0, int64(30), int64(2600)).
		AddRow(int64(1), "alpha@example.com", 12.5, 8.5, int64(8), int64(800), 40.0, 28.0, int64(30), int64(2600)).
		AddRow(int64(3), "gamma@example.com", 4.25, 11.0, int64(5), int64(300), 40.0, 28.0, int64(30), int64(2600))

	mock.ExpectQuery("WITH user_spend AS \\(").
		WithArgs(start, end, 12).
		WillReturnRows(rows)

	got, err := repo.GetUserSpendingRanking(context.Background(), start, end, 12)
	require.NoError(t, err)
	require.Equal(t, &usagestats.UserSpendingRankingResponse{
		Ranking: []usagestats.UserSpendingRankingItem{
			{UserID: 2, Email: "beta@example.com", ActualCost: 12.5, RealActualCost: 8.5, Requests: 9, Tokens: 900},
			{UserID: 1, Email: "alpha@example.com", ActualCost: 12.5, RealActualCost: 8.5, Requests: 8, Tokens: 800},
			{UserID: 3, Email: "gamma@example.com", ActualCost: 4.25, RealActualCost: 11.0, Requests: 5, Tokens: 300},
		},
		TotalActualCost: 40.0,
		RealTotalActualCost: 28.0,
		TotalRequests:   30,
		TotalTokens:     2600,
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildRequestTypeFilterConditionLegacyFallback(t *testing.T) {
	tests := []struct {
		name      string
		request   int16
		wantWhere string
		wantArg   int16
	}{
		{
			name:      "sync_with_legacy_fallback",
			request:   int16(service.RequestTypeSync),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeSync),
		},
		{
			name:      "stream_with_legacy_fallback",
			request:   int16(service.RequestTypeStream),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeStream),
		},
		{
			name:      "ws_v2_with_legacy_fallback",
			request:   int16(service.RequestTypeWSV2),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND openai_ws_mode = TRUE))",
			wantArg:   int16(service.RequestTypeWSV2),
		},
		{
			name:      "invalid_request_type_normalized_to_unknown",
			request:   int16(99),
			wantWhere: "request_type = $3",
			wantArg:   int16(service.RequestTypeUnknown),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildRequestTypeFilterCondition(3, tt.request)
			require.Equal(t, tt.wantWhere, where)
			require.Equal(t, []any{tt.wantArg}, args)
		})
	}
}

type usageLogScannerStub struct {
	values []any
}

func (s usageLogScannerStub) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan arg count mismatch: got %d want %d", len(dest), len(s.values))
	}
	for i := range dest {
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Ptr {
			return fmt.Errorf("dest[%d] is not pointer", i)
		}
		dv.Elem().Set(reflect.ValueOf(s.values[i]))
	}
	return nil
}

type usageLogScanRowOptions struct {
	ID           int64
	UserID       int64
	APIKeyID     int64
	AccountID    int64
	RequestID    string
	Model        string
	RequestedModel string
	BillingType  int16
	RequestType  int16
	LegacyStream bool
	LegacyWS     bool
	ServiceTier  *string
	CreatedAt    time.Time
}

func usageLogInsertExpectationArgs(log *service.UsageLog) []driver.Value {
	cloned := *log
	prepared := prepareUsageLogInsert(&cloned)
	return anySliceToDriverValues(prepared.args)
}

func buildUsageLogScanValues(opts usageLogScanRowOptions) []any {
	requestedModel := opts.RequestedModel
	if requestedModel == "" {
		requestedModel = opts.Model
	}

	return []any{
		opts.ID,
		opts.UserID,
		opts.APIKeyID,
		opts.AccountID,
		nullString(&opts.RequestID),
		opts.Model,
		nullString(&requestedModel),
		sql.NullString{}, // upstream_model
		sql.NullInt64{},  // group_id
		sql.NullInt64{},  // subscription_id
		1,                // input_tokens
		2,                // output_tokens
		3,                // cache_creation_tokens
		4,                // cache_read_tokens
		5,                // cache_creation_5m_tokens
		6,                // cache_creation_1h_tokens
		0,                // image_output_tokens
		0.0,              // image_output_cost
		0.1,              // input_cost
		0.2,              // output_cost
		0.3,              // cache_creation_cost
		0.4,              // cache_read_cost
		1.0,              // total_cost
		0.9,              // actual_cost
		0.9,              // real_actual_cost
		1.0,              // unified_rate_multiplier
		1.0,              // rate_multiplier
		sql.NullFloat64{}, // account_rate_multiplier
		opts.BillingType,
		opts.RequestType,
		opts.LegacyStream,
		opts.LegacyWS,
		sql.NullInt64{},   // duration_ms
		sql.NullInt64{},   // first_token_ms
		sql.NullString{},  // user_agent
		sql.NullString{},  // ip_address
		0,                 // image_count
		sql.NullString{},  // image_size
		nullString(opts.ServiceTier),
		sql.NullString{},  // reasoning_effort
		sql.NullString{},  // inbound_endpoint
		sql.NullString{},  // upstream_endpoint
		sql.NullString{},  // client_profile
		sql.NullString{},  // compatibility_route
		sql.NullString{},  // fallback_chain
		sql.NullString{},  // upstream_transport
		false,             // cache_ttl_overridden
		sql.NullInt64{},   // channel_id
		sql.NullString{},  // model_mapping_chain
		sql.NullString{},  // billing_tier
		sql.NullString{},  // billing_mode
		sql.NullFloat64{}, // account_stats_cost
		opts.CreatedAt,
	}
}

func TestScanUsageLogRequestTypeAndLegacyFallback(t *testing.T) {
	t.Run("request_type_ws_v2_overrides_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		serviceTier := "priority"
		log, err := scanUsageLog(usageLogScannerStub{values: buildUsageLogScanValues(usageLogScanRowOptions{
			ID:             1,
			UserID:         10,
			APIKeyID:       20,
			AccountID:      30,
			RequestID:      "req-1",
			Model:          "gpt-5",
			BillingType:    int16(service.BillingTypeBalance),
			RequestType:    int16(service.RequestTypeWSV2),
			LegacyStream:   false,
			LegacyWS:       false,
			ServiceTier:    &serviceTier,
			CreatedAt:      now,
		})})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
		require.Equal(t, service.RequestTypeWSV2, log.RequestType)
		require.True(t, log.Stream)
		require.True(t, log.OpenAIWSMode)
	})

	t.Run("request_type_unknown_falls_back_to_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		serviceTier := "flex"
		log, err := scanUsageLog(usageLogScannerStub{values: buildUsageLogScanValues(usageLogScanRowOptions{
			ID:             2,
			UserID:         11,
			APIKeyID:       21,
			AccountID:      31,
			RequestID:      "req-2",
			Model:          "gpt-5",
			BillingType:    int16(service.BillingTypeBalance),
			RequestType:    int16(service.RequestTypeUnknown),
			LegacyStream:   true,
			LegacyWS:       false,
			ServiceTier:    &serviceTier,
			CreatedAt:      now,
		})})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "flex", *log.ServiceTier)
		require.Equal(t, service.RequestTypeStream, log.RequestType)
		require.True(t, log.Stream)
		require.False(t, log.OpenAIWSMode)
	})

	t.Run("service_tier_is_scanned", func(t *testing.T) {
		now := time.Now().UTC()
		serviceTier := "priority"
		log, err := scanUsageLog(usageLogScannerStub{values: buildUsageLogScanValues(usageLogScanRowOptions{
			ID:             3,
			UserID:         12,
			APIKeyID:       22,
			AccountID:      32,
			RequestID:      "req-3",
			Model:          "gpt-5.4",
			BillingType:    int16(service.BillingTypeBalance),
			RequestType:    int16(service.RequestTypeSync),
			LegacyStream:   false,
			LegacyWS:       false,
			ServiceTier:    &serviceTier,
			CreatedAt:      now,
		})})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
	})

}
