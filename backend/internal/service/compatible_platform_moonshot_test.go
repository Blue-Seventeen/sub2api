//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/tidwall/gjson"
)

func TestMoonshotCompatibleProviderPreset(t *testing.T) {
	preset := moonshotCompatibleProviderPreset()

	if preset.Platform != PlatformMoonshot {
		t.Fatalf("Platform = %q, want %q", preset.Platform, PlatformMoonshot)
	}
	if preset.DefaultBaseURL != "https://api.moonshot.cn" {
		t.Fatalf("DefaultBaseURL = %q, want %q", preset.DefaultBaseURL, "https://api.moonshot.cn")
	}
	if preset.DefaultTestModel != "kimi-k2.5" {
		t.Fatalf("DefaultTestModel = %q, want %q", preset.DefaultTestModel, "kimi-k2.5")
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
	if !preset.SupportsMessages("kimi-k2.5") {
		t.Fatal("SupportsMessages(kimi-k2.5) = false, want true")
	}
	if len(preset.DefaultModels) != 3 {
		t.Fatalf("len(DefaultModels) = %d, want 3", len(preset.DefaultModels))
	}

	wantModels := []string{"kimi-k2.5", "kimi-k2-thinking", "kimi-k2-thinking-turbo"}
	for i, want := range wantModels {
		if preset.DefaultModels[i].ID != want {
			t.Fatalf("DefaultModels[%d].ID = %q, want %q", i, preset.DefaultModels[i].ID, want)
		}
	}

	baseURL := "https://api.moonshot.cn/"
	wantChatURL := "https://api.moonshot.cn/v1/chat/completions"
	wantMessagesURL := "https://api.moonshot.cn/anthropic/v1/messages"
	if got := preset.BuildChatURL(baseURL, "kimi-k2.5"); got != wantChatURL {
		t.Fatalf("BuildChatURL() = %q, want %q", got, wantChatURL)
	}
	if got := preset.BuildResponsesURL(baseURL, "kimi-k2.5"); got != wantChatURL {
		t.Fatalf("BuildResponsesURL() = %q, want %q", got, wantChatURL)
	}
	if got := preset.BuildMessagesURL(baseURL, "kimi-k2.5"); got != wantMessagesURL {
		t.Fatalf("BuildMessagesURL() = %q, want %q", got, wantMessagesURL)
	}
}

func TestMoonshotCompatibleProviderPreset_ResponsesFallbackAndBodyPatch(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
	}

	body := []byte(`{
		"model": "kimi-k2.5",
		"input": [
			{
				"role": "user",
				"content": [
					{
						"type": "input_text",
						"text": "hi"
					}
				]
			}
		],
		"top_p": 1.2,
		"max_output_tokens": 64,
		"stream": true
	}`)

	prepared, err := svc.prepareRequest(account, CompatibleRouteResponses, body)
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamKind != compatibleUpstreamChat {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamChat)
	}
	if prepared.UpstreamEndpoint != "/v1/chat/completions" {
		t.Fatalf("UpstreamEndpoint = %q, want %q", prepared.UpstreamEndpoint, "/v1/chat/completions")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "kimi-k2.5" {
		t.Fatalf("patched model = %q, want %q", got, "kimi-k2.5")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "messages.0.role").String(); got != "user" {
		t.Fatalf("patched messages.0.role = %q, want %q", got, "user")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "messages.0.content.0.type").String(); got != "text" {
		t.Fatalf("patched messages.0.content.0.type = %q, want %q", got, "text")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "messages.0.content.0.text").String(); got != "hi" {
		t.Fatalf("patched messages.0.content.0.text = %q, want %q", got, "hi")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "top_p").Float(); got != 0.99 {
		t.Fatalf("patched top_p = %v, want 0.99", got)
	}
	if got := gjson.GetBytes(prepared.RequestBody, "max_tokens").Int(); got != 64 {
		t.Fatalf("patched max_tokens = %d, want 64", got)
	}
	if got := gjson.GetBytes(prepared.RequestBody, "max_completion_tokens").Int(); got != 64 {
		t.Fatalf("patched max_completion_tokens = %d, want 64", got)
	}
	if !gjson.GetBytes(prepared.RequestBody, "stream_options.include_usage").Bool() {
		t.Fatal("patched stream_options.include_usage = false, want true")
	}
}

func TestMoonshotCompatibleProviderPreset_ChatStreamingAddsUsageRequest(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
	}

	body := []byte(`{
		"model": "kimi-k2.5",
		"messages": [{"role":"user","content":"hi"}],
		"stream": true,
		"top_p": 1.2
	}`)

	prepared, err := svc.prepareRequest(account, CompatibleRouteChatCompletions, body)
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamKind != compatibleUpstreamChat {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamChat)
	}
	if got := gjson.GetBytes(prepared.RequestBody, "top_p").Float(); got != 0.99 {
		t.Fatalf("patched top_p = %v, want 0.99", got)
	}
	if !gjson.GetBytes(prepared.RequestBody, "stream_options.include_usage").Bool() {
		t.Fatal("chat streaming should force stream_options.include_usage = true")
	}
}

func TestMoonshotCompatibleProviderPreset_CustomRelayMessagesFallbackToChat(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "http://api.hack3rx.cn/v1",
		},
	}

	body := []byte(`{
		"model": "kimi-k2.5",
		"messages": [{"role":"user","content":"hi"}],
		"max_tokens": 32,
		"stream": true
	}`)

	prepared, err := svc.prepareRequest(account, CompatibleRouteMessages, body)
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamKind != compatibleUpstreamChat {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamChat)
	}
	if prepared.UpstreamEndpoint != "/v1/chat/completions" {
		t.Fatalf("UpstreamEndpoint = %q, want %q", prepared.UpstreamEndpoint, "/v1/chat/completions")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "kimi-k2.5" {
		t.Fatalf("patched model = %q, want %q", got, "kimi-k2.5")
	}
	if !gjson.GetBytes(prepared.RequestBody, "stream_options.include_usage").Bool() {
		t.Fatal("messages fallback streaming should force stream_options.include_usage = true")
	}
}

func TestMoonshotCompatibleProviderPreset_OfficialBaseKeepsNativeMessages(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://api.moonshot.cn",
		},
	}

	body := []byte(`{
		"model": "kimi-k2.5",
		"messages": [{"role":"user","content":"hi"}],
		"max_tokens": 32
	}`)

	prepared, err := svc.prepareRequest(account, CompatibleRouteMessages, body)
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamKind != compatibleUpstreamMessages {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamMessages)
	}
	if prepared.UpstreamEndpoint != "/v1/messages" {
		t.Fatalf("UpstreamEndpoint = %q, want %q", prepared.UpstreamEndpoint, "/v1/messages")
	}
}

func TestMoonshotCompatibleProviderPreset_ApplyAuthUsesBearerAPIKey(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "moonshot-api-key",
			"token":   "moonshot-generated-token",
		},
	}

	req, err := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	if err := svc.applyAuth(req, account); err != nil {
		t.Fatalf("applyAuth() error = %v", err)
	}
	if got := getHeaderRaw(req.Header, "authorization"); got != "Bearer moonshot-api-key" {
		t.Fatalf("authorization = %q, want %q", got, "Bearer moonshot-api-key")
	}
}
