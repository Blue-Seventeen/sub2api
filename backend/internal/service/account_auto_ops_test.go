package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAccountAutoOpsConfig(t *testing.T) {
	cfg := NormalizeAccountAutoOpsConfig(&AccountAutoOpsConfig{
		Enabled:         true,
		IntervalMinutes: 15,
		TargetRules: []AccountAutoOpsTargetRule{
			{
				Name:     "  target rule  ",
				Action:   AccountAutoOpsTargetActionTakeover,
				Priority: 0,
				Conditions: []AccountAutoOpsTargetCondition{
					{Field: AccountAutoOpsTargetFieldAccountStatus, Operator: AccountAutoOpsTargetOperatorEQ, Value: " error "},
				},
			},
		},
		Rules: []AccountAutoOpsRule{
			{
				ID:        "",
				Name:      "  rule one  ",
				Subjects:  []string{AccountAutoOpsSubjectTestResponse, AccountAutoOpsSubjectTestResponse, " account_name "},
				Priority:  0,
				MatchType: AccountAutoOpsMatchContains,
				Pattern:   " deactivated ",
				Action:    AccountAutoOpsActionDisableSchedulable,
			},
		},
		TestModelsByPlatform: map[string][]string{
			PlatformOpenAI: {"gpt-4o", "gpt-4o", "  ", "gpt-4.1"},
		},
	})

	require.True(t, cfg.Enabled)
	require.Equal(t, 15, cfg.IntervalMinutes)
	require.Len(t, cfg.TargetRules, 1)
	require.True(t, cfg.TargetRulesInitialized)
	require.NotEmpty(t, cfg.TargetRules[0].ID)
	require.Equal(t, "target rule", cfg.TargetRules[0].Name)
	require.Equal(t, 10, cfg.TargetRules[0].Priority)
	require.Equal(t, "error", cfg.TargetRules[0].Conditions[0].Value)
	require.Len(t, cfg.Rules, 1)
	require.NotEmpty(t, cfg.Rules[0].ID)
	require.Equal(t, "rule one", cfg.Rules[0].Name)
	require.Equal(t, AccountAutoOpsSubjectTestResponse, cfg.Rules[0].Subject)
	require.Equal(t, 10, cfg.Rules[0].Priority)
	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, cfg.TestModelsByPlatform[PlatformOpenAI])
	require.Contains(t, cfg.TestModelsByPlatform, PlatformAnthropic)
}

func TestValidateAccountAutoOpsConfig(t *testing.T) {
	valid := &AccountAutoOpsConfig{
		Enabled:         true,
		IntervalMinutes: 10,
		TargetRules: []AccountAutoOpsTargetRule{
			{
				ID:       "target_1",
				Name:     "Target",
				Priority: 10,
				Action:   AccountAutoOpsTargetActionTakeover,
				Conditions: []AccountAutoOpsTargetCondition{
					{Field: AccountAutoOpsTargetFieldAccountStatus, Operator: AccountAutoOpsTargetOperatorEQ, Value: AccountAutoOpsTargetStatusError},
				},
			},
		},
		Rules: []AccountAutoOpsRule{
			{
				ID:        "rule_1",
				Name:      "Account name match",
				Subject:   AccountAutoOpsSubjectAccountName,
				Priority:  10,
				MatchType: AccountAutoOpsMatchContains,
				Pattern:   "test",
				Action:    AccountAutoOpsActionRecoverState,
			},
		},
	}
	require.NoError(t, ValidateAccountAutoOpsConfig(valid))

	invalid := &AccountAutoOpsConfig{
		IntervalMinutes: 0,
		TargetRules: []AccountAutoOpsTargetRule{
			{
				Name:     "",
				Priority: 0,
				Action:   "bad",
				Conditions: []AccountAutoOpsTargetCondition{
					{Field: "bad", Operator: "bad", Value: ""},
				},
			},
		},
		Rules: []AccountAutoOpsRule{
			{
				Name:      "",
				Subject:   "unknown",
				Priority:  0,
				MatchType: "bad",
				Pattern:   "",
				Action:    "bad",
			},
		},
	}
	require.Error(t, ValidateAccountAutoOpsConfig(invalid))
}

