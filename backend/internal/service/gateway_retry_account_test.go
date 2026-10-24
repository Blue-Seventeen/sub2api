//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type gatewayRetryTestRepo struct {
	AccountRepository
	accounts []Account
	fresh    map[int64]Account
	readErr  error
	missing  int64
}

func (r *gatewayRetryTestRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if id == r.missing {
		return nil, ErrAccountNotFound
	}
	if r.readErr != nil {
		return nil, r.readErr
	}
	if account, ok := r.fresh[id]; ok {
		return &account, nil
	}
	for _, account := range r.accounts {
		if account.ID == id {
			return &account, nil
		}
	}
	return nil, errors.New("account not found")
}
func (r *gatewayRetryTestRepo) listed(platforms ...string) []Account {
	var out []Account
	for _, account := range r.accounts {
		for _, platform := range platforms {
			if account.Platform == platform {
				out = append(out, account)
				break
			}
		}
	}
	return out
}
func (r *gatewayRetryTestRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]Account, error) {
	return r.listed(platform), nil
}
func (r *gatewayRetryTestRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]Account, error) {
	return r.listed(platforms...), nil
}
func (r *gatewayRetryTestRepo) ListSchedulableByGroupID(_ context.Context, _ int64) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}
func (r *gatewayRetryTestRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	return r.listed(platform), nil
}
func (r *gatewayRetryTestRepo) ListSchedulableByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.listed(platforms...), nil
}
func (r *gatewayRetryTestRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]Account, error) {
	return r.listed(platform), nil
}
func (r *gatewayRetryTestRepo) ListSchedulableUngroupedByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.listed(platforms...), nil
}

func gatewayRetryTestAccount(id int64, platform string, priority int) Account {
	return Account{ID: id, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: priority}
}

// Without an explicit retry pin the normal scheduler picks B, even though the
// handler has charged the retry to A. Cover both scheduler families and Images.
func TestGatewayRetryAccount_PinsActualSelectionWithoutStickySession(t *testing.T) {
	for _, family := range []string{"gateway_model", "gateway_load", "openai_legacy", "openai_advanced", "images_legacy", "images_advanced"} {
		t.Run(family, func(t *testing.T) {
			platform, model := PlatformOpenAI, "gpt-5.1"
			if family == "gateway_model" || family == "gateway_load" {
				platform, model = PlatformAnthropic, "claude-sonnet-4-5"
			}
			a := gatewayRetryTestAccount(97001, platform, 20)
			b := gatewayRetryTestAccount(97002, platform, 0)
			repo := &gatewayRetryTestRepo{accounts: []Account{a, b}}
			ctx := WithGatewayRetryAccount(NewGatewayRequestAttemptContext(context.Background()), a.ID)
			cfg := &config.Config{}
			var selected *Account
			var release func()
			if platform == PlatformAnthropic {
				svc := &GatewayService{accountRepo: repo, cfg: cfg}
				if family == "gateway_model" {
					got, err := svc.SelectAccountForModelWithExclusions(ctx, nil, "", model, nil)
					require.NoError(t, err)
					selected = got
				} else {
					got, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", model, nil, "", 0)
					require.NoError(t, err)
					selected, release = got.Account, got.ReleaseFunc
				}
			} else {
				enabled := "false"
				if family == "openai_advanced" || family == "images_advanced" {
					enabled = "true"
				}
				svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(enabled)}
				var got *AccountSelectionResult
				var err error
				if family == "images_legacy" || family == "images_advanced" {
					got, _, err = svc.SelectAccountWithSchedulerForImages(ctx, nil, "", "gpt-image-2", nil, OpenAIImagesCapabilityNative)
				} else {
					got, _, err = svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", model, nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
				}
				require.NoError(t, err)
				require.NotNil(t, got)
				selected, release = got.Account, got.ReleaseFunc
			}
			if release != nil {
				defer release()
			}
			require.NotNil(t, selected)
			require.Equal(t, int64(97001), selected.ID)
		})
	}
}

