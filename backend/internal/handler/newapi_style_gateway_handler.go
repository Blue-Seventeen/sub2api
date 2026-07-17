package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type NewAPIStyleGatewayHandler struct {
	base *GatewayHandler
}

func NewNewAPIStyleGatewayHandler(base *GatewayHandler) *NewAPIStyleGatewayHandler {
	return &NewAPIStyleGatewayHandler{base: base}
}

func (h *NewAPIStyleGatewayHandler) Images(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteImages)
}
func (h *NewAPIStyleGatewayHandler) Audio(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteAudio)
}
func (h *NewAPIStyleGatewayHandler) QwenMultimodalGeneration(c *gin.Context) {
	apiKey, _ := middleware2.GetAPIKeyFromContext(c)
	if apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformAli {
		h.writeError(c, http.StatusForbidden, "permission_error", "The DashScope multimodal official path alias is only available for Qwen/DashScope groups")
		return
	}
	model, _, err := peekNewAPIStyleRequestModel(c)
	if err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if strings.TrimSpace(model) == "" {
		h.writeError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if service.IsAliQwenImageModel(model) {
		h.forward(c, service.NewAPIStyleRouteQwenImage)
		return
	}
	h.forward(c, service.NewAPIStyleRouteQwenTTS)
}
func (h *NewAPIStyleGatewayHandler) QwenTTS(c *gin.Context) {
	h.QwenMultimodalGeneration(c)
}
func (h *NewAPIStyleGatewayHandler) Embeddings(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteEmbeddings)
}
func (h *NewAPIStyleGatewayHandler) Rerank(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteRerank)
}
func (h *NewAPIStyleGatewayHandler) Videos(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteVideo)
}
func (h *NewAPIStyleGatewayHandler) VideoGenerations(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteVideo)
}
func (h *NewAPIStyleGatewayHandler) Suno(c *gin.Context) { h.forward(c, service.NewAPIStyleRouteSuno) }
func (h *NewAPIStyleGatewayHandler) Kling(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteKling)
}
func (h *NewAPIStyleGatewayHandler) Midjourney(c *gin.Context) {
	h.forward(c, service.NewAPIStyleRouteMidjourney)
}
func (h *NewAPIStyleGatewayHandler) Task(c *gin.Context) { h.forward(c, service.NewAPIStyleRouteTask) }

