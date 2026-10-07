package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeGoModelProtocol(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model string
		want  string
	}{
		{"grok-4.6", APIProtocolResponses},
		{"opencode-go/gpt-5.6-luna", APIProtocolResponses},
		{"muse-spark-1.3-contributor", APIProtocolResponses},
		{"minimax-m3", APIProtocolAnthropic},
		{"qwen3.8-max", APIProtocolAnthropic},
		{"qwen3.7-plus", APIProtocolAnthropic},
		{"glm-5.3", APIProtocolChatCompletions},
		{"kimi-k3", APIProtocolChatCompletions},
		{"deepseek-v4-pro", APIProtocolChatCompletions},
		{"mimo-v2.5-pro", APIProtocolChatCompletions},
		{"hy4-preview", APIProtocolChatCompletions},
		{"omen-alpha", APIProtocolChatCompletions},
		{"", APIProtocolChatCompletions},
		{"unknown-model", APIProtocolChatCompletions},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, OpenCodeGoModelProtocol(tc.model), "model=%s", tc.model)
	}
}

func TestResolveOpenCodeGoUpstreamProtocol(t *testing.T) {
	t.Parallel()
	adaptive := &Account{Platform: PlatformOpenCodeGo, Credentials: map[string]any{"api_protocol": APIProtocolAdaptive}}
	require.Equal(t, APIProtocolAnthropic, adaptive.ResolveOpenCodeGoUpstreamProtocol("minimax-m3"))
	require.Equal(t, APIProtocolResponses, adaptive.ResolveOpenCodeGoUpstreamProtocol("grok-4.6"))
	require.Equal(t, APIProtocolChatCompletions, adaptive.ResolveOpenCodeGoUpstreamProtocol("glm-5.3"))

	pinned := &Account{Platform: PlatformOpenCodeGo, Credentials: map[string]any{"api_protocol": APIProtocolChatCompletions}}
	require.Equal(t, APIProtocolChatCompletions, pinned.ResolveOpenCodeGoUpstreamProtocol("minimax-m3"))

	empty := &Account{Platform: PlatformOpenCodeGo}
	require.Equal(t, APIProtocolResponses, empty.ResolveOpenCodeGoUpstreamProtocol("gpt-5.6-luna"))
	require.Equal(t, AccountModeGo, empty.GetOpenCodeAccountMode())
	require.True(t, empty.IsOpenCodeGoPlan())

	zen := &Account{Platform: PlatformOpenCodeGo, Credentials: map[string]any{"account_mode": AccountModeZen, "api_protocol": APIProtocolAdaptive}}
	require.Equal(t, APIProtocolChatCompletions, zen.ResolveOpenCodeGoUpstreamProtocol("minimax-m3"))
	require.Equal(t, APIProtocolAnthropic, zen.ResolveOpenCodeGoUpstreamProtocol("claude-opus-4-6"))
	require.Equal(t, DefaultOpenCodeZenBaseURL, zen.GetOpenAIBaseURL())

	require.Equal(t, "", (&Account{Platform: PlatformKimi}).ResolveOpenCodeGoUpstreamProtocol("glm-5.3"))
}

func TestResolveOpenCodeGoUpstreamProtocolUsesAccountRules(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAdaptive,
			openCodeGoProtocolRulesKey: []any{
				map[string]any{"pattern": "grok-*", "protocol": APIProtocolChatCompletions},
				map[string]any{"pattern": "deepseek-v4-flash", "protocol": APIProtocolResponses},
				map[string]any{"pattern": "qwen*", "protocol": APIProtocolAnthropic},
			},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, account.ResolveOpenCodeGoUpstreamProtocol("grok-4.6"))
	require.Equal(t, APIProtocolResponses, account.ResolveOpenCodeGoUpstreamProtocol("deepseek-v4-flash"))
	require.Equal(t, APIProtocolAnthropic, account.ResolveOpenCodeGoUpstreamProtocol("qwen3.8-max"))
	require.Equal(t, APIProtocolChatCompletions, account.ResolveOpenCodeGoUpstreamProtocol("glm-5.3"))
	require.Equal(t, APIProtocolChatCompletions, account.ResolveOpenCodeGoUpstreamProtocol("minimax-m3"))
}

func TestResolveOpenCodeGoUpstreamProtocolEmptyRulesAreAllChatCompletions(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			openCodeGoProtocolRulesKey: []any{},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, account.ResolveOpenCodeGoUpstreamProtocol("grok-4.6"))
	require.Equal(t, APIProtocolChatCompletions, account.ResolveOpenCodeGoUpstreamProtocol("minimax-m3"))
}

