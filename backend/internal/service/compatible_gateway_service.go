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
	compatibleEndpointModeNative       compatibleEndpointMode = "native"
	compatibleEndpointModeRelay        compatibleEndpointMode = "relay"
	compatibleEndpointModeChatFallback compatibleEndpointMode = "chat_fallback"
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
	ClientRoute      CompatibleRequestRoute
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
	startTime := time.Now()
	preparedRequests, err := s.prepareRequests(account, route, body)
	if err != nil {
		return nil, "", err
	}
	upstreamEndpoint := ""
	if len(preparedRequests) > 0 {
		upstreamEndpoint = preparedRequests[0].UpstreamEndpoint
	}

	baseURL, err := s.gatewayService.validateUpstreamBaseURL(account.GetCompatibleBaseURL())
	if err != nil {
		return nil, upstreamEndpoint, err
	}
	proxyURL := resolveAccountProxyURL(ctx, account, nil)
	var lastErr error
	for _, prepared := range s.orderPreparedRequests(account, route, preparedRequests, baseURL) {
		result, endpoint, unsupported, err := s.forwardPreparedRequestAttempt(ctx, c, account, prepared, baseURL, proxyURL, startTime)
		if err == nil {
			return result, endpoint, nil
		}
		lastErr = err
		if !unsupported {
			return nil, endpoint, err
		}
		upstreamEndpoint = endpoint
	}
	if upstreamEndpoint == "" && len(preparedRequests) > 0 {
		upstreamEndpoint = preparedRequests[len(preparedRequests)-1].UpstreamEndpoint
	}
	if lastErr != nil {
		return nil, upstreamEndpoint, lastErr
	}
	return nil, upstreamEndpoint, &CompatibleUpstreamError{
		StatusCode: http.StatusBadGateway,
		Message:    "compatible upstream error",
	}
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
		ClientRoute:   route,
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
		if shouldUseCompatibleNativeMessages(account, preset, upstreamModel) {
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

func (s *CompatibleGatewayService) prepareRequests(account *Account, route CompatibleRequestRoute, body []byte) ([]*compatiblePreparedRequest, error) {
	prepared, err := s.prepareRequest(account, route, body)
	if err != nil {
		return nil, err
	}
	preparedRequests := []*compatiblePreparedRequest{prepared}
	if !shouldAddMoonshotMessagesChatFallback(account, route, prepared) {
		return preparedRequests, nil
	}
	fallbackPrepared, err := s.prepareMoonshotAnthropicMessagesChatFallbackRequest(account, body, prepared.OriginalModel, prepared.UpstreamModel, prepared.ClientStream)
	if err != nil {
		return nil, err
	}
	return append(preparedRequests, fallbackPrepared), nil
}

func shouldUseCompatibleNativeMessages(account *Account, preset CompatibleProviderPreset, upstreamModel string) bool {
	if preset.SupportsMessages == nil || !preset.SupportsMessages(upstreamModel) {
		return false
	}
	return account != nil
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
	if prepared != nil &&
		account != nil &&
		account.Platform == PlatformMoonshot &&
		compatiblePreparedClientRoute(prepared) == CompatibleRouteMessages &&
		prepared.UpstreamKind == compatibleUpstreamChat {
		return []compatibleURLCandidate{{URL: primary, Mode: compatibleEndpointModeChatFallback}}
	}
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
	return isCompatibleUnsupportedEndpointError(statusCode, respBody)
}

func shouldFallbackMoonshotMessagesToChat(account *Account, prepared *compatiblePreparedRequest, statusCode int, respBody []byte) bool {
	if account == nil || account.Platform != PlatformMoonshot || prepared == nil {
		return false
	}
	if statusCode != http.StatusBadRequest {
		return false
	}
	if compatiblePreparedClientRoute(prepared) != CompatibleRouteMessages || prepared.UpstreamKind != compatibleUpstreamMessages {
		return false
	}

	msg := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
	if msg == "" {
		msg = strings.ToLower(strings.TrimSpace(string(respBody)))
	}
	if msg == "" {
		return false
	}

	hasToolCallContext := strings.Contains(msg, "assistant tool call message") ||
		(strings.Contains(msg, "tool call") && strings.Contains(msg, "assistant"))
	if !hasToolCallContext {
		return false
	}

	return strings.Contains(msg, "reasoning_content") ||
		(strings.Contains(msg, "thinking is enabled") && strings.Contains(msg, "missing"))
}

func isCompatibleUnsupportedEndpointError(statusCode int, respBody []byte) bool {
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

func shouldRetryCompatibleTransientStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		520, 521, 522, 523, 524, 525:
		return true
	default:
		return false
	}
}

