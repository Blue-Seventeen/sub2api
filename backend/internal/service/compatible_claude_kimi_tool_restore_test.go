package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubClaudeKimiToolRestoreCache struct {
	entries map[string]ClaudeKimiToolRestoreLedgerEntry
}

func newStubClaudeKimiToolRestoreCache() *stubClaudeKimiToolRestoreCache {
	return &stubClaudeKimiToolRestoreCache{
		entries: make(map[string]ClaudeKimiToolRestoreLedgerEntry),
	}
}

func (s *stubClaudeKimiToolRestoreCache) ledgerKey(groupID int64, sessionHash, callID string) string {
	return fmt.Sprintf("%d|%s|%s", groupID, strings.TrimSpace(sessionHash), strings.TrimSpace(callID))
}

func (s *stubClaudeKimiToolRestoreCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}

func (s *stubClaudeKimiToolRestoreCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (s *stubClaudeKimiToolRestoreCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *stubClaudeKimiToolRestoreCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (s *stubClaudeKimiToolRestoreCache) GetClaudeKimiToolRestoreEntry(_ context.Context, groupID int64, sessionHash, callID string) (*ClaudeKimiToolRestoreLedgerEntry, error) {
	entry, ok := s.entries[s.ledgerKey(groupID, sessionHash, callID)]
	if !ok {
		return nil, nil
	}
	copyEntry := entry
	return &copyEntry, nil
}

func (s *stubClaudeKimiToolRestoreCache) PutClaudeKimiToolRestoreEntry(_ context.Context, groupID int64, sessionHash string, entry ClaudeKimiToolRestoreLedgerEntry, _ time.Duration) error {
	s.entries[s.ledgerKey(groupID, sessionHash, entry.CallID)] = entry
	return nil
}

func newClaudeKimiToolRestoreTestHarness(t *testing.T, stream bool, restoreCtx ClaudeKimiToolRestoreContext) (*CompatibleGatewayService, *stubClaudeKimiToolRestoreCache, *gin.Context, *httptest.ResponseRecorder, *compatiblePreparedRequest) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	SetCompatibilityClientProfile(c, ClientProfileClaudeCode)
	SetCompatibilityInboundProtocol(c, InboundProtocolAnthropicMessages)
	SetCompatibilityRoute(c, CompatibilityRouteCompatibleEndpointRelay)

	restoreCtx.ClientProfile = ClientProfileClaudeCode
	restoreCtx.InboundProtocol = InboundProtocolAnthropicMessages
	restoreCtx = restoreCtx.Normalize()
	SetClaudeKimiToolRestoreContext(c, restoreCtx)

	cache := newStubClaudeKimiToolRestoreCache()
	svc := newCompatibleGatewayServiceForTest(nil)
	svc.gatewayService.cache = cache

	prepared := &compatiblePreparedRequest{
		ClientRoute:      CompatibleRouteMessages,
		ClientStream:     stream,
		UpstreamKind:     compatibleUpstreamMessages,
		UpstreamEndpoint: "/v1/messages",
	}
	return svc, cache, c, rec, prepared
}

func mustMarshalAnthropicResponse(t *testing.T, resp apicompat.AnthropicResponse) string {
	t.Helper()
	body, err := json.Marshal(resp)
	require.NoError(t, err)
	return string(body)
}

func mustAnthropicSSE(t *testing.T, events ...apicompat.AnthropicStreamEvent) string {
	t.Helper()
	var b strings.Builder
	for _, evt := range events {
		sse, err := apicompat.ResponsesAnthropicEventToSSE(evt)
		require.NoError(t, err)
		b.WriteString(sse)
	}
	return b.String()
}

func decodeAnthropicSSE(t *testing.T, raw string) []apicompat.AnthropicStreamEvent {
	t.Helper()
	blocks := strings.Split(raw, "\n\n")
	events := make([]apicompat.AnthropicStreamEvent, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		eventType, data, ok := parseAnthropicSSEEventBlock(strings.Split(block, "\n"))
		require.True(t, ok, "failed to parse SSE block: %s", block)

		var evt apicompat.AnthropicStreamEvent
		require.NoError(t, json.Unmarshal([]byte(data), &evt))
		if evt.Type == "" {
			evt.Type = eventType
		}
		events = append(events, evt)
	}
	return events
}

func TestHandleMessagesResponse_RestoresClaudeKimiCollapsedToolUse_NonStreaming(t *testing.T) {
	restoreCtx := ClaudeKimiToolRestoreContext{
		Enabled:     true,
		GroupID:     7,
		SessionHash: "session-a",
		AccountID:   88,
		Platform:    PlatformMoonshot,
		ToolNames:   []string{"Bash"},
	}
	svc, cache, c, rec, prepared := newClaudeKimiToolRestoreTestHarness(t, false, restoreCtx)

	body := mustMarshalAnthropicResponse(t, apicompat.AnthropicResponse{
		ID:    "msg_kimi_1",
		Type:  "message",
		Role:  "assistant",
		Model: "Kimi-K2.5",
		Content: []apicompat.AnthropicContentBlock{{
			Type: "text",
			Text: `我先看看目录。Previous assistant tool call: id=fc_tool_123; name=Bash; arguments={"command":"du -sh 百度网盘"}`,
		}},
		StopReason: "end_turn",
	})

	result := svc.handleMessagesResponse(newCompatibleGatewayHTTPResponse(http.StatusOK, body), c, prepared, time.Now())
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)

	var repaired apicompat.AnthropicResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &repaired))
	require.Len(t, repaired.Content, 2)
	require.Equal(t, "text", repaired.Content[0].Type)
	require.Equal(t, "我先看看目录。", repaired.Content[0].Text)
	require.Equal(t, "tool_use", repaired.Content[1].Type)
	require.Equal(t, "fc_tool_123", repaired.Content[1].ID)
	require.Equal(t, "Bash", repaired.Content[1].Name)
	require.JSONEq(t, `{"command":"du -sh 百度网盘"}`, string(repaired.Content[1].Input))
	require.Equal(t, "tool_use", repaired.StopReason)
	require.NotContains(t, rec.Body.String(), claudeKimiCollapsedToolUseMarker)

	entry, err := cache.GetClaudeKimiToolRestoreEntry(context.Background(), restoreCtx.GroupID, restoreCtx.SessionHash, "fc_tool_123")
	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, "Bash", entry.ToolName)
}