func TestGatewayRetryAccount_UnavailableDoesNotFallThroughToHealthyBackup(t *testing.T) {
	for _, change := range []string{"disabled", "cooldown", "wrong_group", "model_removed"} {
		t.Run(change, func(t *testing.T) {
			groupID := int64(771)
			a, b := gatewayRetryTestAccount(97011, PlatformOpenAI, 0), gatewayRetryTestAccount(97012, PlatformOpenAI, 10)
			a.GroupIDs, b.GroupIDs = []int64{groupID}, []int64{groupID}
			fresh := a
			switch change {
			case "disabled":
				fresh.Schedulable = false
			case "cooldown":
				until := time.Now().Add(time.Minute)
				fresh.TempUnschedulableUntil = &until
			case "wrong_group":
				fresh.GroupIDs = []int64{772}
			case "model_removed":
				fresh.Credentials = map[string]any{"model_mapping": map[string]any{"different-model": "different-model"}}
			}
			repo := &gatewayRetryTestRepo{accounts: []Account{a, b}, fresh: map[int64]Account{a.ID: fresh}}
			snapshot := &openAISnapshotCacheStub{snapshotAccounts: []*Account{&a, &b}, accountsByID: map[int64]*Account{a.ID: &a, b.ID: &b}}
			acquired := []int64{}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, schedulerSnapshot: &SchedulerSnapshotService{cache: snapshot}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired}), rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true")}
			got, _, err := svc.SelectAccountWithSchedulerForCapability(WithGatewayRetryAccount(context.Background(), a.ID), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
			if got != nil && got.ReleaseFunc != nil {
				got.ReleaseFunc()
			}
			require.ErrorIs(t, err, ErrSameAccountRetryUnavailable)
			require.Nil(t, got)
			require.NotContains(t, acquired, b.ID)
		})
	}
}

func TestGatewayRetryAccount_DeletedPinnedAccountAllowsHealthyBackup(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			a, b := gatewayRetryTestAccount(97501, platform, 0), gatewayRetryTestAccount(97502, platform, 1)
			repo := &gatewayRetryTestRepo{accounts: []Account{a, b}, missing: a.ID}
			ctx := WithGatewayRetryAccount(NewGatewayRequestAttemptContext(context.Background()), a.ID)
			var retryErr error
			var backup *Account
			if platform == PlatformAnthropic {
				svc := &GatewayService{accountRepo: repo, cfg: &config.Config{}}
				_, retryErr = svc.SelectAccountForModelWithExclusions(ctx, nil, "", "claude-sonnet-4-5", nil)
				var err error
				backup, err = svc.SelectAccountForModelWithExclusions(WithGatewayRetryAccount(ctx, 0), nil, "", "claude-sonnet-4-5", map[int64]struct{}{a.ID: {}})
				require.NoError(t, err)
			} else {
				svc := &OpenAIGatewayService{accountRepo: repo, cfg: &config.Config{}, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("false")}
				_, _, retryErr = svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(WithGatewayRetryAccount(ctx, 0), nil, "", "", "gpt-5.1", map[int64]struct{}{a.ID: {}}, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
				require.NoError(t, err)
				require.NotNil(t, selection)
				backup = selection.Account
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
			}
			require.ErrorIs(t, retryErr, ErrSameAccountRetryUnavailable, "a deleted retry target must be an account-local rejection, not a terminal DB failure")
			require.NotNil(t, backup)
			require.Equal(t, b.ID, backup.ID)
		})
	}
}

func TestGatewayRequestAttemptContext_DoesNotExpandCandidateUniverseDuringFailover(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			a, b := gatewayRetryTestAccount(97021, platform, 0), gatewayRetryTestAccount(97022, platform, 10)
			repo := &gatewayRetryTestRepo{accounts: []Account{a, b}}
			ctx := NewGatewayRequestAttemptContext(context.Background())
			cfg := &config.Config{}
			generic := &GatewayService{accountRepo: repo, cfg: cfg}
			openai := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("false")}
			selectAccount := func(excluded map[int64]struct{}) (*AccountSelectionResult, error) {
				if platform == PlatformAnthropic {
					return generic.SelectAccountWithLoadAwareness(ctx, nil, "", "claude-sonnet-4-5", excluded, "", 0)
				}
				got, _, err := openai.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-5.1", excluded, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
				return got, err
			}
			first, err := selectAccount(nil)
			require.NoError(t, err)
			require.Equal(t, int64(97021), first.Account.ID)
			if first.ReleaseFunc != nil {
				first.ReleaseFunc()
			}
			repo.accounts = append(repo.accounts, gatewayRetryTestAccount(97023, platform, -10))
			second, err := selectAccount(map[int64]struct{}{97021: {}})
			require.NoError(t, err)
			require.Equal(t, int64(97022), second.Account.ID)
			if second.ReleaseFunc != nil {
				second.ReleaseFunc()
			}
			last, err := selectAccount(map[int64]struct{}{97021: {}, 97022: {}})
			require.Error(t, err)
			require.Nil(t, last)
		})
	}
}