func (s *CompatibleGatewayService) endpointModeCacheKey(account *Account, prepared *compatiblePreparedRequest, baseURL string) string {
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	return fmt.Sprintf("%d|%s|%s", accountID, strings.TrimSpace(baseURL), compatiblePreparedClientRoute(prepared))
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
	switch entry.Mode {
	case compatibleEndpointModeRelay:
		return compatibleEndpointModeRelay
	case compatibleEndpointModeChatFallback:
		return compatibleEndpointModeChatFallback
	}
	return compatibleEndpointModeNative
}

func compatiblePreparedClientRoute(prepared *compatiblePreparedRequest) CompatibleRequestRoute {
	if prepared != nil && prepared.ClientRoute != "" {
		return prepared.ClientRoute
	}
	if prepared == nil {
		return CompatibleRouteChatCompletions
	}
	switch prepared.UpstreamKind {
	case compatibleUpstreamMessages:
		return CompatibleRouteMessages
	case compatibleUpstreamResponses:
		return CompatibleRouteResponses
	default:
		return CompatibleRouteChatCompletions
	}
}

func compatiblePreparedUpstreamTransport(prepared *compatiblePreparedRequest) UpstreamTransport {
	if prepared == nil {
		return UpstreamTransportUnknown
	}
	if prepared.ClientStream {
		return UpstreamTransportSSE
	}
	return UpstreamTransportHTTPJSON
}