func (h *NewAPIStyleGatewayHandler) forward(c *gin.Context, route service.NewAPIStyleRoute) {
	if h == nil || h.base == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "api_error", "message": "Service temporarily unavailable"}})
		return
	}

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.writeError(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.writeError(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}

	reqLog := requestLogger(
		c,
		"handler.newapi_style."+string(route),
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	var body []byte
	if requestCanHaveBody(c.Request.Method) {
		var err error
		body, err = readRequestBodyWithObservability(c, reqLog)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				h.writeError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
				return
			}
			h.writeError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
			return
		}
	}
	if len(body) == 0 && methodRequiresRequestBody(c.Request.Method) {
		h.writeError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	model := service.ExtractNewAPIStyleModel(body, c.GetHeader("Content-Type"))
	if (route == service.NewAPIStyleRouteImages || route == service.NewAPIStyleRouteQwenTTS || route == service.NewAPIStyleRouteQwenImage) && strings.TrimSpace(model) == "" {
		h.writeError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	stream := gjson.GetBytes(body, "stream").Bool()
	setOpsRequestContext(c, model, stream)
	setOpsEndpointContext(c, model, int16(service.RequestTypeFromLegacy(stream, false)))
	if whitelistModel, ok := newAPIStyleGroupModelsListModel(route, model, c.Request.Method, c.Request.URL.Path); ok {
		if !groupAllowsRequestedModel(apiKey.Group, whitelistModel) {
			h.writeError(c, http.StatusForbidden, "permission_error", groupModelsListDisallowedMessage(whitelistModel))
			return
		}
	}
	if decision := h.base.checkSecurityAudit(c, reqLog, apiKey, subject, contentModerationProtocolForNewAPIStyleRoute(route), model, body); decision != nil && !decision.AllowNextStage {
		h.writeError(c, securityAuditStatus(decision), securityAuditErrorCode(decision), securityAuditMessage(decision))
		return
	}
	channelMapping, _ := h.base.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, model)
	forwardModel := model
	if channelMapping.Mapped {
		forwardModel = channelMapping.MappedModel
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	parsedReq, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), "")
	if err != nil {
		parsedReq = &service.ParsedRequest{
			Model:  model,
			Stream: stream,
			Body:   service.NewRequestBodyRef(body),
		}
	}
	if parsedReq.Model == "" {
		parsedReq.Model = model
	}
	parsedReq.Stream = stream
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.base.gatewayService.GenerateSessionHash(parsedReq)
	if sessionHash == "" {
		sessionHash = strings.TrimSpace(c.GetHeader("X-Request-Id"))
	}

	maxWait := service.CalculateMaxWait(subject.Concurrency)
	canWait, err := h.base.concurrencyHelper.IncrementWaitCount(c.Request.Context(), subject.UserID, maxWait)
	waitCounted := false
	if err != nil {
		reqLog.Warn("newapi.user_wait_counter_increment_failed", zap.Error(err))
	} else if !canWait {
		h.writeError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
		return
	}
	if err == nil && canWait {
		waitCounted = true
	}
	defer func() {
		if waitCounted {
			h.base.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		}
	}()

	streamStarted := false
	userReleaseFunc, err := h.base.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, stream, &streamStarted)
	if err != nil {
		reqLog.Warn("newapi.user_slot_acquire_failed", zap.Error(err))
		h.base.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	if waitCounted {
		h.base.concurrencyHelper.DecrementWaitCount(c.Request.Context(), subject.UserID)
		waitCounted = false
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	if err := h.base.billingCacheService.CheckBillingEligibilityFreshSubscription(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, quotaPlatform); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
		}
		h.base.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	fs := NewFailoverState(h.base.maxAccountSwitches, false)
	unsupportedCapabilitySeen := false
	for {
		selection, err := h.base.gatewayService.SelectAccountWithLoadAwareness(
			c.Request.Context(),
			apiKey.GroupID,
			sessionHash,
			model,
			fs.FailedAccountIDs,
			"",
			subject.UserID,
		)
		if err != nil {
			if unsupportedCapabilitySeen {
				h.writeError(c, http.StatusBadRequest, "unsupported_capability", "No selected account supports this endpoint")
				return
			}
			if len(fs.FailedAccountIDs) == 0 {
				h.writeError(c, http.StatusServiceUnavailable, "api_error", "No available accounts: "+err.Error())
				return
			}
			action := fs.HandleSelectionExhausted(c.Request.Context())
			switch action {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				return
			default:
				if fs.LastFailoverErr != nil {
					h.base.handleFailoverExhaustedSimple(c, fs.LastFailoverErr.StatusCode, streamStarted)
				} else {
					h.writeError(c, http.StatusBadGateway, "upstream_error", "No selected account supports this endpoint")
				}
				return
			}
		}
		account := selection.Account
		if account == nil {
			h.writeError(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)
		if !h.base.newAPIStyleService.SupportsForGroup(account, apiKey.Group, route) {
			unsupportedCapabilitySeen = true
			fs.FailedAccountIDs[account.ID] = struct{}{}
			if selection.Acquired && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			continue
		}

		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				h.writeError(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
				return
			}
			accountWaitCounted := false
			canWait, err := h.base.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
			if err != nil {
				reqLog.Warn("newapi.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			} else if !canWait {
				h.writeError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many pending requests, please retry later")
				return
			}
			if err == nil && canWait {
				accountWaitCounted = true
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
				stream,
				&streamStarted,
			)
			if err != nil {
				releaseWait()
				h.base.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
			releaseWait()
			if err := h.base.gatewayService.BindStickySession(c.Request.Context(), apiKey.GroupID, sessionHash, account.ID); err != nil {
				reqLog.Warn("newapi.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		writerSizeBefore := c.Writer.Size()
		activeUsageHandle := beginProxyActiveUsage(h.base.proxyActiveUsageTracker, account)
		result, upstreamEndpoint, err := h.base.newAPIStyleService.Forward(
			c.Request.Context(),
			c,
			account,
			service.NewAPIStyleForwardOptions{
				Route:              route,
				Group:              apiKey.Group,
				RequestBody:        body,
				Stream:             stream,
				Model:              model,
				ChannelMappedModel: forwardModel,
				Method:             c.Request.Method,
				InboundPath:        c.Request.URL.Path,
				QueryString:        c.Request.URL.RawQuery,
				ContentType:        c.GetHeader("Content-Type"),
				HeaderSource:       c.Request.Header,
			},
		)
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
				switch fs.HandleFailoverError(c.Request.Context(), h.base.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr) {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					return
				default:
					h.writeFailoverError(c, route, fs.LastFailoverErr, failoverErr.StatusCode, streamStarted, account.Platform)
					return
				}
			}
			if errors.Is(err, service.ErrNewAPIStyleUnsupportedCapability) {
				unsupportedCapabilitySeen = true
				fs.FailedAccountIDs[account.ID] = struct{}{}
				continue
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
			responseStarted := streamStarted || c.Writer.Size() != writerSizeBefore
			h.writeRouteError(c, route, http.StatusBadGateway, "upstream_error", service.SanitizeUserVisibleErrorText(err.Error()), responseStarted)
			return
		}

		if result != nil && result.SkipUsageBilling {
			logger.L().With(
				zap.String("component", "handler.newapi_style"),
				zap.Int64("user_id", subject.UserID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.Int64("account_id", account.ID),
			).Warn("newapi.skip_usage_billing_after_abnormal_stream")
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		if upstreamEndpoint == "" {
			upstreamEndpoint = GetUpstreamEndpoint(c, account.Platform)
		}
		compat := compatibilityLogFields(c)
		compatibleInputTokens := service.EstimateCompatibleInputTokensForPlatform(account.Platform, parsedReq)

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
				RequestPayloadHash:    requestPayloadHash,
				ClientProfile:         compat.ClientProfile,
				CompatibilityRoute:    compat.CompatibilityRoute,
				FallbackChain:         compat.FallbackChain,
				UpstreamTransport:     compat.UpstreamTransport,
				APIKeyService:         h.base.apiKeyService,
				ChannelUsageFields:    channelMapping.ToUsageFields(model, result.UpstreamModel),
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.newapi_style"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.Int64("account_id", account.ID),
				).Error("newapi.record_usage_failed", zap.Error(err))
			}
		})
		return
	}
}

func (h *NewAPIStyleGatewayHandler) writeError(c *gin.Context, status int, errType, message string) {
	if h == nil || h.base == nil {
		c.JSON(status, gin.H{"error": gin.H{"type": errType, "message": message}})
		return
	}
	h.base.handleStreamingAwareError(c, status, errType, message, false)
}

func contentModerationProtocolForNewAPIStyleRoute(route service.NewAPIStyleRoute) string {
	switch route {
	case service.NewAPIStyleRouteMessages:
		return service.ContentModerationProtocolAnthropicMessages
	case service.NewAPIStyleRouteResponses:
		return service.ContentModerationProtocolOpenAIResponses
	case service.NewAPIStyleRouteImages, service.NewAPIStyleRouteQwenImage:
		return service.ContentModerationProtocolOpenAIImages
	default:
		return service.ContentModerationProtocolOpenAIChat
	}
}

func (h *NewAPIStyleGatewayHandler) writeRouteError(c *gin.Context, _ service.NewAPIStyleRoute, status int, errType, message string, streamStarted bool) {
	if h == nil || h.base == nil {
		if streamStarted {
			return
		}
		c.JSON(status, gin.H{"error": gin.H{"type": errType, "message": message}})
		return
	}
	h.base.handleStreamingAwareError(c, status, errType, message, streamStarted)
}

func (h *NewAPIStyleGatewayHandler) writeFailoverError(c *gin.Context, route service.NewAPIStyleRoute, failoverErr *service.UpstreamFailoverError, fallbackStatus int, streamStarted bool, _ string) {
	if failoverErr == nil {
		h.writeRouteError(c, route, http.StatusBadGateway, "upstream_error", "Upstream request failed", streamStarted)
		return
	}
	status := failoverErr.StatusCode
	if status == 0 {
		status = fallbackStatus
	}
	if status == 0 {
		status = http.StatusBadGateway
	}
	message := service.SanitizeUserVisibleErrorText(service.ExtractUpstreamErrorMessage(failoverErr.ResponseBody))
	if strings.TrimSpace(message) == "" {
		message = "Upstream request failed"
	}
	h.writeRouteError(c, route, status, "upstream_error", message, streamStarted)
}

func requestCanHaveBody(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		return false
	default:
		return true
	}
}

func peekNewAPIStyleRequestModel(c *gin.Context) (string, []byte, error) {
	if c == nil || c.Request == nil {
		return "", nil, nil
	}
	body, err := readRequestBodyWithObservability(c, nil)
	if err != nil {
		return "", nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	return service.ExtractNewAPIStyleModel(body, c.GetHeader("Content-Type")), body, nil
}

func methodRequiresRequestBody(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}
