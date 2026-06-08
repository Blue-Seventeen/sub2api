package dto

import (
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
		},
	}

	userDTO := UserSubscriptionFromService(sub)
	if userDTO.SourceType != nil || userDTO.SourceRefID != nil || userDTO.RedeemCodeSnapshot != nil {
		t.Fatalf("user subscription DTO leaked source fields: %#v", userDTO)
	}
	if userDTO.Group == nil || userDTO.Group.Name != groupName {
		t.Fatalf("user subscription DTO should still use effective snapshotted group, got %#v", userDTO.Group)
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
