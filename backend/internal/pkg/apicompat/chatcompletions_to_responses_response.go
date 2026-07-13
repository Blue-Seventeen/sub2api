package apicompat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChatCompletionsToResponsesResponse preserves the current fork call site while
// delegating to the upstream v0.1.130 bridge implementation.
func ChatCompletionsToResponsesResponse(resp *ChatCompletionsResponse) *ResponsesResponse {
	return ChatCompletionsResponseToResponses(resp, "", nil, false, nil)
}

func sanitizeToolCallArgumentsJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if json.Valid([]byte(raw)) {
		return raw
	}
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '{', '[':
			candidate := strings.TrimSpace(raw[i:])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	for end := len(raw); end > 0; end-- {
		candidate := strings.TrimSpace(raw[:end])
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}
	return raw
}

func ChatResponsesEventToSSE(event ResponsesStreamEvent) (string, error) {
	payload := map[string]any{
		"type": event.Type,
	}
	switch event.Type {
	case "response.created", "response.completed", "response.failed", "response.incomplete":
		if event.Response != nil {
			payload["response"] = event.Response
		}
	case "response.output_item.added", "response.output_item.done":
		payload["output_index"] = event.OutputIndex
		if event.Item != nil {
			payload["item"] = event.Item
		}
	case "response.output_text.delta":
		payload["output_index"] = event.OutputIndex
		payload["content_index"] = event.ContentIndex
		payload["delta"] = event.Delta
		if event.ItemID != "" {
			payload["item_id"] = event.ItemID
		}
	case "response.output_text.done":
		payload["output_index"] = event.OutputIndex
		payload["content_index"] = event.ContentIndex
		payload["text"] = event.Text
		if event.ItemID != "" {
			payload["item_id"] = event.ItemID
		}
	case "response.reasoning_summary_text.delta":
		payload["output_index"] = event.OutputIndex
		payload["summary_index"] = event.SummaryIndex
		payload["delta"] = event.Delta
	case "response.reasoning_summary_text.done":
		payload["output_index"] = event.OutputIndex
		payload["summary_index"] = event.SummaryIndex
		payload["text"] = event.Text
	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		payload["output_index"] = event.OutputIndex
		if event.CallID != "" {
			payload["call_id"] = event.CallID
		}
		if event.Name != "" {
			payload["name"] = event.Name
		}
		arguments := event.Arguments
		if arguments == "" {
			arguments = event.Delta
		}
		payload["arguments"] = arguments
	default:
		if event.Response != nil {
			payload["response"] = event.Response
		}
		if event.Item != nil {
			payload["item"] = event.Item
		}
	}
	if event.SequenceNumber != 0 {
		payload["sequence_number"] = event.SequenceNumber
	}
	if event.Code != "" {
		payload["code"] = event.Code
	}
	if event.Param != "" {
		payload["param"] = event.Param
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("data: %s\n\n", data), nil
}
