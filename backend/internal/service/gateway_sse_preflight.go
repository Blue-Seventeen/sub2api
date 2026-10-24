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
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// poolModeHTTPFailover leaves attempt budgeting to the handler. Explicit local
// error policies still run first and can retire an account instead of retrying it.
func poolModeHTTPFailover(ctx context.Context, rateLimit *RateLimitService, account *Account, status int, headers http.Header, body []byte, model string) *UpstreamFailoverError {
	if account == nil || (ctx != nil && ctx.Err() != nil) {
		return nil
	}
	if isDefinitiveUpstreamRequestError(status, body) {
		return nil
	}
	if !account.ShouldHandleErrorCode(status) {
		return nil
	}
	disabled := false
	if rateLimit != nil {
		disabled = rateLimit.HandleUpstreamError(ctx, account, status, headers, body, model)
	} else if account.IsCustomErrorCodesEnabled() && account.ShouldHandleErrorCode(status) {
		// Without a policy backend we cannot safely keep retrying an account
		// explicitly configured to stop on this status.
		disabled = true
	}
	configured := account.IsPoolMode() && account.IsPoolModeRetryableStatus(status)
	if !newAPIStyleShouldFailoverStatus(status) && !configured && !disabled {
		return nil
	}
	return &UpstreamFailoverError{
		StatusCode: status, ResponseBody: sanitizeClientVisibleUpstreamErrorPayload(body),
		ResponseHeaders: headers.Clone(), RetryableOnSameAccount: configured && !disabled,
	}
}

type upstreamSSEProtocol uint8

func isDefinitiveUpstreamRequestError(status int, body []byte) bool {
	// Strong request-level markers stay terminal even when an intermediary
	// wraps them in a retryable HTTP status. Do not retire the credential.
	for _, path := range []string{"error.type", "response.error.type", "type", "error.code", "response.error.code", "code"} {
		switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, path).String())) {
		case "cyber_policy", "content_policy_violation", "content_policy", "content_filter", "content_moderation":
			return true
		}
	}
	if status != http.StatusBadRequest {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(ExtractUpstreamErrorMessage(body)))
	if isOpenAITransientProcessingError(status, message, body) || isGoogleProjectConfigError(message) {
		return false
	}
	if isOpenAIContextWindowError(message, body) {
		return true
	}
	for _, path := range []string{"error.type", "response.error.type", "type", "error.code", "response.error.code", "code"} {
		switch strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, path).String())) {
		case "invalid_request_error", "invalid_request", "invalid_json", "invalid_json_error", "context_length_exceeded":
			return true
		}
	}
	for _, phrase := range []string{"invalid json", "malformed json", "failed to parse json", "could not parse the json", "missing required parameter"} {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}

const (
	upstreamSSEChat upstreamSSEProtocol = iota
	upstreamSSEResponses
	upstreamSSEMessages
	upstreamSSEGemini
)

const upstreamSSEPreflightMaxBytes = 8 << 20

type upstreamSSEPreflightFailure struct {
	status   int
	body     []byte
	message  string
	terminal bool
}

func (f *upstreamSSEPreflightFailure) Error() string { return f.message }

func (f *upstreamSSEPreflightFailure) failover(ctx context.Context, rateLimit *RateLimitService, account *Account, headers http.Header, model string) *UpstreamFailoverError {
	if f.terminal {
		return nil
	}
	return poolModeHTTPFailover(ctx, rateLimit, account, f.status, headers, f.body, model)
}

type upstreamPreflightReadError struct{ err error }

func (r *upstreamPreflightReadError) Read([]byte) (int, error) {
	if r.err == nil {
		return 0, io.EOF
	}
	err := r.err
	r.err = nil
	return 0, err
}

type upstreamSSEReplayBody struct {
	io.Reader
	io.Closer
}

func newUpstreamSSEPreflightFailure(status int, message string, body []byte) *upstreamSSEPreflightFailure {
	if len(body) == 0 {
		body, _ = json.Marshal(map[string]any{"error": map[string]any{"type": "upstream_error", "message": message}})
	}
	return &upstreamSSEPreflightFailure{status: status, body: body, message: message}
}

