//go:build embed

package server

import (
	"log"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
)

func setupEmbeddedFrontend(r *gin.Engine, settingService *service.SettingService, refreshFrameOrigins func()) {
	frontendServer, err := web.NewFrontendServer(settingService)
	if err != nil {
		log.Printf("Warning: Failed to create frontend server with settings injection: %v, using legacy mode", err)
		r.Use(web.ServeEmbeddedFrontend())
		settingService.SetOnUpdateCallback(refreshFrameOrigins)
		return
	}

	settingService.SetOnUpdateCallback(func() {
		frontendServer.InvalidateCache()
		refreshFrameOrigins()
	})
	r.Use(frontendServer.Middleware())
}
