package routes

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RegisterGatewayRoutes 注册 API 网关路由（Claude/OpenAI/Gemini 兼容）
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	compositeResolver *service.CompositeRouteResolver,
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	textBodyLimit := middleware.RequestBodyLimit(cfg.Gateway.TextMaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()
	compositeTarget := compositeTargetPlatformMiddleware(compositeResolver)
	compositeGeminiTarget := compositeGeminiTargetPlatformMiddleware(compositeResolver)

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)
	groupModelAllowlist := middleware.GroupModelAllowlist()
	withLegacyCNNewAPIGroup := func(c *gin.Context, next gin.HandlerFunc) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || apiKey.Group == nil ||
			!apiKey.Group.NewAPIStyleInterfaceEnabled ||
			(getGroupPlatform(c) != service.PlatformZhipu && getGroupPlatform(c) != service.PlatformDeepseek) ||
			service.IsGroupContextValid(apiKey.Group) {
			next(c)
			return
		}

		// Legacy auth snapshots may carry the fields needed for routing without
		// the hydration marker used by the service capability guard. Preserve the
		// original key and make the group valid only for this dispatch decision.
		group := *apiKey.Group
		group.Hydrated = true
		if !service.IsGroupContextValid(&group) {
			next(c)
			return
		}
		key := *apiKey
		key.Group = &group
		c.Set(string(middleware.ContextKeyAPIKey), &key)
		defer c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		next(c)
	}

	isOpenAIResponsesCompatibleGatewayPlatform := func(c *gin.Context) bool {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI, service.PlatformGrok,
			service.PlatformKimi, service.PlatformMiniMax:
			return true
		default:
			return false
		}
	}
	countTokensHandler := func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI, service.PlatformKimi, service.PlatformMiniMax:
			h.OpenAIGateway.CountTokens(c)
		case service.PlatformGrok:
			h.OpenAIGateway.GrokCountTokens(c)
		default:
			if service.IsCompatiblePlatform(getGroupPlatform(c)) {
				h.CompatibleGateway.CountTokens(c)
				return
			}
			h.Gateway.CountTokens(c)
		}
	}
	codexModelsHandler := func(c *gin.Context) {
		dispatchCodexModelsGateway(c, h.OpenAIGateway.CodexModels, h.Gateway.CodexModels)
	}
	modelsHandler := func(c *gin.Context) {
		if c.Query("client_version") != "" {
			codexModelsHandler(c)
			return
		}
		if service.IsCompatiblePlatform(getGroupPlatform(c)) {
			h.CompatibleGateway.Models(c)
			return
		}
		h.Gateway.Models(c)
	}
	imagesHandler := func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.Images(c)
		case service.PlatformGrok:
			h.OpenAIGateway.GrokImages(c)
		default:
			if c.FullPath() == "/v1/images/generations" || c.FullPath() == "/images/generations" {
				h.NewAPIStyleGateway.Images(c)
				return
			}
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			writeImagesUnsupported(c)
		}
	}
	embeddingsHandler := func(c *gin.Context) {
		platform := getGroupPlatform(c)
		switch platform {
		case service.PlatformOpenAI, service.PlatformKimi, service.PlatformMiniMax:
			h.OpenAIGateway.Embeddings(c)
			return
		}
		if service.IsCompatiblePlatform(platform) {
			h.NewAPIStyleGateway.Embeddings(c)
			return
		}
		writeEmbeddingsUnsupported(c)
	}
	videoGenerationHandler := func(c *gin.Context) {
		if platform := getGroupPlatform(c); platform == service.PlatformGrok || platform == service.PlatformComposite {
			h.OpenAIGateway.GrokVideoGeneration(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		writeVideosUnsupported(c)
	}
	videoCollectionHandler := func(c *gin.Context) {
		if platform := getGroupPlatform(c); platform == service.PlatformGrok || platform == service.PlatformComposite {
			videoGenerationHandler(c)
			return
		}
		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Failed to read request body"}})
			return
		}
		requestmodel.ResetRequestBody(c.Request, body)
		// Keep non-Grok NewAPI video models and account-level toggles available.
		if strings.HasPrefix(strings.ToLower(requestmodel.FromBody(c.GetHeader("Content-Type"), body)), "grok-") {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			writeVideosUnsupported(c)
			return
		}
		h.NewAPIStyleGateway.Videos(c)
	}
	videoStatusHandler := func(c *gin.Context) {
		// Video status requests do not carry a model, so composite groups cannot
		// be resolved by compositeTargetPlatformMiddleware. Route them through
		// the Grok handler and let scheduler/account selection enforce capacity.
		if getGroupPlatform(c) == service.PlatformGrok || getGroupPlatform(c) == service.PlatformComposite {
			h.OpenAIGateway.GrokVideoStatus(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		writeVideosUnsupported(c)
	}
	videoContentHandler := func(c *gin.Context) {
		// Video content requests do not carry a model, so composite groups cannot
		// be resolved by compositeTargetPlatformMiddleware. Route them through
		// the Grok handler just like video status lookups.
		if getGroupPlatform(c) == service.PlatformGrok || getGroupPlatform(c) == service.PlatformComposite {
			h.OpenAIGateway.GrokVideoContent(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Videos API is not supported for this platform",
			},
		})
	}
	videoEditHandler := func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			h.OpenAIGateway.GrokVideoEdit(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Videos API is not supported for this platform"}})
	}
	videoExtensionHandler := func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			h.OpenAIGateway.GrokVideoExtension(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Videos API is not supported for this platform"}})
	}
	voiceHandler := func(endpoint string) gin.HandlerFunc {
		return func(c *gin.Context) {
			if getGroupPlatform(c) != service.PlatformGrok {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Voice API is not supported for this platform"}})
				return
			}
			h.OpenAIGateway.GrokVoice(c, endpoint)
		}
	}
	customVoicePathHandler := func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformGrok {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Voice API is not supported for this platform"}})
			return
		}
		h.OpenAIGateway.GrokVoice(c, grokCustomVoiceEndpoint(c))
	}
	realtimeHandler := func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformGrok {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Realtime API is not supported for this platform"}})
			return
		}
		h.OpenAIGateway.GrokRealtime(c)
	}
	webSearchHandler := func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformGrok {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Web Search API is not supported for this platform"}})
			return
		}
		h.Gateway.WebSearch(c)
	}
	xSearchHandler := func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformGrok {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "X Search API is not supported for this platform"}})
			return
		}
		h.Gateway.XSearch(c)
	}
	// /responses/*subpath 的子路径会被转发到上游同名端点之后，因此在入口就拒掉
	// 不可转发的子路径，不让它进入调度与转发流程。可转发的判定见
	// service.IsForwardableOpenAIResponsesRequestPath 及 upstream_path_guard.go。
	guardResponsesSubpath := func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if !service.IsForwardableOpenAIResponsesRequestPath(c) {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Unsupported responses subpath",
					},
				})
				return
			}
			if service.IsOpenAIResponsesInputTokensRequestPath(c) && isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.ResponsesInputTokens(c)
				return
			}
			next(c)
		}
	}

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.GET("/sub2api/billing", h.Gateway.KeyBillingInfo)
	gateway.Use(groupModelAllowlist)
	gateway.Use(compositeTarget)
	gateway.Use(requireGroupAnthropic)
	{
		// /v1/messages: auto-route based on group platform
		gateway.POST("/messages", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Messages(c)
				return
			}
			if service.IsCompatiblePlatform(getGroupPlatform(c)) {
				h.CompatibleGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens: OpenAI bridges upstream, Grok estimates
		// locally, and Anthropic-compatible platforms retain their existing path.
		gateway.POST("/messages/count_tokens", countTokensHandler)
		// Codex CLI / Codex app refresh their model picker from the provider's
		// /models endpoint with a client_version query and expect the ChatGPT
		// Codex manifest format; other clients keep the OpenAI-style list.
		gateway.GET("/models", modelsHandler)
		// Single-model discovery never selects the Codex client_version manifest.
		gateway.GET("/models/:model", h.Gateway.Models)
		gateway.GET("/usage", h.Gateway.Usage)
		gateway.POST("/live", h.OpenAIGateway.Live)
		gateway.GET("/live/:call_id", h.OpenAIGateway.LiveSideband)
		// OpenAI Responses API: auto-route based on group platform
		gateway.POST("/responses", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			if service.IsCompatiblePlatform(getGroupPlatform(c)) {
				h.CompatibleGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.POST("/responses/*subpath", guardResponsesSubpath(func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			if service.IsCompatiblePlatform(getGroupPlatform(c)) {
				h.CompatibleGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		}))
		gateway.POST("/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
		gateway.GET("/responses", func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		// OpenAI Chat Completions API: auto-route based on group platform
		gateway.POST("/chat/completions", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.ChatCompletions(c)
				return
			}
			if service.IsCompatiblePlatform(getGroupPlatform(c)) {
				withLegacyCNNewAPIGroup(c, h.CompatibleGateway.ChatCompletions)
				return
			}
			h.Gateway.ChatCompletions(c)
		})
		gateway.POST("/embeddings", textBodyLimit, embeddingsHandler)
		gateway.POST("/images/generations", imagesHandler)
		gateway.POST("/images/edits", imagesHandler)
		gateway.POST("/images/generations/async", h.AsyncImage.Submit)
		gateway.POST("/images/edits/async", h.AsyncImage.Submit)
		gateway.GET("/images/tasks/:task_id", h.AsyncImage.Get)
		gateway.POST("/images/batches", h.BatchImage.Submit)
		gateway.GET("/images/batches", h.BatchImage.List)
		gateway.GET("/images/batches/models", h.BatchImage.Models)
		gateway.GET("/images/batches/:id", h.BatchImage.Get)
		gateway.GET("/images/batches/:id/items", h.BatchImage.Items)
		gateway.GET("/images/batches/:id/items/:custom_id/content", h.BatchImage.ItemContent)
		gateway.GET("/images/batches/:id/download", h.BatchImage.Download)
		gateway.POST("/images/batches/:id/cancel", h.BatchImage.Cancel)
		gateway.DELETE("/images/batches/:id", h.BatchImage.DeleteRecord)
		gateway.DELETE("/images/batches/:id/outputs", h.BatchImage.DeleteOutputs)
		gateway.POST("/audio/*subpath", h.NewAPIStyleGateway.Audio)
		gateway.POST("/rerank", h.NewAPIStyleGateway.Rerank)
		gateway.GET("/videos", h.NewAPIStyleGateway.Videos)
		gateway.POST("/videos", videoCollectionHandler)
		gateway.POST("/videos/generations", videoGenerationHandler)
		gateway.POST("/videos/edits", videoEditHandler)
		gateway.POST("/videos/extensions", videoExtensionHandler)
		gateway.GET("/videos/generations/:request_id/content", videoContentHandler)
		gateway.GET("/videos/edits/:request_id/content", videoContentHandler)
		gateway.GET("/videos/extensions/:request_id/content", videoContentHandler)
		gateway.GET("/videos/generations/:request_id", videoStatusHandler)
		gateway.GET("/videos/edits/:request_id", videoStatusHandler)
		gateway.GET("/videos/extensions/:request_id", videoStatusHandler)
		gateway.GET("/videos/:request_id", videoStatusHandler)
		gateway.POST("/videos/:id", h.NewAPIStyleGateway.Videos)
		gateway.GET("/video/generations", h.NewAPIStyleGateway.VideoGenerations)
		gateway.POST("/video/generations", h.NewAPIStyleGateway.VideoGenerations)
		gateway.GET("/video/generations/*subpath", h.NewAPIStyleGateway.VideoGenerations)
		gateway.POST("/video/generations/*subpath", h.NewAPIStyleGateway.VideoGenerations)
		gateway.GET("/videos/:request_id/content", videoContentHandler)
		gateway.POST("/tts", voiceHandler("tts"))
		gateway.POST("/stt", voiceHandler("stt"))
		gateway.POST("/custom-voices", voiceHandler("custom-voices"))
		gateway.GET("/custom-voices", voiceHandler("custom-voices"))
		gateway.GET("/custom-voices/:voice_id/audio", customVoicePathHandler)
		gateway.GET("/custom-voices/:voice_id", customVoicePathHandler)
		gateway.PATCH("/custom-voices/:voice_id", customVoicePathHandler)
		gateway.DELETE("/custom-voices/:voice_id", customVoicePathHandler)
		gateway.GET("/realtime", realtimeHandler)
		gateway.POST("/web_search", webSearchHandler)
		gateway.POST("/x_search", xSearchHandler)
	}

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(groupModelAllowlist)
	gemini.Use(compositeGeminiTarget)
	gemini.Use(requireGroupGoogle)
	{
		gemini.GET("/models", h.Gateway.GeminiV1BetaListModels)
		gemini.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		gemini.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			h.OpenAIGateway.Responses(c)
			return
		}
		if service.IsCompatiblePlatform(getGroupPlatform(c)) {
			h.CompatibleGateway.Responses(c)
			return
		}
		h.Gateway.Responses(c)
	}
	rootRoute := func(method, path string, limit gin.HandlerFunc, handler gin.HandlerFunc) {
		r.Handle(method, path, limit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, requireGroupAnthropic, handler)
	}
	for _, prefix := range []string{"/api/v3", "/v3", "/v1", ""} {
		rootRoute(http.MethodPost, prefix+"/contents/generations/tasks", bodyLimit, h.OpenAIGateway.SeedanceTasks)
		rootRoute(http.MethodGet, prefix+"/contents/generations/tasks/:task_id", bodyLimit, h.OpenAIGateway.SeedanceTasks)
		rootRoute(http.MethodDelete, prefix+"/contents/generations/tasks/:task_id", bodyLimit, h.OpenAIGateway.SeedanceTasks)
	}
	rootRoute(http.MethodPost, "/responses", bodyLimit, responsesHandler)
	rootRoute(http.MethodPost, "/responses/*subpath", bodyLimit, guardResponsesSubpath(responsesHandler))
	rootRoute(http.MethodPost, "/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
	rootRoute(http.MethodGet, "/responses", bodyLimit, func(c *gin.Context) {
		h.OpenAIGateway.ResponsesWebSocket(c)
	})
	rootRoute(http.MethodGet, "/models", bodyLimit, modelsHandler)
	rootRoute(http.MethodGet, "/models/:model", bodyLimit, h.Gateway.Models)
	rootRoute(http.MethodPost, "/messages/count_tokens", bodyLimit, countTokensHandler)
	codexDirect := r.Group("/backend-api/codex")
	codexDirect.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, requireGroupAnthropic)
	{
		codexDirect.POST("/realtime/calls", h.OpenAIGateway.Live)
		codexDirect.GET("/:call_id", h.OpenAIGateway.LiveSideband)
		codexDirect.POST("/responses", responsesHandler)
		codexDirect.POST("/responses/*subpath", guardResponsesSubpath(responsesHandler))
		codexDirect.POST("/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
		codexDirect.GET("/responses", func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		codexDirect.GET("/models", codexModelsHandler)
	}
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	rootRoute(http.MethodPost, "/chat/completions", bodyLimit, func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			h.OpenAIGateway.ChatCompletions(c)
			return
		}
		if service.IsCompatiblePlatform(getGroupPlatform(c)) {
			h.CompatibleGateway.ChatCompletions(c)
			return
		}
		h.Gateway.ChatCompletions(c)
	})
	rootRoute(http.MethodPost, "/embeddings", textBodyLimit, embeddingsHandler)
	r.POST("/compatible-mode/v1/chat/completions", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, requireGroupAnthropic, h.CompatibleGateway.QwenCompatibleModeChatCompletions)
	rootRoute(http.MethodPost, "/images/generations", bodyLimit, imagesHandler)
	rootRoute(http.MethodPost, "/images/edits", bodyLimit, imagesHandler)
	newAPIOnly := []gin.HandlerFunc{bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), groupModelAllowlist, compositeTarget, requireGroupAnthropic}
	r.POST("/images/generations/async", append(newAPIOnly, h.AsyncImage.Submit)...)
	r.POST("/images/edits/async", append(newAPIOnly, h.AsyncImage.Submit)...)
	r.GET("/images/tasks/:task_id", append(newAPIOnly, h.AsyncImage.Get)...)
	r.POST("/audio/*subpath", append(newAPIOnly, h.NewAPIStyleGateway.Audio)...)
	r.POST("/api/paas/v4/audio/*subpath", append(newAPIOnly, h.NewAPIStyleGateway.Audio)...)
	r.POST("/api/v1/services/aigc/multimodal-generation/generation", append(newAPIOnly, h.NewAPIStyleGateway.QwenMultimodalGeneration)...)
	r.POST("/rerank", append(newAPIOnly, h.NewAPIStyleGateway.Rerank)...)
	r.GET("/videos", append(newAPIOnly, h.NewAPIStyleGateway.Videos)...)
	r.POST("/videos", append(newAPIOnly, videoCollectionHandler)...)
	r.POST("/videos/generations", append(newAPIOnly, videoGenerationHandler)...)
	r.POST("/videos/edits", append(newAPIOnly, videoEditHandler)...)
	r.POST("/videos/extensions", append(newAPIOnly, videoExtensionHandler)...)
	r.GET("/videos/generations/:request_id/content", append(newAPIOnly, videoContentHandler)...)
	r.GET("/videos/edits/:request_id/content", append(newAPIOnly, videoContentHandler)...)
	r.GET("/videos/extensions/:request_id/content", append(newAPIOnly, videoContentHandler)...)
	r.GET("/videos/generations/:request_id", append(newAPIOnly, videoStatusHandler)...)
	r.GET("/videos/edits/:request_id", append(newAPIOnly, videoStatusHandler)...)
	r.GET("/videos/extensions/:request_id", append(newAPIOnly, videoStatusHandler)...)
	r.GET("/videos/:request_id", append(newAPIOnly, videoStatusHandler)...)
	r.POST("/videos/:id", append(newAPIOnly, h.NewAPIStyleGateway.Videos)...)
	r.GET("/video/generations", append(newAPIOnly, h.NewAPIStyleGateway.VideoGenerations)...)
	r.POST("/video/generations", append(newAPIOnly, h.NewAPIStyleGateway.VideoGenerations)...)
	r.GET("/video/generations/*subpath", append(newAPIOnly, h.NewAPIStyleGateway.VideoGenerations)...)
	r.POST("/video/generations/*subpath", append(newAPIOnly, h.NewAPIStyleGateway.VideoGenerations)...)
	r.Any("/suno/*subpath", append(newAPIOnly, h.NewAPIStyleGateway.Suno)...)
	r.Any("/kling/*subpath", append(newAPIOnly, h.NewAPIStyleGateway.Kling)...)
	r.Any("/mj/*subpath", append(newAPIOnly, h.NewAPIStyleGateway.Midjourney)...)
	rootRoute(http.MethodGet, "/videos/:request_id/content", bodyLimit, videoContentHandler)
	rootRoute(http.MethodPost, "/tts", bodyLimit, voiceHandler("tts"))
	rootRoute(http.MethodPost, "/stt", bodyLimit, voiceHandler("stt"))
	rootRoute(http.MethodPost, "/custom-voices", bodyLimit, voiceHandler("custom-voices"))
	rootRoute(http.MethodGet, "/custom-voices", bodyLimit, voiceHandler("custom-voices"))
	rootRoute(http.MethodGet, "/custom-voices/:voice_id/audio", bodyLimit, customVoicePathHandler)
	rootRoute(http.MethodGet, "/custom-voices/:voice_id", bodyLimit, customVoicePathHandler)
	rootRoute(http.MethodPatch, "/custom-voices/:voice_id", bodyLimit, customVoicePathHandler)
	rootRoute(http.MethodDelete, "/custom-voices/:voice_id", bodyLimit, customVoicePathHandler)
	rootRoute(http.MethodGet, "/realtime", bodyLimit, realtimeHandler)
	rootRoute(http.MethodPost, "/web_search", bodyLimit, webSearchHandler)
	rootRoute(http.MethodPost, "/x_search", bodyLimit, xSearchHandler)

	// Antigravity 模型列表
	r.GET("/antigravity/models", gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	antigravityV1.Use(bodyLimit)
	antigravityV1.Use(clientRequestID)
	antigravityV1.Use(opsErrorLogger)
	antigravityV1.Use(endpointNorm)
	antigravityV1.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1.Use(gin.HandlerFunc(apiKeyAuth))
	antigravityV1.Use(groupModelAllowlist)
	antigravityV1.Use(requireGroupAnthropic)
	{
		antigravityV1.POST("/messages", h.Gateway.Messages)
		antigravityV1.POST("/messages/count_tokens", h.Gateway.CountTokens)
		antigravityV1.GET("/models", h.Gateway.AntigravityModels)
		antigravityV1.GET("/usage", h.Gateway.Usage)
	}

	antigravityV1Beta := r.Group("/antigravity/v1beta")
	antigravityV1Beta.Use(bodyLimit)
	antigravityV1Beta.Use(clientRequestID)
	antigravityV1Beta.Use(opsErrorLogger)
	antigravityV1Beta.Use(endpointNorm)
	antigravityV1Beta.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1Beta.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	antigravityV1Beta.Use(groupModelAllowlist)
	antigravityV1Beta.Use(requireGroupGoogle)
	{
		antigravityV1Beta.GET("/models", h.Gateway.GeminiV1BetaListModels)
		antigravityV1Beta.GET("/models/:model", h.Gateway.GeminiV1BetaGetModel)
		antigravityV1Beta.POST("/models/*modelAction", h.Gateway.GeminiV1BetaModels)
	}

}

func dispatchCodexModelsGateway(c *gin.Context, openAIHandler, generatedHandler gin.HandlerFunc) {
	if getGroupPlatform(c) == service.PlatformOpenAI {
		openAIHandler(c)
		return
	}
	generatedHandler(c)
}

// getGroupPlatform extracts the group platform from the API Key stored in context.
func getGroupPlatform(c *gin.Context) string {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		return ""
	}
	if apiKey.Group.Platform == service.PlatformComposite {
		if platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
			return platform
		}
	}
	return apiKey.Group.Platform
}

