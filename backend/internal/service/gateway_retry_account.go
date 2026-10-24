package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ErrSameAccountRetryUnavailable tells the caller that its pending account can
// no longer serve this request. Selection must not silently spend that retry on
// a different account; the caller clears the pin and excludes the old account.
var ErrSameAccountRetryUnavailable = errors.New("same-account retry is unavailable")

type gatewayRetryAccountContextKey struct{}
type gatewayRequestAttemptContextKey struct{}

type gatewayRequestAttemptState struct {
	mu                  sync.Mutex
	accountIDs          map[string]map[int64]struct{}
	authoritativeScopes map[string]struct{}
}

// WithGatewayRetryAccount restricts the next selection to accountID. Zero or a
// negative ID explicitly clears an inherited pin. It does not bypass admission.
func WithGatewayRetryAccount(ctx context.Context, accountID int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if accountID < 0 {
		accountID = 0
	}
	return context.WithValue(ctx, gatewayRetryAccountContextKey{}, accountID)
}

func GatewayRetryAccountFromContext(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	accountID, _ := ctx.Value(gatewayRetryAccountContextKey{}).(int64)
	return accountID
}

// NewGatewayRequestAttemptContext creates one candidate universe for a logical
// request. Re-entry preserves the existing universe across retry selections.
func NewGatewayRequestAttemptContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState); state != nil {
		return ctx
	}
	return context.WithValue(ctx, gatewayRequestAttemptContextKey{}, &gatewayRequestAttemptState{
		accountIDs:          make(map[string]map[int64]struct{}),
		authoritativeScopes: make(map[string]struct{}),
	})
}

type gatewayCandidateScopeContextKey struct{}

func withGatewayCandidateScope(ctx context.Context, scope string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if GatewayRetryAccountFromContext(ctx) > 0 {
		ctx = NewGatewayRequestAttemptContext(ctx)
	}
	return context.WithValue(ctx, gatewayCandidateScopeContextKey{}, scope)
}

func gatewayCandidateScope(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	scope, _ := ctx.Value(gatewayCandidateScopeContextKey{}).(string)
	return scope
}

func gatewayAttemptNeedsCandidateList(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState)
	return state != nil || GatewayRetryAccountFromContext(ctx) > 0
}

func gatewayAttemptNeedsAuthoritativeCandidates(ctx context.Context) bool {
	if !gatewayAttemptNeedsCandidateList(ctx) {
		return false
	}
	state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState)
	scope := gatewayCandidateScope(ctx)
	if state != nil && scope != "" {
		state.mu.Lock()
		_, authoritative := state.authoritativeScopes[scope]
		candidateCount := len(state.accountIDs[scope])
		state.mu.Unlock()
		if authoritative {
			return true
		}
		if candidateCount > 0 {
			return false
		}
	} else if gatewayAttemptCandidateCount(ctx) > 0 {
		return false
	}
	// Snapshot metadata can be stale, including the pool-mode flag itself.
	// Establish the fixed request universe from the repository before freezing
	// IDs instead of inferring completeness from the returned snapshot subset.
	return markGatewayAttemptAuthoritativeScope(state, scope)
}

func markGatewayAttemptAuthoritativeScope(state *gatewayRequestAttemptState, scope string) bool {
	if state == nil || scope == "" {
		return true
	}
	state.mu.Lock()
	if state.authoritativeScopes == nil {
		state.authoritativeScopes = make(map[string]struct{})
	}
	state.authoritativeScopes[scope] = struct{}{}
	state.mu.Unlock()
	return true
}

func gatewayAttemptUsesAuthoritativeCandidates(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState)
	scope := gatewayCandidateScope(ctx)
	if state == nil || scope == "" {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	_, authoritative := state.authoritativeScopes[scope]
	return authoritative
}