func compatiblePreparedCompatibilityRoute(prepared *compatiblePreparedRequest, mode compatibleEndpointMode) CompatibilityRoute {
	switch mode {
	case compatibleEndpointModeRelay:
		return CompatibilityRouteCompatibleEndpointRelay
	case compatibleEndpointModeChatFallback:
		return CompatibilityRouteCompatibleChatFallback
	}
	if prepared == nil {
		return CompatibilityRouteUnknown
	}
	switch prepared.UpstreamKind {
	case compatibleUpstreamMessages:
		return CompatibilityRouteCompatibleMessagesNative
	case compatibleUpstreamResponses:
		return CompatibilityRouteCompatibleResponsesNative
	case compatibleUpstreamChat:
		return CompatibilityRouteCompatibleChatNative
	default:
		return CompatibilityRouteUnknown
	}
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

func shouldAddMoonshotMessagesChatFallback(account *Account, route CompatibleRequestRoute, prepared *compatiblePreparedRequest) bool {
	return account != nil &&
		account.Platform == PlatformMoonshot &&
		route == CompatibleRouteMessages &&
		prepared != nil &&
		prepared.UpstreamKind == compatibleUpstreamMessages
}

func (s *CompatibleGatewayService) prepareMoonshotAnthropicMessagesChatFallbackRequest(
	account *Account,
	body []byte,
	originalModel string,
	upstreamModel string,
	clientStream bool,
) (*compatiblePreparedRequest, error) {
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
	chatBody, err = patchMoonshotCompatibleChatBodyForAnthropicFallback(chatBody, account, upstreamModel)
	if err != nil {
		return nil, err
	}
	return &compatiblePreparedRequest{
		OriginalModel:    originalModel,
		UpstreamModel:    upstreamModel,
		ClientStream:     clientStream,
		ClientRoute:      CompatibleRouteMessages,
		UpstreamKind:     compatibleUpstreamChat,
		UpstreamEndpoint: "/v1/chat/completions",
		RequestBody:      chatBody,
	}, nil
}

func (s *CompatibleGatewayService) orderPreparedRequests(
	account *Account,
	route CompatibleRequestRoute,
	preparedRequests []*compatiblePreparedRequest,
	baseURL string,
) []*compatiblePreparedRequest {
	if len(preparedRequests) < 2 || account == nil || account.Platform != PlatformMoonshot || route != CompatibleRouteMessages {
		return preparedRequests
	}
	if s.preferredEndpointMode(account, preparedRequests[0], baseURL) != compatibleEndpointModeChatFallback {
		return preparedRequests
	}
	ordered := make([]*compatiblePreparedRequest, 0, len(preparedRequests))
	for _, prepared := range preparedRequests {
		if prepared != nil && prepared.UpstreamKind == compatibleUpstreamChat {
			ordered = append(ordered, prepared)
		}
	}
	for _, prepared := range preparedRequests {
		if prepared == nil || prepared.UpstreamKind == compatibleUpstreamChat {
			continue
		}
		ordered = append(ordered, prepared)
	}
	if len(ordered) == len(preparedRequests) {
		return ordered
	}
	return preparedRequests
}

func (s *CompatibleGatewayService) executePreparedRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	prepared *compatiblePreparedRequest,
	baseURL string,
	proxyURL string,
) (*http.Response, bool, error) {
	urlCandidates := s.buildURLCandidatesForPreparedRequest(account, prepared, baseURL)
	if len(urlCandidates) == 0 {
		return nil, false, &CompatibleUpstreamError{
			StatusCode: http.StatusBadGateway,
			Message:    "compatible upstream error",
		}
	}
	allUnsupported := true
	var lastErr error

	for idx, candidate := range urlCandidates {
		SetCompatibilityRoute(c, compatiblePreparedCompatibilityRoute(prepared, candidate.Mode))
		SetCompatibilityUpstreamTransport(c, compatiblePreparedUpstreamTransport(prepared))
		AppendCompatibilityFallbackStage(c, string(candidate.Mode))
		for attempt := 0; ; attempt++ {
			prepared.URL = candidate.URL

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, prepared.URL, bytes.NewReader(prepared.RequestBody))
			if err != nil {
				return nil, false, err
			}
			req.Header.Set("Content-Type", "application/json")
			if prepared.ClientStream {
				req.Header.Set("Accept", "text/event-stream")
			}
			if err := s.applyAuth(req, account); err != nil {
				return nil, false, err
			}
			s.applyHeaderPatches(req, account, prepared)

			resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
			if err != nil {
				return nil, false, &CompatibleUpstreamError{
					StatusCode: http.StatusBadGateway,
					Message:    sanitizeUpstreamErrorMessage(err.Error()),
				}
			}

			if resp.StatusCode < 400 {
				unsupported, validationErr := validateCompatibleSuccessResponse(resp)
				if validationErr != nil {
					_ = resp.Body.Close()
					lastErr = validationErr
					if !unsupported {
						return nil, false, lastErr
					}
					break
				}
				s.recordEndpointMode(account, prepared, baseURL, candidate.Mode)
				return resp, false, nil
			}

			statusCode := resp.StatusCode
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			_ = resp.Body.Close()
			moonshotChatFallback := shouldFallbackMoonshotMessagesToChat(account, prepared, statusCode, respBody)
			unsupported := isCompatibleUnsupportedEndpointError(statusCode, respBody) || moonshotChatFallback
			if !unsupported {
				allUnsupported = false
			}
			if attempt == 0 && shouldRetryCompatibleTransientStatus(statusCode) {
				continue
			}
			if idx == 0 && idx < len(urlCandidates)-1 && (shouldRetryViaRelayCompatibleEndpoint(prepared, statusCode, respBody) || moonshotChatFallback) {
				lastErr = &CompatibleUpstreamError{
					StatusCode:   mapUpstreamStatusCode(statusCode),
					Message:      sanitizeCompatibleUpstreamMessage(statusCode, respBody),
					ResponseBody: respBody,
				}
				break
			}
			if s.gatewayService.shouldFailoverUpstreamError(statusCode) {
				return nil, false, &UpstreamFailoverError{
					StatusCode:   statusCode,
					ResponseBody: respBody,
				}
			}
			lastErr = &CompatibleUpstreamError{
				StatusCode:   mapUpstreamStatusCode(statusCode),
				Message:      sanitizeCompatibleUpstreamMessage(statusCode, respBody),
				ResponseBody: respBody,
			}
			if !unsupported {
				return nil, false, lastErr
			}
			break
		}
	}

	if lastErr != nil {
		return nil, allUnsupported, lastErr
	}
	return nil, false, &CompatibleUpstreamError{
		StatusCode: http.StatusBadGateway,
		Message:    "compatible upstream error",
	}
}

