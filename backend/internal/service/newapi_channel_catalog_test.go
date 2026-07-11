package service

import "testing"

func TestNewAPIReferenceChannelCatalogCoversReferenceDirectories(t *testing.T) {
	catalog := NewAPIReferenceChannelCatalog()
	if len(catalog) != 36 {
		t.Fatalf("catalog length = %d, want 36", len(catalog))
	}

	seen := make(map[string]struct{}, len(catalog))
	for _, channel := range catalog {
		if channel.Name == "" {
			t.Fatalf("channel name is empty: %+v", channel)
		}
		if channel.Status == "" {
			t.Fatalf("channel status is empty: %+v", channel)
		}
		if _, exists := seen[channel.Name]; exists {
			t.Fatalf("duplicate channel: %s", channel.Name)
		}
		seen[channel.Name] = struct{}{}
	}

	for _, name := range []string{
		"ai360", "ali", "aws", "baidu", "baidu_v2", "claude", "cloudflare",
		"codex", "cohere", "coze", "deepseek", "dify", "gemini", "jimeng",
		"jina", "lingyiwanwu", "minimax", "mistral", "mokaai", "moonshot",
		"ollama", "openai", "openrouter", "palm", "perplexity", "replicate",
		"siliconflow", "submodel", "task", "tencent", "vertex", "volcengine",
		"xinference", "xunfei", "zhipu", "zhipu_4v",
	} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing channel: %s", name)
		}
	}
}

func TestNewAPIReferenceChannelStatusForName(t *testing.T) {
	tests := []struct {
		name   string
		status NewAPIReferenceChannelStatus
	}{
		{name: "openrouter", status: NewAPIReferenceChannelEnabledPreset},
		{name: "OpenAI", status: NewAPIReferenceChannelExistingCustom},
		{name: "replicate", status: NewAPIReferenceChannelTaskWorkerRequired},
		{name: "baidu", status: NewAPIReferenceChannelDedicatedRequired},
		{name: "ai360", status: NewAPIReferenceChannelCandidateUnverified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel, ok := NewAPIReferenceChannelStatusForName(tt.name)
			if !ok {
				t.Fatalf("channel %q not found", tt.name)
			}
			if channel.Status != tt.status {
				t.Fatalf("status = %s, want %s", channel.Status, tt.status)
			}
		})
	}
}
