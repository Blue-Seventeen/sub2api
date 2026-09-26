package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

const openAIResponsesReasoningCacheTTL = 24 * time.Hour

// forwardResponsesViaRawChatCompletions serves /v1/responses clients through an
// upstream that only supports /v1/chat/completions.
func (s *OpenAIGatewayService) forwardResponsesViaRawChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	originalModel := strings.TrimSpace(responsesReq.Model)
	if originalModel == "" {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("missing model in request")
	}

	clientStream := responsesReq.Stream
	serviceTier := extractOpenAIServiceTierFromBody(body)
	s.cacheResponsesReasoningItems(ctx, c, responsesReq.Input)
	// custom 工具（如 codex 的 exec）降级为 function 工具转发，回程需按名字还原为
	// custom_tool_call 项，先记下名字集合；tool_search 工具同理，回程还原为
	// tool_search_call 项；namespace 子工具（如 MCP 工具）摊平转发，回程按映射还原
	// 为带 namespace 字段的 function_call 项。
	effectiveTools, err := apicompat.EffectiveResponsesTools(&responsesReq)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("resolve responses tools: %w", err)
	}
	customTools := apicompat.CustomToolNames(effectiveTools)
	toolSearch := apicompat.HasToolSearchTool(effectiveTools)
	namespaceTools := apicompat.NamespaceToolNames(effectiveTools)

	chatReq, err := apicompat.ResponsesToChatCompletionsRequestWithOptions(
		&responsesReq,
		&apicompat.ResponsesToChatOptions{ReasoningContentByID: s.cachedResponsesReasoningContent(ctx, c)},
	)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("convert responses to chat completions: %w", err)
	}

	billingModel := resolveOpenAIForwardModel(account, originalModel, "")
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	reasoningEffort := extractOpenAIReasoningEffortFromBody(body, upstreamModel, billingModel, originalModel)
	// 国产模型默认 effort 补充：需要 mappedModel 判定，推迟到 billingModel 算出之后。
	reasoningEffort = ApplyThinkingEnabledFallback(reasoningEffort, body, billingModel)
	chatReq.Model = upstreamModel
	if clientStream {
		chatReq.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}

	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions fallback request: %w", err)
	}
	chatBody, fastPolicyTier, err := s.applyOpenAIFastPolicyToBody(ctx, account, upstreamModel, chatBody)
	if err != nil {
		var blocked *OpenAIFastBlockedError
		if errors.As(err, &blocked) {
			writeOpenAIFastPolicyBlockedResponse(c, blocked)
		}
		return nil, err
	}
	if serviceTier == nil {
		serviceTier = fastPolicyTier
	}
	if serviceTier == nil {
		serviceTier = extractOpenAIServiceTierFromBody(chatBody)
	}
	chatBody = applyOllamaCloudRawChatCompletionsRequest(account, chatBody)
	chatBody = clampOllamaCloudUpstreamMaxTokens(account, chatBody)

	logger.L().Debug("openai responses: forwarding via raw chat completions",
		zap.Int64("account_id", account.ID),
		zap.String("original_model", originalModel),
		zap.String("billing_model", billingModel),
		zap.String("upstream_model", upstreamModel),
		zap.Bool("stream", clientStream),
	)

	// Build and send upstream request via the shared CC pipeline
	apiKey, targetURL, err := s.resolveCCFallbackTarget(account)
	if err != nil {
		return nil, err
	}
	resp, err := s.sendCCUpstreamRequest(ctx, c, account, targetURL, chatBody, clientStream, apiKey, account.GetOpenAIUserAgent(), "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, upstreamMsg := s.readOpenAIUpstreamError(resp)
		if foErr := s.failoverOpenAIUpstreamHTTPError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel); foErr != nil {
			return nil, foErr
		}
		return s.handleErrorResponse(ctx, resp, c, account, chatBody, billingModel)
	}

	if clientStream {
		return s.streamChatCompletionsAsResponses(c, resp, account, originalModel, customTools, toolSearch, namespaceTools, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
	}
	return s.bufferChatCompletionsAsResponses(c, resp, account, originalModel, customTools, toolSearch, namespaceTools, billingModel, upstreamModel, reasoningEffort, serviceTier, startTime)
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	ccResp, usage, err := s.readCCUpstreamJSONResponse(c, resp, account, writeOpenAIResponsesFallbackError)
	if err != nil {
		return nil, err
	}
	responsesResp := apicompat.ChatCompletionsResponseToResponses(ccResp, originalModel, customTools, toolSearch, namespaceTools)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.JSON(http.StatusOK, responsesResp)

	return &OpenAIForwardResult{
		RequestID:                   requestID,
		Usage:                       usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		ReasoningEffort:             reasoningEffort,
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      false,
		Duration:                    time.Since(startTime),
	}, nil
}

