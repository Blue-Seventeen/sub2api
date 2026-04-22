package service

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestCompatibleGatewayServicePrepareRequest_RewritesMappedModelForChat(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformZhipu,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"gpt-5.4": "glm-4.6v",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteChatCompletions, []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamModel != "glm-4.6v" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "glm-4.6v")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "glm-4.6v" {
		t.Fatalf("patched request model = %q, want %q", got, "glm-4.6v")
	}
}

func TestCompatibleGatewayServicePrepareRequest_RewritesMappedModelForNativeResponses(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformAli,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"gpt-5.4": "qwen-max",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteResponses, []byte(`{"model":"gpt-5.4","input":"hi"}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamModel != "qwen-max" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "qwen-max")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "qwen-max" {
		t.Fatalf("patched request model = %q, want %q", got, "qwen-max")
	}
}

func TestCompatibleGatewayServicePrepareRequest_RewritesMappedModelForNativeMessages(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"claude-sonnet-4": "kimi-k2.5",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteMessages, []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamModel != "kimi-k2.5" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "kimi-k2.5")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "kimi-k2.5" {
		t.Fatalf("patched request model = %q, want %q", got, "kimi-k2.5")
	}
}

func TestCompatibleGatewayServicePrepareRequest_UsesNativeZhipuMessages(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformZhipu,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"claude-sonnet-4-20250514": "glm-4.6v",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteMessages, []byte(`{
		"model":"claude-sonnet-4-20250514",
		"max_tokens":64,
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]
	}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamKind != compatibleUpstreamMessages {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamMessages)
	}
	if prepared.UpstreamEndpoint != "/v1/messages" {
		t.Fatalf("UpstreamEndpoint = %q, want %q", prepared.UpstreamEndpoint, "/v1/messages")
	}
	if prepared.UpstreamModel != "glm-4.6v" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "glm-4.6v")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "glm-4.6v" {
		t.Fatalf("patched request model = %q, want %q", got, "glm-4.6v")
	}
	if got := svc.buildURLForPreparedRequest(account, prepared, "https://open.bigmodel.cn"); got != "https://open.bigmodel.cn/api/anthropic/v1/messages" {
		t.Fatalf("buildURLForPreparedRequest() = %q, want %q", got, "https://open.bigmodel.cn/api/anthropic/v1/messages")
	}
}
