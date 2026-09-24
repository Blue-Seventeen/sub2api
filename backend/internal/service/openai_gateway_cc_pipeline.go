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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// 本文件收敛三个 CC（Chat Completions）forwarder 之间重复的 HTTP 管线与 SSE
// 循环骨架（PR #3802 遗留项）：
//
//   - forwardAsRawChatCompletions          （原生 CC 直转）
//   - forwardResponsesViaRawChatCompletions（/v1/responses → CC 回退）
//   - forwardAnthropicViaRawChatCompletions（/v1/messages → CC 回退）
//
// 以及 messages / chat_completions 两条 Responses 主路径中逐字相同的错误处理块。
// 所有 helper 都是对既有内联代码的等价提取，不改变任何行为；各路径的差异
// （GLM effort 归一化、fast policy、Grok 分支、ClientDisconnect 语义等）仍留在
// 调用方，属于有意保留的行为差异，不在此强行统一。

// newUpstreamSSEScanner 构造读取上游 SSE 流的行扫描器，按配置放大单行上限。
func (s *OpenAIGatewayService) newUpstreamSSEScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	return scanner
}

// newChatCompletionsUpstreamContext detaches the request from client
// cancellation while retaining an explicit lifecycle cancel for the response
// body. Chat Completions must cancel that context before closing the body so
// transports and custom response readers can terminate promptly.
func newChatCompletionsUpstreamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
		// A request that was already canceled before forwarding is kept fully
		// detached for the existing billing/drain behavior. Active requests get
		// an explicit lifecycle cancel below.
		if ctx.Err() != nil {
			return base, func() {}
		}
	}
	return context.WithCancel(base)
}

// newStreamHeaderWriter 返回幂等的 SSE 响应头写入闭包：首次调用时透传过滤后的
// 上游响应头并写入标准 SSE 头 + 200 状态码，后续调用为 no-op。延迟到首个事件
// 写出前才提交响应头，使上游早期失败仍可改走 failover 或非流式错误响应。
func (s *OpenAIGatewayService) newStreamHeaderWriter(c *gin.Context, upstream http.Header) func() {
	headersWritten := false
	return func() {
		if headersWritten {
			return
		}
		headersWritten = true
		if s.responseHeaderFilter != nil {
			responseheaders.WriteFilteredHeaders(c.Writer.Header(), upstream, s.responseHeaderFilter)
		}
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Header().Set("X-Accel-Buffering", "no")
		c.Writer.WriteHeader(http.StatusOK)
	}
}

// readOpenAIUpstreamError 读取上游错误体并把 resp.Body 回卷为可重读的副本
// （下游 handleXxxErrorResponse 需要再次读取），返回原始错误体与脱敏后的
// 上游错误消息。
func (s *OpenAIGatewayService) readOpenAIUpstreamError(resp *http.Response) ([]byte, string) {
	respBody := s.readUpstreamErrorBody(resp)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	return respBody, upstreamMsg
}

