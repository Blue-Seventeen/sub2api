package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeUpstreamURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips query and preserves host", "https://api.anthropic.com/v1/messages?beta=true", "https://api.anthropic.com/v1/messages"},
		{"strips fragment and preserves host", "https://api.openai.com/v1/responses#frag", "https://api.openai.com/v1/responses"},
		{"strips both and preserves host", "https://host/path?token=secret#x", "https://host/path"},
		{"preserves host", "https://host/path", "https://host/path"},
		{"strips userinfo", "https://user:password@api.openai.com/v1/responses?api_key=secret", "https://api.openai.com/v1/responses"},
		{"preserves ip host", "http://10.0.0.9:8080/v1/chat/completions?token=secret", "http://10.0.0.9:8080/v1/chat/completions"},
		{"empty string", "", ""},
		{"whitespace only", "  ", ""},
		{"query before fragment", "https://h/p?a=1#f", "https://h/p"},
		{"relative path", "/v1/responses?token=secret", "/v1/responses"},
		{"schemeless host redacted", "api.openai.com/v1/responses?token=secret", "<redacted>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, safeUpstreamURL(tt.input))
		})
	}
}
