package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/redis/go-redis/v9"
)

var accountAutoOpsUnlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

type accountAutoOpsActionOutput struct {
	nextSubject string
	nextText    string
	resultText  string
}

type accountAutoOpsLoopGuard struct {
	mu       sync.Mutex
	repeated map[string]int
	total    int
}

func newAccountAutoOpsLoopGuard() *accountAutoOpsLoopGuard {
	return &accountAutoOpsLoopGuard{
		repeated: make(map[string]int),
	}
}

func (g *accountAutoOpsLoopGuard) Record(subject, action, responseHash string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.total++
	key := strings.Join([]string{subject, action, responseHash}, "|")
	g.repeated[key]++
	if g.total > accountAutoOpsLoopGuardMaxSteps {
		return false
	}
	if responseHash == "" {
		return true
	}
	return g.repeated[key] <= accountAutoOpsLoopGuardMaxRepeats
}

type AccountAutoOpsService struct {
	settingService    *SettingService
	accountRepo       AccountRepository
	autoOpsRepo       AccountAutoOpsRepository
	accountTestSvc    *AccountTestService
	accountRefreshSvc *AccountRefreshService
	rateLimitSvc      *RateLimitService
	adminService      AdminService
	redisClient       *redis.Client

	lockMu    sync.Mutex
	localLock bool
}

func NewAccountAutoOpsService(
	settingService *SettingService,
	accountRepo AccountRepository,
	autoOpsRepo AccountAutoOpsRepository,
	accountTestSvc *AccountTestService,
	accountRefreshSvc *AccountRefreshService,
	rateLimitSvc *RateLimitService,
	adminService AdminService,
	redisClient *redis.Client,
) *AccountAutoOpsService {
	return &AccountAutoOpsService{
		settingService:    settingService,
		accountRepo:       accountRepo,
		autoOpsRepo:       autoOpsRepo,
		accountTestSvc:    accountTestSvc,
		accountRefreshSvc: accountRefreshSvc,
		rateLimitSvc:      rateLimitSvc,
		adminService:      adminService,
		redisClient:       redisClient,
	}
}

func (s *AccountAutoOpsService) GetConfig(ctx context.Context) (*AccountAutoOpsConfig, error) {
	if s.settingService == nil {
		cfg := DefaultAccountAutoOpsConfig()
		cfg.Configured = false
		return cfg, nil
	}
	cfg, configured, err := s.settingService.GetAccountAutoOpsConfig(ctx)
	if err != nil {
		return nil, err
	}
	cfg.Configured = configured
	return cfg, nil
}

func (s *AccountAutoOpsService) UpdateConfig(ctx context.Context, cfg *AccountAutoOpsConfig) (*AccountAutoOpsConfig, error) {
	if s.settingService == nil {
		return nil, fmt.Errorf("setting service is not configured")
	}
	normalized := NormalizeAccountAutoOpsConfig(cfg)
	if err := ValidateAccountAutoOpsConfig(normalized); err != nil {
		return nil, err
	}
	if err := s.settingService.SetAccountAutoOpsConfig(ctx, normalized); err != nil {
		return nil, err
	}
	s.cleanupOldLogs(ctx)

	updated, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}

	if updated.Enabled {
		go func() {
			runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			if _, runErr := s.RunAutomatic(runCtx); runErr != nil {
				slog.Warn("account_auto_ops_immediate_run_failed", "error", runErr)
			}
		}()
	}

	return updated, nil
}

