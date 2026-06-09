package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
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

func TestPaymentSubscriptionOrderAlreadyAppliedScansHistoricalCards(t *testing.T) {
	repo := newSubscriptionUserSubRepoStub()
	repo.seed(&UserSubscription{
		ID:      1,
		UserID:  10,
		GroupID: 20,
		Notes:   "payment order 123",
	})
	repo.seed(&UserSubscription{
		ID:      2,
		UserID:  10,
		GroupID: 20,
		Notes:   "payment order 456",
	})
	svc := &PaymentService{
		subscriptionSvc: &SubscriptionService{userSubRepo: repo},
	}

	require.True(t, svc.subscriptionOrderAlreadyApplied(context.Background(), 10, 20, paymentSubscriptionOrderNote(123)))
	require.True(t, svc.subscriptionOrderAlreadyApplied(context.Background(), 10, 20, paymentSubscriptionOrderNote(456)))
	require.False(t, svc.subscriptionOrderAlreadyApplied(context.Background(), 10, 21, paymentSubscriptionOrderNote(123)))
}

func TestPaymentSubscriptionFulfillmentStacksOrdersAndKeepsOrderIdempotency(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("subscription-payment-stack@example.com").
		SetPasswordHash("hash").
		SetUsername("subscription-payment-stack").
		Save(ctx)
	require.NoError(t, err)
	groupID := int64(20)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{
			ID:               groupID,
			Name:             "subscription-payment-stack-group",
			Status:           StatusActive,
			SubscriptionType: SubscriptionTypeSubscription,
			Platform:         PlatformAnthropic,
			RateMultiplier:   1,
		},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subSvc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	svc := &PaymentService{
		entClient:       client,
		groupRepo:       groupRepo,
		subscriptionSvc: subSvc,
	}

	order1 := createPaidSubscriptionOrderForTest(t, ctx, client, user.ID, user.Email, user.Username, groupID, "SUB-PAY-STACK-1")
	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, order1.ID))
	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, order1.ID))

	subs, err := subRepo.ListActiveByUserIDAndGroupID(ctx, user.ID, groupID)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.NotNil(t, subs[0].SourceType)
	require.NotNil(t, subs[0].SourceRefID)
	require.Equal(t, "payment_order", *subs[0].SourceType)
	require.Equal(t, strconv.FormatInt(order1.ID, 10), *subs[0].SourceRefID)

	order2 := createPaidSubscriptionOrderForTest(t, ctx, client, user.ID, user.Email, user.Username, groupID, "SUB-PAY-STACK-2")
	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, order2.ID))

	subs, err = subRepo.ListActiveByUserIDAndGroupID(ctx, user.ID, groupID)
	require.NoError(t, err)
	require.Len(t, subs, 2)
	sourceRefs := map[string]bool{}
	for i := range subs {
		require.NotNil(t, subs[i].SourceType)
		require.Equal(t, "payment_order", *subs[i].SourceType)
		require.NotNil(t, subs[i].SourceRefID)
		sourceRefs[*subs[i].SourceRefID] = true
	}
	require.True(t, sourceRefs[strconv.FormatInt(order1.ID, 10)])
	require.True(t, sourceRefs[strconv.FormatInt(order2.ID, 10)])
}

func TestPaymentSubscriptionFulfillmentRetriesExistingSubscriptionBeforeGroupValidation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("subscription-payment-retry@example.com").
		SetPasswordHash("hash").
		SetUsername("subscription-payment-retry").
		Save(ctx)
	require.NoError(t, err)
	groupID := int64(30)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{
			ID:               groupID,
			Name:             "inactive-after-assignment",
			Status:           "disabled",
			SubscriptionType: SubscriptionTypeSubscription,
			Platform:         PlatformAnthropic,
			RateMultiplier:   1,
		},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subSvc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	svc := &PaymentService{
		entClient:       client,
		groupRepo:       groupRepo,
		subscriptionSvc: subSvc,
	}

	order := createPaidSubscriptionOrderForTest(t, ctx, client, user.ID, user.Email, user.Username, groupID, "SUB-PAY-RETRY-EXISTING")
	sourceType := "payment_order"
	sourceRef := strconv.FormatInt(order.ID, 10)
	subRepo.seed(&UserSubscription{
		UserID:      user.ID,
		GroupID:     groupID,
		StartsAt:    time.Now().Add(-time.Minute),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		Status:      SubscriptionStatusActive,
		Notes:       paymentSubscriptionOrderNote(order.ID),
		SourceType:  &sourceType,
		SourceRefID: &sourceRef,
	})

	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, order.ID))
	require.Equal(t, 0, subRepo.createCalls, "retry must not create a duplicate subscription when source already exists")
	refreshed, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, refreshed.Status)
}

func TestPaymentSubscriptionFulfillmentRetriesExistingNoteBeforeGroupValidation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("subscription-payment-retry-note@example.com").
		SetPasswordHash("hash").
		SetUsername("subscription-payment-retry-note").
		Save(ctx)
	require.NoError(t, err)
	groupID := int64(31)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{
			ID:               groupID,
			Name:             "inactive-after-legacy-note",
			Status:           "disabled",
			SubscriptionType: SubscriptionTypeSubscription,
			Platform:         PlatformAnthropic,
			RateMultiplier:   1,
		},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subSvc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	svc := &PaymentService{
		entClient:       client,
		groupRepo:       groupRepo,
		subscriptionSvc: subSvc,
	}

	order := createPaidSubscriptionOrderForTest(t, ctx, client, user.ID, user.Email, user.Username, groupID, "SUB-PAY-RETRY-NOTE")
	subRepo.seed(&UserSubscription{
		UserID:    user.ID,
		GroupID:   groupID,
		StartsAt:  time.Now().Add(-time.Minute),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Status:    SubscriptionStatusActive,
		Notes:     paymentSubscriptionOrderNote(order.ID),
	})

	require.NoError(t, svc.ExecuteSubscriptionFulfillment(ctx, order.ID))
	require.Equal(t, 0, subRepo.createCalls, "legacy notes-only retry must not create a duplicate subscription")
	refreshed, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, refreshed.Status)
}

func createPaidSubscriptionOrderForTest(t *testing.T, ctx context.Context, client *dbent.Client, userID int64, email, username string, groupID int64, code string) *dbent.PaymentOrder {
	t.Helper()
	order, err := client.PaymentOrder.Create().
		SetUserID(userID).
		SetUserEmail(email).
		SetUserName(username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode(code).
		SetOutTradeNo(code).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo(code).
		SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(groupID).
		SetSubscriptionDays(1).
		SetStatus(OrderStatusPaid).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("test.local").
		Save(ctx)
	require.NoError(t, err)
	return order
}
