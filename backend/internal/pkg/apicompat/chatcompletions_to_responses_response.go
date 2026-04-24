package apicompat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func ChatCompletionsToResponsesResponse(resp *ChatCompletionsResponse) *ResponsesResponse {
	if resp == nil {
		return nil
	}
	out := &ResponsesResponse{
		ID:     resp.ID,
		Object: "response",
		Model:  resp.Model,
		Status: "completed",
	}
	if len(resp.Choices) == 0 {
		out.Output = []ResponsesOutput{}
		return out
	}

	choice := resp.Choices[0]
	outputs := make([]ResponsesOutput, 0, 3)
	if choice.Message.ReasoningContent != "" {
		outputs = append(outputs, ResponsesOutput{
			Type: "reasoning",
			Summary: []ResponsesSummary{{
				Type: "summary_text",
				Text: choice.Message.ReasoningContent,
			}},
		})
	}
	if content := extractChatMessageText(choice.Message.Content); content != "" {
		outputs = append(outputs, ResponsesOutput{
			Type:   "message",
			ID:     "msg_" + resp.ID,
			Role:   "assistant",
			Status: "completed",
			Content: []ResponsesContentPart{{
				Type: "output_text",
				Text: content,
			}},
		})
	}
	for _, toolCall := range choice.Message.ToolCalls {
		outputs = append(outputs, ResponsesOutput{
			Type:      "function_call",
			CallID:    toolCall.ID,
			Name:      toolCall.Function.Name,
			Arguments: sanitizeToolCallArgumentsJSON(toolCall.Function.Arguments),
		})
	}
	out.Output = outputs

	if resp.Usage != nil {
		out.Usage = &ResponsesUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		}
		if resp.Usage.PromptTokensDetails != nil {
			out.Usage.InputTokensDetails = &ResponsesInputTokensDetails{
				CachedTokens: resp.Usage.PromptTokensDetails.CachedTokens,
			}
		}
	}
	if choice.FinishReason == "length" {
		out.Status = "incomplete"
		out.IncompleteDetails = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}
	return out
}

type ChatCompletionsToResponsesState struct {
	ID                   string
	Model                string
	Created              int64
	ResponseCreatedSent  bool
	MessageOutputIndex   *int
	ReasoningOutputIndex *int
	ToolCallOutputIndex  map[int]int
	MessageItemID        string
	MessageText          string
	ReasoningText        string
	ToolCalls            map[int]ResponsesOutput
	NextOutputIndex      int
	Finalized            bool
	IncludeUsage         bool
	Usage                *ResponsesUsage
}

func NewChatCompletionsToResponsesState() *ChatCompletionsToResponsesState {
	return &ChatCompletionsToResponsesState{
		Created:             time.Now().Unix(),
		ToolCallOutputIndex: make(map[int]int),
		ToolCalls:           make(map[int]ResponsesOutput),
	}
}