func (s *AccountAutoOpsService) GetModelOptions() map[string][]AccountAutoOpsModelOption {
	options := map[string][]AccountAutoOpsModelOption{
		PlatformAnthropic:   make([]AccountAutoOpsModelOption, 0, len(claude.DefaultModels)),
		PlatformOpenAI:      make([]AccountAutoOpsModelOption, 0, len(openai.DefaultModels)),
		PlatformGemini:      make([]AccountAutoOpsModelOption, 0, len(geminicli.DefaultModels)),
		PlatformAntigravity: make([]AccountAutoOpsModelOption, 0, len(antigravity.DefaultModels())),
	}

	for _, model := range claude.DefaultModels {
		options[PlatformAnthropic] = append(options[PlatformAnthropic], AccountAutoOpsModelOption{ID: model.ID, DisplayName: model.DisplayName})
	}
	for _, model := range openai.DefaultModels {
		displayName := model.DisplayName
		if strings.TrimSpace(displayName) == "" {
			displayName = model.ID
		}
		options[PlatformOpenAI] = append(options[PlatformOpenAI], AccountAutoOpsModelOption{ID: model.ID, DisplayName: displayName})
	}
	for _, model := range geminicli.DefaultModels {
		displayName := model.DisplayName
		if strings.TrimSpace(displayName) == "" {
			displayName = model.ID
		}
		options[PlatformGemini] = append(options[PlatformGemini], AccountAutoOpsModelOption{ID: model.ID, DisplayName: displayName})
	}
	for _, model := range antigravity.DefaultModels() {
		displayName := model.DisplayName
		if strings.TrimSpace(displayName) == "" {
			displayName = model.ID
		}
		options[PlatformAntigravity] = append(options[PlatformAntigravity], AccountAutoOpsModelOption{ID: model.ID, DisplayName: displayName})
	}
	return options
}

func (s *AccountAutoOpsService) ListLogs(ctx context.Context, limit int) ([]*AccountAutoOpsRun, error) {
	if limit <= 0 {
		limit = accountAutoOpsDefaultLogLimit
	}
	s.cleanupOldLogs(ctx)
	if s.autoOpsRepo == nil {
		return []*AccountAutoOpsRun{}, nil
	}
	since := time.Now().Add(-accountAutoOpsLogRetention)
	runs, err := s.autoOpsRepo.ListRuns(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return runs, nil
	}
	runIDs := make([]int64, 0, len(runs))
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
	}
	steps, err := s.autoOpsRepo.ListStepsByRunIDs(ctx, runIDs)
	if err != nil {
		return nil, err
	}
	stepsByRun := make(map[int64][]*AccountAutoOpsStep, len(runIDs))
	for _, step := range steps {
		stepsByRun[step.RunID] = append(stepsByRun[step.RunID], step)
	}
	for _, run := range runs {
		run.Steps = stepsByRun[run.ID]
	}
	return runs, nil
}

func (s *AccountAutoOpsService) ListSamples(ctx context.Context, limit int) ([]*AccountAutoOpsSample, error) {
	if limit <= 0 {
		limit = accountAutoOpsDefaultSampleLimit
	}
	s.cleanupOldLogs(ctx)
	if s.autoOpsRepo == nil {
		return []*AccountAutoOpsSample{}, nil
	}
	return s.autoOpsRepo.ListSamples(ctx, time.Now().Add(-accountAutoOpsLogRetention), limit)
}

func (s *AccountAutoOpsService) RunAutomatic(ctx context.Context) (*AccountAutoOpsRun, error) {
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled || !cfg.Configured {
		return nil, nil
	}
	accounts, err := s.listEligibleAutoAccounts(ctx)
	if err != nil {
		return nil, err
	}
	return s.runWithAccounts(ctx, AccountAutoOpsTriggerAutomatic, nil, accounts, cfg)
}

