package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatCompatStream_IncompletePreservesUsage(t *testing.T) {
	const partial = `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":2,"candidatesTokenCount":4,"thoughtsTokenCount":2}}`
	for _, readErr := range []error{nil, errors.New("upstream read failed")} {
		name := "eof"
		if readErr != nil {
			name = "read error"
		}
		t.Run(name, func(t *testing.T) {
			reader := io.Reader(strings.NewReader(partial + "\n\n"))
			if readErr != nil {
				reader = io.MultiReader(reader, iotest.ErrReader(readErr))
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(reader)}
			result, err := (&GeminiMessagesCompatService{}).handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-test", false, true)

			require.Error(t, err)
			if readErr == nil {
				require.EqualError(t, err, "stream usage incomplete: missing terminal event")
			} else {
				require.ErrorIs(t, err, readErr)
			}
			require.NotNil(t, result)
			require.Equal(t, 8, result.usage.InputTokens)
			require.Equal(t, 6, result.usage.OutputTokens)
			require.Equal(t, 2, result.usage.CacheReadInputTokens)
			require.Contains(t, rec.Body.String(), "partial")
			require.NotContains(t, rec.Body.String(), "data: [DONE]")
			require.NotContains(t, rec.Body.String(), `"finish_reason":"stop"`)
		})
	}
}

func TestGeminiChatCompatForward_IncompletePreservesUsage(t *testing.T) {
	httpStub := &geminiCompatHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Goog-Request-Id": []string{"gemini-partial"}},
		Body:       io.NopCloser(strings.NewReader(`data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}` + "\n\n")),
	}}
	svc := &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}
	account := &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
	body := []byte(`{"model":"gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body)

	require.EqualError(t, err, "stream usage incomplete: missing terminal event")
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, "gemini-partial", result.RequestID)
	require.Equal(t, "gemini-2.5-flash", result.Model)
	require.Equal(t, "gemini-2.5-flash", result.UpstreamModel)
	require.False(t, result.SkipUsageBilling)
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
}

func TestGeminiChatCompatStream_TerminalCompletes(t *testing.T) {
	for _, terminal := range []string{"STOP", "MAX_TOKENS", "SAFETY", "[DONE]"} {
		t.Run(terminal, func(t *testing.T) {
			body := `data: {"candidates":[{"content":{"parts":[{"text":"answer"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}` + "\n\n"
			if terminal == "[DONE]" {
				body += "data: [DONE]"
			} else {
				body += `data: {"candidates":[{"finishReason":"` + terminal + `"}]}`
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			result, err := (&GeminiMessagesCompatService{}).handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-test", false, true)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 4, result.usage.OutputTokens)
			require.Contains(t, rec.Body.String(), "data: [DONE]")
		})
	}
}

func TestGeminiChatCompatStream_PromptBlockedCompletes(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		name := "direct"
		if wrapped {
			name = "wrapped"
		}
		t.Run(name, func(t *testing.T) {
			payload := `{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":10}}`
			if wrapped {
				payload = `{"response":` + payload + `}`
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\n"))}
			result, err := (&GeminiMessagesCompatService{}).handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-test", wrapped, true)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 10, result.usage.InputTokens)
			require.Contains(t, rec.Body.String(), "data: [DONE]")
		})
	}
}

func TestGeminiResponseToChatCompletionsPreservesInlineData(t *testing.T) {
	tests := []struct {
		name  string
		parts []any
		want  string
	}{
		{
			name: "image only",
			parts: []any{
				map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "aW1hZ2U="}},
			},
			want: "![image](data:image/png;base64,aW1hZ2U=)",
		},
		{
			name: "text and image",
			parts: []any{
				map[string]any{"text": "rendered image:\n"},
				map[string]any{"inlineData": map[string]any{"mimeType": "image/webp", "data": "d2VicA=="}},
			},
			want: "rendered image:\n![image](data:image/webp;base64,d2VicA==)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			geminiResp := map[string]any{
				"candidates": []any{map[string]any{
					"content":      map[string]any{"parts": tt.parts},
					"finishReason": "STOP",
				}},
			}
			rawData, err := json.Marshal(geminiResp)
			require.NoError(t, err)

			got, _, err := geminiResponseToChatCompletions(geminiResp, "gemini-test", rawData, nil)
			require.NoError(t, err)
			require.Len(t, got.Choices, 1)

			var content string
			require.NoError(t, json.Unmarshal(got.Choices[0].Message.Content, &content))
			require.Equal(t, tt.want, content)
			require.Equal(t, "stop", got.Choices[0].FinishReason)
		})
	}
}

