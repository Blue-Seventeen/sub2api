package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func groupModelsListDisallowedMessage(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "model is not allowed by group models list"
	}
	return fmt.Sprintf("model %s is not allowed by group models list", model)
}

func groupAllowsRequestedModel(group *service.Group, model string) bool {
	return service.GroupAllowsRequestedModel(group, model)
}

func groupAllowsClientRequestedModel(c *gin.Context, group *service.Group, fallbackModel string) (string, bool) {
	model := clientRequestedModel(c, fallbackModel)
	return model, groupAllowsRequestedModel(group, model)
}

func newAPIStyleGroupModelsListModel(route service.NewAPIStyleRoute, model, method, path string) (string, bool) {
	model = strings.TrimSpace(model)
	if model != "" {
		return model, true
	}

	if !strings.EqualFold(strings.TrimSpace(method), http.MethodPost) {
		return "", false
	}
	path = strings.ToLower(strings.TrimSpace(path))
	switch route {
	case service.NewAPIStyleRouteSuno:
		if strings.Contains(path, "/submit/lyrics") {
			return "suno_lyrics", true
		}
		if strings.Contains(path, "/submit/music") || strings.Contains(path, "/suno/submit") {
			return "suno_music", true
		}
	case service.NewAPIStyleRouteMidjourney:
		if strings.Contains(path, "/mj/submit") {
			return "midjourney", true
		}
	case service.NewAPIStyleRouteKling:
		if strings.Contains(path, "/kling/v1/videos/text2video") {
			return "kling-v1", true
		}
	}
	return "", false
}
