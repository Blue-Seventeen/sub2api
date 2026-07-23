package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestCompatibleGatewayServicePrepareRequest_RewritesMappedModelForChat(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformZhipu,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"gpt-5.4": "glm-4.6v",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteChatCompletions, []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamModel != "glm-4.6v" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "glm-4.6v")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "glm-4.6v" {
		t.Fatalf("patched request model = %q, want %q", got, "glm-4.6v")
	}
}

func TestCompatibleOpenAIUsageMappersExcludeCacheReadAndCreationFromInput(t *testing.T) {
	responsesUsage := responsesUsageToClaudeUsage(&apicompat.ResponsesUsage{
		InputTokens:              100,
		OutputTokens:             20,
		CacheCreationInputTokens: 10,
		InputTokensDetails:       &apicompat.ResponsesInputTokensDetails{CachedTokens: 25},
	})
	if responsesUsage.InputTokens != 65 || responsesUsage.CacheReadInputTokens != 25 || responsesUsage.CacheCreationInputTokens != 10 {
		t.Fatalf("responses usage = %+v, want input=65 cache_read=25 cache_creation=10", responsesUsage)
	}

	chatUsage := chatUsageToClaudeUsage(&apicompat.ChatUsage{
		PromptTokens:        80,
		CompletionTokens:    10,
		PromptTokensDetails: &apicompat.ChatTokenDetails{CachedTokens: 30, CacheCreationTokens: 5},
	})
	if chatUsage.InputTokens != 45 || chatUsage.CacheReadInputTokens != 30 || chatUsage.CacheCreationInputTokens != 5 {
		t.Fatalf("chat usage = %+v, want input=45 cache_read=30 cache_creation=5", chatUsage)
	}

	legacyUsage := openAIUsageToClaudeUsage(OpenAIUsage{InputTokens: 60, CacheReadInputTokens: 15, CacheCreationInputTokens: 5})
	if legacyUsage.InputTokens != 40 || legacyUsage.CacheReadInputTokens != 15 || legacyUsage.CacheCreationInputTokens != 5 {
		t.Fatalf("legacy usage = %+v, want input=40 cache_read=15 cache_creation=5", legacyUsage)
	}
}

func TestCompatibleChatUsageCanonicalCacheWriteZeroOverridesLegacyAlias(t *testing.T) {
	var chunk apicompat.ChatCompletionsChunk
	if err := json.Unmarshal([]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cache_write_tokens":0,"cache_creation_tokens":19}}}`), &chunk); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	usage := chatUsageToClaudeUsage(chunk.Usage)
	if usage.InputTokens != 100 || usage.CacheCreationInputTokens != 0 {
		t.Fatalf("usage = %+v, want input=100 cache_creation=0", usage)
	}
}

func TestCompatibleGatewayServicePrepareRequest_RewritesMappedModelForNativeResponses(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformAli,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"gpt-5.4": "qwen-max",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteResponses, []byte(`{"model":"gpt-5.4","input":"hi"}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamModel != "qwen-max" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "qwen-max")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "qwen-max" {
		t.Fatalf("patched request model = %q, want %q", got, "qwen-max")
	}
}

func TestCompatibleGatewayServicePrepareRequest_RewritesMappedModelForNativeMessages(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformMoonshot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"claude-sonnet-4": "kimi-k2.5",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteMessages, []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamModel != "kimi-k2.5" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "kimi-k2.5")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "kimi-k2.5" {
		t.Fatalf("patched request model = %q, want %q", got, "kimi-k2.5")
	}
}

func TestCompatibleGatewayServicePrepareRequest_AliQwenASRRejectsRawBase64InputAudio(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform:    PlatformAli,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
	}
	for _, data := range []string{
		"SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==",
		"SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA__",
	} {
		body := []byte(`{"model":"qwen3-asr-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"` + data + `","format":"mp3"}}]}]}`)

		_, err := svc.prepareRequest(account, CompatibleRouteChatCompletions, body)
		if err == nil {
			t.Fatal("prepareRequest() error = nil, want raw base64 rejection")
		}
		var clientErr *CompatibleClientError
		if !errors.As(err, &clientErr) {
			t.Fatalf("error type = %T, want *CompatibleClientError", err)
		}
		if clientErr.StatusCode != http.StatusBadRequest || clientErr.ErrorType != "invalid_request_error" {
			t.Fatalf("client error = %#v", clientErr)
		}
		if !strings.Contains(clientErr.Message, "data:audio/mpeg;base64") {
			t.Fatalf("client error message = %q", clientErr.Message)
		}
	}
}

func TestCompatibleGatewayServicePrepareRequest_AliQwenASRAllowsURLAndDataURLInputAudio(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform:    PlatformAli,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
	}
	for _, data := range []string{
		"https://example.com/audio.mp3",
		"data:audio/mpeg;base64,SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==",
	} {
		body := []byte(`{"model":"qwen3-asr-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"` + data + `","format":"mp3"}}]}]}`)
		if _, err := svc.prepareRequest(account, CompatibleRouteChatCompletions, body); err != nil {
			t.Fatalf("prepareRequest(%q) error = %v", data, err)
		}
	}
}

func TestCompatibleGatewayServicePrepareRequest_UsesNativeZhipuMessages(t *testing.T) {
	svc := &CompatibleGatewayService{}
	account := &Account{
		Platform: PlatformZhipu,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
			"model_mapping": map[string]any{
				"claude-sonnet-4-20250514": "glm-4.6v",
			},
		},
	}

	prepared, err := svc.prepareRequest(account, CompatibleRouteMessages, []byte(`{
		"model":"claude-sonnet-4-20250514",
		"max_tokens":64,
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]
	}`))
	if err != nil {
		t.Fatalf("prepareRequest() error = %v", err)
	}
	if prepared.UpstreamKind != compatibleUpstreamMessages {
		t.Fatalf("UpstreamKind = %q, want %q", prepared.UpstreamKind, compatibleUpstreamMessages)
	}
	if prepared.UpstreamEndpoint != "/v1/messages" {
		t.Fatalf("UpstreamEndpoint = %q, want %q", prepared.UpstreamEndpoint, "/v1/messages")
	}
	if prepared.UpstreamModel != "glm-4.6v" {
		t.Fatalf("UpstreamModel = %q, want %q", prepared.UpstreamModel, "glm-4.6v")
	}
	if got := gjson.GetBytes(prepared.RequestBody, "model").String(); got != "glm-4.6v" {
		t.Fatalf("patched request model = %q, want %q", got, "glm-4.6v")
	}
	if got := svc.buildURLForPreparedRequest(account, prepared, "https://open.bigmodel.cn"); got != "https://open.bigmodel.cn/api/anthropic/v1/messages" {
		t.Fatalf("buildURLForPreparedRequest() = %q, want %q", got, "https://open.bigmodel.cn/api/anthropic/v1/messages")
	}
}

