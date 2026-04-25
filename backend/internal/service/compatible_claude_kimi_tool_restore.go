package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

const claudeKimiCollapsedToolUseMarker = "Previous assistant tool call: "

type claudeKimiCollapsedToolUse struct {
	Prefix        string
	CallID        string
	ToolName      string
	ArgumentsJSON string
}

type claudeKimiToolRestoreStreamBuffer struct {
	Index int
	Text  string
}

type claudeKimiToolRestoreStreamState struct {
	IndexShift          int
	OpenBlockTypes      map[int]string
	LastClosedBlockType string
	TextBuffer          *claudeKimiToolRestoreStreamBuffer
}

func newClaudeKimiToolRestoreStreamState() *claudeKimiToolRestoreStreamState {
	return &claudeKimiToolRestoreStreamState{
		OpenBlockTypes: make(map[int]string),
	}
}

func (s *CompatibleGatewayService) shouldRepairClaudeKimiToolUse(c *gin.Context, prepared *compatiblePreparedRequest) (ClaudeKimiToolRestoreContext, ClaudeKimiToolRestoreLedger, bool, string) {
	restoreCtx := GetClaudeKimiToolRestoreContext(c).Normalize()
	if !restoreCtx.Enabled {
		return restoreCtx, nil, false, "restore_disabled"
	}
	if restoreCtx.ClientProfile != ClientProfileClaudeCode {
		return restoreCtx, nil, false, "client_profile_mismatch"
	}
	if restoreCtx.InboundProtocol != InboundProtocolAnthropicMessages {
		return restoreCtx, nil, false, "inbound_protocol_mismatch"
	}
	if restoreCtx.Platform != PlatformMoonshot {
		return restoreCtx, nil, false, "platform_mismatch"
	}
	if len(restoreCtx.ToolNames) == 0 {
		return restoreCtx, nil, false, "missing_tools"
	}
	if prepared == nil || prepared.ClientRoute != CompatibleRouteMessages {
		return restoreCtx, nil, false, "route_mismatch"
	}
	if GetCompatibilityRoute(c) != CompatibilityRouteCompatibleEndpointRelay {
		return restoreCtx, nil, false, "compatibility_route_mismatch"
	}
	if s == nil || s.gatewayService == nil || s.gatewayService.cache == nil {
		return restoreCtx, nil, false, "ledger_unavailable"
	}
	ledger, ok := any(s.gatewayService.cache).(ClaudeKimiToolRestoreLedger)
	if !ok || ledger == nil {
		return restoreCtx, nil, false, "ledger_unavailable"
	}
	return restoreCtx, ledger, true, ""
}

func parseClaudeKimiCollapsedToolUse(text string) (claudeKimiCollapsedToolUse, bool, string) {
	idx := strings.LastIndex(text, claudeKimiCollapsedToolUseMarker)
	if idx < 0 {
		return claudeKimiCollapsedToolUse{}, false, "marker_missing"
	}
	prefix := text[:idx]
	rest := text[idx+len(claudeKimiCollapsedToolUseMarker):]
	if !strings.HasPrefix(rest, "id=") {
		return claudeKimiCollapsedToolUse{}, false, "invalid_id_prefix"
	}
	nameSep := strings.Index(rest, "; name=")
	if nameSep <= len("id=") {
		return claudeKimiCollapsedToolUse{}, false, "missing_name_separator"
	}
	argsSep := strings.Index(rest, "; arguments=")
	if argsSep <= nameSep+len("; name=") {
		return claudeKimiCollapsedToolUse{}, false, "missing_arguments_separator"
	}

	callID := strings.TrimSpace(rest[len("id="):nameSep])
	toolName := strings.TrimSpace(rest[nameSep+len("; name="):argsSep])
	argumentsJSON := strings.TrimSpace(rest[argsSep+len("; arguments="):])
	if callID == "" {
		return claudeKimiCollapsedToolUse{}, false, "empty_call_id"
	}
	if toolName == "" {
		return claudeKimiCollapsedToolUse{}, false, "empty_tool_name"
	}
	if argumentsJSON == "" {
		return claudeKimiCollapsedToolUse{}, false, "empty_arguments"
	}
	if !json.Valid([]byte(argumentsJSON)) {
		return claudeKimiCollapsedToolUse{}, false, "invalid_arguments_json"
	}

	return claudeKimiCollapsedToolUse{
		Prefix:        prefix,
		CallID:        callID,
		ToolName:      toolName,
		ArgumentsJSON: argumentsJSON,
	}, true, ""
}

