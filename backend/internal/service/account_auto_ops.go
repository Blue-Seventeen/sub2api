package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	AccountAutoOpsTriggerAutomatic = "automatic"
	AccountAutoOpsTriggerManual    = "manual"

	AccountAutoOpsRunStatusRunning   = "running"
	AccountAutoOpsRunStatusCompleted = "completed"
	AccountAutoOpsRunStatusFailed    = "failed"

	AccountAutoOpsSubjectAccountName     = "account_name"
	AccountAutoOpsSubjectTestResponse    = "test_response"
	AccountAutoOpsSubjectRefreshResponse = "refresh_response"

	AccountAutoOpsMatchContains    = "contains"
	AccountAutoOpsMatchNotContains = "not_contains"

	AccountAutoOpsActionRetest             = "retest"
	AccountAutoOpsActionRefreshToken       = "refresh_token"
	AccountAutoOpsActionRecoverState       = "recover_state"
	AccountAutoOpsActionEnableSchedulable  = "enable_schedulable"
	AccountAutoOpsActionDisableSchedulable = "disable_schedulable"
	AccountAutoOpsActionDeleteAccount      = "delete_account"

	AccountAutoOpsTargetFieldAccountName   = "account_name"
	AccountAutoOpsTargetFieldSchedulable   = "schedulable"
	AccountAutoOpsTargetFieldPlatform      = "platform"
	AccountAutoOpsTargetFieldAuthType      = "auth_type"
	AccountAutoOpsTargetFieldAccountStatus = "account_status"
	AccountAutoOpsTargetFieldGroup         = "group"
	AccountAutoOpsTargetFieldLastUsedDays  = "last_used_days"

	AccountAutoOpsTargetOperatorEQ          = "eq"
	AccountAutoOpsTargetOperatorNEQ         = "neq"
	AccountAutoOpsTargetOperatorContains    = "contains"
	AccountAutoOpsTargetOperatorNotContains = "not_contains"

	AccountAutoOpsTargetActionTakeover = "takeover"
	AccountAutoOpsTargetActionManual   = "manual"

	AccountAutoOpsTargetStatusNormal            = "normal"
	AccountAutoOpsTargetStatusRateLimited       = "rate_limited"
	AccountAutoOpsTargetStatusError             = "error"
	AccountAutoOpsTargetStatusPaused            = "paused"
	AccountAutoOpsTargetStatusTempUnschedulable = "temp_unschedulable"

	AccountAutoOpsStepStatusMatched          = "matched"
	AccountAutoOpsStepStatusNoRuleMatched    = "no_rule_matched"
	AccountAutoOpsStepStatusActionExecuted   = "action_executed"
	AccountAutoOpsStepStatusActionFailed     = "action_failed"
	AccountAutoOpsStepStatusLoopGuardStopped = "loop_guard_stopped"
	AccountAutoOpsStepStatusSkipped          = "skipped"

	accountAutoOpsDefaultIntervalMinutes = 10
	accountAutoOpsLogRetention           = 24 * time.Hour
	accountAutoOpsDefaultLogLimit        = 20
	accountAutoOpsDefaultSampleLimit     = 20
	accountAutoOpsResponsePreviewLimit   = 8192
	accountAutoOpsResponseSampleLimit    = 4096
	accountAutoOpsLoopGuardMaxRepeats    = 3
	accountAutoOpsLoopGuardMaxSteps      = 1000
	accountAutoOpsRunLockKey             = "account:auto_ops:run_lock"
	accountAutoOpsRunLockTTL             = 30 * time.Minute
	accountAutoOpsTargetGroupUngrouped   = "ungrouped"
	accountAutoOpsLegacyTargetRuleID     = "legacy_default_takeover"
	accountAutoOpsLegacyTargetRuleName   = "Legacy Auto Ops Target / 旧版自动运维对象"
)

