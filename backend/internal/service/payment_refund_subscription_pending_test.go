//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

// This adapter persists the subscription side of the flow in the same SQLite
// database as the real order/audit services, so transaction rollback is observable.
// PostgreSQL's FOR UPDATE syntax is the only part omitted by the SQLite adapter.
type pendingRefundSubscriptionRepo struct {
	UserSubscriptionRepository
	client *dbent.Client
}

func (r *pendingRefundSubscriptionRepo) db(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}

func pendingRefundSubscription(m *dbent.UserSubscription) *UserSubscription {
	return &UserSubscription{
		ID: m.ID, UserID: m.UserID, GroupID: m.GroupID, StartsAt: m.StartsAt,
		ExpiresAt: m.ExpiresAt, Status: m.Status, AssignedAt: m.AssignedAt,
		Notes: psStringValue(m.Notes), DailyUsageUSD: m.DailyUsageUsd,
		WeeklyUsageUSD: m.WeeklyUsageUsd, MonthlyUsageUSD: m.MonthlyUsageUsd,
		CustomUsageUSD: m.CustomUsageUsd, DailyWindowStart: m.DailyWindowStart,
		WeeklyWindowStart: m.WeeklyWindowStart, MonthlyWindowStart: m.MonthlyWindowStart,
		CustomWindowStart: m.CustomWindowStart,
	}
}

func (r *pendingRefundSubscriptionRepo) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	m, err := r.db(ctx).UserSubscription.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return pendingRefundSubscription(m), nil
}

func (r *pendingRefundSubscriptionRepo) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	return r.GetByID(ctx, id)
}

func (r *pendingRefundSubscriptionRepo) ListActiveByUserIDAndGroupID(ctx context.Context, uid, gid int64) ([]UserSubscription, error) {
	rows, err := r.db(ctx).UserSubscription.Query().Where(
		usersubscription.UserIDEQ(uid), usersubscription.GroupIDEQ(gid),
		usersubscription.StatusEQ(SubscriptionStatusActive), usersubscription.ExpiresAtGT(time.Now()),
	).Order(usersubscription.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserSubscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, *pendingRefundSubscription(row))
	}
	return out, nil
}

func (r *pendingRefundSubscriptionRepo) Update(ctx context.Context, s *UserSubscription) error {
	_, err := r.db(ctx).UserSubscription.UpdateOneID(s.ID).
		SetExpiresAt(s.ExpiresAt).SetStatus(s.Status).SetNotes(s.Notes).
		SetDailyUsageUsd(s.DailyUsageUSD).SetWeeklyUsageUsd(s.WeeklyUsageUSD).
		SetMonthlyUsageUsd(s.MonthlyUsageUSD).SetCustomUsageUsd(s.CustomUsageUSD).
		SetNillableDailyWindowStart(s.DailyWindowStart).SetNillableWeeklyWindowStart(s.WeeklyWindowStart).
		SetNillableMonthlyWindowStart(s.MonthlyWindowStart).SetNillableCustomWindowStart(s.CustomWindowStart).
		Save(ctx)
	return err
}

func (r *pendingRefundSubscriptionRepo) UpdateMutableFields(ctx context.Context, id int64, f UserSubscriptionMutableFields) error {
	b := r.db(ctx).UserSubscription.UpdateOneID(id)
	if f.ExpiresAt != nil {
		b.SetExpiresAt(*f.ExpiresAt)
	}
	if f.Status != nil {
		b.SetStatus(*f.Status)
	}
	if f.Notes != nil {
		b.SetNotes(*f.Notes)
	}
	_, err := b.Save(ctx)
	return err
}

type pendingSubscriptionRefundFixture struct {
	ctx    context.Context
	client *dbent.Client
	svc    *PaymentService
	order  *dbent.PaymentOrder
	before []*dbent.UserSubscription
}

