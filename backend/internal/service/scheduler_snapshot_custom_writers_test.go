//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/stretchr/testify/require"
)

type snapshotBackfillCodeAssist struct{ GeminiCliCodeAssistClient }

func (snapshotBackfillCodeAssist) LoadCodeAssist(context.Context, string, string, *geminicli.LoadCodeAssistRequest) (*geminicli.LoadCodeAssistResponse, error) {
	return &geminicli.LoadCodeAssistResponse{CloudAICompanionProject: "snapshot-project"}, nil
}

func sharedBackfillAccount(platform string) *Account {
	account := &Account{
		ID: 7, Platform: platform, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":           "snapshot-test-token",
			"expires_at":             fmt.Sprint(time.Now().Add(time.Hour).Unix()),
			"auto_detect_project_id": "true",
			"model_mapping":          map[string]any{"alias": "mapped"},
		},
	}
	account.PrepareSharedSnapshot()
	return account
}

func TestGeminiTokenProvider_BackfillDoesNotMutateSharedSnapshot(t *testing.T) {
	master := sharedBackfillAccount(PlatformGemini)
	replica := *master
	provider := &GeminiTokenProvider{geminiOAuthService: &GeminiOAuthService{codeAssist: snapshotBackfillCodeAssist{}}}
	token, err := provider.GetAccessToken(context.Background(), &replica)
	require.NoError(t, err)
	require.Equal(t, "snapshot-test-token", token)
	require.Equal(t, "snapshot-project", replica.GetCredential("project_id"))
	require.NotContains(t, master.Credentials, "project_id")
	require.NotContains(t, master.Credentials, "tier_id")
	require.Equal(t, "mapped", replica.GetMappedModel("alias"))
	require.Equal(t, "mapped", master.GetMappedModel("alias"))
}

func TestAntigravityTokenProvider_BackfillDoesNotMutateSharedSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1internal:loadCodeAssist", r.URL.Path)
		_, _ = w.Write([]byte(`{"cloudaicompanionProject":"snapshot-project"}`))
	}))
	t.Cleanup(server.Close)
	oldURLs := antigravity.BaseURLs
	antigravity.BaseURLs = []string{server.URL}
	t.Cleanup(func() { antigravity.BaseURLs = oldURLs })
	master := sharedBackfillAccount(PlatformAntigravity)
	replica := *master
	provider := &AntigravityTokenProvider{antigravityOAuthService: &AntigravityOAuthService{}}
	token, err := provider.GetAccessToken(context.Background(), &replica)
	require.NoError(t, err)
	require.Equal(t, "snapshot-test-token", token)
	require.Equal(t, "snapshot-project", replica.GetCredential("project_id"))
	require.NotContains(t, master.Credentials, "project_id")
}

func TestAntigravityModelRateLimitCache_DoesNotMutateSharedSnapshot(t *testing.T) {
	limits := map[string]any{"old": map[string]any{"rate_limit_reset_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}
	master := &Account{ID: 7, Platform: PlatformAntigravity, Extra: map[string]any{modelRateLimitsKey: limits, "keep": true}}
	master.PrepareSharedSnapshot()
	replica := *master
	svc := &AntigravityGatewayService{schedulerSnapshot: &SchedulerSnapshotService{}}
	svc.updateAccountModelRateLimitInCache(context.Background(), &replica, "new", time.Now().Add(time.Hour))
	require.Len(t, limits, 1)
	require.NotContains(t, limits, "new")
	require.Contains(t, replica.Extra[modelRateLimitsKey], "new")
	require.Contains(t, replica.Extra[modelRateLimitsKey], "old")
	require.Equal(t, true, replica.Extra["keep"])
}

func TestAntigravityClearCreditsExhausted_DoesNotMutateSharedSnapshot(t *testing.T) {
	limits := map[string]any{creditsExhaustedKey: map[string]any{"rate_limit_reset_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, "keep": true}
	master := &Account{ID: 7, Platform: PlatformAntigravity, Extra: map[string]any{modelRateLimitsKey: limits, "keep": true}}
	master.PrepareSharedSnapshot()
	replica := *master
	repo := &stubAntigravityAccountRepo{}
	svc := &AntigravityGatewayService{accountRepo: repo}
	svc.clearCreditsExhausted(context.Background(), &replica)
	require.Contains(t, limits, creditsExhaustedKey)
	require.NotContains(t, replica.Extra[modelRateLimitsKey], creditsExhaustedKey)
	require.Contains(t, replica.Extra[modelRateLimitsKey], "keep")
	require.Equal(t, true, replica.Extra["keep"])
	require.Len(t, repo.extraUpdateCalls, 1)
}
