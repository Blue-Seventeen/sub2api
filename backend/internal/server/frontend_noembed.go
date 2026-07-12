//go:build !embed

package server

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func setupEmbeddedFrontend(_ *gin.Engine, settingService *service.SettingService, refreshFrameOrigins func()) {
	settingService.SetOnUpdateCallback(refreshFrameOrigins)
}
