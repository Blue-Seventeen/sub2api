package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentSubscriptionOrderAlreadyAppliedMatchesExactNoteLine(t *testing.T) {
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		UserID:  10,
		GroupID: 20,
		Notes:   "manual note\npayment order 123\npayment order 1234",
	})
	svc := &PaymentService{
		subscriptionSvc: &SubscriptionService{userSubRepo: repo},
	}

	require.True(t, svc.subscriptionOrderAlreadyApplied(context.Background(), 10, 20, paymentSubscriptionOrderNote(123)))
	require.False(t, svc.subscriptionOrderAlreadyApplied(context.Background(), 10, 20, paymentSubscriptionOrderNote(12)))
	require.False(t, svc.subscriptionOrderAlreadyApplied(context.Background(), 10, 21, paymentSubscriptionOrderNote(123)))
}