func listSchedulableGatewayCandidatesFromRepository(
	ctx context.Context,
	repo AccountRepository,
	cfg *config.Config,
	groupID *int64,
	platform string,
	hasForcePlatform bool,
) ([]Account, bool, error) {
	useMixed := (platform == PlatformAnthropic || platform == PlatformGemini) && !hasForcePlatform
	if useMixed {
		platforms := []string{platform, PlatformAntigravity}
		var accounts []Account
		var err error
		if groupID != nil {
			accounts, err = repo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, platforms)
		} else if cfg != nil && cfg.RunMode == config.RunModeSimple {
			accounts, err = repo.ListSchedulableByPlatforms(ctx, platforms)
		} else {
			accounts, err = repo.ListSchedulableUngroupedByPlatforms(ctx, platforms)
		}
		if err != nil {
			return nil, useMixed, err
		}
		filtered := make([]Account, 0, len(accounts))
		for _, account := range accounts {
			if account.Platform == PlatformAntigravity && !account.IsMixedSchedulingEnabled() {
				continue
			}
			filtered = append(filtered, account)
		}
		return filtered, useMixed, nil
	}

	var accounts []Account
	var err error
	if cfg != nil && cfg.RunMode == config.RunModeSimple {
		accounts, err = repo.ListSchedulableByPlatform(ctx, platform)
	} else if groupID != nil {
		accounts, err = repo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, platform)
	} else {
		accounts, err = repo.ListSchedulableUngroupedByPlatform(ctx, platform)
	}
	return accounts, false, err
}

// Freeze only IDs, never account objects: each retry still consumes current
// account state from the ordinary eligibility and admission pipeline.
func constrainGatewayAttemptAccounts(ctx context.Context, scope string, accounts []Account) []Account {
	if ctx == nil {
		return accounts
	}
	pin := GatewayRetryAccountFromContext(ctx)
	state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState)
	var universe map[int64]struct{}
	if state != nil {
		state.mu.Lock()
		universe = state.accountIDs[scope]
		if universe == nil {
			universe = make(map[int64]struct{}, len(accounts))
			for i := range accounts {
				universe[accounts[i].ID] = struct{}{}
			}
			state.accountIDs[scope] = universe
		}
		state.mu.Unlock()
	}
	if state == nil && pin == 0 {
		return accounts
	}
	filtered := make([]Account, 0, len(accounts))
	for i := range accounts {
		if pin > 0 && accounts[i].ID != pin {
			continue
		}
		if universe != nil {
			if _, ok := universe[accounts[i].ID]; !ok {
				continue
			}
		}
		filtered = append(filtered, accounts[i])
	}
	return filtered
}

func gatewayAttemptAllowsAccount(ctx context.Context, accountID int64) bool {
	if pin := GatewayRetryAccountFromContext(ctx); pin > 0 && pin != accountID {
		return false
	}
	if ctx == nil {
		return true
	}
	state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState)
	scope := gatewayCandidateScope(ctx)
	if state == nil || scope == "" {
		return true
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	universe, exists := state.accountIDs[scope]
	if !exists {
		return true
	}
	_, allowed := universe[accountID]
	return allowed
}

func gatewayRetrySelectionError(ctx context.Context, err error) error {
	if err == nil || GatewayRetryAccountFromContext(ctx) == 0 {
		return err
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrSameAccountRetryUnavailable) {
		return err
	}
	if !errors.Is(err, ErrNoAvailableAccounts) && !errors.Is(err, ErrNoAvailableCompactAccounts) && !errors.Is(err, ErrAccountNotFound) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrSameAccountRetryUnavailable, err)
}

func gatewayAccountMatchesRetryGroup(account *Account, groupID *int64, cfg *config.Config) bool {
	if cfg != nil && cfg.RunMode == config.RunModeSimple {
		return account != nil
	}
	return openAIStickyAccountMatchesGroup(account, groupID)
}

func (s *GatewayService) constrainGatewayRetryCandidates(ctx context.Context, groupID *int64, platform string, useMixed bool, accounts []Account) ([]Account, error) {
	scope := gatewayCandidateScope(ctx)
	if scope == "" {
		scope = fmt.Sprintf("gateway:%d:%s:%t", derefGroupID(groupID), platform, useMixed)
	}
	accounts = constrainGatewayAttemptAccounts(ctx, scope, accounts)
	pin := GatewayRetryAccountFromContext(ctx)
	if pin == 0 || len(accounts) == 0 {
		return accounts, nil
	}
	fresh, err := s.getSchedulableAccount(ctx, pin)
	if err != nil {
		return nil, err
	}
	if fresh == nil || !fresh.IsSchedulable() || !gatewayAccountMatchesRetryGroup(fresh, groupID, s.cfg) || !s.isAccountAllowedForPlatform(fresh, platform, useMixed) {
		return nil, nil
	}
	return []Account{*fresh}, nil
}

