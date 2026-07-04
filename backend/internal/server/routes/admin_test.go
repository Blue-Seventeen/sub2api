package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterSubscriptionRoutesDoesNotDuplicateRestoreRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")

	require.NotPanics(t, func() {
		registerSubscriptionRoutes(admin, &handler.Handlers{
			Admin: &handler.AdminHandlers{
				Subscription: &adminhandler.SubscriptionHandler{},
			},
		})
	})
}
