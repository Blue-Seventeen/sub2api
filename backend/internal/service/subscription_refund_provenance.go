package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const (
	refundSubscriptionTermsDeducted = "REFUND_SUBSCRIPTION_TERMS_DEDUCTED"
	refundSubscriptionTermsReturned = "REFUND_SUBSCRIPTION_TERMS_RETURNED"
)

type refundTermState struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userID"`
	GroupID   int64     `json:"groupID"`
	ExpiresAt time.Time `json:"expiresAt"`
	Status    string    `json:"status"`
}

type refundTermReturn struct {
	Before     refundTermState `json:"before"`
	After      refundTermState `json:"after"`
	Provenance bool            `json:"provenance"`
}

type refundTermReturnDetail struct {
	DeductionAuditID int64              `json:"deductionAuditID"`
	PendingAuditID   int64              `json:"pendingAuditID,omitempty"`
	Terms            []refundTermReturn `json:"terms"`
}

type refundTermTransition struct {
	before  refundTermState
	after   refundTermState
	auditID int64
}

type refundTermProvenance struct {
	deductionAuditID int64
	pendingAuditID   int64
	rootAuditID      int64
	transitions      []refundTermTransition
}

func refundSubscriptionTerm(sub UserSubscription) refundTermState {
	return refundTermState{ID: sub.ID, UserID: sub.UserID, GroupID: sub.GroupID, ExpiresAt: sub.ExpiresAt, Status: sub.Status}
}

func recordRefundSubscriptionDeduction(ctx context.Context, orderID int64, before, after []UserSubscription) error {
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return fmt.Errorf("refund deduction evidence requires subscription transaction")
	}
	return createPaymentAuditLogTx(ctx, tx.Client(), orderID, refundSubscriptionTermsDeducted, map[string]any{
		"deductionHeld": true, "deductionType": payment.DeductionTypeSubscription,
		"subscriptionDeductionSnapshots": refundSubscriptionSnapshots(before),
		"subscriptionDeductionAfter":     refundSubscriptionSnapshots(after),
	})
}

func sameRefundTermSnapshots(a, b []refundSubscriptionSnapshot) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].UserID != b[i].UserID || a[i].GroupID != b[i].GroupID ||
			a[i].Status != b[i].Status || !a[i].ExpiresAt.Equal(b[i].ExpiresAt) {
			return false
		}
	}
	return true
}

func sameRefundDeduction(a, b refundPendingAuditDetail) bool {
	return sameRefundTermSnapshots(a.SubscriptionDeductionSnapshots, b.SubscriptionDeductionSnapshots) &&
		sameRefundTermSnapshots(a.SubscriptionDeductionAfter, b.SubscriptionDeductionAfter)
}

