package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrPromotionInviteCodeNotFound    = infraerrors.NotFound("PROMOTION_INVITE_CODE_NOT_FOUND", "promotion invite code not found")
	ErrPromotionAlreadyBound          = infraerrors.Conflict("PROMOTION_ALREADY_BOUND", "promotion referrer already bound")
	ErrPromotionBindSelf              = infraerrors.BadRequest("PROMOTION_BIND_SELF", "cannot bind yourself as referrer")
	ErrPromotionBindCycle             = infraerrors.BadRequest("PROMOTION_BIND_CYCLE", "promotion binding would create a cycle")
	ErrPromotionBindAfterConsumption  = infraerrors.BadRequest("PROMOTION_BIND_AFTER_CONSUMPTION", "promotion referrer cannot be bound after consumption occurs")
	ErrPromotionScriptNotFound        = infraerrors.NotFound("PROMOTION_SCRIPT_NOT_FOUND", "promotion script not found")
	ErrPromotionCommissionNotFound    = infraerrors.NotFound("PROMOTION_COMMISSION_NOT_FOUND", "promotion commission record not found")
	ErrPromotionNoLevelsConfigured    = infraerrors.BadRequest("PROMOTION_LEVELS_EMPTY", "promotion levels are not configured")
	ErrPromotionInvalidSettlementTime = infraerrors.BadRequest("PROMOTION_INVALID_SETTLEMENT_TIME", "daily settlement time must use HH:MM format")
	ErrPromotionInvalidCommissionType = infraerrors.BadRequest("PROMOTION_INVALID_COMMISSION_TYPE", "invalid promotion commission type")
	ErrPromotionInvalidScriptCategory = infraerrors.BadRequest("PROMOTION_INVALID_SCRIPT_CATEGORY", "invalid promotion script category")
	ErrPromotionInvalidBindRequest    = infraerrors.BadRequest("PROMOTION_INVALID_BIND_REQUEST", "promotion binding request is invalid")
	ErrPromotionCommissionNotPending  = infraerrors.Conflict("PROMOTION_COMMISSION_NOT_PENDING", "promotion commission record is not pending")
	ErrPromotionCommissionAlreadyDone = infraerrors.Conflict("PROMOTION_COMMISSION_ALREADY_PROCESSED", "promotion commission record is already processed")
	ErrPromotionDownlineNotDirect     = infraerrors.BadRequest("PROMOTION_DOWNLINE_NOT_DIRECT", "target user is not a direct downline")
)

func defaultPromotionRuleTemplates() PromotionRenderedRuleTemplates {
	return PromotionRenderedRuleTemplates{
		Activation:   "激活奖励（每邀请 1 人激活，你可获得 ${{ACTIVATION_BONUS}} 激活奖励（激活条件：被邀请人消耗 > {{ACTIVATION_THRESHOLD}}$））",
		Direct:       "一级返利（你邀请的人每次消费，你可获得其消费金额对应百分比的返利（次日 {{SETTLEMENT_TIME}} 结算，随等级提升而提升，当前等级：{{CURRENT_DIRECT_RATE}}%））",
		Indirect:     "二级返利（你邀请的人邀请的二级代理，你可以获得二级代理消费的相应金额的百分比返利（次日 {{SETTLEMENT_TIME}} 结算，随等级提升而提升，当前等级：{{CURRENT_INDIRECT_RATE}}%））",
		LevelSummary: "等级提成比例：{{LEVEL_RATE_SUMMARY}}",
	}
}

type PromotionService struct {
	repo           PromotionRepository
	userRepo       UserRepository
	cfg            *config.Config
	settingService *SettingService
	billingCache   *BillingCacheService
}

func NewPromotionService(
	repo PromotionRepository,
	userRepo UserRepository,
	cfg *config.Config,
	settingService *SettingService,
	billingCache *BillingCacheService,
) *PromotionService {
	return &PromotionService{
		repo:           repo,
		userRepo:       userRepo,
		cfg:            cfg,
		settingService: settingService,
		billingCache:   billingCache,
	}
}

func (s *PromotionService) PreviewReferrer(ctx context.Context, inviteCode string) (*PromotionReferrerPreview, error) {
	inviteCode = strings.ToUpper(strings.TrimSpace(inviteCode))
	if inviteCode == "" {
		return nil, ErrPromotionInviteCodeNotFound
	}
	referrer, err := s.repo.GetPromotionUserByInviteCode(ctx, inviteCode)
	if err != nil || referrer == nil {
		return nil, ErrPromotionInviteCodeNotFound
	}
	level, err := s.repo.GetCurrentPromotionLevel(ctx, referrer.UserID)
	if err != nil {
		return nil, err
	}
	return &PromotionReferrerPreview{
		UserID:      referrer.UserID,
		InviteCode:  referrer.InviteCode,
		MaskedEmail: MaskEmail(s.mustLookupEmail(ctx, referrer.UserID)),
		LevelName:   levelNameOrFallback(level),
	}, nil
}

