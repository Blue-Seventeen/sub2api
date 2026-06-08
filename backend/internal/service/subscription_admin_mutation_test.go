package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type adminMutationSubRepoStub struct {
	userSubRepoNoop

	sub             *UserSubscription
	restoreCalls    int
	hardDeleteCalls int
}

func (r *adminMutationSubRepoStub) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	if r.sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *adminMutationSubRepoStub) Restore(context.Context, int64) error {
	if r.sub == nil {
		return ErrSubscriptionNotFound
	}
	r.restoreCalls++
	r.sub.DeletedAt = nil
	r.sub.Status = SubscriptionStatusActive
	return nil
}

func (r *adminMutationSubRepoStub) HardDelete(context.Context, int64) error {
	if r.sub == nil {
		return ErrSubscriptionNotFound
	}
	r.hardDeleteCalls++
	r.sub = nil
	return nil
}

func TestRestoreSubscription_RestoresSoftDeletedFutureSubscriptionAsActive(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        10,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusRevoked,
			ExpiresAt: time.Now().Add(time.Hour),
			DeletedAt: &deletedAt,
		},
	}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{
		ID:               200,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeSubscription,
	}}, repo, nil, nil, nil)

	sub, err := svc.RestoreSubscription(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, 1, repo.restoreCalls)
	require.Nil(t, repo.sub.DeletedAt)
	require.Equal(t, SubscriptionStatusActive, repo.sub.Status)
	require.Equal(t, SubscriptionStatusActive, sub.Status)
}

func TestRestoreSubscription_RejectsFutureSubscriptionWhenGroupMissing(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        16,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusRevoked,
			ExpiresAt: time.Now().Add(time.Hour),
			DeletedAt: &deletedAt,
		},
	}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{}, repo, nil, nil, nil)

	_, err := svc.RestoreSubscription(context.Background(), 16)

	require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
	require.Zero(t, repo.restoreCalls)
}

func TestRestoreSubscription_RejectsFutureSubscriptionWhenGroupInactive(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        17,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusRevoked,
			ExpiresAt: time.Now().Add(time.Hour),
			DeletedAt: &deletedAt,
		},
	}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{
		ID:               200,
		Status:           StatusDisabled,
		SubscriptionType: SubscriptionTypeSubscription,
	}}, repo, nil, nil, nil)

	_, err := svc.RestoreSubscription(context.Background(), 17)

	require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
	require.Zero(t, repo.restoreCalls)
}

func TestRestoreSubscription_RejectsFutureSubscriptionWhenGroupNotSubscriptionType(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        18,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusRevoked,
			ExpiresAt: time.Now().Add(time.Hour),
			DeletedAt: &deletedAt,
		},
	}
	svc := NewSubscriptionService(&subscriptionGroupRepoStub{group: &Group{
		ID:               200,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeStandard,
	}}, repo, nil, nil, nil)

	_, err := svc.RestoreSubscription(context.Background(), 18)

	require.ErrorIs(t, err, ErrSubscriptionRestoreGroupInvalid)
	require.Zero(t, repo.restoreCalls)
}

func TestRestoreSubscription_RestoresExpiredSubscriptionButDisplaysExpired(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        11,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusRevoked,
			ExpiresAt: time.Now().Add(-time.Hour),
			DeletedAt: &deletedAt,
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	sub, err := svc.RestoreSubscription(context.Background(), 11)

	require.NoError(t, err)
	require.Equal(t, SubscriptionStatusActive, repo.sub.Status)
	require.Equal(t, SubscriptionStatusExpired, sub.Status)
}

func TestRestoreSubscription_RejectsNonRevokedNonDeletedSubscription(t *testing.T) {
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        12,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	_, err := svc.RestoreSubscription(context.Background(), 12)

	require.ErrorIs(t, err, ErrSubscriptionRestoreInvalid)
	require.Zero(t, repo.restoreCalls)
}

func TestHardDeleteSubscription_AllowsRevokedSoftDeletedSubscription(t *testing.T) {
	deletedAt := time.Now().Add(-time.Hour)
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        13,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusRevoked,
			ExpiresAt: time.Now().Add(time.Hour),
			DeletedAt: &deletedAt,
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	err := svc.HardDeleteSubscription(context.Background(), 13)

	require.NoError(t, err)
	require.Equal(t, 1, repo.hardDeleteCalls)
	require.Nil(t, repo.sub)
}

func TestHardDeleteSubscription_AllowsExpiredActiveSubscription(t *testing.T) {
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        14,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(-time.Hour),
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	err := svc.HardDeleteSubscription(context.Background(), 14)

	require.NoError(t, err)
	require.Equal(t, 1, repo.hardDeleteCalls)
	require.Nil(t, repo.sub)
}

func TestHardDeleteSubscription_RejectsActiveFutureSubscription(t *testing.T) {
	repo := &adminMutationSubRepoStub{
		sub: &UserSubscription{
			ID:        15,
			UserID:    100,
			GroupID:   200,
			Status:    SubscriptionStatusActive,
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)

	err := svc.HardDeleteSubscription(context.Background(), 15)

	require.ErrorIs(t, err, ErrSubscriptionHardDeleteInvalid)
	require.Zero(t, repo.hardDeleteCalls)
}
