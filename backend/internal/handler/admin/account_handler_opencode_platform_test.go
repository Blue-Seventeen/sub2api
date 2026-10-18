package admin

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCreateAccountRequestBindsOpenCodeGoPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/api/v1/admin/accounts", strings.NewReader(
		`{"name":"OpenCode test","platform":"opencode_go","type":"apikey","credentials":{"base_url":"https://opencode.ai/zen/v1","api_key":"test-key"}}`,
	))

	var req CreateAccountRequest
	require.NoError(t, ctx.ShouldBindJSON(&req))
	require.Equal(t, "opencode_go", req.Platform)
}
