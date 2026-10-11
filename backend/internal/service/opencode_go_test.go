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

func TestOpenCodeGoDefaultProtocolRules(t *testing.T) {
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
		require.Equal(t, tc.want, matchProtocolRules(tc.model, DefaultOpenCodeGoProtocolRules()), "model=%s", tc.model)
	}
}

func TestOpenCodeGoModelRoutedProtocol(t *testing.T) {
	t.Parallel()
	adaptive := &Account{Platform: PlatformOpenCodeGo, Credentials: map[string]any{"api_protocol": APIProtocolAdaptive}}
	require.Equal(t, APIProtocolAnthropic, adaptive.resolveModelRoutedProtocol("minimax-m3"))
	require.Equal(t, APIProtocolResponses, adaptive.resolveModelRoutedProtocol("grok-4.6"))
	require.Equal(t, APIProtocolChatCompletions, adaptive.resolveModelRoutedProtocol("glm-5.3"))

	pinned := &Account{Platform: PlatformOpenCodeGo, Credentials: map[string]any{"api_protocol": APIProtocolChatCompletions}}
	require.Equal(t, APIProtocolChatCompletions, pinned.resolveModelRoutedProtocol("minimax-m3"))

	empty := &Account{Platform: PlatformOpenCodeGo}
	require.Equal(t, APIProtocolResponses, empty.resolveModelRoutedProtocol("gpt-5.6-luna"))
	require.Equal(t, AccountModeGo, empty.GetOpenCodeAccountMode())
	require.True(t, empty.IsOpenCodeGoPlan())

	zen := &Account{Platform: PlatformOpenCodeGo, Credentials: map[string]any{"account_mode": AccountModeZen, "api_protocol": APIProtocolAdaptive}}
	require.Equal(t, APIProtocolChatCompletions, zen.resolveModelRoutedProtocol("minimax-m3"))
	require.Equal(t, APIProtocolAnthropic, zen.resolveModelRoutedProtocol("claude-opus-4-6"))
	require.Equal(t, APIProtocolChatCompletions, zen.resolveModelRoutedProtocol("qwen3.8-max"))
	require.Equal(t, APIProtocolAnthropic, zen.resolveModelRoutedProtocol("qwen3.8-flash"))
	require.Equal(t, APIProtocolAnthropic, zen.resolveModelRoutedProtocol("qwen3.7-max"))
	require.Equal(t, DefaultOpenCodeZenBaseURL, zen.GetOpenAIBaseURL())

	goAccount := &Account{Platform: PlatformOpenCodeGo, Credentials: map[string]any{"account_mode": AccountModeGo, "api_protocol": APIProtocolAdaptive}}
	require.Equal(t, APIProtocolAnthropic, goAccount.resolveModelRoutedProtocol("qwen3.8-max"))

	require.False(t, (&Account{Platform: PlatformKimi}).routesByModel())
}

func TestOpenCodeGoModelRoutedProtocolUsesAccountRules(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAdaptive,
			protocolRulesCredentialKey: []any{
				map[string]any{"pattern": "grok-*", "protocol": APIProtocolChatCompletions},
				map[string]any{"pattern": "deepseek-v4-flash", "protocol": APIProtocolResponses},
				map[string]any{"pattern": "qwen*", "protocol": APIProtocolAnthropic},
			},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, account.resolveModelRoutedProtocol("grok-4.6"))
	require.Equal(t, APIProtocolResponses, account.resolveModelRoutedProtocol("deepseek-v4-flash"))
	require.Equal(t, APIProtocolAnthropic, account.resolveModelRoutedProtocol("qwen3.8-max"))
	require.Equal(t, APIProtocolChatCompletions, account.resolveModelRoutedProtocol("glm-5.3"))
	require.Equal(t, APIProtocolChatCompletions, account.resolveModelRoutedProtocol("minimax-m3"))
}

func TestOpenCodeGoModelRoutedProtocolEmptyRulesAreAllChatCompletions(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			protocolRulesCredentialKey: []any{},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, account.resolveModelRoutedProtocol("grok-4.6"))
	require.Equal(t, APIProtocolChatCompletions, account.resolveModelRoutedProtocol("minimax-m3"))
}

func TestOpenCodeGoModelRoutedProtocolFirstMatchWins(t *testing.T) {
	t.Parallel()
	account := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			protocolRulesCredentialKey: []any{
				map[string]any{"pattern": "gpt-5.6-luna", "protocol": APIProtocolChatCompletions},
				map[string]any{"pattern": "gpt-*", "protocol": APIProtocolResponses},
			},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, account.resolveModelRoutedProtocol("gpt-5.6-luna"))
	require.Equal(t, APIProtocolResponses, account.resolveModelRoutedProtocol("gpt-5.4"))
}

