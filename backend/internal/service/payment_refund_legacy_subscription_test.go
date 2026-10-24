//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type legacyRefundSubscriptionRepo struct {
	*pendingRefundSubscriptionRepo
	failWrite int
	writes    int
}

func (r *legacyRefundSubscriptionRepo) Update(ctx context.Context, sub *UserSubscription) error {
	if err := r.pendingRefundSubscriptionRepo.Update(ctx, sub); err != nil {
		return err
	}
	r.writes++
	if r.writes == r.failWrite {
		return errors.New("injected legacy subscription write failure")
	}
	return nil
}

func (r *legacyRefundSubscriptionRepo) ExtendExpiry(ctx context.Context, id int64, expiresAt time.Time) error {
	_, err := r.db(ctx).UserSubscription.UpdateOneID(id).SetExpiresAt(expiresAt).Save(ctx)
	return err
}

func newLegacySubscriptionRefundFixture(t *testing.T, count, days int) (*pendingSubscriptionRefundFixture, *legacyRefundSubscriptionRepo, time.Time) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderForTest(t, ctx, client, "legacy-subscription")
	group, err := client.Group.Create().SetName("legacy-refund-group").SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(group.ID).SetSubscriptionDays(days).Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	f := &pendingSubscriptionRefundFixture{ctx: ctx, client: client, order: order}
	for i := 0; i < count; i++ {
		sub, createErr := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
			SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(time.Duration(3-i) * 24 * time.Hour)).
			SetStatus(SubscriptionStatusActive).SetNotes("original note").SetDailyUsageUsd(7).
			SetWeeklyUsageUsd(8).SetMonthlyUsageUsd(9).SetCustomUsageUsd(10).Save(ctx)
		require.NoError(t, createErr)
		f.before = append(f.before, sub)
	}
	repo := &legacyRefundSubscriptionRepo{pendingRefundSubscriptionRepo: &pendingRefundSubscriptionRepo{client: client}}
	f.svc = &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, client, nil)}
	f.svc.subscriptionSvc.now = func() time.Time { return now }
	t.Cleanup(replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusSuccess}))
	return f, repo, now
}

func TestLegacyPendingSubscriptionRefundSuccessDeductsTerms(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		days  int
	}{
		{name: "single card", count: 1, days: 1},
		{name: "stacked cards", count: 2, days: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, now := newLegacySubscriptionRefundFixture(t, tc.count, tc.days)
			groupID := *f.order.SubscriptionGroupID
			key := subCacheKey(f.order.UserID, groupID)
			versionBefore := f.svc.subscriptionSvc.subCacheVersion(key)
			var versionDuringAudit uint64
			f.client.PaymentAuditLog.Use(func(next dbent.Mutator) dbent.Mutator {
				return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
					if mutation, ok := m.(*dbent.PaymentAuditLogMutation); ok {
						if action, _ := mutation.Action(); action == "REFUND_SUCCESS" {
							versionDuringAudit = f.svc.subscriptionSvc.subCacheVersion(key)
						}
					}
					return next.Mutate(ctx, m)
				})
			})
			result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, tc.days, result.SubDaysDeducted)
			oldest, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
			require.NoError(t, err)
			require.True(t, oldest.ExpiresAt.Equal(now.Add(2*24*time.Hour)), "oldest card must lose one day")
			require.Equal(t, SubscriptionStatusActive, oldest.Status)
			if tc.count == 2 {
				newest, err := f.client.UserSubscription.Get(f.ctx, f.before[1].ID)
				require.NoError(t, err)
				require.True(t, newest.ExpiresAt.Equal(now), "newest card must be exhausted first")
				require.Equal(t, SubscriptionStatusExpired, newest.Status)
			}
			require.Equal(t, 7.0, oldest.DailyUsageUsd)
			require.Equal(t, 8.0, oldest.WeeklyUsageUsd)
			require.Equal(t, 9.0, oldest.MonthlyUsageUsd)
			require.Equal(t, 10.0, oldest.CustomUsageUsd)
			require.Equal(t, versionBefore, versionDuringAudit, "shared cache must remain valid until the order transaction commits")
			require.Greater(t, f.svc.subscriptionSvc.subCacheVersion(key), versionBefore, "committed deduction must invalidate shared cache")
			order, err := f.client.PaymentOrder.Get(f.ctx, f.order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefunded, order.Status)
			require.Equal(t, 1, countRefundAuditsForTest(t, f.ctx, f.client, order.ID, "REFUND_SUCCESS"))
			_, err = f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
			require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
			afterRepeat, err := f.client.UserSubscription.Get(f.ctx, oldest.ID)
			require.NoError(t, err)
			require.True(t, afterRepeat.ExpiresAt.Equal(oldest.ExpiresAt), "terminal retry must not deduct again")
		})
	}
}

func TestLegacyPendingSubscriptionRefundFailureRollsBackTermsAndStatus(t *testing.T) {
	for _, failure := range []string{"audit", "second card write"} {
		t.Run(failure, func(t *testing.T) {
			f, repo, _ := newLegacySubscriptionRefundFixture(t, 2, 3)
			key := subCacheKey(f.order.UserID, *f.order.SubscriptionGroupID)
			versionBefore := f.svc.subscriptionSvc.subCacheVersion(key)
			if failure == "second card write" {
				repo.failWrite = 2
			} else {
				f.client.PaymentAuditLog.Use(func(next dbent.Mutator) dbent.Mutator {
					return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
						if mutation, ok := m.(*dbent.PaymentAuditLogMutation); ok {
							if action, _ := mutation.Action(); action == "REFUND_SUCCESS" {
								return nil, errors.New("injected legacy refund audit failure")
							}
						}
						return next.Mutate(ctx, m)
					})
				})
			}
			_, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
			require.Error(t, err)
			if failure == "audit" {
				require.ErrorContains(t, err, "injected legacy refund audit failure")
			} else {
				require.ErrorContains(t, err, "injected legacy subscription write failure")
			}
			for _, before := range f.before {
				after, err := f.client.UserSubscription.Get(f.ctx, before.ID)
				require.NoError(t, err)
				require.True(t, before.ExpiresAt.Equal(after.ExpiresAt))
				require.Equal(t, before.Status, after.Status)
				require.Equal(t, before.Notes, after.Notes)
				require.Equal(t, before.DailyUsageUsd, after.DailyUsageUsd)
			}
			order, err := f.client.PaymentOrder.Get(f.ctx, f.order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundPending, order.Status)
			require.Nil(t, order.RefundAt)
			require.Zero(t, countRefundAuditsForTest(t, f.ctx, f.client, order.ID, "REFUND_SUCCESS"))
			require.Equal(t, versionBefore, f.svc.subscriptionSvc.subCacheVersion(key), "rolled back transaction must not invalidate shared cache")
		})
	}
}

func TestLegacyPendingSubscriptionRefundHeldTermsAreNotDeductedAgain(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 2, 3)
	t.Cleanup(replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusSuccess}))
	before := make([]*dbent.UserSubscription, 0, len(f.before))
	for _, card := range f.before {
		row, err := f.client.UserSubscription.Get(f.ctx, card.ID)
		require.NoError(t, err)
		before = append(before, row)
	}
	result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	for _, card := range before {
		after, err := f.client.UserSubscription.Get(f.ctx, card.ID)
		require.NoError(t, err)
		require.True(t, card.ExpiresAt.Equal(after.ExpiresAt))
		require.Equal(t, card.Status, after.Status)
	}
	count, err := f.client.PaymentAuditLog.Query().Where(paymentauditlog.ActionEQ("REFUND_SUCCESS")).Count(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