func writeImagesUnsupported(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{
		"error": gin.H{
			"type":    "not_found_error",
			"message": "Images API is not supported for this platform",
		},
	})
}

func writeVideosUnsupported(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{
		"error": gin.H{
			"type":    "not_found_error",
			"message": "Videos API is not supported for this platform",
		},
	})
}

func writeEmbeddingsUnsupported(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{
		"error": gin.H{
			"type":    "not_found_error",
			"message": "Embeddings API is not supported for this platform",
		},
	})
}

func compositeTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	if resolver == nil {
		resolver = service.NewCompositeRouteResolver(nil)
	}
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite {
			c.Next()
			return
		}
		if c.Request == nil || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			status := http.StatusBadRequest
			message := "Failed to read request body"
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
				message = "Request body is too large"
			}
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
			c.Abort()
			return
		}

		routePath := c.FullPath()
		model := requestmodel.FromBodyForRoute(routePath, c.GetHeader("Content-Type"), body)
		if model != "" {
			decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, compositeRouteEndpointForPath(c.Request.URL.Path))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
				c.Abort()
				return
			}
			if decision.Matched {
				c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
				if upstreamModel := strings.TrimSpace(decision.UpstreamModel); upstreamModel != "" && upstreamModel != model && gjson.ValidBytes(body) {
					if _, modelPath := requestmodel.JSONModelPathForRoute(routePath, body); modelPath != "" {
						if rewritten, rewriteErr := sjson.SetBytes(body, modelPath, upstreamModel); rewriteErr == nil {
							body = rewritten
						}
					}
				}
			}
		}
		requestmodel.ResetRequestBody(c.Request, body)
		c.Next()
	}
}

func compositeGeminiTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	if resolver == nil {
		resolver = service.NewCompositeRouteResolver(nil)
	}
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == service.PlatformComposite {
			model := compositeGeminiModelFromParams(c)
			if model != "" {
				decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, service.CompositeRouteEndpointGemini)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
					c.Abort()
					return
				}
				if decision.Matched {
					c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
				}
			}
			if _, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context()); !resolved {
				c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformGemini))
			}
		}
		c.Next()
	}
}

func compositeGeminiModelFromParams(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if model := strings.TrimSpace(c.Param("model")); model != "" {
		return model
	}
	modelAction := strings.TrimPrefix(strings.TrimSpace(c.Param("modelAction")), "/")
	if modelAction == "" {
		return ""
	}
	if idx := strings.LastIndex(modelAction, ":"); idx >= 0 {
		return strings.TrimSpace(modelAction[:idx])
	}
	return modelAction
}

func compositeRouteEndpointForPath(path string) string {
	switch {
	case strings.Contains(path, "/messages/count_tokens"):
		return service.CompositeRouteEndpointCountTokens
	case strings.Contains(path, "/messages"):
		return service.CompositeRouteEndpointMessages
	case strings.Contains(path, "/responses"),
		strings.Contains(path, "/alpha/search"),
		strings.Contains(path, "/realtime/calls"),
		strings.HasSuffix(strings.TrimRight(path, "/"), "/live"):
		return service.CompositeRouteEndpointResponses
	case strings.Contains(path, "/chat/completions"):
		return service.CompositeRouteEndpointChatCompletions
	case strings.Contains(path, "/embeddings"):
		return service.CompositeRouteEndpointEmbeddings
	case strings.Contains(path, "/images/"):
		return service.CompositeRouteEndpointImages
	case strings.Contains(path, "/v1beta/"):
		return service.CompositeRouteEndpointGemini
	default:
		return service.CompositeRouteEndpointAny
	}
}

// grokCustomVoiceEndpoint derives the upstream Voice endpoint from the matched
// route template so a voice literally named "audio" remains addressable.
func grokCustomVoiceEndpoint(c *gin.Context) string {
	endpoint := "custom-voices/" + c.Param("voice_id")
	if strings.HasSuffix(c.FullPath(), "/:voice_id/audio") {
		endpoint += "/audio"
	}
	return endpoint
}
