package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type CompatibleGatewayHandler struct {
	compatibleService *service.CompatibleGatewayService
	base              *GatewayHandler
}

func NewCompatibleGatewayHandler(compatibleService *service.CompatibleGatewayService, base *GatewayHandler) *CompatibleGatewayHandler {
	return &CompatibleGatewayHandler{
		compatibleService: compatibleService,
		base:              base,
	}
}

func (h *CompatibleGatewayHandler) Messages(c *gin.Context) {
	h.forward(c, service.CompatibleRouteMessages)
}

func (h *CompatibleGatewayHandler) Responses(c *gin.Context) {
	if path := strings.TrimSpace(c.Request.URL.Path); strings.Contains(path, "/responses/") {
		h.writeRouteError(c, service.CompatibleRouteResponses, http.StatusBadRequest, "invalid_request_error", "Responses subpaths are not supported for this platform", false)
		return
	}
	h.forward(c, service.CompatibleRouteResponses)
}

func (h *CompatibleGatewayHandler) ChatCompletions(c *gin.Context) {
	h.forward(c, service.CompatibleRouteChatCompletions)
}

func (h *CompatibleGatewayHandler) Models(c *gin.Context) {
	apiKey, _ := middleware2.GetAPIKeyFromContext(c)
	var groupID *int64
	platform := ""
	if apiKey != nil && apiKey.Group != nil {
		groupID = &apiKey.Group.ID
		platform = apiKey.Group.Platform
	}
	if !service.IsCompatiblePlatform(platform) {
		h.base.Models(c)
		return
	}
	availableModels := h.base.gatewayService.GetAvailableModels(c.Request.Context(), groupID, "")
	if len(availableModels) > 0 {
		models := make([]claude.Model, 0, len(availableModels))
		for _, modelID := range availableModels {
			models = append(models, claude.Model{
				ID:          modelID,
				Type:        "model",
				DisplayName: modelID,
				CreatedAt:   "",
			})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   h.compatibleService.DefaultModels(platform),
	})
}

func (h *CompatibleGatewayHandler) CountTokens(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "authentication_error",
				"message": "Invalid API key",
			},
		})
		return
	}
	if apiKey.Group == nil || !service.IsCompatiblePlatform(apiKey.Group.Platform) {
		h.base.CountTokens(c)
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil || len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": "Failed to read request body",
			},
		})
		return
	}

	parsed, err := service.ParseGatewayRequest(body, domain.PlatformAnthropic)
	if err != nil || strings.TrimSpace(parsed.Model) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": "Failed to parse request body",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"input_tokens": service.EstimateCompatibleInputTokens(parsed),
	})
}