func (s *PromotionService) BindReferrer(ctx context.Context, userID int64, inviteCode string) (*PromotionUser, error) {
	if userID <= 0 {
		return nil, ErrPromotionInvalidBindRequest
	}
	inviteCode = strings.ToUpper(strings.TrimSpace(inviteCode))
	if inviteCode == "" {
		return nil, ErrPromotionInviteCodeNotFound
	}

	current, err := s.repo.EnsurePromotionUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	referrer, err := s.repo.GetPromotionUserByInviteCode(ctx, inviteCode)
	if err != nil || referrer == nil {
		return nil, ErrPromotionInviteCodeNotFound
	}
	if referrer.UserID == userID {
		return nil, ErrPromotionBindSelf
	}
	if current.ParentUserID != nil {
		if *current.ParentUserID == referrer.UserID {
			return current, nil
		}
		return nil, ErrPromotionAlreadyBound
	}
	totalCost, err := s.repo.GetUserRealActualCost(ctx, userID)
	if err != nil {
		return nil, err
	}
	if totalCost > 0 {
		return nil, ErrPromotionBindAfterConsumption
	}
	hasCycle, err := s.repo.HasPromotionDescendant(ctx, userID, referrer.UserID)
	if err != nil {
		return nil, err
	}
	if hasCycle {
		return nil, ErrPromotionBindCycle
	}
	return s.repo.SetPromotionParent(ctx, userID, &referrer.UserID, PromotionBindingSourceSelf, "", time.Now())
}

func (s *PromotionService) GetMyOverview(ctx context.Context, userID int64) (*PromotionOverview, error) {
	user, err := s.repo.EnsurePromotionUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	nowLocal := s.nowInAppLocation()
	businessDate := beginningOfLocalDay(nowLocal)

	overview, err := s.repo.GetPromotionOverviewSummary(ctx, userID, businessDate)
	if err != nil {
		return nil, err
	}
	if overview == nil {
		overview = &PromotionOverview{}
	}
	currentLevel, err := s.repo.GetCurrentPromotionLevel(ctx, userID)
	if err != nil {
		return nil, err
	}
	nextLevel, err := s.repo.GetNextPromotionLevel(ctx, userID)
	if err != nil {
		return nil, err
	}
	settings, err := s.repo.GetPromotionSettings(ctx)
	if err != nil {
		return nil, err
	}
	levels, err := s.repo.ListPromotionLevels(ctx)
	if err != nil {
		return nil, err
	}
	leaderboard, err := s.repo.ListPromotionLeaderboard(ctx, 10)
	if err != nil {
		return nil, err
	}

	overview.UserID = userID
	overview.InviteCode = user.InviteCode
	overview.CurrentLevelNo = 0
	overview.CurrentLevelName = "未设置"
	if currentLevel != nil {
		overview.CurrentLevelNo = currentLevel.LevelNo
		overview.CurrentLevelName = currentLevel.LevelName
		overview.CurrentDirectRate = currentLevel.DirectRate
		overview.CurrentIndirectRate = currentLevel.IndirectRate
		overview.CurrentTotalRate = currentLevel.DirectRate + currentLevel.IndirectRate
	}
	if nextLevel != nil {
		overview.NextLevelName = nextLevel.LevelName
		overview.NextLevelNo = &nextLevel.LevelNo
		need := nextLevel.RequiredActivatedInvites
		overview.NextLevelRequiredActivate = &need
	}
	if settings != nil {
		overview.ActivationThresholdAmount = settings.ActivationThresholdAmount
		overview.ActivationBonusAmount = settings.ActivationBonusAmount
		overview.RuleTemplates = renderPromotionRuleTemplates(*settings, overview, levels)
	} else {
		overview.RuleTemplates = renderPromotionRuleTemplates(PromotionSettings{}, overview, levels)
	}
	siteName := s.resolveSiteName(ctx)
	overview.InviteLink = s.buildInviteLinkWithBase(s.resolveInviteBaseURL(ctx, settings), user.InviteCode)
	overview.PosterConfig = buildPromotionPosterConfig(settings, siteName, overview.InviteLink, user.InviteCode)
	overview.LevelRateSummaries = buildPromotionLevelRateSummaries(levels)
	overview.Leaderboard = leaderboard
	return overview, nil
}

func (s *PromotionService) ListMyTeam(ctx context.Context, userID int64, filter PromotionTeamFilter) ([]PromotionTeamItem, int64, error) {
	if _, err := s.repo.EnsurePromotionUser(ctx, userID); err != nil {
		return nil, 0, err
	}
	filter = normalizeTeamFilter(filter)
	start, end := s.currentLocalDayRange()
	items, total, err := s.repo.ListPromotionTeam(ctx, userID, filter, start, end)
	if err != nil {
		return nil, 0, err
	}
	if err := s.applyCurrentLevelsToTeamItems(ctx, items, true); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PromotionService) ListMyEarnings(ctx context.Context, userID int64, filter PromotionCommissionFilter) ([]PromotionCommissionListItem, int64, error) {
	filter = normalizeCommissionFilter(filter)
	items, total, err := s.repo.ListPromotionEarnings(ctx, userID, filter)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].BeneficiaryMasked = MaskEmail(items[i].BeneficiaryEmail)
		items[i].SourceUserMasked = MaskEmail(items[i].SourceUserEmail)
	}
	return items, total, nil
}