func newPendingSubscriptionRefundFixture(t *testing.T, count, days int, fixedNow ...time.Time) *pendingSubscriptionRefundFixture {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createCompletedRefundableOrderForTest(t, ctx, client, "subscription-pending")
	group, err := client.Group.Create().SetName("pending-refund-group").SetSubscriptionType(SubscriptionTypeSubscription).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(group.ID).SetSubscriptionDays(days).Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	if len(fixedNow) > 0 {
		now = fixedNow[0]
	}
	f := &pendingSubscriptionRefundFixture{ctx: ctx, client: client, order: order}
	for i := 0; i < count; i++ {
		// The newest card expires in 2 days, so a 3-day refund affects both cards.
		sub, createErr := client.UserSubscription.Create().SetUserID(order.UserID).SetGroupID(group.ID).
			SetStartsAt(now.Add(-time.Hour)).SetExpiresAt(now.Add(time.Duration(3-i) * 24 * time.Hour)).
			SetStatus(SubscriptionStatusActive).SetNotes("original note").SetDailyUsageUsd(7).
			SetWeeklyUsageUsd(8).SetMonthlyUsageUsd(9).SetCustomUsageUsd(10).
			SetDailyWindowStart(now).SetWeeklyWindowStart(now).SetMonthlyWindowStart(now).SetCustomWindowStart(now).
			Save(ctx)
		require.NoError(t, createErr)
		f.before = append(f.before, sub)
	}
	repo := &pendingRefundSubscriptionRepo{client: client}
	f.svc = &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, client, nil)}
	if len(fixedNow) > 0 {
		f.svc.subscriptionSvc.now = func() time.Time { return now }
	}
	restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed})
	t.Cleanup(restore)
	plan, early, err := f.svc.PrepareRefund(ctx, order.ID, 0, "refund", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	if count > 1 {
		require.Zero(t, plan.SubscriptionID, "exercise the actual aggregate ID=0 path")
	}
	result, err := f.svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "pending")
	// A fresh service must recover the compensation entirely from persisted audit.
	f.svc = &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, client, nil)}
	if len(fixedNow) > 0 {
		f.svc.subscriptionSvc.now = func() time.Time { return now }
	}
	return f
}

func TestPendingSubscriptionRefundFailureReactivatesNaturallyExpiredHeldTerm(t *testing.T) {
	start := time.Date(2030, 3, 1, 12, 0, 0, 0, time.UTC)
	f := newPendingSubscriptionRefundFixture(t, 1, 1, start)
	settledAt := time.Date(2030, 3, 4, 0, 0, 0, 0, time.UTC)
	f.svc.subscriptionSvc.now = func() time.Time { return settledAt }
	// The held term expires March 3. Simulate the expiry worker advancing only
	// status before the gateway reports failure; the original March 4 noon
	// expiry is still in the future when the held day is returned.
	changed, err := f.client.UserSubscription.Update().Where(
		usersubscription.IDEQ(f.before[0].ID), usersubscription.StatusEQ(SubscriptionStatusActive),
		usersubscription.ExpiresAtLTE(settledAt),
	).SetStatus(SubscriptionStatusExpired).Save(f.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, changed)
	result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, f.before[0].ExpiresAt.Equal(after.ExpiresAt))
	require.Equal(t, SubscriptionStatusActive, after.Status)
}

// A deterministic interleaving: an unlocked read can observe the held term,
// then refund compensation commits before the admin writes its absolute expiry.
// A lock read must instead wait and use the compensated current term.
type refundInterleavedAdminSubscriptionRepo struct {
	*pendingRefundSubscriptionRepo
	beforeLock     func()
	afterRead      func()
	failAfterWrite bool
}

func (r *refundInterleavedAdminSubscriptionRepo) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	sub, err := r.pendingRefundSubscriptionRepo.GetByID(ctx, id)
	if err == nil && r.afterRead != nil {
		fn := r.afterRead
		r.afterRead = nil
		fn()
	}
	return sub, err
}

func (r *refundInterleavedAdminSubscriptionRepo) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	if r.beforeLock != nil {
		fn := r.beforeLock
		r.beforeLock = nil
		r.afterRead = nil
		fn()
	}
	return r.pendingRefundSubscriptionRepo.GetByIDForUpdate(ctx, id)
}

