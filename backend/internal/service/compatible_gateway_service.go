package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type CompatibleGatewayService struct {
	gatewayService      *GatewayService
	httpUpstream        HTTPUpstream
	cfg                 *config.Config
	tlsFPProfileService *TLSFingerprintProfileService
	endpointModeCache   sync.Map
}

type CompatibleUpstreamError struct {
	StatusCode   int
	Message      string
	ResponseBody []byte
}

func (e *CompatibleUpstreamError) Error() string {
	if e == nil {
		return "compatible upstream error"
	}
	return fmt.Sprintf("compatible upstream error: %d %s", e.StatusCode, e.Message)
}

type compatibleUpstreamKind string

const (
	compatibleUpstreamChat      compatibleUpstreamKind = "chat"
	compatibleUpstreamResponses compatibleUpstreamKind = "responses"
	compatibleUpstreamMessages  compatibleUpstreamKind = "messages"
)

type compatibleEndpointMode string

const (
	compatibleEndpointModeNative compatibleEndpointMode = "native"
	compatibleEndpointModeRelay  compatibleEndpointMode = "relay"
)

type compatibleEndpointModeCacheEntry struct {
	Mode      compatibleEndpointMode
	UpdatedAt time.Time
}

type compatibleURLCandidate struct {
	URL  string
	Mode compatibleEndpointMode
}

type compatiblePreparedRequest struct {
	OriginalModel    string
	UpstreamModel    string
	ClientStream     bool
	UpstreamKind     compatibleUpstreamKind
	UpstreamEndpoint string
	RequestBody      []byte
	URL              string
}

func NewCompatibleGatewayService(
	gatewayService *GatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFPProfileService *TLSFingerprintProfileService,
) *CompatibleGatewayService {
	return &CompatibleGatewayService{
		gatewayService:      gatewayService,
		httpUpstream:        httpUpstream,
		cfg:                 cfg,
		tlsFPProfileService: tlsFPProfileService,
	}
}

func (s *CompatibleGatewayService) TempUnscheduleRetryableError(ctx context.Context, accountID int64, failoverErr *UpstreamFailoverError) {
	if s == nil || s.gatewayService == nil {
		return
	}
	s.gatewayService.TempUnscheduleRetryableError(ctx, accountID, failoverErr)
}

func (s *CompatibleGatewayService) DefaultModels(platform string) []claude.Model {
	models := CompatibleDefaultModels(platform)
	return models
}

func (s *CompatibleGatewayService) AvailableModelsForAccount(account *Account) []claude.Model {
	if account == nil {
		return nil
	}
	defaultModels := CompatibleDefaultModels(account.Platform)
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		return defaultModels
	}
	modelIndex := make(map[string]claude.Model, len(defaultModels))
	for _, model := range defaultModels {
		modelIndex[model.ID] = model
	}
	out := make([]claude.Model, 0, len(mapping))
	for requestedModel := range mapping {
		if model, ok := modelIndex[requestedModel]; ok {
			out = append(out, model)
			continue
		}
		out = append(out, claude.Model{
			ID:          requestedModel,
			Type:        "model",
			DisplayName: requestedModel,
			CreatedAt:   "",
		})
	}
	return out
}

