//go:build unit

package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type usageCreateRequestIDRepo struct {
	service.UsageLogRepository
	created *service.UsageLog
}

func (r *usageCreateRequestIDRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.created = log
	return true, nil
}

type usageCreateRequestIDUserRepo struct{ service.UserRepository }

func (*usageCreateRequestIDUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id}, nil
}

func TestUsageServiceCreatePreservesUpstreamRequestID(t *testing.T) {
	for _, raw := range []string{
		`{"user_id":7,"request_id":"client-id","upstream_request_id":"provider-id"}`,
		`{"user_id":7,"request_id":"client-id"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			mock.ExpectBegin()
			mock.ExpectCommit()
			repo := &usageCreateRequestIDRepo{}
			svc := service.NewUsageService(repo, &usageCreateRequestIDUserRepo{}, client, nil)
			var req service.CreateUsageLogRequest
			require.NoError(t, json.Unmarshal([]byte(raw), &req))
			log, err := svc.Create(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, repo.created)
			require.Equal(t, req.UpstreamRequestID, log.UpstreamRequestID)
			require.Equal(t, req.UpstreamRequestID, repo.created.UpstreamRequestID)
			require.Equal(t, "client-id", repo.created.RequestID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
