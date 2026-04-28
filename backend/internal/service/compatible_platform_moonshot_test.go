//go:build unit

package service

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestMoonshotCompatibleProviderPreset(t *testing.T) {
	preset := moonshotCompatibleProviderPreset()

	if preset.Platform != PlatformMoonshot {
		t.Fatalf("Platform = %q, want %q", preset.Platform, PlatformMoonshot)
	}
	if preset.DefaultBaseURL != "https://api.moonshot.ai" {
		t.Fatalf("DefaultBaseURL = %q, want %q", preset.DefaultBaseURL, "https://api.moonshot.ai")
	}
	if preset.DefaultTestModel != "kimi-k2.6" {
		t.Fatalf("DefaultTestModel = %q, want %q", preset.DefaultTestModel, "kimi-k2.6")
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
	if len(preset.DefaultModels) != 4 {
		t.Fatalf("len(DefaultModels) = %d, want 4", len(preset.DefaultModels))
	}

	wantModels := []string{"kimi-k2.6", "kimi-k2.5", "kimi-k2-thinking", "kimi-k2-thinking-turbo"}
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

func TestMoonshotCompatibleProviderPreset_ChatRejectsRequiredToolChoice(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
	}

	body := []byte(`{
		"model":"kimi-k2.5",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"type":"function","function":{"name":"pwd","parameters":{"type":"object"}}}],
		"tool_choice":"required"
	}`)

	_, err := svc.prepareRequest(account, CompatibleRouteChatCompletions, body)
	if err == nil {
		t.Fatal("prepareRequest() error = nil, want deterministic compatibility error")
	}
	var clientErr *CompatibleClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("prepareRequest() error = %T, want *CompatibleClientError", err)
	}
	if clientErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", clientErr.StatusCode)
	}
}

func TestMoonshotCompatibleProviderPreset_ResponsesBridgeRejectsPreviousResponseID(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
	}

	body := []byte(`{
		"model":"kimi-k2.5",
		"previous_response_id":"resp_123",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]
	}`)

	_, err := svc.prepareRequest(account, CompatibleRouteResponses, body)
	if err == nil {
		t.Fatal("prepareRequest() error = nil, want deterministic compatibility error")
	}
	var clientErr *CompatibleClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("prepareRequest() error = %T, want *CompatibleClientError", err)
	}
	if !strings.Contains(clientErr.Message, "previous_response_id") {
		t.Fatalf("Message = %q, want previous_response_id", clientErr.Message)
	}
}

func TestPatchMoonshotCompatibleChatBody_PreservesOfficialToolHistory(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"messages":[
			{"role":"user","content":"hi"},
			{
				"role":"assistant",
				"content":"",
				"tool_calls":[
					{
						"id":"call_123",
						"type":"function",
						"function":{"name":"pwd","arguments":"{\"command\":\"rtk read G:\\SoftwareDevelopment\\project\\styles.css --tail-lines 20\"}"}
					}
				]
			},
			{
				"role":"tool",
				"tool_call_id":"call_123",
				"content":"C:/Users/cy/Downloads"
			}
		],
		"reasoning_effort":"high",
		"stream":true
	}`)

	patched, err := patchMoonshotCompatibleChatBody(body, nil, "kimi-k2.5")
	if err != nil {
		t.Fatalf("patchMoonshotCompatibleChatBody() error = %v", err)
	}
	if !gjson.GetBytes(patched, "messages.1.tool_calls").Exists() {
		t.Fatal("messages.1.tool_calls should be preserved for official Kimi chat")
	}
	if got := gjson.GetBytes(patched, "messages.1.tool_calls.0.function.arguments").String(); !strings.Contains(got, `G:\SoftwareDevelopment\project\styles.css`) {
		t.Fatalf("tool call arguments = %q, want Windows path backslashes preserved", got)
	}
	if got := gjson.GetBytes(patched, "messages.2.role").String(); got != "tool" {
		t.Fatalf("messages.2.role = %q, want %q", got, "tool")
	}
	if !gjson.GetBytes(patched, "reasoning_effort").Exists() {
		t.Fatal("reasoning_effort should be preserved for official Kimi chat")
	}
	if !gjson.GetBytes(patched, "stream_options.include_usage").Bool() {
		t.Fatal("stream_options.include_usage = false, want true")
	}
}

func TestPatchMoonshotCompatibleChatBodyForAnthropicFallback_CollapsesHistoricalToolCalls(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"messages":[
			{"role":"user","content":"hi"},
			{
				"role":"assistant",
				"content":"",
				"tool_calls":[
					{
						"id":"call_123",
						"type":"function",
						"function":{"name":"pwd","arguments":"{}"}
					}
				]
			},
			{
				"role":"tool",
				"tool_call_id":"call_123",
				"content":"C:/Users/cy/Downloads"
			}
		],
		"reasoning_effort":"high",
		"stream":true
	}`)

	patched, err := patchMoonshotCompatibleChatBodyForAnthropicFallback(body, nil, "kimi-k2.5")
	if err != nil {
		t.Fatalf("patchMoonshotCompatibleChatBodyForAnthropicFallback() error = %v", err)
	}
	if gjson.GetBytes(patched, "messages.1.tool_calls").Exists() {
		t.Fatal("messages.1.tool_calls should be collapsed to plain text")
	}
	if got := gjson.GetBytes(patched, "messages.1.content").String(); got != "Previous assistant tool call: id=call_123; name=pwd; arguments={}" {
		t.Fatalf("messages.1.content = %q, want collapsed tool_use text", got)
	}
	if got := gjson.GetBytes(patched, "messages.2.role").String(); got != "user" {
		t.Fatalf("messages.2.role = %q, want %q", got, "user")
	}
	if got := gjson.GetBytes(patched, "messages.2.content").String(); got != "Previous tool result for id=call_123\nC:/Users/cy/Downloads" {
		t.Fatalf("messages.2.content = %q, want collapsed tool_result text", got)
	}
	if got := gjson.GetBytes(patched, "messages.1.reasoning_content").String(); got != "" {
		t.Fatalf("messages.1.reasoning_content = %q, want empty after collapsing tool calls", got)
	}
	if gjson.GetBytes(patched, "reasoning_effort").Exists() {
		t.Fatal("reasoning_effort should be removed when tool_calls are present")
	}
	if !gjson.GetBytes(patched, "stream_options.include_usage").Bool() {
		t.Fatal("stream_options.include_usage = false, want true")
	}
}

