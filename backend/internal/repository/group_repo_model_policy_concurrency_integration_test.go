//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGlobalModelOperationsSerializeCreatesOnEmptyPlatform(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(fmt.Sprintf("commit=%t", commit), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			platform := fmt.Sprintf("policy-%d", time.Now().UnixNano())
			t.Cleanup(func() { cleanupGlobalPolicyPlatform(t, platform) })
			firstTx := testEntTx(t)
			defer firstTx.Rollback()
			firstRepo := newGroupRepositoryWithSQL(firstTx.Client(), firstTx)
			first := concurrentPolicyGroup(platform+"-first", platform)
			_, err := firstRepo.CreateWithGlobalModelOperations(ctx, first, []service.GroupModelOperation{{Operation: "add", Model: "first"}})
			require.NoError(t, err)
			var firstPID int
			require.NoError(t, scanSingleRow(ctx, firstTx, "SELECT pg_backend_pid()", nil, &firstPID))
			second := concurrentPolicyGroup(platform+"-second", platform)
			done := make(chan struct{})
			var operationErr error
			go func() {
				_, operationErr = newGroupRepositoryWithSQL(integrationEntClient, integrationDB).
					CreateWithGlobalModelOperations(ctx, second, []service.GroupModelOperation{{Operation: "add", Model: "second"}})
				close(done)
			}()
			// Join even on assertion failure, before fixture cleanup can run.
			defer func() {
				cancel()
				_ = firstTx.Rollback()
				<-done
			}()
			requireGlobalPolicyLockWait(t, ctx, firstPID)

			otherPlatform := platform + "-other"
			t.Cleanup(func() { cleanupGlobalPolicyPlatform(t, otherPlatform) })
			other := concurrentPolicyGroup(otherPlatform, otherPlatform)
			_, err = newGroupRepositoryWithSQL(integrationEntClient, integrationDB).
				CreateWithGlobalModelOperations(ctx, other, []service.GroupModelOperation{{Operation: "add", Model: "other"}})
			require.NoError(t, err, "different platforms must not wait on this lock")
			if commit {
				require.NoError(t, firstTx.Commit())
			} else {
				require.NoError(t, firstTx.Rollback())
			}
			<-done
			require.NoError(t, operationErr)
			loaded, err := integrationEntClient.Group.Get(ctx, second.ID)
			require.NoError(t, err)
			require.Equal(t, []string{"base", "second"}, loaded.ModelsListConfig.Models)
			require.Equal(t, loaded.ModelsListConfig.Models, loaded.ModelAllowlist.Models)
			loaded, err = integrationEntClient.Group.Get(ctx, first.ID)
			if commit {
				require.NoError(t, err)
				require.Equal(t, []string{"base", "first", "second"}, loaded.ModelsListConfig.Models)
				require.Equal(t, loaded.ModelsListConfig.Models, loaded.ModelAllowlist.Models)
			} else {
				require.True(t, dbent.IsNotFound(err))
			}
		})
	}
}

func TestGlobalModelOperationsConcurrentUpdatesRejectStaleSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	platform := fmt.Sprintf("policy-%d", time.Now().UnixNano())
	t.Cleanup(func() { cleanupGlobalPolicyPlatform(t, platform) })
	repo := newGroupRepositoryWithSQL(integrationEntClient, integrationDB)
	first := concurrentPolicyGroup(platform+"-first", platform)
	second := concurrentPolicyGroup(platform+"-second", platform)
	require.NoError(t, repo.Create(ctx, first))
	require.NoError(t, repo.Create(ctx, second))
	firstTx := testEntTx(t)
	defer firstTx.Rollback()
	_, err := newGroupRepositoryWithSQL(firstTx.Client(), firstTx).
		UpdateWithGlobalModelOperations(ctx, first, []service.GroupModelOperation{{Operation: "add", Model: "first"}})
	require.NoError(t, err)
	var firstPID int
	require.NoError(t, scanSingleRow(ctx, firstTx, "SELECT pg_backend_pid()", nil, &firstPID))
	done := make(chan struct{})
	var operationErr error
	go func() {
		_, operationErr = repo.UpdateWithGlobalModelOperations(ctx, second, []service.GroupModelOperation{{Operation: "add", Model: "second"}})
		close(done)
	}()
	defer func() {
		cancel()
		_ = firstTx.Rollback()
		<-done
	}()
	requireGlobalPolicyLockWait(t, ctx, firstPID)
	require.NoError(t, firstTx.Commit())
	<-done
	require.ErrorIs(t, operationErr, service.ErrGroupConcurrentUpdate)
	for _, id := range []int64{first.ID, second.ID} {
		got, err := repo.GetByIDLite(ctx, id)
		require.NoError(t, err)
		require.Equal(t, []string{"base", "first"}, got.ModelsListConfig.Models)
		require.Equal(t, got.ModelsListConfig.Models, got.ModelAllowlist.Models)
	}
	fresh, err := repo.GetByIDLite(ctx, second.ID)
	require.NoError(t, err)
	_, err = repo.UpdateWithGlobalModelOperations(ctx, fresh, []service.GroupModelOperation{{Operation: "add", Model: "second"}})
	require.NoError(t, err)
	for _, id := range []int64{first.ID, second.ID} {
		got, err := repo.GetByIDLite(ctx, id)
		require.NoError(t, err)
		require.Equal(t, []string{"base", "first", "second"}, got.ModelsListConfig.Models)
	}
}

func concurrentPolicyGroup(name, platform string) *service.Group {
	return &service.Group{Name: name, Platform: platform, RateMultiplier: 1, Status: service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard,
		ModelsListConfig: service.GroupModelsListConfig{Enabled: true, Models: []string{"base"}}}
}

func requireGlobalPolicyLockWait(t *testing.T, ctx context.Context, blockerPID int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
WHERE $1 = ANY(pg_blocking_pids(pid)) AND wait_event_type = 'Lock' AND wait_event = 'advisory')`, blockerPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "second global operation must wait on the platform transaction lock")
}

func cleanupGlobalPolicyPlatform(t *testing.T, platform string) {
	t.Helper()
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE group_id IN (SELECT id FROM groups WHERE platform = $1)`, platform)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM groups WHERE platform = $1`, platform)
	require.NoError(t, err)
}
