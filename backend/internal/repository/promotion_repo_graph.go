package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// All parent mutations serialize on one transaction-scoped graph lock. READ
// COMMITTED ensures reads after waiting observe the preceding writer's commit.
func (r *promotionRepository) beginPromotionGraphTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryLockHash("sub2api:promotion:parent-graph")); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func promotionUserInTx(ctx context.Context, tx *sql.Tx, userID int64) (*service.PromotionUser, error) {
	return scanPromotionUser(tx.QueryRowContext(ctx, `
		SELECT user_id, invite_code, parent_user_id, binding_source, bound_at, bound_note, created_at, updated_at
		FROM promotion_users WHERE user_id = $1
	`, userID))
}

func validatePromotionAncestorChain(ctx context.Context, tx *sql.Tx, userID, parentUserID int64) error {
	seen := map[int64]struct{}{userID: {}}
	for currentID := parentUserID; ; {
		if _, exists := seen[currentID]; exists {
			return service.ErrPromotionBindCycle
		}
		seen[currentID] = struct{}{}
		current, err := promotionUserInTx(ctx, tx, currentID)
		if err != nil {
			return err
		}
		if current == nil {
			return service.ErrPromotionInvalidBindRequest
		}
		if current.ParentUserID == nil {
			return nil
		}
		currentID = *current.ParentUserID
	}
}
