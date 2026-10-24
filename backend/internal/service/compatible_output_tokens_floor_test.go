package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCompatibleMessagesConversion_UsesConfiguredOutputTokenFloor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		floor int
		want  int64
	}{
		{name: "default", want: 128},
		{name: "lower_floor", floor: 16, want: 16},
		{name: "higher_floor", floor: 256, want: 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.MinOutputTokens = tc.floor
			svc := &CompatibleGatewayService{cfg: cfg}
			body := []byte(`{"model":"public-alias","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
			t.Run("messages_to_chat", func(t *testing.T) {
				account := &Account{Platform: PlatformVolcEngine, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"model_mapping": map[string]any{"public-alias": "doubao-seed-1-6-thinking-250715"}}}
				prepared, err := svc.prepareRequest(account, CompatibleRouteMessages, body)
				require.NoError(t, err)
				require.Equal(t, compatibleUpstreamChat, prepared.UpstreamKind)
				require.Equal(t, tc.want, gjson.GetBytes(prepared.RequestBody, "max_completion_tokens").Int())
			})
			t.Run("moonshot_chat_fallback", func(t *testing.T) {
				account := &Account{Platform: PlatformMoonshot, Type: AccountTypeAPIKey}
				prepared, err := svc.prepareMoonshotAnthropicMessagesChatFallbackRequest(account, body, "public-alias", "kimi-k2.5", false)
				require.NoError(t, err)
				require.Equal(t, tc.want, gjson.GetBytes(prepared.RequestBody, "max_completion_tokens").Int())
				require.Equal(t, tc.want, gjson.GetBytes(prepared.RequestBody, "max_tokens").Int())
			})
		})
	}
}
