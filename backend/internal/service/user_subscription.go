package service

import "time"

const (
	subscriptionDailyWindow   = 24 * time.Hour
	subscriptionWeeklyWindow  = 7 * 24 * time.Hour
	subscriptionMonthlyWindow = 30 * 24 * time.Hour
)

type UserSubscription struct {
	ID      int64
	UserID  int64
	GroupID int64

	StartsAt  time.Time
	ExpiresAt time.Time
	Status    string

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

	CreatedAt time.Time
	UpdatedAt time.Time

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
	if s.IsExpired() {
		return 0
	}
	return int(time.Until(s.ExpiresAt).Hours() / 24)
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
	if s.CustomWindowStart == nil || group == nil || !group.HasCustomLimit() {
		return false
	}
	return subscriptionWindowExpired(s.CustomWindowStart, customSubscriptionWindow(group), now)
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
	if s.CustomWindowStart == nil || group == nil || !group.HasCustomLimit() {
		return nil
	}
	t := s.CustomWindowStart.Add(customSubscriptionWindow(group))
	return &t
}

func customSubscriptionWindow(group *Group) time.Duration {
	if group == nil || group.CustomLimitHours <= 0 {
		return 0
	}
	hours := group.CustomLimitHours
	if hours > maxCustomLimitHours {
		hours = maxCustomLimitHours
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
	if !group.HasDailyLimit() {
		return true
	}
	return s.DailyUsageUSD+additionalCost <= *group.DailyLimitUSD
}

func (s *UserSubscription) CheckWeeklyLimit(group *Group, additionalCost float64) bool {
	if !group.HasWeeklyLimit() {
		return true
	}
	return s.WeeklyUsageUSD+additionalCost <= *group.WeeklyLimitUSD
}

func (s *UserSubscription) CheckMonthlyLimit(group *Group, additionalCost float64) bool {
	if !group.HasMonthlyLimit() {
		return true
	}
	return s.MonthlyUsageUSD+additionalCost <= *group.MonthlyLimitUSD
}

func (s *UserSubscription) CheckCustomLimit(group *Group, additionalCost float64) bool {
	if !group.HasCustomLimit() {
		return true
	}
	return s.CustomUsageUSD+additionalCost <= *group.CustomLimitUSD
}

func (s *UserSubscription) CheckAllLimits(group *Group, additionalCost float64) (daily, weekly, monthly, custom bool) {
	daily = s.CheckDailyLimit(group, additionalCost)
	weekly = s.CheckWeeklyLimit(group, additionalCost)
	monthly = s.CheckMonthlyLimit(group, additionalCost)
	custom = s.CheckCustomLimit(group, additionalCost)
	return
}
