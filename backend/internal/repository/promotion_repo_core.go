package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type promotionRepository struct {
	db *sql.DB
}

func NewPromotionRepository(sqlDB *sql.DB) service.PromotionRepository {
	return &promotionRepository{db: sqlDB}
}

func (r *promotionRepository) EnsurePromotionUser(ctx context.Context, userID int64) (*service.PromotionUser, error) {
	if userID <= 0 {
		return nil, service.ErrPromotionInvalidBindRequest
	}
	if existing, err := r.GetPromotionUserByUserID(ctx, userID); err == nil && existing != nil {
		return existing, nil
	}
	for i := 0; i < 10; i++ {
		code, err := generatePromotionInviteCode()
		if err != nil {
			return nil, err
		}
		_, err = r.db.ExecContext(ctx, `
			INSERT INTO promotion_users (user_id, invite_code, binding_source, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
			ON CONFLICT (user_id) DO NOTHING
		`, userID, code, service.PromotionBindingSourceSelf)
		if err != nil {
			if isDuplicateKey(err) {
				continue
			}
			return nil, err
		}
		return r.GetPromotionUserByUserID(ctx, userID)
	}
	return nil, fmt.Errorf("failed to allocate unique promotion invite code for user %d", userID)
}

func (r *promotionRepository) GetPromotionUserByUserID(ctx context.Context, userID int64) (*service.PromotionUser, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT user_id, invite_code, parent_user_id, binding_source, bound_at, bound_note, created_at, updated_at
		FROM promotion_users
		WHERE user_id = $1
	`, userID)
	return scanPromotionUser(row)
}

func (r *promotionRepository) GetPromotionUserByInviteCode(ctx context.Context, inviteCode string) (*service.PromotionUser, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT user_id, invite_code, parent_user_id, binding_source, bound_at, bound_note, created_at, updated_at
		FROM promotion_users
		WHERE invite_code = $1
	`, inviteCode)
	return scanPromotionUser(row)
}

func (r *promotionRepository) SetPromotionParent(ctx context.Context, userID int64, parentUserID *int64, source, note string, boundAt time.Time) (*service.PromotionUser, error) {
	var parent any
	if parentUserID != nil {
		parent = *parentUserID
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE promotion_users
		SET parent_user_id = $2,
		    binding_source = $3,
		    bound_at = $4,
		    bound_note = $5,
		    updated_at = NOW()
		WHERE user_id = $1
	`, userID, parent, source, boundAt.UTC(), nullableTrimmedString(note)); err != nil {
		return nil, err
	}
	return r.GetPromotionUserByUserID(ctx, userID)
}

func (r *promotionRepository) HasPromotionDescendant(ctx context.Context, ancestorUserID, descendantUserID int64) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH RECURSIVE tree AS (
			SELECT user_id
			FROM promotion_users
			WHERE parent_user_id = $1
			UNION ALL
			SELECT pu.user_id
			FROM promotion_users pu
			JOIN tree ON pu.parent_user_id = tree.user_id
		)
		SELECT EXISTS(SELECT 1 FROM tree WHERE user_id = $2)
	`, ancestorUserID, descendantUserID)
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *promotionRepository) GetUserRealActualCost(ctx context.Context, userID int64) (float64, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(real_actual_cost), 0)
		FROM usage_logs
		WHERE user_id = $1
	`, userID)
	var total float64
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *promotionRepository) GetPromotionSettings(ctx context.Context) (*service.PromotionSettings, error) {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO promotion_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING`); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT activation_threshold_amount,
		       activation_bonus_amount,
		       TO_CHAR(daily_settlement_time, 'HH24:MI'),
		       settlement_enabled,
		       COALESCE(rule_activation_template, ''),
		       COALESCE(rule_direct_template, ''),
		       COALESCE(rule_indirect_template, ''),
		       COALESCE(rule_level_summary_template, ''),
		       COALESCE(invite_base_url, ''),
		       COALESCE(poster_logo_url, ''),
		       COALESCE(poster_title, ''),
		       COALESCE(poster_headline, ''),
		       COALESCE(poster_description, ''),
		       COALESCE(poster_scan_hint, ''),
		       COALESCE(poster_tags_json, '[]'),
		       created_at,
		       updated_at
		FROM promotion_settings
		WHERE id = 1
	`)
	var settings service.PromotionSettings
	var posterTagsJSON string
	if err := row.Scan(
		&settings.ActivationThresholdAmount,
		&settings.ActivationBonusAmount,
		&settings.DailySettlementTime,
		&settings.SettlementEnabled,
		&settings.RuleActivationTemplate,
		&settings.RuleDirectTemplate,
		&settings.RuleIndirectTemplate,
		&settings.RuleLevelSummaryTemplate,
		&settings.InviteBaseURL,
		&settings.PosterLogoURL,
		&settings.PosterTitle,
		&settings.PosterHeadline,
		&settings.PosterDescription,
		&settings.PosterScanHint,
		&posterTagsJSON,
		&settings.CreatedAt,
		&settings.UpdatedAt,
	); err != nil {
		return nil, err
	}
	settings.PosterTags = parsePromotionPosterTags(posterTagsJSON)
	applyDefaultPromotionSettings(&settings)
	return &settings, nil
}

func (r *promotionRepository) UpdatePromotionSettings(ctx context.Context, settings service.PromotionSettings) (*service.PromotionSettings, error) {
	if _, err := r.db.ExecContext(ctx, `INSERT INTO promotion_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING`); err != nil {
		return nil, err
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE promotion_settings
		SET activation_threshold_amount = $1,
		    activation_bonus_amount = $2,
		    daily_settlement_time = $3::time,
		    settlement_enabled = $4,
		    rule_activation_template = $5,
		    rule_direct_template = $6,
		    rule_indirect_template = $7,
		    rule_level_summary_template = $8,
		    invite_base_url = $9,
		    poster_logo_url = $10,
		    poster_title = $11,
		    poster_headline = $12,
		    poster_description = $13,
		    poster_scan_hint = $14,
		    poster_tags_json = $15,
		    updated_at = NOW()
		WHERE id = 1
	`, settings.ActivationThresholdAmount, settings.ActivationBonusAmount, settings.DailySettlementTime+":00", settings.SettlementEnabled, settings.RuleActivationTemplate, settings.RuleDirectTemplate, settings.RuleIndirectTemplate, settings.RuleLevelSummaryTemplate, nullableTrimmedString(settings.InviteBaseURL), nullableTrimmedString(settings.PosterLogoURL), nullableTrimmedString(settings.PosterTitle), nullableTrimmedString(settings.PosterHeadline), nullableTrimmedString(settings.PosterDescription), nullableTrimmedString(settings.PosterScanHint), promotionPosterTagsJSON(settings.PosterTags)); err != nil {
		return nil, err
	}
	return r.GetPromotionSettings(ctx)
}

