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

func TestEstimateMoonshotCompatibleInputTokens_UsesTokenizerModel(t *testing.T) {
	parsed := &ParsedRequest{
		Body:      []byte(`{"model":"Kimi-K2.5","messages":[{"role":"user","content":"hello there, please reply briefly"}],"system":"reply briefly"}`),
		System:    "reply briefly",
		HasSystem: true,
		Messages: []any{
			map[string]any{
				"role":    "user",
				"content": "hello there, please reply briefly",
			},
		},
	}

	got := EstimateMoonshotCompatibleInputTokens(parsed)
	if got != 14 {
		t.Fatalf("EstimateMoonshotCompatibleInputTokens() = %d, want 14", got)
	}
	if fallback := EstimateCompatibleInputTokens(parsed); fallback == got {
		t.Fatalf("moonshot tokenizer path should differ from generic fallback, both = %d", got)
	}
}
