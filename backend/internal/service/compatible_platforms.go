package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

type CompatibleAuthMode string

const (
	CompatibleAuthBearer     CompatibleAuthMode = "bearer"
	CompatibleAuthZhipuToken CompatibleAuthMode = "zhipu_token"
)

type CompatibleRequestRoute string

const (
	CompatibleRouteMessages        CompatibleRequestRoute = "messages"
	CompatibleRouteChatCompletions CompatibleRequestRoute = "chat_completions"
	CompatibleRouteResponses       CompatibleRequestRoute = "responses"
)

type CompatibleProviderPreset struct {
	Platform              string
	DisplayName           string
	DefaultBaseURL        string
	DefaultModels         []claude.Model
	DefaultTestModel      string
	AuthMode              CompatibleAuthMode
	SupportsChat          bool
	SupportsResponses     bool
	SupportsMessages      func(model string) bool
	BuildChatURL          func(baseURL, upstreamModel string) string
	BuildResponsesURL     func(baseURL, upstreamModel string) string
	BuildMessagesURL      func(baseURL, upstreamModel string) string
	PatchMessagesHeaders  func(req *http.Request, account *Account, upstreamModel string)
	PatchChatHeaders      func(req *http.Request, account *Account, upstreamModel string)
	PatchResponsesHeaders func(req *http.Request, account *Account, upstreamModel string)
	PatchMessagesBody     func(body []byte, account *Account, upstreamModel string) ([]byte, error)
	PatchChatBody         func(body []byte, account *Account, upstreamModel string) ([]byte, error)
	PatchResponsesBody    func(body []byte, account *Account, upstreamModel string) ([]byte, error)
}

var compatiblePlatformOrder = []string{
	PlatformZhipu,
	PlatformDeepSeek,
	PlatformVolcEngine,
	PlatformAli,
	PlatformMoonshot,
}

func CompatiblePlatforms() []string {
	out := make([]string, len(compatiblePlatformOrder))
	copy(out, compatiblePlatformOrder)
	return out
}

func IsCompatiblePlatform(platform string) bool {
	switch strings.TrimSpace(platform) {
	case PlatformZhipu, PlatformDeepSeek, PlatformVolcEngine, PlatformAli, PlatformMoonshot:
		return true
	default:
		return false
	}
}

func compatiblePlatformDisplayName(platform string) string {
	switch strings.TrimSpace(platform) {
	case PlatformZhipu:
		return "GLM/智谱"
	case PlatformDeepSeek:
		return "DeepSeek"
	case PlatformVolcEngine:
		return "火山方舟/豆包"
	case PlatformAli:
		return "Qwen/阿里"
	case PlatformMoonshot:
		return "Kimi/月之暗面"
	default:
		return platform
	}
}

func compatiblePlatformLabels() map[string]string {
	return map[string]string{
		PlatformZhipu:      compatiblePlatformDisplayName(PlatformZhipu),
		PlatformDeepSeek:   compatiblePlatformDisplayName(PlatformDeepSeek),
		PlatformVolcEngine: compatiblePlatformDisplayName(PlatformVolcEngine),
		PlatformAli:        compatiblePlatformDisplayName(PlatformAli),
		PlatformMoonshot:   compatiblePlatformDisplayName(PlatformMoonshot),
	}
}

func CompatibleProviderPresetForPlatform(platform string) (CompatibleProviderPreset, bool) {
	switch strings.TrimSpace(platform) {
	case PlatformZhipu:
		return zhipuCompatibleProviderPreset(), true
	case PlatformDeepSeek:
		return deepseekCompatibleProviderPreset(), true
	case PlatformVolcEngine:
		return volcengineCompatibleProviderPreset(), true
	case PlatformAli:
		return aliCompatibleProviderPreset(), true
	case PlatformMoonshot:
		return moonshotCompatibleProviderPreset(), true
	default:
		return CompatibleProviderPreset{}, false
	}
}

func CompatibleDefaultModels(platform string) []claude.Model {
	preset, ok := CompatibleProviderPresetForPlatform(platform)
	if !ok {
		return nil
	}
	models := make([]claude.Model, len(preset.DefaultModels))
	copy(models, preset.DefaultModels)
	return models
}

func CompatibleDefaultTestModel(platform string) string {
	preset, ok := CompatibleProviderPresetForPlatform(platform)
	if !ok {
		return ""
	}
	return strings.TrimSpace(preset.DefaultTestModel)
}

func CompatibleDefaultBaseURL(platform string) string {
	preset, ok := CompatibleProviderPresetForPlatform(platform)
	if !ok {
		return ""
	}
	return strings.TrimSpace(preset.DefaultBaseURL)
}

func NormalizeCompatibleModelList(models []claude.Model) []claude.Model {
	out := make([]claude.Model, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		entry := model
		entry.ID = id
		if strings.TrimSpace(entry.Type) == "" {
			entry.Type = "model"
		}
		if strings.TrimSpace(entry.DisplayName) == "" {
			entry.DisplayName = id
		}
		out = append(out, entry)
	}
	return out
}

func getCompatiblePreset(account *Account) (CompatibleProviderPreset, error) {
	if account == nil {
		return CompatibleProviderPreset{}, fmt.Errorf("account is nil")
	}
	preset, ok := CompatibleProviderPresetForPlatform(account.Platform)
	if !ok {
		return CompatibleProviderPreset{}, fmt.Errorf("unsupported compatible platform: %s", account.Platform)
	}
	return preset, nil
}

func (a *Account) IsCompatiblePlatform() bool {
	if a == nil {
		return false
	}
	return IsCompatiblePlatform(a.Platform)
}

func (a *Account) GetCompatibleBaseURL() string {
	if a == nil || a.Type != AccountTypeAPIKey || !a.IsCompatiblePlatform() {
		return ""
	}
	baseURL := strings.TrimSpace(a.GetCredential("base_url"))
	if baseURL != "" {
		return baseURL
	}
	return CompatibleDefaultBaseURL(a.Platform)
}

func getCompatibleAuthToken(account *Account, mode CompatibleAuthMode) string {
	if account == nil {
		return ""
	}
	switch mode {
	case CompatibleAuthZhipuToken:
		if token := strings.TrimSpace(account.GetCredential("token")); token != "" {
			return token
		}
		return strings.TrimSpace(account.GetCredential("api_key"))
	default:
		return strings.TrimSpace(account.GetCredential("api_key"))
	}
}
