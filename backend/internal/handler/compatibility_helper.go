package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func setCompatibilityForAnthropicMessages(c *gin.Context, body []byte, parsedReq *service.ParsedRequest) {
	if c == nil {
		return
	}
	service.SetCompatibilityInboundProtocol(c, service.InboundProtocolAnthropicMessages)
	SetClaudeCodeClientContext(c, body, parsedReq)
	if service.IsClaudeCodeClient(c.Request.Context()) {
		service.SetCompatibilityClientProfile(c, service.ClientProfileClaudeCode)
		return
	}
	service.SetCompatibilityClientProfile(c, service.ClientProfileGenericAnthropic)
}

func setCompatibilityForResponsesHTTP(c *gin.Context, forceCodex bool) {
	if c == nil {
		return
	}
	service.SetCompatibilityInboundProtocol(c, service.InboundProtocolOpenAIResponsesHTTP)
	if isCodexCompatibilityRequest(c, forceCodex) {
		service.SetCompatibilityClientProfile(c, service.ClientProfileCodex)
		return
	}
	service.SetCompatibilityClientProfile(c, service.ClientProfileGenericOpenAI)
}

func setCompatibilityForResponsesWS(c *gin.Context, forceCodex bool) {
	if c == nil {
		return
	}
	service.SetCompatibilityInboundProtocol(c, service.InboundProtocolOpenAIResponsesWS)
	if isCodexCompatibilityRequest(c, forceCodex) {
		service.SetCompatibilityClientProfile(c, service.ClientProfileCodex)
		return
	}
	service.SetCompatibilityClientProfile(c, service.ClientProfileGenericOpenAI)
}

func setCompatibilityForChatCompletions(c *gin.Context) {
	if c == nil {
		return
	}
	service.SetCompatibilityInboundProtocol(c, service.InboundProtocolOpenAIChatCompletions)
	if isCherryStudioCompatibilityRequest(c) {
		service.SetCompatibilityClientProfile(c, service.ClientProfileCherryStudio)
		return
	}
	service.SetCompatibilityClientProfile(c, service.ClientProfileGenericOpenAI)
}

func setCompatibilityForImages(c *gin.Context) {
	if c == nil {
		return
	}
	service.SetCompatibilityInboundProtocol(c, service.InboundProtocolOpenAIImages)
	if isCherryStudioCompatibilityRequest(c) {
		service.SetCompatibilityClientProfile(c, service.ClientProfileCherryStudio)
		return
	}
	service.SetCompatibilityClientProfile(c, service.ClientProfileGenericOpenAI)
}

func setCompatibilityForCompatibleRoute(c *gin.Context, route service.CompatibleRequestRoute, body []byte, parsedReq *service.ParsedRequest) {
	switch route {
	case service.CompatibleRouteMessages:
		setCompatibilityForAnthropicMessages(c, body, parsedReq)
	case service.CompatibleRouteResponses:
		setCompatibilityForResponsesHTTP(c, false)
	case service.CompatibleRouteChatCompletions:
		setCompatibilityForChatCompletions(c)
	}
}

func compatibilityLogFields(c *gin.Context) service.CompatibilityLogFields {
	return service.CompatibilityLogFieldsFromContext(c)
}

func isCodexCompatibilityRequest(c *gin.Context, forceCodex bool) bool {
	if c == nil {
		return forceCodex
	}
	if forceCodex {
		return true
	}
	userAgent := c.GetHeader("User-Agent")
	originator := c.GetHeader("originator")
	if openai.IsCodexOfficialClientByHeaders(userAgent, originator) {
		return true
	}
	if c.Request != nil && c.Request.URL != nil {
		return strings.Contains(strings.ToLower(strings.TrimSpace(c.Request.URL.Path)), "/backend-api/codex/")
	}
	return false
}

func isCherryStudioCompatibilityRequest(c *gin.Context) bool {
	if c == nil {
		return false
	}
	candidates := []string{
		c.GetHeader("User-Agent"),
		c.GetHeader("HTTP-Referer"),
		c.GetHeader("Origin"),
		c.GetHeader("Referer"),
		c.GetHeader("X-App"),
		c.GetHeader("X-Title"),
		c.GetHeader("X-Client-Name"),
		c.GetHeader("X-Requested-With"),
		c.GetHeader("Sec-CH-UA-Platform"),
	}
	for _, candidate := range candidates {
		if isCherryStudioHint(candidate) {
			return true
		}
	}
	return false
}

func isCherryStudioHint(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	return strings.Contains(value, "cherrystudio") ||
		strings.Contains(value, "cherry-studio") ||
		strings.Contains(value, "cherryai") ||
		strings.Contains(value, "cherry-ai")
}