func (s *PromotionService) ListMyScripts(ctx context.Context, userID int64) ([]PromotionScript, error) {
	if _, err := s.repo.EnsurePromotionUser(ctx, userID); err != nil {
		return nil, err
	}
	scripts, _, err := s.repo.ListPromotionScripts(ctx, PromotionScriptFilter{
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		return nil, err
	}
	filtered := make([]PromotionScript, 0, len(scripts))
	for _, script := range scripts {
		if script.Enabled {
			filtered = append(filtered, script)
		}
	}
	scripts = filtered
	user, err := s.repo.GetPromotionUserByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	overview, _ := s.repo.GetPromotionOverviewSummary(ctx, userID, beginningOfLocalDay(s.nowInAppLocation()))
	totalEarnings := 0.0
	if overview != nil {
		totalEarnings = overview.TotalRewardAmount
	}
	siteName := s.resolveSiteName(ctx)
	inviteLink := s.buildInviteLink(ctx, user.InviteCode)
	email := s.mustLookupEmail(ctx, userID)
	for i := range scripts {
		scripts[i].RenderedPreview = renderPromotionScriptPreview(scripts[i].Content, map[string]string{
			"INVITE_CODE":    user.InviteCode,
			"REF_LINK":       inviteLink,
			"USER_NAME":      displayNameFromEmail(email),
			"SITE_NAME":      siteName,
			"LEVEL":          overviewLevelName(overview),
			"TOTAL_EARNINGS": fmt.Sprintf("%.2f", totalEarnings),
		})
	}
	return scripts, nil
}

func (s *PromotionService) TrackScriptUse(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrPromotionScriptNotFound
	}
	return s.repo.IncrementPromotionScriptUse(ctx, id)
}

func (s *PromotionService) GetAdminDashboard(ctx context.Context) (*PromotionAdminDashboard, error) {
	return s.repo.GetPromotionAdminDashboard(ctx, beginningOfLocalDay(s.nowInAppLocation()))
}

