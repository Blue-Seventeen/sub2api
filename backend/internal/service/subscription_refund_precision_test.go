//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

// PostgreSQL persists timestamptz at microsecond precision. The repository's
// Update contract does not promise to replace the caller's in-memory expiry.
type microsecondRefundSubscriptionRepo struct {
	*pendingRefundSubscriptionRepo
}

func (r *microsecondRefundSubscriptionRepo) Update(ctx context.Context, sub *UserSubscription) error {
	persisted := cloneUserSubscription(*sub)
	persisted.ExpiresAt = persisted.ExpiresAt.Round(time.Microsecond)
	return r.pendingRefundSubscriptionRepo.Update(ctx, &persisted)
}

func (r *microsecondRefundSubscriptionRepo) UpdateMutableFields(ctx context.Context, id int64, fields UserSubscriptionMutableFields) error {
	if fields.ExpiresAt != nil {
		expires := fields.ExpiresAt.Round(time.Microsecond)
		fields.ExpiresAt = &expires
	}
	return r.pendingRefundSubscriptionRepo.UpdateMutableFields(ctx, id, fields)
}

func TestPendingSubscriptionRefundFailureRestoresMicrosecondPrecisionTerms(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createCompletedRefundableOrderForTest(t, ctx, client, "microsecond-refund")
	group, err := client.Group.Create().SetName("microsecond-refund-group").SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(group.ID).SetSubscriptionDays(3).Save(ctx)
	require.NoError(t, err)
	now := time.Date(2030, 3, 1, 12, 0, 0, 123456789, time.UTC)
	before := make([]*dbent.UserSubscription, 0, 2)
	for i := 0; i < 2; i++ {
		sub, createErr := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
			SetStartsAt(now.Add(-time.Hour).Round(time.Microsecond)).
			SetExpiresAt(now.Add(time.Duration(3-i) * 24 * time.Hour).Round(time.Microsecond)).
			SetStatus(SubscriptionStatusActive).SetNotes("original note").Save(ctx)
		require.NoError(t, createErr)
		before = append(before, sub)
	}
	repo := &microsecondRefundSubscriptionRepo{&pendingRefundSubscriptionRepo{client: client}}
	service := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, client, nil)}
	service.subscriptionSvc.now = func() time.Time { return now }
	restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed})
	defer restore()
	plan, early, err := service.PrepareRefund(ctx, order.ID, 0, "refund", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := service.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "pending")
	// A restart must recover the exact persisted terms solely from the audit.
	service = &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, client, nil)}
	service.subscriptionSvc.now = func() time.Time { return now }
	result, err = service.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	for _, original := range before {
		after, getErr := client.UserSubscription.Get(ctx, original.ID)
		require.NoError(t, getErr)
		require.Equal(t, SubscriptionStatusActive, after.Status, "card %d must be reactivated", original.ID)
		require.True(t, original.ExpiresAt.Equal(after.ExpiresAt), "card %d must recover its exact persisted expiry", original.ID)
	}
}
