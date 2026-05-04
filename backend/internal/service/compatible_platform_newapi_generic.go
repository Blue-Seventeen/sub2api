package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

type newAPIStyleCompatibleDefinition struct {
	Platform         string
	DefaultBaseURL   string
	DefaultModels    []claude.Model
	DefaultTestModel string
	ChatPath         string
}

func newAPIStyleCompatibleProviderPresetForPlatform(platform string) (CompatibleProviderPreset, bool) {
	switch strings.TrimSpace(platform) {
	case PlatformPerplexity:
		return perplexityCompatibleProviderPreset(), true
	case PlatformMistral:
		return mistralCompatibleProviderPreset(), true
	case PlatformSiliconFlow:
		return siliconFlowCompatibleProviderPreset(), true
	case PlatformXAI:
		return xaiCompatibleProviderPreset(), true
	case PlatformOpenRouter:
		return openRouterCompatibleProviderPreset(), true
	case PlatformSuno:
		return sunoCompatibleProviderPreset(), true
	case PlatformKling:
		return klingCompatibleProviderPreset(), true
	case PlatformMidjourney:
		return midjourneyCompatibleProviderPreset(), true
	default:
		return CompatibleProviderPreset{}, false
	}
}

func perplexityCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformPerplexity,
		DefaultBaseURL: "https://api.perplexity.ai",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "sonar", Type: "model", DisplayName: "Sonar"},
			{ID: "sonar-pro", Type: "model", DisplayName: "Sonar Pro"},
			{ID: "sonar-reasoning", Type: "model", DisplayName: "Sonar Reasoning"},
		}),
		DefaultTestModel: "sonar",
		ChatPath:         "/chat/completions",
	})
}

func mistralCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformMistral,
		DefaultBaseURL: "https://api.mistral.ai",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "mistral-small-latest", Type: "model", DisplayName: "Mistral Small"},
			{ID: "mistral-medium-latest", Type: "model", DisplayName: "Mistral Medium"},
			{ID: "mistral-large-latest", Type: "model", DisplayName: "Mistral Large"},
		}),
		DefaultTestModel: "mistral-small-latest",
		ChatPath:         "/v1/chat/completions",
	})
}

func siliconFlowCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformSiliconFlow,
		DefaultBaseURL: "https://api.siliconflow.cn",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "deepseek-ai/DeepSeek-V2-Chat", Type: "model", DisplayName: "DeepSeek V2 Chat"},
			{ID: "Qwen/Qwen2-72B-Instruct", Type: "model", DisplayName: "Qwen2 72B Instruct"},
			{ID: "THUDM/glm-4-9b-chat", Type: "model", DisplayName: "GLM-4 9B Chat"},
		}),
		DefaultTestModel: "deepseek-ai/DeepSeek-V2-Chat",
		ChatPath:         "/v1/chat/completions",
	})
}

func xaiCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformXAI,
		DefaultBaseURL: "https://api.x.ai",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "grok-4-1-fast-reasoning", Type: "model", DisplayName: "Grok 4.1 Fast Reasoning"},
			{ID: "grok-code-fast-1", Type: "model", DisplayName: "Grok Code Fast 1"},
			{ID: "grok-3", Type: "model", DisplayName: "Grok 3"},
		}),
		DefaultTestModel: "grok-3",
		ChatPath:         "/v1/chat/completions",
	})
}

func openRouterCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformOpenRouter,
		DefaultBaseURL: "https://openrouter.ai/api",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "openai/gpt-4o-mini", Type: "model", DisplayName: "OpenAI GPT-4o mini"},
			{ID: "anthropic/claude-sonnet-4.5", Type: "model", DisplayName: "Claude Sonnet 4.5"},
			{ID: "google/gemini-2.5-flash", Type: "model", DisplayName: "Gemini 2.5 Flash"},
		}),
		DefaultTestModel: "openai/gpt-4o-mini",
		ChatPath:         "/v1/chat/completions",
	})
}

func sunoCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformSuno,
		DefaultBaseURL: "",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "suno_music", Type: "model", DisplayName: "Suno Music"},
			{ID: "suno_lyrics", Type: "model", DisplayName: "Suno Lyrics"},
		}),
		DefaultTestModel: "suno_music",
		ChatPath:         "/suno/submit/music",
	})
}

func klingCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformKling,
		DefaultBaseURL: "https://api.klingai.com",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "kling-v1", Type: "model", DisplayName: "Kling v1"},
			{ID: "kling-v1-6", Type: "model", DisplayName: "Kling v1.6"},
		}),
		DefaultTestModel: "kling-v1",
		ChatPath:         "/kling/v1/videos/text2video",
	})
}

func midjourneyCompatibleProviderPreset() CompatibleProviderPreset {
	return newAPIStyleCompatibleProviderPreset(newAPIStyleCompatibleDefinition{
		Platform:       PlatformMidjourney,
		DefaultBaseURL: "",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "midjourney", Type: "model", DisplayName: "Midjourney"},
		}),
		DefaultTestModel: "midjourney",
		ChatPath:         "/mj/submit/imagine",
	})
}

func newAPIStyleCompatibleProviderPreset(def newAPIStyleCompatibleDefinition) CompatibleProviderPreset {
	chatPath := strings.TrimSpace(def.ChatPath)
	if chatPath == "" {
		chatPath = "/v1/chat/completions"
	}
	return CompatibleProviderPreset{
		Platform:          def.Platform,
		DisplayName:       compatiblePlatformDisplayName(def.Platform),
		DefaultBaseURL:    def.DefaultBaseURL,
		DefaultModels:     def.DefaultModels,
		DefaultTestModel:  def.DefaultTestModel,
		AuthMode:          CompatibleAuthBearer,
		SupportsChat:      true,
		SupportsResponses: false,
		SupportsMessages:  func(string) bool { return false },
		BuildChatURL: func(baseURL, _ string) string {
			return joinRelayCompatibleURL(baseURL, chatPath)
		},
		BuildResponsesURL: func(baseURL, _ string) string {
			return joinRelayCompatibleURL(baseURL, chatPath)
		},
		BuildMessagesURL: func(baseURL, _ string) string {
			return joinRelayCompatibleURL(baseURL, chatPath)
		},
		PatchChatBody: normalizeNewAPIStyleCompatibleChatBody,
	}
}

func normalizeNewAPIStyleCompatibleChatBody(body []byte, _ *Account, _ string) ([]byte, error) {
	body = normalizeTopPForCompatibleBodyRaw(body)
	body = normalizeStopStringToArray(body)
	body = normalizeDeveloperRoleToSystem(body)
	body = ensureCompatibleStreamingUsageIncluded(body)
	return body, nil
}