var (
	accountAutoOpsSupportedSubjects = map[string]struct{}{
		AccountAutoOpsSubjectAccountName:     {},
		AccountAutoOpsSubjectTestResponse:    {},
		AccountAutoOpsSubjectRefreshResponse: {},
	}
	accountAutoOpsSupportedMatchTypes = map[string]struct{}{
		AccountAutoOpsMatchContains:    {},
		AccountAutoOpsMatchNotContains: {},
	}
	accountAutoOpsSupportedActions = map[string]struct{}{
		AccountAutoOpsActionRetest:             {},
		AccountAutoOpsActionRefreshToken:       {},
		AccountAutoOpsActionRecoverState:       {},
		AccountAutoOpsActionEnableSchedulable:  {},
		AccountAutoOpsActionDisableSchedulable: {},
		AccountAutoOpsActionDeleteAccount:      {},
	}
	accountAutoOpsSupportedTargetFields = map[string]struct{}{
		AccountAutoOpsTargetFieldAccountName:   {},
		AccountAutoOpsTargetFieldSchedulable:   {},
		AccountAutoOpsTargetFieldPlatform:      {},
		AccountAutoOpsTargetFieldAuthType:      {},
		AccountAutoOpsTargetFieldAccountStatus: {},
		AccountAutoOpsTargetFieldGroup:         {},
		AccountAutoOpsTargetFieldLastUsedDays:  {},
	}
	accountAutoOpsSupportedTargetActions = map[string]struct{}{
		AccountAutoOpsTargetActionTakeover: {},
		AccountAutoOpsTargetActionManual:   {},
	}
	accountAutoOpsSupportedTargetStatuses = map[string]struct{}{
		AccountAutoOpsTargetStatusNormal:            {},
		AccountAutoOpsTargetStatusRateLimited:       {},
		AccountAutoOpsTargetStatusError:             {},
		AccountAutoOpsTargetStatusPaused:            {},
		AccountAutoOpsTargetStatusTempUnschedulable: {},
	}
	accountAutoOpsSupportedTargetPlatforms = map[string]struct{}{
		PlatformAnthropic:   {},
		PlatformOpenAI:      {},
		PlatformGemini:      {},
		PlatformAntigravity: {},
	}
	accountAutoOpsSupportedTargetAuthTypes = map[string]struct{}{
		AccountTypeOAuth:      {},
		AccountTypeSetupToken: {},
		AccountTypeAPIKey:     {},
		AccountTypeBedrock:    {},
		AccountTypeUpstream:   {},
	}
	accountAutoOpsTargetOperatorsByField = map[string]map[string]struct{}{
		AccountAutoOpsTargetFieldAccountName: {
			AccountAutoOpsTargetOperatorEQ:          {},
			AccountAutoOpsTargetOperatorNEQ:         {},
			AccountAutoOpsTargetOperatorContains:    {},
			AccountAutoOpsTargetOperatorNotContains: {},
		},
		AccountAutoOpsTargetFieldSchedulable: {
			AccountAutoOpsTargetOperatorEQ:  {},
			AccountAutoOpsTargetOperatorNEQ: {},
		},
		AccountAutoOpsTargetFieldPlatform: {
			AccountAutoOpsTargetOperatorEQ:  {},
			AccountAutoOpsTargetOperatorNEQ: {},
		},
		AccountAutoOpsTargetFieldAuthType: {
			AccountAutoOpsTargetOperatorEQ:  {},
			AccountAutoOpsTargetOperatorNEQ: {},
		},
		AccountAutoOpsTargetFieldAccountStatus: {
			AccountAutoOpsTargetOperatorEQ:  {},
			AccountAutoOpsTargetOperatorNEQ: {},
		},
		AccountAutoOpsTargetFieldGroup: {
			AccountAutoOpsTargetOperatorEQ:  {},
			AccountAutoOpsTargetOperatorNEQ: {},
		},
		AccountAutoOpsTargetFieldLastUsedDays: {
			AccountAutoOpsTargetOperatorEQ:  {},
			AccountAutoOpsTargetOperatorNEQ: {},
		},
	}
)

type AccountAutoOpsRule struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Subject   string   `json:"subject"`
	Priority  int      `json:"priority"`
	MatchType string   `json:"match_type"`
	Pattern   string   `json:"pattern"`
	Action    string   `json:"action"`
	Subjects  []string `json:"subjects,omitempty"` // legacy compatibility: read old configs only
}

type AccountAutoOpsTargetCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type AccountAutoOpsTargetRule struct {
	ID         string                          `json:"id"`
	Name       string                          `json:"name"`
	Priority   int                             `json:"priority"`
	Action     string                          `json:"action"`
	Conditions []AccountAutoOpsTargetCondition `json:"conditions"`
}

