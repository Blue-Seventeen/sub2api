package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterPromotionRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	adminAuth middleware.AdminAuthMiddleware,
	settingService *service.SettingService,
) {
	if h == nil || h.Promotion == nil || h.Admin == nil || h.Admin.Promotion == nil {
		return
	}

	publicPromotion := v1.Group("/promotion/public")
	{
		publicPromotion.GET("/referrers/:invite_code", h.Promotion.PreviewReferrer)
	}

	authenticated := v1.Group("/promotion")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	{
		authenticated.POST("/me/bind-referrer", h.Promotion.BindReferrer)
		authenticated.GET("/me/overview", h.Promotion.GetOverview)
		authenticated.GET("/me/team", h.Promotion.ListTeam)
		authenticated.GET("/me/earnings", h.Promotion.ListEarnings)
		authenticated.GET("/me/scripts", h.Promotion.ListScripts)
		authenticated.POST("/me/scripts/:id/use", h.Promotion.MarkScriptUsed)
	}

	adminPromotion := v1.Group("/admin/promotion")
	adminPromotion.Use(gin.HandlerFunc(adminAuth))
	{
		adminPromotion.GET("/dashboard", h.Admin.Promotion.GetDashboard)
		adminPromotion.GET("/relations", h.Admin.Promotion.ListRelations)
		adminPromotion.GET("/relations/:user_id/chain", h.Admin.Promotion.GetRelationChain)
		adminPromotion.GET("/relations/:user_id/downlines", h.Admin.Promotion.ListDownlines)
		adminPromotion.DELETE("/relations/:user_id/downlines/:downline_user_id", h.Admin.Promotion.RemoveDirectDownline)
		adminPromotion.POST("/relations/bind-parent", h.Admin.Promotion.BindParent)
		adminPromotion.DELETE("/relations/:user_id/parent", h.Admin.Promotion.RemoveParent)

		adminPromotion.GET("/commissions", h.Admin.Promotion.ListCommissions)
		adminPromotion.POST("/commissions/manual-grant", h.Admin.Promotion.ManualGrantCommission)
		adminPromotion.PUT("/commissions/:id", h.Admin.Promotion.UpdateCommission)
		adminPromotion.POST("/commissions/:id/settle", h.Admin.Promotion.SettleCommission)
		adminPromotion.POST("/commissions/batch-settle", h.Admin.Promotion.BatchSettleCommissions)
		adminPromotion.POST("/commissions/:id/cancel", h.Admin.Promotion.CancelCommission)

		adminPromotion.GET("/config", h.Admin.Promotion.GetConfig)
		adminPromotion.PUT("/config", h.Admin.Promotion.UpdateConfig)

		adminPromotion.GET("/scripts", h.Admin.Promotion.ListScripts)
		adminPromotion.POST("/scripts", h.Admin.Promotion.CreateScript)
		adminPromotion.PUT("/scripts/:id", h.Admin.Promotion.UpdateScript)
		adminPromotion.DELETE("/scripts/:id", h.Admin.Promotion.DeleteScript)
	}
}