func TestGatewayRetryAccount_ClearPinRestoresOrdinarySelection(t *testing.T) {
	repo := &gatewayRetryTestRepo{accounts: []Account{gatewayRetryTestAccount(97031, PlatformOpenAI, 20), gatewayRetryTestAccount(97032, PlatformOpenAI, 0)}}
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: &config.Config{}, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("false")}
	ctx := WithGatewayRetryAccount(context.Background(), 97031)
	ctx = WithGatewayRetryAccount(ctx, 0)
	require.Zero(t, GatewayRetryAccountFromContext(ctx))
	got, _, err := svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
	require.NoError(t, err)
	require.Equal(t, int64(97032), got.Account.ID)
	if got.ReleaseFunc != nil {
		got.ReleaseFunc()
	}
}

func TestOAuth429Failover_DoesNotStopBeforeLastHealthyAccount(t *testing.T) {
	svc := &OpenAIGatewayService{}
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			var state OpenAIOAuth429FailoverState
			reachedHealthy := false
			for accountID := 1; accountID <= 6; accountID++ {
				if accountID == 6 {
					reachedHealthy = true
					break
				}
				account := &Account{ID: int64(accountID), Platform: platform, Type: AccountTypeOAuth}
				if svc.ShouldStopOpenAIOAuth429Failover(account, http.StatusTooManyRequests, accountID, &state) {
					break
				}
			}
			require.True(t, reachedHealthy, "an account-local 429 must not hide an untried healthy candidate")
		})
	}
}

func TestGatewayRetryAccount_TokenCountKeepsPinAndExclusionsWithoutGenerationSlot(t *testing.T) {
	a, b := gatewayRetryTestAccount(97041, PlatformOpenAI, 20), gatewayRetryTestAccount(97042, PlatformOpenAI, 0)
	acquired := []int64{}
	svc := &OpenAIGatewayService{accountRepo: &gatewayRetryTestRepo{accounts: []Account{a, b}}, cfg: &config.Config{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired})}
	ctx := WithGatewayRetryAccount(NewGatewayRequestAttemptContext(context.Background()), a.ID)
	got, err := svc.SelectAccountForTokenCount(ctx, nil, "", "gpt-5.1", OpenAIEndpointCapabilityChatCompletions, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, int64(97041), got.ID)
	require.Empty(t, acquired)
	_, err = svc.SelectAccountForTokenCount(ctx, nil, "", "gpt-5.1", OpenAIEndpointCapabilityChatCompletions, PlatformOpenAI, map[int64]struct{}{97041: {}})
	require.ErrorIs(t, err, ErrSameAccountRetryUnavailable)
	require.Empty(t, acquired)
}

