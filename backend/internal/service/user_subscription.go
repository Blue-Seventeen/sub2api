package service

import "time"

const (
	subscriptionDailyWindow   = 24 * time.Hour
	subscriptionWeeklyWindow  = 7 * 24 * time.Hour
	subscriptionMonthlyWindow = 30 * 24 * time.Hour
	maxCustomLimitHours       = 24 * 365 * 10
)

type UserSubscription struct {
	ID      int64
	UserID  int64
	GroupID int64

	StartsAt  time.Time
	ExpiresAt time.Time
	Status    string

	IsAggregate         bool
	SubscriptionCount   int
	StackedAvailableUSD *float64

	EffectiveAvailableUSD    *float64
	EffectiveResetsAt        *time.Time
	EffectiveDailyResetsAt   *time.Time
	EffectiveWeeklyResetsAt  *time.Time
	EffectiveMonthlyResetsAt *time.Time
	EffectiveCustomResetsAt  *time.Time
	EffectiveDailyUsageUSD   *float64
	EffectiveWeeklyUsageUSD  *float64
	EffectiveMonthlyUsageUSD *float64
	EffectiveCustomUsageUSD  *float64

	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time
	CustomWindowStart  *time.Time

	DailyUsageUSD   float64
	WeeklyUsageUSD  float64
	MonthlyUsageUSD float64
	CustomUsageUSD  float64

	AssignedBy *int64
	AssignedAt time.Time
	Notes      string

	SourceType         *string
	SourceRefID        *string
	SourceRedeemCodeID *int64
	RedeemCodeSnapshot *string

	GroupNameSnapshot           *string
	GroupPlatformSnapshot       *string
	GroupRateMultiplierSnapshot *float64
	DailyLimitUSDSnapshot       *float64
	WeeklyLimitUSDSnapshot      *float64
	MonthlyLimitUSDSnapshot     *float64
	CustomLimitHoursSnapshot    *int
	CustomLimitUSDSnapshot      *float64

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	User           *User
	Group          *Group
	AssignedByUser *User
}

func (s *UserSubscription) IsActive() bool {
	return s.Status == SubscriptionStatusActive && time.Now().Before(s.ExpiresAt)
}

func (s *UserSubscription) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *UserSubscription) DaysRemaining() int {
	return s.daysRemainingAt(time.Now())
}

func (s *UserSubscription) daysRemainingAt(now time.Time) int {
	remaining := s.ExpiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}

	days := int(remaining / subscriptionDailyWindow)
	if remaining%subscriptionDailyWindow != 0 {
		days++
	}
	return days
}

func (s *UserSubscription) IsWindowActivated() bool {
	return s.DailyWindowStart != nil || s.WeeklyWindowStart != nil || s.MonthlyWindowStart != nil || s.CustomWindowStart != nil
}

func (s *UserSubscription) HasOneTimeDailyQuota() bool {
	if s == nil || s.StartsAt.IsZero() || s.ExpiresAt.IsZero() {
		return false
	}
	return !s.ExpiresAt.After(s.StartsAt.AddDate(0, 0, 1))
}

func (s *UserSubscription) NeedsDailyReset() bool {
	return s.NeedsDailyResetAt(time.Now())
}

func (s *UserSubscription) NeedsDailyResetAt(now time.Time) bool {
	if s.DailyWindowStart == nil {
		return false
	}
	if s.HasOneTimeDailyQuota() {
		return false
	}
	return subscriptionWindowExpired(s.DailyWindowStart, subscriptionDailyWindow, now)
}

func (s *UserSubscription) NeedsWeeklyReset() bool {
	return s.NeedsWeeklyResetAt(time.Now())
}

func (s *UserSubscription) NeedsWeeklyResetAt(now time.Time) bool {
	if s.WeeklyWindowStart == nil {
		return false
	}
	return subscriptionWindowExpired(s.WeeklyWindowStart, subscriptionWeeklyWindow, now)
}

func (s *UserSubscription) NeedsMonthlyReset() bool {
	return s.NeedsMonthlyResetAt(time.Now())
}

func (s *UserSubscription) NeedsMonthlyResetAt(now time.Time) bool {
	if s.MonthlyWindowStart == nil {
		return false
	}
	return subscriptionWindowExpired(s.MonthlyWindowStart, subscriptionMonthlyWindow, now)
}

func (s *UserSubscription) NeedsCustomResetAt(group *Group, now time.Time) bool {
	if s.CustomWindowStart == nil {
		return false
	}
	period := customSubscriptionWindowHours(s.EffectiveCustomLimitHours(group))
	if period <= 0 || !hasPositiveLimit(s.EffectiveCustomLimitUSD(group)) {
		return false
	}
	return subscriptionWindowExpired(s.CustomWindowStart, period, now)
}

func (s *UserSubscription) DailyResetTime() *time.Time {
	if s.DailyWindowStart == nil {
		return nil
	}
	if s.HasOneTimeDailyQuota() {
		t := s.ExpiresAt
		return &t
	}
	t := s.DailyWindowStart.Add(subscriptionDailyWindow)
	return &t
}