func TestCompatibleGatewayServiceHandleMessagesResponse_TracksDurationAndFirstToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"x-request-id": []string{"req-messages-stream"},
		},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12}}}\n" +
				"\n" +
				"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n" +
				"\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":34}}\n" +
				"\n",
		)),
	}

	svc := &CompatibleGatewayService{gatewayService: &GatewayService{}}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "claude-sonnet-4",
		UpstreamModel: "kimi-k2.5",
		ClientStream:  true,
	}

	startTime := time.Now().Add(-25 * time.Millisecond)
	result := svc.handleMessagesResponse(resp, c, prepared, startTime)

	if result.Duration <= 0 {
		t.Fatalf("Duration = %v, want > 0", result.Duration)
	}
	if result.FirstTokenMs == nil || *result.FirstTokenMs <= 0 {
		t.Fatalf("FirstTokenMs = %v, want > 0", result.FirstTokenMs)
	}
	if result.Usage.InputTokens != 12 {
		t.Fatalf("InputTokens = %d, want 12", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 34 {
		t.Fatalf("OutputTokens = %d, want 34", result.Usage.OutputTokens)
	}
}

func TestCompatibleGatewayServiceHandleChatPassthrough_NonStreamTracksDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"x-request-id": []string{"req-chat-nonstream"},
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":21,"completion_tokens":8}}`)),
	}

	svc := &CompatibleGatewayService{}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "claude-sonnet-4",
		UpstreamModel: "glm-4.5",
		ClientStream:  false,
	}

	startTime := time.Now().Add(-30 * time.Millisecond)
	result := svc.handleChatPassthrough(resp, c, prepared, startTime)

	if result.Duration <= 0 {
		t.Fatalf("Duration = %v, want > 0", result.Duration)
	}
	if result.FirstTokenMs != nil {
		t.Fatalf("FirstTokenMs = %v, want nil for non-stream", result.FirstTokenMs)
	}
	if result.Usage.InputTokens != 21 {
		t.Fatalf("InputTokens = %d, want 21", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 8 {
		t.Fatalf("OutputTokens = %d, want 8", result.Usage.OutputTokens)
	}
}

func TestCompatibleGatewayServiceHandleChatAsMessages_NonStreamPreservesCacheCreation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl_cache",
			"object":"chat.completion",
			"model":"gpt-5.4",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":25,"cache_write_tokens":10}}
		}`)),
	}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "claude-sonnet-4",
		UpstreamModel: "gpt-5.4",
		ClientStream:  false,
	}

	result := (&CompatibleGatewayService{}).handleChatAsMessages(resp, c, prepared, time.Now().Add(-time.Millisecond))
	if result.Usage.InputTokens != 65 || result.Usage.CacheReadInputTokens != 25 || result.Usage.CacheCreationInputTokens != 10 {
		t.Fatalf("usage = %+v, want input=65 cache_read=25 cache_creation=10", result.Usage)
	}
}

func TestCompatibleGatewayServiceHandleChatAsMessages_CanonicalZeroOverridesLegacyCacheCreation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl_cache_zero",
			"object":"chat.completion",
			"model":"gpt-5.4",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cache_write_tokens":0,"cache_creation_tokens":19}}
		}`)),
	}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "claude-sonnet-4",
		UpstreamModel: "gpt-5.4",
		ClientStream:  false,
	}

	result := (&CompatibleGatewayService{}).handleChatAsMessages(resp, c, prepared, time.Now().Add(-time.Millisecond))
	if result.Usage.InputTokens != 100 || result.Usage.CacheCreationInputTokens != 0 {
		t.Fatalf("usage = %+v, want input=100 cache_creation=0", result.Usage)
	}
}

