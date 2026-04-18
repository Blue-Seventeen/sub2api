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
	require.Equal(t, []string{AccountAutoOpsSubjectAccountName, AccountAutoOpsSubjectTestResponse}, cfg.Rules[0].Subjects)
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
				Subjects:  []string{AccountAutoOpsSubjectAccountName},
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
				Subjects:  []string{"unknown"},
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
		Subjects:  []string{AccountAutoOpsSubjectTestResponse},
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