func (s *CompatibleGatewayService) restoreClaudeKimiCollapsedToolUse(
	ctx context.Context,
	restoreCtx ClaudeKimiToolRestoreContext,
	ledger ClaudeKimiToolRestoreLedger,
	text string,
) ([]apicompat.AnthropicContentBlock, bool, string, string, bool) {
	candidate, ok, reason := parseClaudeKimiCollapsedToolUse(text)
	if !ok {
		return nil, false, reason, "", false
	}
	if !restoreCtx.HasToolName(candidate.ToolName) {
		return nil, false, "tool_name_not_in_request", candidate.CallID, false
	}
	if ledger == nil {
		return nil, false, "ledger_unavailable", candidate.CallID, false
	}
	if existing, err := ledger.GetClaudeKimiToolRestoreEntry(ctx, restoreCtx.GroupID, restoreCtx.SessionHash, candidate.CallID); err != nil {
		return nil, false, "ledger_read_failed", candidate.CallID, false
	} else if existing != nil {
		return nil, false, "duplicate_call_id", candidate.CallID, true
	}

	blocks := make([]apicompat.AnthropicContentBlock, 0, 2)
	if strings.TrimSpace(candidate.Prefix) != "" {
		blocks = append(blocks, apicompat.AnthropicContentBlock{
			Type: "text",
			Text: candidate.Prefix,
		})
	}
	blocks = append(blocks, apicompat.AnthropicContentBlock{
		Type:  "tool_use",
		ID:    candidate.CallID,
		Name:  candidate.ToolName,
		Input: json.RawMessage(candidate.ArgumentsJSON),
	})

	if err := ledger.PutClaudeKimiToolRestoreEntry(ctx, restoreCtx.GroupID, restoreCtx.SessionHash, ClaudeKimiToolRestoreLedgerEntry{
		CallID:        candidate.CallID,
		ToolName:      candidate.ToolName,
		ArgumentsJSON: candidate.ArgumentsJSON,
		AccountID:     restoreCtx.AccountID,
		CreatedAt:     time.Now(),
	}, CompatClaudeKimiToolRestoreTTL()); err != nil {
		return nil, false, "ledger_write_failed", candidate.CallID, false
	}

	return blocks, true, "restored", candidate.CallID, false
}

func logClaudeKimiToolRestore(restoreCtx ClaudeKimiToolRestoreContext, restored bool, reason, callID string, duplicateSuppressed bool) {
	slog.Debug(
		"compat_repair_claude_kimi_tool_restore",
		"compat_repair_mode", "claude_kimi_tool_restore",
		"compat_repair_restored", restored,
		"compat_repair_reason", strings.TrimSpace(reason),
		"compat_repair_call_id", strings.TrimSpace(callID),
		"compat_repair_duplicate_suppressed", duplicateSuppressed,
		"client_profile", string(restoreCtx.ClientProfile),
		"platform", restoreCtx.Platform,
		"account_id", restoreCtx.AccountID,
		"group_id", restoreCtx.GroupID,
	)
}

