package service

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func normalizeTopPForCompatibleBody(body []byte, _ *Account, _ string) ([]byte, error) {
	return normalizeTopPForCompatibleBodyRaw(body), nil
}

func patchMoonshotCompatibleChatBody(body []byte, _ *Account, _ string) ([]byte, error) {
	body = normalizeTopPForCompatibleBodyRaw(body)
	body = collapseMoonshotHistoricalToolCallsToText(body)
	body = ensureMoonshotReasoningContentForToolCalls(body)
	body = stripMoonshotReasoningEffortForToolCalls(body)
	body = ensureCompatibleStreamingUsageIncluded(body)
	return body, nil
}

func patchMoonshotCompatibleChatBodyForAnthropicFallback(body []byte, _ *Account, _ string) ([]byte, error) {
	body = normalizeTopPForCompatibleBodyRaw(body)
	body = ensureMoonshotReasoningContentForToolCalls(body)
	body = stripMoonshotReasoningEffortForToolCalls(body)
	body = ensureCompatibleStreamingUsageIncluded(body)
	return body, nil
}

func patchMoonshotCompatibleMessagesBody(body []byte, _ *Account, _ string) ([]byte, error) {
	body = relaxMoonshotThinkingForToolUse(body)
	return body, nil
}

func normalizeTopPForCompatibleBodyRaw(body []byte) []byte {
	topP := gjson.GetBytes(body, "top_p")
	if !topP.Exists() || topP.Type != gjson.Number {
		return body
	}
	value := topP.Float()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return body
	}
	switch {
	case value >= 1:
		updated, err := sjson.SetBytes(body, "top_p", 0.99)
		if err == nil {
			return updated
		}
	case value <= 0:
		updated, err := sjson.SetBytes(body, "top_p", 0.001)
		if err == nil {
			return updated
		}
	}
	return body
}

func ensureCompatibleStreamingUsageIncluded(body []byte) []byte {
	stream := gjson.GetBytes(body, "stream")
	if !stream.Exists() || !stream.Bool() {
		return body
	}
	updated, err := sjson.SetBytes(body, "stream_options.include_usage", true)
	if err != nil {
		return body
	}
	return updated
}