// inspectUpstreamSSEPrefix never writes downstream. It stops at the first
// replay-unsafe event and restores every consumed byte for the existing relay.
// A live semantic event therefore does not wait for a terminal event or EOF.
func inspectUpstreamSSEPrefix(ctx context.Context, resp *http.Response, protocol upstreamSSEProtocol, idleTimeout time.Duration) *upstreamSSEPreflightFailure {
	if resp == nil || resp.Body == nil {
		return newUpstreamSSEPreflightFailure(http.StatusBadGateway, "Upstream returned an empty stream", nil)
	}
	original := resp.Body
	reader := bufio.NewReaderSize(original, 32<<10)
	var prefix, event bytes.Buffer
	var consumedReadErr error
	defer func() {
		resp.Body = &upstreamSSEReplayBody{Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), &upstreamPreflightReadError{err: consumedReadErr}, reader), Closer: original}
	}()

	// Closing the response body interrupts a blocked read. Join the watcher
	// before returning so a previous attempt cannot close a reused response.
	done, watcherDone := make(chan struct{}), make(chan struct{})
	var timer *time.Timer
	var timeout <-chan time.Time
	if idleTimeout > 0 {
		// This is an upper bound for the complete pre-semantic phase. Do not
		// reset it for arbitrary metadata that never makes useful progress.
		timer = time.NewTimer(idleTimeout)
		timeout = timer.C
		defer timer.Stop()
	}
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			_ = original.Close()
		case <-timeout:
			_ = original.Close()
		case <-done:
		}
	}()
	defer func() { close(done); <-watcherDone }()

	linePrefixBytes := 0
	for {
		fragment, readErr := reader.ReadSlice('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, bufio.ErrBufferFull) {
			consumedReadErr = readErr
		}
		_, _ = prefix.Write(fragment)
		if prefix.Len() > upstreamSSEPreflightMaxBytes {
			// An oversized event may already represent accepted tool/media work.
			// Keep every consumed byte and let the existing relay apply its own
			// limits; lack of a classified boundary is not evidence for replay.
			return nil
		}
		_, _ = event.Write(fragment)
		if errors.Is(readErr, bufio.ErrBufferFull) {
			linePrefixBytes += len(fragment)
			continue
		}
		if ctx.Err() != nil {
			return newUpstreamSSEPreflightFailure(http.StatusBadGateway, "Upstream stream canceled", nil)
		}
		line := bytes.TrimRight(fragment, "\r\n")
		blankLine := linePrefixBytes == 0 && len(line) == 0
		linePrefixBytes = 0
		if blankLine || readErr != nil {
			boundary, failure := classifyUpstreamSSEEvent(event.Bytes(), protocol)
			event.Reset()
			if boundary || failure != nil {
				return failure
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return newUpstreamSSEPreflightFailure(http.StatusBadGateway, "Upstream stream ended before semantic output", nil)
			}
			return newUpstreamSSEPreflightFailure(http.StatusBadGateway, "Upstream stream disconnected before semantic output", nil)
		}
	}
}