func TestMatchAccountAutoOpsTargetRule(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	lastUsed := now.Add(-9 * 24 * time.Hour)
	account := &Account{
		ID:                     1,
		Name:                   "运维-openai",
		Platform:               PlatformOpenAI,
		Type:                   AccountTypeOAuth,
		Status:                 StatusActive,
		Schedulable:            false,
		LastUsedAt:             &lastUsed,
		GroupIDs:               []int64{88},
		TempUnschedulableUntil: nil,
	}
	rateLimitedUntil := now.Add(2 * time.Hour)
	account.RateLimitResetAt = &rateLimitedUntil

	rule := AccountAutoOpsTargetRule{
		ID:       "target_1",
		Name:     "target",
		Priority: 10,
		Action:   AccountAutoOpsTargetActionTakeover,
		Conditions: []AccountAutoOpsTargetCondition{
			{Field: AccountAutoOpsTargetFieldAccountName, Operator: AccountAutoOpsTargetOperatorContains, Value: "运维"},
			{Field: AccountAutoOpsTargetFieldSchedulable, Operator: AccountAutoOpsTargetOperatorEQ, Value: "false"},
			{Field: AccountAutoOpsTargetFieldPlatform, Operator: AccountAutoOpsTargetOperatorEQ, Value: PlatformOpenAI},
			{Field: AccountAutoOpsTargetFieldAuthType, Operator: AccountAutoOpsTargetOperatorEQ, Value: AccountTypeOAuth},
			{Field: AccountAutoOpsTargetFieldAccountStatus, Operator: AccountAutoOpsTargetOperatorEQ, Value: AccountAutoOpsTargetStatusRateLimited},
			{Field: AccountAutoOpsTargetFieldGroup, Operator: AccountAutoOpsTargetOperatorEQ, Value: "88"},
			{Field: AccountAutoOpsTargetFieldLastUsedDays, Operator: AccountAutoOpsTargetOperatorEQ, Value: "8"},
		},
	}
	require.True(t, MatchAccountAutoOpsTargetRule(rule, account, now))

	rule.Conditions[5] = AccountAutoOpsTargetCondition{Field: AccountAutoOpsTargetFieldGroup, Operator: AccountAutoOpsTargetOperatorNEQ, Value: "88"}
	require.False(t, MatchAccountAutoOpsTargetRule(rule, account, now))
}

func TestMatchAccountAutoOpsTargetRule_LastUsedDaysNilMatchesEq(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	account := &Account{
		ID:          2,
		Name:        "manual-account",
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusError,
		Schedulable: true,
	}
	eqRule := AccountAutoOpsTargetRule{
		ID:       "target_eq",
		Name:     "eq",
		Priority: 10,
		Action:   AccountAutoOpsTargetActionTakeover,
		Conditions: []AccountAutoOpsTargetCondition{
			{Field: AccountAutoOpsTargetFieldLastUsedDays, Operator: AccountAutoOpsTargetOperatorEQ, Value: "8"},
		},
	}
	neqRule := AccountAutoOpsTargetRule{
		ID:       "target_neq",
		Name:     "neq",
		Priority: 20,
		Action:   AccountAutoOpsTargetActionManual,
		Conditions: []AccountAutoOpsTargetCondition{
			{Field: AccountAutoOpsTargetFieldLastUsedDays, Operator: AccountAutoOpsTargetOperatorNEQ, Value: "8"},
		},
	}
	require.True(t, MatchAccountAutoOpsTargetRule(eqRule, account, now))
	require.False(t, MatchAccountAutoOpsTargetRule(neqRule, account, now))
}

