package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type globalCreateOperationsRepoStub struct {
	GroupRepository
	source  *Group
	summary *GlobalModelOperationSummary
}

func (r *globalCreateOperationsRepoStub) CreateWithGlobalModelOperations(_ context.Context, group *Group, _ []GroupModelOperation) (*GlobalModelOperationSummary, error) {
	group.ID = 40
	return r.summary, nil
}

func (*globalCreateOperationsRepoStub) UpdateWithGlobalModelOperations(context.Context, *Group, []GroupModelOperation) (*GlobalModelOperationSummary, error) {
	panic("unexpected update global model operations")
}

func (r *globalCreateOperationsRepoStub) GetByIDLite(_ context.Context, _ int64) (*Group, error) {
	return r.source, nil
}

func (r *globalCreateOperationsRepoStub) GetAccountIDsByGroupIDs(_ context.Context, _ []int64) ([]int64, error) {
	return []int64{91}, nil
}

type failingGlobalCreateAccountRepo struct{ AccountRepository }

func (failingGlobalCreateAccountRepo) GetByIDs(_ context.Context, _ []int64) ([]*Account, error) {
	return nil, errors.New("account lookup failed")
}

type globalCreateInvalidatorStub struct{ groupIDs []int64 }

func (*globalCreateInvalidatorStub) InvalidateAuthCacheByKey(context.Context, string)   {}
func (*globalCreateInvalidatorStub) InvalidateAuthCacheByUserID(context.Context, int64) {}
func (s *globalCreateInvalidatorStub) InvalidateAuthCacheByGroupID(_ context.Context, groupID int64) {
	s.groupIDs = append(s.groupIDs, groupID)
}

func TestCreateGroupGlobalOperationsInvalidateCachesBeforeAccountCopyFailure(t *testing.T) {
	repo := &globalCreateOperationsRepoStub{
		source:  &Group{ID: 7, Platform: PlatformOpenAI},
		summary: &GlobalModelOperationSummary{TargetPlatform: PlatformOpenAI, AffectedGroupIDs: []int64{40, 41}},
	}
	invalidator := &globalCreateInvalidatorStub{}
	svc := &adminServiceImpl{
		groupRepo:            repo,
		accountRepo:          failingGlobalCreateAccountRepo{},
		authCacheInvalidator: invalidator,
	}
	requireOAuthOnly := true

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                     "global-cache-before-copy",
		Platform:                 PlatformOpenAI,
		RateMultiplier:           1,
		RequireOAuthOnly:         requireOAuthOnly,
		CopyAccountsFromGroupIDs: []int64{7},
		GlobalModelOperations:    []GroupModelOperation{{Operation: "add", Model: "gpt-5.4"}},
	})

	require.ErrorContains(t, err, "account lookup failed")
	require.Equal(t, []int64{40, 41}, invalidator.groupIDs)
}
