package service

import (
	"context"
	"errors"
	"math"
	"time"
)

var ErrAffiliateProfileNotFound = errors.New("affiliate profile not found")

// Affiliate compatibility is intentionally inert in this fork. The custom
// Promotion module owns invitation/rebate semantics, but upstream OAuth tests
// and DTOs still compile against these types.
type AffiliateSummary struct {
	UserID               int64
	AffCode              string
	InviterID            *int64
	AffCount             int
	AffQuota             float64
	AffFrozenQuota       float64
	AffHistoryQuota      float64
	AffRebateRatePercent *float64
	AffCodeCustom        bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AffiliateInvitee struct {
	UserID    int64
	Email     string
	CreatedAt time.Time
}

type AffiliateAdminFilter struct {
	Query    string
	Page     int
	PageSize int
}

type AffiliateAdminEntry struct {
	UserID int64
	Email  string
}

type AffiliateRecordFilter struct {
	UserID   *int64
	Page     int
	PageSize int
}

type AffiliateInviteRecord struct {
	ID int64
}

type AffiliateRebateRecord struct {
	ID int64
}

type AffiliateTransferRecord struct {
	ID int64
}

// AffiliateWithdrawResult remains available for source compatibility with
// historical repository test doubles and DTO consumers. Withdraw operations
// are intentionally inert in this fork; Promotion owns the active rebate
// workflow and no withdrawal method is exposed by AffiliateRepository.
type AffiliateWithdrawResult struct {
	LedgerID            int64   `json:"ledger_id"`
	UserID              int64   `json:"user_id"`
	Amount              float64 `json:"amount"`
	AvailableQuotaAfter float64 `json:"available_quota_after"`
	FrozenQuotaAfter    float64 `json:"frozen_quota_after"`
	HistoryQuotaAfter   float64 `json:"history_quota_after"`
	Replayed            bool    `json:"-"`
}

type AffiliateUserOverview struct {
	UserID int64
}

type AffiliateRepository interface {
	EnsureUserAffiliate(ctx context.Context, userID int64) (*AffiliateSummary, error)
	GetAffiliateByCode(ctx context.Context, code string) (*AffiliateSummary, error)
	BindInviter(ctx context.Context, userID, inviterID int64) (bool, error)
	AccrueQuota(ctx context.Context, inviterID, inviteeID int64, amount float64, freezeHours int, sourceOrderID *int64) (bool, error)
	GetAccruedRebateFromInvitee(ctx context.Context, inviterID, inviteeID int64) (float64, error)
	ThawFrozenQuota(ctx context.Context, userID int64) (float64, error)
	TransferQuotaToBalance(ctx context.Context, userID int64) (float64, float64, error)
	ListInvitees(ctx context.Context, inviterID int64, limit int) ([]AffiliateInvitee, error)
	UpdateUserAffCode(ctx context.Context, userID int64, code string) error
	ResetUserAffCode(ctx context.Context, userID int64) (string, error)
	SetUserRebateRate(ctx context.Context, userID int64, ratePercent *float64) error
	BatchSetUserRebateRate(ctx context.Context, userIDs []int64, ratePercent *float64) error
	ListUsersWithCustomSettings(ctx context.Context, filter AffiliateAdminFilter) ([]AffiliateAdminEntry, int64, error)
	ListAffiliateInviteRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateInviteRecord, int64, error)
	ListAffiliateRebateRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateRebateRecord, int64, error)
	ListAffiliateTransferRecords(ctx context.Context, filter AffiliateRecordFilter) ([]AffiliateTransferRecord, int64, error)
	GetAffiliateUserOverview(ctx context.Context, userID int64) (*AffiliateUserOverview, error)
}

type AffiliateService struct {
	repo           AffiliateRepository
	settingService *SettingService
}

func NewAffiliateService(repo AffiliateRepository, settingService *SettingService, _ APIKeyAuthCacheInvalidator, _ *BillingCacheService) *AffiliateService {
	return &AffiliateService{repo: repo, settingService: settingService}
}

func (s *AffiliateService) IsEnabled(ctx context.Context) bool {
	return false
}

func (s *AffiliateService) AccrueInviteRebate(ctx context.Context, inviteeUserID int64, baseRechargeAmount float64) (float64, error) {
	return s.AccrueInviteRebateForOrder(ctx, inviteeUserID, baseRechargeAmount, nil)
}

func (s *AffiliateService) AccrueInviteRebateForOrder(ctx context.Context, inviteeUserID int64, baseRechargeAmount float64, sourceOrderID *int64) (float64, error) {
	// Compatibility callers receive the historical no-op result. Promotion is
	// the only referral and rebate system in this fork.
	return 0, nil
}

func (s *AffiliateService) resolveRebateRatePercent(ctx context.Context, inviter *AffiliateSummary) float64 {
	if inviter != nil && inviter.AffRebateRatePercent != nil {
		v := *inviter.AffRebateRatePercent
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			return clampAffiliateRebateRate(v)
		}
	}
	if s == nil || s.settingService == nil {
		return 0
	}
	return clampAffiliateRebateRate(s.settingService.GetAffiliateRebateRatePercent(ctx))
}

func affiliateRoundTo(v float64, scale int) float64 {
	factor := math.Pow10(scale)
	return math.Round(v*factor) / factor
}