func (s *PromotionService) ListAdminRelations(ctx context.Context, keyword string, page, pageSize int) ([]PromotionRelationRow, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	userIDs, total, err := s.repo.SearchPromotionRelationUserIDs(ctx, strings.TrimSpace(keyword), page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	for _, userID := range userIDs {
		if _, ensureErr := s.repo.EnsurePromotionUser(ctx, userID); ensureErr != nil {
			return nil, 0, ensureErr
		}
	}
	rows, err := s.repo.ListPromotionRelationsByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, 0, err
	}
	if err := s.applyCurrentLevelsToRelationRows(ctx, rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *PromotionService) GetAdminRelationChain(ctx context.Context, userID int64) (*PromotionRelationChain, error) {
	if _, err := s.repo.EnsurePromotionUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.repo.GetPromotionRelationChain(ctx, userID)
}

func (s *PromotionService) ListAdminDownlines(ctx context.Context, userID int64, filter PromotionTeamFilter) ([]PromotionTeamItem, int64, error) {
	if _, err := s.repo.EnsurePromotionUser(ctx, userID); err != nil {
		return nil, 0, err
	}
	filter = normalizeTeamFilter(filter)
	start, end := s.currentLocalDayRange()
	items, total, err := s.repo.ListPromotionDownlines(ctx, userID, filter, start, end)
	if err != nil {
		return nil, 0, err
	}
	if err := s.applyCurrentLevelsToTeamItems(ctx, items, false); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *PromotionService) BindAdminParent(ctx context.Context, userID, parentUserID int64, note string) (*PromotionUser, error) {
	if userID <= 0 || parentUserID <= 0 {
		return nil, ErrPromotionInvalidBindRequest
	}
	if userID == parentUserID {
		return nil, ErrPromotionBindSelf
	}
	if _, err := s.repo.EnsurePromotionUser(ctx, userID); err != nil {
		return nil, err
	}
	if _, err := s.repo.EnsurePromotionUser(ctx, parentUserID); err != nil {
		return nil, err
	}
	hasCycle, err := s.repo.HasPromotionDescendant(ctx, userID, parentUserID)
	if err != nil {
		return nil, err
	}
	if hasCycle {
		return nil, ErrPromotionBindCycle
	}
	return s.repo.SetPromotionParent(ctx, userID, &parentUserID, PromotionBindingSourceAdmin, strings.TrimSpace(note), time.Now())
}

func (s *PromotionService) RemoveAdminParent(ctx context.Context, userID int64, note string) (*PromotionUser, error) {
	if userID <= 0 {
		return nil, ErrPromotionInvalidBindRequest
	}
	if _, err := s.repo.EnsurePromotionUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.repo.SetPromotionParent(ctx, userID, nil, PromotionBindingSourceAdmin, strings.TrimSpace(note), time.Now())
}

func (s *PromotionService) RemoveAdminDirectDownline(ctx context.Context, parentUserID, downlineUserID int64, note string) error {
	if parentUserID <= 0 || downlineUserID <= 0 || parentUserID == downlineUserID {
		return ErrPromotionInvalidBindRequest
	}
	if err := s.repo.RemovePromotionDirectDownline(ctx, parentUserID, downlineUserID, note); err != nil {
		return err
	}
	return nil
}

func (s *PromotionService) ListAdminCommissions(ctx context.Context, filter PromotionCommissionAdminFilter) ([]PromotionCommissionListItem, int64, error) {
	filter = normalizeAdminCommissionFilter(filter)
	return s.repo.ListPromotionCommissions(ctx, filter)
}

func (s *PromotionService) ManualGrantCommission(ctx context.Context, operatorUserID *int64, record PromotionCommissionRecord) (*PromotionCommissionRecord, error) {
	record.CommissionType = PromotionCommissionTypeAdjustment
	if record.Amount == 0 {
		return nil, infraerrors.BadRequest("PROMOTION_AMOUNT_REQUIRED", "promotion commission amount must not be zero")
	}
	record.Note = strings.TrimSpace(record.Note)
	record.CreatedByUserID = operatorUserID
	record.BusinessDate = beginningOfLocalDay(s.nowInAppLocation())
	created, err := s.repo.CreateManualPromotionCommission(ctx, record, true)
	if err != nil {
		return nil, err
	}
	s.invalidateUserBalanceCache(created.BeneficiaryUserID)
	return created, nil
}

func (s *PromotionService) UpdateCommission(ctx context.Context, commissionID int64, operatorUserID *int64, amount float64, note string) (*PromotionCommissionRecord, error) {
	if commissionID <= 0 {
		return nil, ErrPromotionCommissionNotFound
	}
	if amount == 0 {
		return nil, infraerrors.BadRequest("PROMOTION_AMOUNT_REQUIRED", "promotion commission amount must not be zero")
	}
	updated, err := s.repo.UpdatePromotionCommission(ctx, commissionID, operatorUserID, amount, strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	s.invalidateUserBalanceCache(updated.BeneficiaryUserID)
	return updated, nil
}

func (s *PromotionService) SettleCommission(ctx context.Context, commissionID int64, operatorUserID *int64, note string) (*PromotionCommissionRecord, error) {
	record, err := s.repo.SettlePromotionCommission(ctx, commissionID, operatorUserID, strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	s.invalidateUserBalanceCache(record.BeneficiaryUserID)
	return record, nil
}

func (s *PromotionService) BatchSettleCommissions(ctx context.Context, ids []int64, operatorUserID *int64, note string) (PromotionSettleSummary, error) {
	summary := PromotionSettleSummary{}
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		record, err := s.SettleCommission(ctx, id, operatorUserID, note)
		if err != nil {
			return summary, err
		}
		summary.SettledCount++
		summary.TotalAmount += record.Amount
	}
	return summary, nil
}

func (s *PromotionService) CancelCommission(ctx context.Context, commissionID int64, operatorUserID *int64, note string) (*PromotionCommissionRecord, error) {
	record, err := s.repo.CancelPromotionCommission(ctx, commissionID, operatorUserID, strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	if record.CancelledAt != nil && record.SettledAt != nil {
		s.invalidateUserBalanceCache(record.BeneficiaryUserID)
	}
	return record, nil
}

func (s *PromotionService) GetAdminConfig(ctx context.Context) (*PromotionConfigPayload, string, error) {
	settings, err := s.repo.GetPromotionSettings(ctx)
	if err != nil {
		return nil, "", err
	}
	levels, err := s.repo.ListPromotionLevels(ctx)
	if err != nil {
		return nil, "", err
	}
	payload := &PromotionConfigPayload{}
	if settings != nil {
		payload.Settings = *settings
	}
	payload.Levels = levels
	return payload, s.appTimezoneName(), nil
}

func (s *PromotionService) UpdateAdminConfig(ctx context.Context, payload PromotionConfigPayload) (*PromotionConfigPayload, string, error) {
	if _, _, err := parseSettlementClock(payload.Settings.DailySettlementTime); err != nil {
		return nil, "", ErrPromotionInvalidSettlementTime
	}
	payload.Settings.InviteBaseURL = strings.TrimSpace(payload.Settings.InviteBaseURL)
	if payload.Settings.InviteBaseURL != "" {
		if err := config.ValidateAbsoluteHTTPURL(payload.Settings.InviteBaseURL); err != nil {
			return nil, "", infraerrors.BadRequest("PROMOTION_INVITE_BASE_URL_INVALID", "promotion invite base url must be an absolute http/https URL")
		}
	}
	payload.Settings.PosterLogoURL = strings.TrimSpace(payload.Settings.PosterLogoURL)
	if payload.Settings.PosterLogoURL != "" {
		if !isValidPromotionPosterLogoValue(payload.Settings.PosterLogoURL) {
			return nil, "", infraerrors.BadRequest("PROMOTION_POSTER_LOGO_URL_INVALID", "promotion poster logo url must be an absolute http/https URL or a valid uploaded image payload")
		}
	}
	payload.Settings.PosterTitle = strings.TrimSpace(payload.Settings.PosterTitle)
	payload.Settings.PosterHeadline = strings.TrimSpace(payload.Settings.PosterHeadline)
	payload.Settings.PosterDescription = strings.TrimSpace(payload.Settings.PosterDescription)
	payload.Settings.PosterScanHint = strings.TrimSpace(payload.Settings.PosterScanHint)
	payload.Settings.PosterTags = normalizePromotionPosterTagsForSettings(payload.Settings.PosterTags)
	for i := range payload.Levels {
		payload.Levels[i].LevelName = strings.TrimSpace(payload.Levels[i].LevelName)
		if payload.Levels[i].LevelName == "" {
			payload.Levels[i].LevelName = fmt.Sprintf("Lv%d", payload.Levels[i].LevelNo)
		}
		if payload.Levels[i].RequiredActivatedInvites < 0 {
			payload.Levels[i].RequiredActivatedInvites = 0
		}
		if payload.Levels[i].DirectRate < 0 {
			payload.Levels[i].DirectRate = 0
		}
		if payload.Levels[i].IndirectRate < 0 {
			payload.Levels[i].IndirectRate = 0
		}
	}
	updatedSettings, err := s.repo.UpdatePromotionSettings(ctx, payload.Settings)
	if err != nil {
		return nil, "", err
	}
	updatedLevels, err := s.repo.UpsertPromotionLevels(ctx, payload.Levels)
	if err != nil {
		return nil, "", err
	}
	return &PromotionConfigPayload{
		Settings: *updatedSettings,
		Levels:   updatedLevels,
	}, s.appTimezoneName(), nil
}

func (s *PromotionService) ListAdminScripts(ctx context.Context, filter PromotionScriptFilter) ([]PromotionScript, int64, error) {
	filter = normalizeScriptFilter(filter)
	return s.repo.ListPromotionScripts(ctx, filter)
}

func (s *PromotionService) CreateAdminScript(ctx context.Context, operatorUserID *int64, script PromotionScript) (*PromotionScript, error) {
	if err := validatePromotionScript(&script); err != nil {
		return nil, err
	}
	script.CreatedByUserID = operatorUserID
	return s.repo.CreatePromotionScript(ctx, script)
}

func (s *PromotionService) UpdateAdminScript(ctx context.Context, script PromotionScript) (*PromotionScript, error) {
	if script.ID <= 0 {
		return nil, ErrPromotionScriptNotFound
	}
	if err := validatePromotionScript(&script); err != nil {
		return nil, err
	}
	return s.repo.UpdatePromotionScript(ctx, script)
}

func (s *PromotionService) DeleteAdminScript(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrPromotionScriptNotFound
	}
	return s.repo.DeletePromotionScript(ctx, id)
}

func (s *PromotionService) ProcessSettlementTick(ctx context.Context, now time.Time) error {
	settings, err := s.repo.GetPromotionSettings(ctx)
	if err != nil {
		return err
	}
	if settings == nil {
		return nil
	}
	if err := s.processActivations(ctx, now, settings); err != nil {
		return err
	}
	if err := s.processDailyCommissions(ctx, now); err != nil {
		return err
	}
	if !settings.SettlementEnabled {
		return nil
	}
	return s.processDueSettlements(ctx, now, settings)
}

func (s *PromotionService) processActivations(ctx context.Context, now time.Time, settings *PromotionSettings) error {
	candidates, err := s.repo.ListPendingActivationCandidates(ctx, settings.ActivationThresholdAmount, now)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		activatedAt := candidate.ActivatedAt.In(s.appLocation())
		activation := PromotionActivation{
			UserID:             candidate.UserID,
			PromoterUserID:     candidate.PromoterUserID,
			ActivatedAt:        activatedAt,
			ThresholdAmount:    settings.ActivationThresholdAmount,
			TriggerUsageAmount: candidate.TriggerUsageAmount,
		}
		if err := s.repo.CreatePromotionActivation(ctx, activation, settings.ActivationBonusAmount); err != nil {
			return err
		}
	}
	return nil
}

func (s *PromotionService) processDailyCommissions(ctx context.Context, now time.Time) error {
	loc := s.appLocation()
	nowLocal := now.In(loc)
	today := beginningOfLocalDay(nowLocal)
	yesterday := today.AddDate(0, 0, -1)
	dates := []time.Time{today, yesterday}
	for _, date := range dates {
		levels, err := s.repo.ListPromotionLevels(ctx)
		if err != nil {
			return err
		}
		if len(levels) == 0 {
			return nil
		}
		start, end := localDateRange(date, loc)
		if err := s.repo.UpsertDailyPromotionCommissions(ctx, date, start, end); err != nil {
			return err
		}
	}
	return nil
}

func (s *PromotionService) processDueSettlements(ctx context.Context, now time.Time, settings *PromotionSettings) error {
	loc := s.appLocation()
	nowLocal := now.In(loc)
	cutoffDate := settlementBoundaryDate(nowLocal, settings.DailySettlementTime, loc)
	businessDates, err := s.repo.ListSettlablePromotionBusinessDates(ctx, cutoffDate)
	if err != nil {
		return err
	}
	for _, businessDate := range businessDates {
		if err := s.repo.SettlePromotionBusinessDate(ctx, businessDate, nil, "auto settlement"); err != nil {
			return err
		}
	}
	return nil
}

func (s *PromotionService) nowInAppLocation() time.Time {
	return time.Now().In(s.appLocation())
}

func (s *PromotionService) appTimezoneName() string {
	if s.cfg != nil && strings.TrimSpace(s.cfg.Timezone) != "" {
		return strings.TrimSpace(s.cfg.Timezone)
	}
	return "Asia/Shanghai"
}

func (s *PromotionService) appLocation() *time.Location {
	name := s.appTimezoneName()
	loc, err := time.LoadLocation(name)
	if err != nil || loc == nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return loc
}

func (s *PromotionService) buildInviteLink(ctx context.Context, inviteCode string) string {
	var settings *PromotionSettings
	if s.repo != nil {
		if loaded, err := s.repo.GetPromotionSettings(ctx); err == nil {
			settings = loaded
		}
	}
	return s.buildInviteLinkWithBase(s.resolveInviteBaseURL(ctx, settings), inviteCode)
}

func (s *PromotionService) buildInviteLinkWithBase(base, inviteCode string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "/register?ref=" + inviteCode
	}
	return base + "/register?ref=" + inviteCode
}