func (r *refundInterleavedAdminSubscriptionRepo) UpdateMutableFields(ctx context.Context, id int64, fields UserSubscriptionMutableFields) error {
	if err := r.pendingRefundSubscriptionRepo.UpdateMutableFields(ctx, id, fields); err != nil {
		return err
	}
	if r.failAfterWrite {
		return errors.New("injected admin failure after expiry write")
	}
	return nil
}

func TestAdjustSubscriptionPreservesInterleavedRefundCompensation(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 1)
	compensate := func() {
		result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
		require.NoError(t, err)
		require.NotContains(t, result.Warning, "rollback failed")
	}
	repo := &refundInterleavedAdminSubscriptionRepo{
		pendingRefundSubscriptionRepo: &pendingRefundSubscriptionRepo{client: f.client},
		beforeLock:                    compensate, afterRead: compensate,
	}
	adminSvc := NewSubscriptionService(nil, repo, nil, f.client, nil)
	days := 4
	_, err := adminSvc.AdjustSubscription(f.ctx, f.before[0].ID, AdjustSubscriptionInput{Days: &days})
	require.NoError(t, err)
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	want := f.before[0].ExpiresAt.Add(4 * 24 * time.Hour)
	require.True(t, want.Equal(after.ExpiresAt), "expiry = %v; want compensation plus admin days = %v", after.ExpiresAt, want)
}

func TestAdjustSubscriptionRollsBackFailureAfterExpiryWrite(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 1)
	before, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	repo := &refundInterleavedAdminSubscriptionRepo{
		pendingRefundSubscriptionRepo: &pendingRefundSubscriptionRepo{client: f.client}, failAfterWrite: true,
	}
	adminSvc := NewSubscriptionService(nil, repo, nil, f.client, nil)
	days := 4
	_, err = adminSvc.AdjustSubscription(f.ctx, before.ID, AdjustSubscriptionInput{Days: &days})
	require.ErrorContains(t, err, "injected admin failure after expiry write")
	after, err := f.client.UserSubscription.Get(f.ctx, before.ID)
	require.NoError(t, err)
	require.True(t, before.ExpiresAt.Equal(after.ExpiresAt), "failed admin mutation must roll back its expiry write")
}

func TestPendingSubscriptionRefundFailureRestoresStackedCardsAfterRestart(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 2, 3)
	result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	for _, before := range f.before {
		after, getErr := f.client.UserSubscription.Get(f.ctx, before.ID)
		require.NoError(t, getErr)
		require.True(t, before.ExpiresAt.Equal(after.ExpiresAt), "card %d expiry = %v; want %v", before.ID, after.ExpiresAt, before.ExpiresAt)
		require.Equal(t, SubscriptionStatusActive, after.Status)
	}
}

func TestPendingSubscriptionRefundFailurePreservesConsumptionAndWindowRollover(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 1)
	rolled := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	_, err := f.client.UserSubscription.UpdateOneID(f.before[0].ID).
		SetDailyUsageUsd(2).SetWeeklyUsageUsd(18).SetMonthlyUsageUsd(19).SetCustomUsageUsd(3).
		SetDailyWindowStart(rolled).SetCustomWindowStart(rolled).Save(f.ctx)
	require.NoError(t, err)
	_, err = f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.Equal(t, 2.0, after.DailyUsageUsd)
	require.Equal(t, 18.0, after.WeeklyUsageUsd)
	require.Equal(t, 19.0, after.MonthlyUsageUsd)
	require.Equal(t, 3.0, after.CustomUsageUsd)
	require.True(t, rolled.Equal(*after.DailyWindowStart))
	require.True(t, rolled.Equal(*after.CustomWindowStart))
}

func TestPendingSubscriptionRefundFailurePreservesAdminRenewalAndNotes(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 1)
	// Admin adds four days after the refund held one day. Compensation must
	// restore that one day on top of the admin's expiry, preserving all four.
	want := f.before[0].ExpiresAt.Add(4 * 24 * time.Hour)
	_, err := f.client.UserSubscription.UpdateOneID(f.before[0].ID).
		SetExpiresAt(f.before[0].ExpiresAt.Add(3 * 24 * time.Hour)).SetNotes("admin renewed four days").Save(f.ctx)
	require.NoError(t, err)
	_, err = f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, want.Equal(after.ExpiresAt), "expiry = %v; want %v", after.ExpiresAt, want)
	require.Contains(t, psStringValue(after.Notes), "admin renewed four days")
}