// failoverOpenAIUpstreamHTTPError 对 >=400 的上游响应做 failover 判定：命中时
// 记录 ops 事件、执行账号级错误处置并返回 *UpstreamFailoverError；未命中返回
// nil，调用方继续走各自端点格式的非 failover 错误处理链。
func (s *OpenAIGatewayService) failoverOpenAIUpstreamHTTPError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	resp *http.Response,
	respBody []byte,
	upstreamMsg string,
	upstreamModel string,
) *UpstreamFailoverError {
	shouldFailover := s.shouldFailoverOpenAIUpstreamResponse(account, resp.StatusCode, upstreamMsg, respBody)
	tempUnscheduled := false
	responseNotCommitted := c == nil || !IsResponseCommitted(c)
	shouldCheckPolicy := c != nil || (account != nil && account.IsPoolMode())
	if account != nil && account.Platform != PlatformGrok && responseNotCommitted && shouldCheckPolicy &&
		(!shouldFailover || account.IsPoolMode()) && s.rateLimitService != nil {
		tempUnscheduled = s.rateLimitService.CheckErrorPolicy(ctx, account, resp.StatusCode, respBody, upstreamModel) == ErrorPolicyTempUnscheduled
		shouldFailover = shouldFailover || tempUnscheduled
	}
	if account != nil && account.Platform == PlatformGrok {
		shouldFailover = s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody)
	}
	if account != nil && account.Platform == PlatformGrok {
		s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
	}
	if !shouldFailover {
		return nil
	}
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(respBody), maxBytes)
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: resp.StatusCode,
		UpstreamRequestID:  resp.Header.Get("x-request-id"),
		Kind:               "failover",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	shouldDisable := tempUnscheduled
	if account.Platform != PlatformGrok && !tempUnscheduled {
		// The base failover decision already identified this response as
		// retryable. Preserve the normal account side effects, but do not let
		// their generic error-policy path reclassify it as model-temp-unschedulable.
		shouldDisable = s.handleOpenAIAccountUpstreamError(
			withTempUnschedulablePolicySkipped(ctx),
			account,
			resp.StatusCode,
			resp.Header,
			respBody,
			upstreamModel,
		)
	}
	return s.newOpenAIAccountFailoverError(
		account,
		resp.StatusCode,
		resp.Header,
		respBody,
		upstreamMsg,
		shouldDisable,
		!shouldDisable && account.IsPoolMode() && (account.IsPoolModeRetryableStatus(resp.StatusCode) || isOpenAITransientProcessingError(resp.StatusCode, upstreamMsg, respBody)),
	)
}

// openAIChatCompletionsTargetURL 解析账号的（非 Grok）Chat Completions 上游端点。
func (s *OpenAIGatewayService) openAIChatCompletionsTargetURL(account *Account) (string, error) {
	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return buildOpenAIChatCompletionsURL(validatedURL), nil
}

// resolveCCFallbackTarget 解析两条 CC 回退路径共用的账号凭证与上游端点
// （回退路径仅面向 APIKey 账号，凭证恒为 openai api_key）。
func (s *OpenAIGatewayService) resolveCCFallbackTarget(account *Account) (apiKey string, targetURL string, err error) {
	apiKey = strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if apiKey == "" {
		return "", "", fmt.Errorf("account %d missing api_key", account.ID)
	}
	targetURL, err = s.openAIChatCompletionsTargetURL(account)
	if err != nil {
		return "", "", err
	}
	return apiKey, targetURL, nil
}