func (s *OpenAIGatewayService) streamChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	originalModel string,
	customTools map[string]bool,
	toolSearch bool,
	namespaceTools map[string]apicompat.NamespacedToolName,
	billingModel string,
	upstreamModel string,
	reasoningEffort *string,
	serviceTier *string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")
	writeStreamHeaders := s.newStreamHeaderWriter(c, resp.Header)

	state := apicompat.NewChatCompletionsToResponsesStreamState(originalModel)
	state.CustomTools = customTools
	state.ToolSearchDeclared = toolSearch
	state.NamespaceTools = namespaceTools
	clientDisconnected := false

	writeEvents := func(events []apicompat.ResponsesStreamEvent) {
		if clientDisconnected || len(events) == 0 {
			return
		}
		writeStreamHeaders()
		for _, event := range events {
			sse, err := apicompat.ResponsesEventToSSE(event)
			if err != nil {
				logger.L().Warn("openai responses chat fallback: failed to marshal stream event",
					zap.Error(err),
					zap.String("request_id", requestID),
				)
				continue
			}
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				clientDisconnected = true
				logger.L().Debug("openai responses chat fallback: client disconnected, continuing to drain upstream for billing",
					zap.Error(err),
					zap.String("request_id", requestID),
				)
				return
			}
		}
		c.Writer.Flush()
	}

	scan := s.scanCCStream(c, resp, "openai responses chat fallback", requestID, startTime, func(chunk *apicompat.ChatCompletionsChunk) {
		writeEvents(apicompat.ChatCompletionsChunkToResponsesEvents(chunk, state))
	})

	if len(scan.ErrorPayload) > 0 {
		// Resolve custom rules before failover, and never finalize/cache errors.
		terminalErr := s.handleCCUpstreamErrorPayload(c, resp, account, scan.ErrorPayload, !c.Writer.Written() && !clientDisconnected,
			func(c *gin.Context, status int, errType, message string) {
				if clientDisconnected {
					return
				}
				if !c.Writer.Written() {
					writeOpenAIResponsesFallbackError(c, status, errType, message)
					return
				}
				// Use only the classified, redacted fields, not the upstream body.
				source, _ := json.Marshal(gin.H{"error": gin.H{"type": errType, "message": message}})
				if _, err := fmt.Fprint(c.Writer, buildOpenAIResponseFailedSSE(state.ResponseID, originalModel, source, message)); err != nil {
					clientDisconnected = true
				} else {
					c.Writer.Flush()
				}
			})
		return &OpenAIForwardResult{
			RequestID:                   requestID,
			UpstreamHeaders:             resp.Header,
			Usage:                       scan.Usage,
			Model:                       originalModel,
			BillingModel:                billingModel,
			UpstreamModel:               upstreamModel,
			UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
			ReasoningEffort:             reasoningEffort,
			ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
			Stream:                      true,
			Duration:                    time.Since(startTime),
			FirstTokenMs:                scan.FirstTokenMs,
			ClientDisconnect:            clientDisconnected,
		}, terminalErr
	}

	if scan.Err != nil {
		return &OpenAIForwardResult{
			RequestID:       requestID,
			Usage:           scan.Usage,
			Model:           originalModel,
			BillingModel:    billingModel,
			UpstreamModel:   upstreamModel,
			ReasoningEffort: reasoningEffort,
			ServiceTier:     serviceTier,
			Stream:          true,
			Duration:        time.Since(startTime),
			FirstTokenMs:    scan.FirstTokenMs,
		}, fmt.Errorf("stream usage incomplete: %w", scan.Err)
	}

	finalEvents := apicompat.FinalizeChatCompletionsResponsesStream(state)
	if state.FinishReason == "length" {
		if err := validateResponsesToolArguments(finalEvents); err != nil {
			return &OpenAIForwardResult{
				RequestID:       requestID,
				Usage:           scan.Usage,
				Model:           originalModel,
				BillingModel:    billingModel,
				UpstreamModel:   upstreamModel,
				ReasoningEffort: reasoningEffort,
				ServiceTier:     resolvedOpenAIUpstreamServiceTier(c, serviceTier),
				Stream:          true,
				Duration:        time.Since(startTime),
				FirstTokenMs:    scan.FirstTokenMs,
			}, err
		}
	}
	cacheCtx := context.Background()
	if c != nil && c.Request != nil {
		cacheCtx = c.Request.Context()
	}
	s.cacheResponsesStreamReasoning(cacheCtx, c, state)
	writeEvents(finalEvents)
	if !clientDisconnected {
		writeStreamHeaders()
		if _, err := fmt.Fprint(c.Writer, "data: [DONE]\n\n"); err != nil {
			clientDisconnected = true
		}
		if !clientDisconnected {
			c.Writer.Flush()
		}
	}
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai responses chat fallback", requestID)
	}

	return &OpenAIForwardResult{
		RequestID:                   requestID,
		Usage:                       scan.Usage,
		Model:                       originalModel,
		BillingModel:                billingModel,
		UpstreamModel:               upstreamModel,
		UpstreamResponseServiceTier: observedUpstreamResponseServiceTier(c),
		ReasoningEffort:             reasoningEffort,
		ServiceTier:                 resolvedOpenAIUpstreamServiceTier(c, serviceTier),
		Stream:                      true,
		Duration:                    time.Since(startTime),
		FirstTokenMs:                scan.FirstTokenMs,
	}, nil
}

