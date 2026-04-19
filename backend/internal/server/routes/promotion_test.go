package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newPromotionRoutesTestRouter(jwt servermiddleware.JWTAuthMiddleware, admin servermiddleware.AdminAuthMiddleware) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterPromotionRoutes(
		v1,
		&handler.Handlers{
			Promotion: &handler.PromotionHandler{},
			Admin: &handler.AdminHandlers{
				Promotion: &adminhandler.PromotionHandler{},
			},
		},
		jwt,
		admin,
		nil,
	)
	return router
}

func TestPromotionUserRoutesRequireJWT(t *testing.T) {
	router := newPromotionRoutesTestRouter(
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		}),
		servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
			c.Next()
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/promotion/me/overview", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPromotionAdminRoutesRequireAdmin(t *testing.T) {
	router := newPromotionRoutesTestRouter(
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "forbidden"})
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/promotion/dashboard", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
}
