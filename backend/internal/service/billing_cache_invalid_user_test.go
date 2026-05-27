package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestCheckBillingEligibility_NonSubscriptionInvalidUserReturnsError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	s := &BillingCacheService{cfg: cfg}

	cases := []struct {
		name string
		user *User
	}{
		{name: "nil_user", user: nil},
		{name: "zero_user_id", user: &User{ID: 0}},
		{name: "negative_user_id", user: &User{ID: -1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.CheckBillingEligibility(
				context.Background(),
				tc.user,
				nil,
				&Group{ID: 20, RateMultiplier: 0},
				nil,
				"openai",
			)
			if !errors.Is(err, ErrBillingUserInvalid) {
				t.Fatalf("expected ErrBillingUserInvalid, got %v", err)
			}
		})
	}
}