func (s *CompatibleGatewayService) Forward(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	route CompatibleRequestRoute,
	body []byte,
) (*ForwardResult, string, error) {
	prepared, err := s.prepareRequest(account, route, body)
	if err != nil {
		return nil, "", err
	}

	baseURL, err := s.gatewayService.validateUpstreamBaseURL(account.GetCompatibleBaseURL())
	if err != nil {
		return nil, prepared.UpstreamEndpoint, err
	}
	proxyURL := resolveAccountProxyURL(ctx, account, nil)
	urlCandidates := s.buildURLCandidatesForPreparedRequest(account, prepared, baseURL)

	var resp *http.Response
	for idx, candidate := range urlCandidates {
		prepared.URL = candidate.URL

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, prepared.URL, bytes.NewReader(prepared.RequestBody))
		if err != nil {
			return nil, prepared.UpstreamEndpoint, err
		}
		req.Header.Set("Content-Type", "application/json")
		if prepared.ClientStream {
			req.Header.Set("Accept", "text/event-stream")
		}
		if err := s.applyAuth(req, account); err != nil {
			return nil, prepared.UpstreamEndpoint, err
		}
		s.applyHeaderPatches(req, account, prepared)

		resp, err = s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
		if err != nil {
			return nil, prepared.UpstreamEndpoint, &CompatibleUpstreamError{
				StatusCode: http.StatusBadGateway,
				Message:    sanitizeUpstreamErrorMessage(err.Error()),
			}
		}

		if resp.StatusCode < 400 {
			s.recordEndpointMode(account, prepared, baseURL, candidate.Mode)
			break
		}

		statusCode := resp.StatusCode
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		resp = nil

		if idx == 0 && shouldRetryViaRelayCompatibleEndpoint(prepared, statusCode, respBody) {
			continue
		}

		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if upstreamMsg == "" {
			upstreamMsg = http.StatusText(statusCode)
		}
		if s.gatewayService.shouldFailoverUpstreamError(statusCode) {
			return nil, prepared.UpstreamEndpoint, &UpstreamFailoverError{
				StatusCode:   statusCode,
				ResponseBody: respBody,
			}
		}
		return nil, prepared.UpstreamEndpoint, &CompatibleUpstreamError{
			StatusCode:   mapUpstreamStatusCode(statusCode),
			Message:      upstreamMsg,
			ResponseBody: respBody,
		}
	}

	if resp == nil {
		return nil, prepared.UpstreamEndpoint, &CompatibleUpstreamError{
			StatusCode: http.StatusBadGateway,
			Message:    "compatible upstream error",
		}
	}
	defer func() { _ = resp.Body.Close() }()

	switch prepared.UpstreamKind {
	case compatibleUpstreamMessages:
		return s.handleMessagesResponse(resp, c, prepared), prepared.UpstreamEndpoint, nil
	case compatibleUpstreamResponses:
		return s.handleResponsesResponse(resp, c, prepared), prepared.UpstreamEndpoint, nil
	case compatibleUpstreamChat:
		switch route {
		case CompatibleRouteChatCompletions:
			return s.handleChatPassthrough(resp, c, prepared), prepared.UpstreamEndpoint, nil
		case CompatibleRouteResponses:
			return s.handleChatAsResponses(resp, c, prepared), prepared.UpstreamEndpoint, nil
		case CompatibleRouteMessages:
			return s.handleChatAsMessages(resp, c, prepared), prepared.UpstreamEndpoint, nil
		}
	}
	return nil, prepared.UpstreamEndpoint, fmt.Errorf("unsupported compatible route")
}

