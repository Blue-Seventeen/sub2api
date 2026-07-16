package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
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

type CompatibleClientError struct {
	StatusCode int
	ErrorType  string
	Message    string
}

func (e *CompatibleClientError) Error() string {
	if e == nil {
		return "compatible client error"
	}
	return e.Message
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

type KimiFeatureClass string
type KimiRelayDecision string
type KimiOfficialCapability string

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
	KimiFeatureClass KimiFeatureClass
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
			if account.Platform == PlatformMoonshot {
				if !moonshotAccountFeatureEnabled(account, "kimi_responses_bridge_enabled", true) {
					return nil, moonshotResponsesBridgeError("Moonshot Kimi Responses bridge is disabled for this account")
				}
				if err := validateMoonshotResponsesBridgeRequest(body); err != nil {
					return nil, err
				}
			}
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
			if account.Platform == PlatformMoonshot {
				chatBody = normalizeMoonshotResponsesChatTextContent(chatBody)
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

	if account.Platform == PlatformMoonshot {
		prepared.KimiFeatureClass = kimiFeatureClassForBody(route, upstreamModel, prepared.RequestBody)
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
	if account != nil && account.Platform == PlatformMoonshot {
		if !moonshotAccountFeatureEnabled(account, "kimi_official_fast_path_enabled", true) ||
			!moonshotAccountFeatureEnabled(account, "kimi_native_messages_enabled", true) {
			return false
		}
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

func validateMoonshotResponsesBridgeRequest(body []byte) error {
	if prev := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()); prev != "" {
		return moonshotResponsesBridgeError("previous_response_id is not supported by the Moonshot Kimi Responses bridge")
	}
	if isMoonshotRequiredToolChoice(body) {
		return moonshotResponsesBridgeError("tool_choice=required is not supported by the Moonshot Kimi Responses bridge; use tool_choice=auto")
	}
	for _, rawTool := range gjson.GetBytes(body, "tools").Array() {
		toolType := strings.TrimSpace(rawTool.Get("type").String())
		if toolType == "" || toolType == "function" {
			continue
		}
		return moonshotResponsesBridgeError("non-function Responses tools are not supported by the Moonshot Kimi bridge")
	}
	input := gjson.GetBytes(body, "input")
	if input.IsArray() {
		for _, item := range input.Array() {
			itemType := strings.TrimSpace(item.Get("type").String())
			switch itemType {
			case "", "message", "function_call", "function_call_output":
				continue
			default:
				return moonshotResponsesBridgeError("unsupported Responses input item type for the Moonshot Kimi bridge: " + itemType)
			}
		}
	}
	return nil
}

func moonshotResponsesBridgeError(message string) error {
	return &CompatibleClientError{
		StatusCode: http.StatusBadRequest,
		ErrorType:  "invalid_request_error",
		Message:    message,
	}
}

func moonshotAccountFeatureEnabled(account *Account, key string, defaultValue bool) bool {
	if account == nil || account.Extra == nil {
		return defaultValue
	}
	if _, ok := account.Extra[key]; !ok {
		return defaultValue
	}
	return account.GetExtraBool(key)
}

func kimiFeatureClassForBody(route CompatibleRequestRoute, upstreamModel string, body []byte) KimiFeatureClass {
	toolChoiceClass := "none"
	toolChoice := gjson.GetBytes(body, "tool_choice")
	if toolChoice.Exists() {
		switch {
		case isMoonshotRequiredToolChoice(body):
			toolChoiceClass = "required"
		case toolChoice.Type == gjson.String:
			toolChoiceClass = strings.ToLower(strings.TrimSpace(toolChoice.String()))
		default:
			if typed := strings.TrimSpace(gjson.GetBytes(body, "tool_choice.type").String()); typed != "" {
				toolChoiceClass = strings.ToLower(typed)
			} else {
				toolChoiceClass = "set"
			}
		}
	}
	return KimiFeatureClass(strings.Join([]string{
		"route=" + strings.ToLower(strings.TrimSpace(string(route))),
		"model=" + strings.ToLower(strings.TrimSpace(upstreamModel)),
		"tools=" + boolFeature(len(gjson.GetBytes(body, "tools").Array()) > 0),
		"thinking=" + boolFeature(moonshotBodyHasThinking(body)),
		"tool_history=" + boolFeature(moonshotBodyHasToolHistory(body)),
		"tool_choice=" + toolChoiceClass,
	}, ";"))
}

func boolFeature(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func moonshotBodyHasThinking(body []byte) bool {
	return gjson.GetBytes(body, "thinking").Exists() ||
		gjson.GetBytes(body, "output_config.effort").Exists() ||
		gjson.GetBytes(body, "reasoning").Exists() ||
		gjson.GetBytes(body, "reasoning_effort").Exists() ||
		bytes.Contains(body, []byte(`"reasoning_content"`)) ||
		bytes.Contains(body, []byte(`"type":"thinking"`)) ||
		bytes.Contains(body, []byte(`"type": "thinking"`))
}

func moonshotBodyHasToolHistory(body []byte) bool {
	return bytes.Contains(body, []byte(`"tool_calls"`)) ||
		bytes.Contains(body, []byte(`"role":"tool"`)) ||
		bytes.Contains(body, []byte(`"role": "tool"`)) ||
		bytes.Contains(body, []byte(`"type":"tool_use"`)) ||
		bytes.Contains(body, []byte(`"type": "tool_use"`)) ||
		bytes.Contains(body, []byte(`"type":"tool_result"`)) ||
		bytes.Contains(body, []byte(`"type": "tool_result"`))
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

	if strings.Contains(msg, "failed to parse request body") ||
		strings.Contains(msg, "invalid request body") ||
		strings.Contains(msg, "parse request body") {
		return true
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
	if account != nil && account.Platform == PlatformMoonshot {
		upstreamModel := ""
		featureClass := KimiFeatureClass("")
		if prepared != nil {
			upstreamModel = prepared.UpstreamModel
			featureClass = prepared.KimiFeatureClass
			if featureClass == "" {
				featureClass = kimiFeatureClassForBody(compatiblePreparedClientRoute(prepared), prepared.UpstreamModel, prepared.RequestBody)
			}
		}
		return fmt.Sprintf("%d|%s|%s|%s|%s",
			accountID,
			strings.TrimSpace(baseURL),
			compatiblePreparedClientRoute(prepared),
			strings.ToLower(strings.TrimSpace(upstreamModel)),
			featureClass,
		)
	}
	return fmt.Sprintf("%d|%s|%s", accountID, strings.TrimSpace(baseURL), compatiblePreparedClientRoute(prepared))
}

func (s *CompatibleGatewayService) preferredEndpointMode(account *Account, prepared *compatiblePreparedRequest, baseURL string) compatibleEndpointMode {
	if s == nil {
		return compatibleEndpointModeNative
	}
	if account != nil && account.Platform == PlatformMoonshot &&
		!moonshotAccountFeatureEnabled(account, "kimi_endpoint_mode_cache_enabled", true) {
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

func shouldUseOpenAIProfileForCompatibleRelay(prepared *compatiblePreparedRequest, targetURL string) bool {
	if prepared == nil {
		return false
	}
	switch prepared.UpstreamKind {
	case compatibleUpstreamChat, compatibleUpstreamResponses:
	default:
		return false
	}

	trimmed := strings.ToLower(strings.TrimRight(strings.TrimSpace(targetURL), "/"))
	return strings.HasSuffix(trimmed, "/v1/chat/completions") ||
		strings.HasSuffix(trimmed, "/v1/responses")
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
	if account != nil && account.Platform == PlatformMoonshot &&
		!moonshotAccountFeatureEnabled(account, "kimi_endpoint_mode_cache_enabled", true) {
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
	preset, err := getCompatiblePreset(account)
	if err != nil {
		return err
	}
	apiKey := getCompatibleAuthToken(account, preset.AuthMode)
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
		moonshotAccountFeatureEnabled(account, "kimi_chat_fallback_enabled", true) &&
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
	chatBody = prefixMoonshotAnthropicChatFallbackToolIDs(chatBody)
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
		KimiFeatureClass: kimiFeatureClassForBody(CompatibleRouteMessages, upstreamModel, body),
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

			reqCtx := ctx
			if shouldUseOpenAIProfileForCompatibleRelay(prepared, candidate.URL) {
				reqCtx = WithHTTPUpstreamProfile(reqCtx, HTTPUpstreamProfileOpenAI)
			}
			req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, prepared.URL, bytes.NewReader(prepared.RequestBody))
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
				ClearAutoSelectedProxyStickyOnTransportError(ctx, account, err)
				if errors.Is(err, context.Canceled) {
					return nil, false, err
				}
				return nil, false, &UpstreamFailoverError{
					StatusCode:   http.StatusBadGateway,
					ResponseBody: []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`),
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

func compatibleInvalidUpstreamResponse(c *gin.Context, body []byte) {
	if c == nil || c.Writer.Written() {
		return
	}
	c.Data(http.StatusBadGateway, gin.MIMEJSON, body)
}

func compatibleReadUpstreamResponseBody(
	resp *http.Response,
	cfg *config.Config,
	c *gin.Context,
	onTooLarge TooLargeWriter,
	invalidBody []byte,
) ([]byte, bool) {
	if resp == nil {
		compatibleInvalidUpstreamResponse(c, invalidBody)
		return nil, false
	}
	body, err := ReadUpstreamResponseBody(resp.Body, cfg, c, onTooLarge)
	if err == nil {
		return body, true
	}
	if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
		setOpsUpstreamError(c, http.StatusBadGateway, "failed to read upstream response", "")
		compatibleInvalidUpstreamResponse(c, invalidBody)
	}
	return nil, false
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
	if handled, repaired := s.maybeRepairClaudeKimiMessagesResponse(resp, c, prepared, startTime); handled {
		return repaired
	}
	usage := ClaudeUsage{}
	if prepared.ClientStream {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		terminal := newCompatibleStreamTerminalTracker(CompatibleRouteMessages)
		var firstTokenMs *int
		var eventBuf bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			terminal.ObserveLine(line)
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
		scanErr := scanner.Err()
		if scanErr == nil && !terminal.Seen() {
			scanErr = errCompatibleStreamMissingTerminal
		}
		logCompatibleStreamScannerError(prepared, scanErr)
		flushCompatibleSSEBuffer(c, &eventBuf)
		return buildCompatibleStreamForwardResult(resp, prepared, usage, startTime, firstTokenMs, scanErr)
	}

	body, ok := compatibleReadUpstreamResponseBody(resp, s.cfg, c, anthropicTooLargeError, []byte(`{"type":"error","error":{"type":"api_error","message":"invalid upstream response"}}`))
	if !ok {
		return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Duration: time.Since(startTime)}
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	if parsed := parseClaudeUsageFromResponseBody(body); parsed != nil {
		usage = *parsed
	}
	return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil, body)
}

func (s *CompatibleGatewayService) handleResponsesResponse(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	if prepared.ClientStream {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		usage := ClaudeUsage{}
		terminal := newCompatibleStreamTerminalTracker(CompatibleRouteResponses)
		var firstTokenMs *int
		var eventBuf bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			terminal.ObserveLine(line)
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				markCompatibleFirstToken(startTime, &firstTokenMs, payload)
				if gjson.Get(payload, "response.usage").Exists() {
					usage = parseNewAPIStyleUsageForRoute([]byte(payload), NewAPIStyleRouteResponses)
				}
			}
			appendCompatibleSSELine(&eventBuf, line)
			if line == "" {
				flushCompatibleSSEBuffer(c, &eventBuf)
			}
		}
		scanErr := scanner.Err()
		if scanErr == nil && !terminal.Seen() {
			scanErr = errCompatibleStreamMissingTerminal
		}
		logCompatibleStreamScannerError(prepared, scanErr)
		flushCompatibleSSEBuffer(c, &eventBuf)
		return buildCompatibleStreamForwardResult(resp, prepared, usage, startTime, firstTokenMs, scanErr)
	}

	body, ok := compatibleReadUpstreamResponseBody(resp, s.cfg, c, openAITooLargeError, []byte(`{"error":{"message":"invalid upstream response"}}`))
	if !ok {
		return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Duration: time.Since(startTime)}
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	usage := ClaudeUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = openAIUsageToClaudeUsage(parsed)
	}
	return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil, body)
}

func (s *CompatibleGatewayService) handleChatPassthrough(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	if prepared.ClientStream {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
		c.Status(resp.StatusCode)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
		usage := ClaudeUsage{}
		terminal := newCompatibleStreamTerminalTracker(CompatibleRouteChatCompletions)
		var firstTokenMs *int
		var eventBuf bytes.Buffer
		for scanner.Scan() {
			line := scanner.Text()
			terminal.ObserveLine(line)
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
		scanErr := scanner.Err()
		if scanErr == nil && !terminal.Seen() {
			scanErr = errCompatibleStreamMissingTerminal
		}
		logCompatibleStreamScannerError(prepared, scanErr)
		flushCompatibleSSEBuffer(c, &eventBuf)
		return buildCompatibleStreamForwardResult(resp, prepared, usage, startTime, firstTokenMs, scanErr)
	}

	body, ok := compatibleReadUpstreamResponseBody(resp, s.cfg, c, openAITooLargeError, []byte(`{"error":{"message":"invalid upstream response"}}`))
	if !ok {
		return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Duration: time.Since(startTime)}
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, nil)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	usage := ClaudeUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(body); ok {
		usage = openAIUsageToClaudeUsage(parsed)
	}
	return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil, body)
}

func (s *CompatibleGatewayService) handleChatAsResponses(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	if !prepared.ClientStream {
		body, ok := compatibleReadUpstreamResponseBody(resp, s.cfg, c, openAITooLargeError, []byte(`{"error":{"message":"invalid upstream response"}}`))
		if !ok {
			return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Duration: time.Since(startTime)}
		}
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
		return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil, body)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Status(resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	state := apicompat.NewChatCompletionsToResponsesStreamState(prepared.UpstreamModel)
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
				for _, event := range finalizeCompatibleChatResponsesStream(state, pendingFinishReason) {
					sse, err := apicompat.ChatResponsesEventToSSE(event)
					if err != nil {
						continue
					}
					_, _ = finalBatch.WriteString(sse)
					if event.Response != nil && event.Response.Usage != nil {
						usage = responsesUsageToClaudeUsage(event.Response.Usage)
					}
				}
				_, _ = finalBatch.WriteString("data: [DONE]\n\n")
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
			_, _ = sseBatch.WriteString(sse)
			if event.Response != nil && event.Response.Usage != nil {
				usage = responsesUsageToClaudeUsage(event.Response.Usage)
			}
		}
		flushCompatibleSSEBuffer(c, &sseBatch)
		if pendingFinishReason != "" && chunk.Usage != nil && len(chunk.Choices) == 0 {
			var finalBatch bytes.Buffer
			for _, event := range finalizeCompatibleChatResponsesStream(state, pendingFinishReason) {
				sse, err := apicompat.ChatResponsesEventToSSE(event)
				if err != nil {
					continue
				}
				_, _ = finalBatch.WriteString(sse)
				if event.Response != nil && event.Response.Usage != nil {
					usage = responsesUsageToClaudeUsage(event.Response.Usage)
				}
			}
			_, _ = finalBatch.WriteString("data: [DONE]\n\n")
			flushCompatibleSSEBuffer(c, &finalBatch)
			_ = resp.Body.Close()
			return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
		}
		if finishReasonReady {
			var finalBatch bytes.Buffer
			for _, event := range finalizeCompatibleChatResponsesStream(state, finalFinishReason) {
				sse, err := apicompat.ChatResponsesEventToSSE(event)
				if err != nil {
					continue
				}
				_, _ = finalBatch.WriteString(sse)
				if event.Response != nil && event.Response.Usage != nil {
					usage = responsesUsageToClaudeUsage(event.Response.Usage)
				}
			}
			_, _ = finalBatch.WriteString("data: [DONE]\n\n")
			flushCompatibleSSEBuffer(c, &finalBatch)
			_ = resp.Body.Close()
			return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
		}
	}
	if err := scanner.Err(); err != nil {
		logCompatibleStreamScannerError(prepared, err)
		return buildCompatibleStreamForwardResult(resp, prepared, usage, startTime, firstTokenMs, err)
	}
	if !seenFinishReason {
		finalFinishReason = "stop"
	}
	var finalBatch bytes.Buffer
	for _, event := range finalizeCompatibleChatResponsesStream(state, finalFinishReason) {
		sse, err := apicompat.ChatResponsesEventToSSE(event)
		if err != nil {
			continue
		}
		_, _ = finalBatch.WriteString(sse)
		if event.Response != nil && event.Response.Usage != nil {
			usage = responsesUsageToClaudeUsage(event.Response.Usage)
		}
	}
	_, _ = finalBatch.WriteString("data: [DONE]\n\n")
	flushCompatibleSSEBuffer(c, &finalBatch)
	return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
}

func (s *CompatibleGatewayService) handleChatAsMessages(resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, startTime time.Time) *ForwardResult {
	if !prepared.ClientStream {
		body, ok := compatibleReadUpstreamResponseBody(resp, s.cfg, c, anthropicTooLargeError, []byte(`{"type":"error","error":{"type":"api_error","message":"invalid upstream response"}}`))
		if !ok {
			return &ForwardResult{Model: prepared.OriginalModel, UpstreamModel: prepared.UpstreamModel, Duration: time.Since(startTime)}
		}
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
				InputTokens:              anthropicResp.Usage.InputTokens,
				OutputTokens:             anthropicResp.Usage.OutputTokens,
				CacheCreationInputTokens: anthropicResp.Usage.CacheCreationInputTokens,
				CacheReadInputTokens:     anthropicResp.Usage.CacheReadInputTokens,
			}
		}
		return buildCompatibleForwardResult(resp, prepared, usage, false, startTime, nil, body)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Status(resp.StatusCode)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	respState := apicompat.NewChatCompletionsToResponsesStreamState(prepared.OriginalModel)
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
				for _, event := range finalizeCompatibleChatResponsesStream(respState, pendingFinishReason) {
					for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
						sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
						if err != nil {
							continue
						}
						_, _ = finalBatch.WriteString(sse)
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
					_, _ = finalBatch.WriteString(sse)
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
				_, _ = sseBatch.WriteString(sse)
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
			for _, event := range finalizeCompatibleChatResponsesStream(respState, pendingFinishReason) {
				for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
					sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
					if err != nil {
						continue
					}
					_, _ = finalBatch.WriteString(sse)
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
				_, _ = finalBatch.WriteString(sse)
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
			var finalBatch bytes.Buffer
			for _, event := range finalizeCompatibleChatResponsesStream(respState, finalFinishReason) {
				for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
					sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
					if err != nil {
						continue
					}
					_, _ = finalBatch.WriteString(sse)
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
				_, _ = finalBatch.WriteString(sse)
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
	}
	if err := scanner.Err(); err != nil {
		logCompatibleStreamScannerError(prepared, err)
		return buildCompatibleStreamForwardResult(resp, prepared, usage, startTime, firstTokenMs, err)
	}
	if !seenFinishReason {
		finalFinishReason = "stop"
	}
	var finalBatch bytes.Buffer
	for _, event := range finalizeCompatibleChatResponsesStream(respState, finalFinishReason) {
		for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&event, anthropicState) {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(anthropicEvent)
			if err != nil {
				continue
			}
			_, _ = finalBatch.WriteString(sse)
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
		_, _ = finalBatch.WriteString(sse)
		if anthropicEvent.Usage != nil {
			usage.InputTokens = anthropicEvent.Usage.InputTokens
			usage.OutputTokens = anthropicEvent.Usage.OutputTokens
			usage.CacheReadInputTokens = anthropicEvent.Usage.CacheReadInputTokens
		}
	}
	flushCompatibleSSEBuffer(c, &finalBatch)
	return buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
}

func finalizeCompatibleChatResponsesStream(state *apicompat.ChatCompletionsToResponsesStreamState, finishReason string) []apicompat.ResponsesStreamEvent {
	if state != nil && finishReason != "" {
		state.FinishReason = finishReason
	}
	return apicompat.FinalizeChatCompletionsResponsesStream(state)
}

func buildCompatibleForwardResult(
	resp *http.Response,
	prepared *compatiblePreparedRequest,
	usage ClaudeUsage,
	stream bool,
	startTime time.Time,
	firstTokenMs *int,
	responseBody ...[]byte,
) *ForwardResult {
	requestID := ""
	if resp != nil {
		requestID = resp.Header.Get("x-request-id")
	}
	result := &ForwardResult{
		RequestID:     requestID,
		Usage:         usage,
		Model:         prepared.OriginalModel,
		UpstreamModel: prepared.UpstreamModel,
		Stream:        stream,
		Duration:      time.Since(startTime),
		FirstTokenMs:  firstTokenMs,
	}
	var body []byte
	if len(responseBody) > 0 {
		body = responseBody[0]
	}
	applyCompatibleBillableQuantities(result, prepared, body)
	return result
}

func buildCompatibleStreamForwardResult(
	resp *http.Response,
	prepared *compatiblePreparedRequest,
	usage ClaudeUsage,
	startTime time.Time,
	firstTokenMs *int,
	streamErr error,
) *ForwardResult {
	result := buildCompatibleForwardResult(resp, prepared, usage, true, startTime, firstTokenMs)
	if streamErr != nil {
		result.SkipUsageBilling = true
	}
	return result
}

func applyCompatibleBillableQuantities(result *ForwardResult, prepared *compatiblePreparedRequest, responseBody []byte) {
	if result == nil || prepared == nil {
		return
	}
	if !isCompatibleASRRequest(prepared) {
		return
	}
	if seconds, ok := extractBillableDurationSeconds(responseBody, prepared.RequestBody, "application/json", true); ok {
		result.BillableDurationSeconds = seconds
		result.BillableUnitType = BillableUnitTypeDuration
	}
}

func applyCompatibleUsageFallback(result *ForwardResult, account *Account, group *Group, estimatedInputTokens int) {
	if result == nil || account == nil || !account.IsCompatiblePlatform() {
		return
	}
	if result.Usage.InputTokens <= 0 && estimatedInputTokens > 0 {
		result.Usage.InputTokens = estimatedInputTokens
		result.UsageEstimated = true
	}
}

func shouldWarnCompatibleZeroCost(account *Account, group *Group, result *ForwardResult, cost *CostBreakdown) bool {
	if account == nil || result == nil || cost == nil || !account.UseNewAPIStyleInterfaceForGroup(group) {
		return false
	}
	if cost.ActualCost != 0 || cost.TotalCost != 0 {
		return false
	}
	return result.RequestCount > 0 || result.BillableUnitType != "" || result.BillableDurationSeconds > 0 || result.BillableCharacterCount > 0
}

func firstExistingGJSONInt(results ...gjson.Result) int {
	for _, result := range results {
		if result.Exists() {
			return int(result.Int())
		}
	}
	return 0
}

func isCompatibleASRRequest(prepared *compatiblePreparedRequest) bool {
	if prepared == nil || compatiblePreparedClientRoute(prepared) != CompatibleRouteChatCompletions {
		return false
	}
	model := strings.ToLower(strings.TrimSpace(firstNonEmptyText(prepared.OriginalModel, prepared.UpstreamModel)))
	if !strings.Contains(model, "asr") {
		return false
	}
	return requestContainsAudioInput(prepared.RequestBody, "application/json")
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
	_, _ = buf.WriteString(line)
	_ = buf.WriteByte('\n')
}

func flushCompatibleSSEBuffer(c *gin.Context, buf *bytes.Buffer) {
	if c == nil || buf == nil || buf.Len() == 0 {
		return
	}
	_, _ = c.Writer.Write(sanitizeClientVisibleSSEEventBlock(buf.Bytes()))
	c.Writer.Flush()
	buf.Reset()
}

var errCompatibleStreamMissingTerminal = errors.New("compatible upstream stream closed before terminal event")

type compatibleStreamTerminalTracker struct {
	route     CompatibleRequestRoute
	eventType string
	seen      bool
}

func newCompatibleStreamTerminalTracker(route CompatibleRequestRoute) *compatibleStreamTerminalTracker {
	return &compatibleStreamTerminalTracker{route: route}
}

func (t *compatibleStreamTerminalTracker) ObserveLine(line string) {
	if t == nil || t.seen {
		return
	}
	if strings.HasPrefix(line, "event:") {
		t.eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		if t.isTerminalEvent(t.eventType) {
			t.seen = true
		}
		return
	}
	if !strings.HasPrefix(line, "data: ") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
	if payload == "[DONE]" {
		t.seen = true
		return
	}
	eventType := strings.TrimSpace(gjson.Get(payload, "type").String())
	if eventType == "" {
		eventType = t.eventType
	}
	if t.isTerminalEvent(eventType) {
		t.seen = true
	}
}

func (t *compatibleStreamTerminalTracker) Seen() bool {
	return t != nil && t.seen
}

func (t *compatibleStreamTerminalTracker) isTerminalEvent(eventType string) bool {
	switch t.route {
	case CompatibleRouteMessages:
		return eventType == "message_stop" || eventType == "error"
	case CompatibleRouteResponses:
		switch eventType {
		case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "error":
			return true
		default:
			return false
		}
	case CompatibleRouteChatCompletions:
		return eventType == "done" || eventType == "error"
	default:
		return false
	}
}

func logCompatibleStreamScannerError(prepared *compatiblePreparedRequest, err error) {
	if err == nil {
		return
	}
	route := ""
	upstreamEndpoint := ""
	if prepared != nil {
		route = string(compatiblePreparedClientRoute(prepared))
		upstreamEndpoint = prepared.UpstreamEndpoint
	}
	logger.LegacyPrintf(
		"service.compatible_gateway",
		"compatible stream read error route=%s upstream_endpoint=%s: %v",
		route,
		upstreamEndpoint,
		err,
	)
}

func openAIUsageToClaudeUsage(usage OpenAIUsage) ClaudeUsage {
	return ClaudeUsage{
		InputTokens:              openAIUncachedInputTokens(usage.InputTokens, usage.CacheReadInputTokens+usage.CacheCreationInputTokens),
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
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
	}
	if usage.InputTokensDetails != nil {
		out.CacheReadInputTokens = usage.InputTokensDetails.CachedTokens
	}
	out.InputTokens = openAIUncachedInputTokens(out.InputTokens, out.CacheReadInputTokens+out.CacheCreationInputTokens)
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
		out.CacheCreationInputTokens = usage.PromptTokensDetails.EffectiveCacheCreationTokens()
	}
	out.InputTokens = openAIUncachedInputTokens(out.InputTokens, out.CacheReadInputTokens+out.CacheCreationInputTokens)
	return out
}

func openAIUncachedInputTokens(total, cached int) int {
	if total <= cached {
		return 0
	}
	return total - max(cached, 0)
}
