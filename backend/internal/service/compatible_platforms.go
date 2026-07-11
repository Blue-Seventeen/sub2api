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

const AccountExtraNewAPIStyleInterfaceEnabled = "newapi_style_interface_enabled"

var compatiblePlatformOrder = []string{
	PlatformZhipu,
	PlatformDeepSeek,
	PlatformVolcEngine,
	PlatformAli,
	PlatformMoonshot,
	PlatformPerplexity,
	PlatformMistral,
	PlatformSiliconFlow,
	PlatformOpenRouter,
	PlatformSuno,
	PlatformKling,
	PlatformMidjourney,
}

func CompatiblePlatforms() []string {
	out := make([]string, len(compatiblePlatformOrder))
	copy(out, compatiblePlatformOrder)
	return out
}

func IsCompatiblePlatform(platform string) bool {
	switch strings.TrimSpace(platform) {
	case PlatformZhipu, PlatformDeepSeek, PlatformVolcEngine, PlatformAli, PlatformMoonshot,
		PlatformPerplexity, PlatformMistral, PlatformSiliconFlow, PlatformOpenRouter,
		PlatformSuno, PlatformKling, PlatformMidjourney:
		return true
	default:
		return false
	}
}

func PlatformRequiresNewAPIStyleInterface(platform string) bool {
	switch strings.TrimSpace(platform) {
	case PlatformPerplexity, PlatformMistral, PlatformSiliconFlow, PlatformOpenRouter,
		PlatformSuno, PlatformKling, PlatformMidjourney:
		return true
	default:
		return false
	}
}

func PlatformSupportsNewAPIStyleInterface(platform string) bool {
	switch strings.TrimSpace(platform) {
	case PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity:
		return true
	default:
		return IsCompatiblePlatform(platform)
	}
}

func NormalizeNewAPIStyleInterfaceExtra(platform string, extra map[string]any) map[string]any {
	var out map[string]any
	if len(extra) > 0 {
		out = make(map[string]any, len(extra)+1)
		for key, value := range extra {
			out[key] = value
		}
	}

	if PlatformRequiresNewAPIStyleInterface(platform) {
		if out == nil {
			out = make(map[string]any, 1)
		}
		out[AccountExtraNewAPIStyleInterfaceEnabled] = true
		return out
	}

	if !PlatformSupportsNewAPIStyleInterface(platform) {
		if out != nil {
			delete(out, AccountExtraNewAPIStyleInterfaceEnabled)
		}
		return out
	}

	if (&Account{Extra: out}).GetExtraBool(AccountExtraNewAPIStyleInterfaceEnabled) {
		out[AccountExtraNewAPIStyleInterfaceEnabled] = true
		return out
	}
	if out != nil {
		delete(out, AccountExtraNewAPIStyleInterfaceEnabled)
	}
	return out
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
	case PlatformPerplexity:
		return "Perplexity"
	case PlatformMistral:
		return "Mistral"
	case PlatformSiliconFlow:
		return "SiliconFlow"
	case PlatformOpenRouter:
		return "OpenRouter"
	case PlatformSuno:
		return "Suno"
	case PlatformKling:
		return "Kling"
	case PlatformMidjourney:
		return "Midjourney"
	default:
		return platform
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
	case PlatformPerplexity:
		return perplexityCompatibleProviderPreset(), true
	case PlatformMistral:
		return mistralCompatibleProviderPreset(), true
	case PlatformSiliconFlow:
		return siliconFlowCompatibleProviderPreset(), true
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
	if account.UseNewAPIStyleInterface() {
		if preset, ok := newAPIStyleCompatibleProviderPresetForPlatform(account.Platform); ok {
			return preset, nil
		}
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

func (a *Account) UseNewAPIStyleInterface() bool {
	return UseNewAPIStyleInterface(a, nil)
}

func (a *Account) UseNewAPIStyleInterfaceForGroup(group *Group) bool {
	return UseNewAPIStyleInterface(a, group)
}

func UseNewAPIStyleInterface(account *Account, group *Group) bool {
	if account == nil || !PlatformSupportsNewAPIStyleInterface(account.Platform) {
		return false
	}
	if PlatformRequiresNewAPIStyleInterface(account.Platform) {
		return true
	}
	if groupEnablesNewAPIStyleInterface(group, account.Platform) {
		return true
	}
	return account.GetExtraBool(AccountExtraNewAPIStyleInterfaceEnabled)
}

func groupEnablesNewAPIStyleInterface(group *Group, platform string) bool {
	if !IsGroupContextValid(group) || !group.NewAPIStyleInterfaceEnabled {
		return false
	}
	if strings.TrimSpace(group.Platform) == "" || strings.TrimSpace(platform) == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform))
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