func (s *CompatibleGatewayService) prepareRequest(account *Account, route CompatibleRequestRoute, body []byte) (*compatiblePreparedRequest, error) {
	if account == nil {
		return nil, fmt.Errorf("account is nil")
	}
	preset, err := getCompatiblePreset(account)
	if err != nil {
		return nil, err
	}

	clientStream := gjson.GetBytes(body, "stream").Bool()
	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	upstreamModel := originalModel
	if account.Type == AccountTypeAPIKey && originalModel != "" {
		upstreamModel = account.GetMappedModel(originalModel)
	}
	if upstreamModel == "" {
		upstreamModel = originalModel
	}

	prepared := &compatiblePreparedRequest{
		OriginalModel: originalModel,
		UpstreamModel: upstreamModel,
		ClientStream:  clientStream,
	}

	switch route {
	case CompatibleRouteChatCompletions:
		prepared.UpstreamKind = compatibleUpstreamChat
		prepared.UpstreamEndpoint = "/v1/chat/completions"
		prepared.RequestBody, err = rewriteCompatibleRequestModel(body, originalModel, upstreamModel)
		if err != nil {
			return nil, err
		}
		if preset.PatchChatBody != nil {
			preparedBody, err := preset.PatchChatBody(prepared.RequestBody, account, upstreamModel)
			if err != nil {
				return nil, err
			}
			prepared.RequestBody = preparedBody
		}
	case CompatibleRouteResponses:
		prepared.UpstreamEndpoint = "/v1/responses"
		if preset.SupportsResponses {
			prepared.UpstreamKind = compatibleUpstreamResponses
			prepared.RequestBody, err = rewriteCompatibleRequestModel(body, originalModel, upstreamModel)
			if err != nil {
				return nil, err
			}
			if preset.PatchResponsesBody != nil {
				preparedBody, err := preset.PatchResponsesBody(prepared.RequestBody, account, upstreamModel)
				if err != nil {
					return nil, err
				}
				prepared.RequestBody = preparedBody
			}
		} else {
			var responsesReq apicompat.ResponsesRequest
			if err := json.Unmarshal(body, &responsesReq); err != nil {
				return nil, fmt.Errorf("parse responses request: %w", err)
			}
			chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
			if err != nil {
				return nil, err
			}
			chatReq.Model = upstreamModel
			chatBody, err := json.Marshal(chatReq)
			if err != nil {
				return nil, err
			}
			prepared.UpstreamKind = compatibleUpstreamChat
			prepared.UpstreamEndpoint = "/v1/chat/completions"
			prepared.RequestBody = chatBody
			if preset.PatchChatBody != nil {
				preparedBody, err := preset.PatchChatBody(prepared.RequestBody, account, upstreamModel)
				if err != nil {
					return nil, err
				}
				prepared.RequestBody = preparedBody
			}
		}
	case CompatibleRouteMessages:
		prepared.UpstreamEndpoint = "/v1/messages"
		if preset.SupportsMessages != nil && preset.SupportsMessages(upstreamModel) {
			prepared.UpstreamKind = compatibleUpstreamMessages
			prepared.RequestBody, err = rewriteCompatibleRequestModel(body, originalModel, upstreamModel)
			if err != nil {
				return nil, err
			}
			if preset.PatchMessagesBody != nil {
				preparedBody, err := preset.PatchMessagesBody(prepared.RequestBody, account, upstreamModel)
				if err != nil {
					return nil, err
				}
				prepared.RequestBody = preparedBody
			}
		} else {
			var anthropicReq apicompat.AnthropicRequest
			if err := json.Unmarshal(body, &anthropicReq); err != nil {
				return nil, fmt.Errorf("parse anthropic request: %w", err)
			}
			responsesReq, err := apicompat.AnthropicToResponses(&anthropicReq)
			if err != nil {
				return nil, err
			}
			chatReq, err := apicompat.ResponsesToChatCompletionsRequest(responsesReq)
			if err != nil {
				return nil, err
			}
			chatReq.Model = upstreamModel
			chatBody, err := json.Marshal(chatReq)
			if err != nil {
				return nil, err
			}
			prepared.UpstreamKind = compatibleUpstreamChat
			prepared.UpstreamEndpoint = "/v1/chat/completions"
			prepared.RequestBody = chatBody
			if preset.PatchChatBody != nil {
				preparedBody, err := preset.PatchChatBody(prepared.RequestBody, account, upstreamModel)
				if err != nil {
					return nil, err
				}
				prepared.RequestBody = preparedBody
			}
		}
	default:
		return nil, fmt.Errorf("unsupported compatible route: %s", route)
	}

	return prepared, nil
}

func rewriteCompatibleRequestModel(body []byte, originalModel, upstreamModel string) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	if strings.TrimSpace(originalModel) == "" || strings.TrimSpace(upstreamModel) == "" || originalModel == upstreamModel {
		return body, nil
	}
	return sjson.SetBytes(body, "model", upstreamModel)
}

func (s *CompatibleGatewayService) buildURLForPreparedRequest(account *Account, prepared *compatiblePreparedRequest, baseURL string) string {
	preset, _ := getCompatiblePreset(account)
	switch prepared.UpstreamKind {
	case compatibleUpstreamMessages:
		return preset.BuildMessagesURL(baseURL, prepared.UpstreamModel)
	case compatibleUpstreamResponses:
		return preset.BuildResponsesURL(baseURL, prepared.UpstreamModel)
	default:
		return preset.BuildChatURL(baseURL, prepared.UpstreamModel)
	}
}

func (s *CompatibleGatewayService) buildURLCandidatesForPreparedRequest(account *Account, prepared *compatiblePreparedRequest, baseURL string) []compatibleURLCandidate {
	primary := s.buildURLForPreparedRequest(account, prepared, baseURL)
	fallback := buildRelayCompatibleFallbackURL(baseURL, prepared.UpstreamKind)
	if fallback == "" || fallback == primary {
		return []compatibleURLCandidate{{URL: primary, Mode: compatibleEndpointModeNative}}
	}
	if s.preferredEndpointMode(account, prepared, baseURL) == compatibleEndpointModeRelay {
		return []compatibleURLCandidate{
			{URL: fallback, Mode: compatibleEndpointModeRelay},
			{URL: primary, Mode: compatibleEndpointModeNative},
		}
	}
	return []compatibleURLCandidate{
		{URL: primary, Mode: compatibleEndpointModeNative},
		{URL: fallback, Mode: compatibleEndpointModeRelay},
	}
}

