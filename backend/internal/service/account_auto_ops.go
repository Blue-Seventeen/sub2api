package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
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
)

type AccountAutoOpsRule struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Subjects  []string `json:"subjects"`
	MatchType string   `json:"match_type"`
	Pattern   string   `json:"pattern"`
	Action    string   `json:"action"`
}

type AccountAutoOpsConfig struct {
	Enabled              bool                 `json:"enabled"`
	IntervalMinutes      int                  `json:"interval_minutes"`
	Rules                []AccountAutoOpsRule `json:"rules"`
	TestModelsByPlatform map[string][]string  `json:"test_models_by_platform"`
	Configured           bool                 `json:"configured,omitempty"`
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

	base.Rules = make([]AccountAutoOpsRule, 0, len(cfg.Rules))
	for idx, rule := range cfg.Rules {
		normalized := AccountAutoOpsRule{
			ID:        strings.TrimSpace(rule.ID),
			Name:      strings.TrimSpace(rule.Name),
			MatchType: strings.TrimSpace(rule.MatchType),
			Pattern:   strings.TrimSpace(rule.Pattern),
			Action:    strings.TrimSpace(rule.Action),
		}
		seenSubjects := make(map[string]struct{}, len(rule.Subjects))
		for _, subject := range rule.Subjects {
			subject = strings.TrimSpace(subject)
			if subject == "" {
				continue
			}
			if _, exists := seenSubjects[subject]; exists {
				continue
			}
			seenSubjects[subject] = struct{}{}
			normalized.Subjects = append(normalized.Subjects, subject)
		}
		sort.Strings(normalized.Subjects)
		if normalized.ID == "" {
			normalized.ID = fmt.Sprintf("rule_%d", idx+1)
		}
		base.Rules = append(base.Rules, normalized)
	}

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
	for idx, rule := range cfg.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rules[%d].name is required", idx)
		}
		if strings.TrimSpace(rule.Pattern) == "" {
			return fmt.Errorf("rules[%d].pattern is required", idx)
		}
		if len(rule.Subjects) == 0 {
			return fmt.Errorf("rules[%d].subjects is required", idx)
		}
		for _, subject := range rule.Subjects {
			if _, ok := accountAutoOpsSupportedSubjects[strings.TrimSpace(subject)]; !ok {
				return fmt.Errorf("rules[%d].subjects contains unsupported subject %q", idx, subject)
			}
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

func MatchAccountAutoOpsRule(rule AccountAutoOpsRule, subject string, input string) bool {
	if len(rule.Subjects) == 0 {
		return false
	}
	subject = strings.TrimSpace(subject)
	matchedSubject := false
	for _, item := range rule.Subjects {
		if strings.TrimSpace(item) == subject {
			matchedSubject = true
			break
		}
	}
	if !matchedSubject {
		return false
	}

	pattern := strings.ToLower(strings.TrimSpace(rule.Pattern))
	if pattern == "" {
		return false
	}
	body := strings.ToLower(input)
	switch strings.TrimSpace(rule.MatchType) {
	case AccountAutoOpsMatchContains:
		return strings.Contains(body, pattern)
	case AccountAutoOpsMatchNotContains:
		return !strings.Contains(body, pattern)
	default:
		return false
	}
}

func NormalizeAutoOpsResponseText(in string, limit int) string {
	in = strings.TrimSpace(in)
	if limit <= 0 || len(in) <= limit {
		return in
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