func (s *CompatibleGatewayService) repairClaudeKimiNonStreamingMessagesBody(
	c *gin.Context,
	body []byte,
	restoreCtx ClaudeKimiToolRestoreContext,
	ledger ClaudeKimiToolRestoreLedger,
) []byte {
	var anthropicResp apicompat.AnthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		logClaudeKimiToolRestore(restoreCtx, false, "invalid_anthropic_json", "", false)
		return body
	}

	changed := false
	newContent := make([]apicompat.AnthropicContentBlock, 0, len(anthropicResp.Content)+1)
	lastBlockType := ""

	for _, block := range anthropicResp.Content {
		if block.Type != "text" || strings.TrimSpace(block.Text) == "" {
			newContent = append(newContent, block)
			if block.Type != "" {
				lastBlockType = block.Type
			}
			continue
		}

		restoredBlocks, restored, reason, callID, duplicateSuppressed := s.restoreClaudeKimiCollapsedToolUse(c.Request.Context(), restoreCtx, ledger, block.Text)
		if !restored {
			if reason != "marker_missing" {
				logClaudeKimiToolRestore(restoreCtx, false, reason, callID, duplicateSuppressed)
			}
			newContent = append(newContent, block)
			lastBlockType = block.Type
			continue
		}

		logClaudeKimiToolRestore(restoreCtx, true, reason, callID, false)
		changed = true
		for _, restoredBlock := range restoredBlocks {
			newContent = append(newContent, restoredBlock)
			if restoredBlock.Type != "" {
				lastBlockType = restoredBlock.Type
			}
		}
	}

	if !changed {
		return body
	}

	anthropicResp.Content = newContent
	if lastBlockType == "tool_use" {
		switch strings.TrimSpace(anthropicResp.StopReason) {
		case "", "end_turn":
			anthropicResp.StopReason = "tool_use"
		}
	}
	updated, err := json.Marshal(&anthropicResp)
	if err != nil {
		logClaudeKimiToolRestore(restoreCtx, false, "marshal_repaired_response_failed", "", false)
		return body
	}
	return updated
}

func (s *CompatibleGatewayService) repairClaudeKimiStreamingMessagesResponse(
	resp *http.Response,
	c *gin.Context,
	prepared *compatiblePreparedRequest,
	startTime time.Time,
	restoreCtx ClaudeKimiToolRestoreContext,
	ledger ClaudeKimiToolRestoreLedger,
) *ForwardResult {
	c.Status(resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)

	streamState := newClaudeKimiToolRestoreStreamState()
	usage := ClaudeUsage{}
	var firstTokenMs *int
	var rawEventLines []string

	flushRawEvent := func(lines []string) {
		if len(lines) == 0 {
			return
		}
		var eventBuf bytes.Buffer
		for _, line := range lines {
			appendCompatibleSSELine(&eventBuf, line)
		}
		appendCompatibleSSELine(&eventBuf, "")
		flushCompatibleSSEBuffer(c, &eventBuf)
	}

	for scanner.Scan() {
		line := scanner.Text()
		rawEventLines = append(rawEventLines, line)
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			if payload != "[DONE]" {
				markCompatibleFirstToken(startTime, &firstTokenMs, payload)
				s.gatewayService.parseSSEUsage(payload, &usage)
			}
		}
		if line != "" {
			continue
		}

		eventType, payload, ok := parseAnthropicSSEEventBlock(rawEventLines)
		if !ok {
			flushRawEvent(rawEventLines)
			rawEventLines = rawEventLines[:0]
			continue
		}
		if payload == "[DONE]" {
			flushRawEvent(rawEventLines)
			rawEventLines = rawEventLines[:0]
			continue
		}

		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			flushRawEvent(rawEventLines)
			rawEventLines = rawEventLines[:0]
			continue
		}
		if event.Type == "" {
			event.Type = eventType
		}

		rewritten, err := s.rewriteClaudeKimiStreamingAnthropicEvent(c.Request.Context(), &event, streamState, restoreCtx, ledger)
		if err != nil {
			flushRawEvent(rawEventLines)
			rawEventLines = rawEventLines[:0]
			continue
		}

		var outBuf bytes.Buffer
		for _, item := range rewritten {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(item)
			if err != nil {
				continue
			}
			outBuf.WriteString(sse)
		}
		flushCompatibleSSEBuffer(c, &outBuf)
		rawEventLines = rawEventLines[:0]
	}

	if len(rawEventLines) > 0 {
		flushRawEvent(rawEventLines)
	}
	return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
}