func (s *AccountAutoOpsService) RunManual(ctx context.Context, accountIDs []int64) (*AccountAutoOpsManualRunResult, error) {
	if len(accountIDs) == 0 {
		return nil, fmt.Errorf("account_ids is required")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Configured {
		return nil, fmt.Errorf("account auto ops config is not set")
	}
	accounts, err := s.listEligibleSelectedAccounts(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	run, err := s.runWithAccounts(ctx, AccountAutoOpsTriggerManual, accountIDs, accounts, cfg)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return &AccountAutoOpsManualRunResult{
			RunID:             0,
			RequestedAccounts: len(accountIDs),
			EligibleAccounts:  0,
		}, nil
	}
	return &AccountAutoOpsManualRunResult{
		RunID:             run.ID,
		RequestedAccounts: len(accountIDs),
		EligibleAccounts:  run.EligibleAccounts,
	}, nil
}

func (s *AccountAutoOpsService) GetLatestAutomaticRunAt(ctx context.Context) (*time.Time, error) {
	if s.autoOpsRepo == nil {
		return nil, nil
	}
	return s.autoOpsRepo.GetLatestStartedAtByTrigger(ctx, AccountAutoOpsTriggerAutomatic)
}

func (s *AccountAutoOpsService) runWithAccounts(ctx context.Context, triggerMode string, requestedIDs []int64, accounts []*Account, cfg *AccountAutoOpsConfig) (*AccountAutoOpsRun, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	release, acquired, err := s.acquireRunLock(ctx)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, nil
	}
	defer release()

	s.cleanupOldLogs(ctx)

	run := &AccountAutoOpsRun{
		TriggerMode:         triggerMode,
		Status:              AccountAutoOpsRunStatusRunning,
		RequestedAccountIDs: append([]int64(nil), requestedIDs...),
		TotalAccounts:       len(requestedIDs),
		EligibleAccounts:    len(accounts),
		CompletedAccounts:   0,
		StartedAt:           time.Now().UTC(),
	}
	if run.TotalAccounts == 0 {
		run.TotalAccounts = len(accounts)
	}
	if s.autoOpsRepo != nil {
		persistedRun, createErr := s.autoOpsRepo.CreateRun(ctx, run)
		if createErr != nil {
			return nil, createErr
		}
		run = persistedRun
	}
	if len(accounts) == 0 {
		if s.autoOpsRepo != nil {
			_ = s.autoOpsRepo.FinishRun(ctx, run.ID, AccountAutoOpsRunStatusCompleted, run.TotalAccounts, 0, 0, "", time.Now().UTC())
		}
		run.Status = AccountAutoOpsRunStatusCompleted
		now := time.Now().UTC()
		run.FinishedAt = &now
		return run, nil
	}

	completed := 0
	var runErr error
	for _, account := range accounts {
		if account == nil {
			continue
		}
		if err := s.processAccount(ctx, run, account, cfg); err != nil {
			runErr = err
			slog.Warn("account_auto_ops_account_failed", "run_id", run.ID, "account_id", account.ID, "error", err)
		}
		completed++
	}

	finishedAt := time.Now().UTC()
	status := AccountAutoOpsRunStatusCompleted
	errMsg := ""
	if runErr != nil {
		status = AccountAutoOpsRunStatusFailed
		errMsg = runErr.Error()
	}
	if s.autoOpsRepo != nil {
		if finishErr := s.autoOpsRepo.FinishRun(ctx, run.ID, status, run.TotalAccounts, len(accounts), completed, errMsg, finishedAt); finishErr != nil {
			return nil, finishErr
		}
	}
	run.Status = status
	run.CompletedAccounts = completed
	run.EligibleAccounts = len(accounts)
	run.ErrorMessage = errMsg
	run.FinishedAt = &finishedAt
	return run, nil
}