type AccountAutoOpsConfig struct {
	Enabled                bool                       `json:"enabled"`
	IntervalMinutes        int                        `json:"interval_minutes"`
	TargetRules            []AccountAutoOpsTargetRule `json:"target_rules"`
	TargetRulesInitialized bool                       `json:"target_rules_initialized,omitempty"`
	Rules                  []AccountAutoOpsRule       `json:"rules"`
	TestModelsByPlatform   map[string][]string        `json:"test_models_by_platform"`
	Configured             bool                       `json:"configured,omitempty"`
}

type AccountAutoOpsRun struct {
	ID                  int64                 `json:"id"`
	TriggerMode         string                `json:"trigger_mode"`
	Status              string                `json:"status"`
	RequestedAccountIDs []int64               `json:"requested_account_ids"`
	TotalAccounts       int                   `json:"total_accounts"`
	EligibleAccounts    int                   `json:"eligible_accounts"`
	CompletedAccounts   int                   `json:"completed_accounts"`
	ErrorMessage        string                `json:"error_message"`
	StartedAt           time.Time             `json:"started_at"`
	FinishedAt          *time.Time            `json:"finished_at"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	Steps               []*AccountAutoOpsStep `json:"steps,omitempty"`
}

type AccountAutoOpsStep struct {
	ID               int64     `json:"id"`
	RunID            int64     `json:"run_id"`
	AccountID        int64     `json:"account_id"`
	AccountName      string    `json:"account_name"`
	StepIndex        int       `json:"step_index"`
	Subject          string    `json:"subject"`
	Action           string    `json:"action"`
	Status           string    `json:"status"`
	MatchedRuleID    string    `json:"matched_rule_id"`
	MatchedRuleName  string    `json:"matched_rule_name"`
	ResponseText     string    `json:"response_text"`
	ResponseHash     string    `json:"response_hash"`
	ActionResultText string    `json:"action_result_text"`
	CreatedAt        time.Time `json:"created_at"`
}

type AccountAutoOpsSample struct {
	Subject      string    `json:"subject"`
	ResponseHash string    `json:"response_hash"`
	ResponseText string    `json:"response_text"`
	Occurrences  int       `json:"occurrences"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

type AccountAutoOpsModelOption struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type AccountAutoOpsManualRunRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

type AccountAutoOpsManualRunResult struct {
	RunID             int64 `json:"run_id"`
	RequestedAccounts int   `json:"requested_accounts"`
	EligibleAccounts  int   `json:"eligible_accounts"`
}

type AccountAutoOpsRepository interface {
	CreateRun(ctx context.Context, run *AccountAutoOpsRun) (*AccountAutoOpsRun, error)
	FinishRun(ctx context.Context, runID int64, status string, totalAccounts, eligibleAccounts, completedAccounts int, errorMessage string, finishedAt time.Time) error
	CreateStep(ctx context.Context, step *AccountAutoOpsStep) (*AccountAutoOpsStep, error)
	ListRuns(ctx context.Context, since time.Time, limit int) ([]*AccountAutoOpsRun, error)
	ListStepsByRunIDs(ctx context.Context, runIDs []int64) ([]*AccountAutoOpsStep, error)
	ListSamples(ctx context.Context, since time.Time, limit int) ([]*AccountAutoOpsSample, error)
	DeleteOlderThan(ctx context.Context, cutoff time.Time) error
	GetLatestStartedAtByTrigger(ctx context.Context, triggerMode string) (*time.Time, error)
}

func DefaultAccountAutoOpsConfig() *AccountAutoOpsConfig {
	return &AccountAutoOpsConfig{
		Enabled:         false,
		IntervalMinutes: accountAutoOpsDefaultIntervalMinutes,
		TargetRules:     []AccountAutoOpsTargetRule{},
		Rules:           []AccountAutoOpsRule{},
		TestModelsByPlatform: map[string][]string{
			PlatformAnthropic:   {},
			PlatformOpenAI:      {},
			PlatformGemini:      {},
			PlatformAntigravity: {},
		},
	}
}

func NormalizeAccountAutoOpsConfig(cfg *AccountAutoOpsConfig) *AccountAutoOpsConfig {
	base := DefaultAccountAutoOpsConfig()
	if cfg == nil {
		return base
	}

	base.Enabled = cfg.Enabled
	if cfg.IntervalMinutes > 0 {
		base.IntervalMinutes = cfg.IntervalMinutes
	}

	base.TargetRules = make([]AccountAutoOpsTargetRule, 0, len(cfg.TargetRules))
	for idx, rule := range cfg.TargetRules {
		priority := rule.Priority
		if priority <= 0 {
			priority = (idx + 1) * 10
		}
		normalized := AccountAutoOpsTargetRule{
			ID:       strings.TrimSpace(rule.ID),
			Name:     strings.TrimSpace(rule.Name),
			Priority: priority,
			Action:   strings.TrimSpace(rule.Action),
		}
		if normalized.ID == "" {
			normalized.ID = fmt.Sprintf("target_rule_%d", idx+1)
		}
		normalized.Conditions = make([]AccountAutoOpsTargetCondition, 0, len(rule.Conditions))
		for _, condition := range rule.Conditions {
			normalized.Conditions = append(normalized.Conditions, AccountAutoOpsTargetCondition{
				Field:    strings.TrimSpace(condition.Field),
				Operator: strings.TrimSpace(condition.Operator),
				Value:    strings.TrimSpace(condition.Value),
			})
		}
		base.TargetRules = append(base.TargetRules, normalized)
	}
	sort.SliceStable(base.TargetRules, func(i, j int) bool {
		if base.TargetRules[i].Priority == base.TargetRules[j].Priority {
			return base.TargetRules[i].ID < base.TargetRules[j].ID
		}
		return base.TargetRules[i].Priority < base.TargetRules[j].Priority
	})
	base.TargetRulesInitialized = cfg.TargetRulesInitialized || len(base.TargetRules) > 0

	base.Rules = make([]AccountAutoOpsRule, 0, len(cfg.Rules))
	for idx, rule := range cfg.Rules {
		subject := strings.TrimSpace(rule.Subject)
		if subject == "" {
			for _, candidate := range rule.Subjects {
				candidate = strings.TrimSpace(candidate)
				if candidate != "" {
					subject = candidate
					break
				}
			}
		}
		if subject == "" {
			subject = AccountAutoOpsSubjectTestResponse
		}
		priority := rule.Priority
		if priority <= 0 {
			priority = (idx + 1) * 10
		}
		normalized := AccountAutoOpsRule{
			ID:        strings.TrimSpace(rule.ID),
			Name:      strings.TrimSpace(rule.Name),
			Subject:   subject,
			Priority:  priority,
			MatchType: strings.TrimSpace(rule.MatchType),
			Pattern:   strings.TrimSpace(rule.Pattern),
			Action:    strings.TrimSpace(rule.Action),
		}
		if normalized.ID == "" {
			normalized.ID = fmt.Sprintf("rule_%d", idx+1)
		}
		base.Rules = append(base.Rules, normalized)
	}
	sort.SliceStable(base.Rules, func(i, j int) bool {
		return base.Rules[i].Priority < base.Rules[j].Priority
	})

	if cfg.TestModelsByPlatform != nil {
		for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity} {
			rawModels := cfg.TestModelsByPlatform[platform]
			base.TestModelsByPlatform[platform] = normalizeAutoOpsModels(rawModels)
		}
	}

	base.Configured = cfg.Configured
	return base
}

func normalizeAutoOpsModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		trimmed := strings.TrimSpace(model)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func ValidateAccountAutoOpsConfig(cfg *AccountAutoOpsConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if cfg.IntervalMinutes <= 0 {
		return fmt.Errorf("interval_minutes must be greater than 0")
	}
	for idx, rule := range cfg.TargetRules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("target_rules[%d].name is required", idx)
		}
		if rule.Priority <= 0 {
			return fmt.Errorf("target_rules[%d].priority must be greater than 0", idx)
		}
		if _, ok := accountAutoOpsSupportedTargetActions[strings.TrimSpace(rule.Action)]; !ok {
			return fmt.Errorf("target_rules[%d].action is invalid", idx)
		}
		if len(rule.Conditions) == 0 {
			return fmt.Errorf("target_rules[%d].conditions is required", idx)
		}
		for condIdx, condition := range rule.Conditions {
			field := strings.TrimSpace(condition.Field)
			if _, ok := accountAutoOpsSupportedTargetFields[field]; !ok {
				return fmt.Errorf("target_rules[%d].conditions[%d].field is invalid", idx, condIdx)
			}
			operator := strings.TrimSpace(condition.Operator)
			if !isSupportedTargetOperator(field, operator) {
				return fmt.Errorf("target_rules[%d].conditions[%d].operator is invalid", idx, condIdx)
			}
			value := strings.TrimSpace(condition.Value)
			if value == "" {
				return fmt.Errorf("target_rules[%d].conditions[%d].value is required", idx, condIdx)
			}
			switch field {
			case AccountAutoOpsTargetFieldSchedulable:
				if value != "true" && value != "false" {
					return fmt.Errorf("target_rules[%d].conditions[%d].value must be true or false", idx, condIdx)
				}
			case AccountAutoOpsTargetFieldPlatform:
				if _, ok := accountAutoOpsSupportedTargetPlatforms[value]; !ok {
					return fmt.Errorf("target_rules[%d].conditions[%d].value contains unsupported platform %q", idx, condIdx, value)
				}
			case AccountAutoOpsTargetFieldAuthType:
				if _, ok := accountAutoOpsSupportedTargetAuthTypes[value]; !ok {
					return fmt.Errorf("target_rules[%d].conditions[%d].value contains unsupported auth type %q", idx, condIdx, value)
				}
			case AccountAutoOpsTargetFieldAccountStatus:
				if _, ok := accountAutoOpsSupportedTargetStatuses[value]; !ok {
					return fmt.Errorf("target_rules[%d].conditions[%d].value contains unsupported account status %q", idx, condIdx, value)
				}
			case AccountAutoOpsTargetFieldGroup:
				if value == accountAutoOpsTargetGroupUngrouped {
					break
				}
				groupID, err := strconv.ParseInt(value, 10, 64)
				if err != nil || groupID <= 0 {
					return fmt.Errorf("target_rules[%d].conditions[%d].value must be a positive group id or %q", idx, condIdx, accountAutoOpsTargetGroupUngrouped)
				}
			case AccountAutoOpsTargetFieldLastUsedDays:
				days, err := strconv.Atoi(value)
				if err != nil || days <= 0 {
					return fmt.Errorf("target_rules[%d].conditions[%d].value must be a positive integer", idx, condIdx)
				}
			}
		}
	}
	for idx, rule := range cfg.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rules[%d].name is required", idx)
		}
		if strings.TrimSpace(rule.Pattern) == "" {
			return fmt.Errorf("rules[%d].pattern is required", idx)
		}
		if rule.Priority <= 0 {
			return fmt.Errorf("rules[%d].priority must be greater than 0", idx)
		}
		subject := strings.TrimSpace(rule.Subject)
		if subject == "" {
			subject = firstAutoOpsSubject(rule.Subjects)
		}
		if subject == "" {
			return fmt.Errorf("rules[%d].subject is required", idx)
		}
		if _, ok := accountAutoOpsSupportedSubjects[subject]; !ok {
			return fmt.Errorf("rules[%d].subject contains unsupported value %q", idx, subject)
		}
		if _, ok := accountAutoOpsSupportedMatchTypes[strings.TrimSpace(rule.MatchType)]; !ok {
			return fmt.Errorf("rules[%d].match_type is invalid", idx)
		}
		if _, ok := accountAutoOpsSupportedActions[strings.TrimSpace(rule.Action)]; !ok {
			return fmt.Errorf("rules[%d].action is invalid", idx)
		}
	}
	return nil
}

