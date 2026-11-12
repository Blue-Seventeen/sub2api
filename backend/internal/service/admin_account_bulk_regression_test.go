//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBulkAccountRegressionLoadFactorClearPreservesIntent(t *testing.T) {
	for _, value := range []int{0, -1} {
		t.Run(map[int]string{0: "zero", -1: "negative"}[value], func(t *testing.T) {
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}}}
			result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, LoadFactor: &value})
			require.NoError(t, err)
			require.Equal(t, 1, result.Success)
			require.NotNil(t, repo.lastBulkUpdate.LoadFactor, "nil would silently omit the requested clear")
			require.LessOrEqual(t, *repo.lastBulkUpdate.LoadFactor, 0)
		})
	}
}

func TestBulkAccountRegressionDeduplicatesAndReportsMissingTargets(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}}}
	priority := 7
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 99, 1, 2, 99}, Priority: &priority})
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, []int64{1, 2}, result.SuccessIDs)
	require.Equal(t, []int64{99}, result.FailedIDs)
	require.Empty(t, bulkAccountUnconfirmedIDs(t, result), "targets missing before the write remain ordinary failures")
	require.Equal(t, []int64{1, 2}, repo.bulkUpdateIDs)
	require.Len(t, result.Results, 3)
}

func TestBulkAccountRegressionSanitizedNoOpCannotReportSuccess(t *testing.T) {
	for _, extra := range []map[string]any{nil, {UpstreamBillingProbeExtraKey: map[string]any{"status": "fake"}}} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}}}
		result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: extra})
		require.Error(t, err)
		require.Nil(t, result)
		require.Zero(t, repo.bulkUpdateCalls)
	}
}

func TestBulkAccountRegressionPureGroupsDoNotRequireColumnRows(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}}}
	empty := []int64{}
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 99, 1}, GroupIDs: &empty, SkipMixedChannelCheck: true})
	require.NoError(t, err)
	require.Equal(t, []int64{1}, result.SuccessIDs)
	require.Equal(t, []int64{99}, result.FailedIDs)
	require.Equal(t, []int64{1}, repo.bindGroupsCalls)
	require.Zero(t, repo.bulkUpdateCalls, "group-only changes have no accounts column update")
}

func TestBulkAccountRegressionSeedanceCapabilities(t *testing.T) {
	for _, capabilities := range [][]string{{"seedance"}, {"chat_completions", "seedance"}, {"chat_completions", "embeddings", "seedance"}} {
		t.Run(capabilities[len(capabilities)-1]+string(rune('0'+len(capabilities))), func(t *testing.T) {
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://ark.example/v3"}}}}
			result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: map[string]any{"openai_capabilities": capabilities}})
			require.NoError(t, err)
			require.Equal(t, 1, result.Success)
			require.Equal(t, capabilities, repo.lastBulkUpdate.Credentials["openai_capabilities"], "the default pair must not erase explicitly enabled seedance")
		})
	}
}

type bulkAccountAffectedRowsRepo struct {
	*accountRepoStubForBulkUpdate
	rows         int64
	after        []*Account
	afterErr     error
	getByIDsRead int
}

func (r *bulkAccountAffectedRowsRepo) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	r.getByIDsRead++
	if r.getByIDsRead > 1 {
		return r.after, r.afterErr
	}
	return r.accountRepoStubForBulkUpdate.GetByIDs(ctx, ids)
}

func (r *bulkAccountAffectedRowsRepo) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	_, err := r.accountRepoStubForBulkUpdate.BulkUpdate(ctx, ids, updates)
	return r.rows, err
}

func TestBulkAccountRegressionAffectedRowMismatchRequiresConfirmation(t *testing.T) {
	for _, test := range []struct {
		name       string
		rows       int64
		after      []*Account
		afterErr   error
		successIDs []int64
		failedIDs  []int64
	}{
		{name: "partial-concurrent-deletion", rows: 1, after: []*Account{{ID: 1}}, successIDs: []int64{1}, failedIDs: []int64{2}},
		{name: "zero-cannot-claim-existing-targets", after: []*Account{{ID: 1}, {ID: 2}}, failedIDs: []int64{1, 2}},
		{name: "partial-count-with-all-targets-still-live", rows: 1, after: []*Account{{ID: 1}, {ID: 2}}, failedIDs: []int64{1, 2}},
		{name: "reread-failed-after-write", rows: 1, afterErr: errors.New("read unavailable"), failedIDs: []int64{1, 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &bulkAccountAffectedRowsRepo{accountRepoStubForBulkUpdate: &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}}}, rows: test.rows, after: test.after, afterErr: test.afterErr}
			priority := 9
			result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Priority: &priority})
			require.NoError(t, err, "the write may already be committed; expose per-target confirmation failures")
			require.ElementsMatch(t, test.successIDs, result.SuccessIDs)
			require.ElementsMatch(t, test.failedIDs, result.FailedIDs)
			require.Equal(t, len(test.successIDs), result.Success)
			require.Equal(t, len(test.failedIDs), result.Failed)
			require.ElementsMatch(t, test.failedIDs, bulkAccountUnconfirmedIDs(t, result), "a possibly committed update must require refresh before retry")
		})
	}
}

