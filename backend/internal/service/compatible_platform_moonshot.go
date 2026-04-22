package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func moonshotCompatibleProviderPreset() CompatibleProviderPreset {
	return CompatibleProviderPreset{
		Platform:       PlatformMoonshot,
		DisplayName:    compatiblePlatformDisplayName(PlatformMoonshot),
		DefaultBaseURL: "https://api.moonshot.cn",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "kimi-k2.5", Type: "model", DisplayName: "Kimi K2.5"},
			{ID: "kimi-k2-thinking", Type: "model", DisplayName: "Kimi K2 Thinking"},
			{ID: "kimi-k2-thinking-turbo", Type: "model", DisplayName: "Kimi K2 Thinking Turbo"},
		}),
		DefaultTestModel:  "kimi-k2.5",
		AuthMode:          CompatibleAuthBearer,
		SupportsChat:      true,
		SupportsResponses: false,
		SupportsMessages:  func(string) bool { return true },
		BuildChatURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
		},
		BuildResponsesURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
		},
		BuildMessagesURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/anthropic/v1/messages"
		},
		PatchChatBody: normalizeTopPForCompatibleBody,
	}
}
