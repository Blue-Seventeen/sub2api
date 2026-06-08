package service

import (
	"time"
)

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	Status       string
	ExpiresAt    time.Time
	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64
	CustomUsage  float64
	Version      int64

	DailyLimitUSD       *float64
	WeeklyLimitUSD      *float64
	MonthlyLimitUSD     *float64
	CustomLimitUSD      *float64
	CustomLimitHours    int
	StackedAvailableUSD *float64
	DailyWindowStart    *time.Time
	WeeklyWindowStart   *time.Time
	MonthlyWindowStart  *time.Time
	CustomWindowStart   *time.Time
}
