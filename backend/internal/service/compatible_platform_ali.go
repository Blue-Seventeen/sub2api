package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

func aliCompatibleProviderPreset() CompatibleProviderPreset {
	return CompatibleProviderPreset{
		Platform:       PlatformAli,
		DisplayName:    compatiblePlatformDisplayName(PlatformAli),
		DefaultBaseURL: "https://dashscope.aliyuncs.com",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "qwen-turbo", Type: "model", DisplayName: "Qwen Turbo"},
			{ID: "qwen-plus", Type: "model", DisplayName: "Qwen Plus"},
			{ID: "qwen-max", Type: "model", DisplayName: "Qwen Max"},
			{ID: "qwq-32b", Type: "model", DisplayName: "QwQ 32B"},
		}),
		DefaultTestModel:  "qwen-turbo",
		AuthMode:          CompatibleAuthBearer,
		SupportsChat:      true,
		SupportsResponses: true,
		SupportsMessages:  supportsAliNativeMessages,
		BuildChatURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/compatible-mode/v1/chat/completions"
		},
		BuildResponsesURL: func(baseURL, _ string) string {
			return strings.TrimRight(baseURL, "/") + "/api/v2/apps/protocols/compatible-mode/v1/responses"
		},
		BuildMessagesURL: func(baseURL, upstreamModel string) string {
			baseURL = strings.TrimRight(baseURL, "/")
			if supportsAliNativeMessages(upstreamModel) {
				return baseURL + "/apps/anthropic/v1/messages"
			}
			return baseURL + "/compatible-mode/v1/chat/completions"
		},
		PatchChatHeaders:      patchAliStreamingHeaders,
		PatchResponsesHeaders: patchAliStreamingHeaders,
		PatchChatBody:         patchAliBody,
		PatchResponsesBody:    patchAliBody,
	}
}

func supportsAliNativeMessages(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "qwen")
}

func patchAliStreamingHeaders(req *http.Request, _ *Account, _ string) {
	if req == nil {
		return
	}
	if strings.Contains(strings.ToLower(req.URL.Path), "/messages") {
		return
	}
	req.Header.Set("X-DashScope-SSE", "enable")
}

func patchAliBody(body []byte, _ *Account, _ string) ([]byte, error) {
	body = normalizeTopPForCompatibleBodyRaw(body)
	body = normalizeStopStringToArray(body)
	return body, nil
}