func (s *UserSubscription) EffectiveDisplayDailyResetTime() *time.Time {
	if s.DailyWindowStart == nil || s.HasOneTimeDailyQuota() {
		return nil
	}
	t := s.DailyWindowStart.Add(subscriptionDailyWindow)
	return &t
}

func (s *UserSubscription) WeeklyResetTime() *time.Time {
	if s.WeeklyWindowStart == nil {
		return nil
	}
	t := s.WeeklyWindowStart.Add(subscriptionWeeklyWindow)
	return &t
}

func (s *UserSubscription) MonthlyResetTime() *time.Time {
	if s.MonthlyWindowStart == nil {
		return nil
	}
	t := s.MonthlyWindowStart.Add(subscriptionMonthlyWindow)
	return &t
}

func (s *UserSubscription) CustomResetTime(group *Group) *time.Time {
	if s.CustomWindowStart == nil {
		return nil
	}
	period := customSubscriptionWindowHours(s.EffectiveCustomLimitHours(group))
	if period <= 0 || !hasPositiveLimit(s.EffectiveCustomLimitUSD(group)) {
		return nil
	}
	t := s.CustomWindowStart.Add(period)
	return &t
}

func customSubscriptionWindow(group *Group) time.Duration {
	if group == nil || group.CustomLimitHours <= 0 {
		return 0
	}
	return customSubscriptionWindowHours(group.CustomLimitHours)
}

func customSubscriptionWindowHours(hours int) time.Duration {
	if hours > maxCustomLimitHours {
		hours = maxCustomLimitHours
	}
	if hours <= 0 {
		return 0
	}
	return time.Duration(hours) * time.Hour
}

func subscriptionWindowExpired(start *time.Time, period time.Duration, now time.Time) bool {
	if start == nil {
		return false
	}
	return !now.Before(start.Add(period))
}

