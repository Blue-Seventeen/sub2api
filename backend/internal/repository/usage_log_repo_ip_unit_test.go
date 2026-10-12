//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestUsageIPListExactCount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := &usageLogRepository{sql: db}
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM usage_logs WHERE ip_address IN \(\$1, \$2\)`).WithArgs("192.0.2.1", "::1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT .* FROM usage_logs WHERE ip_address IN \(\$1, \$2\).*LIMIT \$3 OFFSET \$4`).WithArgs("192.0.2.1", "::1", 20, 0).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, usagestats.UsageLogFilters{IPAddresses: []string{"192.0.2.1", "::1"}})
	require.NoError(t, err)
	require.EqualValues(t, 0, page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}
