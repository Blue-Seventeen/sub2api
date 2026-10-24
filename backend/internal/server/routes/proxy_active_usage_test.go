package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProxyActiveUsageDetailsRoutePreservesUnknownAndLegacy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerProxyRoutes(router.Group("/api/v1/admin"), &handler.Handlers{
		Admin: &handler.AdminHandlers{Proxy: adminhandler.NewProxyHandler(nil, nil, nil)},
	}, func(c *gin.Context) { c.Next() })

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/api/v1/admin/proxies/active-usage/details?ids=10", `{"10":{"account_count":null,"request_count":null,"status":"unavailable"}}`},
		{"/api/v1/admin/proxies/active-usage?ids=10", `{"10":0}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var body struct {
				Data json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.JSONEq(t, tc.want, string(body.Data))
		})
	}
}