func (r *promotionRepository) ListPromotionLevels(ctx context.Context) ([]service.PromotionLevelConfig, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, level_no, level_name, required_activated_invites, direct_rate, indirect_rate, sort_order, enabled, created_at, updated_at
		FROM promotion_level_configs
		ORDER BY sort_order ASC, level_no ASC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.PromotionLevelConfig, 0)
	for rows.Next() {
		item, err := scanPromotionLevelRow(rows)
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, rows.Err()
}

func (r *promotionRepository) UpsertPromotionLevels(ctx context.Context, levels []service.PromotionLevelConfig) ([]service.PromotionLevelConfig, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	seen := make(map[int]struct{}, len(levels))
	for idx, level := range levels {
		sortOrder := level.SortOrder
		if sortOrder == 0 {
			sortOrder = idx + 1
		}
		seen[level.LevelNo] = struct{}{}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO promotion_level_configs (
				level_no, level_name, required_activated_invites, direct_rate, indirect_rate, sort_order, enabled, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
			ON CONFLICT (level_no) DO UPDATE
			SET level_name = EXCLUDED.level_name,
			    required_activated_invites = EXCLUDED.required_activated_invites,
			    direct_rate = EXCLUDED.direct_rate,
			    indirect_rate = EXCLUDED.indirect_rate,
			    sort_order = EXCLUDED.sort_order,
			    enabled = EXCLUDED.enabled,
			    updated_at = NOW()
		`, level.LevelNo, level.LevelName, level.RequiredActivatedInvites, level.DirectRate, level.IndirectRate, sortOrder, level.Enabled); err != nil {
			return nil, err
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT level_no FROM promotion_level_configs`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var missing []int
	for rows.Next() {
		var levelNo int
		if err := rows.Scan(&levelNo); err != nil {
			return nil, err
		}
		if _, ok := seen[levelNo]; !ok {
			missing = append(missing, levelNo)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM promotion_level_configs WHERE level_no = ANY($1)`, pq.Array(missing)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.ListPromotionLevels(ctx)
}

func (r *promotionRepository) ListPromotionScripts(ctx context.Context, filter service.PromotionScriptFilter) ([]service.PromotionScript, int64, error) {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	conditions := []string{"1=1"}
	args := make([]any, 0, 4)
	if filter.Keyword != "" {
		args = append(args, "%"+filter.Keyword+"%")
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d OR content ILIKE $%d OR category ILIKE $%d)", len(args), len(args), len(args)))
	}
	if filter.Category != "" {
		args = append(args, filter.Category)
		conditions = append(conditions, fmt.Sprintf("category = $%d", len(args)))
	}
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promotion_scripts WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, category, content, use_count, enabled, created_by_user_id, created_at, updated_at
		FROM promotion_scripts
		WHERE `+where+`
		ORDER BY enabled DESC, created_at DESC
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.PromotionScript, 0)
	for rows.Next() {
		item, err := scanPromotionScript(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *promotionRepository) GetPromotionScriptByID(ctx context.Context, id int64) (*service.PromotionScript, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, category, content, use_count, enabled, created_by_user_id, created_at, updated_at
		FROM promotion_scripts
		WHERE id = $1
	`, id)
	return scanPromotionScriptRow(row)
}

func (r *promotionRepository) CreatePromotionScript(ctx context.Context, script service.PromotionScript) (*service.PromotionScript, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO promotion_scripts (name, category, content, use_count, enabled, created_by_user_id, created_at, updated_at)
		VALUES ($1, $2, $3, 0, $4, $5, NOW(), NOW())
		RETURNING id, name, category, content, use_count, enabled, created_by_user_id, created_at, updated_at
	`, script.Name, script.Category, script.Content, script.Enabled, nullableInt64(script.CreatedByUserID))
	return scanPromotionScriptRow(row)
}

func (r *promotionRepository) UpdatePromotionScript(ctx context.Context, script service.PromotionScript) (*service.PromotionScript, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE promotion_scripts
		SET name = $2,
		    category = $3,
		    content = $4,
		    enabled = $5,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, category, content, use_count, enabled, created_by_user_id, created_at, updated_at
	`, script.ID, script.Name, script.Category, script.Content, script.Enabled)
	return scanPromotionScriptRow(row)
}

func (r *promotionRepository) DeletePromotionScript(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM promotion_scripts WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return service.ErrPromotionScriptNotFound
	}
	return nil
}

func (r *promotionRepository) IncrementPromotionScriptUse(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE promotion_scripts SET use_count = use_count + 1, updated_at = NOW() WHERE id = $1`, id)
	return err
}

func generatePromotionInviteCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func isDuplicateKey(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code) == "23505"
	}
	return false
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableTrimmedString(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func applyDefaultPromotionSettings(settings *service.PromotionSettings) {
	if settings == nil {
		return
	}
	if strings.TrimSpace(settings.RuleActivationTemplate) == "" {
		settings.RuleActivationTemplate = "Activation bonus: earn ${{ACTIVATION_BONUS}} when 1 invitee activates (activation condition: spend > ${{ACTIVATION_THRESHOLD}})."
	}
	if strings.TrimSpace(settings.RuleDirectTemplate) == "" {
		settings.RuleDirectTemplate = "Direct rebate: earn a percentage of your direct invitees' spend, settled the next day at {{SETTLEMENT_TIME}}. Current level: {{CURRENT_DIRECT_RATE}}%."
	}
	if strings.TrimSpace(settings.RuleIndirectTemplate) == "" {
		settings.RuleIndirectTemplate = "Indirect rebate: earn a percentage of second-level invitees' spend, settled the next day at {{SETTLEMENT_TIME}}. Current level: {{CURRENT_INDIRECT_RATE}}%."
	}
	if strings.TrimSpace(settings.RuleLevelSummaryTemplate) == "" {
		settings.RuleLevelSummaryTemplate = "Level rebate summary: {{LEVEL_RATE_SUMMARY}}"
	}
	if strings.TrimSpace(settings.PosterTitle) == "" {
		settings.PosterTitle = "Sub2API"
	}
	if strings.TrimSpace(settings.PosterHeadline) == "" {
		settings.PosterHeadline = "Invite friends and earn spending rebates"
	}
	if strings.TrimSpace(settings.PosterDescription) == "" {
		settings.PosterDescription = "Direct rebate + indirect rebate + activation bonus, all settled to real balance."
	}
	if strings.TrimSpace(settings.PosterScanHint) == "" {
		settings.PosterScanHint = "扫码快速注册"
	}
	settings.PosterTags = normalizePromotionPosterTags(settings.PosterTags)
}

func parsePromotionPosterTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return normalizePromotionPosterTags(nil)
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return normalizePromotionPosterTags(strings.Split(raw, ","))
	}
	return normalizePromotionPosterTags(tags)
}

func promotionPosterTagsJSON(tags []string) string {
	data, err := json.Marshal(normalizePromotionPosterTags(tags))
	if err != nil {
		return `["Real spending rebate","Next-day settlement","Unique invite code"]`
	}
	return string(data)
}

func normalizePromotionPosterTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) >= 6 {
			break
		}
	}
	if len(out) == 0 {
		return []string{"Real spending rebate", "Next-day settlement", "Unique invite code"}
	}
	return out
}
