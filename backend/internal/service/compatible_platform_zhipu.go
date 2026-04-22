package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	zhipuCompatibleChatPath     = "/api/paas/v4/chat/completions"
	zhipuCompatibleMessagesPath = "/api/anthropic/v1/messages"
)

func zhipuCompatibleProviderPreset() CompatibleProviderPreset {
	return CompatibleProviderPreset{
		Platform:       PlatformZhipu,
		DisplayName:    compatiblePlatformDisplayName(PlatformZhipu),
		DefaultBaseURL: "https://open.bigmodel.cn",
		DefaultModels: NormalizeCompatibleModelList([]claude.Model{
			{ID: "glm-4.5", Type: "model", DisplayName: "GLM-4.5"},
			{ID: "glm-4.5-air", Type: "model", DisplayName: "GLM-4.5 Air"},
			{ID: "glm-4.5-flash", Type: "model", DisplayName: "GLM-4.5 Flash"},
			{ID: "glm-4-plus", Type: "model", DisplayName: "GLM-4 Plus"},
		}),
		DefaultTestModel:      "glm-4.5-flash",
		AuthMode:              CompatibleAuthZhipuToken,
		SupportsChat:          true,
		SupportsResponses:     false,
		SupportsMessages:      func(string) bool { return true },
		BuildChatURL:          zhipuBuildCompatibleChatURL,
		BuildResponsesURL:     zhipuBuildCompatibleChatURL,
		BuildMessagesURL:      zhipuBuildCompatibleMessagesURL,
		PatchMessagesHeaders:  zhipuPatchCompatibleHeaders,
		PatchChatHeaders:      zhipuPatchCompatibleHeaders,
		PatchResponsesHeaders: zhipuPatchCompatibleHeaders,
		PatchMessagesBody:     zhipuPatchMessagesBody,
		PatchChatBody:         zhipuPatchChatBody,
		PatchResponsesBody:    zhipuPatchChatBody,
	}
}

func zhipuBuildCompatibleChatURL(baseURL, _ string) string {
	return strings.TrimRight(baseURL, "/") + zhipuCompatibleChatPath
}

func zhipuBuildCompatibleMessagesURL(baseURL, _ string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	lower := strings.ToLower(baseURL)

	switch {
	case strings.HasSuffix(lower, "/api/anthropic/v1/messages"),
		strings.HasSuffix(lower, "/v1/messages"):
		return baseURL
	case strings.HasSuffix(lower, "/api/anthropic/v1"):
		return baseURL + "/messages"
	case strings.HasSuffix(lower, "/api/anthropic"):
		return baseURL + "/v1/messages"
	default:
		return baseURL + zhipuCompatibleMessagesPath
	}
}

func zhipuPatchCompatibleHeaders(req *http.Request, account *Account, _ string) {
	if req == nil || account == nil {
		return
	}
	if strings.TrimSpace(getHeaderRaw(req.Header, "authorization")) != "" {
		return
	}

	token := strings.TrimSpace(account.GetCredential("token"))
	if token == "" {
		token = strings.TrimSpace(account.GetCredential("api_key"))
	}
	if token == "" {
		return
	}

	setHeaderRaw(req.Header, "authorization", "Bearer "+token)
}

func zhipuPatchMessagesBody(body []byte, _ *Account, _ string) ([]byte, error) {
	return body, nil
}

func zhipuPatchChatBody(body []byte, _ *Account, _ string) ([]byte, error) {
	body = normalizeTopPForCompatibleBodyRaw(body)
	body = normalizeStopStringToArray(body)
	body = normalizeDeveloperRoleToSystem(body)

	if gjson.GetBytes(body, "max_completion_tokens").Exists() && !gjson.GetBytes(body, "max_tokens").Exists() {
		var err error
		body, err = sjson.SetBytes(body, "max_tokens", gjson.GetBytes(body, "max_completion_tokens").Value())
		if err != nil {
			return nil, err
		}
	}
	if gjson.GetBytes(body, "max_completion_tokens").Exists() {
		var err error
		body, err = sjson.DeleteBytes(body, "max_completion_tokens")
		if err != nil {
			return nil, err
		}
	}

	body = stripDataPrefixFromImageURLs(body)
	return body, nil
}
