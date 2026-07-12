package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type openAIRefreshCall struct {
	refreshToken string
	proxyURL     string
	clientID     string
}

type openAIRefreshScript struct {
	resp *openai.TokenResponse
	err  error
}

type openaiOAuthClientRefreshStub struct {
	refreshCalls int32
	scripts      []openAIRefreshScript
	calls        []openAIRefreshCall
}

func (s *openaiOAuthClientRefreshStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, infraerrors.InternalServer("NOT_IMPLEMENTED", "not implemented")
}

func (s *openaiOAuthClientRefreshStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, openai.ClientID)
}

func (s *openaiOAuthClientRefreshStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	atomic.AddInt32(&s.refreshCalls, 1)
	s.calls = append(s.calls, openAIRefreshCall{
		refreshToken: refreshToken,
		proxyURL:     proxyURL,
		clientID:     clientID,
	})
	if len(s.scripts) == 0 {
		return nil, infraerrors.InternalServer("MISSING_SCRIPT", "missing refresh script")
	}
	script := s.scripts[0]
	s.scripts = s.scripts[1:]
	if script.err != nil {
		return nil, script.err
	}
	return script.resp, nil
}

func TestOpenAIOAuthService_RefreshAccountToken_NoRefreshTokenUsesExistingAccessToken(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{}
	svc := NewOpenAIOAuthService(nil, client)
	var privacyClientCalls int32
	svc.SetPrivacyClientFactory(func(proxyURL string) (*req.Client, error) {
		atomic.AddInt32(&privacyClientCalls, 1)
		return nil, errors.New("stop before request")
	})

	expiresAt := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339)
	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "existing-access-token",
			"expires_at":   expiresAt,
			"client_id":    "client-id-1",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "existing-access-token", info.AccessToken)
	require.Equal(t, "client-id-1", info.ClientID)
	require.Zero(t, atomic.LoadInt32(&client.refreshCalls), "existing access token should be reused without calling refresh")
	require.Positive(t, atomic.LoadInt32(&privacyClientCalls), "existing access token should still run enrichment")
}

func TestOpenAIOAuthService_RefreshTokenWithClientID_FallbacksToOfficialClient(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{
		scripts: []openAIRefreshScript{
			{
				err: infraerrors.BadRequest("OPENAI_OAUTH_UNAUTHORIZED_CLIENT", "unauthorized client").WithMetadata(map[string]string{
					"oauth_error":     "unauthorized_client",
					"upstream_status": "401",
				}),
			},
			{
				resp: &openai.TokenResponse{
					AccessToken:  "new-at",
					RefreshToken: "new-rt",
					ExpiresIn:    3600,
				},
			},
		},
	}
	svc := NewOpenAIOAuthService(nil, client)

	info, err := svc.RefreshTokenWithClientID(context.Background(), "rt-1", "http://proxy.local:8080", "legacy-client-id")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, openai.ClientID, info.ClientID)
	require.Equal(t, "new-at", info.AccessToken)
	require.Len(t, client.calls, 2)
	require.Equal(t, "legacy-client-id", client.calls[0].clientID)
	require.Equal(t, openai.ClientID, client.calls[1].clientID)
	require.Equal(t, "http://proxy.local:8080", client.calls[0].proxyURL)
	require.Equal(t, "http://proxy.local:8080", client.calls[1].proxyURL)
}