// The finalization transaction's client observes term facts only after locking
// the current card. Each new fact was committed with its actual row mutation;
// its ID therefore follows the same card's previous term facts even when the
// gateway has not returned yet. Notes and gateway publication times are unused.
func loadRefundTermProvenance(ctx context.Context, client *dbent.Client, userID, groupID, orderID int64, before, after []UserSubscription) (*refundTermProvenance, error) {
	orderIDs, err := client.PaymentOrder.Query().Where(
		paymentorder.UserIDEQ(userID), paymentorder.SubscriptionGroupIDEQ(groupID),
		paymentorder.OrderTypeEQ(payment.OrderTypeSubscription),
	).IDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("query refund provenance orders: %w", err)
	}
	ids := make([]string, 0, len(orderIDs))
	for _, id := range orderIDs {
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	logs, err := client.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDIn(ids...),
		paymentauditlog.ActionIn(refundSubscriptionTermsDeducted, "REFUND_PENDING", refundSubscriptionTermsReturned),
	).Order(paymentauditlog.ByID()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query refund provenance audits: %w", err)
	}
	type deduction struct {
		log    *dbent.PaymentAuditLog
		detail refundPendingAuditDetail
		actual bool
	}
	facts := make(map[int64]*deduction)
	latest := make(map[string]*deduction)
	proof := &refundTermProvenance{}
	want := refundPendingAuditDetail{SubscriptionDeductionSnapshots: refundSubscriptionSnapshots(before), SubscriptionDeductionAfter: refundSubscriptionSnapshots(after)}
	for _, entry := range logs {
		if entry.Action != refundSubscriptionTermsDeducted && entry.Action != "REFUND_PENDING" {
			continue
		}
		var detail refundPendingAuditDetail
		if json.Unmarshal([]byte(entry.Detail), &detail) != nil || !detail.DeductionHeld || detail.DeductionType != payment.DeductionTypeSubscription {
			continue
		}
		fact := &deduction{log: entry, detail: detail, actual: entry.Action == refundSubscriptionTermsDeducted}
		if fact.actual {
			latest[entry.OrderID] = fact
		} else if prior := latest[entry.OrderID]; prior != nil && sameRefundDeduction(prior.detail, detail) {
			// REFUND_PENDING repeats the earlier deduction snapshots. Associate
			// its ID with that fact rather than creating a second mutation edge.
			fact = prior
		}
		facts[entry.ID] = fact
		if entry.OrderID == strconv.FormatInt(orderID, 10) && sameRefundDeduction(detail, want) {
			if entry.Action == refundSubscriptionTermsDeducted {
				proof.deductionAuditID, proof.rootAuditID = entry.ID, entry.ID
			} else {
				proof.pendingAuditID = entry.ID
				if proof.deductionAuditID == 0 {
					proof.rootAuditID = entry.ID
				}
			}
		}
		if !fact.actual || entry.Action != refundSubscriptionTermsDeducted {
			continue
		}
		for i, original := range detail.SubscriptionDeductionSnapshots {
			if i >= len(detail.SubscriptionDeductionAfter) {
				break
			}
			deducted := detail.SubscriptionDeductionAfter[i]
			if original.ID <= 0 || original.ID != deducted.ID || original.UserID != userID || deducted.UserID != userID ||
				original.GroupID != groupID || deducted.GroupID != groupID || original.Status != SubscriptionStatusActive || !original.ExpiresAt.After(deducted.ExpiresAt) {
				continue
			}
			proof.transitions = append(proof.transitions, refundTermTransition{
				before: refundSubscriptionTerm(original.userSubscription()), after: refundSubscriptionTerm(deducted.userSubscription()), auditID: entry.ID,
			})
		}
	}
	if proof.rootAuditID == 0 {
		return nil, fmt.Errorf("missing exact refund deduction evidence")
	}
	for _, entry := range logs {
		if entry.Action != refundSubscriptionTermsReturned {
			continue
		}
		var detail refundTermReturnDetail
		if json.Unmarshal([]byte(entry.Detail), &detail) != nil {
			continue
		}
		fact := facts[detail.DeductionAuditID]
		if detail.DeductionAuditID == 0 {
			fact = facts[detail.PendingAuditID]
		}
		if fact == nil || fact.log.OrderID != entry.OrderID || fact.log.ID >= entry.ID {
			continue
		}
		for _, term := range detail.Terms {
			if term.Before.ID != term.After.ID || term.Before.UserID != userID || term.After.UserID != userID || term.Before.GroupID != groupID || term.After.GroupID != groupID ||
				(!fact.actual && !term.Provenance) || !refundReturnMatchesDeduction(term, fact.detail) {
				continue
			}
			// For actual facts, the path from the current root independently
			// proves provenance. A different originating refund may have lacked
			// that path when it wrote this exact, atomic return mutation.
			proof.transitions = append(proof.transitions, refundTermTransition{before: term.Before, after: term.After, auditID: entry.ID})
		}
	}
	return proof, nil
}

func refundReturnMatchesDeduction(term refundTermReturn, detail refundPendingAuditDetail) bool {
	for i, original := range detail.SubscriptionDeductionSnapshots {
		if i >= len(detail.SubscriptionDeductionAfter) {
			break
		}
		deducted := detail.SubscriptionDeductionAfter[i]
		if original.ID == term.Before.ID && deducted.ID == original.ID && original.UserID == term.Before.UserID && deducted.UserID == original.UserID &&
			original.GroupID == term.Before.GroupID && deducted.GroupID == original.GroupID && original.ExpiresAt.After(deducted.ExpiresAt) &&
			term.After.ExpiresAt.After(term.Before.ExpiresAt) && original.ExpiresAt.Sub(deducted.ExpiresAt) == term.After.ExpiresAt.Sub(term.Before.ExpiresAt) {
			return true
		}
	}
	return false
}

// An administrative expiry change creates a missing transition, so no path
// reaches it. Natural expiry can change active to expired without changing the
// term. Revoked and suspended states cannot be reached by that implicit edge.
func (p *refundTermProvenance) matches(deducted, current UserSubscription, now time.Time) bool {
	type node struct {
		state refundTermState
		last  int64
	}
	type key struct {
		expires int64
		status  string
	}
	seen := make(map[key]int64)
	target := refundSubscriptionTerm(current)
	queue := []node{{state: refundSubscriptionTerm(deducted), last: p.rootAuditID}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		k := key{expires: n.state.ExpiresAt.UnixNano(), status: n.state.Status}
		if earlier, ok := seen[k]; ok && earlier <= n.last {
			continue
		}
		seen[k] = n.last
		if n.state.ExpiresAt.Equal(target.ExpiresAt) && n.state.Status == target.Status {
			return true
		}
		if n.state.Status == SubscriptionStatusActive && !n.state.ExpiresAt.After(now) {
			expired := n.state
			expired.Status = SubscriptionStatusExpired
			queue = append(queue, node{state: expired, last: n.last})
		}
		for _, transition := range p.transitions {
			if transition.auditID > n.last && transition.before.ID == current.ID && transition.before.UserID == current.UserID && transition.before.GroupID == current.GroupID &&
				transition.before.Status == n.state.Status && transition.before.ExpiresAt.Equal(n.state.ExpiresAt) {
				queue = append(queue, node{state: transition.after, last: transition.auditID})
			}
		}
	}
	return false
}