func (s *OpenAIGatewayService) constrainOpenAIRetryCandidates(ctx context.Context, groupID *int64, platform string, accounts []Account) ([]Account, error) {
	scope := gatewayCandidateScope(ctx)
	if scope == "" {
		scope = fmt.Sprintf("openai:%d:%s", derefGroupID(groupID), platform)
	}
	accounts = constrainGatewayAttemptAccounts(ctx, scope, accounts)
	pin := GatewayRetryAccountFromContext(ctx)
	if pin == 0 || len(accounts) == 0 {
		return accounts, nil
	}
	fresh, err := s.getSchedulableAccount(ctx, pin)
	if err != nil {
		return nil, err
	}
	if fresh == nil || !fresh.IsSchedulable() || !s.openAIAccountMatchesSchedulingGroup(fresh, groupID) || fresh.Platform != NormalizeOpenAICompatiblePlatform(platform) {
		return nil, nil
	}
	return []Account{*fresh}, nil
}

func (s *GatewayService) validateGatewayRetrySelection(ctx context.Context, account *Account, groupID *int64, platform, requestedModel, sessionHash string, useMixed bool) (*Account, error) {
	pin := GatewayRetryAccountFromContext(ctx)
	if pin == 0 {
		return account, nil
	}
	if account == nil || account.ID != pin || !gatewayAttemptAllowsAccount(ctx, account.ID) {
		return nil, ErrSameAccountRetryUnavailable
	}
	fresh, err := s.getSchedulableAccount(ctx, pin)
	if err != nil {
		return nil, err
	}
	if fresh == nil || !gatewayAccountMatchesRetryGroup(fresh, groupID, s.cfg) || !s.isAccountAllowedForPlatform(fresh, platform, useMixed) || !s.isAccountSchedulableForSelection(fresh) || !s.isAccountSchedulableForModelSelection(ctx, fresh, requestedModel) || !s.isAccountSchedulableForQuota(fresh) || !s.isGatewayAccountProfitEligible(ctx, fresh) {
		return nil, ErrSameAccountRetryUnavailable
	}
	if requestedModel != "" && !s.isModelSupportedByAccountWithContext(ctx, fresh, requestedModel) {
		return nil, ErrSameAccountRetryUnavailable
	}
	if !s.isAccountSchedulableForWindowCost(ctx, fresh, sessionHash != "") || !s.isAccountSchedulableForRPM(ctx, fresh, sessionHash != "") || s.isStickyAccountUpstreamRestricted(ctx, groupID, fresh, requestedModel) {
		return nil, ErrSameAccountRetryUnavailable
	}
	if group := s.groupFromContext(ctx, derefGroupID(groupID)); group != nil && group.RequirePrivacySet && !fresh.IsPrivacySet() {
		return nil, ErrSameAccountRetryUnavailable
	}
	return fresh, nil
}

func (s *OpenAIGatewayService) validateOpenAIRetrySelection(ctx context.Context, selection *AccountSelectionResult, req OpenAIAccountScheduleRequest) error {
	pin := GatewayRetryAccountFromContext(ctx)
	if pin == 0 {
		return nil
	}
	if selection == nil || selection.Account == nil || selection.Account.ID != pin || !gatewayAttemptAllowsAccount(ctx, pin) {
		return ErrSameAccountRetryUnavailable
	}
	fresh, err := s.getSchedulableAccount(ctx, pin)
	if err != nil {
		return err
	}
	if fresh == nil || !s.openAIAccountMatchesSchedulingGroup(fresh, req.GroupID) || !isOpenAICompatibleAccountEligibleForRequest(ctx, fresh, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability) {
		return ErrSameAccountRetryUnavailable
	}
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	if !scheduler.isAccountRequestCompatible(ctx, fresh, req) || !scheduler.isAccountTransportCompatible(fresh, req.RequiredTransport) {
		return ErrSameAccountRetryUnavailable
	}
	selection.Account = fresh
	return nil
}

func gatewayAttemptCandidateCount(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState)
	scope := gatewayCandidateScope(ctx)
	if state == nil || scope == "" {
		return 0
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return len(state.accountIDs[scope])
}
