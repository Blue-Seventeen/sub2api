//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIForwardResultFromNewAPIStylePreservesBillingAndResponseMetadata(t *testing.T) {
	reasoningEffort := "high"
	serviceTier := "priority"
	responseServiceTier := "flex"
	firstTokenMs := 17
	forwardedAt := time.Second
	headers := http.Header{"X-Request-Id": {"newapi-rid"}, "X-Usage-Meta": {"observed"}}
	audioUsage := &AudioUsage{Mode: "tts", DurationOrUnits: 3.5}
	forwarded := &ForwardResult{
		RequestID:                     "newapi-rid",
		UpstreamHeaders:               headers,
		Usage:                         ClaudeUsage{InputTokens: 10, OutputTokens: 4, CacheReadInputTokens: 3, CacheCreationInputTokens: 2, ImageOutputTokens: 1},
		Model:                         "requested-model",
		UpstreamModel:                 "mapped-model",
		UpstreamResponseModel:         "observed-model",
		UpstreamResponseModelConflict: true,
		UpstreamResponseServiceTier:   "flex",
		RequestedReasoningEffort:      &reasoningEffort,
		ServiceTier:                   &serviceTier,
		SearchCount:                   2,
		AudioUsage:                    audioUsage,
		BillingModel:                  "billing-model",
		Stream:                        true,
		Duration:                      forwardedAt,
		FirstTokenMs:                  &firstTokenMs,
		ClientDisconnect:              true,
		SkipUsageBilling:              true,
		ReasoningEffort:               &reasoningEffort,
		ImageCount:                    2,
		ImageSize:                     "2K",
		ImageInputSize:                "1536x1024",
		ImageOutputSize:               "1024x1024",
		ImageOutputSizes:              []string{"1024x1024", "1536x1024"},
		ImageSizeSource:               "upstream",
		ImageSizeBreakdown:            map[string]int{"1024x1024": 1, "1536x1024": 1},
		RequestCount:                  3,
		TaskCount:                     4,
		BillableDurationSeconds:       5,
		BillableCharacterCount:        6,
		UsageEstimated:                true,
		BillableUnitType:              BillableUnitTypeImage,
	}

	got := OpenAIForwardResultFromForwardResult(forwarded)
	require.NotNil(t, got)
	require.Equal(t, forwarded.RequestID, got.RequestID)
	require.Equal(t, headers, got.UpstreamHeaders)
	require.Equal(t, headers, got.ResponseHeaders)
	require.Equal(t, 15, got.Usage.InputTokens)
	require.Equal(t, 4, got.Usage.OutputTokens)
	require.Equal(t, 3, got.Usage.CacheReadInputTokens)
	require.Equal(t, 2, got.Usage.CacheCreationInputTokens)
	require.Equal(t, 1, got.Usage.ImageOutputTokens)
	require.Equal(t, forwarded.Model, got.Model)
	require.Equal(t, forwarded.UpstreamModel, got.UpstreamModel)
	require.Equal(t, forwarded.UpstreamResponseModel, got.UpstreamResponseModel)
	require.Equal(t, forwarded.UpstreamResponseModelConflict, got.UpstreamResponseModelConflict)
	require.Equal(t, responseServiceTier, got.UpstreamResponseServiceTier)
	require.Equal(t, &reasoningEffort, got.RequestedReasoningEffort)
	require.Equal(t, &serviceTier, got.ServiceTier)
	require.Equal(t, forwarded.SearchCount, got.SearchCount)
	require.Equal(t, audioUsage, got.AudioUsage)
	require.Equal(t, forwarded.BillingModel, got.BillingModel)
	require.Equal(t, forwarded.Stream, got.Stream)
	require.Equal(t, forwarded.Duration, got.Duration)
	require.Equal(t, forwarded.FirstTokenMs, got.FirstTokenMs)
	require.Equal(t, forwarded.ClientDisconnect, got.ClientDisconnect)
	require.Equal(t, forwarded.SkipUsageBilling, got.SkipUsageBilling)
	require.Equal(t, forwarded.ReasoningEffort, got.ReasoningEffort)
	require.Equal(t, forwarded.ImageCount, got.ImageCount)
	require.Equal(t, forwarded.ImageSize, got.ImageSize)
	require.Equal(t, forwarded.ImageInputSize, got.ImageInputSize)
	require.Equal(t, forwarded.ImageOutputSize, got.ImageOutputSize)
	require.Equal(t, forwarded.ImageOutputSizes, got.ImageOutputSizes)
	require.Equal(t, forwarded.ImageSizeSource, got.ImageSizeSource)
	require.Equal(t, forwarded.ImageSizeBreakdown, got.ImageSizeBreakdown)
	require.Equal(t, forwarded.RequestCount, got.RequestCount)
	require.Equal(t, forwarded.TaskCount, got.TaskCount)
	require.Equal(t, forwarded.BillableDurationSeconds, got.BillableDurationSeconds)
	require.Equal(t, forwarded.BillableCharacterCount, got.BillableCharacterCount)
	require.Equal(t, forwarded.UsageEstimated, got.UsageEstimated)
	require.Equal(t, forwarded.BillableUnitType, got.BillableUnitType)
}

func TestOpenAIGatewayService_CodexDefaultInstructionsUseMappedUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestBody := []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(requestBody)))
	c.Request.Header.Set("Content-Type", "application/json")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"mapped-codex-rid"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_mapped\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 1902, Name: "codex-model-mapping", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
			"model_mapping":      map[string]any{"gpt-5.5": "gpt-5.1-codex-max"},
		},
		Extra: map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeOff},
	}

	result, err := svc.Forward(context.Background(), c, account, requestBody)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "gpt-5.3-codex", gjson.GetBytes(upstream.lastBody, "model").String())
	wantInstructions := strings.TrimSpace(defaultCodexSynthInstructions("gpt-5.3-codex"))
	gotInstructions := strings.TrimSpace(gjson.GetBytes(upstream.lastBody, "instructions").String())
	require.True(t, gotInstructions == wantInstructions, "default instructions must be selected using the final mapped upstream model")
}
