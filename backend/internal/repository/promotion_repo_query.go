package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *promotionRepository) CountPromotionDescendants(ctx context.Context, userID int64) (int, error) {
	return r.countPromotionTree(ctx, userID, false)
}

func (r *promotionRepository) CountPromotionActivatedDescendants(ctx context.Context, userID int64) (int, error) {
	return r.countPromotionTree(ctx, userID, true)
}

func (r *promotionRepository) CountDirectActivatedInvites(ctx context.Context, userID int64) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_activations WHERE promoter_user_id = $1`, userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *promotionRepository) GetCurrentPromotionLevel(ctx context.Context, userID int64) (*service.PromotionLevelConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH activated AS (
			SELECT COUNT(*) AS cnt
			FROM promotion_activations
			WHERE promoter_user_id = $1
		)
		SELECT id, level_no, level_name, required_activated_invites, direct_rate, indirect_rate, sort_order, enabled, created_at, updated_at
		FROM promotion_level_configs
		WHERE enabled = TRUE
		  AND required_activated_invites <= (SELECT cnt FROM activated)
		ORDER BY required_activated_invites DESC, level_no DESC
		LIMIT 1
	`, userID)
	return scanPromotionLevelRow(row)
}

func (r *promotionRepository) GetNextPromotionLevel(ctx context.Context, userID int64) (*service.PromotionLevelConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH activated AS (
			SELECT COUNT(*) AS cnt
			FROM promotion_activations
			WHERE promoter_user_id = $1
		)
		SELECT id, level_no, level_name, required_activated_invites, direct_rate, indirect_rate, sort_order, enabled, created_at, updated_at
		FROM promotion_level_configs
		WHERE enabled = TRUE
		  AND required_activated_invites > (SELECT cnt FROM activated)
		ORDER BY required_activated_invites ASC, level_no ASC
		LIMIT 1
	`, userID)
	return scanPromotionLevelRow(row)
}

func (r *promotionRepository) GetPromotionOverviewSummary(ctx context.Context, userID int64, businessDate time.Time) (*service.PromotionOverview, error) {
	totalInvites, err := r.CountPromotionDescendants(ctx, userID)
	if err != nil {
		return nil, err
	}
	activatedInvites, err := r.CountPromotionActivatedDescendants(ctx, userID)
	if err != nil {
		return nil, err
	}
	directActivated, err := r.CountDirectActivatedInvites(ctx, userID)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status <> 'cancelled' AND business_date = $2::date THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'pending' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'settled' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status <> 'cancelled' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status <> 'cancelled' AND commission_type = 'commission' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status <> 'cancelled' AND commission_type = 'activation' THEN amount ELSE 0 END), 0)
		FROM promotion_commission_records
		WHERE beneficiary_user_id = $1
	`, userID, businessDate.Format("2006-01-02"))
	out := &service.PromotionOverview{
		TotalInvites:           totalInvites,
		ActivatedInvites:       activatedInvites,
		InactiveInvites:        totalInvites - activatedInvites,
		CurrentDirectActivated: directActivated,
	}
	if err := row.Scan(
		&out.TodayEarnings,
		&out.PendingAmount,
		&out.SettledAmount,
		&out.TotalRewardAmount,
		&out.CommissionAmount,
		&out.ActivationAmount,
	); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *promotionRepository) ListPromotionLeaderboard(ctx context.Context, limit int) ([]service.PromotionLeaderboardItem, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT pcr.beneficiary_user_id, u.email, COALESCE(SUM(pcr.amount), 0) AS total_amount
		FROM promotion_commission_records pcr
		JOIN users u ON u.id = pcr.beneficiary_user_id
		WHERE u.deleted_at IS NULL
		  AND pcr.status <> 'cancelled'
		  AND pcr.commission_type IN ('commission', 'activation')
		GROUP BY pcr.beneficiary_user_id, u.email
		ORDER BY total_amount DESC, pcr.beneficiary_user_id ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]service.PromotionLeaderboardItem, 0, limit)
	for rows.Next() {
		var item service.PromotionLeaderboardItem
		var email string
		if err := rows.Scan(&item.UserID, &email, &item.TotalEarnings); err != nil {
			return nil, err
		}
		item.MaskedEmail = service.MaskEmail(email)
		item.InviteCount, _ = r.CountPromotionDescendants(ctx, item.UserID)
		level, _ := r.GetCurrentPromotionLevel(ctx, item.UserID)
		if level != nil {
			item.LevelName = level.LevelName
			item.CurrentLevelNo = level.LevelNo
		} else {
			item.LevelName = "未设置"
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *promotionRepository) ListPromotionTeam(ctx context.Context, rootUserID int64, filter service.PromotionTeamFilter, todayStart, todayEnd time.Time) ([]service.PromotionTeamItem, int64, error) {
	return r.listPromotionTree(ctx, rootUserID, filter, todayStart, todayEnd)
}

func (r *promotionRepository) ListPromotionEarnings(ctx context.Context, userID int64, filter service.PromotionCommissionFilter) ([]service.PromotionCommissionListItem, int64, error) {
	where, args := buildPromotionCommissionWhere(filter.Type, filter.Status, filter.Keyword, nil, nil, userID, false)
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM promotion_commission_records pcr
		JOIN users bu ON bu.id = pcr.beneficiary_user_id
		LEFT JOIN users su ON su.id = pcr.source_user_id
		LEFT JOIN promotion_users bpu ON bpu.user_id = pcr.beneficiary_user_id
		LEFT JOIN promotion_users spu ON spu.user_id = pcr.source_user_id
		`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := r.db.QueryContext(ctx, `
		SELECT pcr.id, pcr.beneficiary_user_id, bu.email, pcr.source_user_id, COALESCE(su.email, ''),
		       pcr.commission_type, pcr.relation_depth, pcr.business_date, pcr.base_amount, pcr.amount,
		       pcr.status, COALESCE(pcr.level_snapshot, ''), pcr.rate_snapshot, COALESCE(pcr.note, ''),
		       pcr.settled_at, pcr.cancelled_at, pcr.created_at, pcr.settlement_batch_id, psb.business_date
		FROM promotion_commission_records pcr
		JOIN users bu ON bu.id = pcr.beneficiary_user_id
		LEFT JOIN users su ON su.id = pcr.source_user_id
		LEFT JOIN promotion_users bpu ON bpu.user_id = pcr.beneficiary_user_id
		LEFT JOIN promotion_users spu ON spu.user_id = pcr.source_user_id
		LEFT JOIN promotion_settlement_batches psb ON psb.id = pcr.settlement_batch_id
		`+where+`
		ORDER BY pcr.business_date DESC, pcr.created_at DESC, pcr.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanPromotionCommissionItems(rows)
	return items, total, err
}

func (r *promotionRepository) GetPromotionAdminDashboard(ctx context.Context, businessDate time.Time) (*service.PromotionAdminDashboard, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status = 'settled' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'pending' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'pending' AND business_date = $1::date THEN amount ELSE 0 END), 0)
		FROM promotion_commission_records
	`, businessDate.Format("2006-01-02"))
	out := &service.PromotionAdminDashboard{}
	if err := row.Scan(&out.TotalSettledAmount, &out.PendingAmount, &out.TodayPendingAmount); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_users WHERE parent_user_id IS NOT NULL`).Scan(&out.BoundUsers); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_activations`).Scan(&out.ActivatedUsers); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_users WHERE bound_at >= $1 AND bound_at < $2`, businessDate.UTC(), businessDate.AddDate(0, 0, 1).UTC()).Scan(&out.TodayNewBindings); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_activations WHERE activated_at >= $1 AND activated_at < $2`, businessDate.UTC(), businessDate.AddDate(0, 0, 1).UTC()).Scan(&out.TodayNewActivates); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *promotionRepository) SearchPromotionRelationUserIDs(ctx context.Context, keyword string, page, pageSize int) ([]int64, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	conditions := []string{"u.deleted_at IS NULL"}
	args := make([]any, 0, 2)
	if keyword != "" {
		args = append(args, promotionKeywordPattern(keyword))
		conditions = append(conditions, fmt.Sprintf("(u.email ILIKE $%d ESCAPE '\\' OR COALESCE(u.username, '') ILIKE $%d ESCAPE '\\' OR CAST(u.id AS TEXT) ILIKE $%d ESCAPE '\\' OR COALESCE(pu.invite_code, '') ILIKE $%d ESCAPE '\\')", len(args), len(args), len(args), len(args)))
	}
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM users u
		LEFT JOIN promotion_users pu ON pu.user_id = u.id
		WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id
		FROM users u
		LEFT JOIN promotion_users pu ON pu.user_id = u.id
		WHERE `+where+`
		ORDER BY u.created_at DESC, u.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	return ids, total, rows.Err()
}

func (r *promotionRepository) ListPromotionRelationsByUserIDs(ctx context.Context, userIDs []int64) ([]service.PromotionRelationRow, error) {
	if len(userIDs) == 0 {
		return []service.PromotionRelationRow{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.email, u.username, COALESCE(pu.invite_code, ''), pu.parent_user_id, COALESCE(parent_user.email, ''), pu.bound_at
		FROM users u
		LEFT JOIN promotion_users pu ON pu.user_id = u.id
		LEFT JOIN users parent_user ON parent_user.id = pu.parent_user_id
		WHERE u.deleted_at IS NULL
		  AND u.id = ANY($1)
	`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[int64]service.PromotionRelationRow, len(userIDs))
	for rows.Next() {
		var item service.PromotionRelationRow
		var parentUserID sql.NullInt64
		var boundAt sql.NullTime
		if err := rows.Scan(&item.UserID, &item.Email, &item.Username, &item.InviteCode, &parentUserID, &item.ParentEmail, &boundAt); err != nil {
			return nil, err
		}
		if parentUserID.Valid {
			item.ParentUserID = &parentUserID.Int64
		}
		if boundAt.Valid {
			item.BoundAt = &boundAt.Time
		}
		item.DirectChildrenCount, _ = r.countDirectChildren(ctx, item.UserID)
		item.TotalChildrenCount, _ = r.CountPromotionDescendants(ctx, item.UserID)
		item.ActivatedDirectCount, _ = r.CountDirectActivatedInvites(ctx, item.UserID)
		byID[item.UserID] = item
	}
	items := make([]service.PromotionRelationRow, 0, len(userIDs))
	for _, userID := range userIDs {
		if item, ok := byID[userID]; ok {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}

func (r *promotionRepository) GetPromotionRelationChain(ctx context.Context, userID int64) (*service.PromotionRelationChain, error) {
	current, err := r.loadPromotionRelationNode(ctx, userID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	chain := &service.PromotionRelationChain{Current: current}
	pu, err := r.GetPromotionUserByUserID(ctx, userID)
	if err != nil || pu == nil || pu.ParentUserID == nil {
		return chain, nil
	}
	parent, _ := r.loadPromotionRelationNode(ctx, *pu.ParentUserID)
	if parent != nil {
		if level, levelErr := r.GetCurrentPromotionLevel(ctx, *pu.ParentUserID); levelErr == nil && level != nil {
			rate := level.DirectRate
			parent.ActualRebateRate = &rate
		}
	}
	chain.Parent = parent
	parentPU, err := r.GetPromotionUserByUserID(ctx, *pu.ParentUserID)
	if err == nil && parentPU != nil && parentPU.ParentUserID != nil {
		chain.Grandparent, _ = r.loadPromotionRelationNode(ctx, *parentPU.ParentUserID)
		if chain.Grandparent != nil {
			if level, levelErr := r.GetCurrentPromotionLevel(ctx, *parentPU.ParentUserID); levelErr == nil && level != nil {
				rate := level.IndirectRate
				chain.Grandparent.ActualRebateRate = &rate
			}
		}
	}
	return chain, nil
}

func (r *promotionRepository) ListPromotionDownlines(ctx context.Context, rootUserID int64, filter service.PromotionTeamFilter, todayStart, todayEnd time.Time) ([]service.PromotionTeamItem, int64, error) {
	return r.listPromotionTree(ctx, rootUserID, filter, todayStart, todayEnd)
}

func (r *promotionRepository) RemovePromotionDirectDownline(ctx context.Context, parentUserID, downlineUserID int64, note string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE promotion_users
		SET parent_user_id = NULL,
		    binding_source = $3,
		    bound_note = $4,
		    bound_at = NOW(),
		    updated_at = NOW()
		WHERE user_id = $2
		  AND parent_user_id = $1
	`, parentUserID, downlineUserID, service.PromotionBindingSourceAdmin, nullableTrimmedString(note))
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return service.ErrPromotionDownlineNotDirect
	}
	return nil
}

func (r *promotionRepository) ListPromotionCommissions(ctx context.Context, filter service.PromotionCommissionAdminFilter) ([]service.PromotionCommissionListItem, int64, error) {
	where, args := buildPromotionCommissionWhere(filter.Type, filter.Status, filter.Keyword, filter.DateFrom, filter.DateTo, 0, true)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_commission_records pcr
		JOIN users bu ON bu.id = pcr.beneficiary_user_id
		LEFT JOIN users su ON su.id = pcr.source_user_id
		LEFT JOIN promotion_users bpu ON bpu.user_id = pcr.beneficiary_user_id
		LEFT JOIN promotion_users spu ON spu.user_id = pcr.source_user_id `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := r.db.QueryContext(ctx, `
		SELECT pcr.id, pcr.beneficiary_user_id, bu.email, pcr.source_user_id, COALESCE(su.email, ''),
		       pcr.commission_type, pcr.relation_depth, pcr.business_date, pcr.base_amount, pcr.amount,
		       pcr.status, COALESCE(pcr.level_snapshot, ''), pcr.rate_snapshot, COALESCE(pcr.note, ''),
		       pcr.settled_at, pcr.cancelled_at, pcr.created_at, pcr.settlement_batch_id, psb.business_date
		FROM promotion_commission_records pcr
		JOIN users bu ON bu.id = pcr.beneficiary_user_id
		LEFT JOIN users su ON su.id = pcr.source_user_id
		LEFT JOIN promotion_users bpu ON bpu.user_id = pcr.beneficiary_user_id
		LEFT JOIN promotion_users spu ON spu.user_id = pcr.source_user_id
		LEFT JOIN promotion_settlement_batches psb ON psb.id = pcr.settlement_batch_id
		`+where+`
		ORDER BY pcr.business_date DESC, pcr.created_at DESC, pcr.id DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanPromotionCommissionItems(rows)
	return items, total, err
}

func (r *promotionRepository) listPromotionTree(ctx context.Context, rootUserID int64, filter service.PromotionTeamFilter, todayStart, todayEnd time.Time) ([]service.PromotionTeamItem, int64, error) {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	totalWhere := ""
	listWhere := ""
	switch filter.Status {
	case "active":
		totalWhere = "WHERE pa.user_id IS NOT NULL"
		listWhere = "WHERE pa.user_id IS NOT NULL"
	case "inactive":
		totalWhere = "WHERE pa.user_id IS NULL"
		listWhere = "WHERE pa.user_id IS NULL"
	}
	if filter.Keyword != "" {
		if totalWhere == "" {
			totalWhere = "WHERE "
		} else {
			totalWhere += " AND "
		}
		if listWhere == "" {
			listWhere = "WHERE "
		} else {
			listWhere += " AND "
		}
		totalWhere += "tree.depth <= 2 AND (u.email ILIKE $2 ESCAPE '\\' OR COALESCE(u.username, '') ILIKE $2 ESCAPE '\\' OR CAST(tree.user_id AS TEXT) ILIKE $2 ESCAPE '\\')"
		listWhere += "tree.depth <= 2 AND (u.email ILIKE $4 ESCAPE '\\' OR COALESCE(u.username, '') ILIKE $4 ESCAPE '\\' OR CAST(tree.user_id AS TEXT) ILIKE $4 ESCAPE '\\')"
	}
	totalArgs := []any{rootUserID}
	if filter.Keyword != "" {
		totalArgs = append(totalArgs, promotionKeywordPattern(filter.Keyword))
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE tree AS (
			SELECT pu.user_id, 1 AS depth
			FROM promotion_users pu
			WHERE pu.parent_user_id = $1
			UNION ALL
			SELECT pu.user_id, tree.depth + 1
			FROM promotion_users pu
			JOIN tree ON pu.parent_user_id = tree.user_id
			WHERE tree.depth < 32
		)
		SELECT COUNT(*)
		FROM tree
		JOIN users u ON u.id = tree.user_id AND u.deleted_at IS NULL
		LEFT JOIN promotion_activations pa ON pa.user_id = tree.user_id
		`+totalWhere, totalArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listArgs := []any{rootUserID, todayStart, todayEnd}
	if filter.Keyword != "" {
		listArgs = append(listArgs, promotionKeywordPattern(filter.Keyword))
	}
	listArgs = append(listArgs, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE tree AS (
			SELECT pu.user_id, 1 AS depth
			FROM promotion_users pu
			WHERE pu.parent_user_id = $1
			UNION ALL
			SELECT pu.user_id, tree.depth + 1
			FROM promotion_users pu
			JOIN tree ON pu.parent_user_id = tree.user_id
			WHERE tree.depth < 32
		),
		today_usage AS (
			SELECT user_id, COALESCE(SUM(real_actual_cost), 0) AS total_cost
			FROM usage_logs
			WHERE created_at >= $2
			  AND created_at < $3
			GROUP BY user_id
		),
		total_usage AS (
			SELECT user_id, COALESCE(SUM(real_actual_cost), 0) AS total_cost
			FROM usage_logs
			GROUP BY user_id
		)
		SELECT tree.user_id, u.email, COALESCE(u.username, ''), tree.depth, u.created_at, pa.activated_at,
		       COALESCE(today_usage.total_cost, 0), COALESCE(total_usage.total_cost, 0)
		FROM tree
		JOIN users u ON u.id = tree.user_id AND u.deleted_at IS NULL
		LEFT JOIN promotion_activations pa ON pa.user_id = tree.user_id
		LEFT JOIN today_usage ON today_usage.user_id = tree.user_id
		LEFT JOIN total_usage ON total_usage.user_id = tree.user_id
		`+listWhere+`
		ORDER BY `+buildPromotionTeamOrderClause(filter)+`
		LIMIT $`+fmt.Sprint(len(listArgs)-1)+` OFFSET $`+fmt.Sprint(len(listArgs))+`
	`, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]service.PromotionTeamItem, 0)
	for rows.Next() {
		var item service.PromotionTeamItem
		var activatedAt sql.NullTime
		if err := rows.Scan(&item.UserID, &item.Email, &item.Username, &item.RelationDepth, &item.JoinedAt, &activatedAt, &item.TodayContribution, &item.TotalContribution); err != nil {
			return nil, 0, err
		}
		item.MaskedEmail = service.MaskEmail(item.Email)
		item.Activated = activatedAt.Valid
		if activatedAt.Valid {
			item.ActivatedAt = &activatedAt.Time
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *promotionRepository) countPromotionTree(ctx context.Context, userID int64, activatedOnly bool) (int, error) {
	query := `
		WITH RECURSIVE tree AS (
			SELECT pu.user_id
			FROM promotion_users pu
			WHERE pu.parent_user_id = $1
			UNION ALL
			SELECT pu.user_id
			FROM promotion_users pu
			JOIN tree ON pu.parent_user_id = tree.user_id
		)
		SELECT COUNT(*) FROM tree`
	if activatedOnly {
		query += ` JOIN promotion_activations pa ON pa.user_id = tree.user_id`
	}
	var count int
	if err := r.db.QueryRowContext(ctx, query, userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *promotionRepository) countDirectChildren(ctx context.Context, userID int64) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_users WHERE parent_user_id = $1`, userID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *promotionRepository) loadPromotionRelationNode(ctx context.Context, userID int64) (*service.PromotionRelationNode, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, COALESCE(pu.invite_code, '')
		FROM users u
		LEFT JOIN promotion_users pu ON pu.user_id = u.id
		WHERE u.id = $1
		  AND u.deleted_at IS NULL
	`, userID)
	node := &service.PromotionRelationNode{}
	if err := row.Scan(&node.UserID, &node.Email, &node.InviteCode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	level, _ := r.GetCurrentPromotionLevel(ctx, userID)
	if level != nil {
		node.LevelName = level.LevelName
		node.TotalRate = level.DirectRate + level.IndirectRate
	} else {
		node.LevelName = "未设置"
	}
	node.InviteCount, _ = r.CountPromotionDescendants(ctx, userID)
	return node, nil
}

func buildPromotionCommissionWhere(commissionType, status, keyword string, dateFrom, dateTo *time.Time, userID int64, admin bool) (string, []any) {
	conditions := []string{"WHERE 1=1"}
	args := make([]any, 0, 6)
	if admin {
		conditions = append(conditions, "AND bu.deleted_at IS NULL")
	} else {
		args = append(args, userID)
		conditions = append(conditions, fmt.Sprintf("AND pcr.beneficiary_user_id = $%d", len(args)))
	}
	if commissionType != "" && commissionType != "all" {
		args = append(args, commissionType)
		conditions = append(conditions, fmt.Sprintf("AND pcr.commission_type = $%d", len(args)))
	}
	if status != "" && status != "all" {
		args = append(args, status)
		conditions = append(conditions, fmt.Sprintf("AND pcr.status = $%d", len(args)))
	}
	if keyword != "" {
		args = append(args, promotionKeywordPattern(keyword))
		conditions = append(conditions, fmt.Sprintf("AND (bu.email ILIKE $%d ESCAPE '\\' OR COALESCE(bu.username, '') ILIKE $%d ESCAPE '\\' OR COALESCE(bpu.invite_code, '') ILIKE $%d ESCAPE '\\' OR COALESCE(su.email, '') ILIKE $%d ESCAPE '\\' OR COALESCE(su.username, '') ILIKE $%d ESCAPE '\\' OR COALESCE(spu.invite_code, '') ILIKE $%d ESCAPE '\\' OR CAST(pcr.beneficiary_user_id AS TEXT) ILIKE $%d ESCAPE '\\' OR CAST(COALESCE(pcr.source_user_id, 0) AS TEXT) ILIKE $%d ESCAPE '\\')", len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	if dateFrom != nil {
		args = append(args, dateFrom.Format("2006-01-02"))
		conditions = append(conditions, fmt.Sprintf("AND pcr.business_date >= $%d::date", len(args)))
	}
	if dateTo != nil {
		args = append(args, dateTo.Format("2006-01-02"))
		conditions = append(conditions, fmt.Sprintf("AND pcr.business_date <= $%d::date", len(args)))
	}
	return strings.Join(conditions, " "), args
}

func buildPromotionTeamOrderClause(filter service.PromotionTeamFilter) string {
	order := "DESC"
	if strings.EqualFold(filter.SortOrder, "asc") {
		order = "ASC"
	}
	switch filter.SortBy {
	case "total_contribution":
		return "COALESCE(total_usage.total_cost, 0) " + order + ", tree.user_id DESC"
	case "joined_at":
		return "u.created_at " + order + ", tree.user_id DESC"
	case "activated_at":
		return "pa.activated_at " + order + " NULLS LAST, tree.user_id DESC"
	default:
		return "COALESCE(today_usage.total_cost, 0) " + order + ", tree.user_id DESC"
	}
}

func promotionKeywordPattern(keyword string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(strings.TrimSpace(keyword)) + "%"
}

func scanPromotionUser(row interface{ Scan(dest ...any) error }) (*service.PromotionUser, error) {
	var item service.PromotionUser
	var parentUserID sql.NullInt64
	var boundAt sql.NullTime
	var boundNote sql.NullString
	if err := row.Scan(&item.UserID, &item.InviteCode, &parentUserID, &item.BindingSource, &boundAt, &boundNote, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}
	if parentUserID.Valid {
		item.ParentUserID = &parentUserID.Int64
	}
	if boundAt.Valid {
		item.BoundAt = &boundAt.Time
	}
	if boundNote.Valid {
		item.BoundNote = boundNote.String
	}
	return &item, nil
}

func scanPromotionLevelRow(row interface{ Scan(dest ...any) error }) (*service.PromotionLevelConfig, error) {
	var item service.PromotionLevelConfig
	if err := row.Scan(&item.ID, &item.LevelNo, &item.LevelName, &item.RequiredActivatedInvites, &item.DirectRate, &item.IndirectRate, &item.SortOrder, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func scanPromotionScript(rows *sql.Rows) (service.PromotionScript, error) {
	var item service.PromotionScript
	var createdBy sql.NullInt64
	if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Content, &item.UseCount, &item.Enabled, &createdBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	if createdBy.Valid {
		item.CreatedByUserID = &createdBy.Int64
	}
	return item, nil
}

func scanPromotionScriptRow(row interface{ Scan(dest ...any) error }) (*service.PromotionScript, error) {
	var item service.PromotionScript
	var createdBy sql.NullInt64
	if err := row.Scan(&item.ID, &item.Name, &item.Category, &item.Content, &item.UseCount, &item.Enabled, &createdBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrPromotionScriptNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		item.CreatedByUserID = &createdBy.Int64
	}
	return &item, nil
}

func scanPromotionCommissionItems(rows *sql.Rows) ([]service.PromotionCommissionListItem, error) {
	items := make([]service.PromotionCommissionListItem, 0)
	for rows.Next() {
		item, err := scanPromotionCommissionItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanPromotionCommissionItem(row interface{ Scan(dest ...any) error }) (service.PromotionCommissionListItem, error) {
	var item service.PromotionCommissionListItem
	var sourceUserID sql.NullInt64
	var rateSnapshot sql.NullFloat64
	var settledAt sql.NullTime
	var cancelledAt sql.NullTime
	var batchID sql.NullInt64
	var batchDate sql.NullTime
	if err := row.Scan(&item.ID, &item.BeneficiaryUserID, &item.BeneficiaryEmail, &sourceUserID, &item.SourceUserEmail, &item.CommissionType, &item.RelationDepth, &item.BusinessDate, &item.BaseAmount, &item.Amount, &item.Status, &item.LevelName, &rateSnapshot, &item.Note, &settledAt, &cancelledAt, &item.CreatedAt, &batchID, &batchDate); err != nil {
		return item, err
	}
	item.BeneficiaryMasked = service.MaskEmail(item.BeneficiaryEmail)
	item.SourceUserMasked = service.MaskEmail(item.SourceUserEmail)
	if sourceUserID.Valid {
		item.SourceUserID = &sourceUserID.Int64
	}
	if rateSnapshot.Valid {
		item.RateSnapshot = &rateSnapshot.Float64
	}
	if settledAt.Valid {
		item.SettledAt = &settledAt.Time
	}
	if cancelledAt.Valid {
		item.CancelledAt = &cancelledAt.Time
	}
	if batchID.Valid {
		item.SettlementBatchID = &batchID.Int64
	}
	if batchDate.Valid {
		item.SettlementBatchDate = &batchDate.Time
	}
	return item, nil
}
