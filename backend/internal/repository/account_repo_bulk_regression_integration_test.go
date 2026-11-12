//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// cleanupBulkAccountRegressionFixtures removes only the exact rows created by
// this file. These tests use a persistent Ent client to verify commits and
// therefore must not leak fixtures into the shared isolated integration DB.
func cleanupBulkAccountRegressionFixtures(accountIDs, groupIDs []int64) {
	ctx := context.Background()
	for _, id := range accountIDs {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id = $1 OR payload -> 'account_ids' @> to_jsonb($1::bigint)", id)
	}
	if len(accountIDs) > 0 {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM account_groups WHERE account_id = ANY($1)", pq.Array(accountIDs))
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = ANY($1)", pq.Array(accountIDs))
	}
	if len(groupIDs) > 0 {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE group_id = ANY($1)", pq.Array(groupIDs))
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = ANY($1)", pq.Array(groupIDs))
	}
}

func TestBulkAccountRepositoryRegressionOrdinaryUpdatesRefreshSnapshots(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: "bulk-regression-cache", Credentials: map[string]any{"model_mapping": map[string]any{"old": "old"}}, Extra: map[string]any{"unrelated": true}})
	recorder := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(client, integrationDB, recorder)
	t.Cleanup(func() { cleanupBulkAccountRegressionFixtures([]int64{account.ID}, nil) })
	priority := 9
	rows, err := repo.BulkUpdate(ctx, []int64{account.ID, account.ID}, service.AccountBulkUpdate{Priority: &priority, Credentials: map[string]any{"model_mapping": map[string]any{}}, Extra: map[string]any{"codex_cli_only": false}})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	require.Len(t, recorder.setAccounts, 1)
	require.Equal(t, 9, recorder.setAccounts[0].Priority)
	require.Empty(t, recorder.setAccounts[0].Credentials["model_mapping"])
	require.Equal(t, false, recorder.setAccounts[0].Extra["codex_cli_only"])
	require.Equal(t, true, recorder.setAccounts[0].Extra["unrelated"])
	var events int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox WHERE event_type = $1 AND payload -> 'account_ids' @> to_jsonb($2::bigint)", service.SchedulerOutboxEventAccountBulkChanged, account.ID).Scan(&events))
	require.Equal(t, 1, events, "immediate cache publication must retain durable outbox propagation")
}

func TestBulkAccountRepositoryRegressionSnapshotFailureIsReturned(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: "bulk-regression-cache-error", Priority: 3})
	t.Cleanup(func() { cleanupBulkAccountRegressionFixtures([]int64{account.ID}, nil) })
	recorder := &schedulerCacheRecorder{setErr: errors.New("scheduler cache unavailable")}
	repo := newAccountRepositoryWithSQL(client, integrationDB, recorder)
	priority := 9
	rows, err := repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Priority: &priority})
	require.Error(t, err)
	require.ErrorContains(t, err, "scheduler snapshot synchronization failed")
	require.EqualValues(t, 1, rows, "the database write is committed, but cache failure must be observable")
	actual, getErr := repo.GetByID(ctx, account.ID)
	require.NoError(t, getErr)
	require.Equal(t, priority, actual.Priority)
}

func TestBulkAccountRepositoryRegressionEmptyGroupsPublishOldBuckets(t *testing.T) {
	for _, outer := range []bool{false, true} {
		transactionKind := map[bool]string{false: "owned_transaction", true: "external_transaction"}[outer]
		t.Run(transactionKind, func(t *testing.T) {
			ctx := context.Background()
			client := testEntClient(t)
			group := mustCreateGroup(t, client, &service.Group{Name: "bulk-regression-group-" + transactionKind})
			account := mustCreateAccount(t, client, &service.Account{Name: "bulk-regression-unbind"})
			mustBindAccountToGroup(t, client, account.ID, group.ID, 1)
			t.Cleanup(func() { cleanupBulkAccountRegressionFixtures([]int64{account.ID}, []int64{group.ID}) })
			repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
			var tx *dbent.Tx
			if outer {
				var err error
				tx, err = client.Tx(ctx)
				require.NoError(t, err)
				defer tx.Rollback()
				ctx = dbent.NewTxContext(ctx, tx)
			}
			require.NoError(t, repo.BindGroups(ctx, account.ID, nil))
			if tx != nil {
				require.NoError(t, tx.Commit())
			}
			actual, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.Empty(t, actual.GroupIDs)
			var raw []byte
			require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT payload FROM scheduler_outbox WHERE event_type=$1 AND account_id=$2 ORDER BY id DESC LIMIT 1", service.SchedulerOutboxEventAccountGroupsChanged, account.ID).Scan(&raw))
			var payload map[string][]int64
			require.NoError(t, json.Unmarshal(raw, &payload))
			require.Equal(t, []int64{group.ID}, payload["group_ids"])
		})
	}
}