func TestBulkAccountRegressionAllMissingDoesNotWrite(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	priority := 9
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{99, 99}, Priority: &priority})
	require.NoError(t, err)
	require.Empty(t, result.SuccessIDs)
	require.Equal(t, []int64{99}, result.FailedIDs)
	require.Zero(t, repo.bulkUpdateCalls)
}

type bulkAccountAtomicGroupRepo struct {
	*accountRepoStubForBulkUpdate
	atomicCalls []int64
	atomicErrs  map[int64]error
}

func (r *bulkAccountAtomicGroupRepo) BulkUpdateAndBindGroups(_ context.Context, id int64, _ AccountBulkUpdate, _ []int64) (int64, error) {
	r.atomicCalls = append(r.atomicCalls, id)
	if err := r.atomicErrs[id]; err != nil {
		return 0, err
	}
	return 1, nil
}

func TestBulkAccountRegressionCombinedEditsUseAtomicRepository(t *testing.T) {
	repo := &bulkAccountAtomicGroupRepo{accountRepoStubForBulkUpdate: &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}}}, atomicErrs: map[int64]error{2: errors.New("group write failed and rolled back")}}
	priority := 9
	groups := []int64{}
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Priority: &priority, GroupIDs: &groups, SkipMixedChannelCheck: true})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, repo.atomicCalls)
	require.Zero(t, repo.bulkUpdateCalls, "combined edits must not commit columns before group changes")
	require.Empty(t, repo.bindGroupsCalls)
	require.Equal(t, []int64{1}, result.SuccessIDs)
	require.Equal(t, []int64{2}, result.FailedIDs)
	require.Contains(t, result.Results[1].Error, "group write failed")
	require.Empty(t, bulkAccountUnconfirmedIDs(t, result), "atomic rollback failures are safe to retry")
}

func TestBulkAccountRegressionLegacyCombinedFailureDisclosesCommittedColumns(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}}, bindGroupErrByID: map[int64]error{1: errors.New("binding failed")}}
	priority := 9
	groups := []int64{}
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Priority: &priority, GroupIDs: &groups, SkipMixedChannelCheck: true})
	require.NoError(t, err)
	require.Equal(t, []int64{1}, result.FailedIDs)
	require.Contains(t, result.Results[0].Error, "account fields were updated")
	require.Contains(t, result.Results[0].Error, "binding failed")
	require.Equal(t, []int64{1}, bulkAccountUnconfirmedIDs(t, result))
}

type bulkAccountSnapshotFailureRepo struct {
	*accountRepoStubForBulkUpdate
	rows int64
}

func (r *bulkAccountSnapshotFailureRepo) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	_, _ = r.accountRepoStubForBulkUpdate.BulkUpdate(ctx, ids, updates)
	return r.rows, &SchedulerSnapshotSyncError{AccountIDs: ids, Err: errors.New("cache unavailable")}
}

func TestBulkAccountRegressionCommittedSnapshotFailureIsWarningNotRetryableFailure(t *testing.T) {
	repo := &bulkAccountSnapshotFailureRepo{
		accountRepoStubForBulkUpdate: &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}}},
		rows:                         2,
	}
	priority := 9
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Priority: &priority})
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Empty(t, result.FailedIDs)
	require.Equal(t, []int64{1, 2}, result.SuccessIDs)
	require.Contains(t, result.SchedulerSyncWarning, "outbox")
	require.Empty(t, bulkAccountUnconfirmedIDs(t, result), "durable-outbox recovery is already exposed as a warning")
}

