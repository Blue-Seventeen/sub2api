package service

import "strings"

type NewAPIReferenceChannelStatus string

const (
	NewAPIReferenceChannelEnabledPreset       NewAPIReferenceChannelStatus = "enabled_preset"
	NewAPIReferenceChannelExistingCustom      NewAPIReferenceChannelStatus = "existing_custom"
	NewAPIReferenceChannelDedicatedRequired   NewAPIReferenceChannelStatus = "dedicated_required"
	NewAPIReferenceChannelTaskWorkerRequired  NewAPIReferenceChannelStatus = "task_worker_required"
	NewAPIReferenceChannelCandidateUnverified NewAPIReferenceChannelStatus = "candidate_unverified"
)

type NewAPIReferenceChannel struct {
	Name   string
	Status NewAPIReferenceChannelStatus
	Reason string
}

var newAPIReferenceChannelCatalog = []NewAPIReferenceChannel{
	{Name: "ai360", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "OpenAI-like candidate; verify upstream schema and usage before enabling."},
	{Name: "ali", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing Ali/Qwen compatible preset and platform patches."},
	{Name: "aws", Status: NewAPIReferenceChannelExistingCustom, Reason: "Covered by existing Bedrock account type and SigV4 flow."},
	{Name: "baidu", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Baidu Wenxin API needs a dedicated adapter and token flow."},
	{Name: "baidu_v2", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Baidu v2 endpoints need dedicated request and response mapping."},
	{Name: "claude", Status: NewAPIReferenceChannelExistingCustom, Reason: "Covered by existing Anthropic gateway and OAuth/API key paths."},
	{Name: "cloudflare", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Cloudflare Workers AI needs account-scoped paths and auth."},
	{Name: "codex", Status: NewAPIReferenceChannelExistingCustom, Reason: "Covered by existing OpenAI Codex/OAuth/Responses custom paths."},
	{Name: "cohere", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "Cohere chat/rerank schemas need usage verification before enabling."},
	{Name: "coze", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Coze bot/workflow protocol needs a dedicated adapter."},
	{Name: "deepseek", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing DeepSeek compatible preset."},
	{Name: "dify", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Dify app/chatflow protocol needs a dedicated adapter."},
	{Name: "gemini", Status: NewAPIReferenceChannelExistingCustom, Reason: "Covered by existing Gemini native/OAuth/session paths."},
	{Name: "jimeng", Status: NewAPIReferenceChannelTaskWorkerRequired, Reason: "Jimeng visual generation is task-oriented and needs lifecycle handling."},
	{Name: "jina", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "Embedding/rerank billing and response schema need verification."},
	{Name: "lingyiwanwu", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "OpenAI-like candidate; verify upstream behavior before enabling."},
	{Name: "minimax", Status: NewAPIReferenceChannelTaskWorkerRequired, Reason: "MiniMax media endpoints need task/media handling."},
	{Name: "mistral", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing Mistral New-API style preset."},
	{Name: "mokaai", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "Compatibility and usage extraction are unverified."},
	{Name: "moonshot", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing Moonshot/Kimi compatible preset and fallbacks."},
	{Name: "ollama", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "Self-hosted OpenAI-like behavior varies by deployment."},
	{Name: "openai", Status: NewAPIReferenceChannelExistingCustom, Reason: "Covered by existing OpenAI gateway, Responses, Images, and WS paths."},
	{Name: "openrouter", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing OpenRouter New-API style preset."},
	{Name: "palm", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Legacy PaLM protocol needs a dedicated adapter."},
	{Name: "perplexity", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing Perplexity New-API style preset."},
	{Name: "replicate", Status: NewAPIReferenceChannelTaskWorkerRequired, Reason: "Replicate predictions are asynchronous tasks."},
	{Name: "siliconflow", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing SiliconFlow preset; rerank is explicitly routed."},
	{Name: "submodel", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "Lightweight relay candidate; usage and streaming need verification."},
	{Name: "task", Status: NewAPIReferenceChannelTaskWorkerRequired, Reason: "New-API task aggregator requires lifecycle worker semantics."},
	{Name: "tencent", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Tencent Hunyuan needs signed request support."},
	{Name: "vertex", Status: NewAPIReferenceChannelExistingCustom, Reason: "Covered by existing Vertex service account paths."},
	{Name: "volcengine", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing VolcEngine compatible preset and patches."},
	{Name: "xai", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing xAI New-API style preset."},
	{Name: "xinference", Status: NewAPIReferenceChannelCandidateUnverified, Reason: "Self-hosted compatibility and base URL behavior need verification."},
	{Name: "xunfei", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Xunfei Spark needs dedicated signed/WebSocket handling."},
	{Name: "zhipu", Status: NewAPIReferenceChannelEnabledPreset, Reason: "Covered by the existing GLM/Zhipu compatible preset and auth mode."},
	{Name: "zhipu_4v", Status: NewAPIReferenceChannelDedicatedRequired, Reason: "Zhipu 4V/image paths need dedicated image adapter semantics."},
}

func NewAPIReferenceChannelCatalog() []NewAPIReferenceChannel {
	out := make([]NewAPIReferenceChannel, len(newAPIReferenceChannelCatalog))
	copy(out, newAPIReferenceChannelCatalog)
	return out
}

func NewAPIReferenceChannelStatusForName(name string) (NewAPIReferenceChannel, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, channel := range newAPIReferenceChannelCatalog {
		if channel.Name == normalized {
			return channel, true
		}
	}
	return NewAPIReferenceChannel{}, false
}