func (s *UserSubscription) CheckDailyLimit(group *Group, additionalCost float64) bool {
	limit := s.EffectiveDailyLimitUSD(group)
	if !hasPositiveLimit(limit) {
		return true
	}
	return s.DailyUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckWeeklyLimit(group *Group, additionalCost float64) bool {
	limit := s.EffectiveWeeklyLimitUSD(group)
	if !hasPositiveLimit(limit) {
		return true
	}
	return s.WeeklyUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckMonthlyLimit(group *Group, additionalCost float64) bool {
	limit := s.EffectiveMonthlyLimitUSD(group)
	if !hasPositiveLimit(limit) {
		return true
	}
	return s.MonthlyUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckCustomLimit(group *Group, additionalCost float64) bool {
	limit := s.EffectiveCustomLimitUSD(group)
	if s.EffectiveCustomLimitHours(group) <= 0 || !hasPositiveLimit(limit) {
		return true
	}
	return s.CustomUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckAllLimits(group *Group, additionalCost float64) (daily, weekly, monthly, custom bool) {
	daily = s.CheckDailyLimit(group, additionalCost)
	weekly = s.CheckWeeklyLimit(group, additionalCost)
	monthly = s.CheckMonthlyLimit(group, additionalCost)
	custom = s.CheckCustomLimit(group, additionalCost)
	return
}

func (s *UserSubscription) EffectiveDailyLimitUSD(group *Group) *float64 {
	group = s.resolveEffectiveGroupInput(group)
	if s != nil && s.shouldUseGroupSnapshot(group) && (s.DailyLimitUSDSnapshot != nil || s.hasCompleteLimitSnapshot()) {
		return s.DailyLimitUSDSnapshot
	}
	if group != nil {
		return group.DailyLimitUSD
	}
	if s != nil {
		return s.DailyLimitUSDSnapshot
	}
	return nil
}

func (s *UserSubscription) EffectiveWeeklyLimitUSD(group *Group) *float64 {
	group = s.resolveEffectiveGroupInput(group)
	if s != nil && s.shouldUseGroupSnapshot(group) && (s.WeeklyLimitUSDSnapshot != nil || s.hasCompleteLimitSnapshot()) {
		return s.WeeklyLimitUSDSnapshot
	}
	if group != nil {
		return group.WeeklyLimitUSD
	}
	if s != nil {
		return s.WeeklyLimitUSDSnapshot
	}
	return nil
}

func (s *UserSubscription) EffectiveMonthlyLimitUSD(group *Group) *float64 {
	group = s.resolveEffectiveGroupInput(group)
	if s != nil && s.shouldUseGroupSnapshot(group) && (s.MonthlyLimitUSDSnapshot != nil || s.hasCompleteLimitSnapshot()) {
		return s.MonthlyLimitUSDSnapshot
	}
	if group != nil {
		return group.MonthlyLimitUSD
	}
	if s != nil {
		return s.MonthlyLimitUSDSnapshot
	}
	return nil
}

func (s *UserSubscription) EffectiveCustomLimitHours(group *Group) int {
	group = s.resolveEffectiveGroupInput(group)
	if s != nil && s.shouldUseGroupSnapshot(group) && s.CustomLimitHoursSnapshot != nil {
		return *s.CustomLimitHoursSnapshot
	}
	if group != nil {
		return group.CustomLimitHours
	}
	if s != nil && s.CustomLimitHoursSnapshot != nil {
		return *s.CustomLimitHoursSnapshot
	}
	return 0
}

func (s *UserSubscription) EffectiveCustomLimitUSD(group *Group) *float64 {
	group = s.resolveEffectiveGroupInput(group)
	if s != nil && s.shouldUseGroupSnapshot(group) && (s.CustomLimitUSDSnapshot != nil || s.hasCompleteLimitSnapshot()) {
		return s.CustomLimitUSDSnapshot
	}
	if group != nil {
		return group.CustomLimitUSD
	}
	if s != nil {
		return s.CustomLimitUSDSnapshot
	}
	return nil
}

func (s *UserSubscription) HasGroupSnapshot() bool {
	return s != nil &&
		(s.GroupNameSnapshot != nil ||
			s.GroupPlatformSnapshot != nil ||
			s.GroupRateMultiplierSnapshot != nil ||
			s.DailyLimitUSDSnapshot != nil ||
			s.WeeklyLimitUSDSnapshot != nil ||
			s.MonthlyLimitUSDSnapshot != nil ||
			s.CustomLimitHoursSnapshot != nil ||
			s.CustomLimitUSDSnapshot != nil)
}

func (s *UserSubscription) hasCompleteLimitSnapshot() bool {
	return s != nil &&
		s.DailyLimitUSDSnapshot != nil &&
		s.WeeklyLimitUSDSnapshot != nil &&
		s.MonthlyLimitUSDSnapshot != nil &&
		s.CustomLimitHoursSnapshot != nil &&
		s.CustomLimitUSDSnapshot != nil
}

func (s *UserSubscription) EffectiveGroup(group *Group) *Group {
	if s == nil {
		return nil
	}
	if group == nil {
		group = s.Group
	}
	useSnapshot := s.shouldUseGroupSnapshot(group)
	if group == nil {
		group = &Group{ID: s.GroupID}
	}
	out := *group
	if useSnapshot && s.GroupNameSnapshot != nil {
		out.Name = *s.GroupNameSnapshot
	}
	if useSnapshot && s.GroupPlatformSnapshot != nil {
		out.Platform = *s.GroupPlatformSnapshot
	}
	if useSnapshot && s.GroupRateMultiplierSnapshot != nil {
		out.RateMultiplier = *s.GroupRateMultiplierSnapshot
	}
	completeLimitSnapshot := s.hasCompleteLimitSnapshot()
	if useSnapshot && s.DailyLimitUSDSnapshot != nil {
		out.DailyLimitUSD = s.DailyLimitUSDSnapshot
	} else if useSnapshot && completeLimitSnapshot {
		out.DailyLimitUSD = nil
	} else {
		out.DailyLimitUSD = group.DailyLimitUSD
	}
	if useSnapshot && s.WeeklyLimitUSDSnapshot != nil {
		out.WeeklyLimitUSD = s.WeeklyLimitUSDSnapshot
	} else if useSnapshot && completeLimitSnapshot {
		out.WeeklyLimitUSD = nil
	} else {
		out.WeeklyLimitUSD = group.WeeklyLimitUSD
	}
	if useSnapshot && s.MonthlyLimitUSDSnapshot != nil {
		out.MonthlyLimitUSD = s.MonthlyLimitUSDSnapshot
	} else if useSnapshot && completeLimitSnapshot {
		out.MonthlyLimitUSD = nil
	} else {
		out.MonthlyLimitUSD = group.MonthlyLimitUSD
	}
	if useSnapshot && s.CustomLimitHoursSnapshot != nil {
		out.CustomLimitHours = *s.CustomLimitHoursSnapshot
	} else {
		out.CustomLimitHours = group.CustomLimitHours
	}
	if useSnapshot && s.CustomLimitUSDSnapshot != nil {
		out.CustomLimitUSD = s.CustomLimitUSDSnapshot
	} else if useSnapshot && completeLimitSnapshot {
		out.CustomLimitUSD = nil
	} else {
		out.CustomLimitUSD = group.CustomLimitUSD
	}
	out.SubscriptionType = SubscriptionTypeSubscription
	out.Hydrated = true
	return &out
}

func (s *UserSubscription) resolveEffectiveGroupInput(group *Group) *Group {
	if s == nil {
		return group
	}
	if group != nil {
		return group
	}
	return s.Group
}

func (s *UserSubscription) shouldUseGroupSnapshot(group *Group) bool {
	if s == nil {
		return false
	}
	if group == nil {
		return true
	}
	return s.UsesHistoricalGroupSnapshotAt(time.Now())
}

func (s *UserSubscription) UsesHistoricalGroupSnapshotAt(now time.Time) bool {
	if s == nil {
		return false
	}
	if s.IsAggregate {
		return true
	}
	if s.DeletedAt != nil {
		return true
	}
	if s.Status == SubscriptionStatusRevoked || s.Status == SubscriptionStatusExpired {
		return true
	}
	if !s.ExpiresAt.IsZero() && !s.ExpiresAt.After(now) {
		return true
	}
	return false
}

func hasPositiveLimit(limit *float64) bool {
	return limit != nil && *limit > 0
}