func TestResolveOpenCodeGoUpstreamProtocolFirstMatchWins(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			openCodeGoProtocolRulesKey: []any{
				map[string]any{"pattern": "gpt-5.6-luna", "protocol": APIProtocolChatCompletions},
				map[string]any{"pattern": "gpt-*", "protocol": APIProtocolResponses},
			},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, account.ResolveOpenCodeGoUpstreamProtocol("gpt-5.6-luna"))
	require.Equal(t, APIProtocolResponses, account.ResolveOpenCodeGoUpstreamProtocol("gpt-5.4"))
}

func TestOpenCodeGoNativeProtocolUnmatchedFallsBackToChatCompletions(t *testing.T) {
	t.Parallel()
	withRules := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAdaptive,
			openCodeGoProtocolRulesKey: []any{
				map[string]any{"pattern": "grok-*", "protocol": APIProtocolResponses},
				map[string]any{"pattern": "gpt-*", "protocol": APIProtocolResponses},
				map[string]any{"pattern": "qwen*", "protocol": APIProtocolAnthropic},
			},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, openCodeGoNativeProtocol(withRules, "deepseek-v4-flash"))
	require.Equal(t, APIProtocolChatCompletions, openCodeGoNativeProtocol(withRules, "glm-5.3"))
	require.Equal(t, APIProtocolChatCompletions, openCodeGoNativeProtocol(withRules, "omen-alpha"))
	require.Equal(t, APIProtocolChatCompletions, openCodeGoNativeProtocol(withRules, "kimi-k3"))
	require.Equal(t, APIProtocolChatCompletions, openCodeGoNativeProtocol(withRules, "unknown-model"))
	require.Equal(t, APIProtocolResponses, openCodeGoNativeProtocol(withRules, "grok-4.6"))
	require.Equal(t, APIProtocolAnthropic, openCodeGoNativeProtocol(withRules, "qwen3.8-flash"))

	defaults := &Account{Platform: PlatformOpenCodeGo}
	require.Equal(t, APIProtocolChatCompletions, openCodeGoNativeProtocol(defaults, "deepseek-v4-flash"))
	require.Equal(t, APIProtocolChatCompletions, openCodeGoNativeProtocol(nil, "grok-4.6"))
}

func TestParseOpenCodeGoProtocolRulesAcceptsTypedMaps(t *testing.T) {
	t.Parallel()
	rules, err := parseOpenCodeGoProtocolRules([]map[string]any{
		{"pattern": "grok-*", "protocol": APIProtocolResponses},
		{"pattern": "qwen*", "protocol": APIProtocolAnthropic},
	})
	require.NoError(t, err)
	require.Equal(t, []OpenCodeGoProtocolRule{
		{Pattern: "grok-*", Protocol: APIProtocolResponses},
		{Pattern: "qwen*", Protocol: APIProtocolAnthropic},
	}, rules)
}

func TestShouldForwardOpenAIResponsesViaRawChatCompletions_OpenCodeGoIgnoresProbe(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAdaptive,
		},
		Extra: map[string]any{
			"openai_responses_supported": false,
		},
	}
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account))
	require.Equal(t, APIProtocolResponses, account.ResolveOpenCodeGoUpstreamProtocol("grok-4.6"))
	require.Equal(t, APIProtocolResponses, account.ResolveOpenCodeGoUpstreamProtocol("gpt-5.6-luna"))
	require.Equal(t, APIProtocolAnthropic, account.ResolveOpenCodeGoUpstreamProtocol("qwen3.8-flash"))
}

