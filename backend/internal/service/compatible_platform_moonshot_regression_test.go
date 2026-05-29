package service

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestMoonshotResponsesFallbackKeepsChatTextParts(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteResponses, []byte(`{
		"model": "kimi-k2.5",
		"input": [
			{
				"role": "user",
				"content": [
					{"type": "input_text", "text": "hi"}
				]
			}
		],
		"stream": true
	}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamKind != compatibleUpstreamChat {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamChat)
	}
	if got := gjson.GetBytes(prepared.RequestBody, "messages.0.content.0.type").String(); got != "text" {
		t.Fatalf("messages.0.content.0.type = %q, want text", got)
	}
	if got := gjson.GetBytes(prepared.RequestBody, "messages.0.content.0.text").String(); got != "hi" {
		t.Fatalf("messages.0.content.0.text = %q, want hi", got)
	}
}

func TestMoonshotMessagesChatFallbackPrefixesAnthropicToolIDs(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://api.example.invalid/v1",
		},
	}

	preparedRequests, err := svc.prepareRequests(account, CompatibleRouteMessages, []byte(`{
		"model":"kimi-k2.5",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hi"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_123","name":"pwd","input":{"path":"."}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"C:/Users/cy/Downloads"}]}
		],
		"max_tokens":32,
		"stream":true
	}`))
	if err != nil {
		t.Fatalf("prepareRequests() error = %v", err)
	}
	if len(preparedRequests) != 2 {
		t.Fatalf("len(preparedRequests) = %d, want 2", len(preparedRequests))
	}
	fallback := preparedRequests[1]
	if fallback.UpstreamKind != compatibleUpstreamChat {
		t.Fatalf("fallback UpstreamKind = %q, want %q", fallback.UpstreamKind, compatibleUpstreamChat)
	}
	if got := gjson.GetBytes(fallback.RequestBody, "messages.1.content").String(); got != "Previous assistant tool call: id=fc_toolu_123; name=pwd; arguments={\"path\":\".\"}" {
		t.Fatalf("fallback messages.1.content = %q, want collapsed prefixed tool_use text", got)
	}
	if got := gjson.GetBytes(fallback.RequestBody, "messages.2.content").String(); got != "Previous tool result for id=fc_toolu_123\nC:/Users/cy/Downloads" {
		t.Fatalf("fallback messages.2.content = %q, want collapsed prefixed tool_result text", got)
	}
}