func ChatCompletionsChunkToResponsesEvents(chunk *ChatCompletionsChunk, state *ChatCompletionsToResponsesState) []ResponsesStreamEvent {
	if chunk == nil || state == nil {
		return nil
	}
	if chunk.ID != "" {
		state.ID = chunk.ID
	}
	if chunk.Model != "" {
		state.Model = chunk.Model
	}
	if chunk.Created > 0 {
		state.Created = chunk.Created
	}

	var events []ResponsesStreamEvent
	if !state.ResponseCreatedSent {
		state.ResponseCreatedSent = true
		events = append(events, ResponsesStreamEvent{
			Type: "response.created",
			Response: &ResponsesResponse{
				ID:     state.ID,
				Object: "response",
				Model:  state.Model,
				Status: "in_progress",
			},
		})
	}

	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil {
			state.Usage = chatUsageToResponsesUsage(chunk.Usage)
		}
		return events
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
		if state.ReasoningOutputIndex == nil {
			idx := state.NextOutputIndex
			state.NextOutputIndex++
			state.ReasoningOutputIndex = &idx
			events = append(events, ResponsesStreamEvent{
				Type:        "response.output_item.added",
				OutputIndex: idx,
				Item: &ResponsesOutput{
					Type: "reasoning",
				},
			})
		}
		state.ReasoningText += *delta.ReasoningContent
		events = append(events, ResponsesStreamEvent{
			Type:         "response.reasoning_summary_text.delta",
			OutputIndex:  *state.ReasoningOutputIndex,
			SummaryIndex: 0,
			Delta:        *delta.ReasoningContent,
		})
	}

	if delta.Content != nil && *delta.Content != "" {
		if state.MessageOutputIndex == nil {
			idx := state.NextOutputIndex
			state.NextOutputIndex++
			state.MessageOutputIndex = &idx
			state.MessageItemID = "msg_" + state.ID
			events = append(events, ResponsesStreamEvent{
				Type:        "response.output_item.added",
				OutputIndex: idx,
				Item: &ResponsesOutput{
					Type:   "message",
					ID:     state.MessageItemID,
					Role:   "assistant",
					Status: "in_progress",
				},
			})
		}
		state.MessageText += *delta.Content
		events = append(events, ResponsesStreamEvent{
			Type:         "response.output_text.delta",
			ItemID:       state.MessageItemID,
			OutputIndex:  *state.MessageOutputIndex,
			ContentIndex: 0,
			Delta:        *delta.Content,
		})
	}

	for _, toolCall := range delta.ToolCalls {
		toolIdx := 0
		if toolCall.Index != nil {
			toolIdx = *toolCall.Index
		}
		outputIndex, exists := state.ToolCallOutputIndex[toolIdx]
		if !exists {
			outputIndex = state.NextOutputIndex
			state.NextOutputIndex++
			state.ToolCallOutputIndex[toolIdx] = outputIndex
			state.ToolCalls[toolIdx] = ResponsesOutput{
				Type:   "function_call",
				CallID: toolCall.ID,
				Name:   toolCall.Function.Name,
			}
			events = append(events, ResponsesStreamEvent{
				Type:        "response.output_item.added",
				OutputIndex: outputIndex,
				Item: &ResponsesOutput{
					Type:   "function_call",
					CallID: toolCall.ID,
					Name:   toolCall.Function.Name,
				},
			})
		}
		if toolCall.Function.Arguments != "" {
			current := state.ToolCalls[toolIdx]
			prevArgs := current.Arguments
			current.Arguments = sanitizeToolCallArgumentsJSON(prevArgs + toolCall.Function.Arguments)
			state.ToolCalls[toolIdx] = current
			deltaArgs := current.Arguments
			if prevArgs != "" && strings.HasPrefix(current.Arguments, prevArgs) {
				deltaArgs = current.Arguments[len(prevArgs):]
			}
			if deltaArgs == "" {
				continue
			}
			events = append(events, ResponsesStreamEvent{
				Type:        "response.function_call_arguments.delta",
				OutputIndex: outputIndex,
				CallID:      toolCall.ID,
				Name:        toolCall.Function.Name,
				Arguments:   deltaArgs,
			})
		}
	}

	if chunk.Usage != nil {
		state.Usage = chatUsageToResponsesUsage(chunk.Usage)
	}

	if choice.FinishReason != nil && *choice.FinishReason != "" {
		events = append(events, FinalizeChatCompletionsResponsesStream(state, *choice.FinishReason)...)
	}

	return events
}

