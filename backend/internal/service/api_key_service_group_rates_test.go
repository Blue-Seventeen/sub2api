//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type groupRatesUserRepoStub struct {
	UserRepository
	user *User
	err  error
}

func (s *groupRatesUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	return s.user, s.err
}

type groupRatesGroupRepoStub struct {
	GroupRepository
	groups []Group
	err    error
}

func (s *groupRatesGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	return s.groups, s.err
}

type groupRepoWithByIDStub struct {
	*groupRatesGroupRepoStub
	byID map[int64]*Group
}

func (s *groupRepoWithByIDStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if group, ok := s.byID[id]; ok {
		return group, nil
	}
	return nil, nil
}

type groupRatesUserSubRepoStub struct {
	UserSubscriptionRepository
	subs []UserSubscription
	err  error
}

func (s *groupRatesUserSubRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return s.subs, s.err
}

type groupRatesUserGroupRateRepoStub struct {
	UserGroupRateRepository
	rates map[int64]float64
	err   error
}

func (s *groupRatesUserGroupRateRepoStub) GetByUserID(context.Context, int64) (map[int64]float64, error) {
	return s.rates, s.err
}

type groupRatesAPIKeyRepoStub struct {
	authRepoStub
	groupIDs []int64
	err      error
}

func (s *groupRatesAPIKeyRepoStub) ListGroupIDsByUserID(context.Context, int64) ([]int64, error) {
	return s.groupIDs, s.err
}

func TestAPIKeyService_GetUserGroupRates_ReturnsDenseFinalRateMap(t *testing.T) {
	userRepo := &groupRatesUserRepoStub{
		user: &User{
			ID:                    7,
			AllowedGroups:         []int64{2},
			UnifiedRateEnabled:    true,
			UnifiedRateMultiplier: 1.5,
		},
	}
	groupRepo := &groupRatesGroupRepoStub{
		groups: []Group{
			{ID: 1, RateMultiplier: 1.2, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: false},
			{ID: 2, RateMultiplier: 2.0, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true},
			{ID: 3, RateMultiplier: 4.0, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		},
	}
	userSubRepo := &groupRatesUserSubRepoStub{
		subs: []UserSubscription{
			{GroupID: 3},
		},
	}
	rateRepo := &groupRatesUserGroupRateRepoStub{
		rates: map[int64]float64{
			2: 1.8,
		},
	}

	svc := NewAPIKeyService(&groupRatesAPIKeyRepoStub{}, userRepo, groupRepo, userSubRepo, rateRepo, nil, nil)

	rates, err := svc.GetUserGroupRates(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rates, 3)
	require.InDelta(t, 1.8, rates[1], 1e-12)
	require.InDelta(t, 2.7, rates[2], 1e-12)
	require.InDelta(t, 6.0, rates[3], 1e-12)
}

func TestAPIKeyService_GetUserGroupRates_FallsBackToGroupDefaultsWithoutCustomRepo(t *testing.T) {
	userRepo := &groupRatesUserRepoStub{
		user: &User{
			ID:                    9,
			UnifiedRateEnabled:    true,
			UnifiedRateMultiplier: 2,
		},
	}
	groupRepo := &groupRatesGroupRepoStub{
		groups: []Group{
			{ID: 11, RateMultiplier: 0.75, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: false},
		},
	}
	userSubRepo := &groupRatesUserSubRepoStub{}

	svc := NewAPIKeyService(&groupRatesAPIKeyRepoStub{}, userRepo, groupRepo, userSubRepo, nil, nil, nil)

	rates, err := svc.GetUserGroupRates(context.Background(), 9)
	require.NoError(t, err)
	require.Len(t, rates, 1)
	require.InDelta(t, 1.5, rates[11], 1e-12)
}

func TestAPIKeyService_GetUserGroupRates_IncludesBoundButCurrentlyUnavailableGroups(t *testing.T) {
	userRepo := &groupRatesUserRepoStub{
		user: &User{
			ID:                    12,
			AllowedGroups:         []int64{},
			UnifiedRateEnabled:    true,
			UnifiedRateMultiplier: 2,
		},
	}
	groupRepo := &groupRatesGroupRepoStub{
		groups: []Group{
			{ID: 1, RateMultiplier: 1.1, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: false},
		},
	}
	userSubRepo := &groupRatesUserSubRepoStub{}
	rateRepo := &groupRatesUserGroupRateRepoStub{
		rates: map[int64]float64{
			2: 0.9,
		},
	}
	apiKeyRepo := &groupRatesAPIKeyRepoStub{
		groupIDs: []int64{2},
	}
	groupRepoByID := &groupRepoWithByIDStub{
		groupRatesGroupRepoStub: groupRepo,
		byID: map[int64]*Group{
			2: {ID: 2, RateMultiplier: 1.3, Status: "inactive", SubscriptionType: SubscriptionTypeStandard, IsExclusive: true},
		},
	}

	svc := NewAPIKeyService(apiKeyRepo, userRepo, groupRepoByID, userSubRepo, rateRepo, nil, nil)

	rates, err := svc.GetUserGroupRates(context.Background(), 12)
	require.NoError(t, err)
	require.Len(t, rates, 2)
	require.InDelta(t, 2.2, rates[1], 1e-12)
	require.InDelta(t, 1.8, rates[2], 1e-12)
}
