package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type NewAPIStyleRoute string

const (
	NewAPIStyleRouteChatCompletions NewAPIStyleRoute = "chat_completions"
	NewAPIStyleRouteMessages        NewAPIStyleRoute = "messages"
	NewAPIStyleRouteResponses       NewAPIStyleRoute = "responses"
	NewAPIStyleRouteImages          NewAPIStyleRoute = "images"
	NewAPIStyleRouteAudio           NewAPIStyleRoute = "audio"
	NewAPIStyleRouteQwenTTS         NewAPIStyleRoute = "qwen_tts"
	NewAPIStyleRouteQwenImage       NewAPIStyleRoute = "qwen_image"
	NewAPIStyleRouteEmbeddings      NewAPIStyleRoute = "embeddings"
	NewAPIStyleRouteRerank          NewAPIStyleRoute = "rerank"
	NewAPIStyleRouteVideo           NewAPIStyleRoute = "video"
	NewAPIStyleRouteSuno            NewAPIStyleRoute = "suno"
	NewAPIStyleRouteKling           NewAPIStyleRoute = "kling"
	NewAPIStyleRouteMidjourney      NewAPIStyleRoute = "midjourney"
	NewAPIStyleRouteTask            NewAPIStyleRoute = "task"
)

const (
	BillableUnitTypeToken     = "token"
	BillableUnitTypeImage     = "image"
	BillableUnitTypeRequest   = "request"
	BillableUnitTypeTask      = "task"
	BillableUnitTypeDuration  = "duration"
	BillableUnitTypeCharacter = "character"
)

const (
	zhipuAudioTranscriptionsPath    = "/api/paas/v4/audio/transcriptions"
	zhipuAudioSpeechPath            = "/api/paas/v4/audio/speech"
	aliQwenMultimodalGenerationPath = "/api/v1/services/aigc/multimodal-generation/generation"
	aliQwenTTSGenerationPath        = aliQwenMultimodalGenerationPath

	maxNewAPIStyleMultipartModelBytes          = 64 << 10
	maxNewAPIStyleAudioStreamErrorCaptureBytes = 1 << 20
	maxNewAPIStyleTerminalLinePrefixBytes      = 8 << 10
)

var ErrNewAPIStyleUnsupportedCapability = errors.New("new-api style capability unsupported")

type NewAPIStyleGatewayService struct {
	gatewayService      *GatewayService
	httpUpstream        HTTPUpstream
	cfg                 *config.Config
	tlsFPProfileService *TLSFingerprintProfileService
	runtimeBlocker      AccountRuntimeBlocker
}

func NewNewAPIStyleGatewayService(
	gatewayService *GatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFingerprintProfileService *TLSFingerprintProfileService,
	runtimeBlocker AccountRuntimeBlocker,
) *NewAPIStyleGatewayService {
	return &NewAPIStyleGatewayService{
		gatewayService:      gatewayService,
		httpUpstream:        httpUpstream,
		cfg:                 cfg,
		tlsFPProfileService: tlsFingerprintProfileService,
		runtimeBlocker:      runtimeBlocker,
	}
}

type NewAPIStyleForwardOptions struct {
	Route       NewAPIStyleRoute
	Group       *Group
	RequestBody []byte
	Stream      bool
	Model       string
	// ChannelMappedModel is resolved by handlers before async billing so
	// forwarding and usage records share the same channel mapping semantics.
	ChannelMappedModel string
	ImageSize          string
	Method             string
	InboundPath        string
	QueryString        string
	ForceTask          bool
	ContentType        string
	HeaderSource       http.Header
}

func (s *NewAPIStyleGatewayService) Supports(account *Account, route NewAPIStyleRoute) bool {
	return s.SupportsForGroup(account, nil, route)
}

func (s *NewAPIStyleGatewayService) SupportsForGroup(account *Account, group *Group, route NewAPIStyleRoute) bool {
	if account == nil {
		return false
	}
	if route == NewAPIStyleRouteImages && account.Platform == PlatformAli {
		return true
	}
	if route == NewAPIStyleRouteQwenTTS || route == NewAPIStyleRouteQwenImage {
		return account.Platform == PlatformAli
	}
	if !account.UseNewAPIStyleInterfaceForGroup(group) {
		return false
	}
	if !PlatformSupportsNewAPIStyleInterface(account.Platform) {
		return false
	}
	switch route {
	case NewAPIStyleRouteChatCompletions:
		return true
	case NewAPIStyleRouteMessages:
		return account.Platform == PlatformAnthropic
	case NewAPIStyleRouteResponses:
		return account.Platform == PlatformOpenAI
	case NewAPIStyleRouteImages:
		return account.Platform == PlatformOpenAI || account.Platform == PlatformAli
	case NewAPIStyleRouteVideo:
		return account.Platform == PlatformOpenAI
	case NewAPIStyleRouteEmbeddings:
		return platformSupportsNewAPIStyleEmbeddings(account.Platform)
	case NewAPIStyleRouteAudio:
		return account.Platform == PlatformOpenAI || account.Platform == PlatformZhipu
	case NewAPIStyleRouteRerank:
		return account.Platform == PlatformSiliconFlow
	case NewAPIStyleRouteSuno:
		return account.Platform == PlatformSuno
	case NewAPIStyleRouteKling:
		return account.Platform == PlatformKling
	case NewAPIStyleRouteMidjourney:
		return account.Platform == PlatformMidjourney
	case NewAPIStyleRouteTask:
		return account.Platform == PlatformSuno || account.Platform == PlatformKling || account.Platform == PlatformMidjourney
	default:
		return false
	}
}

func platformSupportsNewAPIStyleEmbeddings(platform string) bool {
	switch strings.TrimSpace(platform) {
	case PlatformOpenAI, PlatformZhipu, PlatformDeepSeek, PlatformVolcEngine, PlatformAli, PlatformMoonshot,
		PlatformPerplexity, PlatformMistral, PlatformSiliconFlow, PlatformOpenRouter:
		return true
	default:
		return false
	}
}

