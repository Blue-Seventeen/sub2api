package service

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// restorePendingRefundSubscriptionTerms runs inside refund finalization's
// transaction. It reverses the recorded expiry delta against the locked current
// term; usage, windows, source and group snapshots are never written here.
func (s *SubscriptionService) restorePendingRefundSubscriptionTerms(ctx context.Context, before, after []UserSubscription, orderID int64) error {
	if len(before) == 0 || len(before) != len(after) {
		// Before-only audits cannot distinguish a clamped full-card deduction
		// from later administrative renewal. Require manual recovery rather than
		// silently overwrite newer state or return an invented number of days.
		return fmt.Errorf("refund subscription deduction lacks exact post-deduction terms; manual recovery required")
	}
	afterByID := make(map[int64]UserSubscription, len(after))
	userID, groupID := before[0].UserID, before[0].GroupID
	for _, term := range after {
		if term.ID <= 0 || term.ExpiresAt.IsZero() || term.UserID != userID || term.GroupID != groupID {
			return fmt.Errorf("invalid refund post-deduction subscription term")
		}
		if _, exists := afterByID[term.ID]; exists {
			return fmt.Errorf("duplicate refund post-deduction subscription %d", term.ID)
		}
		afterByID[term.ID] = term
	}
	for _, original := range before {
		if original.ID <= 0 || original.UserID != userID || original.GroupID != groupID {
			return fmt.Errorf("refund deduction contains multiple subscription owners or groups")
		}
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("refund term compensation requires finalization transaction")
	}
	if err := s.lockSubscriptionAssignmentInDB(ctx, userID, groupID); err != nil {
		return err
	}
	var proof *refundTermProvenance
	returns := make([]refundTermReturn, 0, len(before))
	for _, original := range before {
		deducted, ok := afterByID[original.ID]
		if !ok || original.UserID != deducted.UserID || original.GroupID != deducted.GroupID || !original.ExpiresAt.After(deducted.ExpiresAt) {
			return fmt.Errorf("invalid refund deduction delta for subscription %d", original.ID)
		}
		delete(afterByID, original.ID)
		current, err := s.userSubRepo.GetByIDForUpdate(ctx, original.ID)
		if err != nil {
			return err
		}
		if current.UserID != original.UserID || current.GroupID != original.GroupID || current.Status == SubscriptionStatusRevoked || current.DeletedAt != nil {
			return fmt.Errorf("refund subscription %d was moved or revoked; manual recovery required", original.ID)
		}
		// Other cards can change while this refund waits for their row locks.
		// Refresh the evidence after each lock so it matches this current row.
		proof, err = loadRefundTermProvenance(ctx, tx.Client(), current.UserID, current.GroupID, orderID, before, after)
		if err != nil {
			return err
		}
		provenance := proof.matches(deducted, *current, now)
		newExpiry := current.ExpiresAt.Add(original.ExpiresAt.Sub(deducted.ExpiresAt))
		if newExpiry.After(MaxExpiresAt) {
			return fmt.Errorf("refund compensation exceeds maximum expiry for subscription %d", original.ID)
		}
		notes := appendSubscriptionNotes(current.Notes, fmt.Sprintf("payment refund order %d failed; held subscription term returned", orderID))
		fields := UserSubscriptionMutableFields{ExpiresAt: &newExpiry, Notes: &notes}
		// The expiry worker can mark a partially held, initially active term as
		// expired while the gateway is pending. Reactivate that term too when
		// its expiry is unchanged and returning the held duration makes it valid.
		// Revocation, suspension and administrative expiry changes remain intact.
		expiredByDeduction := deducted.Status == SubscriptionStatusExpired
		expiredWhilePending := deducted.Status == SubscriptionStatusActive && !deducted.ExpiresAt.After(now)
		unchangedExpiredTerm := current.ExpiresAt.Equal(deducted.ExpiresAt) && (expiredByDeduction || expiredWhilePending)
		if original.Status == SubscriptionStatusActive && current.Status == SubscriptionStatusExpired &&
			(unchangedExpiredTerm || provenance) && newExpiry.After(now) {
			fields.Status = &original.Status
		}
		if err := s.userSubRepo.UpdateMutableFields(ctx, current.ID, fields); err != nil {
			return err
		}
		persisted, err := s.userSubRepo.GetByID(ctx, current.ID)
		if err != nil {
			return err
		}
		returns = append(returns, refundTermReturn{Before: refundSubscriptionTerm(*current), After: refundSubscriptionTerm(*persisted), Provenance: provenance})
	}
	return createPaymentAuditLogTx(ctx, tx.Client(), orderID, refundSubscriptionTermsReturned, map[string]any{
		"deductionAuditID": proof.deductionAuditID, "pendingAuditID": proof.pendingAuditID, "terms": returns,
	})
}
