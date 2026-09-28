//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAdminService_CreateGroup_RejectsEmptyEnabledModelAllowlist(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 51}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:           "allowlist-empty-group",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		ModelAllowlist: GroupModelAllowlist{Enabled: true},
	})

	require.Error(t, err)
	appErr := infraerrors.FromError(err)
	require.Equal(t, int32(http.StatusBadRequest), appErr.Code)
	require.Equal(t, "INVALID_MODEL_ALLOWLIST", appErr.Reason)
	require.Nil(t, repo.created, "拒绝时不得落库")
}

func TestAdminService_CreateGroup_AcceptsInteriorAllowlistWildcard(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 51}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:           "allowlist-wildcard-group",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-*-5.4"}},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.Equal(t, []string{"gpt-*-5.4"}, repo.created.ModelAllowlist.Models)
}

func TestAdminService_CreateGroup_NormalizesModelAllowlist(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 52}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:           "allowlist-normalized-group",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		ModelAllowlist: GroupModelAllowlist{
			Enabled: true,
			Models:  []string{" gpt-5.4 ", "GPT-5.4", "claude-*", "  "},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.True(t, repo.created.ModelAllowlist.Enabled)
	require.Equal(t, []string{"gpt-5.4", "claude-*"}, repo.created.ModelAllowlist.Models)
}

func TestAdminService_CreateGroup_CanonicalExplicitEmptyWinsOverLegacyAlias(t *testing.T) {
	repo := &groupRepoStubForAdmin{createID: 53}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                "canonical-empty-group",
		Platform:            PlatformOpenAI,
		RateMultiplier:      1,
		ModelsListConfigSet: true,
		ModelsListConfig:    GroupModelsListConfig{Enabled: false},
		ModelAllowlist:      GroupModelAllowlist{Enabled: true, Models: []string{"legacy-only"}},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	require.False(t, repo.created.ModelsListConfig.Enabled)
	require.Empty(t, repo.created.ModelsListConfig.Models)
	require.Equal(t, repo.created.ModelsListConfig, GroupModelsListConfig{Enabled: repo.created.ModelAllowlist.Enabled, Models: repo.created.ModelAllowlist.Models})
}

type globalModelAdminRepoStub struct {
	groupRepoStubForAdmin
	summary *GlobalModelOperationSummary
}

func (r *globalModelAdminRepoStub) CreateWithGlobalModelOperations(_ context.Context, group *Group, _ []GroupModelOperation) (*GlobalModelOperationSummary, error) {
	if r.createID > 0 {
		group.ID = r.createID
	}
	r.created = group
	return r.summary, nil
}

func (r *globalModelAdminRepoStub) UpdateWithGlobalModelOperations(_ context.Context, group *Group, _ []GroupModelOperation) (*GlobalModelOperationSummary, error) {
	group.ModelsListConfig = GroupModelsListConfig{Enabled: true, Models: []string{"canonical-final"}}
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"canonical-final"}}
	r.updated = group
	return r.summary, nil
}

func TestAdminService_UpdateGroup_GlobalOperationKeepsReturnedCurrentGroupInSync(t *testing.T) {
	existing := &Group{ID: 9, Name: "global", Platform: PlatformOpenAI, Status: StatusActive}
	repo := &globalModelAdminRepoStub{summary: &GlobalModelOperationSummary{TargetPlatform: PlatformOpenAI, AffectedGroupIDs: []int64{9}}}
	repo.getByID = existing
	svc := &adminServiceImpl{groupRepo: repo}

	got, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		GlobalModelOperations: []GroupModelOperation{{Operation: "add", Model: "canonical-final"}},
	})

	require.NoError(t, err)
	require.Equal(t, GroupModelsListConfig{Enabled: true, Models: []string{"canonical-final"}}, got.ModelsListConfig)
	require.Equal(t, GroupModelAllowlist{Enabled: true, Models: []string{"canonical-final"}}, got.ModelAllowlist)
}

type failingAccountLookupForGlobalCreate struct{ AccountRepository }

func (failingAccountLookupForGlobalCreate) GetByIDs(_ context.Context, _ []int64) ([]*Account, error) {
	return nil, errors.New("account lookup failed")
}

func TestAdminService_CreateGroup_GlobalOperationInvalidatesCachesBeforeAccountCopyFailure(t *testing.T) {
	repo := &globalModelAdminRepoStub{
		groupRepoStubForAdmin: groupRepoStubForAdmin{
			createID:                  40,
			getByIDByID:               map[int64]*Group{7: {ID: 7, Platform: PlatformOpenAI}},
			getAccountIDsByGroupIDsFn: func([]int64) ([]int64, error) { return []int64{91}, nil },
		},
		summary: &GlobalModelOperationSummary{TargetPlatform: PlatformOpenAI, AffectedGroupIDs: []int64{40, 41}},
	}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		groupRepo:            repo,
		accountRepo:          failingAccountLookupForGlobalCreate{},
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

func TestAdminService_UpdateGroupDoesNotPromoteStaleMirrorIntoHydratedEmptyCanonical(t *testing.T) {
	existing := &Group{
		ID:               12,
		Name:             "canonical-empty",
		Platform:         PlatformOpenAI,
		Status:           StatusActive,
		Hydrated:         true,
		ModelsListConfig: GroupModelsListConfig{},
		ModelAllowlist:   GroupModelAllowlist{Enabled: true, Models: []string{"stale-legacy"}},
	}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{Name: "canonical-empty-updated"})

	require.NoError(t, err)
	require.Equal(t, GroupModelsListConfig{}, repo.updated.ModelsListConfig)
	require.Equal(t, GroupModelAllowlist{}, repo.updated.ModelAllowlist)
}

func TestAdminService_UpdateGroup_RejectsEmptyEnabledModelAllowlist(t *testing.T) {
	existing := &Group{ID: 1, Name: "existing", Platform: PlatformOpenAI, Status: StatusActive}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		ModelAllowlist: &GroupModelAllowlist{Enabled: true},
	})

	require.Error(t, err)
	appErr := infraerrors.FromError(err)
	require.Equal(t, int32(http.StatusBadRequest), appErr.Code)
	require.Equal(t, "INVALID_MODEL_ALLOWLIST", appErr.Reason)
	require.Nil(t, repo.updated, "拒绝时不得落库")
}

func TestAdminService_UpdateGroup_AcceptsInteriorAllowlistWildcard(t *testing.T) {
	existing := &Group{ID: 1, Name: "existing", Platform: PlatformOpenAI, Status: StatusActive}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		ModelAllowlist: &GroupModelAllowlist{Enabled: true, Models: []string{"foo-*bar"}},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, []string{"foo-*bar"}, repo.updated.ModelAllowlist.Models)
}

func TestAdminService_UpdateGroup_NormalizesAndResetsModelAllowlist(t *testing.T) {
	existing := &Group{
		ID: 1, Name: "existing", Platform: PlatformOpenAI, Status: StatusActive,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}},
	}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: repo}

	// 归一化：按小写去重保序（保留首次出现的原始拼写）。
	updated := GroupModelAllowlist{Enabled: true, Models: []string{" GPT-5.4 ", "claude-*"}}
	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{ModelAllowlist: &updated})
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, []string{"GPT-5.4", "claude-*"}, repo.updated.ModelAllowlist.Models)

	// 关闭且清空条目也应被接受（关闭白名单）。
	repo.updated = nil
	disabled := GroupModelAllowlist{Enabled: false}
	_, err = svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{ModelAllowlist: &disabled})
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.False(t, repo.updated.ModelAllowlist.Enabled)
	require.Empty(t, repo.updated.ModelAllowlist.Models)
}
