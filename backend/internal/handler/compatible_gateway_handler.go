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

func (h *CompatibleGatewayHandler) QwenCompatibleModeChatCompletions(c *gin.Context) {
	apiKey, _ := middleware2.GetAPIKeyFromContext(c)
	if apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformAli {
		h.writeRouteError(c, service.CompatibleRouteChatCompletions, http.StatusForbidden, "permission_error", "The /compatible-mode/v1/chat/completions alias is only available for Qwen/DashScope groups", false)
		return
	}
	h.ChatCompletions(c)
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
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.CustomModelsListEnabled() {
		availableModels = filterModelsByCustomList(availableModels, defaultModelIDsForPlatform(platform), apiKey.Group.ModelsListConfig.Models)
		writeCustomModelsList(c, platform, availableModels)
		return
	}
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

	body, err := readRequestBodyWithObservability(c, nil)
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

	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), domain.PlatformAnthropic)
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
	if !groupAllowsRequestedModel(apiKey.Group, parsed.Model) {
		h.base.errorResponse(c, http.StatusForbidden, "permission_error", groupModelsListDisallowedMessage(parsed.Model))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"input_tokens": service.EstimateCompatibleInputTokensForPlatform(apiKey.Group.Platform, parsed),
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

	body, err := readRequestBodyWithObservability(c, reqLog)
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
	setCompatibilityForCompatibleRoute(c, route, body, parsed)

	setOpsRequestContext(c, parsed.Model, parsed.Stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsed.Stream, false)))
	if !groupAllowsRequestedModel(apiKey.Group, parsed.Model) {
		h.writeRouteError(c, route, http.StatusForbidden, "permission_error", groupModelsListDisallowedMessage(parsed.Model), false)
		return
	}
	if decision := h.base.checkSecurityAudit(c, reqLog, apiKey, subject, contentModerationProtocolForCompatibleRoute(route), parsed.Model, body); decision != nil && !decision.AllowNextStage {
		h.writeRouteError(c, route, securityAuditStatus(decision), securityAuditErrorCode(decision), securityAuditMessage(decision), false)
		return
	}
	channelMapping, _ := h.base.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, parsed.Model)
	forwardBody := body
	forwardModel := parsed.Model
	if channelMapping.Mapped {
		forwardBody = h.base.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		forwardModel = channelMapping.MappedModel
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	parsed.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.base.gatewayService.GenerateSessionHash(parsed)
	repairToolNames := extractCompatibleAnthropicToolNames(body, route)

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

	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	if err := h.base.billingCacheService.CheckBillingEligibilityFreshSubscription(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, quotaPlatform); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.writeRouteError(c, route, status, code, message, streamStarted)
		return
	}

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
		if route == service.CompatibleRouteMessages {
			service.SetClaudeKimiToolRestoreContext(c, service.ClaudeKimiToolRestoreContext{
				Enabled:         service.GetCompatibilityClientProfile(c) == service.ClientProfileClaudeCode && service.GetCompatibilityInboundProtocol(c) == service.InboundProtocolAnthropicMessages && account != nil && account.Platform == service.PlatformMoonshot && account.GetExtraBool("compat_claude_kimi_tool_restore"),
				GroupID:         compatibleGroupIDValue(apiKey.GroupID),
				SessionHash:     sessionHash,
				AccountID:       account.ID,
				Platform:        account.Platform,
				ClientProfile:   service.GetCompatibilityClientProfile(c),
				InboundProtocol: service.GetCompatibilityInboundProtocol(c),
				ToolNames:       repairToolNames,
			})
		}

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
		var result *service.ForwardResult
		var upstreamEndpoint string
		activeUsageHandle := beginProxyActiveUsage(h.base.proxyActiveUsageTracker, account)
		if h.base.newAPIStyleService != nil && account.UseNewAPIStyleInterfaceForGroup(apiKey.Group) {
			var newAPIRoute service.NewAPIStyleRoute
			switch route {
			case service.CompatibleRouteMessages:
				newAPIRoute = service.NewAPIStyleRouteMessages
			case service.CompatibleRouteChatCompletions:
				newAPIRoute = service.NewAPIStyleRouteChatCompletions
			case service.CompatibleRouteResponses:
				newAPIRoute = service.NewAPIStyleRouteResponses
			}
			if h.base.newAPIStyleService.SupportsForGroup(account, apiKey.Group, newAPIRoute) {
				result, upstreamEndpoint, err = h.base.newAPIStyleService.Forward(
					c.Request.Context(),
					c,
					account,
					service.NewAPIStyleForwardOptions{
						Route:              newAPIRoute,
						Group:              apiKey.Group,
						RequestBody:        forwardBody,
						Stream:             parsed.Stream,
						Model:              parsed.Model,
						ChannelMappedModel: forwardModel,
						Method:             http.MethodPost,
						InboundPath:        c.Request.URL.Path,
						QueryString:        c.Request.URL.RawQuery,
						ContentType:        c.GetHeader("Content-Type"),
						HeaderSource:       c.Request.Header,
					},
				)
			} else {
				result, upstreamEndpoint, err = h.compatibleService.Forward(c.Request.Context(), c, account, route, forwardBody)
			}
		} else {
			result, upstreamEndpoint, err = h.compatibleService.Forward(c.Request.Context(), c, account, route, forwardBody)
		}
		endProxyActiveUsage(activeUsageHandle)
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
				switch fs.HandleFailoverError(c.Request.Context(), h.compatibleService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr) {
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
				msg := sanitizeCompatibleUpstreamClientMessage(upstreamErr.Message)
				if msg == "" {
					msg = "Upstream request failed"
				}
				if h.base.errorPassthroughService != nil && len(upstreamErr.ResponseBody) > 0 {
					if rule := h.base.errorPassthroughService.MatchRule(account.Platform, upstreamErr.StatusCode, upstreamErr.ResponseBody); rule != nil {
						respCode := upstreamErr.StatusCode
						if !rule.PassthroughCode && rule.ResponseCode != nil {
							respCode = *rule.ResponseCode
						}
						if !rule.PassthroughBody && rule.CustomMessage != nil {
							msg = service.SanitizeUserVisibleErrorText(*rule.CustomMessage)
						}
						h.writeRouteError(c, route, respCode, "upstream_error", msg, streamStarted)
						return
					}
				}
				h.writeRouteError(c, route, upstreamErr.StatusCode, "upstream_error", msg, streamStarted)
				return
			}

			var clientErr *service.CompatibleClientError
			if errors.As(err, &clientErr) {
				status := clientErr.StatusCode
				if status == 0 {
					status = http.StatusBadRequest
				}
				errType := clientErr.ErrorType
				if strings.TrimSpace(errType) == "" {
					errType = "invalid_request_error"
				}
				h.writeRouteError(c, route, status, errType, clientErr.Message, streamStarted)
				return
			}

			h.writeRouteError(c, route, http.StatusBadGateway, "upstream_error", sanitizeCompatibleUpstreamClientMessage(err.Error()), streamStarted)
			return
		}

		if result != nil && result.SkipUsageBilling {
			reqLog.Warn("compatible.skip_usage_billing_after_abnormal_stream", zap.Int64("account_id", account.ID))
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		compat := compatibilityLogFields(c)
		compatibleInputTokens := service.EstimateCompatibleInputTokensForPlatform(account.Platform, parsed)
		h.base.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.base.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:                result,
				CompatibleInputTokens: compatibleInputTokens,
				APIKey:                apiKey,
				User:                  apiKey.User,
				Account:               account,
				Subscription:          subscription,
				QuotaPlatform:         quotaPlatform,
				InboundEndpoint:       inboundEndpoint,
				UpstreamEndpoint:      upstreamEndpoint,
				UserAgent:             userAgent,
				IPAddress:             clientIP,
				ClientProfile:         compat.ClientProfile,
				CompatibilityRoute:    compat.CompatibilityRoute,
				FallbackChain:         compat.FallbackChain,
				UpstreamTransport:     compat.UpstreamTransport,
				RequestPayloadHash:    requestPayloadHash,
				APIKeyService:         h.base.apiKeyService,
				ChannelUsageFields:    channelMapping.ToUsageFields(parsed.Model, result.UpstreamModel),
			}); err != nil {
				reqLog.Error("compatible.record_usage_failed", zap.Error(err), zap.Int64("account_id", account.ID))
			}
		})
		return
	}
}

