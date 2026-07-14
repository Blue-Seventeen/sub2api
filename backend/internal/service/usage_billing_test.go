package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestUsageBillingCommandMatchesLegacyFingerprintForZeroQuantities(t *testing.T) {
	subscriptionID := int64(9)
	cmd := &UsageBillingCommand{
		APIKeyID:                3,
		RequestPayloadHash:      "payload-hash",
		UserID:                  1,
		AccountID:               2,
		SubscriptionID:          &subscriptionID,
		AccountType:             " apikey ",
		Model:                   " asr-model ",
		ServiceTier:             " flex ",
		ReasoningEffort:         " medium ",
		BillingType:             BillingTypeBalance,
		InputTokens:             11,
		OutputTokens:            7,
		CacheCreationTokens:     5,
		CacheReadTokens:         3,
		ImageCount:              0,
		MediaType:               " audio ",
		BalanceCost:             0.12,
		SubscriptionCost:        0.34,
		APIKeyQuotaCost:         0.56,
		APIKeyRateLimitCost:     0.78,
		AccountQuotaCost:        0.91,
		RequestFingerprint:      "",
		BillableDurationSeconds: 0,
		BillableCharacterCount:  0,
	}
	cmd.Normalize()
	legacyFingerprint := legacyUsageBillingFingerprintForTest(cmd)

	if !cmd.MatchesRequestFingerprint(legacyFingerprint) {
		t.Fatalf("expected zero-quantity command to match legacy fingerprint")
	}
	if cmd.RequestFingerprint == legacyFingerprint {
		t.Fatalf("new fingerprint should remain version-distinct from legacy fingerprint")
	}
}

func TestUsageBillingCommandDoesNotMatchLegacyFingerprintForNonZeroQuantities(t *testing.T) {
	cmd := &UsageBillingCommand{
		APIKeyID:                3,
		UserID:                  1,
		AccountID:               2,
		AccountType:             AccountTypeAPIKey,
		Model:                   "asr-model",
		BillingType:             BillingTypeBalance,
		BalanceCost:             0.12,
		BillableDurationSeconds: 6,
		BillableCharacterCount:  0,
	}
	cmd.Normalize()
	withoutQuantities := *cmd
	withoutQuantities.BillableDurationSeconds = 0
	legacyFingerprint := legacyUsageBillingFingerprintForTest(&withoutQuantities)

	if cmd.MatchesRequestFingerprint(legacyFingerprint) {
		t.Fatalf("non-zero duration/character quantities must not match legacy fingerprint")
	}
}

func TestBuildUsageBillingCommandFingerprintsQuantitiesOnlyForQuantityModes(t *testing.T) {
	perRequestMode := string(BillingModePerRequest)
	durationMode := string(BillingModeDuration)
	characterMode := string(BillingModeCharacter)
	params := &postUsageBillingParams{
		Cost:    &CostBreakdown{ActualCost: 0.12},
		APIKey:  &APIKey{ID: 3},
		User:    &User{ID: 1},
		Account: &Account{ID: 2, Type: AccountTypeAPIKey},
	}

	perRequestCmd := buildUsageBillingCommand("req-1", &UsageLog{
		Model:                   "asr-model",
		BillingType:             BillingTypeBalance,
		BillingMode:             &perRequestMode,
		BillableDurationSeconds: 9,
		BillableCharacterCount:  1200,
	}, params)
	if perRequestCmd.BillableDurationSeconds != 0 || perRequestCmd.BillableCharacterCount != 0 {
		t.Fatalf("per_request command quantities = %d/%d, want 0/0",
			perRequestCmd.BillableDurationSeconds,
			perRequestCmd.BillableCharacterCount,
		)
	}

	durationCmd := buildUsageBillingCommand("req-2", &UsageLog{
		Model:                   "asr-model",
		BillingType:             BillingTypeBalance,
		BillingMode:             &durationMode,
		BillableDurationSeconds: 9,
		BillableCharacterCount:  1200,
	}, params)
	if durationCmd.BillableDurationSeconds != 9 || durationCmd.BillableCharacterCount != 0 {
		t.Fatalf("duration command quantities = %d/%d, want 9/0",
			durationCmd.BillableDurationSeconds,
			durationCmd.BillableCharacterCount,
		)
	}

	characterCmd := buildUsageBillingCommand("req-3", &UsageLog{
		Model:                   "tts-model",
		BillingType:             BillingTypeBalance,
		BillingMode:             &characterMode,
		BillableDurationSeconds: 9,
		BillableCharacterCount:  1200,
	}, params)
	if characterCmd.BillableDurationSeconds != 0 || characterCmd.BillableCharacterCount != 1200 {
		t.Fatalf("character command quantities = %d/%d, want 0/1200",
			characterCmd.BillableDurationSeconds,
			characterCmd.BillableCharacterCount,
		)
	}
}