func TestGatewayRetryAccount_InfrastructureFailureDoesNotBecomeAccountExhaustion(t *testing.T) {
	dbErr := errors.New("database unavailable")
	a := gatewayRetryTestAccount(97051, PlatformOpenAI, 0)
	repo := &gatewayRetryTestRepo{accounts: []Account{a}, readErr: dbErr}
	svc := &OpenAIGatewayService{accountRepo: repo, cfg: &config.Config{}, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("false")}
	selected, _, err := svc.SelectAccountWithSchedulerForCapability(WithGatewayRetryAccount(context.Background(), a.ID), nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
	require.ErrorIs(t, err, dbErr)
	require.NotErrorIs(t, err, ErrSameAccountRetryUnavailable)
	require.Nil(t, selected)
}
func TestGatewayRequestAttemptContext_AdvancedSchedulerReachesLastOfSeventyFreshCandidates(t *testing.T) {
	groupID := int64(779)
	repo := &gatewayRetryTestRepo{fresh: make(map[int64]Account)}
	snapshot := &openAISnapshotCacheStub{accountsByID: make(map[int64]*Account)}
	for i := 0; i < 70; i++ {
		account := gatewayRetryTestAccount(int64(97100+i), PlatformOpenAI, i)
		account.GroupIDs = []int64{groupID}
		repo.accounts = append(repo.accounts, account)
		snapshotCopy := account
		snapshot.snapshotAccounts = append(snapshot.snapshotAccounts, &snapshotCopy)
		snapshot.accountsByID[account.ID] = &snapshotCopy
		if i < 69 {
			account.Schedulable = false
		}
		repo.fresh[account.ID] = account
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.OpenAIWS.LBTopK = 1
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	acquired, released := []int64{}, []int64{}
	svc := &OpenAIGatewayService{
		accountRepo: repo, cfg: cfg,
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshot},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired, releasedIDs: &released}),
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
	}
	got, _, err := svc.SelectAccountWithSchedulerForCapability(NewGatewayRequestAttemptContext(context.Background()), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, int64(97169), got.Account.ID, "all earlier snapshot candidates were disabled in the authoritative DB")
	require.NotNil(t, got.ReleaseFunc)
	got.ReleaseFunc()
	require.Equal(t, len(acquired), len(released), "every rejected stale-account slot and the final slot must be released")
}

func TestGatewayRequestAttemptContext_PoolModeUsesAuthoritativeCandidateList(t *testing.T) {
	groupID := int64(780)
	for _, platform := range []string{PlatformOpenAI, PlatformAli} {
		t.Run(platform, func(t *testing.T) {
			repo := &gatewayRetryTestRepo{}
			snapshot := &openAISnapshotCacheStub{}
			for i := 0; i < 12; i++ {
				account := gatewayRetryTestAccount(int64(97200+i), platform, i)
				account.GroupIDs = []int64{groupID}
				account.Credentials = map[string]any{"pool_mode": true}
				repo.accounts = append(repo.accounts, account)
				if i < 3 {
					copy := account
					snapshot.snapshotAccounts = append(snapshot.snapshotAccounts, &copy)
				}
			}
			ctx := NewGatewayRequestAttemptContext(context.Background())
			cfg := &config.Config{RunMode: config.RunModeStandard}
			var accounts []Account
			var err error
			if platform == PlatformOpenAI {
				svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, schedulerSnapshot: &SchedulerSnapshotService{cache: snapshot}}
				accounts, err = svc.listSchedulableAccounts(ctx, &groupID, platform)
			} else {
				svc := &GatewayService{accountRepo: repo, cfg: cfg, schedulerSnapshot: &SchedulerSnapshotService{cache: snapshot}}
				accounts, _, err = svc.listSchedulableAccounts(ctx, &groupID, platform, false)
			}
			require.NoError(t, err)
			require.Len(t, accounts, 12, "the request candidate universe must not freeze a partial pool-mode snapshot")
		})
	}
}

func TestGatewayRequestAttemptContext_StalePoolFlagCannotFreezePartialSnapshot(t *testing.T) {
	groupID := int64(782)
	for _, platform := range []string{PlatformOpenAI, PlatformAli} {
		for _, staleFlag := range []string{"missing", "false"} {
			t.Run(platform+"/"+staleFlag, func(t *testing.T) {
				repo := &gatewayRetryTestRepo{}
				snapshot := &openAISnapshotCacheStub{}
				for i := 0; i < 12; i++ {
					account := gatewayRetryTestAccount(int64(97400+i), platform, i)
					account.GroupIDs = []int64{groupID}
					account.Credentials = map[string]any{"pool_mode": true}
					repo.accounts = append(repo.accounts, account)
					if i < 3 {
						stale := account
						stale.Credentials = map[string]any{}
						if staleFlag == "false" {
							stale.Credentials["pool_mode"] = false
						}
						snapshot.snapshotAccounts = append(snapshot.snapshotAccounts, &stale)
					}
				}
				ctx := NewGatewayRequestAttemptContext(context.Background())
				cfg := &config.Config{RunMode: config.RunModeStandard}
				var accounts []Account
				var err error
				if platform == PlatformOpenAI {
					svc := &OpenAIGatewayService{accountRepo: repo, cfg: cfg, schedulerSnapshot: &SchedulerSnapshotService{cache: snapshot}}
					accounts, err = svc.listSchedulableAccounts(ctx, &groupID, platform)
				} else {
					svc := &GatewayService{accountRepo: repo, cfg: cfg, schedulerSnapshot: &SchedulerSnapshotService{cache: snapshot}}
					accounts, _, err = svc.listSchedulableAccounts(ctx, &groupID, platform, false)
				}
				require.NoError(t, err)
				require.Len(t, accounts, 12, "stale pool metadata must not hide the remaining eligible group accounts")
			})
		}
	}
}

