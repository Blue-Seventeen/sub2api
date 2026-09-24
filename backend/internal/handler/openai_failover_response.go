package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func openAICapacityFailoverClientResponse(failure *service.UpstreamFailoverError) (int, string, bool) {
	if failure == nil || !failure.IsOpenAICapacityShed() {
		return 0, "", false
	}
	message := strings.TrimSpace(failure.ClientMessage)
	if message == "" {
		message = "Upstream service is temporarily overloaded, please retry later"
	}
	// Apply gateway credential redaction before the user-facing network mask.
	body, _ := json.Marshal(gin.H{"error": gin.H{"message": message}})
	message = service.SanitizeUserVisibleErrorText(service.ExtractUpstreamErrorMessage(body))
	return http.StatusServiceUnavailable, message, true
}

// Preserve the deterministic model error after managed account retries without
// reflecting arbitrary provider fields, credentials, or routing diagnostics.
func (h *OpenAIGatewayHandler) tryWriteModelNotFoundFailover(c *gin.Context, failure *service.UpstreamFailoverError, streamStarted bool) bool {
	if failure.StatusCode != http.StatusBadRequest || !service.IsOpenAICompatibleModelNotFound400(failure.ResponseBody) {
		return false
	}
	message := service.SanitizeUserVisibleErrorText(service.ExtractUpstreamErrorMessage(failure.ResponseBody))
	if strings.TrimSpace(message) == "" {
		message = "The requested model is unavailable on this account"
	}
	service.SetOpsUpstreamError(c, failure.StatusCode, message, "")
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) || c.Writer.Written() {
		streamStarted = true
	}
	if streamStarted {
		h.handleStreamingAwareErrorWithCode(c, http.StatusBadRequest, "invalid_request_error", "model_not_found", message, true, true)
		return true
	}
	errBody := gin.H{"type": "invalid_request_error", "code": "model_not_found", "message": message}
	if gjson.GetBytes(failure.ResponseBody, "error.param").String() == "model" {
		errBody["param"] = "model"
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": errBody})
	return true
}