// sendCCUpstreamRequest 构建并发送 CC 上游请求：分离的上游 context、OpenAI HTTP
// profile、标准头（含流式 Accept 切换）、客户端 header 白名单透传、自定义 UA 与
// 账号级 header 覆写，最后经代理发出。传输层失败（DNS/TCP/TLS，无 HTTP 响应）
// 统一由 handleOpenAIUpstreamTransportError 归一为 failover。
//
// userAgent 为空时保留默认 UA；Grok 的默认 UA 兜底由调用方解析后传入。
func (s *OpenAIGatewayService) sendCCUpstreamRequest(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	targetURL string,
	body []byte,
	stream bool,
	bearerToken string,
	userAgent string,
	grokCacheIdentity string,
) (*http.Response, error) {
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		releaseUpstreamCtx()
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	// 记录本次实际选择的协议端点，供错误日志和用量日志在没有
	// OpenAIForwardResult（例如 503/传输失败）时使用。每次发送都覆盖，
	// 避免 Gin context 在账号 failover 尝试之间残留旧端点。
	SetActualOpenAIUpstreamEndpoint(c, "/v1/chat/completions")
	upstreamReq = upstreamReq.WithContext(WithHTTPUpstreamProfile(upstreamReq.Context(), HTTPUpstreamProfileOpenAI))
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+bearerToken)
	if stream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}

	// 透传白名单中的客户端 header。详见 openaiCCRawAllowedHeaders 的设计说明。
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if openaiCCRawAllowedHeaders[lowerKey] {
			for _, v := range values {
				upstreamReq.Header.Add(key, v)
			}
		}
	}
	if userAgent != "" {
		upstreamReq.Header.Set("user-agent", userAgent)
	}

	if account.Platform == PlatformGrok {
		if account.IsGrokOAuth() {
			applyGrokCLIHeaders(upstreamReq.Header)
		}
		applyGrokCacheHeaders(upstreamReq.Header, grokCacheIdentity)
	}
	// 账号级请求头覆写：放在所有内置默认头（含 Grok CLI 身份头）之后应用，
	// 使配置值获得除共享传输层强制头之外的最高优先级。
	account.ApplyHeaderOverrides(upstreamReq.Header)
	applyOpenCodeSessionHeader(c, account, targetURL, upstreamReq.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.doOpenAIUpstream(upstreamReq, proxyURL, account)
	if err != nil {
		releaseUpstreamCtx()
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	if resp == nil || resp.Body == nil {
		releaseUpstreamCtx()
		return nil, errors.New("openai upstream returned an empty response body")
	}
	resp.Body = &openAIRequestContextReadCloser{
		ReadCloser: resp.Body,
		cleanup:    releaseUpstreamCtx,
	}
	return resp, nil
}

// ccStreamScanState 是 scanCCStream 返回的读取状态快照。
type ccStreamScanState struct {
	// Usage 为 include_usage chunk 中最近一次出现的用量（上游可能重复发送，
	// 总是保留最新值）；终态事件中的用量由调用方在 finalize 阶段自行覆盖。
	Usage OpenAIUsage
	// FirstTokenMs 为首个实际输出 chunk（排除 usage-only chunk）的到达时延。
	FirstTokenMs *int
	// SawDone 表示上游发出了 [DONE] 哨兵。
	SawDone bool
	// Err 为 scanner 读错误（客户端 context 取消不属于此类，会原样带出）。
	// 非 nil 时调用方必须跳过 finalize 并返回 usage-incomplete 错误，避免
	// 把上游截断伪装成正常收尾。
	Err error
	// ErrorPayload is a terminal bare error event emitted by a Responses-like
	// provider over the Chat Completions SSE transport.
	ErrorPayload []byte
}

// scanCCStream 驱动两条 CC 回退路径共享的 SSE 读循环：提取 data 行、在 [DONE]
// 哨兵处停止、保留最新 usage、记录首 token 时延，并把每个解析成功的 chunk 交给
// emit 回调做各自的协议转换与写出。读错误按既有约定过滤 context 取消类噪声后
// 记入 Warn 日志。
func (s *OpenAIGatewayService) scanCCStream(
	c *gin.Context,
	resp *http.Response,
	logPrefix string,
	requestID string,
	startTime time.Time,
	emit func(*apicompat.ChatCompletionsChunk),
) ccStreamScanState {
	var st ccStreamScanState

	scanner := s.newUpstreamSSEScanner(resp.Body)
	var namedEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			namedEvent = ""
			continue
		}
		if name, ok := extractOpenAISSEEventLine(line); ok {
			namedEvent = strings.TrimSpace(name)
			continue
		}
		payload, ok := extractOpenAISSEDataLine(line)
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			st.SawDone = true
			break
		}
		if isCCUpstreamErrorPayload(namedEvent, []byte(payload)) {
			st.ErrorPayload = append(st.ErrorPayload[:0], []byte(payload)...)
			break
		}
		// 观察上游 CC chunk 回显的 model / service_tier（计费以回显为准）。
		// CC chunk 无 type 字段，按 untyped payload 观察（上游约束：只有终止
		// 事件与无类型 body 报告实际处理档位）。
		if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
			observer.ObserveOpenAI([]byte(payload), "")
		}

		if u := extractCCStreamUsage(payload); u != nil {
			st.Usage = *u
		}

		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			logger.L().Warn(logPrefix+": failed to parse chat stream chunk",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
			continue
		}
		if st.FirstTokenMs == nil && !isOpenAIChatUsageOnlyStreamChunk(payload) && chatChunkStartsResponsesOutput(&chunk) {
			ms := int(time.Since(startTime).Milliseconds())
			st.FirstTokenMs = &ms
		}
		emit(&chunk)
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn(logPrefix+": stream read error",
				zap.Error(err),
				zap.String("request_id", requestID),
			)
		}
		st.Err = err
	}
	return st
}