func TestBulkAccountRepositoryRegressionGroupEventRollsBackWithOuterTransaction(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	group := mustCreateGroup(t, client, &service.Group{Name: "bulk-regression-rollback"})
	account := mustCreateAccount(t, client, &service.Account{Name: "bulk-regression-rollback"})
	mustBindAccountToGroup(t, client, account.ID, group.ID, 1)
	t.Cleanup(func() { cleanupBulkAccountRegressionFixtures([]int64{account.ID}, []int64{group.ID}) })
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, repo.BindGroups(dbent.NewTxContext(ctx, tx), account.ID, nil))
	require.NoError(t, tx.Rollback())
	actual, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{group.ID}, actual.GroupIDs)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox WHERE event_type=$1 AND account_id=$2", service.SchedulerOutboxEventAccountGroupsChanged, account.ID).Scan(&count))
	require.Zero(t, count)
}

func TestBulkAccountRepositoryRegressionLoadFactorAndDeletedRows(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)
	account := mustCreateAccount(t, client, &service.Account{Name: "bulk-regression-load"})
	deleted := mustCreateAccount(t, client, &service.Account{Name: "bulk-regression-deleted"})
	require.NoError(t, client.Account.UpdateOneID(account.ID).SetLoadFactor(37).Exec(ctx))
	require.NoError(t, client.Account.UpdateOneID(deleted.ID).SetDeletedAt(account.CreatedAt).Exec(ctx))
	zero := 0
	rows, err := repo.BulkUpdate(ctx, []int64{account.ID, deleted.ID, account.ID, 9223372036854775806}, service.AccountBulkUpdate{LoadFactor: &zero})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	actual, err := client.Account.Query().Where(dbaccount.ID(account.ID)).Only(ctx)
	require.NoError(t, err)
	require.Nil(t, actual.LoadFactor)
}

func TestBulkAccountRepositoryRegressionTransactionClientDoesNotPublishBeforeCommit(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{Name: "bulk-regression-tx-cache", Priority: 3})
	t.Cleanup(func() { cleanupBulkAccountRegressionFixtures([]int64{account.ID}, nil) })
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	defer tx.Rollback()
	recorder := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, recorder)
	priority := 9
	rows, err := repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Priority: &priority})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	require.Empty(t, recorder.setAccounts, "an outer transaction owns publication until commit")
	require.NoError(t, tx.Rollback())
	actual, err := client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, account.Priority, actual.Priority)
	var events int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox WHERE event_type = $1 AND payload -> 'account_ids' @> to_jsonb($2::bigint)", service.SchedulerOutboxEventAccountBulkChanged, account.ID).Scan(&events))
	require.Zero(t, events)
}

func TestBulkAccountRepositoryRegressionCombinedColumnsAndGroupsAreAtomic(t *testing.T) {
	for _, failBinding := range []bool{false, true} {
		name := map[bool]string{false: "commit", true: "binding-error-rolls-back-columns"}[failBinding]
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			client := testEntClient(t)
			group := mustCreateGroup(t, client, &service.Group{Name: "bulk-combined-" + name})
			account := mustCreateAccount(t, client, &service.Account{Name: "bulk-combined-" + name, Priority: 3})
			mustBindAccountToGroup(t, client, account.ID, group.ID, 1)
			t.Cleanup(func() { cleanupBulkAccountRegressionFixtures([]int64{account.ID}, []int64{group.ID}) })
			recorder := &schedulerCacheRecorder{}
			repo := newAccountRepositoryWithSQL(client, integrationDB, recorder)
			updater, ok := any(repo).(interface {
				BulkUpdateAndBindGroups(context.Context, int64, service.AccountBulkUpdate, []int64) (int64, error)
			})
			require.True(t, ok, "combined edits need one repository transaction")
			priority := 9
			groups := []int64{}
			if failBinding {
				groups = []int64{9223372036854775806}
			}
			rows, err := updater.BulkUpdateAndBindGroups(ctx, account.ID, service.AccountBulkUpdate{Priority: &priority}, groups)
			if failBinding {
				require.Error(t, err)
				require.Zero(t, rows)
				require.Empty(t, recorder.setAccounts)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 1, rows)
				require.Len(t, recorder.setAccounts, 1)
				require.Equal(t, priority, recorder.setAccounts[0].Priority)
				require.Empty(t, recorder.setAccounts[0].GroupIDs)
			}
			actual, err := repo.GetByID(ctx, account.ID)
			require.NoError(t, err)
			if failBinding {
				require.Equal(t, 3, actual.Priority)
				require.Equal(t, []int64{group.ID}, actual.GroupIDs)
				var events int
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_outbox WHERE account_id = $1 OR payload -> 'account_ids' @> to_jsonb($1::bigint)", account.ID).Scan(&events))
				require.Zero(t, events)
			} else {
				require.Equal(t, priority, actual.Priority)
				require.Empty(t, actual.GroupIDs)
			}
		})
	}
}