func TestPendingSubscriptionRefundFailurePreservesAdminRevocation(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 3)
	_, err := f.client.UserSubscription.UpdateOneID(f.before[0].ID).
		SetStatus(SubscriptionStatusRevoked).SetNotes("admin revoked during pending").Save(f.ctx)
	require.NoError(t, err)
	result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.Contains(t, result.Warning, "rollback failed")
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.Equal(t, SubscriptionStatusRevoked, after.Status)
	require.Equal(t, "admin revoked during pending", psStringValue(after.Notes))
	require.Equal(t, 1, countRefundAuditsForTest(t, f.ctx, f.client, f.order.ID, "REFUND_ROLLBACK_FAILED"))
}

func TestPendingSubscriptionRefundFailureDoesNotReactivateLaterExpiryChange(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 1)
	changedExpiry := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	_, err := f.client.UserSubscription.UpdateOneID(f.before[0].ID).
		SetExpiresAt(changedExpiry).SetStatus(SubscriptionStatusExpired).Save(f.ctx)
	require.NoError(t, err)
	_, err = f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.Equal(t, SubscriptionStatusExpired, after.Status)
	require.True(t, changedExpiry.Add(24*time.Hour).Equal(after.ExpiresAt))
}

func TestPendingSubscriptionRefundFailureCompensationRollsBackWithAudit(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 1)
	deducted, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	f.client.PaymentAuditLog.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
			if mutation, ok := m.(*dbent.PaymentAuditLogMutation); ok {
				if action, _ := mutation.Action(); action == "REFUND_FAILED" {
					return nil, errors.New("injected refund audit failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	_, err = f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.ErrorContains(t, err, "injected refund audit failure")
	after, err := f.client.UserSubscription.Get(f.ctx, deducted.ID)
	require.NoError(t, err)
	require.True(t, deducted.ExpiresAt.Equal(after.ExpiresAt))
	order, err := f.client.PaymentOrder.Get(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, order.Status)
	require.Zero(t, countRefundAuditsForTest(t, f.ctx, f.client, order.ID, "REFUND_FAILED"))
	require.Zero(t, countRefundAuditsForTest(t, f.ctx, f.client, order.ID, refundSubscriptionTermsReturned))
}

func TestPendingSubscriptionRefundFailureRejectsAmbiguousLegacySnapshot(t *testing.T) {
	f := newPendingSubscriptionRefundFixture(t, 1, 1)
	audit, err := f.client.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(f.order.ID, 10)),
		paymentauditlog.ActionEQ("REFUND_PENDING")).Only(f.ctx)
	require.NoError(t, err)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(audit.Detail), &detail))
	delete(detail, "subscriptionDeductionAfter")
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	_, err = f.client.PaymentAuditLog.UpdateOneID(audit.ID).SetDetail(string(raw)).Save(f.ctx)
	require.NoError(t, err)
	deducted, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.Contains(t, result.Warning, "rollback failed")
	after, err := f.client.UserSubscription.Get(f.ctx, deducted.ID)
	require.NoError(t, err)
	require.True(t, deducted.ExpiresAt.Equal(after.ExpiresAt))
	require.Equal(t, 1, countRefundAuditsForTest(t, f.ctx, f.client, f.order.ID, "REFUND_ROLLBACK_FAILED"))
}