func validateCompatibleSuccessResponse(resp *http.Response) (bool, error) {
	if resp == nil {
		return false, &CompatibleUpstreamError{
			StatusCode: http.StatusBadGateway,
			Message:    "empty upstream response",
		}
	}
	sample, err := peekCompatibleResponseSample(resp, 512)
	if err != nil {
		return false, &CompatibleUpstreamError{
			StatusCode: http.StatusBadGateway,
			Message:    "failed to inspect upstream response",
		}
	}
	if isLikelyCompatibleHTMLResponse(resp.Header.Get("Content-Type"), sample) {
		return true, &CompatibleUpstreamError{
			StatusCode:   http.StatusBadGateway,
			Message:      "upstream returned an HTML page instead of API response",
			ResponseBody: sample,
		}
	}
	return false, nil
}

func peekCompatibleResponseSample(resp *http.Response, maxBytes int) ([]byte, error) {
	if resp == nil || resp.Body == nil || maxBytes <= 0 {
		return nil, nil
	}
	originalBody := resp.Body
	reader := bufio.NewReader(originalBody)
	sample, err := reader.Peek(maxBytes)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, err
	}
	resp.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: reader,
		Closer: originalBody,
	}
	return append([]byte(nil), sample...), nil
}

func isLikelyCompatibleHTMLResponse(contentType string, sample []byte) bool {
	trimmedContentType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.Contains(trimmedContentType, "text/html") || strings.Contains(trimmedContentType, "application/xhtml+xml") {
		return true
	}
	trimmedSample := bytes.TrimSpace(sample)
	if len(trimmedSample) == 0 {
		return false
	}
	lowerSample := bytes.ToLower(trimmedSample)
	return bytes.HasPrefix(lowerSample, []byte("<!doctype html")) ||
		bytes.HasPrefix(lowerSample, []byte("<html")) ||
		bytes.HasPrefix(lowerSample, []byte("<head")) ||
		bytes.HasPrefix(lowerSample, []byte("<body"))
}

func sanitizeCompatibleUpstreamMessage(statusCode int, respBody []byte) string {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
	if upstreamMsg == "" {
		upstreamMsg = http.StatusText(statusCode)
	}
	return upstreamMsg
}

func (s *CompatibleGatewayService) forwardPreparedRequestAttempt(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	prepared *compatiblePreparedRequest,
	baseURL string,
	proxyURL string,
	startTime time.Time,
) (*ForwardResult, string, bool, error) {
	resp, unsupported, err := s.executePreparedRequest(ctx, c, account, prepared, baseURL, proxyURL)
	if err != nil {
		return nil, prepared.UpstreamEndpoint, unsupported, err
	}
	if resp == nil {
		return nil, prepared.UpstreamEndpoint, unsupported, &CompatibleUpstreamError{
			StatusCode: http.StatusBadGateway,
			Message:    "compatible upstream error",
		}
	}
	defer func() { _ = resp.Body.Close() }()

	switch prepared.UpstreamKind {
	case compatibleUpstreamMessages:
		return s.handleMessagesResponse(resp, c, prepared, startTime), prepared.UpstreamEndpoint, false, nil
	case compatibleUpstreamResponses:
		return s.handleResponsesResponse(resp, c, prepared, startTime), prepared.UpstreamEndpoint, false, nil
	case compatibleUpstreamChat:
		switch compatiblePreparedClientRoute(prepared) {
		case CompatibleRouteChatCompletions:
			return s.handleChatPassthrough(resp, c, prepared, startTime), prepared.UpstreamEndpoint, false, nil
		case CompatibleRouteResponses:
			return s.handleChatAsResponses(resp, c, prepared, startTime), prepared.UpstreamEndpoint, false, nil
		case CompatibleRouteMessages:
			return s.handleChatAsMessages(resp, c, prepared, startTime), prepared.UpstreamEndpoint, false, nil
		}
	}
	return nil, prepared.UpstreamEndpoint, false, fmt.Errorf("unsupported compatible route")
}

