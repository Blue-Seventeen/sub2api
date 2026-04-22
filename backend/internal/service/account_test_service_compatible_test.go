//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountTestService_CompatibleAccountFallsBackToRelayChatEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, recorder := newTestContext()

	upstream := &queuedHTTPUpstream{
		responses: []*http.Response{
			newJSONResponse(http.StatusNotFound, `{"error":{"message":"route not found"}}`),
			newJSONResponse(http.StatusOK, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"glm-4.6v\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"),
		},
	}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{},
			},
		},
	}
	account := &Account{
		ID:          7,
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"base_url": "https://relay.example.com",
			"api_key":  "test-key",
		},
	}

	err := svc.testCompatibleAccountConnection(ctx, account, "glm-4.6v")
	require.NoError(t, err)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://relay.example.com/api/paas/v4/chat/completions", upstream.requests[0].URL.String())
	require.Equal(t, "https://relay.example.com/v1/chat/completions", upstream.requests[1].URL.String())
	require.Contains(t, recorder.Body.String(), "test_complete")
}