func TestGatewayRequestAttemptContext_PoolModeFailoverUsesAuthoritativeCandidates(t *testing.T) {
	groupID := int64(781)
	for _, platform := range []string{PlatformDeepSeek} {
		t.Run(platform, func(t *testing.T) {
			repo := &gatewayRetryTestRepo{}
			snapshot := &openAISnapshotCacheStub{}
			for i := 0; i < 12; i++ {
				account := gatewayRetryTestAccount(int64(97300+i), platform, i)
				account.GroupIDs = []int64{groupID}
				account.Credentials = map[string]any{"api_key": "fixture-only", "pool_mode": true}
				repo.accounts = append(repo.accounts, account)
				if i < 3 {
					copy := account
					snapshot.snapshotAccounts = append(snapshot.snapshotAccounts, &copy)
				}
			}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			svc := &OpenAIGatewayService{
				accountRepo: repo, cfg: cfg,
				schedulerSnapshot: &SchedulerSnapshotService{cache: snapshot, accountRepo: repo},
				rateLimitService:  newOpenAIAdvancedSchedulerRateLimitService("false"),
			}
			ctx := NewGatewayRequestAttemptContext(context.Background())
			excluded := make(map[int64]struct{}, len(repo.accounts))
			for i := 0; i < len(repo.accounts); i++ {
				selection, _, err := svc.SelectAccountWithSchedulerForCapability(
					ctx, &groupID, "", "", "deepseek-chat", excluded,
					OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions,
					false, false, true, platform,
				)
				if err != nil {
					t.Fatalf("selection %d failed before exhausting the authoritative group: %v", i+1, err)
				}
				require.NotNil(t, selection)
				require.NotNil(t, selection.Account)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				excluded[selection.Account.ID] = struct{}{}
				if i == 0 {
					stale := gatewayRetryTestAccount(selection.Account.ID, platform, 0)
					stale.GroupIDs = []int64{groupID}
					stale.Credentials = map[string]any{"api_key": "fixture-only"}
					snapshot.snapshotAccounts = []*Account{&stale}
				}
			}
			require.Len(t, excluded, len(repo.accounts), "failover must be able to reach every frozen eligible candidate")
		})
	}
}

type gatewayRetryAcquireErrorCache struct {
	schedulerTestConcurrencyCache
	err error
}

func (c gatewayRetryAcquireErrorCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return false, c.err
}

func TestGatewayRetryAccount_ConcurrencyInfrastructureErrorDoesNotBecomeWaitPlan(t *testing.T) {
	cacheErr := errors.New("concurrency cache unavailable")
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			a := gatewayRetryTestAccount(97061, platform, 0)
			repo := &gatewayRetryTestRepo{accounts: []Account{a}}
			concurrency := NewConcurrencyService(gatewayRetryAcquireErrorCache{err: cacheErr})
			ctx := WithGatewayRetryAccount(context.Background(), a.ID)
			var got *AccountSelectionResult
			var err error
			if platform == PlatformAnthropic {
				svc := &GatewayService{accountRepo: repo, cfg: &config.Config{}, concurrencyService: concurrency}
				got, err = svc.SelectAccountWithLoadAwareness(ctx, nil, "", "claude-sonnet-4-5", nil, "", 0)
			} else {
				svc := &OpenAIGatewayService{accountRepo: repo, cfg: &config.Config{}, concurrencyService: concurrency, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("false")}
				got, _, err = svc.SelectAccountWithSchedulerForCapability(ctx, nil, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityChatCompletions, false, false, true)
			}
			if got != nil && got.ReleaseFunc != nil {
				got.ReleaseFunc()
			}
			require.ErrorIs(t, err, cacheErr)
			require.NotErrorIs(t, err, ErrSameAccountRetryUnavailable)
			require.Nil(t, got)
		})
	}
}