func TestWithMigratedLegacyAccountAutoOpsTargetRules(t *testing.T) {
	cfg := WithMigratedLegacyAccountAutoOpsTargetRules(&AccountAutoOpsConfig{
		Enabled:                true,
		IntervalMinutes:        10,
		TargetRules:            nil,
		TargetRulesInitialized: false,
		Rules: []AccountAutoOpsRule{
			{
				ID:        "rule_1",
				Name:      "Account name match",
				Subject:   AccountAutoOpsSubjectAccountName,
				Priority:  10,
				MatchType: AccountAutoOpsMatchContains,
				Pattern:   "test",
				Action:    AccountAutoOpsActionRecoverState,
			},
		},
	})
	require.Len(t, cfg.TargetRules, 1)
	require.True(t, cfg.TargetRulesInitialized)
	require.Equal(t, accountAutoOpsLegacyTargetRuleID, cfg.TargetRules[0].ID)
	require.Equal(t, AccountAutoOpsTargetActionTakeover, cfg.TargetRules[0].Action)
	require.Equal(t, AccountAutoOpsTargetFieldAccountStatus, cfg.TargetRules[0].Conditions[0].Field)
	require.Equal(t, AccountAutoOpsTargetFieldSchedulable, cfg.TargetRules[0].Conditions[1].Field)
}

type accountAutoOpsRepoStub struct {
	AccountRepository
	listItems     []Account
	selectedItems []*Account
}

func (s *accountAutoOpsRepoStub) List(_ context.Context, _ pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return s.listItems, &pagination.PaginationResult{Total: int64(len(s.listItems)), Page: 1, PageSize: len(s.listItems), Pages: 1}, nil
}

func (s *accountAutoOpsRepoStub) GetByIDs(_ context.Context, _ []int64) ([]*Account, error) {
	return s.selectedItems, nil
}

func TestAccountAutoOpsServiceFilterTargetRulesForManualAndAutomatic(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)
	older := now.Add(-9 * 24 * time.Hour)
	matching := Account{ID: 1, Name: "ops-openai", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError, Schedulable: true, LastUsedAt: &older}
	manual := Account{ID: 2, Name: "manual-openai", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError, Schedulable: false}
	repo := &accountAutoOpsRepoStub{
		listItems:     []Account{matching, manual},
		selectedItems: []*Account{&matching, &manual},
	}
	svc := NewAccountAutoOpsService(nil, repo, nil, nil, nil, nil, nil, nil)
	cfg := &AccountAutoOpsConfig{
		TargetRules: []AccountAutoOpsTargetRule{
			{
				ID:       "takeover",
				Name:     "takeover",
				Priority: 10,
				Action:   AccountAutoOpsTargetActionTakeover,
				Conditions: []AccountAutoOpsTargetCondition{
					{Field: AccountAutoOpsTargetFieldAccountStatus, Operator: AccountAutoOpsTargetOperatorEQ, Value: AccountAutoOpsTargetStatusError},
					{Field: AccountAutoOpsTargetFieldSchedulable, Operator: AccountAutoOpsTargetOperatorEQ, Value: "true"},
				},
			},
			{
				ID:       "manual",
				Name:     "manual",
				Priority: 20,
				Action:   AccountAutoOpsTargetActionManual,
				Conditions: []AccountAutoOpsTargetCondition{
					{Field: AccountAutoOpsTargetFieldAccountStatus, Operator: AccountAutoOpsTargetOperatorEQ, Value: AccountAutoOpsTargetStatusError},
				},
			},
		},
	}
	filteredAuto := svc.filterAccountsByTargetRules([]*Account{&matching, &manual}, cfg, now)
	require.Len(t, filteredAuto, 1)
	require.Equal(t, int64(1), filteredAuto[0].ID)
}