func (s *CompatibleGatewayService) handleMessagesResponse(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	if handled, repaired := s.maybeRepairClaudeKimiMessagesResponse(resp, c, prepared, startTime); handled {
		return repaired
	}
	usage := ClaudeUsage{}
	if prepared.ClientStream {
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		var firstTokenMs *int
		var eventBuf bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				markCompatibleFirstToken(startTime, &firstTokenMs, payload)
				s.gatewayService.parseSSEUsage(payload, &usage)
			}
			appendCompatibleSSELine(&eventBuf, line)
			if line == "" {
				flushCompatibleSSEBuffer(c, &eventBuf)
			}
		}
		flushCompatibleSSEBuffer(c, &eventBuf)
		return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
	}

	body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	if parsed := parseClaudeUsageFromResponseBody(body); parsed != nil {
		usage = *parsed
	}
	return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil)
}

func (s *CompatibleGatewayService) handleResponsesResponse(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	if prepared.ClientStream {
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		usage := ClaudeUsage{}
		var firstTokenMs *int
		var eventBuf bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				markCompatibleFirstToken(startTime, &firstTokenMs, payload)
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
			appendCompatibleSSELine(&eventBuf, line)
			if line == "" {
				flushCompatibleSSEBuffer(c, &eventBuf)
			}
		}
		flushCompatibleSSEBuffer(c, &eventBuf)
		return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
	}

	body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	usage := ClaudeUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = openAIUsageToClaudeUsage(parsed)
	}
	return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil)
}

func (s *CompatibleGatewayService) handleChatPassthrough(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	if prepared.ClientStream {
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		usage := ClaudeUsage{}
		var firstTokenMs *int
		var eventBuf bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				if payload != "[DONE]" {
					markCompatibleFirstToken(startTime, &firstTokenMs, payload)
				}
				if payload != "[DONE]" && gjson.Get(payload, "usage").Exists() {
					if parsed, ok := extractOpenAIUsageFromJSONBytes([]byte(payload)); ok {
						usage = openAIUsageToClaudeUsage(parsed)
					}
				}
			}
			appendCompatibleSSELine(&eventBuf, line)
			if line == "" {
				flushCompatibleSSEBuffer(c, &eventBuf)
			}
		}
		flushCompatibleSSEBuffer(c, &eventBuf)
		return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
	}

	body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	usage := ClaudeUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = openAIUsageToClaudeUsage(parsed)
	}
	return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil)
}

