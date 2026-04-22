package apicompat

import (
	"encoding/json"
	"fmt"
)

// ResponsesToChatCompletionsRequest converts a Responses API request into a
// Chat Completions request. This is the reverse of ChatCompletionsToResponses
// for the subset of features currently used by sub2api's compatible providers.
func ResponsesToChatCompletionsRequest(req *ResponsesRequest) (*ChatCompletionsRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("responses request is nil")
	}

	msgs, err := responsesInputToChatMessages(req.Input)
	if err != nil {
		return nil, err
	}

	out := &ChatCompletionsRequest{
		Model:       req.Model,
		Messages:    msgs,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		ToolChoice:  req.ToolChoice,
		ServiceTier: req.ServiceTier,
	}

	if req.MaxOutputTokens != nil {
		out.MaxTokens = req.MaxOutputTokens
		out.MaxCompletionTokens = req.MaxOutputTokens
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	if len(req.Tools) > 0 {
		out.Tools = make([]ChatTool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			switch tool.Type {
			case "", "function":
				out.Tools = append(out.Tools, ChatTool{
					Type: "function",
					Function: &ChatFunction{
						Name:        tool.Name,
						Description: tool.Description,
						Parameters:  tool.Parameters,
						Strict:      tool.Strict,
					},
				})
			}
		}
	}
	if req.Stream {
		out.StreamOptions = &ChatStreamOptions{IncludeUsage: true}
	}

	return out, nil
}

func responsesInputToChatMessages(raw json.RawMessage) ([]ChatMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var asText string
	if err := json.Unmarshal(raw, &asText); err == nil {
		content, _ := json.Marshal(asText)
		return []ChatMessage{{Role: "user", Content: content}}, nil
	}

	var items []ResponsesInputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse responses input: %w", err)
	}

	messages := make([]ChatMessage, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "", "message":
			role := item.Role
			if role == "" {
				role = "user"
			}
			msg := ChatMessage{Role: role}
			if len(item.Content) > 0 {
				content := normalizeResponsesContentToChat(item.Content)
				msg.Content = content
			}
			messages = append(messages, msg)
		case "function_call":
			content, _ := json.Marshal("")
			messages = append(messages, ChatMessage{
				Role:    "assistant",
				Content: content,
				ToolCalls: []ChatToolCall{{
					ID:   item.CallID,
					Type: "function",
					Function: ChatFunctionCall{
						Name:      item.Name,
						Arguments: item.Arguments,
					},
				}},
			})
		case "function_call_output":
			content, _ := json.Marshal(item.Output)
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: item.CallID,
				Content:    content,
			})
		default:
			if len(item.Content) > 0 {
				role := item.Role
				if role == "" {
					role = "user"
				}
				messages = append(messages, ChatMessage{Role: role, Content: item.Content})
			}
		}
	}
	return messages, nil
}

func normalizeResponsesContentToChat(raw json.RawMessage) json.RawMessage {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		content, _ := json.Marshal(text)
		return content
	}

	var parts []ResponsesContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return raw
	}
	chatParts := make([]ChatContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_text", "output_text":
			chatParts = append(chatParts, ChatContentPart{Type: "text", Text: part.Text})
		case "input_image":
			chatParts = append(chatParts, ChatContentPart{
				Type: "image_url",
				ImageURL: &ChatImageURL{
					URL: part.ImageURL,
				},
			})
		}
	}
	content, _ := json.Marshal(chatParts)
	return content
}
