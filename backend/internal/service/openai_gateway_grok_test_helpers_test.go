//go:build unit

package service

import "time"

func healthyGrokOAuthGatewayTestAccount(id int64, accessToken string) *Account {
	return &Account{
		ID:          id,
		Name:        "grok-oauth-test",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  accessToken,
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
			"base_url":      "https://cli-chat-proxy.grok.com/v1",
		},
		Extra: map[string]any{},
	}
}
