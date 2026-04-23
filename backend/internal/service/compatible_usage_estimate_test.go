//go:build unit

package service

import "testing"

func TestEstimateCompatibleInputTokens(t *testing.T) {
	parsed := &ParsedRequest{
		Body:      []byte(`{"model":"Kimi-K2.5","messages":[{"role":"user","content":"hello"}],"system":"reply briefly"}`),
		System:    "reply briefly",
		HasSystem: true,
		Messages: []any{
			map[string]any{
				"role":    "user",
				"content": "hello",
			},
		},
	}

	got := EstimateCompatibleInputTokens(parsed)
	if got <= 0 {
		t.Fatalf("EstimateCompatibleInputTokens() = %d, want > 0", got)
	}
}