func TestBuildUsageBillingCommand_BalanceUsesRealActualCostOnly(t *testing.T) {
	params := &postUsageBillingParams{
		Cost:          &CostBreakdown{ActualCost: 0.25, RealActualCost: 0},
		APIKey:        &APIKey{ID: 3, Quota: 10},
		User:          &User{ID: 1},
		Account:       &Account{ID: 2, Type: AccountTypeAPIKey},
		APIKeyService: usageBillingAPIKeyQuotaStub{},
	}

	cmd := buildUsageBillingCommand("req-zero-real", &UsageLog{
		Model:       "image-model",
		BillingType: BillingTypeBalance,
	}, params)
	if cmd.BalanceCost != 0 {
		t.Fatalf("BalanceCost = %v, want 0 when RealActualCost is an explicit zero", cmd.BalanceCost)
	}
	if cmd.APIKeyQuotaCost != 0.25 {
		t.Fatalf("APIKeyQuotaCost = %v, want ActualCost to remain the quota cost", cmd.APIKeyQuotaCost)
	}

	params.Cost.RealActualCost = 0.12
	cmd = buildUsageBillingCommand("req-real", nil, params)
	if cmd.BalanceCost != 0.12 {
		t.Fatalf("BalanceCost = %v, want RealActualCost", cmd.BalanceCost)
	}
}

type usageBillingAPIKeyQuotaStub struct{}

func (usageBillingAPIKeyQuotaStub) UpdateQuotaUsed(context.Context, int64, float64) error {
	return nil
}

func (usageBillingAPIKeyQuotaStub) UpdateRateLimitUsage(context.Context, int64, float64) error {
	return nil
}

func TestPostUsageBilling_FlusherModeFallsBackToDBWhenQuotaCacheNotSynced(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.UserPlatformQuotaFlusherEnabled = true
	repo := &usageBillingPlatformQuotaRepo{}
	p := &postUsageBillingParams{
		Cost:     &CostBreakdown{ActualCost: 0.75},
		User:     &User{ID: 11},
		APIKey:   &APIKey{ID: 22},
		Account:  &Account{ID: 33},
		Platform: "openai",
	}
	deps := &billingDeps{
		billingCacheService:   &BillingCacheService{cfg: cfg},
		userPlatformQuotaRepo: repo,
		cfg:                   cfg,
	}

	postUsageBilling(context.Background(), p, deps)

	if repo.calls != 1 {
		t.Fatalf("IncrementUsageWithReset calls = %d, want 1", repo.calls)
	}
	if repo.userID != 11 || repo.platform != "openai" || repo.cost != 0.75 {
		t.Fatalf("fallback args = user:%d platform:%q cost:%f, want user:11 platform:openai cost:0.75",
			repo.userID, repo.platform, repo.cost)
	}
}

type usageBillingPlatformQuotaRepo struct {
	UserPlatformQuotaRepository

	calls    int
	userID   int64
	platform string
	cost     float64
}

func (r *usageBillingPlatformQuotaRepo) GetByUserPlatform(context.Context, int64, string) (*UserPlatformQuotaRecord, error) {
	return nil, nil
}

func (r *usageBillingPlatformQuotaRepo) BulkInsertInitial(context.Context, []UserPlatformQuotaRecord) error {
	return nil
}

func (r *usageBillingPlatformQuotaRepo) IncrementUsageWithReset(_ context.Context, userID int64, platform string, cost float64, _ time.Time) error {
	r.calls++
	r.userID = userID
	r.platform = platform
	r.cost = cost
	return nil
}

func (r *usageBillingPlatformQuotaRepo) ListByUser(context.Context, int64) ([]UserPlatformQuotaRecord, error) {
	return nil, nil
}

func (r *usageBillingPlatformQuotaRepo) UpsertForUser(context.Context, int64, []UserPlatformQuotaRecord) error {
	return nil
}

func (r *usageBillingPlatformQuotaRepo) ResetExpiredWindow(context.Context, int64, string, string, time.Time) error {
	return nil
}

func (r *usageBillingPlatformQuotaRepo) BatchSnapshotUsage(context.Context, []UserPlatformQuotaSnapshot, time.Time) error {
	return nil
}

func legacyUsageBillingFingerprintForTest(c *UsageBillingCommand) string {
	raw := fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%s|%d|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f",
		c.UserID,
		c.AccountID,
		c.APIKeyID,
		strings.TrimSpace(c.AccountType),
		strings.TrimSpace(c.Model),
		strings.TrimSpace(c.ServiceTier),
		strings.TrimSpace(c.ReasoningEffort),
		c.BillingType,
		c.InputTokens,
		c.OutputTokens,
		c.CacheCreationTokens,
		c.CacheReadTokens,
		c.ImageCount,
		strings.TrimSpace(c.MediaType),
		valueOrZero(c.SubscriptionID),
		c.BalanceCost,
		c.SubscriptionCost,
		c.APIKeyQuotaCost,
		c.APIKeyRateLimitCost,
		c.AccountQuotaCost,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