func ShouldMigrateLegacyAccountAutoOpsTargetRules(cfg *AccountAutoOpsConfig) bool {
	if cfg == nil {
		return false
	}
	return !cfg.TargetRulesInitialized && len(cfg.TargetRules) == 0
}

func WithMigratedLegacyAccountAutoOpsTargetRules(cfg *AccountAutoOpsConfig) *AccountAutoOpsConfig {
	normalized := NormalizeAccountAutoOpsConfig(cfg)
	if !ShouldMigrateLegacyAccountAutoOpsTargetRules(normalized) {
		return normalized
	}
	normalized.TargetRules = []AccountAutoOpsTargetRule{DefaultLegacyAccountAutoOpsTargetRule()}
	normalized.TargetRulesInitialized = true
	return NormalizeAccountAutoOpsConfig(normalized)
}

func DefaultLegacyAccountAutoOpsTargetRule() AccountAutoOpsTargetRule {
	return AccountAutoOpsTargetRule{
		ID:       accountAutoOpsLegacyTargetRuleID,
		Name:     accountAutoOpsLegacyTargetRuleName,
		Priority: 10,
		Action:   AccountAutoOpsTargetActionTakeover,
		Conditions: []AccountAutoOpsTargetCondition{
			{
				Field:    AccountAutoOpsTargetFieldAccountStatus,
				Operator: AccountAutoOpsTargetOperatorEQ,
				Value:    AccountAutoOpsTargetStatusError,
			},
			{
				Field:    AccountAutoOpsTargetFieldSchedulable,
				Operator: AccountAutoOpsTargetOperatorEQ,
				Value:    "true",
			},
		},
	}
}