func TestGeminiResponseToChatCompletionsOmitsInvalidInlineData(t *testing.T) {
	tests := []struct {
		name       string
		inlineData map[string]any
	}{
		{
			name:       "unsupported MIME type",
			inlineData: map[string]any{"mimeType": "image/svg+xml", "data": "PHN2Zz48L3N2Zz4="},
		},
		{
			name:       "malformed MIME type",
			inlineData: map[string]any{"mimeType": "image/png; charset=utf-8", "data": "aW1hZ2U="},
		},
		{
			name:       "malformed base64",
			inlineData: map[string]any{"mimeType": "image/png", "data": "not-valid-base64!!!"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			geminiResp := map[string]any{
				"candidates": []any{map[string]any{
					"content":      map[string]any{"parts": []any{map[string]any{"text": "before"}, map[string]any{"inlineData": tt.inlineData}, map[string]any{"text": "after"}}},
					"finishReason": "STOP",
				}},
			}
			rawData, err := json.Marshal(geminiResp)
			require.NoError(t, err)

			got, _, err := geminiResponseToChatCompletions(geminiResp, "gemini-test", rawData, nil)
			require.NoError(t, err)

			var content string
			require.NoError(t, json.Unmarshal(got.Choices[0].Message.Content, &content))
			require.Equal(t, "beforeafter", content)
		})
	}
}

func TestConvertGeminiToClaudeMessageOmitsInlineDataForAnthropicMessages(t *testing.T) {
	geminiResp := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{
				map[string]any{"text": "before"},
				map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "aW1hZ2U="}},
				map[string]any{"functionCall": map[string]any{"name": "get_weather", "args": map[string]any{"city": "Paris"}}},
				map[string]any{"text": "after"},
			}},
			"finishReason": "STOP",
		}},
	}
	rawData, err := json.Marshal(geminiResp)
	require.NoError(t, err)

	withInlineData, _ := convertGeminiToClaudeMessage(geminiResp, "gemini-test", rawData, true)
	contentWithInlineData, ok := withInlineData["content"].([]any)
	require.True(t, ok)
	require.Len(t, contentWithInlineData, 4)
	require.Equal(t, map[string]any{"type": "text", "text": "before"}, contentWithInlineData[0])
	require.Equal(t, map[string]any{"type": "text", "text": "![image](data:image/png;base64,aW1hZ2U=)"}, contentWithInlineData[1])
	toolUse, ok := contentWithInlineData[2].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tool_use", toolUse["type"])
	require.Equal(t, "get_weather", toolUse["name"])
	require.Equal(t, map[string]any{"type": "text", "text": "after"}, contentWithInlineData[3])

	withoutInlineData, _ := convertGeminiToClaudeMessage(geminiResp, "gemini-test", rawData, false)
	contentWithoutInlineData, ok := withoutInlineData["content"].([]any)
	require.True(t, ok)
	require.Len(t, contentWithoutInlineData, 3)
	require.Equal(t, map[string]any{"type": "text", "text": "before"}, contentWithoutInlineData[0])
	toolUseWithoutInlineData, ok := contentWithoutInlineData[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tool_use", toolUseWithoutInlineData["type"])
	require.Equal(t, "get_weather", toolUseWithoutInlineData["name"])
	require.Equal(t, map[string]any{"type": "text", "text": "after"}, contentWithoutInlineData[2])
}

func TestGeminiResponseToChatCompletionsRetainsTextAndToolBehavior(t *testing.T) {
	geminiResp := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{
				map[string]any{"text": "checking"},
				map[string]any{"functionCall": map[string]any{
					"name": "get_weather",
					"args": map[string]any{"city": "Paris"},
				}},
			}},
			"finishReason": "STOP",
		}},
	}
	rawData, err := json.Marshal(geminiResp)
	require.NoError(t, err)

	got, _, err := geminiResponseToChatCompletions(geminiResp, "gemini-test", rawData, nil)
	require.NoError(t, err)
	require.Len(t, got.Choices, 1)

	choice := got.Choices[0]
	var content string
	require.NoError(t, json.Unmarshal(choice.Message.Content, &content))
	require.Equal(t, "checking", content)
	require.Equal(t, "tool_calls", choice.FinishReason)
	require.Len(t, choice.Message.ToolCalls, 1)
	require.Equal(t, "get_weather", choice.Message.ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"city":"Paris"}`, choice.Message.ToolCalls[0].Function.Arguments)
}