func buildRelayCompatibleFallbackURL(baseURL string, kind compatibleUpstreamKind) string {
	switch kind {
	case compatibleUpstreamMessages:
		return joinRelayCompatibleURL(baseURL, "/v1/messages")
	case compatibleUpstreamResponses:
		return joinRelayCompatibleURL(baseURL, "/v1/responses")
	default:
		return joinRelayCompatibleURL(baseURL, "/v1/chat/completions")
	}
}

func joinRelayCompatibleURL(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}

	lowerBase := strings.ToLower(baseURL)
	lowerEndpoint := strings.ToLower(endpoint)
	if strings.HasSuffix(lowerBase, lowerEndpoint) {
		return baseURL
	}
	if strings.HasSuffix(lowerBase, "/v1") && strings.HasPrefix(lowerEndpoint, "/v1/") {
		return baseURL + endpoint[len("/v1"):]
	}
	return baseURL + endpoint
}

func shouldRetryViaRelayCompatibleEndpoint(prepared *compatiblePreparedRequest, statusCode int, respBody []byte) bool {
	if prepared == nil {
		return false
	}
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	if statusCode != http.StatusBadRequest {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
	if msg == "" {
		msg = strings.ToLower(strings.TrimSpace(string(respBody)))
	}
	return strings.Contains(msg, "path") ||
		strings.Contains(msg, "route") ||
		strings.Contains(msg, "endpoint") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "unsupported")
}

func (s *CompatibleGatewayService) endpointModeCacheKey(account *Account, prepared *compatiblePreparedRequest, baseURL string) string {
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	upstreamKind := compatibleUpstreamChat
	if prepared != nil {
		upstreamKind = prepared.UpstreamKind
	}
	return fmt.Sprintf("%d|%s|%s", accountID, strings.TrimSpace(baseURL), upstreamKind)
}

func (s *CompatibleGatewayService) preferredEndpointMode(account *Account, prepared *compatiblePreparedRequest, baseURL string) compatibleEndpointMode {
	if s == nil {
		return compatibleEndpointModeNative
	}
	key := s.endpointModeCacheKey(account, prepared, baseURL)
	raw, ok := s.endpointModeCache.Load(key)
	if !ok {
		return compatibleEndpointModeNative
	}
	entry, ok := raw.(compatibleEndpointModeCacheEntry)
	if !ok {
		s.endpointModeCache.Delete(key)
		return compatibleEndpointModeNative
	}
	if entry.Mode == compatibleEndpointModeRelay {
		return compatibleEndpointModeRelay
	}
	return compatibleEndpointModeNative
}

func (s *CompatibleGatewayService) recordEndpointMode(account *Account, prepared *compatiblePreparedRequest, baseURL string, mode compatibleEndpointMode) {
	if s == nil {
		return
	}
	s.endpointModeCache.Store(s.endpointModeCacheKey(account, prepared, baseURL), compatibleEndpointModeCacheEntry{
		Mode:      mode,
		UpdatedAt: time.Now(),
	})
}

func (s *CompatibleGatewayService) InvalidateEndpointModeCacheForAccount(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	prefix := fmt.Sprintf("%d|", accountID)
	s.endpointModeCache.Range(func(key, _ any) bool {
		keyStr, ok := key.(string)
		if ok && strings.HasPrefix(keyStr, prefix) {
			s.endpointModeCache.Delete(key)
		}
		return true
	})
}

