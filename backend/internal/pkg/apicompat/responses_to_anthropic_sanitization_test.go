package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesToAnthropicRequest_SkipsBlankAndInvalidContent(t *testing.T) {
	_, messages, err := convertResponsesInputToAnthropic("", json.RawMessage(`[
		{"role":"user","content":"   "},
		{"role":"assistant","content":"   "},
		{"role":"critic","content":[{"type":"bogus","text":"discard"}]},
		{"role":"assistant","content":[{"type":"bogus","text":"discard"}]},
		{"role":"user","content":[{"type":"input_text","text":"hello"}]},
		{"role":"assistant","content":"reply"}
	]`))
	require.NoError(t, err)
	require.Len(t, messages, 2)

	require.Equal(t, "user", messages[0].Role)
	userBlocks := parseContentBlocks(messages[0].Content)
	require.Len(t, userBlocks, 1)
	assert.Equal(t, "text", userBlocks[0].Type)
	assert.Equal(t, "hello", userBlocks[0].Text)

	require.Equal(t, "assistant", messages[1].Role)
	assistantBlocks := parseContentBlocks(messages[1].Content)
	require.Len(t, assistantBlocks, 1)
	assert.Equal(t, "text", assistantBlocks[0].Type)
	assert.Equal(t, "reply", assistantBlocks[0].Text)
}

func TestNormalizeAnthropicToolPairing_DropsBlankOnlyMessages(t *testing.T) {
	msgs := []AnthropicMessage{
		{Role: "assistant", Content: json.RawMessage(`""`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":" "}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"call_A","name":"exec","input":{}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call_A","content":"ok"},{"type":"text","text":" "}]`)},
	}

	got := normalizeAnthropicToolPairing(msgs)
	require.Len(t, got, 2)

	require.Equal(t, "assistant", got[0].Role)
	require.True(t, hasToolUse(parseContentBlocks(got[0].Content), "call_A"))

	require.Equal(t, "user", got[1].Role)
	blocks := parseContentBlocks(got[1].Content)
	require.Len(t, blocks, 1)
	require.True(t, hasToolResult(blocks, "call_A"))
}

func TestAnthropicContentBlankHelpers(t *testing.T) {
	assert.True(t, anthropicContentIsEmpty(json.RawMessage(`""`)))
	assert.True(t, anthropicContentIsOnlyBlankText(json.RawMessage(`[{"type":"text","text":" "}]`)))
	assert.False(t, anthropicContentIsOnlyBlankText(json.RawMessage(`[{"type":"tool_use","id":"call_A","name":"exec","input":{}}]`)))
}
