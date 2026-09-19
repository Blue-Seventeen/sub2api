package service

import "testing"

func TestSSEUsagePassthroughEventSemantics(t *testing.T) {
	tests := []struct {
		name    string
		initial ClaudeUsage
		data    string
		want    ClaudeUsage
	}{
		{
			name: "start resets canonical input and cache but not output",
			initial: ClaudeUsage{InputTokens: 10, OutputTokens: 8, CacheCreationInputTokens: 7,
				CacheReadInputTokens: 6, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
			data: `{"type":"message_start","message":{"usage":{"input_tokens":0,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0}}}}`,
			want: ClaudeUsage{OutputTokens: 8},
		},
		{
			name:    "start missing canonical fields resets them but preserves absent TTL details",
			initial: ClaudeUsage{InputTokens: 10, OutputTokens: 8, CacheCreationInputTokens: 7, CacheReadInputTokens: 6, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
			data:    `{"type":"message_start","message":{"usage":{}}}`,
			want:    ClaudeUsage{OutputTokens: 8, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
		},
		{
			name: "start derives aggregate cache creation from TTL details",
			data: `{"type":"message_start","message":{"usage":{"input_tokens":12,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cached_tokens":9,"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4}}}}`,
			want: ClaudeUsage{InputTokens: 12, CacheReadInputTokens: 9, CacheCreationInputTokens: 7, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
		},
		{
			name: "start canonical cache counts take precedence over aliases",
			data: `{"type":"message_start","message":{"usage":{"input_tokens":12,"cache_creation_input_tokens":20,"cache_read_input_tokens":15,"cached_tokens":9,"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4}}}}`,
			want: ClaudeUsage{InputTokens: 12, CacheReadInputTokens: 15, CacheCreationInputTokens: 20, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
		},
		{
			name:    "delta preserves zero canonical counts and replaces supplied TTL zeros",
			initial: ClaudeUsage{InputTokens: 10, OutputTokens: 8, CacheCreationInputTokens: 7, CacheReadInputTokens: 6, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
			data:    `{"type":"message_delta","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}}}`,
			want:    ClaudeUsage{InputTokens: 10, OutputTokens: 8, CacheCreationInputTokens: 7, CacheReadInputTokens: 6},
		},
		{
			name:    "delta preserves omitted TTL bucket",
			initial: ClaudeUsage{InputTokens: 10, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
			data:    `{"type":"message_delta","usage":{"output_tokens":8,"cache_creation_input_tokens":4,"cache_creation":{"ephemeral_5m_input_tokens":0}}}`,
			want:    ClaudeUsage{InputTokens: 10, OutputTokens: 8, CacheCreationInputTokens: 4, CacheCreation1hTokens: 4},
		},
		{
			name: "delta updates positive counts and TTL details",
			data: `{"type":"message_delta","usage":{"input_tokens":10,"output_tokens":8,"cache_read_input_tokens":6,"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4}}}`,
			want: ClaudeUsage{InputTokens: 10, OutputTokens: 8, CacheReadInputTokens: 6, CacheCreationInputTokens: 7, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4},
		},
		{
			name:    "other events ignore canonical fields but use cache fallbacks",
			initial: ClaudeUsage{InputTokens: 10, OutputTokens: 8},
			data:    `{"type":"content_block_delta","usage":{"input_tokens":99,"output_tokens":99,"cache_read_input_tokens":99,"cache_creation_input_tokens":99,"cached_tokens":6,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":1}}}`,
			want:    ClaudeUsage{InputTokens: 10, OutputTokens: 8, CacheReadInputTokens: 6, CacheCreationInputTokens: 3},
		},
		{
			name: "nested cache fallbacks precede top level even outside start",
			data: `{"type":"content_block_delta","message":{"usage":{"cached_tokens":6,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":1}}},"usage":{"cached_tokens":99,"cache_creation":{"ephemeral_5m_input_tokens":9,"ephemeral_1h_input_tokens":9}}}`,
			want: ClaudeUsage{CacheReadInputTokens: 6, CacheCreationInputTokens: 3},
		},
		{
			name: "missing start usage still permits top level cache fallbacks",
			data: `{"type":"message_start","usage":{"cached_tokens":6,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":1}}}`,
			want: ClaudeUsage{CacheReadInputTokens: 6, CacheCreationInputTokens: 3},
		},
		{
			name: "zero nested cache fallbacks allow top level values",
			data: `{"type":"message_start","message":{"usage":{"cached_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0}}},"usage":{"cached_tokens":6,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":1}}}`,
			want: ClaudeUsage{CacheReadInputTokens: 6, CacheCreationInputTokens: 3},
		},
		{
			name:    "late aliases do not replace existing cache counts",
			initial: ClaudeUsage{CacheReadInputTokens: 6, CacheCreationInputTokens: 3},
			data:    `{"type":"content_block_delta","usage":{"cached_tokens":99,"cache_creation":{"ephemeral_5m_input_tokens":9,"ephemeral_1h_input_tokens":9}}}`,
			want:    ClaudeUsage{CacheReadInputTokens: 6, CacheCreationInputTokens: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.initial
			parseSSEUsagePassthrough(tt.data, &got)
			if got != tt.want {
				t.Fatalf("usage = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSSEUsagePassthroughCompatiblePromptBuckets(t *testing.T) {
	tests := []struct {
		name    string
		initial ClaudeUsage
		events  []string
		want    ClaudeUsage
	}{
		{
			name: "Kimi start total becomes uncached input without double subtraction",
			events: []string{
				`{"type":"message_start","message":{"usage":{"input_tokens":173306,"prompt_tokens":173306,"cached_tokens":0}}}`,
				`{"type":"message_delta","usage":{"input_tokens":250,"cache_read_input_tokens":173056,"output_tokens":166,"prompt_tokens":173306,"cached_tokens":173056}}`,
			},
			want: ClaudeUsage{InputTokens: 250, OutputTokens: 166, CacheReadInputTokens: 173056},
		},
		{
			name: "Kimi fully cached delta clears start input",
			events: []string{
				`{"type":"message_start","message":{"usage":{"input_tokens":173306,"prompt_tokens":173306}}}`,
				`{"type":"message_delta","usage":{"input_tokens":0,"prompt_tokens":173306,"cached_tokens":173306,"output_tokens":8}}`,
			},
			want: ClaudeUsage{OutputTokens: 8, CacheReadInputTokens: 173306},
		},
		{
			name:   "Kimi start cache read and creation are excluded from prompt total",
			events: []string{`{"type":"message_start","message":{"usage":{"input_tokens":1200,"prompt_tokens":1200,"cached_tokens":800,"cache_creation":{"ephemeral_5m_input_tokens":60,"ephemeral_1h_input_tokens":40}}}}`},
			want:   ClaudeUsage{InputTokens: 300, CacheReadInputTokens: 800, CacheCreationInputTokens: 100, CacheCreation5mTokens: 60, CacheCreation1hTokens: 40},
		},
		{
			name:   "GLM nested cache details",
			events: []string{`{"type":"message_delta","usage":{"input_tokens":1200,"prompt_tokens":1200,"prompt_tokens_details":{"cached_tokens":800},"output_tokens":30}}`},
			want:   ClaudeUsage{InputTokens: 400, OutputTokens: 30, CacheReadInputTokens: 800},
		},
		{
			name:   "DeepSeek hit and miss override overloaded input",
			events: []string{`{"type":"message_delta","usage":{"input_tokens":1200,"prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":400,"output_tokens":30}}`},
			want:   ClaudeUsage{InputTokens: 400, OutputTokens: 30, CacheReadInputTokens: 800},
		},
		{
			name:    "explicit compatible zeros clear stale cache buckets",
			initial: ClaudeUsage{InputTokens: 10, CacheReadInputTokens: 800, CacheCreationInputTokens: 100},
			events:  []string{`{"type":"message_delta","usage":{"prompt_tokens":1200,"cache_read_input_tokens":0,"cached_tokens":0,"cache_creation_input_tokens":0}}`},
			want:    ClaudeUsage{InputTokens: 1200},
		},
		{
			name:    "missing compatible cache fields retain previous buckets",
			initial: ClaudeUsage{CacheReadInputTokens: 800, CacheCreationInputTokens: 100},
			events:  []string{`{"type":"message_delta","usage":{"prompt_tokens":1200,"output_tokens":30}}`},
			want:    ClaudeUsage{InputTokens: 300, OutputTokens: 30, CacheReadInputTokens: 800, CacheCreationInputTokens: 100},
		},
		{
			name:   "canonical cache read wins over provider aliases",
			events: []string{`{"type":"message_delta","usage":{"prompt_tokens":1200,"cache_read_input_tokens":800,"cached_tokens":600,"prompt_tokens_details":{"cached_tokens":400},"prompt_cache_hit_tokens":200}}`},
			want:   ClaudeUsage{InputTokens: 400, CacheReadInputTokens: 800},
		},
		{
			name:   "cache aliases prefer top level before nested and hit fields",
			events: []string{`{"type":"message_delta","usage":{"prompt_tokens":1200,"cached_tokens":600,"prompt_tokens_details":{"cached_tokens":400},"prompt_cache_hit_tokens":200}}`},
			want:   ClaudeUsage{InputTokens: 600, CacheReadInputTokens: 600},
		},
		{
			name:   "explicit miss bucket is authoritative and nonnegative",
			events: []string{`{"type":"message_delta","usage":{"input_tokens":100,"prompt_tokens":1200,"prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":-1}}`},
			want:   ClaudeUsage{CacheReadInputTokens: 800},
		},
		{
			name:   "cache exceeding prompt total clamps uncached input to zero",
			events: []string{`{"type":"message_delta","usage":{"input_tokens":100,"prompt_tokens":20,"cached_tokens":30}}`},
			want:   ClaudeUsage{CacheReadInputTokens: 30},
		},
		{
			name:   "native Anthropic buckets remain disjoint without provider aliases",
			events: []string{`{"type":"message_start","message":{"usage":{"input_tokens":2,"cache_read_input_tokens":376156,"cache_creation_input_tokens":733}}}`},
			want:   ClaudeUsage{InputTokens: 2, CacheReadInputTokens: 376156, CacheCreationInputTokens: 733},
		},
		{
			name:   "zero prompt total alone does not trigger compatible normalization",
			events: []string{`{"type":"message_delta","usage":{"input_tokens":2,"prompt_tokens":0,"cache_read_input_tokens":30}}`},
			want:   ClaudeUsage{InputTokens: 2, CacheReadInputTokens: 30},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.initial
			for _, data := range tt.events {
				parseSSEUsagePassthrough(data, &got)
			}
			if got != tt.want {
				t.Fatalf("usage = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSSEUsagePassthroughNoop(t *testing.T) {
	want := ClaudeUsage{InputTokens: 3, OutputTokens: 5, CacheReadInputTokens: 7, CacheCreationInputTokens: 11, CacheCreation5mTokens: 4, CacheCreation1hTokens: 7, ImageOutputTokens: 2}
	for _, data := range []string{"", "[DONE]", "not-json", `{}`, `{"type":"message_start"}`, `{"type":"message_delta"}`, `{"type":"message_stop"}`} {
		t.Run(data, func(t *testing.T) {
			got := want
			parseSSEUsagePassthrough(data, &got)
			if got != want {
				t.Fatalf("usage = %+v, want %+v", got, want)
			}
			parseSSEUsagePassthrough(data, nil)
		})
	}
}
