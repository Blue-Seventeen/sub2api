package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUserSubscriptionFromService_HidesAdminSourceSnapshots(t *testing.T) {
	sourceType := "redeem_code"
	sourceRefID := "12"
	redeemCode := "RC-SECRET"
	groupName := "historic group"
	limit := 100.0
	currentLimit := 50.0
	sub := &service.UserSubscription{
		ID:                    1,
		UserID:                2,
		GroupID:               3,
		StartsAt:              time.Now().Add(-time.Hour),
		ExpiresAt:             time.Now().Add(time.Hour),
		Status:                service.SubscriptionStatusActive,
		SourceType:            &sourceType,
		SourceRefID:           &sourceRefID,
		RedeemCodeSnapshot:    &redeemCode,
		GroupNameSnapshot:     &groupName,
		DailyLimitUSDSnapshot: &limit,
		Group: &service.Group{
			ID:               3,
			Name:             "current group",
			SubscriptionType: service.SubscriptionTypeSubscription,
			DailyLimitUSD:    &currentLimit,
		},
	}

	userDTO := UserSubscriptionFromService(sub)
	if userDTO.SourceType != nil || userDTO.SourceRefID != nil || userDTO.RedeemCodeSnapshot != nil {
		t.Fatalf("user subscription DTO leaked source fields: %#v", userDTO)
	}
	payload, err := json.Marshal(userDTO)
	if err != nil {
		t.Fatalf("marshal user subscription DTO: %v", err)
	}
	if strings.Contains(string(payload), "can_reactivate") {
		t.Fatalf("user subscription DTO leaked admin reactivation flag: %s", payload)
	}
	if userDTO.Group == nil || userDTO.Group.Name != "current group" {
		t.Fatalf("active user subscription DTO should use current group, got %#v", userDTO.Group)
	}
	if userDTO.Group.DailyLimitUSD == nil || *userDTO.Group.DailyLimitUSD != currentLimit {
		t.Fatalf("active user subscription DTO should use current group limit, got %#v", userDTO.Group)
	}

	adminDTO := UserSubscriptionFromServiceAdmin(sub)
	if adminDTO.SourceType == nil || *adminDTO.SourceType != sourceType {
		t.Fatalf("admin subscription DTO missing source_type: %#v", adminDTO)
	}
	if adminDTO.RedeemCodeSnapshot == nil || *adminDTO.RedeemCodeSnapshot != redeemCode {
		t.Fatalf("admin subscription DTO missing redeem_code_snapshot: %#v", adminDTO)
	}
	if adminDTO.GroupNameSnapshot == nil || *adminDTO.GroupNameSnapshot != groupName {
		t.Fatalf("admin subscription DTO missing group_name_snapshot: %#v", adminDTO)
	}
}

func TestUserSubscriptionFromService_HistoricalSubscriptionsUseSnapshots(t *testing.T) {
	groupName := "historic group"
	limit := 100.0
	currentLimit := 50.0
	sub := &service.UserSubscription{
		ID:                    1,
		UserID:                2,
		GroupID:               3,
		StartsAt:              time.Now().Add(-48 * time.Hour),
		ExpiresAt:             time.Now().Add(-24 * time.Hour),
		Status:                service.SubscriptionStatusExpired,
		GroupNameSnapshot:     &groupName,
		DailyLimitUSDSnapshot: &limit,
		Group: &service.Group{
			ID:               3,
			Name:             "current group",
			SubscriptionType: service.SubscriptionTypeSubscription,
			DailyLimitUSD:    &currentLimit,
		},
	}

	userDTO := UserSubscriptionFromService(sub)
	if userDTO.Group == nil || userDTO.Group.Name != groupName {
		t.Fatalf("historical user subscription DTO should use snapshot group, got %#v", userDTO.Group)
	}
	if userDTO.Group.DailyLimitUSD == nil || *userDTO.Group.DailyLimitUSD != limit {
		t.Fatalf("historical user subscription DTO should use snapshot limit, got %#v", userDTO.Group)
	}
}