func TestCompatibleGatewayServiceHandleChatPassthrough_ASRDurationBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"x-request-id": []string{"req-asr-duration"},
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl_test",
			"object":"chat.completion",
			"model":"qwen3-asr-flash",
			"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}],
			"usage":{"seconds":2.1,"prompt_tokens":18,"completion_tokens":9,"total_tokens":27}
		}`)),
	}

	svc := &CompatibleGatewayService{}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "qwen3-asr-flash",
		UpstreamModel: "qwen3-asr-flash",
		ClientStream:  false,
		ClientRoute:   CompatibleRouteChatCompletions,
		RequestBody:   []byte(`{"model":"qwen3-asr-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"data:audio/mpeg;base64,SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==","format":"mp3"}}]}]}`),
	}

	result := svc.handleChatPassthrough(resp, c, prepared, time.Now().Add(-30*time.Millisecond))

	if result.BillableDurationSeconds != 3 {
		t.Fatalf("BillableDurationSeconds = %d, want 3", result.BillableDurationSeconds)
	}
	if result.BillableUnitType != BillableUnitTypeDuration {
		t.Fatalf("BillableUnitType = %q, want %q", result.BillableUnitType, BillableUnitTypeDuration)
	}
	if result.Usage.InputTokens != 18 || result.Usage.OutputTokens != 9 {
		t.Fatalf("usage = %+v, want input=18 output=9", result.Usage)
	}
}

func TestCompatibleGatewayService_NonStreamTooLargeReturnsBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.Gateway.UpstreamResponseReadMaxBytes = 3
	prepared := &compatiblePreparedRequest{
		OriginalModel: "claude-sonnet-4",
		UpstreamModel: "glm-4.5",
		ClientStream:  false,
	}

	tests := []struct {
		name         string
		handler      func(*CompatibleGatewayService, *http.Response, *gin.Context, *compatiblePreparedRequest, time.Time) *ForwardResult
		wantFragment string
	}{
		{
			name: "messages",
			handler: func(svc *CompatibleGatewayService, resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, start time.Time) *ForwardResult {
				return svc.handleMessagesResponse(resp, c, prepared, start)
			},
			wantFragment: "Upstream response too large",
		},
		{
			name: "responses",
			handler: func(svc *CompatibleGatewayService, resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, start time.Time) *ForwardResult {
				return svc.handleResponsesResponse(resp, c, prepared, start)
			},
			wantFragment: "Upstream response too large",
		},
		{
			name: "chat_passthrough",
			handler: func(svc *CompatibleGatewayService, resp *http.Response, c *gin.Context, prepared *compatiblePreparedRequest, start time.Time) *ForwardResult {
				return svc.handleChatPassthrough(resp, c, prepared, start)
			},
			wantFragment: "Upstream response too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			resp := newCompatibleGatewayHTTPResponse(http.StatusOK, "toolong")
			svc := &CompatibleGatewayService{cfg: cfg, gatewayService: &GatewayService{}}

			result := tt.handler(svc, resp, c, prepared, time.Now().Add(-time.Millisecond))

			if result == nil {
				t.Fatal("result is nil")
			}
			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tt.wantFragment) {
				t.Fatalf("body = %q, want fragment %q", recorder.Body.String(), tt.wantFragment)
			}
		})
	}
}

func TestCompatibleGatewayStreamWithoutTerminalSkipsBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &CompatibleGatewayService{}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "glm-4.5-air",
		UpstreamModel: "glm-4.5-air",
		ClientStream:  true,
		ClientRoute:   CompatibleRouteChatCompletions,
		RequestBody:   []byte(`{"model":"glm-4.5-air","stream":true}`),
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n",
		)),
	}

	result := svc.handleChatPassthrough(resp, c, prepared, time.Now())
	if result == nil || !result.SkipUsageBilling {
		t.Fatalf("SkipUsageBilling = false, want true when stream lacks terminal event")
	}
}

func TestCompatibleGatewayStreamWithDoneKeepsBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &CompatibleGatewayService{}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "glm-4.5-air",
		UpstreamModel: "glm-4.5-air",
		ClientStream:  true,
		ClientRoute:   CompatibleRouteChatCompletions,
		RequestBody:   []byte(`{"model":"glm-4.5-air","stream":true}`),
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n",
		)),
	}

	result := svc.handleChatPassthrough(resp, c, prepared, time.Now())
	if result == nil || result.SkipUsageBilling {
		t.Fatalf("SkipUsageBilling = true, want false when stream has terminal event")
	}
}
