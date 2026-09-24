package service

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClientVisibleStreamRedactionPreservesSuccessfulNullErrors(t *testing.T) {
	for _, eventType := range []string{"response.created", "response.in_progress", "response.completed", "response.done"} {
		for _, fields := range []struct {
			name     string
			topLevel string
			nested   string
		}{
			{name: "absent"},
			{name: "top_level_null", topLevel: `,"error":null`},
			{name: "response_null", nested: `,"error":null`},
			{name: "both_null", topLevel: `,"error":null`, nested: `,"error":null`},
		} {
			t.Run(eventType+"/"+fields.name, func(t *testing.T) {
				payload := []byte(fmt.Sprintf(`{"type":%q,"sequence_number":9007199254740993%s,"response":{"id":"resp_12345678-1234-1234-1234-123456789012","status":"completed"%s,"output":[{"type":"message","content":[{"type":"output_text","text":"Visit example.com or 10.0.0.1"}]}],"usage":{"input_tokens":108,"output_tokens":77}}}`, eventType, fields.topLevel, fields.nested))
				t.Run("websocket", func(t *testing.T) {
					require.Equal(t, string(payload), string(sanitizeClientVisibleOpenAIWSEvent(payload)))
				})
				for _, lineEnding := range []string{"\n", "\r\n"} {
					t.Run(fmt.Sprintf("sse_%q", lineEnding), func(t *testing.T) {
						block := []byte("event: " + eventType + lineEnding + "data: " + string(payload) + lineEnding + lineEnding)
						require.Equal(t, string(block), string(sanitizeClientVisibleSSEEventBlock(block)))
					})
				}
			})
		}
	}
}

func TestClientVisibleStreamRedactionStillRedactsActualErrors(t *testing.T) {
	for _, payload := range []string{
		`{"type":"error","error":{"type":"api.internal.example","message":"https://upstream.example 10.0.0.1 Bearer secret-123456789","token":"private-token"}}`,
		`{"type":"response.failed","response":{"id":"resp_failed","error":{"message":"https://upstream.example 10.0.0.1 Bearer secret-123456789","token":"private-token"}}}`,
		`{"type":"response.failed","response":{"error":null},"message":"https://upstream.example 10.0.0.1 Bearer secret-123456789","token":"private-token"}`,
		`{"error":"https://upstream.example 10.0.0.1 Bearer secret-123456789","token":"private-token"}`,
		`{"response":{"error":"https://upstream.example 10.0.0.1 Bearer secret-123456789"},"token":"private-token"}`,
	} {
		t.Run(payload, func(t *testing.T) {
			ws := sanitizeClientVisibleOpenAIWSEvent([]byte(payload))
			require.True(t, gjson.ValidBytes(ws))
			require.Equal(t, gjson.Get(payload, "type").String(), gjson.GetBytes(ws, "type").String())
			sse := sanitizeClientVisibleSSEEventBlock([]byte("data: " + payload + "\n\n"))
			for _, redacted := range [][]byte{ws, sse} {
				for _, secret := range []string{"api.internal.example", "upstream.example", "10.0.0.1", "secret-123456789", "private-token"} {
					require.NotContains(t, string(redacted), secret)
				}
			}
		})
	}
}