func TestForwardOpenCodeGoResponsesUsesMappedModelProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	protocols := []struct {
		name     string
		model    string
		mapped   string
		protocol string
		wantPath string
	}{
		{name: "responses model", model: "public-gpt", mapped: "gpt-5.6-luna", protocol: APIProtocolResponses, wantPath: "/v1/responses"},
		{name: "chat model", model: "glm-5.3", protocol: APIProtocolChatCompletions, wantPath: "/v1/chat/completions"},
		{name: "anthropic model", model: "qwen3.8-flash", protocol: APIProtocolAnthropic, wantPath: "/v1/messages"},
	}
	inbounds := []struct {
		name    string
		path    string
		body    func(string) []byte
		forward func(*OpenAIGatewayService, context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error)
	}{
		{
			name: "responses", path: "/v1/responses",
			body: func(model string) []byte { return []byte(`{"model":"` + model + `","input":"hello","stream":false}`) },
			forward: func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return s.Forward(ctx, c, account, body)
			},
		},
		{
			name: "chat", path: "/v1/chat/completions",
			body: func(model string) []byte {
				return []byte(`{"model":"` + model + `","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			},
			forward: func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return s.ForwardAsChatCompletions(ctx, c, account, body, "", "")
			},
		},
		{
			name: "messages", path: "/v1/messages",
			body: func(model string) []byte {
				return []byte(`{"model":"` + model + `","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
			},
			forward: func(s *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return s.ForwardAsAnthropic(ctx, c, account, body, "", "")
			},
		},
	}
	for _, protocol := range protocols {
		for _, inbound := range inbounds {
			t.Run(inbound.name+"/"+protocol.name, func(t *testing.T) {
				credentials := map[string]any{
					"api_key":      "test-key",
					"base_url":     "http://upstream.example",
					"account_mode": AccountModeGo,
					"api_protocol": APIProtocolAdaptive,
				}
				if protocol.mapped != "" {
					credentials["model_mapping"] = map[string]any{protocol.model: protocol.mapped}
				}
				mappedModel := protocol.mapped
				if mappedModel == "" {
					mappedModel = protocol.model
				}
				credentials[openCodeGoProtocolRulesKey] = []map[string]any{{"pattern": mappedModel, "protocol": protocol.protocol}}
				account := &Account{ID: 41, Name: "opencode-go", Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: credentials}
				body := inbound.body(protocol.model)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, inbound.path, strings.NewReader(string(body)))
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"injected failure"}}`)),
				}}
				svc := &OpenAIGatewayService{cfg: &config.Config{
					Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}},
				}, httpUpstream: upstream}

				_, err := inbound.forward(svc, context.Background(), c, account, body)

				require.Error(t, err)
				require.Len(t, upstream.requests, 1)
				require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, protocol.wantPath), "upstream path=%q want suffix=%q", upstream.lastReq.URL.Path, protocol.wantPath)
				require.Contains(t, string(upstream.lastBody), `"model":"`+mappedModel+`"`)
			})
		}
	}
}

func TestStampOpenAIResponsesUpstreamEndpoint(t *testing.T) {
	t.Parallel()
	result := &OpenAIForwardResult{}
	stampOpenAIResponsesUpstreamEndpoint(nil, result)
	require.Equal(t, "/v1/responses", result.UpstreamEndpoint)

	existing := &OpenAIForwardResult{UpstreamEndpoint: "/v1/messages"}
	stampOpenAIResponsesUpstreamEndpoint(nil, existing)
	require.Equal(t, "/v1/messages", existing.UpstreamEndpoint)
}

func TestNormalizeOpenCodeGoProtocolRulesCredentials(t *testing.T) {
	t.Parallel()
	require.NoError(t, NormalizeOpenCodeGoProtocolRulesCredentials(nil))
	require.NoError(t, NormalizeOpenCodeGoProtocolRulesCredentials(map[string]any{}))

	creds := map[string]any{
		openCodeGoProtocolRulesKey: []any{
			map[string]any{"pattern": " Grok-* ", "protocol": APIProtocolResponses},
		},
	}
	require.NoError(t, NormalizeOpenCodeGoProtocolRulesCredentials(creds))
	rules, ok := creds[openCodeGoProtocolRulesKey].([]any)
	require.True(t, ok)
	require.NotEmpty(t, rules)
	entry, ok := rules[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "grok-*", entry["pattern"])

	err := NormalizeOpenCodeGoProtocolRulesCredentials(map[string]any{
		openCodeGoProtocolRulesKey: []any{
			map[string]any{"pattern": "*grok", "protocol": APIProtocolResponses},
		},
	})
	require.Error(t, err)

	err = NormalizeOpenCodeGoProtocolRulesCredentials(map[string]any{
		openCodeGoProtocolRulesKey: []any{
			map[string]any{"pattern": "grok-*", "protocol": "adaptive"},
		},
	})
	require.Error(t, err)
}

func TestOpenCodeGoQuotaURL(t *testing.T) {
	t.Parallel()
	const want = "https://opencode.ai/zen/go/v1/usage"
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"empty falls back to default", "", want},
		{"chat base (/v1)", DefaultOpenCodeGoBaseURL, want},
		{"chat base trailing slash", DefaultOpenCodeGoBaseURL + "/", want},
		{"anthropic base (no /v1)", DefaultOpenCodeGoAnthropicBaseURL, want},
		{"anthropic base trailing slash", DefaultOpenCodeGoAnthropicBaseURL + "/", want},
		{"zen base stays idempotent", DefaultOpenCodeZenBaseURL, "https://opencode.ai/zen/v1/usage"},
		{"custom base with /v1", "https://custom.example/v1", "https://custom.example/v1/usage"},
		{"custom base with /v1 trailing slash", "https://custom.example/v1/", "https://custom.example/v1/usage"},
		// 与 kimiQuotaURL 同语义：无条件补 /v1，非官方域名同样归一。
		{"custom base without /v1", "https://custom.example", "https://custom.example/v1/usage"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, openCodeGoQuotaURL(tc.baseURL), "case=%s", tc.name)
	}
}

func TestDefaultOpenCodeGoModelIDsCoverDocumentedCatalog(t *testing.T) {
	t.Parallel()
	ids := DefaultOpenCodeGoModelIDs()
	require.Contains(t, ids, "grok-4.6")
	require.Contains(t, ids, "minimax-m3")
	require.Contains(t, ids, "glm-5.3")
	require.NotEmpty(t, ids)
}