func (s *CompatibleGatewayService) rewriteClaudeKimiStreamingAnthropicEvent(
	ctx context.Context,
	event *apicompat.AnthropicStreamEvent,
	streamState *claudeKimiToolRestoreStreamState,
	restoreCtx ClaudeKimiToolRestoreContext,
	ledger ClaudeKimiToolRestoreLedger,
) ([]apicompat.AnthropicStreamEvent, error) {
	if event == nil || streamState == nil {
		return nil, nil
	}

	shifted := shiftAnthropicStreamEventIndex(event, streamState.IndexShift)

	switch shifted.Type {
	case "content_block_start":
		if shifted.Index != nil && shifted.ContentBlock != nil && shifted.ContentBlock.Type == "text" {
			streamState.TextBuffer = &claudeKimiToolRestoreStreamBuffer{Index: *shifted.Index}
			return nil, nil
		}
		if shifted.Index != nil && shifted.ContentBlock != nil {
			streamState.OpenBlockTypes[*shifted.Index] = shifted.ContentBlock.Type
		}
		return []apicompat.AnthropicStreamEvent{shifted}, nil

	case "content_block_delta":
		if streamState.TextBuffer != nil && shifted.Index != nil && *shifted.Index == streamState.TextBuffer.Index {
			if shifted.Delta != nil && shifted.Delta.Type == "text_delta" {
				streamState.TextBuffer.Text += shifted.Delta.Text
				return nil, nil
			}
		}
		return []apicompat.AnthropicStreamEvent{shifted}, nil

	case "content_block_stop":
		if streamState.TextBuffer != nil && shifted.Index != nil && *shifted.Index == streamState.TextBuffer.Index {
			out := s.finalizeClaudeKimiStreamingTextBuffer(ctx, streamState, restoreCtx, ledger)
			streamState.TextBuffer = nil
			return out, nil
		}
		if shifted.Index != nil {
			if blockType, ok := streamState.OpenBlockTypes[*shifted.Index]; ok {
				streamState.LastClosedBlockType = blockType
				delete(streamState.OpenBlockTypes, *shifted.Index)
			}
		}
		return []apicompat.AnthropicStreamEvent{shifted}, nil

	case "message_delta":
		if shifted.Delta != nil && streamState.LastClosedBlockType == "tool_use" {
			switch strings.TrimSpace(shifted.Delta.StopReason) {
			case "", "end_turn":
				shifted.Delta.StopReason = "tool_use"
			}
		}
		return []apicompat.AnthropicStreamEvent{shifted}, nil

	default:
		return []apicompat.AnthropicStreamEvent{shifted}, nil
	}
}

func (s *CompatibleGatewayService) finalizeClaudeKimiStreamingTextBuffer(
	ctx context.Context,
	streamState *claudeKimiToolRestoreStreamState,
	restoreCtx ClaudeKimiToolRestoreContext,
	ledger ClaudeKimiToolRestoreLedger,
) []apicompat.AnthropicStreamEvent {
	if streamState == nil || streamState.TextBuffer == nil {
		return nil
	}

	index := streamState.TextBuffer.Index
	text := streamState.TextBuffer.Text
	restoredBlocks, restored, reason, callID, duplicateSuppressed := s.restoreClaudeKimiCollapsedToolUse(ctx, restoreCtx, ledger, text)
	if !restored {
		if reason != "marker_missing" {
			logClaudeKimiToolRestore(restoreCtx, false, reason, callID, duplicateSuppressed)
		}
		streamState.LastClosedBlockType = "text"
		return buildAnthropicStreamingTextBlock(index, text)
	}

	logClaudeKimiToolRestore(restoreCtx, true, reason, callID, false)

	events := make([]apicompat.AnthropicStreamEvent, 0, 6)
	nextIndex := index
	insertedExtraBlock := false
	for _, block := range restoredBlocks {
		switch block.Type {
		case "text":
			events = append(events, buildAnthropicStreamingTextBlock(nextIndex, block.Text)...)
			streamState.LastClosedBlockType = "text"
			nextIndex++
			insertedExtraBlock = true
		case "tool_use":
			events = append(events, buildAnthropicStreamingToolUseBlock(nextIndex, block)...)
			streamState.LastClosedBlockType = "tool_use"
		default:
			events = append(events, apicompat.AnthropicStreamEvent{
				Type:         "content_block_start",
				Index:        intPtr(nextIndex),
				ContentBlock: &block,
			}, apicompat.AnthropicStreamEvent{
				Type:  "content_block_stop",
				Index: intPtr(nextIndex),
			})
			streamState.LastClosedBlockType = block.Type
		}
	}
	if insertedExtraBlock {
		streamState.IndexShift++
	}
	return events
}