func (s *PromotionService) resolveInviteBaseURL(ctx context.Context, settings *PromotionSettings) string {
	if settings != nil && strings.TrimSpace(settings.InviteBaseURL) != "" {
		return strings.TrimSpace(settings.InviteBaseURL)
	}
	if s.settingService != nil {
		if base := strings.TrimSpace(s.settingService.GetFrontendURL(ctx)); base != "" {
			return base
		}
	}
	if s.cfg != nil {
		return strings.TrimSpace(s.cfg.Server.FrontendURL)
	}
	return ""
}

func (s *PromotionService) resolveSiteName(ctx context.Context) string {
	if s.settingService != nil {
		if settings, err := s.settingService.GetPublicSettings(ctx); err == nil && settings != nil && strings.TrimSpace(settings.SiteName) != "" {
			return settings.SiteName
		}
	}
	return "Sub2API"
}

func (s *PromotionService) invalidateUserBalanceCache(userID int64) {
	if s.billingCache == nil || userID <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.billingCache.InvalidateUserBalance(ctx, userID)
	}()
}

func (s *PromotionService) mustLookupEmail(ctx context.Context, userID int64) string {
	if userID <= 0 {
		return ""
	}
	if s.userRepo != nil {
		user, err := s.userRepo.GetByID(ctx, userID)
		if err == nil && user != nil {
			return user.Email
		}
	}
	return fmt.Sprintf("user-%d@example.com", userID)
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

func normalizeTeamFilter(filter PromotionTeamFilter) PromotionTeamFilter {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	if filter.PageSize == 20 {
		filter.PageSize = 10
	}
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	switch filter.Status {
	case "", "all", "active", "inactive":
	default:
		filter.Status = ""
	}
	filter.SortBy = strings.TrimSpace(strings.ToLower(filter.SortBy))
	switch filter.SortBy {
	case "", "today_contribution", "total_contribution", "joined_at", "activated_at":
	default:
		filter.SortBy = ""
	}
	if filter.SortBy == "" {
		filter.SortBy = "today_contribution"
	}
	filter.SortOrder = strings.TrimSpace(strings.ToLower(filter.SortOrder))
	switch filter.SortOrder {
	case "", "asc", "desc":
	default:
		filter.SortOrder = ""
	}
	if filter.SortOrder == "" {
		filter.SortOrder = "desc"
	}
	return filter
}

func normalizeCommissionFilter(filter PromotionCommissionFilter) PromotionCommissionFilter {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	filter.Type = strings.TrimSpace(strings.ToLower(filter.Type))
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	return filter
}

func normalizeAdminCommissionFilter(filter PromotionCommissionAdminFilter) PromotionCommissionAdminFilter {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	filter.Type = strings.TrimSpace(strings.ToLower(filter.Type))
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	return filter
}

func normalizeScriptFilter(filter PromotionScriptFilter) PromotionScriptFilter {
	filter.Page, filter.PageSize = normalizePage(filter.Page, filter.PageSize)
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	filter.Category = strings.TrimSpace(filter.Category)
	return filter
}

func normalizePromotionPosterTagsForSettings(tags []string) []string {
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
	return out
}

func isValidPromotionPosterLogoValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "data:image/") {
		switch {
		case strings.HasPrefix(value, "data:image/png;base64,"),
			strings.HasPrefix(value, "data:image/jpeg;base64,"),
			strings.HasPrefix(value, "data:image/jpg;base64,"),
			strings.HasPrefix(value, "data:image/webp;base64,"),
			strings.HasPrefix(value, "data:image/gif;base64,"):
			return true
		default:
			return false
		}
	}
	return config.ValidateAbsoluteHTTPURL(value) == nil
}