func MatchAccountAutoOpsRule(rule AccountAutoOpsRule, subject string, input string) bool {
	subject = strings.TrimSpace(subject)
	ruleSubject := strings.TrimSpace(rule.Subject)
	if ruleSubject == "" {
		ruleSubject = firstAutoOpsSubject(rule.Subjects)
	}
	if ruleSubject == "" || ruleSubject != subject {
		return false
	}

	pattern := strings.TrimSpace(rule.Pattern)
	if pattern == "" {
		return false
	}
	matched := autoOpsMatchText(pattern, input)
	switch strings.TrimSpace(rule.MatchType) {
	case AccountAutoOpsMatchContains:
		return matched
	case AccountAutoOpsMatchNotContains:
		return !matched
	default:
		return false
	}
}

func MatchAccountAutoOpsTargetRule(rule AccountAutoOpsTargetRule, account *Account, now time.Time) bool {
	if account == nil || len(rule.Conditions) == 0 {
		return false
	}
	for _, condition := range rule.Conditions {
		if !matchAccountAutoOpsTargetCondition(condition, account, now) {
			return false
		}
	}
	return true
}

func FindMatchingAccountAutoOpsTargetRule(rules []AccountAutoOpsTargetRule, account *Account, now time.Time) *AccountAutoOpsTargetRule {
	for idx := range rules {
		if MatchAccountAutoOpsTargetRule(rules[idx], account, now) {
			rule := rules[idx]
			return &rule
		}
	}
	return nil
}

func firstAutoOpsSubject(subjects []string) string {
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject != "" {
			return subject
		}
	}
	return ""
}

func isSupportedTargetOperator(field string, operator string) bool {
	operators, ok := accountAutoOpsTargetOperatorsByField[field]
	if !ok {
		return false
	}
	_, ok = operators[operator]
	return ok
}

func matchAccountAutoOpsTargetCondition(condition AccountAutoOpsTargetCondition, account *Account, now time.Time) bool {
	field := strings.TrimSpace(condition.Field)
	operator := strings.TrimSpace(condition.Operator)
	value := strings.TrimSpace(condition.Value)
	if account == nil || field == "" || operator == "" || value == "" {
		return false
	}
	switch field {
	case AccountAutoOpsTargetFieldAccountName:
		switch operator {
		case AccountAutoOpsTargetOperatorContains:
			return autoOpsMatchText(value, account.Name)
		case AccountAutoOpsTargetOperatorNotContains:
			return !autoOpsMatchText(value, account.Name)
		case AccountAutoOpsTargetOperatorEQ:
			return strings.EqualFold(strings.TrimSpace(account.Name), value)
		case AccountAutoOpsTargetOperatorNEQ:
			return !strings.EqualFold(strings.TrimSpace(account.Name), value)
		default:
			return false
		}
	case AccountAutoOpsTargetFieldSchedulable:
		expected := value == "true"
		if operator == AccountAutoOpsTargetOperatorEQ {
			return account.Schedulable == expected
		}
		if operator == AccountAutoOpsTargetOperatorNEQ {
			return account.Schedulable != expected
		}
		return false
	case AccountAutoOpsTargetFieldPlatform:
		return compareAutoOpsTargetScalar(account.Platform, operator, value)
	case AccountAutoOpsTargetFieldAuthType:
		return compareAutoOpsTargetScalar(account.Type, operator, value)
	case AccountAutoOpsTargetFieldAccountStatus:
		return compareAutoOpsTargetScalar(resolveAccountAutoOpsTargetStatus(account, now), operator, value)
	case AccountAutoOpsTargetFieldGroup:
		return matchAccountAutoOpsTargetGroup(account, operator, value)
	case AccountAutoOpsTargetFieldLastUsedDays:
		days, err := strconv.Atoi(value)
		if err != nil || days <= 0 {
			return false
		}
		return matchAccountAutoOpsTargetLastUsedDays(account, operator, days, now)
	default:
		return false
	}
}

func compareAutoOpsTargetScalar(actual string, operator string, expected string) bool {
	switch operator {
	case AccountAutoOpsTargetOperatorEQ:
		return actual == expected
	case AccountAutoOpsTargetOperatorNEQ:
		return actual != expected
	default:
		return false
	}
}