// Both orders use the same persisted card: A holds one of its three days,
// then B holds the other two. The second hold expires the card entirely.
func newOverlappingPendingSubscriptionRefundFixture(t *testing.T) (*pendingSubscriptionRefundFixture, *dbent.PaymentOrder, time.Time) {
	t.Helper()
	start := time.Date(2030, 3, 1, 12, 0, 0, 0, time.UTC)
	f := newPendingSubscriptionRefundFixture(t, 1, 1, start)
	second := createCompletedRefundableOrderForTest(t, f.ctx, f.client, "subscription-overlap-second")
	second, err := f.client.PaymentOrder.UpdateOneID(second.ID).SetUserID(f.order.UserID).
		SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(*f.order.SubscriptionGroupID).
		SetSubscriptionDays(2).Save(f.ctx)
	require.NoError(t, err)
	plan, early, err := f.svc.PrepareRefund(f.ctx, second.ID, 0, "second refund", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := f.svc.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.Contains(t, result.Warning, "pending")
	held, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.Equal(t, SubscriptionStatusExpired, held.Status)
	require.True(t, held.ExpiresAt.Equal(start))
	return f, second, start
}

func restartOverlappingPendingRefundService(f *pendingSubscriptionRefundFixture, now time.Time) {
	repo := &pendingRefundSubscriptionRepo{client: f.client}
	f.svc = &PaymentService{entClient: f.client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: NewSubscriptionService(nil, repo, nil, f.client, nil)}
	f.svc.subscriptionSvc.now = func() time.Time { return now }
}

func TestOverlappingPendingSubscriptionRefundsRestoreActiveInEitherFailureOrder(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "earlier_hold_fails_first"
		if reverse {
			name = "later_hold_fails_first"
		}
		t.Run(name, func(t *testing.T) {
			f, second, start := newOverlappingPendingSubscriptionRefundFixture(t)
			ids := []int64{f.order.ID, second.ID}
			if reverse {
				ids[0], ids[1] = ids[1], ids[0]
			}
			for _, id := range ids {
				restartOverlappingPendingRefundService(f, start.Add(12*time.Hour))
				result, err := f.svc.QueryAndFinalizeRefund(f.ctx, id)
				require.NoError(t, err)
				require.NotContains(t, result.Warning, "rollback failed")
				current, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
				require.NoError(t, err)
				require.Equal(t, SubscriptionStatusActive, current.Status, "a returned future term must be usable")
			}
			after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
			require.NoError(t, err)
			require.True(t, after.ExpiresAt.Equal(start.Add(72*time.Hour)))
		})
	}
}

func TestOverlappingPendingSubscriptionRefundsReactivateAfterFirstReturnStillExpired(t *testing.T) {
	f, second, start := newOverlappingPendingSubscriptionRefundFixture(t)
	restartOverlappingPendingRefundService(f, start.Add(36*time.Hour))
	result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	first, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, first.ExpiresAt.Equal(start.Add(24*time.Hour)))
	require.Equal(t, SubscriptionStatusExpired, first.Status)
	// Recovery after restart must recognize the first persisted compensation,
	// even though that compensation could not reactivate the card yet.
	restartOverlappingPendingRefundService(f, start.Add(36*time.Hour))
	result, err = f.svc.QueryAndFinalizeRefund(f.ctx, second.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, after.ExpiresAt.Equal(start.Add(72*time.Hour)))
	require.Equal(t, SubscriptionStatusActive, after.Status)
}

func TestOverlappingPendingSubscriptionRefundsPreserveAdminExpiryAfterFirstReturn(t *testing.T) {
	f, second, start := newOverlappingPendingSubscriptionRefundFixture(t)
	restartOverlappingPendingRefundService(f, start.Add(36*time.Hour))
	_, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	// The administrator changes the expired term after the first return. The
	// second return would make its expiry future, but must preserve expired.
	_, err = f.client.UserSubscription.UpdateOneID(f.before[0].ID).
		SetExpiresAt(start.Add(12 * time.Hour)).SetStatus(SubscriptionStatusExpired).
		SetNotes("payment refund order 999 failed; held subscription term returned").Save(f.ctx)
	require.NoError(t, err)
	restartOverlappingPendingRefundService(f, start.Add(36*time.Hour))
	result, err := f.svc.QueryAndFinalizeRefund(f.ctx, second.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, after.ExpiresAt.Equal(start.Add(60*time.Hour)))
	require.Equal(t, SubscriptionStatusExpired, after.Status)
}

