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
		{"strips query and redacts host", "https://api.anthropic.com/v1/messages?beta=true", "https://*.*.*.*/v1/messages"},
		{"strips fragment and redacts host", "https://api.openai.com/v1/responses#frag", "https://*.*.*.*/v1/responses"},
		{"strips both and redacts host", "https://host/path?token=secret#x", "https://*.*.*.*/path"},
		{"redacts host", "https://host/path", "https://*.*.*.*/path"},
		{"empty string", "", ""},
		{"whitespace only", "  ", ""},
		{"query before fragment", "https://h/p?a=1#f", "https://*.*.*.*/p"},
		{"relative path", "/v1/responses?token=secret", "/v1/responses"},
		{"schemeless host redacted", "api.openai.com/v1/responses?token=secret", "<redacted>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, safeUpstreamURL(tt.input))
		})
	}
}
