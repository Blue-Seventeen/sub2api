package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *promotionRepository) CreateManualPromotionCommission(ctx context.Context, record service.PromotionCommissionRecord, settleNow bool) (*service.PromotionCommissionRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	status := service.PromotionCommissionStatusPending
	var settledAt any
	if settleNow {
		status = service.PromotionCommissionStatusSettled
		settledAt = now
	}
	row := tx.QueryRowContext(ctx, `
		INSERT INTO promotion_commission_records (
			beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
			base_amount, amount, status, note, created_by_user_id, settled_at, created_at, updated_at
		) VALUES ($1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
		RETURNING id, beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
		          level_id, COALESCE(level_snapshot, ''), rate_snapshot, base_amount, amount, status,
		          settlement_batch_id, COALESCE(note, ''), created_by_user_id, settled_at, cancelled_at,
		          created_at, updated_at
	`, record.BeneficiaryUserID, nullableInt64(record.SourceUserID), record.BusinessDate.Format("2006-01-02"), record.CommissionType, record.RelationDepth, record.BaseAmount, record.Amount, status, nullableTrimmedString(record.Note), nullableInt64(record.CreatedByUserID), settledAt)
	created, err := scanPromotionCommissionRecordRow(row)
	if err != nil {
		return nil, err
	}
	if settleNow {
		if err := r.applyUserBalanceDeltaTx(ctx, tx, created.BeneficiaryUserID, created.Amount, fmt.Sprintf("promotion manual settlement #%d", created.ID)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *promotionRepository) UpdatePromotionCommission(ctx context.Context, commissionID int64, operatorUserID *int64, amount float64, note string) (*service.PromotionCommissionRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	record, err := r.lockPromotionCommissionTx(ctx, tx, commissionID)
	if err != nil {
		return nil, err
	}
	if record.Status == service.PromotionCommissionStatusCancelled {
		return nil, service.ErrPromotionCommissionAlreadyDone
	}

	delta := amount - record.Amount
	if record.Status == service.PromotionCommissionStatusSettled && delta != 0 {
		if err := r.applyUserBalanceDeltaTx(ctx, tx, record.BeneficiaryUserID, delta, fmt.Sprintf("promotion commission edit #%d", record.ID)); err != nil {
			return nil, err
		}
	}

	row := tx.QueryRowContext(ctx, `
		UPDATE promotion_commission_records
		SET amount = $2,
		    note = COALESCE(NULLIF($3, ''), note),
		    created_by_user_id = COALESCE($4, created_by_user_id),
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
		          level_id, COALESCE(level_snapshot, ''), rate_snapshot, base_amount, amount, status,
		          settlement_batch_id, COALESCE(note, ''), created_by_user_id, settled_at, cancelled_at,
		          created_at, updated_at
	`, commissionID, amount, note, nullableInt64(operatorUserID))
	updated, err := scanPromotionCommissionRecordRow(row)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *promotionRepository) SettlePromotionCommission(ctx context.Context, commissionID int64, operatorUserID *int64, note string) (*service.PromotionCommissionRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := r.lockPromotionCommissionTx(ctx, tx, commissionID)
	if err != nil {
		return nil, err
	}
	if record.Status == service.PromotionCommissionStatusSettled || record.Status == service.PromotionCommissionStatusCancelled {
		return nil, service.ErrPromotionCommissionAlreadyDone
	}
	if err := r.applyUserBalanceDeltaTx(ctx, tx, record.BeneficiaryUserID, record.Amount, fmt.Sprintf("promotion commission settle #%d", record.ID)); err != nil {
		return nil, err
	}
	row := tx.QueryRowContext(ctx, `
		UPDATE promotion_commission_records
		SET status = $2,
		    settled_at = NOW(),
		    note = COALESCE(NULLIF($3, ''), note),
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
		          level_id, COALESCE(level_snapshot, ''), rate_snapshot, base_amount, amount, status,
		          settlement_batch_id, COALESCE(note, ''), created_by_user_id, settled_at, cancelled_at,
		          created_at, updated_at
	`, commissionID, service.PromotionCommissionStatusSettled, note)
	updated, err := scanPromotionCommissionRecordRow(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = operatorUserID
	return updated, nil
}

func (r *promotionRepository) BatchSettlePromotionCommissions(ctx context.Context, ids []int64, operatorUserID *int64, note string) (service.PromotionSettleSummary, error) {
	summary := service.PromotionSettleSummary{}
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		record, err := r.SettlePromotionCommission(ctx, id, operatorUserID, note)
		if err != nil {
			return summary, err
		}
		summary.SettledCount++
		summary.TotalAmount += record.Amount
	}
	return summary, nil
}

func (r *promotionRepository) CancelPromotionCommission(ctx context.Context, commissionID int64, operatorUserID *int64, note string) (*service.PromotionCommissionRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := r.lockPromotionCommissionTx(ctx, tx, commissionID)
	if err != nil {
		return nil, err
	}
	if record.Status == service.PromotionCommissionStatusCancelled {
		return nil, service.ErrPromotionCommissionAlreadyDone
	}
	if record.Status == service.PromotionCommissionStatusSettled {
		if err := r.applyUserBalanceDeltaTx(ctx, tx, record.BeneficiaryUserID, -record.Amount, fmt.Sprintf("promotion commission cancel #%d", record.ID)); err != nil {
			return nil, err
		}
	}
	row := tx.QueryRowContext(ctx, `
		UPDATE promotion_commission_records
		SET status = $2,
		    cancelled_at = NOW(),
		    note = COALESCE(NULLIF($3, ''), note),
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
		          level_id, COALESCE(level_snapshot, ''), rate_snapshot, base_amount, amount, status,
		          settlement_batch_id, COALESCE(note, ''), created_by_user_id, settled_at, cancelled_at,
		          created_at, updated_at
	`, commissionID, service.PromotionCommissionStatusCancelled, note)
	updated, err := scanPromotionCommissionRecordRow(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = operatorUserID
	return updated, nil
}

func (r *promotionRepository) ListPendingActivationCandidates(ctx context.Context, threshold float64, now time.Time) ([]service.PromotionActivationCandidate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT pu.user_id, pu.parent_user_id, COALESCE(SUM(ul.real_actual_cost), 0) AS usage_amount
		FROM promotion_users pu
		JOIN usage_logs ul ON ul.user_id = pu.user_id
		LEFT JOIN promotion_activations pa ON pa.user_id = pu.user_id
		WHERE pu.parent_user_id IS NOT NULL
		  AND pa.user_id IS NULL
		GROUP BY pu.user_id, pu.parent_user_id
		HAVING COALESCE(SUM(ul.real_actual_cost), 0) > $1
	`, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]service.PromotionActivationCandidate, 0)
	for rows.Next() {
		var item service.PromotionActivationCandidate
		if err := rows.Scan(&item.UserID, &item.PromoterUserID, &item.TriggerUsageAmount); err != nil {
			return nil, err
		}
		item.ActivatedAt = now
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *promotionRepository) CreatePromotionActivation(ctx context.Context, activation service.PromotionActivation, bonusAmount float64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO promotion_activations (user_id, promoter_user_id, activated_at, threshold_amount, trigger_usage_amount, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (user_id) DO NOTHING
	`, activation.UserID, activation.PromoterUserID, activation.ActivatedAt.UTC(), activation.ThresholdAmount, activation.TriggerUsageAmount)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return tx.Commit()
	}

	if bonusAmount > 0 {
		var commissionID int64
		row := tx.QueryRowContext(ctx, `
			INSERT INTO promotion_commission_records (
				beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
				base_amount, amount, status, note, created_at, updated_at
			)
			VALUES ($1, $2, $3::date, 'activation', 1, $4, $5, 'pending', 'promotion activation bonus', NOW(), NOW())
			ON CONFLICT (beneficiary_user_id, source_user_id)
			WHERE commission_type = 'activation' AND source_user_id IS NOT NULL
			DO NOTHING
			RETURNING id
		`, activation.PromoterUserID, activation.UserID, activation.ActivatedAt.Format("2006-01-02"), activation.TriggerUsageAmount, bonusAmount)
		_ = row.Scan(&commissionID)
		if commissionID > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE promotion_activations SET commission_record_id = $2, updated_at = NOW() WHERE user_id = $1`, activation.UserID, commissionID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (r *promotionRepository) UpsertDailyPromotionCommissions(ctx context.Context, businessDate time.Time, businessStart, businessEnd time.Time) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ul.user_id AS source_user_id,
		       pu.parent_user_id AS direct_parent_user_id,
		       parent_pu.parent_user_id AS indirect_parent_user_id,
		       COALESCE(SUM(ul.real_actual_cost), 0) AS base_amount
		FROM usage_logs ul
		JOIN promotion_users pu ON pu.user_id = ul.user_id
		JOIN promotion_activations pa ON pa.user_id = ul.user_id
		LEFT JOIN promotion_users parent_pu ON parent_pu.user_id = pu.parent_user_id
		WHERE pu.parent_user_id IS NOT NULL
		  AND ul.created_at >= $1
		  AND ul.created_at < $2
		  AND ul.real_actual_cost > 0
		  AND ul.created_at >= pa.activated_at
		GROUP BY ul.user_id, pu.parent_user_id, parent_pu.parent_user_id
	`, businessStart, businessEnd)
	if err != nil {
		return err
	}
	defer rows.Close()

	type aggregate struct {
		sourceUserID   int64
		directParent   sql.NullInt64
		indirectParent sql.NullInt64
		baseAmount     float64
	}
	var aggregates []aggregate
	for rows.Next() {
		var item aggregate
		if err := rows.Scan(&item.sourceUserID, &item.directParent, &item.indirectParent, &item.baseAmount); err != nil {
			return err
		}
		aggregates = append(aggregates, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(aggregates) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, item := range aggregates {
		if item.directParent.Valid {
			level, err := r.GetCurrentPromotionLevel(ctx, item.directParent.Int64)
			if err != nil {
				return err
			}
			if level != nil && level.Enabled && level.DirectRate > 0 {
				record := service.PromotionCommissionRecord{
					BeneficiaryUserID: item.directParent.Int64,
					SourceUserID:      &item.sourceUserID,
					BusinessDate:      businessDate,
					CommissionType:    service.PromotionCommissionTypeCommission,
					RelationDepth:     1,
					LevelID:           &level.ID,
					LevelSnapshot:     level.LevelName,
					BaseAmount:        item.baseAmount,
					Amount:            roundPromotionAmount(item.baseAmount * level.DirectRate / 100),
				}
				if record.Amount > 0 {
					if err := upsertDailyPromotionCommissionTx(ctx, tx, record, level.DirectRate); err != nil {
						return err
					}
				}
			}
		}
		if item.indirectParent.Valid {
			level, err := r.GetCurrentPromotionLevel(ctx, item.indirectParent.Int64)
			if err != nil {
				return err
			}
			if level != nil && level.Enabled && level.IndirectRate > 0 {
				record := service.PromotionCommissionRecord{
					BeneficiaryUserID: item.indirectParent.Int64,
					SourceUserID:      &item.sourceUserID,
					BusinessDate:      businessDate,
					CommissionType:    service.PromotionCommissionTypeCommission,
					RelationDepth:     2,
					LevelID:           &level.ID,
					LevelSnapshot:     level.LevelName,
					BaseAmount:        item.baseAmount,
					Amount:            roundPromotionAmount(item.baseAmount * level.IndirectRate / 100),
				}
				if record.Amount > 0 {
					if err := upsertDailyPromotionCommissionTx(ctx, tx, record, level.IndirectRate); err != nil {
						return err
					}
				}
			}
		}
	}
	return tx.Commit()
}

func (r *promotionRepository) ListSettlablePromotionBusinessDates(ctx context.Context, boundaryDate time.Time) ([]time.Time, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT business_date
		FROM promotion_commission_records
		WHERE status = 'pending'
		  AND business_date < $1::date
		ORDER BY business_date ASC
	`, boundaryDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dates []time.Time
	for rows.Next() {
		var businessDate time.Time
		if err := rows.Scan(&businessDate); err != nil {
			return nil, err
		}
		dates = append(dates, businessDate)
	}
	return dates, rows.Err()
}

func (r *promotionRepository) SettlePromotionBusinessDate(ctx context.Context, businessDate time.Time, operatorUserID *int64, note string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	batchID, err := ensurePromotionBatchTx(ctx, tx, businessDate, operatorUserID, note)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT beneficiary_user_id, COALESCE(SUM(amount), 0)
		FROM promotion_commission_records
		WHERE business_date = $1::date
		  AND status = 'pending'
		GROUP BY beneficiary_user_id
	`, businessDate.Format("2006-01-02"))
	if err != nil {
		return err
	}
	defer rows.Close()
	type beneficiarySettlement struct {
		userID int64
		amount float64
	}
	beneficiaries := make([]beneficiarySettlement, 0)
	totalAmount := 0.0
	for rows.Next() {
		var userID int64
		var amount float64
		if err := rows.Scan(&userID, &amount); err != nil {
			return err
		}
		beneficiaries = append(beneficiaries, beneficiarySettlement{userID: userID, amount: amount})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, beneficiary := range beneficiaries {
		if err := r.applyUserBalanceDeltaTx(ctx, tx, beneficiary.userID, beneficiary.amount, fmt.Sprintf("promotion settlement %s", businessDate.Format("2006-01-02"))); err != nil {
			return err
		}
		totalAmount += beneficiary.amount
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE promotion_commission_records
		SET status = 'settled',
		    settled_at = NOW(),
		    settlement_batch_id = $2,
		    updated_at = NOW()
		WHERE business_date = $1::date
		  AND status = 'pending'
	`, businessDate.Format("2006-01-02"), batchID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `
		UPDATE promotion_settlement_batches
		SET status = $2,
		    total_records = $3,
		    total_amount = $4,
		    executed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, batchID, service.PromotionSettlementStatusSettled, affected, totalAmount); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *promotionRepository) lockPromotionCommissionTx(ctx context.Context, tx *sql.Tx, commissionID int64) (*service.PromotionCommissionRecord, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
		       level_id, COALESCE(level_snapshot, ''), rate_snapshot, base_amount, amount, status,
		       settlement_batch_id, COALESCE(note, ''), created_by_user_id, settled_at, cancelled_at,
		       created_at, updated_at
		FROM promotion_commission_records
		WHERE id = $1
		FOR UPDATE
	`, commissionID)
	return scanPromotionCommissionRecordRow(row)
}

func (r *promotionRepository) applyUserBalanceDeltaTx(ctx context.Context, tx *sql.Tx, userID int64, delta float64, note string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE users SET balance = balance + $2, updated_at = NOW() WHERE id = $1`, userID, delta); err != nil {
		return err
	}
	code, err := service.GenerateRedeemCode()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO redeem_codes (code, type, value, status, used_by, used_at, notes, created_at, group_id, validity_days)
		VALUES ($1, $2, $3, $4, $5, NOW(), $6, NOW(), NULL, 30)
	`, code, service.AdjustmentTypePromotionBalance, delta, service.StatusUsed, userID, nullableTrimmedString(note))
	return err
}

func ensurePromotionBatchTx(ctx context.Context, tx *sql.Tx, businessDate time.Time, operatorUserID *int64, note string) (int64, error) {
	var batchID int64
	row := tx.QueryRowContext(ctx, `
		INSERT INTO promotion_settlement_batches (business_date, status, total_records, total_amount, executed_by_user_id, note, created_at, updated_at)
		VALUES ($1::date, $2, 0, 0, $3, $4, NOW(), NOW())
		ON CONFLICT (business_date) DO UPDATE
		SET status = EXCLUDED.status,
		    executed_by_user_id = EXCLUDED.executed_by_user_id,
		    note = COALESCE(EXCLUDED.note, promotion_settlement_batches.note),
		    updated_at = NOW()
		RETURNING id
	`, businessDate.Format("2006-01-02"), service.PromotionSettlementStatusRunning, nullableInt64(operatorUserID), nullableTrimmedString(note))
	if err := row.Scan(&batchID); err != nil {
		return 0, err
	}
	return batchID, nil
}

func upsertDailyPromotionCommissionTx(ctx context.Context, tx *sql.Tx, record service.PromotionCommissionRecord, rate float64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO promotion_commission_records (
			beneficiary_user_id, source_user_id, business_date, commission_type, relation_depth,
			level_id, level_snapshot, rate_snapshot, base_amount, amount, status, note, created_at, updated_at
		)
		VALUES ($1, $2, $3::date, 'commission', $4, $5, $6, $7, $8, $9, 'pending', NULL, NOW(), NOW())
		ON CONFLICT (business_date, beneficiary_user_id, source_user_id, relation_depth)
		WHERE commission_type = 'commission' AND source_user_id IS NOT NULL
		DO UPDATE SET
			level_id = EXCLUDED.level_id,
			level_snapshot = EXCLUDED.level_snapshot,
			rate_snapshot = EXCLUDED.rate_snapshot,
			base_amount = EXCLUDED.base_amount,
			amount = EXCLUDED.amount,
			updated_at = NOW()
		WHERE promotion_commission_records.status = 'pending'
	`, record.BeneficiaryUserID, nullableInt64(record.SourceUserID), record.BusinessDate.Format("2006-01-02"), record.RelationDepth, nullableInt64(record.LevelID), nullableTrimmedString(record.LevelSnapshot), rate, record.BaseAmount, record.Amount)
	return err
}

func scanPromotionCommissionRecordRow(row interface{ Scan(dest ...any) error }) (*service.PromotionCommissionRecord, error) {
	var item service.PromotionCommissionRecord
	var sourceUserID sql.NullInt64
	var levelID sql.NullInt64
	var rateSnapshot sql.NullFloat64
	var settlementBatchID sql.NullInt64
	var createdByUserID sql.NullInt64
	var settledAt sql.NullTime
	var cancelledAt sql.NullTime
	if err := row.Scan(&item.ID, &item.BeneficiaryUserID, &sourceUserID, &item.BusinessDate, &item.CommissionType, &item.RelationDepth, &levelID, &item.LevelSnapshot, &rateSnapshot, &item.BaseAmount, &item.Amount, &item.Status, &settlementBatchID, &item.Note, &createdByUserID, &settledAt, &cancelledAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrPromotionCommissionNotFound
		}
		return nil, err
	}
	if sourceUserID.Valid {
		item.SourceUserID = &sourceUserID.Int64
	}
	if levelID.Valid {
		item.LevelID = &levelID.Int64
	}
	if rateSnapshot.Valid {
		item.RateSnapshot = &rateSnapshot.Float64
	}
	if settlementBatchID.Valid {
		item.SettlementBatchID = &settlementBatchID.Int64
	}
	if createdByUserID.Valid {
		item.CreatedByUserID = &createdByUserID.Int64
	}
	if settledAt.Valid {
		item.SettledAt = &settledAt.Time
	}
	if cancelledAt.Valid {
		item.CancelledAt = &cancelledAt.Time
	}
	return &item, nil
}

func roundPromotionAmount(amount float64) float64 {
	if amount < 0 {
		return -roundPromotionAmount(-amount)
	}
	return float64(int64(amount*1e8+0.5)) / 1e8
}
