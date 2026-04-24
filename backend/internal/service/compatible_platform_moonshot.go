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
		BuildChatURL:      moonshotBuildCompatibleChatURL,
		BuildResponsesURL: moonshotBuildCompatibleChatURL,
		BuildMessagesURL:  moonshotBuildCompatibleMessagesURL,
		PatchMessagesBody: patchMoonshotCompatibleMessagesBody,
		PatchChatBody:     patchMoonshotCompatibleChatBody,
	}
}

func moonshotBuildCompatibleChatURL(baseURL, _ string) string {
	return joinRelayCompatibleURL(baseURL, "/v1/chat/completions")
}

func moonshotBuildCompatibleMessagesURL(baseURL, _ string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	lower := strings.ToLower(baseURL)

	switch {
	case strings.HasSuffix(lower, "/anthropic/v1/messages"),
		strings.HasSuffix(lower, "/v1/messages"):
		return baseURL
	case strings.HasSuffix(lower, "/anthropic/v1"):
		return baseURL + "/messages"
	case strings.HasSuffix(lower, "/anthropic"):
		return baseURL + "/v1/messages"
	case strings.HasSuffix(lower, "/v1"):
		return strings.TrimRight(baseURL[:len(baseURL)-len("/v1")], "/") + "/anthropic/v1/messages"
	default:
		return baseURL + "/anthropic/v1/messages"
	}
}