func TestHandleMessagesResponse_DoesNotRestoreDuplicateCollapsedToolUse_NonStreaming(t *testing.T) {
	restoreCtx := ClaudeKimiToolRestoreContext{
		Enabled:     true,
		GroupID:     9,
		SessionHash: "session-b",
		AccountID:   99,
		Platform:    PlatformMoonshot,
		ToolNames:   []string{"Bash"},
	}
	svc, cache, c, rec, prepared := newClaudeKimiToolRestoreTestHarness(t, false, restoreCtx)
	require.NoError(t, cache.PutClaudeKimiToolRestoreEntry(context.Background(), restoreCtx.GroupID, restoreCtx.SessionHash, ClaudeKimiToolRestoreLedgerEntry{
		CallID:        "fc_tool_dup",
		ToolName:      "Bash",
		ArgumentsJSON: `{"command":"du -sh 百度网盘"}`,
		AccountID:     restoreCtx.AccountID,
		CreatedAt:     time.Now(),
	}, CompatClaudeKimiToolRestoreTTL()))

	body := mustMarshalAnthropicResponse(t, apicompat.AnthropicResponse{
		ID:    "msg_kimi_dup",
		Type:  "message",
		Role:  "assistant",
		Model: "Kimi-K2.5",
		Content: []apicompat.AnthropicContentBlock{{
			Type: "text",
			Text: `Previous assistant tool call: id=fc_tool_dup; name=Bash; arguments={"command":"du -sh 百度网盘"}`,
		}},
		StopReason: "end_turn",
	})

	result := svc.handleMessagesResponse(newCompatibleGatewayHTTPResponse(http.StatusOK, body), c, prepared, time.Now())
	require.NotNil(t, result)

	var passthrough apicompat.AnthropicResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &passthrough))
	require.Len(t, passthrough.Content, 1)
	require.Equal(t, "text", passthrough.Content[0].Type)
	require.Contains(t, passthrough.Content[0].Text, claudeKimiCollapsedToolUseMarker)
	require.Equal(t, "end_turn", passthrough.StopReason)
}