func (s *AccountAutoOpsService) processAccount(ctx context.Context, run *AccountAutoOpsRun, account *Account, cfg *AccountAutoOpsConfig) error {
	if run == nil || account == nil || cfg == nil {
		return nil
	}
	loopGuard := newAccountAutoOpsLoopGuard()
	currentSubject := AccountAutoOpsSubjectAccountName
	currentText := account.Name
	stepIndex := 0

	for {
		responseHash := AccountAutoOpsResponseHash(currentText)
		rule := s.findMatchingRule(cfg.Rules, currentSubject, currentText)
		if rule == nil {
			if currentSubject != AccountAutoOpsSubjectAccountName {
				return s.appendStep(ctx, &AccountAutoOpsStep{
					RunID:            run.ID,
					AccountID:        account.ID,
					AccountName:      account.Name,
					StepIndex:        stepIndex,
					Subject:          currentSubject,
					Status:           AccountAutoOpsStepStatusNoRuleMatched,
					ResponseText:     NormalizeAutoOpsResponseText(currentText, accountAutoOpsResponsePreviewLimit),
					ResponseHash:     responseHash,
					ActionResultText: "no matching rule",
				})
			}
			rule = &AccountAutoOpsRule{
				ID:        "default_retest",
				Name:      "Default Retest",
				Subjects:  []string{AccountAutoOpsSubjectAccountName},
				MatchType: AccountAutoOpsMatchContains,
				Pattern:   account.Name,
				Action:    AccountAutoOpsActionRetest,
			}
		}
		if !loopGuard.Record(currentSubject, rule.Action, responseHash) {
			return s.appendStep(ctx, &AccountAutoOpsStep{
				RunID:            run.ID,
				AccountID:        account.ID,
				AccountName:      account.Name,
				StepIndex:        stepIndex,
				Subject:          currentSubject,
				Action:           rule.Action,
				Status:           AccountAutoOpsStepStatusLoopGuardStopped,
				MatchedRuleID:    rule.ID,
				MatchedRuleName:  rule.Name,
				ResponseText:     NormalizeAutoOpsResponseText(currentText, accountAutoOpsResponsePreviewLimit),
				ResponseHash:     responseHash,
				ActionResultText: "loop_guard_stopped",
			})
		}

		actionOutput, terminal, actionErr := s.executeRuleAction(ctx, account, cfg, rule)
		stepStatus := AccountAutoOpsStepStatusMatched
		if actionErr != nil {
			stepStatus = AccountAutoOpsStepStatusActionFailed
		} else if terminal {
			stepStatus = AccountAutoOpsStepStatusActionExecuted
		} else {
			stepStatus = AccountAutoOpsStepStatusActionExecuted
		}
		if err := s.appendStep(ctx, &AccountAutoOpsStep{
			RunID:            run.ID,
			AccountID:        account.ID,
			AccountName:      account.Name,
			StepIndex:        stepIndex,
			Subject:          currentSubject,
			Action:           rule.Action,
			Status:           stepStatus,
			MatchedRuleID:    rule.ID,
			MatchedRuleName:  rule.Name,
			ResponseText:     NormalizeAutoOpsResponseText(currentText, accountAutoOpsResponsePreviewLimit),
			ResponseHash:     responseHash,
			ActionResultText: NormalizeAutoOpsResponseText(actionOutput.resultText, accountAutoOpsResponsePreviewLimit),
		}); err != nil {
			return err
		}
		if actionErr != nil {
			return actionErr
		}
		if terminal {
			return nil
		}
		currentSubject = actionOutput.nextSubject
		currentText = actionOutput.nextText
		stepIndex++
	}
}