func (h *CompatibleGatewayHandler) forward(c *gin.Context, route service.CompatibleRequestRoute) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.writeRouteError(c, route, http.StatusUnauthorized, "authentication_error", "Invalid API key", false)
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.writeRouteError(c, route, http.StatusInternalServerError, "api_error", "User context not found", false)
		return
	}
	if apiKey.Group == nil || !service.IsCompatiblePlatform(apiKey.Group.Platform) {
		h.writeRouteError(c, route, http.StatusBadRequest, "invalid_request_error", "Incompatible group platform", false)
		return
	}

	reqLog := requestLogger(
		c,
		"handler.compatible_gateway."+string(route),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("platform", apiKey.Group.Platform),
	)

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.writeRouteError(c, route, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit), false)
			return
		}
		h.writeRouteError(c, route, http.StatusBadRequest, "invalid_request_error", "Failed to read request body", false)
		return
	}

	parsed, err := parseCompatibleParsedRequest(body, route)
	if err != nil {
		h.writeRouteError(c, route, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body", false)
		return
	}
	if parsed.Model == "" {
		h.writeRouteError(c, route, http.StatusBadRequest, "invalid_request_error", "model is required", false)
		return
	}

	setOpsRequestContext(c, parsed.Model, parsed.Stream, body)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsed.Stream, false)))

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	if err := h.base.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.writeRouteError(c, route, status, code, message, false)
		return
	}

	parsed.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.base.gatewayService.GenerateSessionHash(parsed)

	maxWait := service.CalculateMaxWait(subject.Concurrency)
	waitCounted := false
	canWait, err := h.base.concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	if err == nil && canWait {
		waitCounted = true
	}
	if err == nil && !canWait {
		h.writeRouteError(c, route, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", false)
		return
	}
	defer func() {
		if waitCounted {
			h.base.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	streamStarted := false
	userReleaseFunc, err := h.base.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, parsed.Stream, &streamStarted)
	if err != nil {
		h.writeRouteError(c, route, http.StatusTooManyRequests, "rate_limit_error", fmt.Sprintf("Concurrency limit exceeded for %s, please retry later", "user"), streamStarted)
		return
	}
	if waitCounted {
		h.base.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	defer func() {
		if userReleaseFunc != nil {
			userReleaseFunc()
		}
	}()

	fs := NewFailoverState(h.base.maxAccountSwitches, false)
	for {
		selection, err := h.base.gatewayService.SelectAccountWithLoadAwareness(
			c.Request.Context(),
			apiKey.GroupID,
			sessionHash,
			parsed.Model,
			fs.FailedAccountIDs,
			parsed.MetadataUserID,
			subject.UserID,
		)
		if err != nil {
			if len(fs.FailedAccountIDs) == 0 {
				h.writeRouteError(c, route, http.StatusServiceUnavailable, "api_error", "No available accounts: "+err.Error(), streamStarted)
				return
			}
			switch fs.HandleSelectionExhausted(c.Request.Context()) {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				return
			default:
				h.writeFailoverError(c, route, fs.LastFailoverErr, 502, streamStarted, apiKey.Group.Platform)
				return
			}
		}

		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				h.writeRouteError(c, route, http.StatusServiceUnavailable, "api_error", "No available accounts", streamStarted)
				return
			}
			accountWaitCounted := false
			canWait, err := h.base.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
			if err == nil && canWait {
				accountWaitCounted = true
			}
			if err == nil && !canWait {
				h.writeRouteError(c, route, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later", streamStarted)
				return
			}
			releaseWait := func() {
				if accountWaitCounted {
					h.base.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
					accountWaitCounted = false
				}
			}

			accountReleaseFunc, err = h.base.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				selection.WaitPlan.Timeout,
				parsed.Stream,
				&streamStarted,
			)
			if err != nil {
				releaseWait()
				h.writeRouteError(c, route, http.StatusTooManyRequests, "rate_limit_error", "Concurrency limit exceeded for account, please retry later", streamStarted)
				return
			}
			releaseWait()
			if err := h.base.gatewayService.BindStickySession(c.Request.Context(), apiKey.GroupID, sessionHash, account.ID); err != nil {
				reqLog.Warn("compatible.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		writerSizeBefore := c.Writer.Size()
		result, upstreamEndpoint, err := h.compatibleService.Forward(c.Request.Context(), c, account, route, body)
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBefore {
					h.writeFailoverError(c, route, failoverErr, failoverErr.StatusCode, true, account.Platform)
					return
				}
				switch fs.HandleFailoverError(c.Request.Context(), h.compatibleService, account.ID, account.Platform, failoverErr) {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					return
				default:
					h.writeFailoverError(c, route, fs.LastFailoverErr, failoverErr.StatusCode, streamStarted, account.Platform)
					return
				}
			}

			var upstreamErr *service.CompatibleUpstreamError
			if errors.As(err, &upstreamErr) {
				if h.base.errorPassthroughService != nil && len(upstreamErr.ResponseBody) > 0 {
					if rule := h.base.errorPassthroughService.MatchRule(account.Platform, upstreamErr.StatusCode, upstreamErr.ResponseBody); rule != nil {
						respCode := upstreamErr.StatusCode
						if !rule.PassthroughCode && rule.ResponseCode != nil {
							respCode = *rule.ResponseCode
						}
						msg := upstreamErr.Message
						if !rule.PassthroughBody && rule.CustomMessage != nil {
							msg = *rule.CustomMessage
						}
						h.writeRouteError(c, route, respCode, "upstream_error", msg, streamStarted)
						return
					}
				}
				h.writeRouteError(c, route, upstreamErr.StatusCode, "upstream_error", upstreamErr.Message, streamStarted)
				return
			}

			h.writeRouteError(c, route, http.StatusBadGateway, "upstream_error", err.Error(), streamStarted)
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		h.base.submitUsageRecordTask(func(ctx context.Context) {
			if err := h.base.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				ParsedRequest:      parsed,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.base.apiKeyService,
			}); err != nil {
				reqLog.Error("compatible.record_usage_failed", zap.Error(err), zap.Int64("account_id", account.ID))
			}
		})
		return
	}
}