func (s *CompatibleGatewayService) applyAuth(req *http.Request, account *Account) error {
	if req == nil || account == nil {
		return fmt.Errorf("nil request/account")
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	preset, err := getCompatiblePreset(account)
	if err != nil {
		return err
	}
	apiKey = getCompatibleAuthToken(account, preset.AuthMode)
	if apiKey == "" {
		return fmt.Errorf("api_key not found in credentials")
	}
	switch preset.AuthMode {
	case CompatibleAuthBearer, CompatibleAuthZhipuToken:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	default:
		return fmt.Errorf("unsupported compatible auth mode: %s", preset.AuthMode)
	}
	return nil
}

func (s *CompatibleGatewayService) applyHeaderPatches(req *http.Request, account *Account, prepared *compatiblePreparedRequest) {
	preset, err := getCompatiblePreset(account)
	if err != nil {
		return
	}
	switch prepared.UpstreamKind {
	case compatibleUpstreamMessages:
		if preset.PatchMessagesHeaders != nil {
			preset.PatchMessagesHeaders(req, account, prepared.UpstreamModel)
		}
	case compatibleUpstreamResponses:
		if preset.PatchResponsesHeaders != nil {
			preset.PatchResponsesHeaders(req, account, prepared.UpstreamModel)
		}
	default:
		if preset.PatchChatHeaders != nil {
			preset.PatchChatHeaders(req, account, prepared.UpstreamModel)
		}
	}
}

func (s *CompatibleGatewayService) handleMessagesResponse(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest) *ForwardResult {
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	usage := ClaudeUsage{}
	if prepared.ClientStream {
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				s.gatewayService.parseSSEUsage(strings.TrimPrefix(line, "data: "), &usage)
			}
			_, _ = fmt.Fprintln(c.Writer, line)
			c.Writer.Flush()
		}
		return &ForwardResult{
			RequestID:     resp.Header.Get("x-request-id"),
			Usage:         usage,
			Model:         prepared.OriginalModel,
			UpstreamModel: prepared.UpstreamModel,
			Stream:        true,
		}
	}

	body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	if parsed := parseClaudeUsageFromResponseBody(body); parsed != nil {
		usage = *parsed
	}
	return &ForwardResult{
		RequestID:     resp.Header.Get("x-request-id"),
		Usage:         usage,
		Model:         prepared.OriginalModel,
		UpstreamModel: prepared.UpstreamModel,
		Stream:        false,
	}
}

func (s *CompatibleGatewayService) handleResponsesResponse(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest) *ForwardResult {
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	if prepared.ClientStream {
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		usage := ClaudeUsage{}
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				if gjson.Get(payload, "response.usage").Exists() {
					usage.InputTokens = firstExistingGJSONInt(
						gjson.Get(payload, "response.usage.input_tokens"),
						gjson.Get(payload, "response.usage.prompt_tokens"),
					)
					usage.OutputTokens = firstExistingGJSONInt(
						gjson.Get(payload, "response.usage.output_tokens"),
						gjson.Get(payload, "response.usage.completion_tokens"),
					)
					usage.CacheReadInputTokens = firstExistingGJSONInt(
						gjson.Get(payload, "response.usage.input_tokens_details.cached_tokens"),
						gjson.Get(payload, "response.usage.prompt_tokens_details.cached_tokens"),
						gjson.Get(payload, "response.usage.cached_tokens"),
					)
				}
			}
			_, _ = fmt.Fprintln(c.Writer, line)
			c.Writer.Flush()
		}
		return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Stream: true}
	}

	body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	usage := ClaudeUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = openAIUsageToClaudeUsage(parsed)
	}
	return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Stream: false}
}

func (s *CompatibleGatewayService) handleChatPassthrough(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest) *ForwardResult {
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	if prepared.ClientStream {
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		usage := ClaudeUsage{}
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				if payload == "[DONE]" {
					_, _ = fmt.Fprintln(c.Writer, line)
					c.Writer.Flush()
					continue
				}
				if gjson.Get(payload, "usage").Exists() {
					if parsed, ok := extractOpenAIUsageFromJSONBytes([]byte(payload)); ok {
						usage = openAIUsageToClaudeUsage(parsed)
					}
				}
			}
			_, _ = fmt.Fprintln(c.Writer, line)
			c.Writer.Flush()
		}
		return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Stream: true}
	}

	body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	usage := ClaudeUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = openAIUsageToClaudeUsage(parsed)
	}
	return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Stream: false}
}

