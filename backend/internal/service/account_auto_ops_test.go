package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAccountAutoOpsConfig(t *testing.T) {
	cfg := NormalizeAccountAutoOpsConfig(&AccountAutoOpsConfig{
		Enabled:         true,
		IntervalMinutes: 15,
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