func (s *CompatibleGatewayService) handleChatAsResponses(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	if !prepared.ClientStream {
		body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
		var chatResp apicompat.ChatCompletionsResponse
		if err := json.Unmarshal(body, &chatResp); err != nil {
			c.Data(http.StatusBadGateway, gin.MIMEJSON, []byte(`{"error":{"message":"invalid upstream response"}}`))
			return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Duration: time.Since(startTime)}
		}
		responsesResp := apicompat.ChatCompletionsToResponsesResponse(&chatResp)
		responseBody, _ := json.Marshal(responsesResp)
		c.Data(resp.StatusCode, gin.MIMEJSON, responseBody)
		usage := ClaudeUsage{}
		if responsesResp != nil && responsesResp.Usage != nil {
			usage = responsesUsageToClaudeUsage(responsesResp.Usage)
		}
		return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Status(resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	state := apicompat.NewChatCompletionsToResponsesState()
	state.Model = prepared.UpstreamModel
	usage := ClaudeUsage{}
	var firstTokenMs *int
	finalFinishReason := "stop"
	seenFinishReason := false
	pendingFinishReason := ""
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			if pendingFinishReason != "" {
				var finalBatch bytes.Buffer
				for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(state, pendingFinishReason) {
					sse, err := apicompat.ChatResponsesEventToSSE(event)
					if err != nil {
						continue
					}
					finalBatch.WriteString(sse)
					if event.Response != nil && event.Response.Usage != nil {
						usage = responsesUsageToClaudeUsage(event.Response.Usage)
					}
				}
				finalBatch.WriteString("data: [DONE]\n\n")
				flushCompatibleSSEBuffer(c, &finalBatch)
				_ = resp.Body.Close()
				return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
			}
			break
		}
		markCompatibleFirstToken(startTime, &firstTokenMs, payload)
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chatUsageToClaudeUsage(chunk.Usage)
		}
		finishReasonReady := false
		if len(chunk.Choices) > 0 {
			choice := &chunk.Choices[0]
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finalFinishReason = *choice.FinishReason
				seenFinishReason = true
				if chunk.Usage == nil {
					pendingFinishReason = finalFinishReason
					choice.FinishReason = nil
				} else {
					pendingFinishReason = ""
					finishReasonReady = true
				}
			}
		}
		events := apicompat.ChatCompletionsChunkToResponsesEvents(&chunk, state)
		var sseBatch bytes.Buffer
		for _, event := range events {
			sse, err := apicompat.ChatResponsesEventToSSE(event)
			if err != nil {
				continue
			}
			sseBatch.WriteString(sse)
			if event.Response != nil && event.Response.Usage != nil {
				usage = responsesUsageToClaudeUsage(event.Response.Usage)
			}
		}
		flushCompatibleSSEBuffer(c, &sseBatch)
		if pendingFinishReason != "" && chunk.Usage != nil && len(chunk.Choices) == 0 {
			var finalBatch bytes.Buffer
			for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(state, pendingFinishReason) {
				sse, err := apicompat.ChatResponsesEventToSSE(event)
				if err != nil {
					continue
				}
				finalBatch.WriteString(sse)
				if event.Response != nil && event.Response.Usage != nil {
					usage = responsesUsageToClaudeUsage(event.Response.Usage)
				}
			}
			finalBatch.WriteString("data: [DONE]\n\n")
			flushCompatibleSSEBuffer(c, &finalBatch)
			_ = resp.Body.Close()
			return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
		}
		if finishReasonReady {
			var finalBatch bytes.Buffer
			finalBatch.WriteString("data: [DONE]\n\n")
			flushCompatibleSSEBuffer(c, &finalBatch)
			_ = resp.Body.Close()
			return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
		}
	}
	if !seenFinishReason {
		finalFinishReason = "stop"
	}
	var finalBatch bytes.Buffer
	for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(state, finalFinishReason) {
		sse, err := apicompat.ChatResponsesEventToSSE(event)
		if err != nil {
			continue
		}
		finalBatch.WriteString(sse)
		if event.Response != nil && event.Response.Usage != nil {
			usage = responsesUsageToClaudeUsage(event.Response.Usage)
		}
	}
	finalBatch.WriteString("data: [DONE]\n\n")
	flushCompatibleSSEBuffer(c, &finalBatch)
	return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
}

