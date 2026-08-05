package repository

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUserSubscriptionGetByIDForUpdateLocksRow(t *testing.T) {
	var capturedSQL string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &capturedSQL}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sqlDriver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(sqlDriver))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewUserSubscriptionRepository(client)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	row := []driver.Value{
		int64(7), now, now, nil,
		int64(11), int64(13), now, now.AddDate(0, 0, 30), "active",
		nil, nil, nil, nil,
		0.0, 0.0, 0.0, 0.0,
		nil, now, "renewal",
		nil, nil, nil, nil,
		"Test Group", "openai", 1.0,
		nil, nil, nil, nil, nil,
	}

	mock.ExpectQuery("locked subscription").WillReturnRows(
		sqlmock.NewRows(usersubscription.Columns).AddRow(row...),
	)

	sub, err := repo.GetByIDForUpdate(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), sub.ID)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Contains(t, strings.ToUpper(normalizeSQLWhitespace(capturedSQL)), "FOR UPDATE")
}