type overlappingBeforePendingProvider struct {
	*pendingRefundProviderTestDouble
	beforePending      func()
	beforeLaterReturns func()
	laterDirectSuccess bool
	laterFailure       bool
}

func (p *overlappingBeforePendingProvider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	if p.beforePending != nil {
		fn := p.beforePending
		p.beforePending = nil
		fn()
		return p.pendingRefundProviderTestDouble.Refund(ctx, req)
	}
	if p.beforeLaterReturns != nil {
		fn := p.beforeLaterReturns
		p.beforeLaterReturns = nil
		fn()
	}
	if p.laterFailure {
		return nil, errors.New("later gateway failed immediately")
	}
	if p.laterDirectSuccess {
		return &payment.RefundResponse{RefundID: "rf_direct", Status: payment.ProviderStatusSuccess}, nil
	}
	return p.pendingRefundProviderTestDouble.Refund(ctx, req)
}

func TestOverlappingPendingSubscriptionRefundsEarlierGatewayPublishesPendingAfterLaterSettlement(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "pending_then_success"
		if direct {
			name = "direct_success"
		}
		t.Run(name, func(t *testing.T) { testOverlappingRefundDelayedPublication(t, direct) })
	}
}

func testOverlappingRefundDelayedPublication(t *testing.T, direct bool) {
	start := time.Date(2030, 3, 1, 12, 0, 0, 0, time.UTC)
	f := newPendingSubscriptionRefundFixture(t, 1, 1, start)
	// Reset the isolated fixture to its completed, undeducted baseline so the
	// callback below can exercise the actual A gateway call still in flight.
	_, err := f.client.PaymentAuditLog.Delete().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(f.order.ID, 10))).Exec(f.ctx)
	require.NoError(t, err)
	_, err = f.client.PaymentOrder.UpdateOneID(f.order.ID).SetStatus(OrderStatusCompleted).Save(f.ctx)
	require.NoError(t, err)
	_, err = f.client.UserSubscription.UpdateOneID(f.before[0].ID).
		SetExpiresAt(f.before[0].ExpiresAt).SetStatus(SubscriptionStatusActive).Save(f.ctx)
	require.NoError(t, err)
	second := createCompletedRefundableOrderForTest(t, f.ctx, f.client, "subscription-inflight-second")
	second, err = f.client.PaymentOrder.UpdateOneID(second.ID).SetUserID(f.order.UserID).
		SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(*f.order.SubscriptionGroupID).
		SetSubscriptionDays(2).Save(f.ctx)
	require.NoError(t, err)
	prov := &overlappingBeforePendingProvider{pendingRefundProviderTestDouble: &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusSuccess}, laterDirectSuccess: direct}
	prov.beforePending = func() {
		plan, early, err := f.svc.PrepareRefund(f.ctx, second.ID, 0, "second refund", false, true)
		require.NoError(t, err)
		require.Nil(t, early)
		result, err := f.svc.ExecuteRefund(f.ctx, plan)
		require.NoError(t, err)
		if !direct {
			require.Contains(t, result.Warning, "pending")
			result, err = f.svc.QueryAndFinalizeRefund(f.ctx, second.ID)
			require.NoError(t, err)
		}
		require.True(t, result.Success)
	}
	restore := replacePaymentProviderFactoryForTest(t, prov)
	defer restore()
	plan, early, err := f.svc.PrepareRefund(f.ctx, f.order.ID, 0, "first refund", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	result, err := f.svc.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.Contains(t, result.Warning, "pending")
	prov.queryStatus = payment.ProviderStatusFailed
	restartOverlappingPendingRefundService(f, start.Add(12*time.Hour))
	result, err = f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.NotContains(t, result.Warning, "rollback failed")
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, after.ExpiresAt.Equal(start.Add(24*time.Hour)))
	require.Equal(t, SubscriptionStatusActive, after.Status)
}