func TestOpenAIOAuthService_RefreshAccountToken_PATIgnoresStaleRefreshToken(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{}
	var whoamiCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&whoamiCalls, 1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"email":"user@example.com",
			"chatgpt_user_id":"user-123",
			"chatgpt_account_id":"acct-123",
			"chatgpt_plan_type":"plus",
			"chatgpt_account_is_fedramp":false
		}`))
	}))
	defer server.Close()

	originalURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	defer func() { openAICodexPATWhoamiURL = originalURL }()

	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	account := &Account{
		ID:       77,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "at-test-token",
			"refresh_token": "stale-refresh-token",
			"expires_at":    time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"auth_mode":     "personal_access_token",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, OpenAIAuthModePersonalAccessToken, info.AuthMode)
	require.Equal(t, "at-test-token", info.AccessToken)
	require.Empty(t, info.RefreshToken)
	require.Equal(t, int32(1), atomic.LoadInt32(&whoamiCalls))
	require.Zero(t, atomic.LoadInt32(&client.refreshCalls), "PAT accounts must not call OAuth refresh even if stale refresh_token remains")
}

func TestOpenAITokenRefresher_NeedsRefresh_SkipsAccountWithoutRefreshToken(t *testing.T) {
	refresher := NewOpenAITokenRefresher(nil, nil)
	expiresAt := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)

	withoutRT := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "access-token",
			"expires_at":   expiresAt,
		},
	}
	require.False(t, refresher.NeedsRefresh(withoutRT, 5*time.Minute))

	withRT := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    expiresAt,
		},
	}
	require.True(t, refresher.NeedsRefresh(withRT, 5*time.Minute))

	patWithStaleRT := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "at-test-token",
			"refresh_token": "stale-refresh-token",
			"expires_at":    expiresAt,
			"auth_mode":     OpenAIAuthModePersonalAccessToken,
		},
	}
	require.False(t, refresher.NeedsRefresh(patWithStaleRT, 5*time.Minute))
}

func TestOpenAITokenRefresher_Refresh_PATRemovesStaleOAuthFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"email":"user@example.com",
			"chatgpt_user_id":"user-123",
			"chatgpt_account_id":"acct-123",
			"chatgpt_plan_type":"plus",
			"chatgpt_account_is_fedramp":true
		}`))
	}))
	defer server.Close()

	originalURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	defer func() { openAICodexPATWhoamiURL = originalURL }()

	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()
	refresher := NewOpenAITokenRefresher(svc, nil)

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "at-test-token",
			"refresh_token": "stale-refresh-token",
			"id_token":      "stale-id-token",
			"expires_at":    time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			"expires_in":    3600,
			"client_id":     "stale-client",
			"auth_mode":     OpenAIAuthModePersonalAccessToken,
			"model_mapping": map[string]any{"gpt-5": "gpt-5-codex"},
		},
	}

	credentials, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "at-test-token", credentials["access_token"])
	require.Equal(t, OpenAIAuthModePersonalAccessToken, credentials["auth_mode"])
	require.Equal(t, "personal_access_token", credentials["openai_auth_mode"])
	require.NotContains(t, credentials, "refresh_token")
	require.NotContains(t, credentials, "id_token")
	require.NotContains(t, credentials, "expires_at")
	require.NotContains(t, credentials, "expires_in")
	require.NotContains(t, credentials, "client_id")
	require.Equal(t, map[string]any{"gpt-5": "gpt-5-codex"}, credentials["model_mapping"])
}

func TestOpenAIOAuthService_RefreshTokenWithClientID_RetriesGatewayFailures(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{
		scripts: []openAIRefreshScript{
			{
				err: infraerrors.New(502, "OPENAI_OAUTH_REQUEST_FAILED", "request failed"),
			},
			{
				resp: &openai.TokenResponse{
					AccessToken:  "retry-at",
					RefreshToken: "retry-rt",
					ExpiresIn:    1800,
				},
			},
		},
	}
	svc := NewOpenAIOAuthService(nil, client)

	info, err := svc.RefreshTokenWithClientID(context.Background(), "rt-2", "http://proxy.local:8080", "")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "retry-at", info.AccessToken)
	require.Len(t, client.calls, 2)
	require.Equal(t, openai.ClientID, client.calls[0].clientID)
	require.Equal(t, openai.ClientID, client.calls[1].clientID)
}

func TestOpenAIOAuthService_RefreshTokenWithClientID_DoesNotRetryInvalidGrant(t *testing.T) {
	client := &openaiOAuthClientRefreshStub{
		scripts: []openAIRefreshScript{
			{
				err: infraerrors.Unauthorized("OPENAI_OAUTH_INVALID_GRANT", "expired").WithMetadata(map[string]string{
					"oauth_error":     "invalid_grant",
					"upstream_status": "400",
				}),
			},
		},
	}
	svc := NewOpenAIOAuthService(nil, client)

	info, err := svc.RefreshTokenWithClientID(context.Background(), "rt-3", "http://proxy.local:8080", "")
	require.Error(t, err)
	require.Nil(t, info)
	require.Len(t, client.calls, 1)
	require.Equal(t, httpStatusUnauthorized, infraerrors.Code(err))
}

const httpStatusUnauthorized = 401
