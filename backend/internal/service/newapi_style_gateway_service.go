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
	NewAPIStyleRouteEmbeddings      NewAPIStyleRoute = "embeddings"
	NewAPIStyleRouteRerank          NewAPIStyleRoute = "rerank"
	NewAPIStyleRouteVideo           NewAPIStyleRoute = "video"
	NewAPIStyleRouteSuno            NewAPIStyleRoute = "suno"
	NewAPIStyleRouteKling           NewAPIStyleRoute = "kling"
	NewAPIStyleRouteMidjourney      NewAPIStyleRoute = "midjourney"
	NewAPIStyleRouteTask            NewAPIStyleRoute = "task"
)

const (
	BillableUnitTypeToken   = "token"
	BillableUnitTypeImage   = "image"
	BillableUnitTypeRequest = "request"
	BillableUnitTypeTask    = "task"
)

const (
	zhipuAudioTranscriptionsPath = "/api/paas/v4/audio/transcriptions"
	zhipuAudioSpeechPath         = "/api/paas/v4/audio/speech"

	maxNewAPIStyleMultipartModelBytes = 64 << 10
)

var ErrNewAPIStyleUnsupportedCapability = errors.New("new-api style capability unsupported")

type NewAPIStyleGatewayService struct {
	gatewayService      *GatewayService
	httpUpstream        HTTPUpstream
	cfg                 *config.Config
	tlsFPProfileService *TLSFingerprintProfileService
}

func NewNewAPIStyleGatewayService(
	gatewayService *GatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFingerprintProfileService *TLSFingerprintProfileService,
) *NewAPIStyleGatewayService {
	return &NewAPIStyleGatewayService{
		gatewayService:      gatewayService,
		httpUpstream:        httpUpstream,
		cfg:                 cfg,
		tlsFPProfileService: tlsFingerprintProfileService,
	}
}

type NewAPIStyleForwardOptions struct {
	Route        NewAPIStyleRoute
	Group        *Group
	RequestBody  []byte
	Stream       bool
	Model        string
	ImageSize    string
	Method       string
	InboundPath  string
	QueryString  string
	ForceTask    bool
	ContentType  string
	HeaderSource http.Header
}

func (s *NewAPIStyleGatewayService) Supports(account *Account, route NewAPIStyleRoute) bool {
	return s.SupportsForGroup(account, nil, route)
}