func (s *AccountAutoOpsService) executeRuleAction(ctx context.Context, account *Account, cfg *AccountAutoOpsConfig, rule *AccountAutoOpsRule) (accountAutoOpsActionOutput, bool, error) {
	out := accountAutoOpsActionOutput{}
	switch rule.Action {
	case AccountAutoOpsActionRetest:
		text, resultText := s.runRetestAction(ctx, account, cfg)
		out.nextSubject = AccountAutoOpsSubjectTestResponse
		out.nextText = text
		out.resultText = resultText
		return out, false, nil
	case AccountAutoOpsActionRefreshToken:
		text, resultText := s.runRefreshAction(ctx, account)
		out.nextSubject = AccountAutoOpsSubjectRefreshResponse
		out.nextText = text
		out.resultText = resultText
		return out, false, nil
	case AccountAutoOpsActionRecoverState:
		if s.rateLimitSvc == nil {
			return out, true, fmt.Errorf("rate limit service not configured")
		}
		recovery, err := s.rateLimitSvc.RecoverAccountState(ctx, account.ID, AccountRecoveryOptions{InvalidateToken: true})
		if err != nil {
			return out, true, err
		}
		data, _ := json.Marshal(recovery)
		out.resultText = string(data)
		return out, true, nil
	case AccountAutoOpsActionEnableSchedulable:
		if s.adminService == nil {
			return out, true, fmt.Errorf("admin service not configured")
		}
		updated, err := s.adminService.SetAccountSchedulable(ctx, account.ID, true)
		if err != nil {
			return out, true, err
		}
		if updated != nil {
			account.Schedulable = updated.Schedulable
		}
		out.resultText = `{"schedulable":true}`
		return out, true, nil
	case AccountAutoOpsActionDisableSchedulable:
		if s.adminService == nil {
			return out, true, fmt.Errorf("admin service not configured")
		}
		updated, err := s.adminService.SetAccountSchedulable(ctx, account.ID, false)
		if err != nil {
			return out, true, err
		}
		if updated != nil {
			account.Schedulable = updated.Schedulable
		}
		out.resultText = `{"schedulable":false}`
		return out, true, nil
	case AccountAutoOpsActionDeleteAccount:
		if s.adminService == nil {
			return out, true, fmt.Errorf("admin service not configured")
		}
		if err := s.adminService.DeleteAccount(ctx, account.ID); err != nil {
			return out, true, err
		}
		out.resultText = `{"deleted":true}`
		return out, true, nil
	default:
		return out, true, fmt.Errorf("unsupported action %q", rule.Action)
	}
}

func (s *AccountAutoOpsService) runRetestAction(ctx context.Context, account *Account, cfg *AccountAutoOpsConfig) (string, string) {
	if s.accountTestSvc == nil {
		errMsg := "account test service not configured"
		return errMsg, errMsg
	}
	models := s.resolveTestModels(cfg, account.Platform)
	if len(models) == 0 {
		models = []string{""}
	}
	var lastText string
	for idx, modelID := range models {
		result, err := s.accountTestSvc.RunTestBackground(ctx, account.ID, modelID)
		if err != nil {
			lastText = buildAutoOpsTestResponseText(modelID, nil, err.Error())
		} else {
			lastText = buildAutoOpsTestResponseText(modelID, result, "")
		}
		if idx < len(models)-1 && shouldAutoOpsFallbackToNextTestModel(lastText) {
			continue
		}
		break
	}
	return NormalizeAutoOpsResponseText(lastText, accountAutoOpsResponseSampleLimit), lastText
}

func (s *AccountAutoOpsService) runRefreshAction(ctx context.Context, account *Account) (string, string) {
	if s.accountRefreshSvc == nil {
		errMsg := "account refresh service not configured"
		return errMsg, errMsg
	}
	result, err := s.accountRefreshSvc.RefreshAccount(ctx, account)
	if err != nil {
		text := NormalizeAutoOpsResponseText(err.Error(), accountAutoOpsResponseSampleLimit)
		return text, err.Error()
	}
	text := NormalizeAutoOpsResponseText(result.ResponseText, accountAutoOpsResponseSampleLimit)
	return text, result.ResponseText
}

func buildAutoOpsTestResponseText(modelID string, result *ScheduledTestResult, transportErr string) string {
	payload := map[string]any{
		"model_id": modelID,
	}
	if transportErr != "" {
		payload["status"] = "failed"
		payload["error_message"] = transportErr
	} else if result != nil {
		payload["status"] = result.Status
		payload["response_text"] = result.ResponseText
		payload["error_message"] = result.ErrorMessage
		payload["latency_ms"] = result.LatencyMs
	} else {
		payload["status"] = "failed"
		payload["error_message"] = "empty test result"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		if transportErr != "" {
			return transportErr
		}
		if result != nil && strings.TrimSpace(result.ErrorMessage) != "" {
			return result.ErrorMessage
		}
		if result != nil {
			return result.ResponseText
		}
		return "empty test result"
	}
	return string(data)
}

