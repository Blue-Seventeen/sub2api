package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func openAIChannelForwardModel(mapping service.ChannelMappingResult, requestedModel string) string {
	if mapping.Mapped {
		if mapped := strings.TrimSpace(mapping.MappedModel); mapped != "" {
			return mapped
		}
	}
	return requestedModel
}

func openAIAccountScheduleModel(c *gin.Context, account *service.Account, forwardModel string, requireCompact bool, result *service.OpenAIForwardResult) string {
	if result != nil && strings.TrimSpace(result.UpstreamModel) != "" {
		return result.UpstreamModel
	}
	if c != nil {
		if value, ok := c.Get(service.OpsUpstreamModelKey); ok {
			if model, ok := value.(string); ok && strings.TrimSpace(model) != "" {
				return strings.TrimSpace(model)
			}
		}
	}
	return service.ResolveOpenAIAccountUpstreamModelForRequest(account, forwardModel, requireCompact)
}