func validatePromotionScript(script *PromotionScript) error {
	if script == nil {
		return ErrPromotionScriptNotFound
	}
	script.Name = strings.TrimSpace(script.Name)
	script.Category = strings.TrimSpace(script.Category)
	script.Content = strings.TrimSpace(script.Content)
	if script.Name == "" || script.Content == "" {
		return infraerrors.BadRequest("PROMOTION_SCRIPT_INVALID", "promotion script name and content are required")
	}
	if script.Category == "" {
		script.Category = PromotionScriptCategoryDefault
	}
	if utf8.RuneCountInString(script.Category) > 32 {
		return infraerrors.BadRequest("PROMOTION_SCRIPT_INVALID_CATEGORY_LENGTH", "promotion script tag must be 32 characters or fewer")
	}
	return nil
}

func validManualCommissionType(commissionType string) (string, bool) {
	switch commissionType {
	case PromotionCommissionTypeManual, PromotionCommissionTypeAdjustment, PromotionCommissionTypePromotion:
		return commissionType, true
	default:
		return "", false
	}
}

func renderPromotionScriptPreview(content string, variables map[string]string) string {
	rendered := content
	for key, value := range variables {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}

func renderPromotionRuleTemplates(settings PromotionSettings, overview *PromotionOverview, levels []PromotionLevelConfig) PromotionRenderedRuleTemplates {
	defaults := defaultPromotionRuleTemplates()
	if strings.TrimSpace(settings.RuleActivationTemplate) == "" {
		settings.RuleActivationTemplate = defaults.Activation
	}
	if strings.TrimSpace(settings.RuleDirectTemplate) == "" {
		settings.RuleDirectTemplate = defaults.Direct
	}
	if strings.TrimSpace(settings.RuleIndirectTemplate) == "" {
		settings.RuleIndirectTemplate = defaults.Indirect
	}
	if strings.TrimSpace(settings.RuleLevelSummaryTemplate) == "" {
		settings.RuleLevelSummaryTemplate = defaults.LevelSummary
	}
	levelSummaryText := buildPromotionLevelSummaryText(levels)
	values := map[string]string{
		"ACTIVATION_THRESHOLD":  formatPromotionRateNumber(overview.ActivationThresholdAmount),
		"ACTIVATION_BONUS":      formatPromotionRateNumber(overview.ActivationBonusAmount),
		"CURRENT_DIRECT_RATE":   formatPromotionRateNumber(overview.CurrentDirectRate),
		"CURRENT_INDIRECT_RATE": formatPromotionRateNumber(overview.CurrentIndirectRate),
		"CURRENT_TOTAL_RATE":    formatPromotionRateNumber(overview.CurrentTotalRate),
		"SETTLEMENT_TIME":       fallbackSettlementTime(settings.DailySettlementTime),
		"LEVEL_RATE_SUMMARY":    levelSummaryText,
	}
	return PromotionRenderedRuleTemplates{
		Activation:   replacePromotionTemplateVars(settings.RuleActivationTemplate, values),
		Direct:       replacePromotionTemplateVars(settings.RuleDirectTemplate, values),
		Indirect:     replacePromotionTemplateVars(settings.RuleIndirectTemplate, values),
		LevelSummary: replacePromotionTemplateVars(settings.RuleLevelSummaryTemplate, values),
	}
}

func buildPromotionPosterConfig(settings *PromotionSettings, siteName, inviteLink, inviteCode string) PromotionPosterConfig {
	config := PromotionPosterConfig{
		InviteBaseURL:     "",
		LogoURL:           "",
		Title:             siteName,
		Headline:          "Invite friends and earn spending rebates",
		Description:       "Direct rebate + indirect rebate + activation bonus, all settled to real balance.",
		ScanHint:          "扫码快速注册",
		Tags:              []string{"Real spending rebate", "Next-day settlement", "Unique invite code"},
		PrimaryInviteCode: inviteCode,
	}
	if settings == nil {
		return config
	}
	config.InviteBaseURL = strings.TrimSpace(settings.InviteBaseURL)
	if strings.TrimSpace(settings.PosterLogoURL) != "" {
		config.LogoURL = strings.TrimSpace(settings.PosterLogoURL)
	}
	if strings.TrimSpace(settings.PosterTitle) != "" {
		config.Title = strings.TrimSpace(settings.PosterTitle)
	}
	if strings.TrimSpace(settings.PosterHeadline) != "" {
		config.Headline = strings.TrimSpace(settings.PosterHeadline)
	}
	if strings.TrimSpace(settings.PosterDescription) != "" {
		config.Description = strings.TrimSpace(settings.PosterDescription)
	}
	if strings.TrimSpace(settings.PosterScanHint) != "" {
		config.ScanHint = strings.TrimSpace(settings.PosterScanHint)
	}
	if len(settings.PosterTags) > 0 {
		config.Tags = settings.PosterTags
	}
	if strings.TrimSpace(inviteLink) != "" && config.InviteBaseURL == "" {
		config.InviteBaseURL = strings.TrimRight(strings.TrimSuffix(inviteLink, "/register?ref="+inviteCode), "/")
	}
	return config
}

func buildPromotionLevelRateSummaries(levels []PromotionLevelConfig) []PromotionLevelRateSummary {
	out := make([]PromotionLevelRateSummary, 0, len(levels))
	for _, level := range levels {
		if !level.Enabled {
			continue
		}
		out = append(out, PromotionLevelRateSummary{
			LevelNo:                  level.LevelNo,
			LevelName:                level.LevelName,
			RequiredActivatedInvites: level.RequiredActivatedInvites,
			DirectRate:               level.DirectRate,
			IndirectRate:             level.IndirectRate,
			TotalRate:                level.DirectRate + level.IndirectRate,
		})
	}
	return out
}

func buildPromotionLevelSummaryText(levels []PromotionLevelConfig) string {
	summaries := buildPromotionLevelRateSummaries(levels)
	if len(summaries) == 0 {
		return "暂未配置"
	}
	parts := make([]string, 0, len(summaries))
	for _, item := range summaries {
		parts = append(parts, fmt.Sprintf("Lv%d %s：%s%% + %s%% = %s%%", item.LevelNo, item.LevelName, formatPromotionRateNumber(item.DirectRate), formatPromotionRateNumber(item.IndirectRate), formatPromotionRateNumber(item.TotalRate)))
	}
	return strings.Join(parts, "；")
}

func replacePromotionTemplateVars(template string, values map[string]string) string {
	rendered := template
	for key, value := range values {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}

func formatPromotionRateNumber(value float64) string {
	formatted := fmt.Sprintf("%.4f", value)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	if formatted == "" {
		return "0"
	}
	return formatted
}

func fallbackSettlementTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "00:00"
	}
	return value
}

func displayNameFromEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "推广用户"
	}
	if idx := strings.IndexByte(email, '@'); idx > 0 {
		return email[:idx]
	}
	return email
}

func overviewLevelName(overview *PromotionOverview) string {
	if overview == nil || overview.CurrentLevelName == "" {
		return "未设置"
	}
	return overview.CurrentLevelName
}

func levelNameOrFallback(level *PromotionLevelConfig) string {
	if level == nil || strings.TrimSpace(level.LevelName) == "" {
		return "未设置"
	}
	return level.LevelName
}

func (s *PromotionService) applyCurrentLevelsToTeamItems(ctx context.Context, items []PromotionTeamItem, maskEmail bool) error {
	if len(items) == 0 {
		return nil
	}
	userIDs := make([]int64, 0, len(items))
	for _, item := range items {
		userIDs = append(userIDs, item.UserID)
	}
	levelMap, err := s.repo.GetCurrentPromotionLevels(ctx, userIDs)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].LevelName = levelNameOrFallback(levelMap[items[i].UserID])
		if maskEmail {
			items[i].MaskedEmail = MaskEmail(items[i].Email)
		}
	}
	return nil
}

func (s *PromotionService) applyCurrentLevelsToRelationRows(ctx context.Context, rows []PromotionRelationRow) error {
	if len(rows) == 0 {
		return nil
	}
	userIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.UserID)
	}
	levelMap, err := s.repo.GetCurrentPromotionLevels(ctx, userIDs)
	if err != nil {
		return err
	}
	for i := range rows {
		if strings.TrimSpace(rows[i].LevelName) == "" {
			rows[i].LevelName = levelNameOrFallback(levelMap[rows[i].UserID])
		}
	}
	return nil
}