func classifyUpstreamSSEEvent(raw []byte, protocol upstreamSSEProtocol) (bool, *upstreamSSEPreflightFailure) {
	var data []string
	eventType := ""
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte{'{'}) || bytes.HasPrefix(bytes.TrimSpace(line), []byte{'['}) {
			data = append(data, string(line))
			continue
		}
		field, value, found := bytes.Cut(line, []byte{':'})
		if !found {
			// Some upstreams return JSON despite an SSE content type.
			if bytes.HasPrefix(bytes.TrimSpace(line), []byte{'{'}) || bytes.HasPrefix(bytes.TrimSpace(line), []byte{'['}) {
				data = append(data, string(line))
			}
			continue
		}
		value = bytes.TrimPrefix(value, []byte{' '})
		switch string(field) {
		case "event":
			eventType = strings.TrimSpace(string(value))
		case "data":
			data = append(data, string(value))
		}
	}
	payload := []byte(strings.TrimSpace(strings.Join(data, "\n")))
	if len(payload) == 0 {
		return false, nil
	}
	if bytes.Equal(payload, []byte("[DONE]")) {
		return true, nil
	}
	if !gjson.ValidBytes(payload) {
		// Unknown data is a conservative replay boundary; do not buffer a
		// proprietary or binary protocol until it happens to resemble JSON.
		return true, nil
	}
	if protocol == upstreamSSEGemini {
		if inner := gjson.GetBytes(payload, "response"); inner.IsObject() {
			payload = []byte(inner.Raw)
		}
	}
	// An error terminal can carry billed usage or accumulated content/tool
	// output even with no preceding delta. Release it for settlement/error
	// delivery instead of replaying already accepted work.
	if upstreamSSEPayloadHasUsage(payload) || openAIResponsesPayloadHasOutput(payload) {
		return true, nil
	}
	if typ := strings.TrimSpace(gjson.GetBytes(payload, "type").String()); typ != "" {
		eventType = typ
	}
	if eventType == "ping" || eventType == "keepalive" {
		return false, nil
	}
	if failure := upstreamSSEErrorPayload(payload, eventType, protocol); failure != nil {
		return false, failure
	}
	switch protocol {
	case upstreamSSEResponses:
		switch eventType {
		case "response.completed", "response.done", "response.incomplete", "response.cancelled", "response.canceled":
			return true, nil
		case "response.output_item.added":
			switch gjson.GetBytes(payload, "item.type").String() {
			case "function_call", "custom_tool_call":
				return true, nil
			}
		}
		return openAIStreamDataStartsClientOutput(string(payload), eventType), nil
	case upstreamSSEMessages:
		switch eventType {
		case "ping", "message_start":
			return false, nil
		case "content_block_start":
			block := gjson.GetBytes(payload, "content_block")
			switch block.Get("type").String() {
			case "text":
				return block.Get("text").String() != "", nil
			case "thinking":
				return block.Get("thinking").String() != "" || block.Get("signature").String() != "", nil
			default:
				return true, nil
			}
		default:
			return true, nil
		}
	case upstreamSSEGemini:
		if sig, ok := detectGeminiResponseSignal(payload); ok && sig.Kind != geminiSignalNone {
			return true, nil
		}
		candidate := geminiPrimaryCandidate(payload)
		if candidate.Get("finishReason").String() != "" {
			return true, nil
		}
		for _, part := range candidate.Get("content.parts").Array() {
			if part.Get("text").String() != "" || part.Get("thoughtSignature").String() != "" || part.Get("functionCall").Exists() || part.Get("inlineData").Exists() || part.Get("fileData").Exists() {
				return true, nil
			}
		}
		if candidate.Exists() || gjson.GetBytes(payload, "candidates").Exists() || gjson.GetBytes(payload, "usageMetadata").Exists() {
			return false, nil
		}
		for key := range gjson.ParseBytes(payload).Map() {
			switch key {
			case "modelVersion", "responseId", "createTime":
			default:
				return true, nil
			}
		}
		return false, nil
	default:
		choices := gjson.GetBytes(payload, "choices")
		if !choices.IsArray() {
			return true, nil
		}
		for _, choice := range choices.Array() {
			if reason := choice.Get("finish_reason"); reason.Exists() && reason.Type != gjson.Null && reason.String() != "" {
				return true, nil
			}
			for key, value := range choice.Get("delta").Map() {
				if key == "role" || value.Type == gjson.Null || value.String() == "" || value.Raw == "[]" || value.Raw == "{}" {
					continue
				}
				return true, nil
			}
		}
		return false, nil
	}
}

func upstreamSSEPayloadHasUsage(payload []byte) bool {
	for _, path := range []string{"usage", "response.usage", "message.usage", "usageMetadata"} {
		if upstreamUsageHasPositiveValue(gjson.GetBytes(payload, path)) {
			return true
		}
	}
	return false
}

// Providers express billable usage in tokens, cache aliases, image counts,
// characters and durations. Any observed positive quantity is conservative
// evidence of acceptance, including nested provider-specific breakdowns.
func upstreamUsageHasPositiveValue(value gjson.Result) bool {
	if value.Type == gjson.Number || value.Type == gjson.String {
		return value.Float() > 0
	}
	if !value.IsObject() && !value.IsArray() {
		return false
	}
	found := false
	value.ForEach(func(_, child gjson.Result) bool {
		found = upstreamUsageHasPositiveValue(child)
		return !found
	})
	return found
}

func upstreamSSEErrorPayload(payload []byte, eventType string, protocol upstreamSSEProtocol) *upstreamSSEPreflightFailure {
	errorValue := gjson.GetBytes(payload, "error")
	if nested := gjson.GetBytes(payload, "response.error"); nested.Exists() && nested.Type != gjson.Null {
		errorValue = nested
	}
	isError := (errorValue.Exists() && errorValue.Type != gjson.Null && errorValue.Raw != "{}") || eventType == "error" || eventType == "response.failed"
	if !isError {
		return nil
	}
	status := 0
	for _, path := range []string{"response.error.status_code", "response.error.status", "response.error.code", "error.status_code", "error.status", "error.code", "status_code", "status", "code"} {
		value := gjson.GetBytes(payload, path)
		parsed, _ := strconv.Atoi(value.String())
		if parsed >= 400 && parsed <= 599 {
			status = parsed
			break
		}
	}
	message := strings.TrimSpace(ExtractUpstreamErrorMessage(payload))
	if status == 0 && isOpenAIContextWindowError(message, payload) {
		status = http.StatusBadRequest
	}
	if protocol == upstreamSSEGemini {
		if sig, ok := detectGeminiResponseSignal(payload); ok && sig.Kind == geminiSignalError {
			status, message = sig.Status, sig.Message
		}
	}
	if status == 0 {
		combined := strings.ToLower(errorValue.Get("type").String() + " " + errorValue.Get("code").String() + " " + gjson.GetBytes(payload, "code").String())
		switch {
		case strings.Contains(combined, "overloaded_error"):
			status = 529
		case strings.Contains(combined, "rate_limit"), strings.Contains(combined, "resource_exhausted"):
			status = http.StatusTooManyRequests
		case strings.Contains(combined, "invalid_request"), strings.Contains(combined, "context_length"):
			status = http.StatusBadRequest
		case strings.Contains(combined, "authentication"), strings.Contains(combined, "invalid_api_key"):
			status = http.StatusUnauthorized
		case strings.Contains(combined, "permission"), strings.Contains(combined, "forbidden"):
			status = http.StatusForbidden
		default:
			status = http.StatusBadGateway
		}
	}
	if message == "" {
		message = fmt.Sprintf("Upstream stream returned an error (%d)", status)
	}
	return newUpstreamSSEPreflightFailure(status, sanitizeUpstreamErrorMessage(message), payload)
}

func upstreamSSEIdleTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Gateway.StreamDataIntervalTimeout > 0 {
		return time.Duration(cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	return 0
}

// Run before installing any response headers or protocol-conversion writers.
// The existing relay retains usage and output after the first replay boundary.
func (s *GatewayService) preflightAnthropicUpstream(ctx context.Context, c *gin.Context, account *Account, resp *http.Response, model string) error {
	failure := inspectUpstreamSSEPrefix(ctx, resp, upstreamSSEMessages, upstreamSSEIdleTimeout(s.cfg))
	if failure == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if failure.terminal {
		return failure
	}
	failover := failure.failover(ctx, s.rateLimitService, account, resp.Header, model)
	if failover == nil {
		return nil
	}
	setOpsUpstreamError(c, failure.status, failure.message, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: failure.status, UpstreamRequestID: resp.Header.Get("x-request-id"), Kind: "stream_error", Message: failure.message})
	return failover
}

func inspectUpstreamJSONError(resp *http.Response, cfg *config.Config) *upstreamSSEPreflightFailure {
	if resp == nil || resp.Body == nil {
		return nil
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "json") && !strings.HasPrefix(contentType, "text/") {
		return nil
	}
	original := resp.Body
	limit := int64(upstreamSSEPreflightMaxBytes)
	if cfg != nil && cfg.Gateway.UpstreamResponseReadMaxBytes > 0 && cfg.Gateway.UpstreamResponseReadMaxBytes < limit {
		limit = cfg.Gateway.UpstreamResponseReadMaxBytes
	}
	data, err := io.ReadAll(io.LimitReader(original, limit+1))
	var readErr error
	if err != nil && !errors.Is(err, io.EOF) {
		readErr = err
	}
	resp.Body = &upstreamSSEReplayBody{Reader: io.MultiReader(bytes.NewReader(data), &upstreamPreflightReadError{err: readErr}, original), Closer: original}
	if err != nil || int64(len(data)) > limit || !gjson.ValidBytes(data) || upstreamSSEPayloadHasUsage(data) {
		return nil
	}
	return upstreamSSEErrorPayload(data, strings.TrimSpace(gjson.GetBytes(data, "type").String()), upstreamSSEChat)
}

func (s *GeminiMessagesCompatService) preflightGeminiUpstream(ctx context.Context, c *gin.Context, account *Account, resp *http.Response, model string) error {
	if isImageGenerationModel(model) {
		return nil
	}
	failure := inspectUpstreamSSEPrefix(ctx, resp, upstreamSSEGemini, upstreamSSEIdleTimeout(s.cfg))
	if failure == nil {
		return nil
	}
	if failure.terminal {
		return failure
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if isGeminiCachedContentNotFound(failure.status, failure.body) {
		return nil
	}
	if isGeminiBoundCacheUpstreamPassthrough(ctx, account, failure.status) {
		return &UpstreamFailoverError{StatusCode: failure.status, ResponseBody: failure.body, ResponseHeaders: resp.Header.Clone(), BoundUpstreamPassthrough: true}
	}
	failoverErr := failure.failover(ctx, s.rateLimitService, account, resp.Header, model)
	if failoverErr == nil {
		return nil
	}
	setOpsUpstreamError(c, failure.status, failure.message, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: failure.status, UpstreamRequestID: resp.Header.Get("x-request-id"), Kind: "stream_error", Message: failure.message})
	return failoverErr
}
