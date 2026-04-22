//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

func TestDeepSeekCompatibleProviderPreset_Defaults(t *testing.T) {
	preset := deepseekCompatibleProviderPreset()

	if preset.Platform != PlatformDeepSeek {
		t.Fatalf("Platform = %q, want %q", preset.Platform, PlatformDeepSeek)
	}
	if preset.DefaultBaseURL != "https://api.deepseek.com" {
		t.Fatalf("DefaultBaseURL = %q, want %q", preset.DefaultBaseURL, "https://api.deepseek.com")
	}
	if got := CompatibleDefaultBaseURL(PlatformDeepSeek); got != preset.DefaultBaseURL {
		t.Fatalf("CompatibleDefaultBaseURL() = %q, want %q", got, preset.DefaultBaseURL)
	}
	if preset.DefaultTestModel != "deepseek-chat" {
		t.Fatalf("DefaultTestModel = %q, want %q", preset.DefaultTestModel, "deepseek-chat")
	}
	if got := CompatibleDefaultTestModel(PlatformDeepSeek); got != preset.DefaultTestModel {
		t.Fatalf("CompatibleDefaultTestModel() = %q, want %q", got, preset.DefaultTestModel)
	}
	if preset.AuthMode != CompatibleAuthBearer {
		t.Fatalf("AuthMode = %q, want %q", preset.AuthMode, CompatibleAuthBearer)
	}
	if !preset.SupportsChat {
		t.Fatal("SupportsChat = false, want true")
	}
	if preset.SupportsResponses {
		t.Fatal("SupportsResponses = true, want false")
	}
	if preset.SupportsMessages == nil {
		t.Fatal("SupportsMessages should not be nil")
	}
	if !preset.SupportsMessages("deepseek-chat") || !preset.SupportsMessages("deepseek-reasoner") {
		t.Fatal("SupportsMessages should accept DeepSeek models")
	}

	wantModels := []string{"deepseek-chat", "deepseek-reasoner"}
	if len(preset.DefaultModels) != len(wantModels) {
		t.Fatalf("len(DefaultModels) = %d, want %d", len(preset.DefaultModels), len(wantModels))
	}
	defaultModels := CompatibleDefaultModels(PlatformDeepSeek)
	if len(defaultModels) != len(wantModels) {
		t.Fatalf("len(CompatibleDefaultModels()) = %d, want %d", len(defaultModels), len(wantModels))
	}
	for i, want := range wantModels {
		if got := preset.DefaultModels[i].ID; got != want {
			t.Fatalf("DefaultModels[%d].ID = %q, want %q", i, got, want)
		}
		if got := defaultModels[i].ID; got != want {
			t.Fatalf("CompatibleDefaultModels()[%d].ID = %q, want %q", i, got, want)
		}
	}
}

func TestDeepSeekCompatibleProviderPreset_RoutesAndResponsesFallback(t *testing.T) {
	preset := deepseekCompatibleProviderPreset()

	baseURL := preset.DefaultBaseURL + "/"
	wantChatURL := "https://api.deepseek.com/chat/completions"
	wantMessagesURL := "https://api.deepseek.com/anthropic/v1/messages"

	if got := preset.BuildChatURL(baseURL, "deepseek-chat"); got != wantChatURL {
		t.Fatalf("BuildChatURL() = %q, want %q", got, wantChatURL)
	}
	if got := preset.BuildMessagesURL(baseURL, "deepseek-chat"); got != wantMessagesURL {
		t.Fatalf("BuildMessagesURL() = %q, want %q", got, wantMessagesURL)
	}
	if got := preset.BuildResponsesURL(baseURL, "deepseek-chat"); got != wantChatURL {
		t.Fatalf("BuildResponsesURL() = %q, want %q", got, wantChatURL)
	}

	topP := 1.25
	responsesReq := &apicompat.ResponsesRequest{
		Model:   "deepseek-chat",
		Input:   json.RawMessage(`"hello from responses"`),
		TopP:    &topP,
		Stream:  true,
		Include: []string{"reasoning.encrypted_content"},
	}
	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(responsesReq)
	if err != nil {
		t.Fatalf("ResponsesToChatCompletionsRequest() error = %v", err)
	}
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		t.Fatalf("json.Marshal(chatReq) error = %v", err)
	}
	patchedFallbackBody, err := preset.PatchChatBody(chatBody, nil, "deepseek-chat")
	if err != nil {
		t.Fatalf("PatchChatBody(fallback) error = %v", err)
	}
	if got := gjson.GetBytes(patchedFallbackBody, "messages.0.role").String(); got != "user" {
		t.Fatalf("responses fallback messages.0.role = %q, want %q", got, "user")
	}
	if got := gjson.GetBytes(patchedFallbackBody, "messages.0.content").String(); got != "hello from responses" {
		t.Fatalf("responses fallback messages.0.content = %q, want %q", got, "hello from responses")
	}
	if got := gjson.GetBytes(patchedFallbackBody, "top_p").Float(); got != 0.99 {
		t.Fatalf("responses fallback top_p = %v, want 0.99", got)
	}
	if !gjson.GetBytes(patchedFallbackBody, "stream_options.include_usage").Bool() {
		t.Fatal("responses fallback should preserve stream_options.include_usage = true")
	}
}

func TestDeepSeekCompatibleProviderPreset_BearerAuthAndChatBodyPatch(t *testing.T) {
	preset := deepseekCompatibleProviderPreset()
	if preset.AuthMode != CompatibleAuthBearer {
		t.Fatalf("AuthMode = %q, want %q", preset.AuthMode, CompatibleAuthBearer)
	}

	if preset.PatchChatBody == nil {
		t.Fatal("PatchChatBody should not be nil")
	}
	patchedBody, err := preset.PatchChatBody([]byte(`{
		"model": "deepseek-chat",
		"messages": [{"role": "user", "content": "hello"}],
		"top_p": 1.2,
		"stop": "END"
	}`), nil, "deepseek-chat")
	if err != nil {
		t.Fatalf("PatchChatBody() error = %v", err)
	}
	if got := gjson.GetBytes(patchedBody, "top_p").Float(); got != 0.99 {
		t.Fatalf("patched top_p = %v, want 0.99", got)
	}
	if got := gjson.GetBytes(patchedBody, "stop").String(); got != "END" {
		t.Fatalf("patched stop = %q, want %q", got, "END")
	}
	if gjson.GetBytes(patchedBody, "stop").IsArray() {
		t.Fatalf("patched stop should remain string, got %s", gjson.GetBytes(patchedBody, "stop").Raw)
	}
}