func (s *NewAPIStyleGatewayService) SupportsForGroup(account *Account, group *Group, route NewAPIStyleRoute) bool {
	if account == nil || !account.UseNewAPIStyleInterfaceForGroup(group) {
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
		return account.Platform == PlatformOpenAI || account.Platform == PlatformXAI
	case NewAPIStyleRouteImages, NewAPIStyleRouteEmbeddings, NewAPIStyleRouteVideo:
		return account.Platform == PlatformOpenAI
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

	targetURL, upstreamEndpoint, err := s.buildTargetURL(account, opts)
	if err != nil {
		return nil, upstreamEndpoint, err
	}
	body := opts.RequestBody
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
		return nil, upstreamEndpoint, fmt.Errorf("new-api style upstream request failed: %w", err)
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
		eventKind := "http_error"
		if s.gatewayService != nil && s.gatewayService.shouldFailoverUpstreamError(resp.StatusCode) {
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
		return nil, upstreamEndpoint, &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    respBody,
			ResponseHeaders: resp.Header.Clone(),
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
		if c != nil {
			c.Status(resp.StatusCode)
			_, _ = io.Copy(c.Writer, resp.Body)
			if f, ok := c.Writer.(http.Flusher); ok {
				f.Flush()
			}
		} else {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		s.applyUsageGuardrails(result, opts, nil)
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
	s.applyUsageGuardrails(result, opts, respBody)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	if c != nil {
		c.Data(resp.StatusCode, contentType, respBody)
	}
	result.Duration = time.Since(start)
	return result, upstreamEndpoint, nil
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
	return &OpenAIForwardResult{
		RequestID: result.RequestID,
		Usage: OpenAIUsage{
			InputTokens:              result.Usage.InputTokens,
			OutputTokens:             result.Usage.OutputTokens,
			CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
			ImageOutputTokens:        result.Usage.ImageOutputTokens,
		},
		Model:            result.Model,
		UpstreamModel:    result.UpstreamModel,
		ReasoningEffort:  result.ReasoningEffort,
		Stream:           result.Stream,
		Duration:         result.Duration,
		FirstTokenMs:     result.FirstTokenMs,
		ImageCount:       result.ImageCount,
		ImageSize:        result.ImageSize,
		RequestCount:     result.RequestCount,
		TaskCount:        result.TaskCount,
		UsageEstimated:   result.UsageEstimated,
		BillableUnitType: result.BillableUnitType,
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
	baseURL = strings.TrimRight(baseURL, "/")

	path := newAPIStyleRoutePath(account.Platform, opts)
	if path == "" {
		if opts.Route == NewAPIStyleRouteAudio {
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
		if strings.Contains(inbound, "/edits") {
			return "/v1/images/edits"
		}
		return "/v1/images/generations"
	case NewAPIStyleRouteAudio:
		return newAPIStyleAudioRoutePath(platform, inbound)
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
	return strings.TrimSpace(gjson.GetBytes(body, "model").String())
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
	if len(respBody) > 0 {
		usage := parseNewAPIStyleUsage(respBody)
		result.Usage.InputTokens = usage.InputTokens
		result.Usage.OutputTokens = usage.OutputTokens
		result.Usage.CacheCreationInputTokens = usage.CacheCreationInputTokens
		result.Usage.CacheReadInputTokens = usage.CacheReadInputTokens
		result.Usage.ImageOutputTokens = usage.ImageOutputTokens
	}

	switch opts.Route {
	case NewAPIStyleRouteImages:
		result.ImageCount = inferImageCount(opts.RequestBody, respBody)
		result.ImageSize = inferImageSize(opts.RequestBody)
		result.BillableUnitType = BillableUnitTypeImage
	case NewAPIStyleRouteAudio, NewAPIStyleRouteEmbeddings, NewAPIStyleRouteRerank:
		result.RequestCount = 1
		result.BillableUnitType = BillableUnitTypeRequest
	case NewAPIStyleRouteVideo, NewAPIStyleRouteSuno, NewAPIStyleRouteKling, NewAPIStyleRouteMidjourney, NewAPIStyleRouteTask:
		result.TaskCount = 1
		result.BillableUnitType = BillableUnitTypeTask
	default:
		result.BillableUnitType = BillableUnitTypeToken
	}

	if !forwardResultHasBillableUsage(result) {
		result.Usage.InputTokens = estimateNewAPIStyleInputTokens(opts.RequestBody)
		result.UsageEstimated = true
		if result.Usage.InputTokens == 0 && opts.Route != NewAPIStyleRouteImages {
			result.RequestCount = 1
			result.BillableUnitType = BillableUnitTypeRequest
		}
	}
}

func parseNewAPIStyleUsage(body []byte) ClaudeUsage {
	usage := gjson.GetBytes(body, "usage")
	if !usage.Exists() {
		return ClaudeUsage{}
	}
	input := int(firstPositiveInt(
		usage.Get("input_tokens").Int(),
		usage.Get("prompt_tokens").Int(),
	))
	cacheRead := int(firstPositiveInt(
		usage.Get("cache_read_input_tokens").Int(),
		usage.Get("prompt_tokens_details.cached_tokens").Int(),
		usage.Get("input_tokens_details.cached_tokens").Int(),
	))
	if input >= cacheRead && cacheRead > 0 {
		input -= cacheRead
	}
	return ClaudeUsage{
		InputTokens:              input,
		OutputTokens:             int(firstPositiveInt(usage.Get("output_tokens").Int(), usage.Get("completion_tokens").Int())),
		CacheCreationInputTokens: int(usage.Get("cache_creation_input_tokens").Int()),
		CacheReadInputTokens:     cacheRead,
		ImageOutputTokens:        int(usage.Get("image_output_tokens").Int()),
	}
}

func inferImageCount(requestBody, responseBody []byte) int {
	if n := int(gjson.GetBytes(requestBody, "n").Int()); n > 0 {
		return n
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
	return 1
}

func inferImageSize(requestBody []byte) string {
	size := strings.ToLower(strings.TrimSpace(gjson.GetBytes(requestBody, "size").String()))
	switch {
	case strings.Contains(size, "4096"), strings.Contains(size, "4k"):
		return "4K"
	case strings.Contains(size, "2048"), strings.Contains(size, "2k"):
		return "2K"
	default:
		return "1K"
	}
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
		result.TaskCount > 0
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