func parseCompatibleParsedRequest(body []byte, route service.CompatibleRequestRoute) (*service.ParsedRequest, error) {
	switch route {
	case service.CompatibleRouteMessages:
		return service.ParseGatewayRequest(body, domain.PlatformAnthropic)
	case service.CompatibleRouteResponses:
		var req apicompat.ResponsesRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, err
		}
		anthropicReq, err := apicompat.ResponsesToAnthropicRequest(&req)
		if err != nil {
			return nil, err
		}
		anthropicBody, err := json.Marshal(anthropicReq)
		if err != nil {
			return nil, err
		}
		return service.ParseGatewayRequest(anthropicBody, domain.PlatformAnthropic)
	default:
		return service.ParseGatewayRequest(body, domain.PlatformOpenAI)
	}
}

func (h *CompatibleGatewayHandler) writeFailoverError(c *gin.Context, route service.CompatibleRequestRoute, failoverErr *service.UpstreamFailoverError, fallbackStatus int, streamStarted bool, platform string) {
	if failoverErr == nil {
		h.writeRouteError(c, route, http.StatusBadGateway, "upstream_error", "Upstream request failed", streamStarted)
		return
	}
	statusCode := failoverErr.StatusCode
	responseBody := failoverErr.ResponseBody
	msg := service.ExtractUpstreamErrorMessage(responseBody)
	if msg == "" {
		_, _, msg = h.base.mapUpstreamError(statusCode)
	}
	if h.base.errorPassthroughService != nil && len(responseBody) > 0 {
		if rule := h.base.errorPassthroughService.MatchRule(platform, statusCode, responseBody); rule != nil {
			respCode := statusCode
			if !rule.PassthroughCode && rule.ResponseCode != nil {
				respCode = *rule.ResponseCode
			}
			if !rule.PassthroughBody && rule.CustomMessage != nil {
				msg = *rule.CustomMessage
			}
			h.writeRouteError(c, route, respCode, "upstream_error", msg, streamStarted)
			return
		}
	}
	status, errType, errMsg := h.base.mapUpstreamError(statusCode)
	if msg != "" {
		errMsg = msg
	}
	h.writeRouteError(c, route, status, errType, errMsg, streamStarted)
}

func (h *CompatibleGatewayHandler) writeRouteError(c *gin.Context, route service.CompatibleRequestRoute, status int, errType, message string, streamStarted bool) {
	switch route {
	case service.CompatibleRouteResponses:
		if streamStarted {
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", `{"error":{"code":"`+errType+`","message":`+strconv.Quote(message)+`}}`); err == nil {
				c.Writer.Flush()
			}
			return
		}
		c.JSON(status, gin.H{"error": gin.H{"code": errType, "message": message}})
	case service.CompatibleRouteChatCompletions:
		if streamStarted {
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", `{"error":{"type":"`+errType+`","message":`+strconv.Quote(message)+`}}`); err == nil {
				c.Writer.Flush()
			}
			return
		}
		c.JSON(status, gin.H{"error": gin.H{"type": errType, "message": message}})
	default:
		h.base.handleStreamingAwareError(c, status, errType, message, streamStarted)
	}
}