func TestOverlappingPendingSubscriptionRefundsReturnWhileLaterGatewayStillInFlight(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "later_becomes_pending"
		if fail {
			name = "later_fails_immediately"
		}
		t.Run(name, func(t *testing.T) {
			start := time.Date(2030, 3, 1, 12, 0, 0, 0, time.UTC)
			f := newPendingSubscriptionRefundFixture(t, 1, 1, start)
			second := createCompletedRefundableOrderForTest(t, f.ctx, f.client, "subscription-inflight-return")
			second, err := f.client.PaymentOrder.UpdateOneID(second.ID).SetUserID(f.order.UserID).
				SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(*f.order.SubscriptionGroupID).
				SetSubscriptionDays(2).Save(f.ctx)
			require.NoError(t, err)
			prov := &overlappingBeforePendingProvider{pendingRefundProviderTestDouble: &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed}, laterFailure: fail}
			prov.beforeLaterReturns = func() {
				// B's term deduction has committed, but its gateway call has not
				// returned yet: neither B pending nor success audit exists.
				result, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
				require.NoError(t, err)
				require.NotContains(t, result.Warning, "rollback failed")
			}
			restore := replacePaymentProviderFactoryForTest(t, prov)
			defer restore()
			plan, early, err := f.svc.PrepareRefund(f.ctx, second.ID, 0, "second refund", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := f.svc.ExecuteRefund(f.ctx, plan)
			require.NoError(t, err)
			if !fail {
				require.Contains(t, result.Warning, "pending")
				current, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
				require.NoError(t, err)
				require.True(t, current.ExpiresAt.Equal(start.Add(24*time.Hour)))
				require.Equal(t, SubscriptionStatusActive, current.Status)
				restartOverlappingPendingRefundService(f, start.Add(12*time.Hour))
				result, err = f.svc.QueryAndFinalizeRefund(f.ctx, second.ID)
				require.NoError(t, err)
				require.NotContains(t, result.Warning, "rollback failed")
			} else {
				require.Contains(t, result.Warning, "rolled back")
			}
			after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
			require.NoError(t, err)
			require.True(t, after.ExpiresAt.Equal(start.Add(72*time.Hour)), "both returned deductions must survive")
			require.Equal(t, SubscriptionStatusActive, after.Status)
		})
	}
}

