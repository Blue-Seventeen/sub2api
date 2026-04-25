package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newCompatibilityTestContext(method, path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, nil)
	return c
}

func assertCompatibilityContext(t *testing.T, c *gin.Context, wantProfile service.ClientProfile, wantProtocol service.InboundProtocol) {
	t.Helper()

	require.Equal(t, wantProfile, service.GetCompatibilityClientProfile(c))
	require.Equal(t, wantProtocol, service.GetCompatibilityInboundProtocol(c))

	profileFromRequest, ok := service.ClientProfileFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, wantProfile, profileFromRequest)

	protocolFromRequest, ok := service.InboundProtocolFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, wantProtocol, protocolFromRequest)
}

func TestCompatibilityHelperDetections(t *testing.T) {
	t.Run("anthropic_messages_detects_claude_code", func(t *testing.T) {
		c := newCompatibilityTestContext(http.MethodPost, "/v1/messages")
		c.Request.Header.Set("User-Agent", "claude-cli/1.0.1")
		c.Request.Header.Set("X-App", "claude-code")
		c.Request.Header.Set("anthropic-beta", "message-batches-2024-09-24")
		c.Request.Header.Set("anthropic-version", "2023-06-01")

		setCompatibilityForAnthropicMessages(c, validClaudeCodeBodyJSON(), nil)

		assertCompatibilityContext(t, c, service.ClientProfileClaudeCode, service.InboundProtocolAnthropicMessages)
	})

	t.Run("responses_http_detects_codex", func(t *testing.T) {
		c := newCompatibilityTestContext(http.MethodPost, "/backend-api/codex/responses")

		setCompatibilityForResponsesHTTP(c, false)

		assertCompatibilityContext(t, c, service.ClientProfileCodex, service.InboundProtocolOpenAIResponsesHTTP)
	})

	t.Run("responses_ws_force_codex_sets_ws_protocol", func(t *testing.T) {
		c := newCompatibilityTestContext(http.MethodGet, "/v1/realtime")

		setCompatibilityForResponsesWS(c, true)

		assertCompatibilityContext(t, c, service.ClientProfileCodex, service.InboundProtocolOpenAIResponsesWS)
	})

	t.Run("chat_completions_detects_cherry_studio", func(t *testing.T) {
		c := newCompatibilityTestContext(http.MethodPost, "/v1/chat/completions")
		c.Request.Header.Set("X-Title", "Cherry-Studio")

		setCompatibilityForChatCompletions(c)

		assertCompatibilityContext(t, c, service.ClientProfileCherryStudio, service.InboundProtocolOpenAIChatCompletions)
	})

	t.Run("images_without_hint_stays_generic_openai", func(t *testing.T) {
		c := newCompatibilityTestContext(http.MethodPost, "/v1/images/generations")
		c.Request.Header.Set("User-Agent", "curl/8.6.0")

		setCompatibilityForImages(c)

		assertCompatibilityContext(t, c, service.ClientProfileGenericOpenAI, service.InboundProtocolOpenAIImages)
	})

	t.Run("images_detects_cherry_studio", func(t *testing.T) {
		c := newCompatibilityTestContext(http.MethodPost, "/v1/images/generations")
		c.Request.Header.Set("Origin", "app://cherry-studio")

		setCompatibilityForImages(c)

		assertCompatibilityContext(t, c, service.ClientProfileCherryStudio, service.InboundProtocolOpenAIImages)
	})

	t.Run("compatible_route_responses_delegates_to_http_responses", func(t *testing.T) {
		c := newCompatibilityTestContext(http.MethodPost, "/v1/responses")

		setCompatibilityForCompatibleRoute(c, service.CompatibleRouteResponses, nil, nil)

		assertCompatibilityContext(t, c, service.ClientProfileGenericOpenAI, service.InboundProtocolOpenAIResponsesHTTP)
	})
}
