package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGrokRawChatCompletionsURLUsesSecurityPolicy(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "http://grok.example.test/v1",
		},
	}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	svc.cfg.Security.URLAllowlist.Enabled = false
	svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true

	target, err := svc.rawChatCompletionsURL(account)
	require.NoError(t, err)
	require.Equal(t, "http://grok.example.test/v1/chat/completions", target)

	svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = false
	_, err = svc.rawChatCompletionsURL(account)
	require.EqualError(t, err, "invalid grok base_url: invalid base url: base URL rejected by URL security policy")
}
