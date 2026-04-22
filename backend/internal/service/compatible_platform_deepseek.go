package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func deepseekCompatibleProviderPreset() CompatibleProviderPreset {
	return CompatibleProviderPreset{
		Platform:       PlatformDeepSeek,
		DisplayName:    compatiblePlatformDisplayName(PlatformDeepSeek),
		DefaultBaseURL: "https://api.deepseek.com",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "deepseek-chat", Type: "model", DisplayName: "DeepSeek Chat"},
			{ID: "deepseek-reasoner", Type: "model", DisplayName: "DeepSeek Reasoner"},
		}),
		DefaultTestModel:  "deepseek-chat",
		AuthMode:          CompatibleAuthBearer,
		SupportsChat:      true,
		SupportsResponses: false,
		SupportsMessages:  func(string) bool { return true },
		BuildChatURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/chat/completions"
		},
		BuildResponsesURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/chat/completions"
		},
		BuildMessagesURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/anthropic/v1/messages"
		},
		PatchChatBody:     normalizeTopPForCompatibleBody,
		PatchMessagesBody: nil,
	}
}
