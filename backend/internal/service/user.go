package service

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID                   int64
	Email                string
	Username             string
	Notes                string
	AvatarURL            string
	AvatarSource         string
	AvatarMIME           string
	AvatarByteSize       int
	AvatarSHA256         string
	PasswordHash         string
	Role                 string
	Balance              float64
	Concurrency          int
	Status               string
	AllowedGroups        []int64
	TokenVersion         int64 // Incremented on password change to invalidate existing tokens
	TokenVersionResolved bool  // TokenVersion already contains the fingerprint-derived value expected in JWT claims and refresh-token state.
	SignupSource         string
	LastLoginAt          *time.Time
	LastActiveAt         *time.Time
	LastUsedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time

	// UnifiedRateEnabled indicates whether the user's unified multiplier is enabled.
	// 关闭时统一倍率按 1.0 处理。
	UnifiedRateEnabled bool
	// UnifiedRateMultiplier is the user-level unified multiplier.
	// 允许为 0；仅在 UnifiedRateEnabled=true 时生效。
	UnifiedRateMultiplier float64
	// v0.1.114_Beta 增量兼容说明：
	// RealBalance / DisplayBalance 均为服务层派生展示字段，不单独落库。
	// - RealBalance = Balance
	// - DisplayBalance = Balance * effective unified multiplier
	// 当前管理员前端为了减少改动，允许继续把旧字段 balance 当作“显示余额”读取。
	// RealBalance is the admin-facing real balance view derived from Balance.
	// 仅用于展示，不单独持久化。
	RealBalance float64
	// DisplayBalance is the admin-facing display balance view derived from
	// RealBalance × unified multiplier. 仅用于展示，不单独持久化。
	DisplayBalance float64

	// GroupRates 用户专属分组倍率配置
	// map[groupID]rateMultiplier
	GroupRates map[int64]float64

	// TOTP 双因素认证字段
	TotpSecretEncrypted *string    // AES-256-GCM 加密的 TOTP 密钥
	TotpEnabled         bool       // 是否启用 TOTP
	TotpEnabledAt       *time.Time // TOTP 启用时间

	// 余额不足通知
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string // "fixed" (default) | "percentage"
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []NotifyEmailEntry
	TotalRecharged             float64

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）。仅在所用分组未设置 rpm_limit
	// 且该 (用户, 分组) 无 rpm_override 时作为全局兜底生效，计数键 rpm:u:{userID}:{min}。
	RPMLimit int

	// UserGroupRPMOverride 来自 auth cache snapshot 的 (user, group) RPM 覆盖值。
	// nil = 该 API Key 对应的 (user, group) 无 override；非 nil 时 checkRPM 直接使用，
	// 避免每请求查 DB。字段不持久化到数据库。
	UserGroupRPMOverride *int

	APIKeys       []APIKey
	Subscriptions []UserSubscription
}

func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin
}

func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

// EffectiveUnifiedRateMultiplier returns the effective unified multiplier.
// 规则：
//   - 未启用时返回 1
//   - 小于 0 的非法值按 1 兜底
//   - 允许 0，表示显示余额/显示扣费均为 0
func (u *User) EffectiveUnifiedRateMultiplier() float64 {
	if u == nil || !u.UnifiedRateEnabled {
		return 1
	}
	if u.UnifiedRateMultiplier < 0 {
		return 1
	}
	return u.UnifiedRateMultiplier
}

// RefreshBalanceViews recalculates the derived real/display balance fields.
func (u *User) RefreshBalanceViews() {
	if u == nil {
		return
	}
	u.RealBalance = u.Balance
	u.DisplayBalance = u.Balance * u.EffectiveUnifiedRateMultiplier()
}

// CanBindGroup checks whether a user can bind to a given group.
// For standard groups:
// - Public groups (non-exclusive): all users can bind
// - Exclusive groups: only users with the group in AllowedGroups can bind
func (u *User) CanBindGroup(groupID int64, isExclusive bool) bool {
	// 公开分组（非专属）：所有用户都可以绑定
	if !isExclusive {
		return true
	}
	// 专属分组：需要在 AllowedGroups 中
	for _, id := range u.AllowedGroups {
		if id == groupID {
			return true
		}
	}
	return false
}

func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}