func TestRefundSubscriptionDeductionAuditFailureRollsBackBeforeGateway(t *testing.T) {
	start := time.Date(2030, 3, 1, 12, 0, 0, 0, time.UTC)
	f := newPendingSubscriptionRefundFixture(t, 1, 1, start)
	second := createCompletedRefundableOrderForTest(t, f.ctx, f.client, "subscription-deduction-audit-error")
	second, err := f.client.PaymentOrder.UpdateOneID(second.ID).SetUserID(f.order.UserID).
		SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(*f.order.SubscriptionGroupID).
		SetSubscriptionDays(2).Save(f.ctx)
	require.NoError(t, err)
	prov := &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed}
	restore := replacePaymentProviderFactoryForTest(t, prov)
	defer restore()
	f.client.PaymentAuditLog.Use(func(next dbent.Mutator) dbent.Mutator {
		return dbent.MutateFunc(func(ctx context.Context, m dbent.Mutation) (dbent.Value, error) {
			if mutation, ok := m.(*dbent.PaymentAuditLogMutation); ok {
				if action, _ := mutation.Action(); action == "REFUND_SUBSCRIPTION_TERMS_DEDUCTED" {
					return nil, errors.New("injected deduction fact failure")
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	plan, early, err := f.svc.PrepareRefund(f.ctx, second.ID, 0, "second refund", false, true)
	require.NoError(t, err)
	require.Nil(t, early)
	_, err = f.svc.ExecuteRefund(f.ctx, plan)
	require.ErrorContains(t, err, "injected deduction fact failure")
	require.Zero(t, prov.refundCalls, "gateway must not run after the deduction fact failed")
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, after.ExpiresAt.Equal(start.Add(48*time.Hour)))
	require.Equal(t, SubscriptionStatusActive, after.Status)
	order, err := f.client.PaymentOrder.Get(f.ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, order.Status)
}

type refundBeforeCardLockRepo struct {
	*pendingRefundSubscriptionRepo
	beforeLock func(context.Context, int64)
}

func (r *refundBeforeCardLockRepo) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	if r.beforeLock != nil {
		r.beforeLock(ctx, id)
	}
	return r.pendingRefundSubscriptionRepo.GetByIDForUpdate(ctx, id)
}

func TestPendingSubscriptionRefundRefreshesProvenanceAfterEachCardLock(t *testing.T) {
	start := time.Date(2030, 3, 1, 12, 0, 0, 0, time.UTC)
	f := newPendingSubscriptionRefundFixture(t, 2, 3, start)
	// A affected card2 and then card1. Simulate another order's transaction
	// committing a provenance-preserving return on card1 just before A obtains
	// that second lock, after A already loaded card2's proof.
	var injected bool
	repo := &refundBeforeCardLockRepo{pendingRefundSubscriptionRepo: &pendingRefundSubscriptionRepo{client: f.client}}
	repo.beforeLock = func(txCtx context.Context, id int64) {
		if id != f.before[0].ID || injected {
			return
		}
		injected = true
		tx := dbent.TxFromContext(txCtx)
		require.NotNil(t, tx)
		other, err := tx.PaymentOrder.Create().SetUserID(f.order.UserID).SetUserEmail(f.order.UserEmail).SetUserName(f.order.UserName).
			SetAmount(1).SetPayAmount(1).SetFeeRate(0).SetRechargeCode("SECOND-LOCK").SetOutTradeNo("second-lock").
			SetPaymentType(f.order.PaymentType).SetPaymentTradeNo("second-lock").SetOrderType(payment.OrderTypeSubscription).
			SetClientIP("127.0.0.1").SetSrcHost("test.local").
			SetSubscriptionGroupID(*f.order.SubscriptionGroupID).SetSubscriptionDays(1).SetStatus(OrderStatusRefundFailed).
			SetExpiresAt(start.Add(24 * time.Hour)).Save(txCtx)
		require.NoError(t, err)
		current, err := repo.GetByID(txCtx, id)
		require.NoError(t, err)
		// The current held card expires day 2. A valid preceding B deduction
		// from day3→day2 is represented by its actual deduction fact.
		original := *current
		original.ExpiresAt = current.ExpiresAt.Add(24 * time.Hour)
		require.NoError(t, recordRefundSubscriptionDeduction(txCtx, other.ID, []UserSubscription{original}, []UserSubscription{*current}))
		fact, err := tx.PaymentAuditLog.Query().Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(other.ID, 10)),
			paymentauditlog.ActionEQ(refundSubscriptionTermsDeducted)).Only(txCtx)
		require.NoError(t, err)
		returned := refundSubscriptionTerm(*current)
		returned.ExpiresAt = current.ExpiresAt.Add(24 * time.Hour)
		_, err = tx.UserSubscription.UpdateOneID(id).SetExpiresAt(returned.ExpiresAt).SetStatus(SubscriptionStatusExpired).Save(txCtx)
		require.NoError(t, err)
		require.NoError(t, createPaymentAuditLogTx(txCtx, tx.Client(), other.ID, refundSubscriptionTermsReturned, map[string]any{
			"deductionAuditID": fact.ID, "terms": []refundTermReturn{{Before: refundSubscriptionTerm(*current), After: refundTermState{
				ID: returned.ID, UserID: returned.UserID, GroupID: returned.GroupID, ExpiresAt: returned.ExpiresAt, Status: SubscriptionStatusExpired,
			}, Provenance: true}},
		}))
	}
	f.svc.subscriptionSvc = NewSubscriptionService(nil, repo, nil, f.client, nil)
	f.svc.subscriptionSvc.now = func() time.Time { return start.Add(84 * time.Hour) }
	_, err := f.svc.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.True(t, injected)
	after, err := f.client.UserSubscription.Get(f.ctx, f.before[0].ID)
	require.NoError(t, err)
	require.True(t, after.ExpiresAt.Equal(start.Add(96*time.Hour)))
	require.Equal(t, SubscriptionStatusActive, after.Status)
}