func TestOpenCodeGoUnmatchedFallsBackToChatCompletions(t *testing.T) {
	t.Parallel()
	withRules := &Account{
		Platform: PlatformOpenCodeGo,
		Credentials: map[string]any{
			"api_protocol": APIProtocolAdaptive,
			protocolRulesCredentialKey: []any{
				map[string]any{"pattern": "grok-*", "protocol": APIProtocolResponses},
				map[string]any{"pattern": "gpt-*", "protocol": APIProtocolResponses},
				map[string]any{"pattern": "qwen*", "protocol": APIProtocolAnthropic},
			},
		},
	}
	require.Equal(t, APIProtocolChatCompletions, withRules.resolveModelRoutedProtocol("deepseek-v4-flash"))
	require.Equal(t, APIProtocolChatCompletions, withRules.resolveModelRoutedProtocol("glm-5.3"))
	require.Equal(t, APIProtocolChatCompletions, withRules.resolveModelRoutedProtocol("omen-alpha"))
	require.Equal(t, APIProtocolChatCompletions, withRules.resolveModelRoutedProtocol("kimi-k3"))
	require.Equal(t, APIProtocolChatCompletions, withRules.resolveModelRoutedProtocol("unknown-model"))
	require.Equal(t, APIProtocolResponses, withRules.resolveModelRoutedProtocol("grok-4.6"))
	require.Equal(t, APIProtocolAnthropic, withRules.resolveModelRoutedProtocol("qwen3.8-flash"))

	defaults := &Account{Platform: PlatformOpenCodeGo}
	require.Equal(t, APIProtocolChatCompletions, defaults.resolveModelRoutedProtocol("deepseek-v4-flash"))
}

func TestParseOpenCodeGoProtocolRulesAcceptsTypedMaps(t *testing.T) {
	t.Parallel()
	rules, err := parseProtocolRules([]map[string]any{
		{"pattern": "grok-*", "protocol": APIProtocolResponses},
		{"pattern": "qwen*", "protocol": APIProtocolAnthropic},
	})
	require.NoError(t, err)
	require.Equal(t, []ProtocolRule{
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
	require.Equal(t, APIProtocolResponses, account.resolveModelRoutedProtocol("grok-4.6"))
	require.Equal(t, APIProtocolResponses, account.resolveModelRoutedProtocol("gpt-5.6-luna"))
	require.Equal(t, APIProtocolAnthropic, account.resolveModelRoutedProtocol("qwen3.8-flash"))
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
				credentials[protocolRulesCredentialKey] = []map[string]any{{"pattern": mappedModel, "protocol": protocol.protocol}}
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
	require.NoError(t, NormalizeProtocolRulesCredentials(nil))
	require.NoError(t, NormalizeProtocolRulesCredentials(map[string]any{}))

	creds := map[string]any{
		protocolRulesCredentialKey: []any{
			map[string]any{"pattern": " Grok-* ", "protocol": APIProtocolResponses},
		},
	}
	require.NoError(t, NormalizeProtocolRulesCredentials(creds))
	rules, ok := creds[protocolRulesCredentialKey].([]any)
	require.True(t, ok)
	require.NotEmpty(t, rules)
	entry, ok := rules[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "grok-*", entry["pattern"])

	err := NormalizeProtocolRulesCredentials(map[string]any{
		protocolRulesCredentialKey: []any{
			map[string]any{"pattern": "*grok", "protocol": APIProtocolResponses},
		},
	})
	require.Error(t, err)

	err = NormalizeProtocolRulesCredentials(map[string]any{
		protocolRulesCredentialKey: []any{
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

func TestIsOpenCodeUnsupportedModel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model string
		want  bool
	}{
		{"gemini-3.8-flash", true},
		{"gemini-3.7-flash", true},
		{"opencode/gemini-3.8-flash", true},
		{"opencode-go/gemini-3.1-pro", true},
		{"jev-1.13", true},
		{"jev-1.13-free", true},
		{"opencode/jev-1.13", true},
		{"qwen3.8-max", false},
		{"qwen3.8-flash", false},
		{"gpt-5.6-luna", false},
		{"grok-4.6", false},
		{"minimax-m3", false},
		{"glm-5.3", false},
		{"", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, IsOpenCodeUnsupportedModel(tc.model), "model=%s", tc.model)
	}
}
