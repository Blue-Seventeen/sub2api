package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountAutoOpsSettingRepoStub struct {
	SettingRepository
	values map[string]string
}

func (s *accountAutoOpsSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if s.values == nil {
		s.values = map[string]string{}
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *accountAutoOpsSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func TestSettingServiceGetAccountAutoOpsConfigMigratesLegacyTargetRules(t *testing.T) {
	repo := &accountAutoOpsSettingRepoStub{values: map[string]string{}}
	legacyCfg := &AccountAutoOpsConfig{
		Enabled:         true,
		IntervalMinutes: 10,
		Rules: []AccountAutoOpsRule{
			{
				ID:        "rule_1",
				Name:      "recover",
				Subject:   AccountAutoOpsSubjectTestResponse,
				Priority:  10,
				MatchType: AccountAutoOpsMatchContains,
				Pattern:   "token_expired",
				Action:    AccountAutoOpsActionRecoverState,
			},
		},
	}
	raw, err := json.Marshal(legacyCfg)
	require.NoError(t, err)
	repo.values[SettingKeyAccountAutoOpsConfig] = string(raw)

	svc := NewSettingService(repo, nil)
	cfg, configured, err := svc.GetAccountAutoOpsConfig(context.Background())
	require.NoError(t, err)
	require.True(t, configured)
	require.True(t, cfg.TargetRulesInitialized)
	require.Len(t, cfg.TargetRules, 1)
	require.Equal(t, accountAutoOpsLegacyTargetRuleID, cfg.TargetRules[0].ID)

	persisted := &AccountAutoOpsConfig{}
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyAccountAutoOpsConfig]), persisted))
	require.True(t, persisted.TargetRulesInitialized)
	require.Len(t, persisted.TargetRules, 1)
}

func TestSettingServiceSetAccountAutoOpsConfigMarksTargetRulesInitialized(t *testing.T) {
	repo := &accountAutoOpsSettingRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, nil)

	err := svc.SetAccountAutoOpsConfig(context.Background(), &AccountAutoOpsConfig{
		Enabled:                true,
		IntervalMinutes:        10,
		TargetRulesInitialized: false,
		TargetRules:            []AccountAutoOpsTargetRule{},
	})
	require.NoError(t, err)

	persisted := &AccountAutoOpsConfig{}
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyAccountAutoOpsConfig]), persisted))
	require.True(t, persisted.TargetRulesInitialized)
	require.Empty(t, persisted.TargetRules)
}

func TestAccountAutoOpsServiceRunManualAndAutomaticRespectTargetRules(t *testing.T) {
	repo := &accountAutoOpsSettingRepoStub{values: map[string]string{}}
	settingSvc := NewSettingService(repo, nil)
	cfg := &AccountAutoOpsConfig{
		Enabled:                true,
		IntervalMinutes:        10,
		TargetRulesInitialized: true,
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
	require.NoError(t, settingSvc.SetAccountAutoOpsConfig(context.Background(), cfg))

	lastUsed := time.Now().Add(-9 * 24 * time.Hour)
	matching := Account{ID: 1, Name: "ops-a", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError, Schedulable: true, LastUsedAt: &lastUsed}
	manual := Account{ID: 2, Name: "ops-b", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError, Schedulable: false}
	accountRepo := &accountAutoOpsRepoStub{
		listItems:     []Account{matching, manual},
		selectedItems: []*Account{&matching, &manual},
	}

	svc := NewAccountAutoOpsService(settingSvc, accountRepo, nil, nil, nil, nil, nil, nil)

	manualResult, err := svc.RunManual(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.Equal(t, 1, manualResult.EligibleAccounts)

	run, err := svc.RunAutomatic(context.Background())
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, 1, run.EligibleAccounts)

	cfg.TargetRules[0].Action = AccountAutoOpsTargetActionManual
	require.NoError(t, settingSvc.SetAccountAutoOpsConfig(context.Background(), cfg))
	run, err = svc.RunAutomatic(context.Background())
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, 0, run.EligibleAccounts)
	require.Equal(t, AccountAutoOpsRunStatusCompleted, run.Status)
}