func TestHandleMessagesResponse_DoesNotRestoreWhenContextDisabledOrPlatformMismatch(t *testing.T) {
	tests := []struct {
		name       string
		restoreCtx ClaudeKimiToolRestoreContext
	}{
		{
			name: "disabled",
			restoreCtx: ClaudeKimiToolRestoreContext{
				Enabled:     false,
				GroupID:     1,
				SessionHash: "disabled",
				AccountID:   2,
				Platform:    PlatformMoonshot,
				ToolNames:   []string{"Bash"},
			},
		},
		{
			name: "platform_mismatch",
			restoreCtx: ClaudeKimiToolRestoreContext{
				Enabled:     true,
				GroupID:     1,
				SessionHash: "glm",
				AccountID:   2,
				Platform:    PlatformZhipu,
				ToolNames:   []string{"Bash"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, c, rec, prepared := newClaudeKimiToolRestoreTestHarness(t, false, tt.restoreCtx)
			body := mustMarshalAnthropicResponse(t, apicompat.AnthropicResponse{
				ID:    "msg_passthrough",
				Type:  "message",
				Role:  "assistant",
				Model: "Kimi-K2.5",
				Content: []apicompat.AnthropicContentBlock{{
					Type: "text",
					Text: `Previous assistant tool call: id=fc_tool_0; name=Bash; arguments={"command":"du -sh 百度网盘"}`,
				}},
				StopReason: "end_turn",
			})

			result := svc.handleMessagesResponse(newCompatibleGatewayHTTPResponse(http.StatusOK, body), c, prepared, time.Now())
			require.NotNil(t, result)
			require.Contains(t, rec.Body.String(), claudeKimiCollapsedToolUseMarker)
			require.NotContains(t, rec.Body.String(), `"type":"tool_use"`)
		})
	}
}

func TestRestoreClaudeKimiCollapsedToolUse_StrictValidation(t *testing.T) {
	svc := newCompatibleGatewayServiceForTest(nil)
	ledger := newStubClaudeKimiToolRestoreCache()
	restoreCtx := ClaudeKimiToolRestoreContext{
		Enabled:         true,
		GroupID:         21,
		SessionHash:     "session-validate",
		AccountID:       5,
		Platform:        PlatformMoonshot,
		ClientProfile:   ClientProfileClaudeCode,
		InboundProtocol: InboundProtocolAnthropicMessages,
		ToolNames:       []string{"Bash"},
	}.Normalize()

	tests := []struct {
		name   string
		text   string
		reason string
	}{
		{
			name:   "discussion_only",
			text:   `这里是在讨论 Previous assistant tool call: 这个短语，并不是完整模板。`,
			reason: "invalid_id_prefix",
		},
		{
			name:   "tool_name_not_in_request",
			text:   `Previous assistant tool call: id=fc_tool_x; name=Python; arguments={"code":"print(1)"}`,
			reason: "tool_name_not_in_request",
		},
		{
			name:   "arguments_invalid_json",
			text:   `Previous assistant tool call: id=fc_tool_bad; name=Bash; arguments={bad-json}`,
			reason: "invalid_arguments_json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks, restored, reason, callID, duplicateSuppressed := svc.restoreClaudeKimiCollapsedToolUse(context.Background(), restoreCtx, ledger, tt.text)
			require.False(t, restored)
			require.Nil(t, blocks)
			require.Equal(t, tt.reason, reason)
			require.False(t, duplicateSuppressed)
			if tt.name == "tool_name_not_in_request" {
				require.Equal(t, "fc_tool_x", callID)
			}
		})
	}
}