func TestUserSubscriptionFromServiceAdmin_RevokedSubscriptionKeepsWeeklySnapshot(t *testing.T) {
	groupName := "historic group"
	dailyLimit := 100.0
	weeklyLimit := 500.0
	currentDaily := 10.0
	currentWeekly := 20.0
	deletedAt := time.Now().Add(-time.Minute)
	sub := &service.UserSubscription{
		ID:                       1,
		UserID:                   2,
		GroupID:                  3,
		StartsAt:                 time.Now().Add(-time.Hour),
		ExpiresAt:                time.Now().Add(time.Hour),
		Status:                   service.SubscriptionStatusRevoked,
		DeletedAt:                &deletedAt,
		GroupNameSnapshot:        &groupName,
		DailyLimitUSDSnapshot:    &dailyLimit,
		WeeklyLimitUSDSnapshot:   &weeklyLimit,
		MonthlyLimitUSDSnapshot:  floatPtrForSubscriptionDTOTest(0),
		CustomLimitUSDSnapshot:   floatPtrForSubscriptionDTOTest(0),
		CustomLimitHoursSnapshot: intPtrForSubscriptionDTOTest(0),
		Group: &service.Group{
			ID:               3,
			Name:             "current group",
			SubscriptionType: service.SubscriptionTypeSubscription,
			DailyLimitUSD:    &currentDaily,
			WeeklyLimitUSD:   &currentWeekly,
		},
	}

	adminDTO := UserSubscriptionFromServiceAdmin(sub)

	if adminDTO.Group == nil || adminDTO.Group.Name != groupName {
		t.Fatalf("revoked subscription should use snapshot group, got %#v", adminDTO.Group)
	}
	if adminDTO.Group.DailyLimitUSD == nil || *adminDTO.Group.DailyLimitUSD != dailyLimit {
		t.Fatalf("revoked subscription should use daily snapshot, got %#v", adminDTO.Group)
	}
	if adminDTO.Group.WeeklyLimitUSD == nil || *adminDTO.Group.WeeklyLimitUSD != weeklyLimit {
		t.Fatalf("revoked subscription should keep weekly snapshot, got %#v", adminDTO.Group)
	}
}

func TestUserSubscriptionFromServiceAdmin_MarksOrphanHistoricalSubscriptionNotReactivatable(t *testing.T) {
	groupName := "deleted group snapshot"
	sub := &service.UserSubscription{
		ID:                1,
		UserID:            2,
		GroupID:           3,
		StartsAt:          time.Now().Add(-48 * time.Hour),
		ExpiresAt:         time.Now().Add(-24 * time.Hour),
		Status:            service.SubscriptionStatusExpired,
		GroupNameSnapshot: &groupName,
	}

	adminDTO := UserSubscriptionFromServiceAdmin(sub)

	if adminDTO.Group == nil || adminDTO.Group.Name != groupName {
		t.Fatalf("historical subscription should still display snapshot group, got %#v", adminDTO.Group)
	}
	if adminDTO.CanReactivate {
		t.Fatalf("orphan historical subscription must not be reactivatable: %#v", adminDTO)
	}
}

func TestUserSubscriptionFromServiceAdmin_MarksHistoricalSubscriptionWithActiveGroupReactivatable(t *testing.T) {
	groupName := "historic group"
	sub := &service.UserSubscription{
		ID:                1,
		UserID:            2,
		GroupID:           3,
		StartsAt:          time.Now().Add(-48 * time.Hour),
		ExpiresAt:         time.Now().Add(-24 * time.Hour),
		Status:            service.SubscriptionStatusExpired,
		GroupNameSnapshot: &groupName,
		Group: &service.Group{
			ID:               3,
			Name:             "current group",
			Status:           service.StatusActive,
			SubscriptionType: service.SubscriptionTypeSubscription,
		},
	}

	adminDTO := UserSubscriptionFromServiceAdmin(sub)

	if !adminDTO.CanReactivate {
		t.Fatalf("historical subscription with active subscription group should be reactivatable: %#v", adminDTO)
	}
}

func TestUserSubscriptionFromService_HidesAggregateInternals(t *testing.T) {
	sub := &service.UserSubscription{
		ID:                0,
		UserID:            2,
		GroupID:           3,
		StartsAt:          time.Now().Add(-time.Hour),
		ExpiresAt:         time.Now().Add(time.Hour),
		Status:            service.SubscriptionStatusActive,
		IsAggregate:       true,
		SubscriptionCount: 2,
		Group: &service.Group{
			ID:               3,
			Name:             "stacked group",
			SubscriptionType: service.SubscriptionTypeSubscription,
		},
	}

	userDTO := UserSubscriptionFromService(sub)
	if userDTO.ID == 0 {
		t.Fatalf("aggregate user subscription DTO returned id=0: %#v", userDTO)
	}
	if userDTO.IsAggregate || userDTO.SubscriptionCount != 0 {
		t.Fatalf("user subscription DTO leaked aggregate fields: %#v", userDTO)
	}

	adminDTO := UserSubscriptionFromServiceAdmin(sub)
	if !adminDTO.IsAggregate || adminDTO.SubscriptionCount != 2 {
		t.Fatalf("admin subscription DTO should preserve aggregate fields: %#v", adminDTO)
	}
}

func floatPtrForSubscriptionDTOTest(v float64) *float64 {
	return &v
}

func intPtrForSubscriptionDTOTest(v int) *int {
	return &v
}