func shouldAutoOpsFallbackToNextTestModel(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "unsupported") ||
		strings.Contains(lower, "invalid model") ||
		strings.Contains(lower, "model not found") ||
		strings.Contains(lower, "not support")
}

func (s *AccountAutoOpsService) resolveTestModels(cfg *AccountAutoOpsConfig, platform string) []string {
	if cfg != nil && cfg.TestModelsByPlatform != nil {
		if models := normalizeAutoOpsModels(cfg.TestModelsByPlatform[platform]); len(models) > 0 {
			return models
		}
	}
	switch platform {
	case PlatformOpenAI:
		return []string{openai.DefaultTestModel}
	case PlatformGemini:
		return []string{geminicli.DefaultTestModel}
	case PlatformAntigravity:
		return []string{claude.DefaultTestModel}
	default:
		return []string{claude.DefaultTestModel}
	}
}

func (s *AccountAutoOpsService) findMatchingRule(rules []AccountAutoOpsRule, subject, input string) *AccountAutoOpsRule {
	for idx := range rules {
		if MatchAccountAutoOpsRule(rules[idx], subject, input) {
			rule := rules[idx]
			return &rule
		}
	}
	return nil
}

func (s *AccountAutoOpsService) appendStep(ctx context.Context, step *AccountAutoOpsStep) error {
	if s.autoOpsRepo == nil || step == nil {
		return nil
	}
	_, err := s.autoOpsRepo.CreateStep(ctx, step)
	return err
}

func (s *AccountAutoOpsService) cleanupOldLogs(ctx context.Context) {
	if s.autoOpsRepo == nil {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.autoOpsRepo.DeleteOlderThan(cleanupCtx, time.Now().Add(-accountAutoOpsLogRetention)); err != nil {
		slog.Warn("account_auto_ops_cleanup_old_logs_failed", "error", err)
	}
}

func (s *AccountAutoOpsService) listEligibleAutoAccounts(ctx context.Context) ([]*Account, error) {
	if s.accountRepo == nil {
		return []*Account{}, nil
	}
	accounts, err := s.accountRepo.ListAutoOpsEligible(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*Account, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		result = append(result, &account)
	}
	return result, nil
}

func (s *AccountAutoOpsService) listEligibleSelectedAccounts(ctx context.Context, accountIDs []int64) ([]*Account, error) {
	if s.accountRepo == nil {
		return []*Account{}, nil
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
	if err != nil {
		return nil, err
	}
	result := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		if account.Status == StatusError && account.Schedulable {
			result = append(result, account)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *AccountAutoOpsService) acquireRunLock(ctx context.Context) (func(), bool, error) {
	s.lockMu.Lock()
	if s.localLock {
		s.lockMu.Unlock()
		return func() {}, false, nil
	}
	s.localLock = true
	s.lockMu.Unlock()

	token := accountAutoOpsRandomToken()
	release := func() {
		if s.redisClient != nil {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = accountAutoOpsUnlockScript.Run(releaseCtx, s.redisClient, []string{accountAutoOpsRunLockKey}, token).Err()
		}
		s.lockMu.Lock()
		s.localLock = false
		s.lockMu.Unlock()
	}

	if s.redisClient == nil {
		return release, true, nil
	}

	ok, err := s.redisClient.SetNX(ctx, accountAutoOpsRunLockKey, token, accountAutoOpsRunLockTTL).Result()
	if err != nil {
		release()
		return nil, false, err
	}
	if !ok {
		release()
		return func() {}, false, nil
	}
	return release, true, nil
}

func accountAutoOpsRandomToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