func contentModerationProtocolForCompatibleRoute(route service.CompatibleRequestRoute) string {
	switch route {
	case service.CompatibleRouteMessages:
		return service.ContentModerationProtocolAnthropicMessages
	case service.CompatibleRouteResponses:
		return service.ContentModerationProtocolOpenAIResponses
	default:
		return service.ContentModerationProtocolOpenAIChat
	}
}

func parseCompatibleParsedRequest(body []byte, route service.CompatibleRequestRoute) (*service.ParsedRequest, error) {
	switch route {
	case service.CompatibleRouteMessages:
		return service.ParseGatewayRequest(service.NewRequestBodyRef(body), domain.PlatformAnthropic)
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
		return service.ParseGatewayRequest(service.NewRequestBodyRef(anthropicBody), domain.PlatformAnthropic)
	default:
		return service.ParseGatewayRequest(service.NewRequestBodyRef(body), domain.PlatformOpenAI)
	}
}

func extractCompatibleAnthropicToolNames(body []byte, route service.CompatibleRequestRoute) []string {
	if route != service.CompatibleRouteMessages || len(body) == 0 {
		return nil
	}
	var req apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}
	out := make([]string, 0, len(req.Tools))
	seen := make(map[string]struct{}, len(req.Tools))
	for _, tool := range req.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

func compatibleGroupIDValue(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func (h *CompatibleGatewayHandler) writeFailoverError(c *gin.Context, route service.CompatibleRequestRoute, failoverErr *service.UpstreamFailoverError, fallbackStatus int, streamStarted bool, platform string) {
	if failoverErr == nil {
		h.writeRouteError(c, route, http.StatusBadGateway, "upstream_error", "Upstream request failed", streamStarted)
		return
	}
	statusCode := failoverErr.StatusCode
	responseBody := failoverErr.ResponseBody
	msg := sanitizeCompatibleUpstreamClientMessage(service.ExtractUpstreamErrorMessage(responseBody))
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
				msg = service.SanitizeUserVisibleErrorText(*rule.CustomMessage)
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

func sanitizeCompatibleUpstreamClientMessage(message string) string {
	return strings.TrimSpace(service.SanitizeUserVisibleErrorText(message))
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