func (s *CompatibleGatewayService) handleChatAsResponses(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest) *ForwardResult {
	c.Header("Content-Type", "text/event-stream")
	if !prepared.ClientStream {
		body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
		var chatResp apicompat.ChatCompletionsResponse
		if err := json.Unmarshal(body, &chatResp); err != nil {
			c.Data(http.StatusBadGateway, gin.MIMEJSON, []byte(`{"error":{"message":"invalid upstream response"}}`))
			return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel}
		}
		responsesResp := apicompat.ChatCompletionsToResponsesResponse(&chatResp)
		responseBody, _ := json.Marshal(responsesResp)
		c.Data(resp.StatusCode, gin.MIMEJSON, responseBody)
		usage := ClaudeUsage{}
		if responsesResp != nil && responsesResp.Usage != nil {
			usage = responsesUsageToClaudeUsage(responsesResp.Usage)
		}
		return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel}
	}

	c.Status(resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	state := apicompat.NewChatCompletionsToResponsesState()
	state.Model = prepared.UpstreamModel
	usage := ClaudeUsage{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		events := apicompat.ChatCompletionsChunkToResponsesEvents(&chunk, state)
		for _, event := range events {
			sse, err := apicompat.ChatResponsesEventToSSE(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprint(c.Writer, sse)
			c.Writer.Flush()
			if event.Response != nil && event.Response.Usage != nil {
				usage = responsesUsageToClaudeUsage(event.Response.Usage)
			}
		}
	}
	for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(state, "stop") {
		sse, err := apicompat.ChatResponsesEventToSSE(event)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprint(c.Writer, sse)
		c.Writer.Flush()
		if event.Response != nil && event.Response.Usage != nil {
			usage = responsesUsageToClaudeUsage(event.Response.Usage)
		}
	}
	_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
	return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Stream: true}
}

func (s *CompatibleGatewayService) handleChatAsMessages(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest) *ForwardResult {
	c.Header("Content-Type", "text/event-stream")
	if !prepared.ClientStream {
		body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
		var chatResp apicompat.ChatCompletionsResponse
		if err := json.Unmarshal(body, &chatResp); err != nil {
			c.Data(http.StatusBadGateway, gin.MIMEJSON, []byte(`{"type":"error","error":{"type":"api_error","message":"invalid upstream response"}}`))
			return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel}
		}
		responsesResp := apicompat.ChatCompletionsToResponsesResponse(&chatResp)
		anthropicResp := apicompat.ResponsesToAnthropic(responsesResp, prepared.OriginalModel)
		responseBody, _ := json.Marshal(anthropicResp)
		c.Data(resp.StatusCode, gin.MIMEJSON, responseBody)
		usage := ClaudeUsage{}
		if anthropicResp != nil {
			usage = ClaudeUsage{
				InputTokens:          anthropicResp.Usage.InputTokens,
				OutputTokens:         anthropicResp.Usage.OutputTokens,
				CacheReadInputTokens: anthropicResp.Usage.CacheReadInputTokens,
			}
		}
		return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel}
	}

	c.Status(resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	respState := apicompat.NewChatCompletionsToResponsesState()
	respState.Model = prepared.OriginalModel
	anthropicState := apicompat.NewResponsesEventToAnthropicState()
	anthropicState.Model = prepared.OriginalModel
	usage := ClaudeUsage{}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		responsesEvents := apicompat.ChatCompletionsChunkToResponsesEvents(&chunk, respState)
		for _, event := range responsesEvents {
			for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
				sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
				if err != nil {
					continue
				}
				_, _ = fmt.Fprint(c.Writer, sse)
				c.Writer.Flush()
				if anthropicEvent.Usage != nil {
					usage.InputTokens = anthropicEvent.Usage.InputTokens
					usage.OutputTokens = anthropicEvent.Usage.OutputTokens
					usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
				}
			}
		}
	}
	for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(respState, "stop") {
		for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprint(c.Writer, sse)
			c.Writer.Flush()
			if anthropicEvent.Usage != nil {
				usage.InputTokens = anthropicEvent.Usage.InputTokens
				usage.OutputTokens = anthropicEvent.Usage.OutputTokens
				usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
			}
		}
	}
	for _, anthropicEvent := range apicompat.FinalizeResponsesAnthropicStream(anthropicState) {
		sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprint(c.Writer, sse)
		c.Writer.Flush()
		if anthropicEvent.Usage != nil {
			usage.InputTokens = anthropicEvent.Usage.InputTokens
			usage.OutputTokens = anthropicEvent.Usage.OutputTokens
			usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
		}
	}
	return &ForwardResult{RequestID: resp.Header.Get("x-request-id"), Usage: usage, Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Stream: true}
}

func openAIUsageToClaudeUsage(usage OpenAIUsage) ClaudeUsage {
	return ClaudeUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
	}
}

func responsesUsageToClaudeUsage(usage *apicompat.ResponsesUsage) ClaudeUsage {
	if usage == nil {
		return ClaudeUsage{}
	}
	out := ClaudeUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
	}
	if usage.InputTokensDetails != nil {
		out.CacheReadInputTokens = usage.InputTokensDetails.CachedTokens
	}
	return out
}