func buildAnthropicStreamingTextBlock(index int, text string) []apicompat.AnthropicStreamEvent {
	events := []apicompat.AnthropicStreamEvent{{
		Type:  "content_block_start",
		Index: intPtr(index),
		ContentBlock: &apicompat.AnthropicContentBlock{
			Type: "text",
			Text: "",
		},
	}}
	if text != "" {
		events = append(events, apicompat.AnthropicStreamEvent{
			Type:  "content_block_delta",
			Index: intPtr(index),
			Delta: &apicompat.AnthropicDelta{
				Type: "text_delta",
				Text: text,
			},
		})
	}
	events = append(events, apicompat.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: intPtr(index),
	})
	return events
}

func buildAnthropicStreamingToolUseBlock(index int, block apicompat.AnthropicContentBlock) []apicompat.AnthropicStreamEvent {
	events := []apicompat.AnthropicStreamEvent{{
		Type:  "content_block_start",
		Index: intPtr(index),
		ContentBlock: &apicompat.AnthropicContentBlock{
			Type:  "tool_use",
			ID:    block.ID,
			Name:  block.Name,
			Input: json.RawMessage("{}"),
		},
	}}
	if strings.TrimSpace(string(block.Input)) != "" {
		events = append(events, apicompat.AnthropicStreamEvent{
			Type:  "content_block_delta",
			Index: intPtr(index),
			Delta: &apicompat.AnthropicDelta{
				Type:        "input_json_delta",
				PartialJSON: string(block.Input),
			},
		})
	}
	events = append(events, apicompat.AnthropicStreamEvent{
		Type:  "content_block_stop",
		Index: intPtr(index),
	})
	return events
}

func parseAnthropicSSEEventBlock(lines []string) (eventType, data string, ok bool) {
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
		case strings.HasPrefix(line, "data: "):
			if data != "" {
				data += "\n"
			}
			data += strings.TrimPrefix(line, "data: ")
		}
	}
	if eventType == "" || data == "" {
		return "", "", false
	}
	return eventType, data, true
}

func shiftAnthropicStreamEventIndex(event *apicompat.AnthropicStreamEvent, shift int) apicompat.AnthropicStreamEvent {
	if event == nil {
		return apicompat.AnthropicStreamEvent{}
	}
	shifted := *event
	if shift == 0 || event.Index == nil {
		return shifted
	}
	index := *event.Index + shift
	shifted.Index = &index
	return shifted
}

func (s *CompatibleGatewayService) maybeRepairClaudeKimiMessagesResponse(
	resp *http.Response,
	c *gin.Context,
	prepared *compatiblePreparedRequest,
	startTime time.Time,
) (handled bool, result *ForwardResult) {
	restoreCtx, ledger, ok, reason := s.shouldRepairClaudeKimiToolUse(c, prepared)
	if !ok {
		if reason != "" && reason != "restore_disabled" {
			logClaudeKimiToolRestore(restoreCtx, false, reason, "", false)
		}
		return false, nil
	}

	if prepared.ClientStream {
		return true, s.repairClaudeKimiStreamingMessagesResponse(resp, c, prepared, startTime, restoreCtx, ledger)
	}

	body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	body = s.repairClaudeKimiNonStreamingMessagesBody(c, body, restoreCtx, ledger)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	usage := ClaudeUsage{}
	if parsed := parseClaudeUsageFromResponseBody(body); parsed != nil {
		usage = *parsed
	}
	return true, buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil)
}
