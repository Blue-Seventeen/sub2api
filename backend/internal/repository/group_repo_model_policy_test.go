package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	sqlmock "github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGlobalModelOperationLocksPlatformBeforeAnyWrites(t *testing.T) {
	for _, method := range []string{"create", "update"} {
		t.Run(method, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			repo := newGroupRepositoryWithSQL(client, db)
			lockFailure := errors.New("platform lock unavailable")
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
				WithArgs("sub2api:global-model-policy:openai").WillReturnError(lockFailure)
			mock.ExpectRollback()
			group := &service.Group{ID: 10, Platform: service.PlatformOpenAI}
			operations := []service.GroupModelOperation{{Operation: "add", Model: "gpt-*"}}
			if method == "create" {
				_, err = repo.CreateWithGlobalModelOperations(context.Background(), group, operations)
			} else {
				_, err = repo.UpdateWithGlobalModelOperations(context.Background(), group, operations)
			}
			require.ErrorIs(t, err, lockFailure)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGroupModelPolicyProjectionNeverFallsBackToStaleMirror(t *testing.T) {
	for _, canonical := range []service.GroupModelsListConfig{
		{}, {Models: []string{"disabled-canonical"}}, {Enabled: true, Models: []string{"canonical"}},
	} {
		got := groupEntityToService(&dbent.Group{
			ModelsListConfig: canonical,
			ModelAllowlist:   service.DomainGroupModelAllowlist(service.GroupModelAllowlist{Enabled: true, Models: []string{"stale-mirror"}}),
		})
		require.True(t, got.Hydrated)
		require.Equal(t, canonical, got.ModelsListConfig)
		require.Equal(t, canonical.Enabled, got.ModelAllowlist.Enabled)
		require.Equal(t, canonical.Models, got.ModelAllowlist.Models)
		require.Equal(t, !canonical.Enabled, got.AllowsModel("stale-mirror"))
	}
}