func TestMatchAccountAutoOpsRule(t *testing.T) {
	rule := AccountAutoOpsRule{
		ID:        "rule_1",
		Name:      "test",
		Subject:   AccountAutoOpsSubjectTestResponse,
		Priority:  10,
		MatchType: AccountAutoOpsMatchContains,
		Pattern:   "deactivated",
		Action:    AccountAutoOpsActionDisableSchedulable,
	}
	require.True(t, MatchAccountAutoOpsRule(rule, AccountAutoOpsSubjectTestResponse, `{"error":"deactivated workspace"}`))
	require.False(t, MatchAccountAutoOpsRule(rule, AccountAutoOpsSubjectRefreshResponse, `{"error":"deactivated workspace"}`))

	notContains := rule
	notContains.MatchType = AccountAutoOpsMatchNotContains
	require.True(t, MatchAccountAutoOpsRule(notContains, AccountAutoOpsSubjectTestResponse, `{"error":"token invalidated"}`))
	require.False(t, MatchAccountAutoOpsRule(notContains, AccountAutoOpsSubjectTestResponse, `{"error":"deactivated workspace"}`))
}

func TestMatchAccountAutoOpsRule_StrictEnglishBoundary(t *testing.T) {
	rule := AccountAutoOpsRule{
		ID:        "rule_hi",
		Name:      "Hi",
		Subject:   AccountAutoOpsSubjectTestResponse,
		Priority:  10,
		MatchType: AccountAutoOpsMatchContains,
		Pattern:   "Hi",
		Action:    AccountAutoOpsActionRecoverState,
	}
	require.True(t, MatchAccountAutoOpsRule(rule, AccountAutoOpsSubjectTestResponse, `{"response_text":"Hi! What can I do for you?"}`))
	require.False(t, MatchAccountAutoOpsRule(rule, AccountAutoOpsSubjectTestResponse, `{"error_message":"this account is deactivated"}`))
}

func TestMatchAccountAutoOpsRule_StrictASCIIJSONToken(t *testing.T) {
	rule := AccountAutoOpsRule{
		ID:        "rule_token_expired",
		Name:      "token_expired",
		Subject:   AccountAutoOpsSubjectRefreshResponse,
		Priority:  10,
		MatchType: AccountAutoOpsMatchContains,
		Pattern:   "token_expired",
		Action:    AccountAutoOpsActionDeleteAccount,
	}
	require.True(t, MatchAccountAutoOpsRule(rule, AccountAutoOpsSubjectRefreshResponse, `{"code":"token_expired"}`))
	require.False(t, MatchAccountAutoOpsRule(rule, AccountAutoOpsSubjectRefreshResponse, `{"code":"mytoken_expired_backup"}`))
}

func TestMatchAccountAutoOpsRule_ChineseStillSubstring(t *testing.T) {
	rule := AccountAutoOpsRule{
		ID:        "rule_chinese",
		Name:      "中文",
		Subject:   AccountAutoOpsSubjectTestResponse,
		Priority:  10,
		MatchType: AccountAutoOpsMatchContains,
		Pattern:   "停用",
		Action:    AccountAutoOpsActionDisableSchedulable,
	}
	require.True(t, MatchAccountAutoOpsRule(rule, AccountAutoOpsSubjectTestResponse, `账号已经停用，请联系管理员`))
}

func TestAccountAutoOpsLoopGuard(t *testing.T) {
	guard := newAccountAutoOpsLoopGuard()
	for i := 0; i < accountAutoOpsLoopGuardMaxRepeats; i++ {
		require.True(t, guard.Record(AccountAutoOpsSubjectTestResponse, AccountAutoOpsActionRefreshToken, "hash-1"))
	}
	require.False(t, guard.Record(AccountAutoOpsSubjectTestResponse, AccountAutoOpsActionRefreshToken, "hash-1"))
}

func TestAccountAutoOpsServiceRunManualRequiresSavedConfig(t *testing.T) {
	svc := NewAccountAutoOpsService(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := svc.RunManual(context.Background(), []int64{1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "config")
}