func TestHandleMessagesResponse_RestoresClaudeKimiCollapsedToolUse_Streaming(t *testing.T) {
	restoreCtx := ClaudeKimiToolRestoreContext{
		Enabled:     true,
		GroupID:     13,
		SessionHash: "session-stream",
		AccountID:   188,
		Platform:    PlatformMoonshot,
		ToolNames:   []string{"Bash"},
	}
	svc, cache, c, rec, prepared := newClaudeKimiToolRestoreTestHarness(t, true, restoreCtx)

	streamBody := mustAnthropicSSE(t,
		apicompat.AnthropicStreamEvent{
			Type: "message_start",
			Message: &apicompat.AnthropicResponse{
				ID:    "msg_stream_1",
				Type:  "message",
				Role:  "assistant",
				Model: "Kimi-K2.5",
			},
		},
		apicompat.AnthropicStreamEvent{
			Type:  "content_block_start",
			Index: intPtr(0),
			ContentBlock: &apicompat.AnthropicContentBlock{
				Type: "text",
				Text: "",
			},
		},
		apicompat.AnthropicStreamEvent{
			Type:  "content_block_delta",
			Index: intPtr(0),
			Delta: &apicompat.AnthropicDelta{
				Type: "text_delta",
				Text: `我先看看目录。Previous assistant tool call: id=fc_tool_stream; name=Bash; arguments={"command":"du -sh 百度网盘"}`,
			},
		},
		apicompat.AnthropicStreamEvent{
			Type:  "content_block_stop",
			Index: intPtr(0),
		},
		apicompat.AnthropicStreamEvent{
			Type: "message_delta",
			Delta: &apicompat.AnthropicDelta{
				StopReason: "end_turn",
			},
		},
		apicompat.AnthropicStreamEvent{
			Type: "message_stop",
		},
	)

	result := svc.handleMessagesResponse(newCompatibleGatewayEventStreamResponse(http.StatusOK, streamBody), c, prepared, time.Now())
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), claudeKimiCollapsedToolUseMarker)

	events := decodeAnthropicSSE(t, rec.Body.String())
	require.Len(t, events, 9)
	require.Equal(t, "message_start", events[0].Type)
	require.Equal(t, "content_block_start", events[1].Type)
	require.Equal(t, "text", events[1].ContentBlock.Type)
	require.Equal(t, "content_block_delta", events[2].Type)
	require.Equal(t, "我先看看目录。", events[2].Delta.Text)
	require.Equal(t, "content_block_stop", events[3].Type)
	require.Equal(t, "content_block_start", events[4].Type)
	require.Equal(t, "tool_use", events[4].ContentBlock.Type)
	require.Equal(t, "fc_tool_stream", events[4].ContentBlock.ID)
	require.Equal(t, "Bash", events[4].ContentBlock.Name)
	require.Equal(t, "content_block_delta", events[5].Type)
	require.Equal(t, "input_json_delta", events[5].Delta.Type)
	require.JSONEq(t, `{"command":"du -sh 百度网盘"}`, events[5].Delta.PartialJSON)
	require.Equal(t, "content_block_stop", events[6].Type)
	require.Equal(t, "message_delta", events[7].Type)
	require.Equal(t, "tool_use", events[7].Delta.StopReason)
	require.Equal(t, "message_stop", events[8].Type)

	entry, err := cache.GetClaudeKimiToolRestoreEntry(context.Background(), restoreCtx.GroupID, restoreCtx.SessionHash, "fc_tool_stream")
	require.NoError(t, err)
	require.NotNil(t, entry)
}
