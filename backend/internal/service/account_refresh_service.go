package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type AccountRefreshResult struct {
	Account      *Account `json:"account,omitempty"`
	ResponseText string   `json:"response_text"`
	Warning      string   `json:"warning,omitempty"`
}

type AccountRefreshService struct {
	adminService            AdminService
	oauthService            *OAuthService
	openaiOAuthService      *OpenAIOAuthService
	geminiOAuthService      *GeminiOAuthService
	antigravityOAuthService *AntigravityOAuthService
	tokenCacheInvalidator   TokenCacheInvalidator
}

func NewAccountRefreshService(
	adminService AdminService,
	oauthService *OAuthService,
	openaiOAuthService *OpenAIOAuthService,
	geminiOAuthService *GeminiOAuthService,
	antigravityOAuthService *AntigravityOAuthService,
	tokenCacheInvalidator TokenCacheInvalidator,
) *AccountRefreshService {
	return &AccountRefreshService{
		adminService:            adminService,
		oauthService:            oauthService,
		openaiOAuthService:      openaiOAuthService,
		geminiOAuthService:      geminiOAuthService,
		antigravityOAuthService: antigravityOAuthService,
		tokenCacheInvalidator:   tokenCacheInvalidator,
	}
}

func (s *AccountRefreshService) RefreshAccount(ctx context.Context, account *Account) (*AccountRefreshResult, error) {
	if account == nil {
		return nil, fmt.Errorf("account is required")
	}
	if !account.IsOAuth() {
		return nil, fmt.Errorf("cannot refresh non-OAuth account")
	}

	var (
		newCredentials map[string]any
		updatedAccount *Account
		warning        string
		err            error
	)

	switch account.Platform {
	case PlatformOpenAI:
		if s.openaiOAuthService == nil {
			return nil, fmt.Errorf("openai oauth service not configured")
		}
		updatedAccount, err = s.openaiOAuthService.ForceRefreshAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		if s.adminService != nil {
			s.adminService.EnsureOpenAIPrivacy(ctx, updatedAccount)
			s.adminService.EnsureAntigravityPrivacy(ctx, updatedAccount)
		}
	case PlatformGemini:
		if s.geminiOAuthService == nil {
			return nil, fmt.Errorf("gemini oauth service not configured")
		}
		tokenInfo, refreshErr := s.geminiOAuthService.RefreshAccountToken(ctx, account)
		if refreshErr != nil {
			return nil, fmt.Errorf("failed to refresh credentials: %w", refreshErr)
		}
		newCredentials = s.geminiOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
	case PlatformAntigravity:
		if s.antigravityOAuthService == nil {
			return nil, fmt.Errorf("antigravity oauth service not configured")
		}
		tokenInfo, refreshErr := s.antigravityOAuthService.RefreshAccountToken(ctx, account)
		if refreshErr != nil {
			return nil, refreshErr
		}
		newCredentials = s.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
		for k, v := range account.Credentials {
			if _, exists := newCredentials[k]; !exists {
				newCredentials[k] = v
			}
		}
		if newProjectID, _ := newCredentials["project_id"].(string); newProjectID == "" {
			if oldProjectID := strings.TrimSpace(account.GetCredential("project_id")); oldProjectID != "" {
				newCredentials["project_id"] = oldProjectID
			}
		}
		if tokenInfo.ProjectIDMissing {
			if s.adminService == nil {
				return nil, fmt.Errorf("admin service not configured")
			}
			updatedAccount, err = s.adminService.UpdateAccount(ctx, account.ID, &UpdateAccountInput{
				Credentials: newCredentials,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to update credentials: %w", err)
			}
			s.adminService.EnsureAntigravityPrivacy(ctx, updatedAccount)
			warning = "missing_project_id_temporary"
		}
		if warning == "" && account.Status == StatusError && strings.Contains(account.ErrorMessage, "missing_project_id:") && s.adminService != nil {
			if _, clearErr := s.adminService.ClearAccountError(ctx, account.ID); clearErr != nil {
				return nil, fmt.Errorf("failed to clear account error: %w", clearErr)
			}
		}
	default:
		if s.oauthService == nil {
			return nil, fmt.Errorf("oauth service not configured")
		}
		tokenInfo, refreshErr := s.oauthService.RefreshAccountToken(ctx, account)
		if refreshErr != nil {
			return nil, refreshErr
		}
		newCredentials = make(map[string]any, len(account.Credentials)+6)
		for k, v := range account.Credentials {
			newCredentials[k] = v
		}
		newCredentials["access_token"] = tokenInfo.AccessToken
		newCredentials["token_type"] = tokenInfo.TokenType
		newCredentials["expires_in"] = strconv.FormatInt(tokenInfo.ExpiresIn, 10)
		newCredentials["expires_at"] = strconv.FormatInt(tokenInfo.ExpiresAt, 10)
		if strings.TrimSpace(tokenInfo.RefreshToken) != "" {
			newCredentials["refresh_token"] = tokenInfo.RefreshToken
		}
		if strings.TrimSpace(tokenInfo.Scope) != "" {
			newCredentials["scope"] = tokenInfo.Scope
		}
	}

	if updatedAccount == nil {
		if s.adminService == nil {
			return nil, fmt.Errorf("admin service not configured")
		}
		updatedAccount, err = s.adminService.UpdateAccount(ctx, account.ID, &UpdateAccountInput{
			Credentials: newCredentials,
		})
		if err != nil {
			return nil, err
		}
		s.adminService.EnsureOpenAIPrivacy(ctx, updatedAccount)
		s.adminService.EnsureAntigravityPrivacy(ctx, updatedAccount)
	}

	if s.tokenCacheInvalidator != nil && updatedAccount != nil {
		_ = s.tokenCacheInvalidator.InvalidateToken(ctx, updatedAccount)
	}

	return &AccountRefreshResult{
		Account:      updatedAccount,
		Warning:      warning,
		ResponseText: buildAccountRefreshResponseText(updatedAccount, warning),
	}, nil
}

func buildAccountRefreshResponseText(account *Account, warning string) string {
	message := "Token refreshed successfully"
	payload := map[string]any{
		"success": true,
		"message": message,
	}
	if account != nil {
		payload["account_id"] = account.ID
		payload["status"] = account.Status
	}
	if warning == "missing_project_id_temporary" {
		payload["warning"] = warning
		payload["message"] = "Token refreshed successfully, but project_id could not be retrieved (will retry automatically)"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return message
	}
	return string(data)
}