func resolveAccountAutoOpsTargetStatus(account *Account, now time.Time) string {
	if account == nil {
		return ""
	}
	status := strings.TrimSpace(account.Status)
	switch status {
	case StatusError:
		return AccountAutoOpsTargetStatusError
	case StatusDisabled, "inactive":
		return AccountAutoOpsTargetStatusPaused
	}
	if account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) {
		return AccountAutoOpsTargetStatusTempUnschedulable
	}
	if account.RateLimitResetAt != nil && now.Before(*account.RateLimitResetAt) {
		return AccountAutoOpsTargetStatusRateLimited
	}
	if status == StatusActive {
		return AccountAutoOpsTargetStatusNormal
	}
	return status
}

func matchAccountAutoOpsTargetGroup(account *Account, operator string, value string) bool {
	groupIDs := make([]int64, 0, len(account.GroupIDs))
	groupIDs = append(groupIDs, account.GroupIDs...)
	if len(groupIDs) == 0 && len(account.Groups) > 0 {
		for _, group := range account.Groups {
			if group == nil {
				continue
			}
			groupIDs = append(groupIDs, group.ID)
		}
	}
	if value == accountAutoOpsTargetGroupUngrouped {
		isUngrouped := len(groupIDs) == 0
		if operator == AccountAutoOpsTargetOperatorEQ {
			return isUngrouped
		}
		if operator == AccountAutoOpsTargetOperatorNEQ {
			return !isUngrouped
		}
		return false
	}
	groupID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || groupID <= 0 {
		return false
	}
	hasGroup := false
	for _, candidate := range groupIDs {
		if candidate == groupID {
			hasGroup = true
			break
		}
	}
	if operator == AccountAutoOpsTargetOperatorEQ {
		return hasGroup
	}
	if operator == AccountAutoOpsTargetOperatorNEQ {
		return !hasGroup
	}
	return false
}

func matchAccountAutoOpsTargetLastUsedDays(account *Account, operator string, days int, now time.Time) bool {
	threshold := now.Add(-time.Duration(days) * 24 * time.Hour)
	switch operator {
	case AccountAutoOpsTargetOperatorEQ:
		if account.LastUsedAt == nil {
			return true
		}
		return !account.LastUsedAt.After(threshold)
	case AccountAutoOpsTargetOperatorNEQ:
		if account.LastUsedAt == nil {
			return false
		}
		return account.LastUsedAt.After(threshold)
	default:
		return false
	}
}

func autoOpsMatchText(pattern string, input string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if usesStrictASCIIAutoOpsMatch(pattern) {
		return matchStrictASCIIAutoOps(pattern, input)
	}
	return strings.Contains(strings.ToLower(input), strings.ToLower(pattern))
}

func usesStrictASCIIAutoOpsMatch(pattern string) bool {
	hasASCIIWord := false
	for _, r := range pattern {
		if r > unicode.MaxASCII {
			return false
		}
		if isAutoOpsWordRune(r) {
			hasASCIIWord = true
		}
	}
	return hasASCIIWord
}

func matchStrictASCIIAutoOps(pattern string, input string) bool {
	needle := strings.ToLower(pattern)
	haystack := strings.ToLower(input)
	if needle == "" || haystack == "" {
		return false
	}
	searchFrom := 0
	for {
		idx := strings.Index(haystack[searchFrom:], needle)
		if idx == -1 {
			return false
		}
		start := searchFrom + idx
		end := start + len(needle)
		if hasAutoOpsBoundary(haystack, start-1) && hasAutoOpsBoundary(haystack, end) {
			return true
		}
		searchFrom = start + 1
		if searchFrom >= len(haystack) {
			return false
		}
	}
}

func isAutoOpsWordRune(r rune) bool {
	return r == '_' || unicode.IsDigit(r) || unicode.IsLetter(r)
}

func hasAutoOpsBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[index:])
	return !isAutoOpsWordRune(r)
}

func NormalizeAutoOpsResponseText(in string, limit int) string {
	in = strings.TrimSpace(in)
	if limit <= 0 || len(in) <= limit {
		return in
	}
	for limit > 0 && !utf8.ValidString(in[:limit]) {
		limit--
	}
	if limit <= 0 {
		return ""
	}
	return strings.TrimSpace(in[:limit])
}

func AccountAutoOpsResponseHash(in string) string {
	trimmed := strings.TrimSpace(in)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}