func FinalizeChatCompletionsResponsesStream(state *ChatCompletionsToResponsesState, finishReason string) []ResponsesStreamEvent {
	if state == nil || state.Finalized {
		return nil
	}
	state.Finalized = true

	status := "completed"
	var incomplete *ResponsesIncompleteDetails
	if finishReason == "length" {
		status = "incomplete"
		incomplete = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
	}

	resp := &ResponsesResponse{
		ID:                state.ID,
		Object:            "response",
		Model:             state.Model,
		Status:            status,
		IncompleteDetails: incomplete,
		Usage:             state.Usage,
		Output:            buildResponsesOutputsFromState(state, status),
	}

	events := make([]ResponsesStreamEvent, 0, 6)
	if state.ReasoningOutputIndex != nil {
		events = append(events, ResponsesStreamEvent{
			Type:         "response.reasoning_summary_text.done",
			OutputIndex:  *state.ReasoningOutputIndex,
			SummaryIndex: 0,
			Text:         state.ReasoningText,
		})
		events = append(events, ResponsesStreamEvent{
			Type:        "response.output_item.done",
			OutputIndex: *state.ReasoningOutputIndex,
			Item: &ResponsesOutput{
				Type: "reasoning",
				Summary: []ResponsesSummary{{
					Type: "summary_text",
					Text: state.ReasoningText,
				}},
			},
		})
	}
	if state.MessageOutputIndex != nil {
		events = append(events, ResponsesStreamEvent{
			Type:         "response.output_text.done",
			ItemID:       state.MessageItemID,
			OutputIndex:  *state.MessageOutputIndex,
			ContentIndex: 0,
			Text:         state.MessageText,
		})
		events = append(events, ResponsesStreamEvent{
			Type:        "response.output_item.done",
			OutputIndex: *state.MessageOutputIndex,
			Item: &ResponsesOutput{
				Type:   "message",
				ID:     state.MessageItemID,
				Role:   "assistant",
				Status: "completed",
				Content: []ResponsesContentPart{{
					Type: "output_text",
					Text: state.MessageText,
				}},
			},
		})
	}
	for toolIdx, outputIndex := range state.ToolCallOutputIndex {
		item := state.ToolCalls[toolIdx]
		events = append(events, ResponsesStreamEvent{
			Type:        "response.function_call_arguments.done",
			OutputIndex: outputIndex,
			CallID:      item.CallID,
			Name:        item.Name,
			Arguments:   item.Arguments,
		})
		toolItem := item
		events = append(events, ResponsesStreamEvent{
			Type:        "response.output_item.done",
			OutputIndex: outputIndex,
			Item:        &toolItem,
		})
	}
	events = append(events, ResponsesStreamEvent{
		Type:     "response.completed",
		Response: resp,
	})
	return events
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
		payload["arguments"] = event.Arguments
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

func buildResponsesOutputsFromState(state *ChatCompletionsToResponsesState, status string) []ResponsesOutput {
	if state == nil {
		return nil
	}
	outputs := make([]ResponsesOutput, 0, 2+len(state.ToolCalls))
	if state.ReasoningText != "" {
		outputs = append(outputs, ResponsesOutput{
			Type: "reasoning",
			Summary: []ResponsesSummary{{
				Type: "summary_text",
				Text: state.ReasoningText,
			}},
		})
	}
	if state.MessageOutputIndex != nil {
		messageStatus := status
		if messageStatus == "" {
			messageStatus = "completed"
		}
		outputs = append(outputs, ResponsesOutput{
			Type:   "message",
			ID:     state.MessageItemID,
			Role:   "assistant",
			Status: messageStatus,
			Content: []ResponsesContentPart{{
				Type: "output_text",
				Text: state.MessageText,
			}},
		})
	}
	for toolIdx, item := range state.ToolCalls {
		if _, ok := state.ToolCallOutputIndex[toolIdx]; ok {
			outputs = append(outputs, item)
		}
	}
	return outputs
}

func extractChatMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []ChatContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var combined string
		for _, part := range parts {
			if part.Type == "text" && part.Text != "" {
				combined += part.Text
			}
		}
		return combined
	}
	return ""
}

func chatUsageToResponsesUsage(usage *ChatUsage) *ResponsesUsage {
	if usage == nil {
		return nil
	}
	out := &ResponsesUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.InputTokensDetails = &ResponsesInputTokensDetails{
			CachedTokens: usage.PromptTokensDetails.CachedTokens,
		}
	}
	return out
}