func beginningOfLocalDay(ts time.Time) time.Time {
	y, m, d := ts.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, ts.Location())
}

func localDateRange(businessDate time.Time, loc *time.Location) (time.Time, time.Time) {
	date := beginningOfLocalDay(businessDate.In(loc))
	return date.UTC(), date.AddDate(0, 0, 1).UTC()
}

func (s *PromotionService) currentLocalDayRange() (time.Time, time.Time) {
	return localDateRange(s.nowInAppLocation(), s.appLocation())
}

func parseSettlementClock(clock string) (int, int, error) {
	clock = strings.TrimSpace(clock)
	if len(clock) != 5 || clock[2] != ':' {
		return 0, 0, ErrPromotionInvalidSettlementTime
	}
	for _, idx := range []int{0, 1, 3, 4} {
		if clock[idx] < '0' || clock[idx] > '9' {
			return 0, 0, ErrPromotionInvalidSettlementTime
		}
	}
	hour := int(clock[0]-'0')*10 + int(clock[1]-'0')
	minute := int(clock[3]-'0')*10 + int(clock[4]-'0')
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, ErrPromotionInvalidSettlementTime
	}
	return hour, minute, nil
}

func settlementBoundaryDate(nowLocal time.Time, clock string, loc *time.Location) time.Time {
	hour, minute, err := parseSettlementClock(clock)
	if err != nil {
		hour = 0
		minute = 0
	}
	today := beginningOfLocalDay(nowLocal.In(loc))
	trigger := time.Date(today.Year(), today.Month(), today.Day(), hour, minute, 0, 0, loc)
	if nowLocal.Before(trigger) {
		return today.AddDate(0, 0, -1)
	}
	return today
}
