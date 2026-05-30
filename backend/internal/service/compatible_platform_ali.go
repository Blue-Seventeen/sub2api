package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/tidwall/gjson"
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
	if err := validateAliQwenASRInputAudio(body); err != nil {
		return nil, err
	}
	body = normalizeTopPForCompatibleBodyRaw(body)
	body = normalizeStopStringToArray(body)
	return body, nil
}

func validateAliQwenASRInputAudio(body []byte) error {
	model := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "model").String()))
	if !strings.HasPrefix(model, "qwen3-asr") {
		return nil
	}
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		content := message.Get("content")
		if !content.IsArray() {
			continue
		}
		for _, part := range content.Array() {
			if strings.TrimSpace(part.Get("type").String()) != "input_audio" {
				continue
			}
			data := strings.TrimSpace(part.Get("input_audio.data").String())
			if data == "" || isAliQwenASRSupportedAudioReference(data) {
				continue
			}
			if looksLikeBase64AudioPayload(data) {
				return &CompatibleClientError{
					StatusCode: http.StatusBadRequest,
					ErrorType:  "invalid_request_error",
					Message:    "Qwen ASR input_audio.data must be a URL or a Data URL such as data:audio/mpeg;base64,<base64>; raw base64 is not accepted",
				}
			}
		}
	}
	return nil
}

func isAliQwenASRSupportedAudioReference(data string) bool {
	lower := strings.ToLower(strings.TrimSpace(data))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")
}

func looksLikeBase64AudioPayload(data string) bool {
	if len(data) < 32 {
		return false
	}
	for _, r := range data {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '+', r == '/', r == '-', r == '_', r == '=', r == '\r', r == '\n':
		default:
			return false
		}
	}
	return true
}
