package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/imroc/req/v3"
)

// NewOpenAIOAuthClient creates a new OpenAI OAuth client
func NewOpenAIOAuthClient() service.OpenAIOAuthClient {
	return &openaiOAuthService{tokenURL: openai.TokenURL}
}

type openaiOAuthService struct {
	tokenURL string
}

type openAIOAuthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Code             string `json:"code"`
}

func (s *openaiOAuthService) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	client, err := createOpenAIReqClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}

	if redirectURI == "" {
		redirectURI = openai.DefaultRedirectURI
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openai.ClientID
	}

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("client_id", clientID)
	formData.Set("code", code)
	formData.Set("redirect_uri", redirectURI)
	formData.Set("code_verifier", codeVerifier)

	var tokenResp openai.TokenResponse

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", "codex-cli/0.91.0").
		SetFormDataFromValues(formData).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)

	if err != nil {
		if shouldReturnOpenAINoProxyHint(ctx, proxyURL, err) {
			return nil, newOpenAINoProxyHintError(err)
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}

	if !resp.IsSuccessState() {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_TOKEN_EXCHANGE_FAILED", "token exchange failed: status %d, body: %s", resp.StatusCode, resp.String())
	}

	return &tokenResp, nil
}

func (s *openaiOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, "")
}

func (s *openaiOAuthService) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	// Keep honoring a caller-provided client_id, but default to the official Codex client.
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = openai.ClientID
	}
	return s.refreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
}

func (s *openaiOAuthService) refreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL, clientID string) (*openai.TokenResponse, error) {
	client, err := createOpenAIReqClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}

	formData := url.Values{}
	formData.Set("grant_type", "refresh_token")
	formData.Set("refresh_token", refreshToken)
	formData.Set("client_id", clientID)
	formData.Set("scope", openai.RefreshScopes)

	var tokenResp openai.TokenResponse

	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", "codex-cli/0.91.0").
		SetFormDataFromValues(formData).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)

	if err != nil {
		if shouldReturnOpenAINoProxyHint(ctx, proxyURL, err) {
			return nil, newOpenAINoProxyHintError(err)
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}

	if !resp.IsSuccessState() {
		return nil, mapOpenAIRefreshHTTPError(resp.StatusCode, resp.String())
	}

	return &tokenResp, nil
}

func createOpenAIReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL: proxyURL,
		Timeout:  120 * time.Second,
	})
}

func mapOpenAIRefreshHTTPError(statusCode int, body string) error {
	oauthErr := parseOpenAIOAuthError(body)
	metadata := map[string]string{
		"upstream_status": strconv.Itoa(statusCode),
	}
	if oauthErr != nil {
		if v := strings.TrimSpace(oauthErr.Error); v != "" {
			metadata["oauth_error"] = v
		}
		if v := strings.TrimSpace(oauthErr.Code); v != "" {
			metadata["oauth_code"] = v
		}
	}

	message := fmt.Sprintf("token refresh failed: status %d", statusCode)
	if oauthErr != nil {
		if desc := strings.TrimSpace(oauthErr.ErrorDescription); desc != "" {
			message = desc
		} else if raw := strings.TrimSpace(oauthErr.Error); raw != "" {
			message = raw
		}
	} else if trimmed := strings.TrimSpace(body); trimmed != "" {
		message = fmt.Sprintf("token refresh failed: status %d, body: %s", statusCode, trimmed)
	}

	if statusCode >= http.StatusInternalServerError {
		return infraerrors.New(http.StatusBadGateway, "OPENAI_OAUTH_UPSTREAM_ERROR", message).WithMetadata(metadata)
	}

	oauthError := ""
	oauthCode := ""
	if oauthErr != nil {
		oauthError = strings.ToLower(strings.TrimSpace(oauthErr.Error))
		oauthCode = strings.ToLower(strings.TrimSpace(oauthErr.Code))
	} else {
		lowerBody := strings.ToLower(body)
		if strings.Contains(lowerBody, "refresh_token_reused") {
			oauthCode = "refresh_token_reused"
		}
	}

	switch {
	case oauthCode == "refresh_token_reused":
		return infraerrors.Unauthorized("OPENAI_OAUTH_REFRESH_TOKEN_REUSED", message).WithMetadata(metadata)
	case oauthError == "invalid_grant":
		return infraerrors.Unauthorized("OPENAI_OAUTH_INVALID_GRANT", message).WithMetadata(metadata)
	case oauthError == "invalid_client":
		return infraerrors.BadRequest("OPENAI_OAUTH_INVALID_CLIENT", message).WithMetadata(metadata)
	case oauthError == "unauthorized_client":
		return infraerrors.BadRequest("OPENAI_OAUTH_UNAUTHORIZED_CLIENT", message).WithMetadata(metadata)
	case statusCode == http.StatusUnauthorized:
		return infraerrors.Unauthorized("OPENAI_OAUTH_TOKEN_REFRESH_FAILED", message).WithMetadata(metadata)
	case statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError:
		return infraerrors.BadRequest("OPENAI_OAUTH_TOKEN_REFRESH_FAILED", message).WithMetadata(metadata)
	default:
		return infraerrors.New(http.StatusBadGateway, "OPENAI_OAUTH_TOKEN_REFRESH_FAILED", message).WithMetadata(metadata)
	}
}

func parseOpenAIOAuthError(body string) *openAIOAuthErrorResponse {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil
	}
	var resp openAIOAuthErrorResponse
	if err := json.Unmarshal([]byte(trimmed), &resp); err != nil {
		return nil
	}
	if strings.TrimSpace(resp.Error) == "" && strings.TrimSpace(resp.Code) == "" && strings.TrimSpace(resp.ErrorDescription) == "" {
		return nil
	}
	return &resp
}

func shouldReturnOpenAINoProxyHint(ctx context.Context, proxyURL string, err error) bool {
	if strings.TrimSpace(proxyURL) != "" || err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return !errors.Is(err, context.Canceled)
}

func newOpenAINoProxyHintError(cause error) error {
	return infraerrors.New(
		http.StatusBadGateway,
		"OPENAI_OAUTH_PROXY_REQUIRED",
		"OpenAI OAuth request failed: no proxy is configured and this server could not reach OpenAI directly. Select a proxy that can access OpenAI, then retry; if the authorization code has expired, regenerate the authorization URL.",
	).WithCause(cause)
}
