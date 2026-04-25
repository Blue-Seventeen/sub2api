package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompatibilityRuntimeContextRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)

	SetCompatibilityClientProfile(c, ClientProfileCodex)
	SetCompatibilityInboundProtocol(c, InboundProtocolOpenAIResponsesHTTP)
	SetCompatibilityRoute(c, CompatibilityRouteOpenAIResponsesNative)
	SetCompatibilityUpstreamTransport(c, UpstreamTransportSSE)
	AppendCompatibilityFallbackStage(c, "native")
	AppendCompatibilityFallbackStage(c, "native")
	AppendCompatibilityFallbackStage(c, "relay")

	require.Equal(t, ClientProfileCodex, GetCompatibilityClientProfile(c))
	require.Equal(t, InboundProtocolOpenAIResponsesHTTP, GetCompatibilityInboundProtocol(c))
	require.Equal(t, CompatibilityRouteOpenAIResponsesNative, GetCompatibilityRoute(c))
	require.Equal(t, UpstreamTransportSSE, GetCompatibilityUpstreamTransport(c))
	require.Equal(t, []string{"native", "relay"}, GetCompatibilityFallbackStages(c))
	require.Equal(t, "native -> relay", GetCompatibilityFallbackChain(c))

	clientProfile, ok := ClientProfileFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, ClientProfileCodex, clientProfile)

	inboundProtocol, ok := InboundProtocolFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, InboundProtocolOpenAIResponsesHTTP, inboundProtocol)

	fields := CompatibilityLogFieldsFromContext(c)
	require.Equal(t, "codex", fields.ClientProfile)
	require.Equal(t, "openai_responses_native", fields.CompatibilityRoute)
	require.Equal(t, "native -> relay", fields.FallbackChain)
	require.Equal(t, "sse", fields.UpstreamTransport)
}