func (s *NewAPIStyleGatewayService) Forward(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	opts NewAPIStyleForwardOptions,
) (*ForwardResult, string, error) {
	start := time.Now()
	if account == nil {
		return nil, "", fmt.Errorf("account is nil")
	}
	if !s.SupportsForGroup(account, opts.Group, opts.Route) {
		return nil, "", fmt.Errorf("%w: platform=%s route=%s", ErrNewAPIStyleUnsupportedCapability, account.Platform, opts.Route)
	}
	if opts.Method == "" {
		opts.Method = http.MethodPost
	}
	if opts.HeaderSource == nil && c != nil && c.Request != nil {
		opts.HeaderSource = c.Request.Header
	}
	if opts.InboundPath == "" && c != nil && c.Request != nil && c.Request.URL != nil {
		opts.InboundPath = c.Request.URL.Path
		opts.QueryString = c.Request.URL.RawQuery
	}
	if opts.Model == "" && len(opts.RequestBody) > 0 {
		opts.Model = ExtractNewAPIStyleModel(opts.RequestBody, opts.ContentType)
	}
	if !opts.Stream && len(opts.RequestBody) > 0 {
		opts.Stream = gjson.GetBytes(opts.RequestBody, "stream").Bool()
	}
	body := opts.RequestBody
	if channelMappedModel := strings.TrimSpace(opts.ChannelMappedModel); channelMappedModel != "" && opts.Model != "" && channelMappedModel != opts.Model {
		rewrittenBody, rewrittenContentType, err := replaceNewAPIStyleModelInBody(body, opts.ContentType, channelMappedModel)
		if err != nil {
			return nil, "", err
		}
		body = rewrittenBody
		opts.RequestBody = rewrittenBody
		opts.ContentType = rewrittenContentType
		opts.Model = channelMappedModel
	}

	targetURL, upstreamEndpoint, err := s.buildTargetURL(account, opts)
	if err != nil {
		return nil, upstreamEndpoint, err
	}
	if opts.Model != "" {
		if mapped := account.GetMappedModel(opts.Model); mapped != "" && mapped != opts.Model {
			rewrittenBody, rewrittenContentType, err := replaceNewAPIStyleModelInBody(body, opts.ContentType, mapped)
			if err != nil {
				return nil, upstreamEndpoint, err
			}
			body = rewrittenBody
			opts.ContentType = rewrittenContentType
			opts.Model = mapped
		}
	}
	body, err = patchNewAPIStyleCompatibleBody(body, account, opts)
	if err != nil {
		return nil, upstreamEndpoint, err
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, opts.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, upstreamEndpoint, err
	}
	s.patchHeaders(upstreamReq, account, opts)
	setOpsUpstreamRequestBody(c, body)

	proxyURL := resolveAccountProxyURL(ctx, account, nil)
	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.tlsProfile(account))
	if err != nil {
		ClearAutoSelectedProxyStickyOnTransportError(ctx, account, err)
		if account.Platform == PlatformOpenAI && s.gatewayService != nil {
			return nil, upstreamEndpoint, s.gatewayService.handleOpenAIUpstreamTransportError(ctx, c, account, err, false, safeUpstreamURL(upstreamReq.URL.String()), s.runtimeBlocker)
		}
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			UpstreamURL:        safeUpstreamURL(upstreamReq.URL.String()),
			Kind:               "request_error",
			Message:            safeErr,
		})
		if errors.Is(err, context.Canceled) {
			return nil, upstreamEndpoint, err
		}
		return nil, upstreamEndpoint, &UpstreamFailoverError{
			StatusCode:   http.StatusBadGateway,
			ResponseBody: []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`),
		}
	}
	if resp == nil {
		return nil, upstreamEndpoint, fmt.Errorf("new-api style upstream response is nil")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		respBody, _ := ReadUpstreamResponseBody(resp.Body, s.cfg, c, nil)
		if s.gatewayService != nil && s.gatewayService.rateLimitService != nil {
			s.gatewayService.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
		}
		upstreamMsg := sanitizeUpstreamErrorMessage(ExtractUpstreamErrorMessage(respBody))
		setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, "")
		shouldFailover := newAPIStyleShouldFailoverStatus(resp.StatusCode)
		eventKind := "http_error"
		if shouldFailover {
			eventKind = "failover"
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:             account.Platform,
			AccountID:            account.ID,
			AccountName:          account.Name,
			UpstreamStatusCode:   resp.StatusCode,
			UpstreamRequestID:    firstNonEmptyText(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
			UpstreamURL:          safeUpstreamURL(upstreamReq.URL.String()),
			UpstreamResponseBody: string(respBody),
			Kind:                 eventKind,
			Message:              upstreamMsg,
		})
		safeBody := sanitizeClientVisibleUpstreamErrorPayload(respBody)
		if shouldFailover {
			return nil, upstreamEndpoint, &UpstreamFailoverError{
				StatusCode:      resp.StatusCode,
				ResponseBody:    safeBody,
				ResponseHeaders: resp.Header.Clone(),
			}
		}
		safeMessage := strings.TrimSpace(ExtractUpstreamErrorMessage(safeBody))
		if safeMessage == "" {
			safeMessage = "Upstream request failed"
		}
		return nil, upstreamEndpoint, &CompatibleClientError{
			StatusCode: resp.StatusCode,
			ErrorType:  "upstream_error",
			Message:    safeMessage,
		}
	}

	resultModel := opts.Model
	if resultModel == "" {
		resultModel = ExtractNewAPIStyleModel(body, opts.ContentType)
	}
	result := &ForwardResult{
		RequestID:        firstNonEmptyText(resp.Header.Get("x-request-id"), resp.Header.Get("request-id")),
		Model:            resultModel,
		UpstreamModel:    opts.Model,
		Stream:           opts.Stream,
		Duration:         time.Since(start),
		BillableUnitType: BillableUnitTypeToken,
	}
	if result.UpstreamModel == result.Model {
		result.UpstreamModel = ""
	}

	writeResponseHeaders(c, resp.Header)
	if opts.Stream || isEventStream(resp.Header.Get("Content-Type")) {
		captureAudioStream := isNewAPIStyleASRRoute(opts) || isNewAPIStyleTTSRoute(opts)
		captureStreamUsage := newAPIStyleShouldCaptureStreamUsage(opts)
		captureStreamTerminal := newAPIStyleShouldCaptureTerminal(opts, resp.Header.Get("Content-Type"))
		var audioStreamCapture bytes.Buffer
		var streamTerminalCapture *newAPIStyleSSETerminalCaptureWriter
		var usageCapture *newAPIStyleSSEUsageCaptureWriter
		streamReader := io.Reader(resp.Body)
		if captureAudioStream || captureStreamUsage || captureStreamTerminal {
			writers := make([]io.Writer, 0, 3)
			if captureAudioStream {
				writers = append(writers, &tailBufferWriter{
					buffer: &audioStreamCapture,
					limit:  maxNewAPIStyleAudioStreamErrorCaptureBytes,
				})
			}
			if captureStreamUsage {
				usageCapture = &newAPIStyleSSEUsageCaptureWriter{route: opts.Route}
				writers = append(writers, usageCapture)
			}
			if captureStreamTerminal {
				streamTerminalCapture = &newAPIStyleSSETerminalCaptureWriter{route: opts.Route}
				writers = append(writers, streamTerminalCapture)
			}
			streamReader = io.TeeReader(resp.Body, io.MultiWriter(writers...))
		}
		var copiedBytes int64
		var copyErr error
		if c != nil {
			c.Status(resp.StatusCode)
			if isEventStream(resp.Header.Get("Content-Type")) {
				safeWriter := newClientVisibleSSESanitizingWriter(c.Writer)
				copiedBytes, copyErr = io.Copy(safeWriter, streamReader)
				if flushErr := safeWriter.FlushRemaining(); copyErr == nil && flushErr != nil {
					copyErr = flushErr
				}
			} else {
				copiedBytes, copyErr = io.Copy(c.Writer, streamReader)
			}
			if copiedBytes > 0 || copyErr == nil {
				if f, ok := c.Writer.(http.Flusher); ok {
					f.Flush()
				}
			}
		} else {
			copiedBytes, copyErr = io.Copy(io.Discard, streamReader)
		}
		if copyErr != nil {
			safeMessage := "Upstream stream disconnected"
			setOpsUpstreamError(c, http.StatusBadGateway, safeMessage, "")
			if copiedBytes == 0 {
				return nil, upstreamEndpoint, &UpstreamFailoverError{
					StatusCode:             http.StatusBadGateway,
					ResponseBody:           []byte(`{"error":{"type":"upstream_error","message":"Upstream stream disconnected"}}`),
					RetryableOnSameAccount: true,
				}
			}
			terminal, successfulTerminal := streamTerminalCapture.State()
			if terminal {
				result.SkipUsageBilling = !successfulTerminal
			} else if captureStreamTerminal {
				result.SkipUsageBilling = true
				return nil, upstreamEndpoint, errors.New("upstream stream disconnected")
			} else {
				result.SkipUsageBilling = true
			}
		}
		if copyErr == nil && captureStreamTerminal && newAPIStyleRequiresTerminalEvent(opts.Route) {
			terminal, successfulTerminal := streamTerminalCapture.State()
			if !terminal {
				result.SkipUsageBilling = true
				setOpsUpstreamError(c, http.StatusBadGateway, "Upstream stream closed before terminal event", "")
				return nil, upstreamEndpoint, errors.New("upstream stream closed before terminal event")
			}
			result.SkipUsageBilling = !successfulTerminal
		}
		if captureAudioStream {
			if upstreamMsg, ok := newAPIStyleAudioUpstreamErrorPayload(opts, audioStreamCapture.Bytes()); ok {
				return nil, upstreamEndpoint, newAPIStyleAudioErrorPayloadResult(c, account, resp, upstreamReq, audioStreamCapture.Bytes(), upstreamMsg)
			}
		}
		if usageCapture != nil {
			s.applyUsageGuardrailsWithParsedUsage(result, opts, nil, usageCapture.Usage())
		} else {
			s.applyUsageGuardrails(result, opts, audioStreamCapture.Bytes())
		}
		result.Duration = time.Since(start)
		return result, upstreamEndpoint, nil
	}

	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, func(c *gin.Context) {
		if c != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "Upstream response too large"}})
		}
	})
	if err != nil {
		return nil, upstreamEndpoint, err
	}
	if upstreamMsg, ok := newAPIStyleAudioUpstreamErrorPayload(opts, respBody); ok {
		return nil, upstreamEndpoint, newAPIStyleAudioErrorPayloadResult(c, account, resp, upstreamReq, respBody, upstreamMsg)
	}
	downstreamBody := respBody
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if normalized, ok := normalizeAliQwenImagesOpenAIResponse(account, opts, respBody); ok {
		downstreamBody = normalized
		contentType = "application/json"
	}
	s.applyUsageGuardrails(result, opts, respBody)
	if contentType == "" {
		contentType = "application/json"
	}
	if c != nil {
		c.Data(resp.StatusCode, contentType, downstreamBody)
	}
	result.Duration = time.Since(start)
	return result, upstreamEndpoint, nil
}

func newAPIStyleStreamTerminalState(route NewAPIStyleRoute, payload []byte) (terminal bool, successful bool) {
	w := &newAPIStyleSSETerminalCaptureWriter{route: route}
	_, _ = w.Write(payload)
	return w.State()
}

func newAPIStyleTerminalEventType(route NewAPIStyleRoute, eventType string) (terminal bool, successful bool) {
	switch route {
	case NewAPIStyleRouteResponses:
		switch eventType {
		case "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
			return true, false
		case "response.completed", "response.done":
			return true, true
		}
	case NewAPIStyleRouteMessages:
		switch eventType {
		case "error":
			return true, false
		case "message_stop":
			return true, true
		}
	}
	return false, false
}

func newAPIStyleRequiresTerminalEvent(route NewAPIStyleRoute) bool {
	switch route {
	case NewAPIStyleRouteChatCompletions, NewAPIStyleRouteMessages, NewAPIStyleRouteResponses:
		return true
	default:
		return false
	}
}

func newAPIStyleShouldCaptureTerminal(opts NewAPIStyleForwardOptions, contentType string) bool {
	if !newAPIStyleRequiresTerminalEvent(opts.Route) {
		return false
	}
	if isEventStream(contentType) {
		return true
	}
	if !opts.Stream {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	}
	return !strings.HasPrefix(mediaType, "audio/") &&
		!strings.HasPrefix(mediaType, "image/") &&
		!strings.HasPrefix(mediaType, "video/") &&
		mediaType != "application/octet-stream"
}

func newAPIStyleShouldFailoverStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, 529:
		return true
	default:
		return statusCode >= http.StatusInternalServerError
	}
}

func (s *NewAPIStyleGatewayService) ForwardOpenAI(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	opts NewAPIStyleForwardOptions,
) (*OpenAIForwardResult, string, error) {
	result, endpoint, err := s.Forward(ctx, c, account, opts)
	if err != nil {
		return nil, endpoint, err
	}
	return OpenAIForwardResultFromForwardResult(result), endpoint, nil
}

func OpenAIForwardResultFromForwardResult(result *ForwardResult) *OpenAIForwardResult {
	if result == nil {
		return nil
	}
	openAIInputTokens := result.Usage.InputTokens
	openAIInputTokens += result.Usage.CacheReadInputTokens + result.Usage.CacheCreationInputTokens
	return &OpenAIForwardResult{
		RequestID: result.RequestID,
		Usage: OpenAIUsage{
			InputTokens:              openAIInputTokens,
			OutputTokens:             result.Usage.OutputTokens,
			CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
			CacheCreation5mTokens:    result.Usage.CacheCreation5mTokens,
			CacheCreation1hTokens:    result.Usage.CacheCreation1hTokens,
			CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
			ImageOutputTokens:        result.Usage.ImageOutputTokens,
		},
		Model:                   result.Model,
		UpstreamModel:           result.UpstreamModel,
		ReasoningEffort:         result.ReasoningEffort,
		Stream:                  result.Stream,
		Duration:                result.Duration,
		FirstTokenMs:            result.FirstTokenMs,
		ImageCount:              result.ImageCount,
		SkipUsageBilling:        result.SkipUsageBilling,
		ImageSize:               result.ImageSize,
		ImageInputSize:          result.ImageInputSize,
		ImageOutputSize:         result.ImageOutputSize,
		ImageOutputSizes:        result.ImageOutputSizes,
		ImageSizeSource:         result.ImageSizeSource,
		ImageSizeBreakdown:      result.ImageSizeBreakdown,
		RequestCount:            result.RequestCount,
		TaskCount:               result.TaskCount,
		BillableDurationSeconds: result.BillableDurationSeconds,
		BillableCharacterCount:  result.BillableCharacterCount,
		UsageEstimated:          result.UsageEstimated,
		BillableUnitType:        result.BillableUnitType,
	}
}

func (s *NewAPIStyleGatewayService) buildTargetURL(account *Account, opts NewAPIStyleForwardOptions) (string, string, error) {
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" && account.IsCustomBaseURLEnabled() {
		baseURL = strings.TrimSpace(account.GetCustomBaseURL())
	}
	if baseURL == "" {
		baseURL = newAPIStyleDefaultBaseURL(account.Platform)
	}
	if baseURL == "" && account.IsCompatiblePlatform() {
		baseURL = CompatibleDefaultBaseURL(account.Platform)
	}
	if baseURL == "" {
		return "", "", fmt.Errorf("new-api style base_url is required for platform %s", account.Platform)
	}
	if s.gatewayService != nil && s.gatewayService.cfg != nil {
		validatedBaseURL, err := s.gatewayService.validateUpstreamBaseURL(baseURL)
		if err != nil {
			return "", "", err
		}
		baseURL = validatedBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	path := newAPIStyleRoutePath(account.Platform, opts)
	if opts.Route == NewAPIStyleRouteChatCompletions && account.Platform == PlatformZhipu {
		if isZhipuOfficialBaseURL(baseURL) {
			path = zhipuCompatibleChatPath
		} else {
			path = "/v1/chat/completions"
		}
	}
	if path == "" {
		if opts.Route == NewAPIStyleRouteAudio || opts.Route == NewAPIStyleRouteQwenTTS || opts.Route == NewAPIStyleRouteQwenImage {
			return "", "", fmt.Errorf("%w: platform=%s route=%s path=%s", ErrNewAPIStyleUnsupportedCapability, account.Platform, opts.Route, opts.InboundPath)
		}
		path = "/v1/" + strings.TrimPrefix(strings.TrimSpace(opts.InboundPath), "/")
	}
	target := joinRelayCompatibleURL(baseURL, path)
	if opts.QueryString != "" && !strings.Contains(path, "?") {
		target += "?" + opts.QueryString
	}
	if _, err := url.ParseRequestURI(target); err != nil {
		return "", path, err
	}
	return target, path, nil
}

func newAPIStyleDefaultBaseURL(platform string) string {
	switch strings.TrimSpace(platform) {
	case PlatformOpenAI:
		return "https://api.openai.com"
	case PlatformAnthropic:
		return "https://api.anthropic.com"
	case PlatformGemini:
		return "https://generativelanguage.googleapis.com"
	case PlatformAntigravity:
		return "https://generativelanguage.googleapis.com/antigravity"
	default:
		return CompatibleDefaultBaseURL(platform)
	}
}

func newAPIStyleRoutePath(platform string, opts NewAPIStyleForwardOptions) string {
	inbound := "/" + strings.TrimLeft(strings.TrimSpace(opts.InboundPath), "/")
	switch opts.Route {
	case NewAPIStyleRouteChatCompletions:
		if preset, ok := newAPIStyleCompatibleProviderPresetForPlatform(platform); ok && preset.BuildChatURL != nil {
			base := strings.TrimRight(preset.DefaultBaseURL, "/")
			full := preset.BuildChatURL(base, opts.Model)
			return "/" + strings.TrimPrefix(strings.TrimPrefix(full, base), "/")
		}
		if preset, ok := CompatibleProviderPresetForPlatform(platform); ok && preset.BuildChatURL != nil {
			base := strings.TrimRight(preset.DefaultBaseURL, "/")
			full := preset.BuildChatURL(base, opts.Model)
			return "/" + strings.TrimPrefix(strings.TrimPrefix(full, base), "/")
		}
		return "/v1/chat/completions"
	case NewAPIStyleRouteMessages:
		return "/v1/messages"
	case NewAPIStyleRouteResponses:
		return "/v1/responses"
	case NewAPIStyleRouteImages:
		if strings.TrimSpace(platform) == PlatformAli {
			if strings.Contains(inbound, "/edits") {
				return ""
			}
			return aliQwenMultimodalGenerationPath
		}
		if strings.Contains(inbound, "/edits") {
			return "/v1/images/edits"
		}
		return "/v1/images/generations"
	case NewAPIStyleRouteAudio:
		return newAPIStyleAudioRoutePath(platform, inbound)
	case NewAPIStyleRouteQwenTTS:
		if strings.TrimSpace(platform) == PlatformAli {
			return aliQwenTTSGenerationPath
		}
		return ""
	case NewAPIStyleRouteQwenImage:
		if strings.TrimSpace(platform) == PlatformAli {
			return aliQwenMultimodalGenerationPath
		}
		return ""
	case NewAPIStyleRouteEmbeddings, NewAPIStyleRouteRerank,
		NewAPIStyleRouteVideo, NewAPIStyleRouteSuno, NewAPIStyleRouteKling, NewAPIStyleRouteMidjourney:
		return inbound
	case NewAPIStyleRouteTask:
		return inbound
	default:
		return ""
	}
}

func ExtractNewAPIStyleModel(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}
	if isMultipartFormData(contentType) {
		return extractNewAPIStyleMultipartModel(body, contentType)
	}
	if model := strings.TrimSpace(gjson.GetBytes(body, "model").String()); model != "" {
		return model
	}
	return strings.TrimSpace(gjson.GetBytes(body, "model_name").String())
}

func extractNewAPIStyleMultipartModel(body []byte, contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return ""
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return ""
		}
		if err != nil {
			return ""
		}

		formName := strings.TrimSpace(part.FormName())
		fileName := strings.TrimSpace(part.FileName())
		if formName != "model" || fileName != "" {
			_ = part.Close()
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, maxNewAPIStyleMultipartModelBytes))
		_ = part.Close()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(value))
	}
}

func replaceNewAPIStyleModelInBody(body []byte, contentType string, model string) ([]byte, string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return body, contentType, nil
	}
	if isMultipartFormData(contentType) {
		return rewriteNewAPIStyleMultipartModel(body, contentType, model)
	}
	return ReplaceModelInBody(body, model), contentType, nil
}

func rewriteNewAPIStyleMultipartModel(body []byte, contentType string, model string) ([]byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, "", fmt.Errorf("parse multipart content-type: %w", err)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return nil, "", fmt.Errorf("multipart boundary is required")
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	modelWritten := false

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read multipart body: %w", err)
		}

		formName := strings.TrimSpace(part.FormName())
		partHeader := cloneMultipartHeader(part.Header)
		target, err := writer.CreatePart(partHeader)
		if err != nil {
			_ = part.Close()
			return nil, "", fmt.Errorf("create multipart part: %w", err)
		}

		if formName == "model" && part.FileName() == "" {
			if _, err := target.Write([]byte(model)); err != nil {
				_ = part.Close()
				return nil, "", fmt.Errorf("rewrite multipart model: %w", err)
			}
			modelWritten = true
			_ = part.Close()
			continue
		}
		if _, err := io.Copy(target, part); err != nil {
			_ = part.Close()
			return nil, "", fmt.Errorf("copy multipart part: %w", err)
		}
		_ = part.Close()
	}

	if !modelWritten {
		if err := writer.WriteField("model", model); err != nil {
			return nil, "", fmt.Errorf("append multipart model field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("finalize multipart body: %w", err)
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func isMultipartFormData(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "multipart/form-data")
	}
	return strings.EqualFold(mediaType, "multipart/form-data")
}

func newAPIStyleAudioRoutePath(platform string, inbound string) string {
	normalized := normalizeNewAPIStyleAudioInboundPath(inbound)
	if normalized == "" {
		return ""
	}
	switch strings.TrimSpace(platform) {
	case PlatformZhipu:
		switch normalized {
		case "/v1/audio/transcriptions", "/audio/transcriptions", zhipuAudioTranscriptionsPath:
			return zhipuAudioTranscriptionsPath
		case "/v1/audio/speech", "/audio/speech", zhipuAudioSpeechPath:
			return zhipuAudioSpeechPath
		default:
			return ""
		}
	case PlatformOpenAI:
		if isZhipuOfficialAudioAliasPath(normalized) {
			return ""
		}
		if strings.HasPrefix(normalized, "/audio/") {
			return "/v1" + normalized
		}
		if strings.HasPrefix(normalized, "/v1/audio/") {
			return normalized
		}
		return ""
	default:
		return ""
	}
}

func normalizeNewAPIStyleAudioInboundPath(path string) string {
	normalized := "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	normalized = strings.ToLower(strings.TrimRight(normalized, "/"))
	if normalized == "" || normalized == "/" {
		return ""
	}
	return normalized
}

func isZhipuOfficialAudioAliasPath(path string) bool {
	normalized := normalizeNewAPIStyleAudioInboundPath(path)
	return normalized == zhipuAudioTranscriptionsPath || normalized == zhipuAudioSpeechPath ||
		strings.HasPrefix(normalized, "/api/paas/v4/audio/")
}

func (s *NewAPIStyleGatewayService) patchHeaders(req *http.Request, account *Account, opts NewAPIStyleForwardOptions) {
	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	} else if len(opts.RequestBody) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", firstNonEmptyText(opts.HeaderSource.Get("Accept"), "application/json"))
	if ua := strings.TrimSpace(opts.HeaderSource.Get("User-Agent")); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	token := newAPIStyleAuthToken(account)
	if token != "" {
		if account.Platform == PlatformAnthropic {
			req.Header.Set("x-api-key", token)
			req.Header.Set("anthropic-version", firstNonEmptyText(opts.HeaderSource.Get("anthropic-version"), "2023-06-01"))
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	if org := strings.TrimSpace(account.GetCredential("organization")); org != "" {
		req.Header.Set("OpenAI-Organization", org)
	}
	if account != nil && account.Platform == PlatformAli && opts.Route == NewAPIStyleRouteChatCompletions {
		patchAliStreamingHeaders(req, account, opts.Model)
	}
	if account != nil && account.Platform == PlatformAli && opts.Route == NewAPIStyleRouteQwenTTS {
		sse := strings.TrimSpace(opts.HeaderSource.Get("X-DashScope-SSE"))
		if sse == "" {
			sse = "enable"
		}
		req.Header.Set("X-DashScope-SSE", sse)
	}
}

func IsAliQwenImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "qwen-image")
}

func patchNewAPIStyleCompatibleBody(body []byte, account *Account, opts NewAPIStyleForwardOptions) ([]byte, error) {
	if account == nil || account.Platform != PlatformAli {
		return body, nil
	}
	switch opts.Route {
	case NewAPIStyleRouteChatCompletions:
		return patchAliBody(body, account, opts.Model)
	case NewAPIStyleRouteImages:
		return convertOpenAIImagesRequestToAliQwenMultimodal(body, opts.Model)
	default:
		return body, nil
	}
}

func convertOpenAIImagesRequestToAliQwenMultimodal(body []byte, model string) ([]byte, error) {
	model = strings.TrimSpace(firstNonEmptyText(model, gjson.GetBytes(body, "model").String()))
	if model == "" {
		return nil, &CompatibleClientError{
			StatusCode: http.StatusBadRequest,
			ErrorType:  "invalid_request_error",
			Message:    "model is required",
		}
	}
	prompt := strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	if prompt == "" {
		return nil, &CompatibleClientError{
			StatusCode: http.StatusBadRequest,
			ErrorType:  "invalid_request_error",
			Message:    "prompt is required for Qwen image generation",
		}
	}

	parameters := map[string]any{
		"watermark": false,
	}
	if value := strings.TrimSpace(gjson.GetBytes(body, "negative_prompt").String()); value != "" {
		parameters["negative_prompt"] = value
	}
	if value := strings.TrimSpace(gjson.GetBytes(body, "size").String()); value != "" {
		parameters["size"] = normalizeAliQwenImageSize(value)
	}
	if n := gjson.GetBytes(body, "n"); n.Exists() {
		parameters["n"] = n.Int()
	}
	if value := gjson.GetBytes(body, "prompt_extend"); value.Exists() {
		parameters["prompt_extend"] = value.Bool()
	}
	if value := gjson.GetBytes(body, "watermark"); value.Exists() {
		parameters["watermark"] = value.Bool()
	}
	if seed := gjson.GetBytes(body, "seed"); seed.Exists() {
		parameters["seed"] = seed.Int()
	}

	payload := map[string]any{
		"model": model,
		"input": map[string]any{
			"messages": []map[string]any{
				{
					"role": "user",
					"content": []map[string]any{
						{"text": prompt},
					},
				},
			},
		},
		"parameters": parameters,
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeAliQwenImageSize(size string) string {
	size = strings.TrimSpace(size)
	if size == "" {
		return ""
	}
	lower := strings.ToLower(size)
	if strings.Contains(lower, "x") {
		return strings.ReplaceAll(lower, "x", "*")
	}
	return size
}

func newAPIStyleAuthToken(account *Account) string {
	if account == nil {
		return ""
	}
	if account.Platform == PlatformZhipu {
		return getCompatibleAuthToken(account, CompatibleAuthZhipuToken)
	}
	return firstNonEmptyText(
		account.GetCredential("api_key"),
		account.GetCredential("token"),
		account.GetCredential("access_token"),
	)
}

func (s *NewAPIStyleGatewayService) tlsProfile(account *Account) *tlsfingerprint.Profile {
	if s == nil || s.tlsFPProfileService == nil {
		return nil
	}
	return s.tlsFPProfileService.ResolveTLSProfile(account)
}

func (s *NewAPIStyleGatewayService) applyUsageGuardrails(result *ForwardResult, opts NewAPIStyleForwardOptions, respBody []byte) {
	if result == nil {
		return
	}
	usage := ClaudeUsage{}
	if len(respBody) > 0 {
		usage = parseNewAPIStyleUsageForRoute(respBody, opts.Route)
		if !claudeUsageHasAny(usage) {
			usage = parseNewAPIStyleSSEUsageForRoute(respBody, opts.Route)
		}
	}
	s.applyUsageGuardrailsWithParsedUsage(result, opts, respBody, usage)
}

func (s *NewAPIStyleGatewayService) applyUsageGuardrailsWithParsedUsage(result *ForwardResult, opts NewAPIStyleForwardOptions, respBody []byte, usage ClaudeUsage) {
	if result == nil {
		return
	}
	if claudeUsageHasAny(usage) {
		result.Usage.InputTokens = usage.InputTokens
		result.Usage.OutputTokens = usage.OutputTokens
		result.Usage.CacheCreationInputTokens = usage.CacheCreationInputTokens
		result.Usage.CacheReadInputTokens = usage.CacheReadInputTokens
		result.Usage.CacheCreation5mTokens = usage.CacheCreation5mTokens
		result.Usage.CacheCreation1hTokens = usage.CacheCreation1hTokens
		result.Usage.ImageOutputTokens = usage.ImageOutputTokens
	}
	switch opts.Route {
	case NewAPIStyleRouteImages, NewAPIStyleRouteQwenImage:
		result.ImageCount = inferImageCount(opts.RequestBody, respBody)
		result.ImageSize = inferImageSize(opts.RequestBody)
		result.ImageInputSize = inferImageRequestedSize(opts.RequestBody)
		result.ImageOutputSizes = collectOpenAIResponseImageOutputSizesFromJSONBytes(respBody)
		result.BillableUnitType = BillableUnitTypeImage
	case NewAPIStyleRouteAudio, NewAPIStyleRouteQwenTTS, NewAPIStyleRouteEmbeddings, NewAPIStyleRouteRerank:
		result.RequestCount = 1
		result.BillableUnitType = BillableUnitTypeRequest
	case NewAPIStyleRouteVideo, NewAPIStyleRouteSuno, NewAPIStyleRouteKling, NewAPIStyleRouteMidjourney, NewAPIStyleRouteTask:
		result.TaskCount = 1
		result.BillableUnitType = BillableUnitTypeTask
	default:
		result.BillableUnitType = BillableUnitTypeToken
	}

	if isASRRoute := isNewAPIStyleASRRoute(opts); isASRRoute {
		if seconds, ok := extractBillableDurationSeconds(respBody, opts.RequestBody, opts.ContentType, isASRRoute); ok {
			result.BillableDurationSeconds = seconds
			result.BillableUnitType = BillableUnitTypeDuration
		}
	}
	if isTTSRoute := isNewAPIStyleTTSRoute(opts); isTTSRoute {
		if chars, ok := extractBillableCharacterCount(opts.RequestBody, opts.ContentType, isTTSRoute); ok {
			result.BillableCharacterCount = chars
			result.BillableUnitType = BillableUnitTypeCharacter
		}
	}

	if !forwardResultHasBillableUsage(result) {
		result.Usage.InputTokens = estimateNewAPIStyleInputTokens(opts.RequestBody)
		result.UsageEstimated = true
		if result.Usage.InputTokens == 0 && opts.Route != NewAPIStyleRouteImages && opts.Route != NewAPIStyleRouteQwenImage {
			result.RequestCount = 1
			result.BillableUnitType = BillableUnitTypeRequest
		}
	}
}

func newAPIStyleShouldCaptureStreamUsage(opts NewAPIStyleForwardOptions) bool {
	switch opts.Route {
	case NewAPIStyleRouteChatCompletions, NewAPIStyleRouteMessages, NewAPIStyleRouteResponses:
		return true
	default:
		return false
	}
}

func isNewAPIStyleASRRoute(opts NewAPIStyleForwardOptions) bool {
	if opts.Route == NewAPIStyleRouteAudio {
		path := strings.ToLower(strings.TrimSpace(opts.InboundPath))
		return strings.Contains(path, "transcriptions") || strings.Contains(path, "translations")
	}
	if opts.Route != NewAPIStyleRouteChatCompletions &&
		opts.Route != NewAPIStyleRouteMessages &&
		opts.Route != NewAPIStyleRouteResponses {
		return false
	}
	model := strings.ToLower(firstNonEmptyText(opts.Model, ExtractNewAPIStyleModel(opts.RequestBody, opts.ContentType)))
	return strings.Contains(model, "asr") && requestContainsAudioInput(opts.RequestBody, opts.ContentType)
}

func isNewAPIStyleTTSRoute(opts NewAPIStyleForwardOptions) bool {
	if opts.Route == NewAPIStyleRouteQwenTTS {
		return true
	}
	if opts.Route != NewAPIStyleRouteAudio {
		return false
	}
	path := strings.ToLower(strings.TrimSpace(opts.InboundPath))
	return strings.Contains(path, "speech")
}

func newAPIStyleAudioUpstreamErrorPayload(opts NewAPIStyleForwardOptions, body []byte) (string, bool) {
	if len(body) == 0 || (!isNewAPIStyleASRRoute(opts) && !isNewAPIStyleTTSRoute(opts)) {
		return "", false
	}
	if json.Valid(body) {
		return newAPIStyleAudioJSONErrorPayload(body)
	}
	return newAPIStyleAudioSSEErrorPayload(body)
}

func newAPIStyleAudioJSONErrorPayload(body []byte) (string, bool) {
	value := gjson.GetBytes(body, "error")
	if !value.Exists() || value.Raw == "null" {
		return "", false
	}
	if value.Type == gjson.String {
		msg := strings.TrimSpace(value.String())
		return msg, msg != ""
	}
	if value.IsObject() {
		msg := strings.TrimSpace(firstNonEmptyText(
			value.Get("message").String(),
			value.Get("msg").String(),
			value.Get("code").String(),
		))
		if msg == "" {
			msg = "upstream returned error payload"
		}
		return msg, true
	}
	return "upstream returned error payload", true
}

func newAPIStyleAudioSSEErrorPayload(body []byte) (string, bool) {
	var eventName string
	for _, rawLine := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" {
			eventName = ""
			continue
		}
		if event, ok := strings.CutPrefix(strings.ToLower(line), "event:"); ok {
			eventName = strings.TrimSpace(event)
			continue
		}
		if !sseDataRe.MatchString(line) {
			continue
		}
		data := strings.TrimSpace(sseDataRe.ReplaceAllString(line, ""))
		if data == "" || data == "[DONE]" {
			continue
		}
		if msg, ok := newAPIStyleAudioJSONErrorPayload([]byte(data)); ok {
			return msg, true
		}
		if strings.Contains(eventName, "error") {
			if json.Valid([]byte(data)) {
				if msg := strings.TrimSpace(firstNonEmptyText(
					gjson.Get(data, "message").String(),
					gjson.Get(data, "msg").String(),
					gjson.Get(data, "code").String(),
				)); msg != "" {
					return msg, true
				}
			}
			return data, true
		}
	}
	return "", false
}

func newAPIStyleAudioErrorPayloadResult(
	c *gin.Context,
	account *Account,
	resp *http.Response,
	upstreamReq *http.Request,
	respBody []byte,
	upstreamMsg string,
) error {
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	if upstreamMsg == "" {
		upstreamMsg = "upstream returned error payload"
	}
	upstreamStatus := 0
	var responseHeaders http.Header
	var upstreamRequestID string
	if resp != nil {
		upstreamStatus = resp.StatusCode
		responseHeaders = resp.Header.Clone()
		upstreamRequestID = firstNonEmptyText(resp.Header.Get("x-request-id"), resp.Header.Get("request-id"))
	}
	upstreamURL := ""
	if upstreamReq != nil && upstreamReq.URL != nil {
		upstreamURL = safeUpstreamURL(upstreamReq.URL.String())
	}
	setOpsUpstreamError(c, http.StatusBadGateway, upstreamMsg, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:             account.Platform,
		AccountID:            account.ID,
		AccountName:          account.Name,
		UpstreamStatusCode:   upstreamStatus,
		UpstreamRequestID:    upstreamRequestID,
		UpstreamURL:          upstreamURL,
		UpstreamResponseBody: string(respBody),
		Kind:                 "http_error",
		Message:              upstreamMsg,
	})
	return &UpstreamFailoverError{
		StatusCode:      http.StatusBadGateway,
		ResponseBody:    respBody,
		ResponseHeaders: responseHeaders,
	}
}

type tailBufferWriter struct {
	buffer *bytes.Buffer
	limit  int
}

type newAPIStyleSSETerminalCaptureWriter struct {
	route           NewAPIStyleRoute
	pending         []byte
	discardLine     bool
	eventTerminal   bool
	eventSuccessful bool
	terminal        bool
	successful      bool
	scanJSONStarted bool
	scanDepth       int
	scanInString    bool
	scanEscaped     bool
	scanStringRole  byte
	scanString      []byte
	scanKey         string
	scanWantKey     bool
	scanWantValue   bool
}

func (w *newAPIStyleSSETerminalCaptureWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			w.appendLineFragment(p)
			break
		}
		w.appendLineFragment(p[:idx])
		w.finishLine()
		p = p[idx+1:]
	}
	return n, nil
}

func (w *newAPIStyleSSETerminalCaptureWriter) appendLineFragment(fragment []byte) {
	if w == nil || len(fragment) == 0 {
		return
	}
	w.scanLineFragment(fragment)
	if w.discardLine {
		return
	}
	remaining := maxNewAPIStyleTerminalLinePrefixBytes - len(w.pending)
	if remaining <= 0 {
		w.pending = nil
		w.discardLine = true
		return
	}
	if len(fragment) <= remaining {
		w.pending = append(w.pending, fragment...)
		return
	}
	w.pending = append(w.pending, fragment[:remaining]...)
	w.pending = nil
	w.discardLine = true
}

func (w *newAPIStyleSSETerminalCaptureWriter) scanLineFragment(fragment []byte) {
	const maxScannedStringBytes = 128
	for _, ch := range fragment {
		if !w.scanJSONStarted {
			if ch == '{' {
				w.scanJSONStarted = true
				w.scanDepth = 1
				w.scanWantKey = true
			}
			continue
		}
		if w.scanInString {
			if w.scanEscaped {
				w.scanEscaped = false
				if w.scanStringRole != 0 && len(w.scanString) < maxScannedStringBytes {
					w.scanString = append(w.scanString, ch)
				}
				continue
			}
			if ch == '\\' {
				w.scanEscaped = true
				continue
			}
			if ch != '"' {
				if w.scanStringRole != 0 && len(w.scanString) < maxScannedStringBytes {
					w.scanString = append(w.scanString, ch)
				}
				continue
			}
			w.scanInString = false
			value := string(w.scanString)
			switch w.scanStringRole {
			case 1:
				w.scanKey = value
				w.scanWantKey = false
			case 2:
				if strings.EqualFold(w.scanKey, "type") {
					w.captureScannedEventType(value)
				}
				w.scanWantValue = false
			}
			w.scanStringRole = 0
			w.scanString = w.scanString[:0]
			continue
		}

		switch ch {
		case '"':
			w.scanInString = true
			w.scanStringRole = 0
			w.scanString = w.scanString[:0]
			if w.scanDepth == 1 && w.scanWantKey {
				w.scanStringRole = 1
			} else if w.scanDepth == 1 && w.scanWantValue {
				w.scanStringRole = 2
			}
		case '{', '[':
			if w.scanDepth == 1 && w.scanWantValue {
				w.scanWantValue = false
			}
			w.scanDepth++
		case '}', ']':
			w.scanDepth--
		case ':':
			if w.scanDepth == 1 && w.scanKey != "" {
				w.scanWantValue = true
			}
		case ',':
			if w.scanDepth == 1 {
				w.scanKey = ""
				w.scanWantKey = true
				w.scanWantValue = false
			}
		}
	}
}

func (w *newAPIStyleSSETerminalCaptureWriter) captureScannedEventType(eventType string) {
	terminal, successful := newAPIStyleTerminalEventType(w.route, strings.ToLower(strings.TrimSpace(eventType)))
	if !terminal {
		return
	}
	if !w.eventTerminal || !successful {
		w.eventTerminal = true
		w.eventSuccessful = successful
	}
}

func (w *newAPIStyleSSETerminalCaptureWriter) finishLine() {
	if w == nil {
		return
	}
	if !w.discardLine && len(bytes.TrimSpace(w.pending)) == 0 {
		w.finishEvent()
	} else if !w.discardLine {
		w.consumeLine(w.pending)
	}
	w.pending = nil
	w.discardLine = false
	w.scanJSONStarted = false
	w.scanDepth = 0
	w.scanInString = false
	w.scanEscaped = false
	w.scanStringRole = 0
	w.scanString = w.scanString[:0]
	w.scanKey = ""
	w.scanWantKey = false
	w.scanWantValue = false
}

func (w *newAPIStyleSSETerminalCaptureWriter) State() (terminal bool, successful bool) {
	if w == nil {
		return false, false
	}
	return w.terminal, w.successful
}

func (w *newAPIStyleSSETerminalCaptureWriter) consumeLine(rawLine []byte) {
	terminal, successful := newAPIStyleTerminalStateFromLine(w.route, rawLine)
	if !terminal {
		return
	}
	if !w.eventTerminal || !successful {
		w.eventTerminal = true
		w.eventSuccessful = successful
	}
}

func (w *newAPIStyleSSETerminalCaptureWriter) finishEvent() {
	if w == nil || !w.eventTerminal {
		return
	}
	if !w.terminal || !w.eventSuccessful {
		w.terminal = true
		w.successful = w.eventSuccessful
	}
	w.eventTerminal = false
	w.eventSuccessful = false
}

func newAPIStyleTerminalStateFromLine(route NewAPIStyleRoute, rawLine []byte) (terminal bool, successful bool) {
	line := strings.TrimSpace(string(rawLine))
	if line == "" {
		return false, false
	}
	lowerLine := strings.ToLower(line)
	if strings.HasPrefix(lowerLine, "event:") {
		return newAPIStyleTerminalEventType(route, strings.TrimSpace(lowerLine[len("event:"):]))
	}
	data := line
	if strings.HasPrefix(lowerLine, "data:") {
		data = strings.TrimSpace(line[len("data:"):])
	}
	if strings.EqualFold(data, "[DONE]") {
		return route != NewAPIStyleRouteResponses && route != NewAPIStyleRouteMessages, true
	}
	if gjson.Valid(data) {
		return newAPIStyleTerminalEventType(route, strings.ToLower(strings.TrimSpace(gjson.Get(data, "type").String())))
	}
	compact := strings.NewReplacer(" ", "", "\t", "").Replace(strings.ToLower(data))
	for _, eventType := range []string{
		"response.failed",
		"response.incomplete",
		"response.cancelled",
		"response.canceled",
		"response.completed",
		"response.done",
		"message_stop",
		"error",
	} {
		if strings.Contains(compact, `"type":"`+eventType+`"`) {
			return newAPIStyleTerminalEventType(route, eventType)
		}
	}
	return false, false
}

func (w *tailBufferWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w == nil || w.buffer == nil || w.limit <= 0 {
		return n, nil
	}
	if len(p) >= w.limit {
		w.buffer.Reset()
		_, _ = w.buffer.Write(p[len(p)-w.limit:])
		return n, nil
	}
	overflow := w.buffer.Len() + len(p) - w.limit
	if overflow > 0 {
		current := append([]byte(nil), w.buffer.Bytes()...)
		if overflow > len(current) {
			overflow = len(current)
		}
		w.buffer.Reset()
		_, _ = w.buffer.Write(current[overflow:])
	}
	_, _ = w.buffer.Write(p)
	return n, nil
}

type newAPIStyleSSEUsageCaptureWriter struct {
	pending string
	usage   ClaudeUsage
	route   NewAPIStyleRoute
}

func (w *newAPIStyleSSEUsageCaptureWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w == nil || len(p) == 0 {
		return n, nil
	}
	w.pending += string(p)
	w.pending = strings.ReplaceAll(w.pending, "\r\n", "\n")
	if usage := parseNewAPIStyleUsageFromEventTail(w.pending, w.route); claudeUsageHasAny(usage) {
		w.usage = mergeNewAPIStyleUsage(w.usage, usage)
	}
	for {
		idx := strings.Index(w.pending, "\n\n")
		if idx < 0 {
			if len(w.pending) > 64*1024 {
				w.pending = w.pending[len(w.pending)-64*1024:]
			}
			return n, nil
		}
		event := w.pending[:idx]
		w.pending = w.pending[idx+2:]
		usage := parseNewAPIStyleSSEUsageEventForRoute(event, w.route)
		if !claudeUsageHasAny(usage) {
			usage = parseNewAPIStyleUsageFromEventTail(event, w.route)
		}
		if claudeUsageHasAny(usage) {
			w.usage = mergeNewAPIStyleUsage(w.usage, usage)
		}
	}
}

func (w *newAPIStyleSSEUsageCaptureWriter) Usage() ClaudeUsage {
	if w == nil {
		return ClaudeUsage{}
	}
	usage := parseNewAPIStyleSSEUsageEventForRoute(w.pending, w.route)
	if !claudeUsageHasAny(usage) {
		usage = parseNewAPIStyleUsageFromEventTail(w.pending, w.route)
	}
	if claudeUsageHasAny(usage) {
		return mergeNewAPIStyleUsage(w.usage, usage)
	}
	return w.usage
}

func parseNewAPIStyleUsageFromEventTail(event string, route NewAPIStyleRoute) ClaudeUsage {
	lower := strings.ToLower(event)
	usageKey := strings.LastIndex(lower, `"usage"`)
	if usageKey < 0 {
		return ClaudeUsage{}
	}
	colon := strings.IndexByte(event[usageKey+len(`"usage"`):], ':')
	if colon < 0 {
		return ClaudeUsage{}
	}
	start := usageKey + len(`"usage"`) + colon + 1
	for start < len(event) && (event[start] == ' ' || event[start] == '\t' || event[start] == '\r' || event[start] == '\n') {
		start++
	}
	if start >= len(event) || event[start] != '{' {
		return ClaudeUsage{}
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(event); i++ {
		ch := event[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				payload := `{"usage":` + event[start:i+1] + `}`
				return parseNewAPIStyleUsageForRoute([]byte(payload), route)
			}
		}
	}
	return ClaudeUsage{}
}

func requestContainsAudioInput(body []byte, contentType string) bool {
	if len(body) == 0 {
		return false
	}
	if isAudioContentType(contentType) {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return true
	}
	if !json.Valid(body) {
		return false
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "input_audio") ||
		strings.Contains(lower, "data:audio/") ||
		strings.Contains(lower, `"audio"`) ||
		strings.Contains(lower, `"audio_url"`)
}

func parseNewAPIStyleUsage(body []byte) ClaudeUsage {
	return parseNewAPIStyleUsageForRoute(body, NewAPIStyleRouteResponses)
}

func parseNewAPIStyleUsageForRoute(body []byte, route NewAPIStyleRoute) ClaudeUsage {
	usage := gjson.GetBytes(body, "usage")
	if !usage.Exists() {
		usage = gjson.GetBytes(body, "response.usage")
	}
	if !usage.Exists() {
		usage = gjson.GetBytes(body, "message.usage")
	}
	if !usage.Exists() {
		return ClaudeUsage{}
	}
	cacheRead := firstExistingGJSONInt(
		usage.Get("input_tokens_details.cached_tokens"),
		usage.Get("prompt_tokens_details.cached_tokens"),
		usage.Get("cache_read_input_tokens"),
		usage.Get("cached_tokens"),
	)
	cacheCreation5m := int(usage.Get("cache_creation.ephemeral_5m_input_tokens").Int())
	cacheCreation1h := int(usage.Get("cache_creation.ephemeral_1h_input_tokens").Int())
	cacheCreation := firstExistingGJSONInt(
		usage.Get("input_tokens_details.cache_write_tokens"),
		usage.Get("input_tokens_details.cache_creation_tokens"),
		usage.Get("input_tokens_details.cache_creation_input_tokens"),
		usage.Get("prompt_tokens_details.cache_write_tokens"),
		usage.Get("prompt_tokens_details.cache_creation_tokens"),
		usage.Get("prompt_tokens_details.cache_creation_input_tokens"),
		usage.Get("cache_write_input_tokens"),
		usage.Get("cache_write_tokens"),
		usage.Get("cache_creation_input_tokens"),
		usage.Get("cache_creation_tokens"),
	)
	if cacheCreation == 0 && (cacheCreation5m > 0 || cacheCreation1h > 0) {
		cacheCreation = cacheCreation5m + cacheCreation1h
	}
	inputResult := usage.Get("input_tokens")
	input := int(inputResult.Int())
	usesOpenAITotalInput := route != NewAPIStyleRouteMessages && inputResult.Exists() && newAPIStyleCacheBreakdownExists(usage)
	if input == 0 {
		input = int(usage.Get("prompt_tokens").Int())
		usesOpenAITotalInput = true
	}
	if usesOpenAITotalInput {
		input = openAIUncachedInputTokens(input, cacheRead+cacheCreation)
	}
	return ClaudeUsage{
		InputTokens:              input,
		OutputTokens:             int(firstPositiveInt(usage.Get("output_tokens").Int(), usage.Get("completion_tokens").Int())),
		CacheCreationInputTokens: cacheCreation,
		CacheReadInputTokens:     cacheRead,
		CacheCreation5mTokens:    cacheCreation5m,
		CacheCreation1hTokens:    cacheCreation1h,
		ImageOutputTokens: int(firstPositiveInt(
			usage.Get("image_output_tokens").Int(),
			usage.Get("output_tokens_details.image_tokens").Int(),
			usage.Get("completion_tokens_details.image_tokens").Int(),
		)),
	}
}

func newAPIStyleCacheBreakdownExists(usage gjson.Result) bool {
	for _, result := range []gjson.Result{
		usage.Get("input_tokens_details.cached_tokens"),
		usage.Get("prompt_tokens_details.cached_tokens"),
		usage.Get("cache_read_input_tokens"),
		usage.Get("cached_tokens"),
		usage.Get("input_tokens_details.cache_write_tokens"),
		usage.Get("input_tokens_details.cache_creation_tokens"),
		usage.Get("input_tokens_details.cache_creation_input_tokens"),
		usage.Get("prompt_tokens_details.cache_write_tokens"),
		usage.Get("prompt_tokens_details.cache_creation_tokens"),
		usage.Get("prompt_tokens_details.cache_creation_input_tokens"),
		usage.Get("cache_write_input_tokens"),
		usage.Get("cache_write_tokens"),
		usage.Get("cache_creation_input_tokens"),
		usage.Get("cache_creation_tokens"),
		usage.Get("cache_creation.ephemeral_5m_input_tokens"),
		usage.Get("cache_creation.ephemeral_1h_input_tokens"),
	} {
		if result.Exists() {
			return true
		}
	}
	return false
}

func parseNewAPIStyleSSEUsage(body []byte) ClaudeUsage {
	return parseNewAPIStyleSSEUsageForRoute(body, NewAPIStyleRouteMessages)
}

func parseNewAPIStyleSSEUsageForRoute(body []byte, route NewAPIStyleRoute) ClaudeUsage {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	var out ClaudeUsage
	for _, event := range strings.Split(text, "\n\n") {
		if usage := parseNewAPIStyleSSEUsageEventForRoute(event, route); claudeUsageHasAny(usage) {
			out = mergeNewAPIStyleUsage(out, usage)
		}
	}
	return out
}

func parseNewAPIStyleSSEUsageEventForRoute(event string, route NewAPIStyleRoute) ClaudeUsage {
	var dataLines []string
	for _, line := range strings.Split(event, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		dataLines = append(dataLines, data)
	}
	if len(dataLines) == 0 {
		return ClaudeUsage{}
	}
	return parseNewAPIStyleUsageForRoute([]byte(strings.Join(dataLines, "\n")), route)
}

func mergeNewAPIStyleUsage(base, next ClaudeUsage) ClaudeUsage {
	if next.InputTokens > 0 {
		base.InputTokens = next.InputTokens
	}
	if next.OutputTokens > base.OutputTokens {
		base.OutputTokens = next.OutputTokens
	}
	if next.CacheCreationInputTokens > base.CacheCreationInputTokens {
		base.CacheCreationInputTokens = next.CacheCreationInputTokens
	}
	if next.CacheReadInputTokens > base.CacheReadInputTokens {
		base.CacheReadInputTokens = next.CacheReadInputTokens
	}
	if next.CacheCreation5mTokens > base.CacheCreation5mTokens {
		base.CacheCreation5mTokens = next.CacheCreation5mTokens
	}
	if next.CacheCreation1hTokens > base.CacheCreation1hTokens {
		base.CacheCreation1hTokens = next.CacheCreation1hTokens
	}
	if next.ImageOutputTokens > base.ImageOutputTokens {
		base.ImageOutputTokens = next.ImageOutputTokens
	}
	return base
}

func claudeUsageHasAny(usage ClaudeUsage) bool {
	return usage.InputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 ||
		usage.CacheReadInputTokens > 0 ||
		usage.CacheCreation5mTokens > 0 ||
		usage.CacheCreation1hTokens > 0 ||
		usage.ImageOutputTokens > 0
}

func normalizeAliQwenImagesOpenAIResponse(account *Account, opts NewAPIStyleForwardOptions, body []byte) ([]byte, bool) {
	if account == nil || account.Platform != PlatformAli || opts.Route != NewAPIStyleRouteImages {
		return nil, false
	}
	data := aliQwenImageDataItems(body)
	if len(data) == 0 {
		return nil, false
	}
	payload := map[string]any{
		"created": time.Now().Unix(),
		"data":    data,
	}
	if id := strings.TrimSpace(gjson.GetBytes(body, "request_id").String()); id != "" {
		payload["id"] = id
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return out, true
}

func aliQwenImageDataItems(body []byte) []map[string]any {
	var data []map[string]any
	for _, choice := range gjson.GetBytes(body, "output.choices").Array() {
		for _, part := range choice.Get("message.content").Array() {
			if item := aliQwenImageDataItem(part); item != nil {
				data = append(data, item)
			}
		}
	}
	if len(data) > 0 {
		return data
	}
	for _, path := range []string{"output.image", "output.image_url", "output.url", "image", "image_url", "url"} {
		if item := aliQwenImageDataItem(gjson.GetBytes(body, path)); item != nil {
			return []map[string]any{item}
		}
	}
	return nil
}

func aliQwenImageDataItem(value gjson.Result) map[string]any {
	if !value.Exists() {
		return nil
	}
	var urlValue string
	var b64Value string
	if value.Type == gjson.String {
		urlValue = strings.TrimSpace(value.String())
	} else {
		for _, key := range []string{"image", "image_url", "url"} {
			if s := strings.TrimSpace(value.Get(key).String()); s != "" {
				urlValue = s
				break
			}
		}
		for _, key := range []string{"b64_json", "base64", "data"} {
			if s := strings.TrimSpace(value.Get(key).String()); s != "" {
				b64Value = s
				break
			}
		}
	}
	if urlValue == "" && b64Value == "" {
		return nil
	}
	item := make(map[string]any, 1)
	if urlValue != "" {
		item["url"] = urlValue
	}
	if b64Value != "" {
		item["b64_json"] = b64Value
	}
	return item
}

func inferImageCount(requestBody, responseBody []byte) int {
	for _, path := range []string{"usage.image_count", "output.usage.image_count"} {
		if n := int(gjson.GetBytes(responseBody, path).Int()); n > 0 {
			return n
		}
	}
	if data := gjson.GetBytes(responseBody, "data"); data.IsArray() {
		count := 0
		data.ForEach(func(_, _ gjson.Result) bool {
			count++
			return true
		})
		if count > 0 {
			return count
		}
	}
	if items := aliQwenImageDataItems(responseBody); len(items) > 0 {
		return len(items)
	}
	if n := int(gjson.GetBytes(requestBody, "n").Int()); n > 0 {
		return n
	}
	if n := int(gjson.GetBytes(requestBody, "parameters.n").Int()); n > 0 {
		return n
	}
	return 1
}

func inferImageSize(requestBody []byte) string {
	size := strings.ToLower(strings.TrimSpace(firstNonEmptyText(
		gjson.GetBytes(requestBody, "size").String(),
		gjson.GetBytes(requestBody, "parameters.size").String(),
	)))
	switch {
	case strings.Contains(size, "4096"), strings.Contains(size, "4k"):
		return "4K"
	case strings.Contains(size, "2048"), strings.Contains(size, "2k"):
		return "2K"
	default:
		return "1K"
	}
}

func inferImageRequestedSize(requestBody []byte) string {
	size := strings.TrimSpace(firstNonEmptyText(
		gjson.GetBytes(requestBody, "size").String(),
		gjson.GetBytes(requestBody, "parameters.size").String(),
	))
	if size == "" {
		return ""
	}
	normalized := strings.ToLower(strings.ReplaceAll(size, "*", "x"))
	if width, height, ok := parseImageBillingDimensions(normalized); ok {
		return fmt.Sprintf("%dx%d", width, height)
	}
	if tier, ok := ClassifyImageBillingTier(normalized); ok {
		return tier
	}
	return size
}

func estimateNewAPIStyleInputTokens(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	compact := bytes.TrimSpace(body)
	if len(compact) == 0 {
		return 0
	}
	estimated := len(compact) / 4
	if estimated < 1 {
		estimated = 1
	}
	return estimated
}

func forwardResultHasBillableUsage(result *ForwardResult) bool {
	if result == nil {
		return false
	}
	return result.Usage.InputTokens > 0 ||
		result.Usage.OutputTokens > 0 ||
		result.Usage.CacheCreationInputTokens > 0 ||
		result.Usage.CacheReadInputTokens > 0 ||
		result.Usage.CacheCreation5mTokens > 0 ||
		result.Usage.CacheCreation1hTokens > 0 ||
		result.Usage.ImageOutputTokens > 0 ||
		result.ImageCount > 0 ||
		result.RequestCount > 0 ||
		result.TaskCount > 0 ||
		result.BillableDurationSeconds > 0 ||
		result.BillableCharacterCount > 0
}

func isEventStream(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "text/event-stream")
	}
	return strings.EqualFold(mediaType, "text/event-stream")
}

func writeResponseHeaders(c *gin.Context, header http.Header) {
	if c == nil || header == nil {
		return
	}
	for key, values := range header {
		lower := strings.ToLower(key)
		if lower == "content-length" || lower == "transfer-encoding" || lower == "connection" {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveInt(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func NewAPIStyleTaskSummary(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
