package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountHandlerCreateSyncV024Platforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{
		"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "minimax",
		"zhipu", "deepseek", "volcengine", "ali", "moonshot", "perplexity", "mistral",
		"siliconflow", "openrouter", "suno", "kling", "midjourney",
	} {
		t.Run(platform, func(t *testing.T) {
			svc := newStubAdminService()
			h := &AccountHandler{adminService: svc}
			router := gin.New()
			router.POST("/accounts", h.Create)
			body, err := json.Marshal(map[string]any{
				"name": "sync", "platform": platform, "type": "apikey",
				"credentials": map[string]any{"api_key": "test-key"},
			})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/accounts", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			require.Equal(t, http.StatusOK, res.Code, res.Body.String())
			require.Len(t, svc.createdAccounts, 1)
			require.Equal(t, platform, svc.createdAccounts[0].Platform)
		})
	}
}
