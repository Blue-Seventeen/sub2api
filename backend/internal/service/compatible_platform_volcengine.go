package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func volcengineCompatibleProviderPreset() CompatibleProviderPreset {
	return CompatibleProviderPreset{
		Platform:       PlatformVolcEngine,
		DisplayName:    compatiblePlatformDisplayName(PlatformVolcEngine),
		DefaultBaseURL: "https://ark.cn-beijing.volces.com",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "doubao-seed-1-6-thinking-250715", Type: "model", DisplayName: "Doubao Seed 1.6 Thinking"},
			{ID: "Doubao-pro-128k", Type: "model", DisplayName: "Doubao Pro 128k"},
			{ID: "Doubao-lite-32k", Type: "model", DisplayName: "Doubao Lite 32k"},
		}),
		DefaultTestModel:  "Doubao-lite-32k",
		AuthMode:          CompatibleAuthBearer,
		SupportsChat:      true,
		SupportsResponses: true,
		SupportsMessages:  func(string) bool { return false },
		BuildChatURL: func(baseURL, upstreamModel string) string {
			baseURL = strings.TrimRight(baseURL, "/")
			if strings.HasPrefix(strings.TrimSpace(upstreamModel), "bot") {
				return baseURL + "/api/v3/bots/chat/completions"
			}
			return baseURL + "/api/v3/chat/completions"
		},
		BuildResponsesURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/api/v3/responses"
		},
		BuildMessagesURL: func(baseURL, upstreamModel string) string {
			baseURL = strings.TrimRight(baseURL, "/")
			if strings.HasPrefix(strings.TrimSpace(upstreamModel), "bot") {
				return baseURL + "/api/v3/bots/chat/completions"
			}
			return baseURL + "/api/v3/chat/completions"
		},
		PatchChatBody: patchVolcengineChatBody,
	}
}

func patchVolcengineChatBody(body []byte, _ *Account, _ string) ([]byte, error) {
	return normalizeStopStringToArray(body), nil
}