func TestPatchMoonshotCompatibleMessagesBody_PreservesThinkingForToolUse(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"thinking":{"type":"enabled"},
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hi"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_123","name":"pwd","input":{"path":"."}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"C:/Users/cy/Downloads"}]}
		]
	}`)

	patched, err := patchMoonshotCompatibleMessagesBody(body, nil, "kimi-k2.5")
	if err != nil {
		t.Fatalf("patchMoonshotCompatibleMessagesBody() error = %v", err)
	}
	if !gjson.GetBytes(patched, "thinking").Exists() {
		t.Fatal("thinking should be preserved for official Kimi Anthropic messages")
	}
	if got := gjson.GetBytes(patched, "messages.1.content.0.type").String(); got != "tool_use" {
		t.Fatalf("messages.1.content.0.type = %q, want %q", got, "tool_use")
	}
	if got := gjson.GetBytes(patched, "messages.2.content.0.type").String(); got != "tool_result" {
		t.Fatalf("messages.2.content.0.type = %q, want %q", got, "tool_result")
	}
}

func TestPatchMoonshotCompatibleMessagesBody_PreservesOutputConfigEffortForToolUse(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"output_config":{"effort":"high"},
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hi"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_123","name":"pwd","input":{"path":"."}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"C:/Users/cy/Downloads"}]}
		]
	}`)

	patched, err := patchMoonshotCompatibleMessagesBody(body, nil, "kimi-k2.5")
	if err != nil {
		t.Fatalf("patchMoonshotCompatibleMessagesBody() error = %v", err)
	}
	if !gjson.GetBytes(patched, "output_config").Exists() {
		t.Fatal("output_config should be preserved for official Kimi Anthropic messages")
	}
	if got := gjson.GetBytes(patched, "messages.1.content.0.type").String(); got != "tool_use" {
		t.Fatalf("messages.1.content.0.type = %q, want %q", got, "tool_use")
	}
	if got := gjson.GetBytes(patched, "messages.2.content.0.type").String(); got != "tool_result" {
		t.Fatalf("messages.2.content.0.type = %q, want %q", got, "tool_result")
	}
}

func TestMoonshotCompatibleProviderPreset_CustomRelayKeepsNativeMessagesInPrepareRequest(t *testing.T) {
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
	if prepared.UpstreamKind != compatibleUpstreamMessages {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamMessages)
	}
	if prepared.UpstreamEndpoint != "/v1/messages" {
		t.Fatalf("UpstreamEndpoint = %q, want %q", prepared.UpstreamEndpoint, "/v1/messages")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "kimi-k2.5" {
		t.Fatalf("patched model = %q, want %q", got, "kimi-k2.5")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "messages.0.role").String(); got != "user" {
		t.Fatalf("messages.0.role = %q, want %q", got, "user")
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

func TestMoonshotCompatibleProviderPreset_MessagesPrepareRequestsCollapseChatFallbackToolHistory(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://api.hack3rx.cn/v1",
		},
	}

	body := []byte(`{
		"model":"kimi-k2.5",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hi"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_123","name":"pwd","input":{"path":"."}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"C:/Users/cy/Downloads"}]}
		],
		"max_tokens":32,
		"stream":true
	}`)

	preparedRequests, err := svc.prepareRequests(account, CompatibleRouteMessages, body)
	if err != nil {
		t.Fatalf("prepareRequests() error = %v", err)
	}
	if len(preparedRequests) != 2 {
		t.Fatalf("len(preparedRequests) = %d, want 2", len(preparedRequests))
	}
	if preparedRequests[0].UpstreamKind != compatibleUpstreamMessages {
		t.Fatalf("preparedRequests[0].UpstreamKind = %q, want %q", preparedRequests[0].UpstreamKind, compatibleUpstreamMessages)
	}
	if preparedRequests[1].UpstreamKind != compatibleUpstreamChat {
		t.Fatalf("preparedRequests[1].UpstreamKind = %q, want %q", preparedRequests[1].UpstreamKind, compatibleUpstreamChat)
	}
	if gjson.GetBytes(preparedRequests[1].RequestBody, "messages.1.tool_calls").Exists() {
		t.Fatalf("fallback request body should collapse assistant tool_calls: %s", string(preparedRequests[1].RequestBody))
	}
	if got := gjson.GetBytes(preparedRequests[1].RequestBody, "messages.1.content").String(); got != "Previous assistant tool call: id=fc_toolu_123; name=pwd; arguments={\"path\":\".\"}" {
		t.Fatalf("fallback messages.1.content = %q, want collapsed tool_use text", got)
	}
	if got := gjson.GetBytes(preparedRequests[1].RequestBody, "messages.2.role").String(); got != "user" {
		t.Fatalf("fallback messages.2.role = %q, want %q", got, "user")
	}
	if got := gjson.GetBytes(preparedRequests[1].RequestBody, "messages.2.content").String(); got != "Previous tool result for id=fc_toolu_123\nC:/Users/cy/Downloads" {
		t.Fatalf("fallback messages.2.content = %q, want collapsed tool_result text", got)
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