func (s *OpenAIGatewayService) responsesReasoningCache(c *gin.Context) (ScopedReasoningContentCache, int64, int64) {
	if s == nil || c == nil {
		return nil, 0, 0
	}
	cache, ok := s.cache.(ScopedReasoningContentCache)
	if !ok {
		return nil, 0, 0
	}
	// Only the post-authentication API key context is authoritative, never
	// request headers, body metadata, group IDs, or upstream account IDs.
	value, _ := c.Get("api_key")
	key, ok := value.(*APIKey)
	if !ok || key == nil || key.UserID <= 0 || key.ID <= 0 {
		return nil, 0, 0
	}
	return cache, key.UserID, key.ID
}

func (s *OpenAIGatewayService) cachedResponsesReasoningContent(ctx context.Context, c *gin.Context) func(string) string {
	cache, userID, apiKeyID := s.responsesReasoningCache(c)
	return func(itemID string) string {
		if cache == nil || strings.TrimSpace(itemID) == "" {
			return ""
		}
		content, err := cache.GetScopedReasoningContent(ctx, userID, apiKeyID, strings.TrimSpace(itemID))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(content)
	}
}

func (s *OpenAIGatewayService) cacheResponsesReasoningItems(ctx context.Context, c *gin.Context, input json.RawMessage) {
	cache, userID, apiKeyID := s.responsesReasoningCache(c)
	if cache == nil {
		return
	}
	parsed := gjson.ParseBytes(input)
	if !parsed.IsArray() {
		return
	}
	for _, item := range parsed.Array() {
		if strings.TrimSpace(item.Get("type").String()) != "reasoning" {
			continue
		}
		itemID := strings.TrimSpace(item.Get("id").String())
		content := responsesReasoningItemText(item)
		if itemID == "" || content == "" {
			continue
		}
		_ = cache.SetScopedReasoningContent(ctx, userID, apiKeyID, itemID, content, openAIResponsesReasoningCacheTTL)
	}
}

func (s *OpenAIGatewayService) cacheResponsesStreamReasoning(ctx context.Context, c *gin.Context, state *apicompat.ChatCompletionsToResponsesStreamState) {
	cache, userID, apiKeyID := s.responsesReasoningCache(c)
	if cache == nil || state == nil {
		return
	}
	itemID := strings.TrimSpace(state.ReasoningItemID)
	content := strings.TrimSpace(state.Reasoning.String())
	if itemID == "" || content == "" {
		return
	}
	_ = cache.SetScopedReasoningContent(ctx, userID, apiKeyID, itemID, content, openAIResponsesReasoningCacheTTL)
}

func responsesReasoningItemText(item gjson.Result) string {
	var parts []string
	for _, summary := range item.Get("summary").Array() {
		if text := strings.TrimSpace(summary.Get("text").String()); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		for _, part := range item.Get("content").Array() {
			if text := strings.TrimSpace(part.Get("text").String()); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func validateResponsesToolArguments(events []apicompat.ResponsesStreamEvent) error {
	for _, event := range events {
		if strings.TrimSpace(event.Type) != "response.function_call_arguments.done" {
			continue
		}
		arguments := strings.TrimSpace(event.Arguments)
		if arguments != "" && !json.Valid([]byte(arguments)) {
			return fmt.Errorf("invalid JSON tool arguments at output limit")
		}
	}
	return nil
}

func chatChunkStartsResponsesOutput(chunk *apicompat.ChatCompletionsChunk) bool {
	if chunk == nil {
		return false
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil || choice.Delta.ReasoningContent != nil || len(choice.Delta.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

const deepSeekChatReasoningPlaceholderText = " "

// targetsDeepSeekAPIHost reports whether the selected account sends requests
// to the official DeepSeek API, including OpenAI-compatible mapped accounts.
func targetsDeepSeekAPIHost(account *Account) bool {
	if account == nil {
		return false
	}
	if account.Platform == PlatformDeepseek {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(account.GetOpenAIBaseURL()))
	if err != nil {
		return false
	}
	deepseekURL, err := url.Parse(DefaultDeepseekBaseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), deepseekURL.Hostname())
}

// ensureDeepSeekChatReasoningPlaceholders fills missing assistant reasoning
// fields required by DeepSeek thinking mode without overwriting real content.
func ensureDeepSeekChatReasoningPlaceholders(account *Account, body []byte) []byte {
	if !targetsDeepSeekAPIHost(account) {
		return body
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return body
	}
	updated := body
	changed := false
	for i, msg := range messages.Array() {
		if strings.TrimSpace(msg.Get("role").String()) != "assistant" || msg.Get("reasoning_content").String() != "" {
			continue
		}
		next, err := sjson.SetBytes(updated, "messages."+strconv.Itoa(i)+".reasoning_content", deepSeekChatReasoningPlaceholderText)
		if err != nil {
			return body
		}
		updated = next
		changed = true
	}
	if !changed {
		return body
	}
	return updated
}