func (s *CompatibleGatewayService) handleChatAsMessages(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	if !prepared.ClientStream {
		body, _ := readUpstreamResponseBodyLimited(resp.Body, resolveUpstreamResponseReadLimit(s.cfg))
		var chatResp apicompat.ChatCompletionsResponse
		if err := json.Unmarshal(body, &chatResp); err != nil {
			c.Data(http.StatusBadGateway, gin.MIMEJSON, []byte(`{"type":"error","error":{"type":"api_error","message":"invalid upstream response"}}`))
			return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Duration: time.Since(startTime)}
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
		return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Status(resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	respState := apicompat.NewChatCompletionsToResponsesState()
	respState.Model = prepared.OriginalModel
	anthropicState := apicompat.NewResponsesEventToAnthropicState()
	anthropicState.Model = prepared.OriginalModel
	usage := ClaudeUsage{}
	var firstTokenMs *int
	finalFinishReason := "stop"
	seenFinishReason := false
	pendingFinishReason := ""

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			if pendingFinishReason != "" {
				var finalBatch bytes.Buffer
				for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(respState, pendingFinishReason) {
					for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
						sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
						if err != nil {
							continue
						}
						finalBatch.WriteString(sse)
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
					finalBatch.WriteString(sse)
					if anthropicEvent.Usage != nil {
						usage.InputTokens = anthropicEvent.Usage.InputTokens
						usage.OutputTokens = anthropicEvent.Usage.OutputTokens
						usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
					}
				}
				flushCompatibleSSEBuffer(c, &finalBatch)
				_ = resp.Body.Close()
				return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
			}
			break
		}
		markCompatibleFirstToken(startTime, &firstTokenMs, payload)
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chatUsageToClaudeUsage(chunk.Usage)
		}
		finishReasonReady := false
		if len(chunk.Choices) > 0 {
			choice := &chunk.Choices[0]
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finalFinishReason = *choice.FinishReason
				seenFinishReason = true
				if chunk.Usage == nil {
					pendingFinishReason = finalFinishReason
					choice.FinishReason = nil
				} else {
					pendingFinishReason = ""
					finishReasonReady = true
				}
			}
		}
		responsesEvents := apicompat.ChatCompletionsChunkToResponsesEvents(&chunk, respState)
		var sseBatch bytes.Buffer
		for _, event := range responsesEvents {
			for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
				sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
				if err != nil {
					continue
				}
				sseBatch.WriteString(sse)
				if anthropicEvent.Usage != nil {
					usage.InputTokens = anthropicEvent.Usage.InputTokens
					usage.OutputTokens = anthropicEvent.Usage.OutputTokens
					usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
				}
			}
		}
		flushCompatibleSSEBuffer(c, &sseBatch)
		if pendingFinishReason != "" && chunk.Usage != nil && len(chunk.Choices) == 0 {
			var finalBatch bytes.Buffer
			for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(respState, pendingFinishReason) {
				for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
					sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
					if err != nil {
						continue
					}
					finalBatch.WriteString(sse)
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
				finalBatch.WriteString(sse)
				if anthropicEvent.Usage != nil {
					usage.InputTokens = anthropicEvent.Usage.InputTokens
					usage.OutputTokens = anthropicEvent.Usage.OutputTokens
					usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
				}
			}
			flushCompatibleSSEBuffer(c, &finalBatch)
			_ = resp.Body.Close()
			return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
		}
		if finishReasonReady {
			_ = resp.Body.Close()
			return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
		}
	}
	if !seenFinishReason {
		finalFinishReason = "stop"
	}
	var finalBatch bytes.Buffer
	for _, event := range apicompat.FinalizeChatCompletionsResponsesStream(respState, finalFinishReason) {
		for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
			if err != nil {
				continue
			}
			finalBatch.WriteString(sse)
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
		finalBatch.WriteString(sse)
		if anthropicEvent.Usage != nil {
			usage.InputTokens = anthropicEvent.Usage.InputTokens
			usage.OutputTokens = anthropicEvent.Usage.OutputTokens
			usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
		}
	}
	flushCompatibleSSEBuffer(c, &finalBatch)
	return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
}

func buildCompatibleForwardResult(
	resp *http.Response,
	prepared *compatiblePreparedRequest,
	usage ClaudeUsage,
	stream bool,
	startTime time.Time,
	firstTokenMs *int,
) *ForwardResult {
	requestID := ""
	if resp != nil {
		requestID = resp.Header.Get("x-request-id")
	}
	return &ForwardResult{
		RequestID:     requestID,
		Usage:         usage,
		Model:         prepared.OriginalModel,
		UpstreamModel: prepared.UpstreamModel,
		Stream:        stream,
		Duration:      time.Since(startTime),
		FirstTokenMs:  firstTokenMs,
	}
}

func markCompatibleFirstToken(startTime time.Time, firstTokenMs **int, payload string) {
	if firstTokenMs == nil || *firstTokenMs != nil {
		return
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" || trimmed == "[DONE]" {
		return
	}
	ms := int(time.Since(startTime).Milliseconds())
	*firstTokenMs = &ms
}

func appendCompatibleSSELine(buf *bytes.Buffer, line string) {
	if buf == nil {
		return
	}
	buf.WriteString(line)
	buf.WriteByte('\n')
}

func flushCompatibleSSEBuffer(c *gin.Context, buf *bytes.Buffer) {
	if c == nil || buf == nil || buf.Len() == 0 {
		return
	}
	_, _ = c.Writer.Write(buf.Bytes())
	c.Writer.Flush()
	buf.Reset()
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

func chatUsageToClaudeUsage(usage *apicompat.ChatUsage) ClaudeUsage {
	if usage == nil {
		return ClaudeUsage{}
	}
	out := ClaudeUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CacheReadInputTokens = usage.PromptTokensDetails.CachedTokens
	}
	return out
}
