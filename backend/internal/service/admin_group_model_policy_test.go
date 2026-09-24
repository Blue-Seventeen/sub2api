package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type policyGlobalWriteRepo struct {
	GroupRepository
	t           *testing.T
	invalidator *globalCreateInvalidatorStub
	err         error
	calls       int
}

func (r *policyGlobalWriteRepo) GetByID(context.Context, int64) (*Group, error) {
	return &Group{ID: 40, Name: "policy", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1}, nil
}

func (r *policyGlobalWriteRepo) CreateWithGlobalModelOperations(_ context.Context, group *Group, operations []GroupModelOperation) (*GlobalModelOperationSummary, error) {
	r.calls++
	require.Empty(r.t, r.invalidator.groupIDs, "cache invalidation must follow successful persistence")
	require.Equal(r.t, []GroupModelOperation{{Operation: "add", Model: "gpt-*"}}, operations)
	if r.err != nil {
		return nil, r.err
	}
	group.ID = 40
	return &GlobalModelOperationSummary{TargetPlatform: PlatformOpenAI, AffectedGroupIDs: []int64{40, 41, 42}}, nil
}

func (r *policyGlobalWriteRepo) UpdateWithGlobalModelOperations(ctx context.Context, group *Group, operations []GroupModelOperation) (*GlobalModelOperationSummary, error) {
	return r.CreateWithGlobalModelOperations(ctx, group, operations)
}

func TestAdminGroupGlobalPolicyInvalidationFollowsSuccessfulWrite(t *testing.T) {
	for _, method := range []string{"create", "update"} {
		for _, failed := range []bool{false, true} {
			name := method + "/success"
			if failed {
				name = method + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				invalidator := &globalCreateInvalidatorStub{}
				repo := &policyGlobalWriteRepo{t: t, invalidator: invalidator}
				if failed {
					repo.err = errors.New("global transaction rejected")
				}
				svc := &adminServiceImpl{groupRepo: repo, authCacheInvalidator: invalidator}
				operations := []GroupModelOperation{{Operation: "add", Model: "gpt-*"}}
				var err error
				if method == "create" {
					_, err = svc.CreateGroup(context.Background(), &CreateGroupInput{
						Name: "policy", Platform: PlatformOpenAI, RateMultiplier: 1, GlobalModelOperations: operations,
					})
				} else {
					_, err = svc.UpdateGroup(context.Background(), 40, &UpdateGroupInput{GlobalModelOperations: operations})
				}
				require.Equal(t, 1, repo.calls)
				if failed {
					require.ErrorIs(t, err, repo.err)
					require.Empty(t, invalidator.groupIDs)
				} else {
					require.NoError(t, err)
					require.Equal(t, []int64{40, 41, 42}, invalidator.groupIDs)
				}
			})
		}
	}
}

func TestDuplicateGroupPolicyNeverRevivesLegacyMirror(t *testing.T) {
	for _, canonical := range []GroupModelsListConfig{
		{}, {Models: []string{"disabled-canonical"}}, {Enabled: true, Models: []string{"enabled-canonical"}},
	} {
		source := &Group{ModelsListConfig: canonical, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"legacy"}}}
		duplicate := cloneGroupForDuplicate(source, "")
		require.Equal(t, canonical, duplicate.ModelsListConfig)
		require.Equal(t, canonical.Enabled, duplicate.ModelAllowlist.Enabled)
		require.Equal(t, canonical.Models, duplicate.ModelAllowlist.Models)
		if len(canonical.Models) > 0 {
			duplicate.ModelsListConfig.Models[0] = "changed"
			require.NotEqual(t, "changed", source.ModelsListConfig.Models[0])
			require.NotEqual(t, "changed", duplicate.ModelAllowlist.Models[0])
		}
	}
}