func collapseMoonshotHistoricalToolCallsToText(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	hasAssistantToolCalls := bytes.Contains(body, []byte(`"tool_calls"`))
	hasToolRole := bytes.Contains(body, []byte(`"role":"tool"`)) || bytes.Contains(body, []byte(`"role": "tool"`))
	if !hasAssistantToolCalls && !hasToolRole {
		return body
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	rawMessages, ok := payload["messages"].([]any)
	if !ok || len(rawMessages) == 0 {
		return body
	}

	newMessages := make([]any, 0, len(rawMessages))
	changed := false

	for _, rawMsg := range rawMessages {
		msgMap, ok := rawMsg.(map[string]any)
		if !ok {
			newMessages = append(newMessages, rawMsg)
			continue
		}

		role, _ := msgMap["role"].(string)
		switch strings.ToLower(strings.TrimSpace(role)) {
		case "assistant":
			if rewritten, ok := rewriteMoonshotAssistantToolCallMessage(msgMap); ok {
				newMessages = append(newMessages, rewritten)
				changed = true
				continue
			}
		case "tool":
			if rewritten, ok := rewriteMoonshotToolResultMessage(msgMap); ok {
				newMessages = append(newMessages, rewritten)
				changed = true
				continue
			}
		}

		newMessages = append(newMessages, rawMsg)
	}

	if !changed {
		return body
	}

	payload["messages"] = newMessages
	if _, exists := payload["reasoning_effort"]; exists {
		delete(payload, "reasoning_effort")
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func rewriteMoonshotAssistantToolCallMessage(msgMap map[string]any) (map[string]any, bool) {
	toolCalls, ok := msgMap["tool_calls"].([]any)
	if !ok || len(toolCalls) == 0 {
		return nil, false
	}

	textParts := make([]string, 0, len(toolCalls)+1)
	if content := extractMoonshotChatMessageContentText(msgMap["content"]); strings.TrimSpace(content) != "" {
		textParts = append(textParts, content)
	}

	for _, rawToolCall := range toolCalls {
		toolCallMap, ok := rawToolCall.(map[string]any)
		if !ok {
			continue
		}
		text := "(tool_use)"
		if id, _ := toolCallMap["id"].(string); strings.TrimSpace(id) != "" {
			text += " id=" + id
		}
		if functionMap, ok := toolCallMap["function"].(map[string]any); ok {
			if name, _ := functionMap["name"].(string); strings.TrimSpace(name) != "" {
				text += " name=" + name
			}
			if args, _ := functionMap["arguments"].(string); strings.TrimSpace(args) != "" {
				text += " arguments=" + args
			}
		}
		textParts = append(textParts, text)
	}

	text := strings.TrimSpace(strings.Join(textParts, "\n"))
	if text == "" {
		text = "(tool_use)"
	}
	return map[string]any{
		"role":    "assistant",
		"content": text,
	}, true
}

func rewriteMoonshotToolResultMessage(msgMap map[string]any) (map[string]any, bool) {
	text := "(tool_result)"
	if toolCallID, _ := msgMap["tool_call_id"].(string); strings.TrimSpace(toolCallID) != "" {
		text += " tool_call_id=" + toolCallID
	}
	if content := extractMoonshotChatMessageContentText(msgMap["content"]); strings.TrimSpace(content) != "" {
		text += "\n" + content
	}
	return map[string]any{
		"role":    "user",
		"content": strings.TrimSpace(text),
	}, true
}

func extractMoonshotChatMessageContentText(content any) string {
	switch typed := content.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, rawPart := range typed {
			partMap, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := partMap["type"].(string)
			switch partType {
			case "text":
				if text, _ := partMap["text"].(string); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			default:
				if blob, err := json.Marshal(partMap); err == nil && len(blob) > 0 {
					parts = append(parts, string(blob))
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		blob, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(blob)
	}
}

func stripMoonshotReasoningEffortForToolCalls(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	if !bytes.Contains(body, []byte(`"tool_calls"`)) {
		return body
	}
	if !gjson.GetBytes(body, "reasoning_effort").Exists() {
		return body
	}
	updated, err := sjson.DeleteBytes(body, "reasoning_effort")
	if err != nil {
		return body
	}
	return updated
}

func ensureMoonshotReasoningContentForToolCalls(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	if !bytes.Contains(body, []byte(`"tool_calls"`)) {
		return body
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return body
	}

	changed := false
	for _, rawMsg := range messages {
		msgMap, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		if !strings.EqualFold(strings.TrimSpace(role), "assistant") {
			continue
		}
		toolCalls, ok := msgMap["tool_calls"].([]any)
		if !ok || len(toolCalls) == 0 {
			continue
		}
		if existing, ok := msgMap["reasoning_content"].(string); ok && strings.TrimSpace(existing) != "" {
			continue
		}
		msgMap["reasoning_content"] = "."
		changed = true
	}
	if !changed {
		return body
	}

	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func relaxMoonshotThinkingForToolUse(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	if !gjson.GetBytes(body, "thinking").Exists() {
		return body
	}
	if !bytes.Contains(body, []byte(`"type":"tool_use"`)) &&
		!bytes.Contains(body, []byte(`"type": "tool_use"`)) &&
		!bytes.Contains(body, []byte(`"type":"tool_result"`)) &&
		!bytes.Contains(body, []byte(`"type": "tool_result"`)) {
		return body
	}
	return FilterThinkingBlocksForRetry(body)
}

func normalizeStopStringToArray(body []byte) []byte {
	stop := gjson.GetBytes(body, "stop")
	if !stop.Exists() || stop.Type != gjson.String {
		return body
	}
	updated, err := sjson.SetBytes(body, "stop", []string{stop.String()})
	if err != nil {
		return body
	}
	return updated
}

func stripDataPrefixFromImageURLs(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	changed := false

	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, value := range typed {
				switch v := value.(type) {
				case string:
					if key == "url" || key == "image_url" {
						if trimmed, ok := stripDataImagePrefix(v); ok {
							typed[key] = trimmed
							changed = true
						}
					}
				default:
					walk(v)
				}
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}

	walk(payload)
	if !changed {
		return body
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func normalizeDeveloperRoleToSystem(body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return body
	}

	changed := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, ok := msgMap["role"].(string)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(role), "developer") {
			msgMap["role"] = "system"
			changed = true
		}
	}
	if !changed {
		return body
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func stripDataImagePrefix(v string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(v))
	if !strings.HasPrefix(lower, "data:image/") {
		return v, false
	}
	idx := strings.Index(lower, "base64,")
	if idx < 0 {
		return v, false
	}
	origIdx := strings.Index(strings.TrimSpace(v), "base64,")
	if origIdx < 0 {
		return v, false
	}
	trimmed := strings.TrimSpace(v)[origIdx+len("base64,"):]
	if trimmed == "" {
		return v, false
	}
	return trimmed, true
}