func bulkAccountUnconfirmedIDs(t *testing.T, result *BulkUpdateAccountsResult) []int64 {
	t.Helper()
	payload, err := json.Marshal(result)
	require.NoError(t, err)
	var response struct {
		UnconfirmedIDs []int64 `json:"unconfirmed_ids"`
	}
	require.NoError(t, json.Unmarshal(payload, &response))
	return response.UnconfirmedIDs
}

type bulkAccountPostCommitFailureRepo struct {
	*accountRepoStubForBulkUpdate
	rows          int64
	postCommitErr error
	shadowErrs    map[int64]error
}

func (r *bulkAccountPostCommitFailureRepo) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	_, _ = r.accountRepoStubForBulkUpdate.BulkUpdate(ctx, ids, updates)
	return r.rows, r.postCommitErr
}

func (r *bulkAccountPostCommitFailureRepo) ListShadowsByParent(_ context.Context, id int64) ([]*Account, error) {
	return nil, r.shadowErrs[id]
}

func TestBulkAccountRegressionPostCommitFailureCannotBecomeRetryableFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		rows int64
	}{
		{name: "complete-column-write", rows: 2},
		{name: "partial-column-write", rows: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &bulkAccountPostCommitFailureRepo{
				accountRepoStubForBulkUpdate: &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}}},
				rows:                         test.rows, postCommitErr: errors.New("post-commit propagation failed"),
			}
			priority := 9
			result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2, 99}, Priority: &priority})
			require.NoError(t, err, "a nonzero affected-row count cannot be presented as a rolled-back request")
			require.Empty(t, result.SuccessIDs)
			require.Equal(t, []int64{1, 2, 99}, result.FailedIDs)
			require.Equal(t, []int64{1, 2}, bulkAccountUnconfirmedIDs(t, result))
			require.Contains(t, result.Results[0].Error, "post-commit propagation failed")
		})
	}
}

func TestBulkAccountRegressionProxyPropagationFailurePreservesPerTargetResult(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit-ids", true: "filter-ids"}[filtered], func(t *testing.T) {
			repo := &bulkAccountPostCommitFailureRepo{
				accountRepoStubForBulkUpdate: &accountRepoStubForBulkUpdate{
					getByIDsAccounts: []*Account{{ID: 7}, {ID: 11}},
					listData:         []Account{{ID: 7}, {ID: 11}},
				},
				rows: 2, shadowErrs: map[int64]error{7: errors.New("shadow lookup failed")},
			}
			proxyID := int64(9)
			input := &BulkUpdateAccountsInput{AccountIDs: []int64{7, 11}, ProxyID: &proxyID}
			if filtered {
				input.AccountIDs = nil
				input.Filters = &BulkUpdateAccountFilters{Platform: PlatformOpenAI}
			}
			result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), input)
			require.NoError(t, err, "the parent proxy write was committed before shadow propagation")
			require.Equal(t, []int64{11}, result.SuccessIDs)
			require.Equal(t, []int64{7}, result.FailedIDs)
			require.Equal(t, []int64{7}, bulkAccountUnconfirmedIDs(t, result))
			require.Contains(t, result.Results[0].Error, "shadow lookup failed")
		})
	}
}

type bulkAccountAtomicPostCommitFailureRepo struct {
	*accountRepoStubForBulkUpdate
}

func (r *bulkAccountAtomicPostCommitFailureRepo) BulkUpdateAndBindGroups(_ context.Context, id int64, _ AccountBulkUpdate, _ []int64) (int64, error) {
	if id == 1 {
		return 1, errors.New("post-commit follow-up failed")
	}
	return 0, errors.New("group write rolled back")
}

func TestBulkAccountRegressionAtomicAffectedRowsDistinguishCommittedFailureFromRollback(t *testing.T) {
	repo := &bulkAccountAtomicPostCommitFailureRepo{accountRepoStubForBulkUpdate: &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}}}}
	priority := 9
	groups := []int64{}
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Priority: &priority, GroupIDs: &groups, SkipMixedChannelCheck: true})
	require.NoError(t, err)
	require.Empty(t, result.SuccessIDs)
	require.Equal(t, []int64{1, 2}, result.FailedIDs)
	require.Equal(t, []int64{1}, bulkAccountUnconfirmedIDs(t, result))
}

func TestBulkAccountRegressionPreCommitRepositoryFailureRemainsOrdinaryError(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1}}, bulkUpdateErr: errors.New("write rolled back")}
	priority := 9
	result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Priority: &priority})
	require.Nil(t, result)
	require.EqualError(t, err, "write rolled back")
}
