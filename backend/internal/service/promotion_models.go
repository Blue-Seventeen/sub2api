package service

import (
	"context"
	"time"
)

const (
	PromotionBindingSourceSelf  = "self"
	PromotionBindingSourceAdmin = "admin"

	PromotionCommissionTypeCommission = "commission"
	PromotionCommissionTypeActivation = "activation"
	PromotionCommissionTypeManual     = "manual"
	PromotionCommissionTypeAdjustment = "adjustment"
	PromotionCommissionTypePromotion  = "promotion"

	PromotionCommissionStatusPending   = "pending"
	PromotionCommissionStatusSettled   = "settled"
	PromotionCommissionStatusCancelled = "cancelled"

	PromotionSettlementStatusRunning   = "running"
	PromotionSettlementStatusSettled   = "settled"
	PromotionSettlementStatusFailed    = "failed"
	PromotionSettlementStatusCancelled = "cancelled"

	PromotionScriptCategoryDefault = "default"
	PromotionScriptCategoryWechat  = "wechat"
	PromotionScriptCategoryTech    = "tech"
	PromotionScriptCategorySocial  = "social"
	PromotionScriptCategoryEmail   = "email"
)

type PromotionUser struct {
	UserID        int64      `json:"user_id"`
	InviteCode    string     `json:"invite_code"`
	ParentUserID  *int64     `json:"parent_user_id,omitempty"`
	BindingSource string     `json:"binding_source"`
	BoundAt       *time.Time `json:"bound_at,omitempty"`
	BoundNote     string     `json:"bound_note,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type PromotionSettings struct {
	ActivationThresholdAmount float64   `json:"activation_threshold_amount"`
	ActivationBonusAmount     float64   `json:"activation_bonus_amount"`
	DailySettlementTime       string    `json:"daily_settlement_time"`
	SettlementEnabled         bool      `json:"settlement_enabled"`
	RuleActivationTemplate    string    `json:"rule_activation_template"`
	RuleDirectTemplate        string    `json:"rule_direct_template"`
	RuleIndirectTemplate      string    `json:"rule_indirect_template"`
	RuleLevelSummaryTemplate  string    `json:"rule_level_summary_template"`
	InviteBaseURL             string    `json:"invite_base_url"`
	PosterLogoURL             string    `json:"poster_logo_url"`
	PosterTitle               string    `json:"poster_title"`
	PosterHeadline            string    `json:"poster_headline"`
	PosterDescription         string    `json:"poster_description"`
	PosterScanHint            string    `json:"poster_scan_hint"`
	PosterTags                []string  `json:"poster_tags"`
	CreatedAt                 time.Time `json:"created_at,omitempty"`
	UpdatedAt                 time.Time `json:"updated_at,omitempty"`
}

type PromotionPosterConfig struct {
	InviteBaseURL     string   `json:"invite_base_url"`
	LogoURL           string   `json:"logo_url"`
	Title             string   `json:"title"`
	Headline          string   `json:"headline"`
	Description       string   `json:"description"`
	ScanHint          string   `json:"scan_hint"`
	Tags              []string `json:"tags"`
	PrimaryInviteCode string   `json:"primary_invite_code,omitempty"`
}

type PromotionLevelConfig struct {
	ID                       int64     `json:"id,omitempty"`
	LevelNo                  int       `json:"level_no"`
	LevelName                string    `json:"level_name"`
	RequiredActivatedInvites int       `json:"required_activated_invites"`
	DirectRate               float64   `json:"direct_rate"`
	IndirectRate             float64   `json:"indirect_rate"`
	SortOrder                int       `json:"sort_order"`
	Enabled                  bool      `json:"enabled"`
	CreatedAt                time.Time `json:"created_at,omitempty"`
	UpdatedAt                time.Time `json:"updated_at,omitempty"`
}

type PromotionActivation struct {
	UserID             int64     `json:"user_id"`
	PromoterUserID     int64     `json:"promoter_user_id"`
	ActivatedAt        time.Time `json:"activated_at"`
	ThresholdAmount    float64   `json:"threshold_amount"`
	TriggerUsageAmount float64   `json:"trigger_usage_amount"`
	CommissionRecordID *int64    `json:"commission_record_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PromotionSettlementBatch struct {
	ID               int64      `json:"id"`
	BusinessDate     time.Time  `json:"business_date"`
	Status           string     `json:"status"`
	TotalRecords     int        `json:"total_records"`
	TotalAmount      float64    `json:"total_amount"`
	ExecutedByUserID *int64     `json:"executed_by_user_id,omitempty"`
	ExecutedAt       *time.Time `json:"executed_at,omitempty"`
	Note             string     `json:"note,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type PromotionCommissionRecord struct {
	ID                int64      `json:"id"`
	BeneficiaryUserID int64      `json:"beneficiary_user_id"`
	SourceUserID      *int64     `json:"source_user_id,omitempty"`
	BusinessDate      time.Time  `json:"business_date"`
	CommissionType    string     `json:"commission_type"`
	RelationDepth     int        `json:"relation_depth"`
	LevelID           *int64     `json:"level_id,omitempty"`
	LevelSnapshot     string     `json:"level_snapshot,omitempty"`
	RateSnapshot      *float64   `json:"rate_snapshot,omitempty"`
	BaseAmount        float64    `json:"base_amount"`
	Amount            float64    `json:"amount"`
	Status            string     `json:"status"`
	SettlementBatchID *int64     `json:"settlement_batch_id,omitempty"`
	Note              string     `json:"note,omitempty"`
	CreatedByUserID   *int64     `json:"created_by_user_id,omitempty"`
	SettledAt         *time.Time `json:"settled_at,omitempty"`
	CancelledAt       *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type PromotionScript struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Category        string    `json:"category"`
	Content         string    `json:"content"`
	RenderedPreview string    `json:"rendered_preview,omitempty"`
	UseCount        int64     `json:"use_count"`
	Enabled         bool      `json:"enabled"`
	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PromotionReferrerPreview struct {
	UserID      int64  `json:"user_id"`
	InviteCode  string `json:"invite_code"`
	MaskedEmail string `json:"masked_email"`
	LevelName   string `json:"level_name"`
}

type PromotionLeaderboardItem struct {
	UserID         int64   `json:"user_id"`
	MaskedEmail    string  `json:"masked_email"`
	LevelName      string  `json:"level_name"`
	InviteCount    int     `json:"invite_count"`
	TotalEarnings  float64 `json:"total_earnings"`
	CurrentLevelNo int     `json:"current_level_no"`
}

type PromotionOverview struct {
	UserID                    int64                          `json:"user_id"`
	InviteCode                string                         `json:"invite_code"`
	InviteLink                string                         `json:"invite_link"`
	CurrentLevelNo            int                            `json:"current_level_no"`
	CurrentLevelName          string                         `json:"current_level_name"`
	CurrentDirectActivated    int                            `json:"current_direct_activated"`
	CurrentDirectRate         float64                        `json:"current_direct_rate"`
	CurrentIndirectRate       float64                        `json:"current_indirect_rate"`
	CurrentTotalRate          float64                        `json:"current_total_rate"`
	NextLevelNo               *int                           `json:"next_level_no,omitempty"`
	NextLevelName             string                         `json:"next_level_name,omitempty"`
	NextLevelRequiredActivate *int                           `json:"next_level_required_activate,omitempty"`
	TodayEarnings             float64                        `json:"today_earnings"`
	PendingAmount             float64                        `json:"pending_amount"`
	SettledAmount             float64                        `json:"settled_amount"`
	TotalRewardAmount         float64                        `json:"total_reward_amount"`
	CommissionAmount          float64                        `json:"commission_amount"`
	ActivationAmount          float64                        `json:"activation_amount"`
	TotalInvites              int                            `json:"total_invites"`
	ActivatedInvites          int                            `json:"activated_invites"`
	InactiveInvites           int                            `json:"inactive_invites"`
	ActivationThresholdAmount float64                        `json:"activation_threshold_amount"`
	ActivationBonusAmount     float64                        `json:"activation_bonus_amount"`
	LevelRateSummaries        []PromotionLevelRateSummary    `json:"level_rate_summaries"`
	RuleTemplates             PromotionRenderedRuleTemplates `json:"rule_templates"`
	PosterConfig              PromotionPosterConfig          `json:"poster_config"`
	Leaderboard               []PromotionLeaderboardItem     `json:"leaderboard"`
}

type PromotionLevelRateSummary struct {
	LevelNo                  int     `json:"level_no"`
	LevelName                string  `json:"level_name"`
	RequiredActivatedInvites int     `json:"required_activated_invites"`
	DirectRate               float64 `json:"direct_rate"`
	IndirectRate             float64 `json:"indirect_rate"`
	TotalRate                float64 `json:"total_rate"`
}

type PromotionRenderedRuleTemplates struct {
	Activation   string `json:"activation"`
	Direct       string `json:"direct"`
	Indirect     string `json:"indirect"`
	LevelSummary string `json:"level_summary"`
}

type PromotionTeamItem struct {
	UserID            int64      `json:"user_id"`
	Email             string     `json:"email"`
	Username          string     `json:"username"`
	MaskedEmail       string     `json:"masked_email"`
	RelationDepth     int        `json:"relation_depth"`
	LevelName         string     `json:"level_name"`
	Activated         bool       `json:"activated"`
	TodayContribution float64    `json:"today_contribution"`
	TotalContribution float64    `json:"total_contribution"`
	JoinedAt          time.Time  `json:"joined_at"`
	ActivatedAt       *time.Time `json:"activated_at,omitempty"`
}

type PromotionRelationRow struct {
	UserID               int64      `json:"user_id"`
	Email                string     `json:"email"`
	Username             string     `json:"username"`
	InviteCode           string     `json:"invite_code"`
	LevelName            string     `json:"level_name"`
	ParentUserID         *int64     `json:"parent_user_id,omitempty"`
	ParentEmail          string     `json:"parent_email"`
	DirectChildrenCount  int        `json:"direct_children_count"`
	TotalChildrenCount   int        `json:"total_children_count"`
	ActivatedDirectCount int        `json:"activated_direct_count"`
	BoundAt              *time.Time `json:"bound_at,omitempty"`
}

type PromotionRelationNode struct {
	UserID           int64    `json:"user_id"`
	Email            string   `json:"email"`
	LevelName        string   `json:"level_name"`
	InviteCode       string   `json:"invite_code"`
	InviteCount      int      `json:"invite_count"`
	TotalRate        float64  `json:"total_rate"`
	ActualRebateRate *float64 `json:"actual_rebate_rate,omitempty"`
}

type PromotionRelationChain struct {
	Current     *PromotionRelationNode `json:"current,omitempty"`
	Parent      *PromotionRelationNode `json:"parent,omitempty"`
	Grandparent *PromotionRelationNode `json:"grandparent,omitempty"`
}

type PromotionCommissionListItem struct {
	ID                  int64      `json:"id"`
	BeneficiaryUserID   int64      `json:"beneficiary_user_id"`
	BeneficiaryEmail    string     `json:"beneficiary_email"`
	BeneficiaryMasked   string     `json:"beneficiary_masked"`
	SourceUserID        *int64     `json:"source_user_id,omitempty"`
	SourceUserEmail     string     `json:"source_user_email"`
	SourceUserMasked    string     `json:"source_user_masked"`
	CommissionType      string     `json:"commission_type"`
	RelationDepth       int        `json:"relation_depth"`
	BusinessDate        time.Time  `json:"business_date"`
	BaseAmount          float64    `json:"base_amount"`
	Amount              float64    `json:"amount"`
	Status              string     `json:"status"`
	LevelName           string     `json:"level_name"`
	RateSnapshot        *float64   `json:"rate_snapshot,omitempty"`
	Note                string     `json:"note"`
	SettledAt           *time.Time `json:"settled_at,omitempty"`
	CancelledAt         *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	SettlementBatchID   *int64     `json:"settlement_batch_id,omitempty"`
	SettlementBatchDate *time.Time `json:"settlement_batch_date,omitempty"`
}

type PromotionAdminDashboard struct {
	TotalSettledAmount float64 `json:"total_settled_amount"`
	PendingAmount      float64 `json:"pending_amount"`
	BoundUsers         int     `json:"bound_users"`
	ActivatedUsers     int     `json:"activated_users"`
	TodayNewBindings   int     `json:"today_new_bindings"`
	TodayNewActivates  int     `json:"today_new_activates"`
	TodayPendingAmount float64 `json:"today_pending_amount"`
}

type PromotionTeamFilter struct {
	Page      int
	PageSize  int
	Keyword   string
	Status    string
	SortBy    string
	SortOrder string
}

type PromotionCommissionFilter struct {
	Page     int
	PageSize int
	Keyword  string
	Type     string
	Status   string
}

type PromotionCommissionAdminFilter struct {
	Page     int
	PageSize int
	Keyword  string
	Type     string
	Status   string
	DateFrom *time.Time
	DateTo   *time.Time
}

type PromotionScriptFilter struct {
	Page     int
	PageSize int
	Keyword  string
	Category string
}

type PromotionConfigPayload struct {
	Settings PromotionSettings
	Levels   []PromotionLevelConfig
}

type PromotionActivationCandidate struct {
	UserID             int64
	PromoterUserID     int64
	TriggerUsageAmount float64
	ActivatedAt        time.Time
}

type PromotionDailyAggregate struct {
	SourceUserID        int64
	DirectBeneficiary   *int64
	IndirectBeneficiary *int64
	BaseAmount          float64
}

type PromotionSettleSummary struct {
	SettledCount int
	TotalAmount  float64
}

type PromotionRepository interface {
	EnsurePromotionUser(ctx context.Context, userID int64) (*PromotionUser, error)
	GetPromotionUserByUserID(ctx context.Context, userID int64) (*PromotionUser, error)
	GetPromotionUserByInviteCode(ctx context.Context, inviteCode string) (*PromotionUser, error)
	SetPromotionParent(ctx context.Context, userID int64, parentUserID *int64, source, note string, boundAt time.Time) (*PromotionUser, error)
	HasPromotionDescendant(ctx context.Context, ancestorUserID, descendantUserID int64) (bool, error)
	GetUserRealActualCost(ctx context.Context, userID int64) (float64, error)

	GetPromotionSettings(ctx context.Context) (*PromotionSettings, error)
	UpdatePromotionSettings(ctx context.Context, settings PromotionSettings) (*PromotionSettings, error)
	ListPromotionLevels(ctx context.Context) ([]PromotionLevelConfig, error)
	UpsertPromotionLevels(ctx context.Context, levels []PromotionLevelConfig) ([]PromotionLevelConfig, error)

	ListPromotionScripts(ctx context.Context, filter PromotionScriptFilter) ([]PromotionScript, int64, error)
	GetPromotionScriptByID(ctx context.Context, id int64) (*PromotionScript, error)
	CreatePromotionScript(ctx context.Context, script PromotionScript) (*PromotionScript, error)
	UpdatePromotionScript(ctx context.Context, script PromotionScript) (*PromotionScript, error)
	DeletePromotionScript(ctx context.Context, id int64) error
	IncrementPromotionScriptUse(ctx context.Context, id int64) error

	CountPromotionDescendants(ctx context.Context, userID int64) (int, error)
	CountPromotionActivatedDescendants(ctx context.Context, userID int64) (int, error)
	CountDirectActivatedInvites(ctx context.Context, userID int64) (int, error)
	GetCurrentPromotionLevel(ctx context.Context, userID int64) (*PromotionLevelConfig, error)
	GetNextPromotionLevel(ctx context.Context, userID int64) (*PromotionLevelConfig, error)
	GetPromotionOverviewSummary(ctx context.Context, userID int64, businessDate time.Time) (*PromotionOverview, error)
	ListPromotionLeaderboard(ctx context.Context, limit int) ([]PromotionLeaderboardItem, error)
	ListPromotionTeam(ctx context.Context, rootUserID int64, filter PromotionTeamFilter, todayStart, todayEnd time.Time) ([]PromotionTeamItem, int64, error)
	ListPromotionEarnings(ctx context.Context, userID int64, filter PromotionCommissionFilter) ([]PromotionCommissionListItem, int64, error)

	GetPromotionAdminDashboard(ctx context.Context, businessDate time.Time) (*PromotionAdminDashboard, error)
	SearchPromotionRelationUserIDs(ctx context.Context, keyword string, page, pageSize int) ([]int64, int64, error)
	ListPromotionRelationsByUserIDs(ctx context.Context, userIDs []int64) ([]PromotionRelationRow, error)
	GetPromotionRelationChain(ctx context.Context, userID int64) (*PromotionRelationChain, error)
	ListPromotionDownlines(ctx context.Context, rootUserID int64, filter PromotionTeamFilter, todayStart, todayEnd time.Time) ([]PromotionTeamItem, int64, error)
	RemovePromotionDirectDownline(ctx context.Context, parentUserID, downlineUserID int64, note string) error
	ListPromotionCommissions(ctx context.Context, filter PromotionCommissionAdminFilter) ([]PromotionCommissionListItem, int64, error)

	CreateManualPromotionCommission(ctx context.Context, record PromotionCommissionRecord, settleNow bool) (*PromotionCommissionRecord, error)
	UpdatePromotionCommission(ctx context.Context, commissionID int64, operatorUserID *int64, amount float64, note string) (*PromotionCommissionRecord, error)
	SettlePromotionCommission(ctx context.Context, commissionID int64, operatorUserID *int64, note string) (*PromotionCommissionRecord, error)
	BatchSettlePromotionCommissions(ctx context.Context, ids []int64, operatorUserID *int64, note string) (PromotionSettleSummary, error)
	CancelPromotionCommission(ctx context.Context, commissionID int64, operatorUserID *int64, note string) (*PromotionCommissionRecord, error)

	ListPendingActivationCandidates(ctx context.Context, threshold float64, now time.Time) ([]PromotionActivationCandidate, error)
	CreatePromotionActivation(ctx context.Context, activation PromotionActivation, bonusAmount float64) error
	UpsertDailyPromotionCommissions(ctx context.Context, businessDate time.Time, businessStart, businessEnd time.Time) error
	ListSettlablePromotionBusinessDates(ctx context.Context, boundaryDate time.Time) ([]time.Time, error)
	SettlePromotionBusinessDate(ctx context.Context, businessDate time.Time, operatorUserID *int64, note string) error
}