// logCCStreamMissingDoneSentinel 记录"上游未发 [DONE] 哨兵即结束"的 debug 日志。
func logCCStreamMissingDoneSentinel(logPrefix, requestID string) {
	logger.L().Debug(logPrefix+": upstream stream ended without done sentinel",
		zap.String("request_id", requestID),
	)
}

// readCCUpstreamJSONResponse 读取并解析 CC 非流式 JSON 响应，失败时以调用方
// 端点格式回写错误；成功时顺带提取 usage。
func (s *OpenAIGatewayService) readCCUpstreamJSONResponse(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	writeError compatErrorWriter,
) (*apicompat.ChatCompletionsResponse, OpenAIUsage, error) {
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
		}
		return nil, OpenAIUsage{}, fmt.Errorf("read upstream body: %w", err)
	}

	if isCCUpstreamErrorPayload("", respBody) {
		return nil, OpenAIUsage{}, s.handleCCUpstreamErrorPayload(c, resp, account, respBody, true, writeError)
	}
	var ccResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(respBody, &ccResp); err != nil {
		writeError(c, http.StatusBadGateway, "api_error", "Failed to parse upstream response")
		return nil, OpenAIUsage{}, fmt.Errorf("parse chat completions response: %w", err)
	}
	// 观察上游 CC JSON 回显的 model / service_tier（计费以回显为准）。
	// CC JSON 无 type 字段，按 untyped payload 观察（上游约束）。
	if observer := upstreamResponseModelObserverFromContext(c); observer != nil {
		observer.ObserveOpenAI(respBody, "")
	}

	usage := OpenAIUsage{}
	if parsed, ok := extractOpenAIUsageFromJSONBytes(respBody); ok {
		usage = parsed
	}
	return &ccResp, usage, nil
}

func isCCUpstreamErrorPayload(namedEvent string, payload []byte) bool {
	if namedEvent == "error" || namedEvent == "response.failed" {
		return true
	}
	event := gjson.ParseBytes(payload)
	eventType := strings.TrimSpace(event.Get("type").String())
	return eventType == "error" || eventType == "response.failed" ||
		hasClientVisibleErrorValue(event.Get("error")) || hasClientVisibleErrorValue(event.Get("response.error"))
}

// HTTP 200 is only the transport status: an error envelope must never enter
// successful conversion/billing. Custom rules take precedence over retries.
func (s *OpenAIGatewayService) handleCCUpstreamErrorPayload(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	payload []byte,
	allowFailover bool,
	writeError compatErrorWriter,
) error {
	message := extractOpenAISSEErrorMessage(payload)
	platform := PlatformOpenAI
	if account != nil {
		platform = account.Platform
	}
	status, errType, errMsg, matched := applyOpenAIStreamFailedErrorPassthroughRule(c, platform, payload, message)
	if !matched {
		if allowFailover && openAIStreamErrorEventShouldFailover(payload, message) {
			return s.newOpenAIStreamFailoverError(c, account, false, resp.Header.Get("x-request-id"), payload, message, resp.Header)
		}
		status = openAIStreamFailedEventSemanticStatus(payload, message)
		errType = firstNonEmpty(gjson.GetBytes(payload, "error.type").String(), gjson.GetBytes(payload, "response.error.type").String(), "upstream_error")
		errMsg = message
	}
	s.recordOpenAIStreamUpstreamError(c, account, false, resp.Header.Get("x-request-id"), "http_error", payload, message)
	if strings.TrimSpace(errMsg) == "" {
		errMsg = "Upstream request failed"
	}
	errMsg = sanitizeUserVisibleErrorText(sanitizeUpstreamErrorMessage(errMsg))
	errType = sanitizeUserVisibleErrorText(sanitizeUpstreamErrorMessage(errType))
	MarkResponseCommitted(c)
	writeError(c, status, errType, errMsg)
	return fmt.Errorf("upstream response failed: %s", errMsg)
}

// writeOpenAIResponsesFallbackError 以 /v1/responses 回退路径的既有错误格式回写
// （裸 error 对象；不调用 MarkResponseCommitted，与原内联写法保持一致）。
func writeOpenAIResponsesFallbackError(c *gin.Context, statusCode int, errType, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
	})
}
