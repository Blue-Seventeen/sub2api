package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNewAPIStyleAudioForwardMapsOpenAIAndZhipuPaths(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		inboundPath string
		wantPath    string
		wantErr     error
	}{
		{
			name:        "openai v1 speech stays on openai path",
			platform:    PlatformOpenAI,
			inboundPath: "/v1/audio/speech",
			wantPath:    "/v1/audio/speech",
		},
		{
			name:        "openai root audio alias maps to v1 path",
			platform:    PlatformOpenAI,
			inboundPath: "/audio/transcriptions",
			wantPath:    "/v1/audio/transcriptions",
		},
		{
			name:        "zhipu unified transcription maps to glm official path",
			platform:    PlatformZhipu,
			inboundPath: "/v1/audio/transcriptions",
			wantPath:    zhipuAudioTranscriptionsPath,
		},
		{
			name:        "zhipu official speech alias stays on glm official path",
			platform:    PlatformZhipu,
			inboundPath: zhipuAudioSpeechPath,
			wantPath:    zhipuAudioSpeechPath,
		},
		{
			name:        "openai rejects glm official alias",
			platform:    PlatformOpenAI,
			inboundPath: zhipuAudioSpeechPath,
			wantErr:     ErrNewAPIStyleUnsupportedCapability,
		},
		{
			name:        "zhipu rejects unsupported audio subpath",
			platform:    PlatformZhipu,
			inboundPath: "/v1/audio/translations",
			wantErr:     ErrNewAPIStyleUnsupportedCapability,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"ok":true}`)}
			svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
			account := newAPIStyleAudioAccount(tt.platform, nil)

			_, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), account, NewAPIStyleForwardOptions{
				Route:       NewAPIStyleRouteAudio,
				RequestBody: []byte(`{"model":"audio-model","input":"hello","voice":"alloy","response_format":"wav"}`),
				InboundPath: tt.inboundPath,
				ContentType: "application/json",
				HeaderSource: http.Header{
					"Authorization": []string{"Bearer client-token"},
					"Accept":        []string{"audio/wav"},
				},
			})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Forward() error = %v, want %v", err, tt.wantErr)
				}
				if upstream.lastReq != nil {
					t.Fatalf("upstream request should not be sent for unsupported path")
				}
				return
			}
			if err != nil {
				t.Fatalf("Forward() error = %v", err)
			}
			if endpoint != tt.wantPath {
				t.Fatalf("upstream endpoint = %q, want %q", endpoint, tt.wantPath)
			}
			if upstream.lastReq == nil {
				t.Fatalf("upstream request was not sent")
			}
			if got := upstream.lastReq.URL.Path; got != tt.wantPath {
				t.Fatalf("upstream path = %q, want %q", got, tt.wantPath)
			}
			wantAuth := "Bearer account-token"
			if tt.platform == PlatformZhipu {
				wantAuth = "Bearer zhipu-token"
			}
			if got := upstream.lastReq.Header.Get("Authorization"); got != wantAuth {
				t.Fatalf("authorization = %q, want %q", got, wantAuth)
			}
			if got := upstream.lastReq.Header.Get("Accept"); got != "audio/wav" {
				t.Fatalf("accept = %q, want client Accept", got)
			}
		})
	}
}

func TestNewAPIStyleDeepSeekNewAPIChatUsesV1Path(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"id":"chatcmpl_test","model":"deepseek-chat","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":5}}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformDeepSeek, nil), NewAPIStyleForwardOptions{
		Route:        NewAPIStyleRouteChatCompletions,
		Method:       http.MethodPost,
		RequestBody:  []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`),
		InboundPath:  "/v1/chat/completions",
		ContentType:  "application/json",
		HeaderSource: http.Header{"Authorization": []string{"Bearer client-token"}},
	})

	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", endpoint)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
}

func TestNewAPIStyleZhipuNewAPIChatUsesV1Path(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"id":"chatcmpl_test","model":"glm-5.2","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":6}}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:        NewAPIStyleRouteChatCompletions,
		Method:       http.MethodPost,
		RequestBody:  []byte(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`),
		InboundPath:  "/v1/chat/completions",
		ContentType:  "application/json",
		HeaderSource: http.Header{"Authorization": []string{"Bearer client-token"}},
	})

	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", endpoint)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer zhipu-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 6, result.Usage.OutputTokens)
}

func TestNewAPIStyleZhipuOfficialChatKeepsPaaSPath(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"id":"chatcmpl_test","model":"glm-4.5","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":6}}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
	account := newAPIStyleAudioAccount(PlatformZhipu, nil)
	account.Credentials["base_url"] = "https://open.bigmodel.cn"

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), account, NewAPIStyleForwardOptions{
		Route:        NewAPIStyleRouteChatCompletions,
		Method:       http.MethodPost,
		RequestBody:  []byte(`{"model":"glm-4.5","messages":[{"role":"user","content":"hi"}]}`),
		InboundPath:  "/v1/chat/completions",
		ContentType:  "application/json",
		HeaderSource: http.Header{"Authorization": []string{"Bearer client-token"}},
	})

	require.NoError(t, err)
	require.Equal(t, zhipuCompatibleChatPath, endpoint)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, zhipuCompatibleChatPath, upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer zhipu-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 6, result.Usage.OutputTokens)
}

func TestZhipuOfficialCompatibleChatKeepsPaaSPath(t *testing.T) {
	preset := zhipuCompatibleProviderPreset()

	require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/chat/completions", preset.BuildChatURL("https://open.bigmodel.cn", "glm-4.5"))
	require.Equal(t, "https://zhipu-compatible.example/v1/chat/completions", preset.BuildChatURL("https://zhipu-compatible.example/", "glm-5.2"))
}

func TestNewAPIStyleStreamParsesSSEUsage(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("text/event-stream", strings.Join([]string{
		`data: {"id":"chatcmpl_test","choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"id":"chatcmpl_test","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":13,"prompt_tokens_details":{"cached_tokens":2}}}`,
		`data: [DONE]`,
		``,
	}, "\n\n"))}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformDeepSeek, nil), NewAPIStyleForwardOptions{
		Route:        NewAPIStyleRouteChatCompletions,
		Method:       http.MethodPost,
		Stream:       true,
		RequestBody:  []byte(`{"model":"deepseek-chat","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
		InboundPath:  "/v1/chat/completions",
		ContentType:  "application/json",
		HeaderSource: http.Header{"Authorization": []string{"Bearer client-token"}},
	})

	require.NoError(t, err)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 13, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.False(t, result.UsageEstimated)
}

func TestNewAPIStyleStreamParsesAnthropicSSEUsageAcrossEvents(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":17,"cache_read_input_tokens":3}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"output_tokens":19}}`,
		``,
	}, "\n")

	usage := parseNewAPIStyleSSEUsage([]byte(body))

	require.Equal(t, 17, usage.InputTokens)
	require.Equal(t, 3, usage.CacheReadInputTokens)
	require.Equal(t, 19, usage.OutputTokens)
}

func TestNewAPIStyleStreamParsesAnthropicSSEUsageDetails(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":30,"cached_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":7,"ephemeral_1h_input_tokens":11}}}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"output_tokens":40,"completion_tokens_details":{"image_tokens":12}}}`,
		``,
	}, "\n")

	usage := parseNewAPIStyleSSEUsage([]byte(body))

	require.Equal(t, 30, usage.InputTokens)
	require.Equal(t, 5, usage.CacheReadInputTokens)
	require.Equal(t, 18, usage.CacheCreationInputTokens)
	require.Equal(t, 7, usage.CacheCreation5mTokens)
	require.Equal(t, 11, usage.CacheCreation1hTokens)
	require.Equal(t, 40, usage.OutputTokens)
	require.Equal(t, 12, usage.ImageOutputTokens)
}

func TestNewAPIStyleUsageParsesOpenAIStyleDetailAliases(t *testing.T) {
	usage := parseNewAPIStyleUsage([]byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"cached_tokens":9,"cache_creation_tokens":4,"output_tokens_details":{"image_tokens":6}}}`))

	require.Equal(t, 87, usage.InputTokens)
	require.Equal(t, 9, usage.CacheReadInputTokens)
	require.Equal(t, 4, usage.CacheCreationInputTokens)
	require.Equal(t, 20, usage.OutputTokens)
	require.Equal(t, 6, usage.ImageOutputTokens)
}

func TestNewAPIStyleUsageExcludesResponsesCachedTokensFromInput(t *testing.T) {
	usage := parseNewAPIStyleUsage([]byte(`{"usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":10,"input_tokens_details":{"cached_tokens":25,"cache_creation_tokens":10}}}`))

	require.Equal(t, 65, usage.InputTokens)
	require.Equal(t, 25, usage.CacheReadInputTokens)
	require.Equal(t, 10, usage.CacheCreationInputTokens)
	require.Equal(t, 20, usage.OutputTokens)
}

func TestNewAPIStyleUsageGuardrailsPreserveCacheBreakdownForOpenAIForwardResult(t *testing.T) {
	svc := &NewAPIStyleGatewayService{}
	result := &ForwardResult{}

	svc.applyUsageGuardrailsWithParsedUsage(result, NewAPIStyleForwardOptions{Route: NewAPIStyleRouteChatCompletions}, nil, ClaudeUsage{
		InputTokens:              30,
		OutputTokens:             40,
		CacheCreationInputTokens: 18,
		CacheReadInputTokens:     5,
		CacheCreation5mTokens:    7,
		CacheCreation1hTokens:    11,
		ImageOutputTokens:        12,
	})

	require.Equal(t, 30, result.Usage.InputTokens)
	require.Equal(t, 18, result.Usage.CacheCreationInputTokens)
	require.Equal(t, 7, result.Usage.CacheCreation5mTokens)
	require.Equal(t, 11, result.Usage.CacheCreation1hTokens)

	openAIResult := OpenAIForwardResultFromForwardResult(result)
	require.NotNil(t, openAIResult)
	require.Equal(t, 53, openAIResult.Usage.InputTokens)
	require.Equal(t, 5, openAIResult.Usage.CacheReadInputTokens)
	require.Equal(t, 18, openAIResult.Usage.CacheCreationInputTokens)
	require.Equal(t, 7, openAIResult.Usage.CacheCreation5mTokens)
	require.Equal(t, 11, openAIResult.Usage.CacheCreation1hTokens)
	require.Equal(t, 12, openAIResult.Usage.ImageOutputTokens)
}

func TestNewAPIStyleUsageNestedExplicitZeroOverridesLegacyCacheAliases(t *testing.T) {
	usage := parseNewAPIStyleUsage([]byte(`{"usage":{"input_tokens":100,"cached_tokens":25,"cache_creation_tokens":10,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`))

	require.Equal(t, 100, usage.InputTokens)
	require.Zero(t, usage.CacheReadInputTokens)
	require.Zero(t, usage.CacheCreationInputTokens)
}

func TestNewAPIStyleUsageParsesCacheWriteAliases(t *testing.T) {
	usage := parseNewAPIStyleUsage([]byte(`{"usage":{"input_tokens":100,"output_tokens":20,"input_tokens_details":{"cached_tokens":25,"cache_write_tokens":10}}}`))

	require.Equal(t, 65, usage.InputTokens)
	require.Equal(t, 25, usage.CacheReadInputTokens)
	require.Equal(t, 10, usage.CacheCreationInputTokens)
}

func TestNewAPIStyleUsageTopLevelCacheWriteIsPartOfOpenAITotalInput(t *testing.T) {
	usage := parseNewAPIStyleUsage([]byte(`{"usage":{"input_tokens":100,"output_tokens":20,"cache_write_input_tokens":10}}`))

	require.Equal(t, 90, usage.InputTokens)
	require.Equal(t, 10, usage.CacheCreationInputTokens)
}

func TestNewAPIStyleMessagesUsageKeepsAnthropicUncachedInput(t *testing.T) {
	usage := parseNewAPIStyleUsageForRoute([]byte(`{"usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":10}}`), NewAPIStyleRouteMessages)

	require.Equal(t, 100, usage.InputTokens)
	require.Equal(t, 10, usage.CacheCreationInputTokens)
}

func TestNewAPIStyleSSEUsageCaptureWriterParsesSplitEvents(t *testing.T) {
	writer := &newAPIStyleSSEUsageCaptureWriter{}
	_, err := writer.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n"))
	require.NoError(t, err)
	_, err = writer.Write([]byte("\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n"))
	require.NoError(t, err)

	usage := writer.Usage()

	require.Equal(t, 7, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
}

func TestNewAPIStyleAudioForwardPreservesTTSJSONBodyWithoutMapping(t *testing.T) {
	body := []byte(`{"model":"glm-4-voice","input":"hello","voice":"tongtong","response_format":"wav","speed":1.1}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("audio/wav", "RIFFxxxxWAVEaudio")}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	_, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/speech",
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if got := upstream.lastReq.URL.Path; got != zhipuAudioSpeechPath {
		t.Fatalf("upstream path = %q, want %q", got, zhipuAudioSpeechPath)
	}
	if !bytes.Equal(upstream.lastBody, body) {
		t.Fatalf("forwarded body changed:\n got %s\nwant %s", string(upstream.lastBody), string(body))
	}
}

func TestNewAPIStyleAliQwenASRRejectsRawBase64BeforeUpstream(t *testing.T) {
	body := []byte(`{"model":"qwen3-asr-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==","format":"mp3"}}]}]}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"ok":true}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	_, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		RequestBody: body,
		InboundPath: "/compatible-mode/v1/chat/completions",
		ContentType: "application/json",
	})

	var clientErr *CompatibleClientError
	require.ErrorAs(t, err, &clientErr)
	require.Equal(t, http.StatusBadRequest, clientErr.StatusCode)
	require.Contains(t, clientErr.Message, "data:audio/mpeg;base64")
	require.Nil(t, upstream.lastReq, "invalid ASR input must not reach upstream")
}

func TestNewAPIStyleAliQwenASRAllowsDataURLAndAddsDashScopeHeader(t *testing.T) {
	body := []byte(`{"model":"qwen3-asr-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"data:audio/mpeg;base64,SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==","format":"mp3"}}]}]}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"id":"chatcmpl_test","object":"chat.completion","model":"qwen3-asr-flash","choices":[{"message":{"role":"assistant","content":"你好。"},"finish_reason":"stop","index":0}],"usage":{"prompt_tokens":18,"completion_tokens":9,"total_tokens":27}}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		RequestBody: body,
		InboundPath: "/compatible-mode/v1/chat/completions",
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "/compatible-mode/v1/chat/completions", endpoint)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "/compatible-mode/v1/chat/completions", upstream.lastReq.URL.Path)
	require.Equal(t, "enable", upstream.lastReq.Header.Get("X-DashScope-SSE"))
	require.Equal(t, "qwen3-asr-flash", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "data:audio/mpeg;base64,SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==", gjson.GetBytes(upstream.lastBody, "messages.0.content.0.input_audio.data").String())
	require.Equal(t, 18, result.Usage.InputTokens)
	require.Equal(t, 9, result.Usage.OutputTokens)
}

func TestNewAPIStyleAliQwenASRBillsUsageSecondsOnChatRoute(t *testing.T) {
	body := []byte(`{"model":"qwen3-asr-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"data:audio/mpeg;base64,SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==","format":"mp3"}}]}]}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{
		"id":"chatcmpl_test",
		"object":"chat.completion",
		"model":"qwen3-asr-flash",
		"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}],
		"usage":{"seconds":2.1,"prompt_tokens":18,"completion_tokens":9,"total_tokens":27}
	}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		RequestBody: body,
		InboundPath: "/compatible-mode/v1/chat/completions",
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Equal(t, 3, result.BillableDurationSeconds)
	require.Equal(t, BillableUnitTypeDuration, result.BillableUnitType)
	require.Zero(t, result.BillableCharacterCount)
}

func TestNewAPIStyleZhipuASRBillsFallbackAudioDuration(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr-2512", testWAVBytes(3))
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{
		"id":"asr-test",
		"created":123,
		"request_id":"asr-req",
		"model":"glm-asr-2512",
		"text":"ok"
	}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/transcriptions",
		ContentType: contentType,
	})

	require.NoError(t, err)
	require.Equal(t, zhipuAudioTranscriptionsPath, endpoint)
	require.Equal(t, zhipuAudioTranscriptionsPath, upstream.lastReq.URL.Path)
	require.NotNil(t, result)
	require.Equal(t, 3, result.BillableDurationSeconds)
	require.Equal(t, BillableUnitTypeDuration, result.BillableUnitType)

	groupID := int64(2)
	unitPrice := 0.04
	pricingSvc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:        PlatformZhipu,
		Models:          []string{"glm-asr-2512"},
		BillingMode:     BillingModeDuration,
		PerRequestPrice: &unitPrice,
	})
	cost := pricingSvc.calculateRecordUsageCost(context.Background(), result, &APIKey{GroupID: &groupID}, "glm-asr-2512", 1, 1, &recordUsageOpts{})

	require.NotNil(t, cost)
	require.Equal(t, string(BillingModeDuration), cost.BillingMode)
	require.InDelta(t, 0.12, cost.TotalCost, 1e-12)
}

func TestNewAPIStyleZhipuASRBillsResponseDurationSeconds(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr-2512", []byte("not-audio"))
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{
		"id":"asr-test",
		"created":123,
		"request_id":"asr-req",
		"model":"glm-asr-2512",
		"text":"ok",
		"usage":{"duration_seconds":2.1}
	}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/transcriptions",
		ContentType: contentType,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 3, result.BillableDurationSeconds)
	require.Equal(t, BillableUnitTypeDuration, result.BillableUnitType)
}

func TestNewAPIStyleASRUpstreamHTTPErrorReturnsNilResult(t *testing.T) {
	body := []byte(`{"model":"qwen3-asr-flash","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"data:audio/mpeg;base64,SUQzBAAAAAAAI1RTU0UAAAAPAAADTGF2ZjYwLjE2LjEwMA==","format":"mp3"}}]}]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"seconds":9},"error":{"message":"upstream failed"}}`)),
	}}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		RequestBody: body,
		InboundPath: "/compatible-mode/v1/chat/completions",
		ContentType: "application/json",
	})

	require.Error(t, err)
	require.Nil(t, result)
}

func TestNewAPIStyleZhipuASRUpstreamHTTPErrorReturnsNilResult(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr-2512", testWAVBytes(3))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"duration_seconds":9},"error":{"message":"upstream failed"}}`)),
	}}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/transcriptions",
		ContentType: contentType,
	})

	require.Error(t, err)
	require.Nil(t, result)
}

func TestNewAPIStyleZhipuASRHTTP200ErrorPayloadReturnsNilResult(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr-2512", testWAVBytes(3))
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"error":{"message":"business failed"}}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/transcriptions",
		ContentType: contentType,
	})

	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
}

func TestNewAPIStyleTTSUpstreamTransportErrorReturnsNilResult(t *testing.T) {
	body := []byte(`{"model":"qwen3-tts-flash","input":{"text":"hello","voice":"Cherry"}}`)
	upstream := &httpUpstreamRecorder{err: errors.New("dial tcp: connection refused")}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteQwenTTS,
		RequestBody: body,
		InboundPath: aliQwenTTSGenerationPath,
		ContentType: "application/json",
	})

	require.Error(t, err)
	require.Nil(t, result)
}

func TestNewAPIStyleAliQwenTTSOfficialRouteForwardsDashScopeRequest(t *testing.T) {
	body := []byte(`{"model":"qwen3-tts-flash","input":{"text":"hello","voice":"Cherry","language_type":"English"}}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("text/event-stream", `event:result
data:{"output":{"audio":{"data":"UklGRg==","id":"audio-test"},"finish_reason":"stop"},"usage":{"characters":5},"request_id":"req-test"}

`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteQwenTTS,
		RequestBody: body,
		InboundPath: aliQwenTTSGenerationPath,
		ContentType: "application/json",
		HeaderSource: http.Header{
			"Accept": []string{"text/event-stream"},
		},
	})

	require.NoError(t, err)
	require.Equal(t, aliQwenTTSGenerationPath, endpoint)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, aliQwenTTSGenerationPath, upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer account-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "enable", upstream.lastReq.Header.Get("X-DashScope-SSE"))
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.JSONEq(t, string(body), string(upstream.lastBody))
	require.NotNil(t, result)
	require.Equal(t, "qwen3-tts-flash", result.Model)
	require.Equal(t, 5, result.BillableCharacterCount)
	require.Equal(t, BillableUnitTypeCharacter, result.BillableUnitType)
}

func TestNewAPIStyleZhipuTTSBillsRequestCharacters(t *testing.T) {
	body := []byte(`{"model":"glm-tts","input":"\u4f60\u597dabc","voice":"tongtong","response_format":"wav"}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("audio/wav", "RIFFxxxxWAVEaudio")}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/speech",
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.Equal(t, zhipuAudioSpeechPath, endpoint)
	require.Equal(t, zhipuAudioSpeechPath, upstream.lastReq.URL.Path)
	require.NotNil(t, result)
	require.Equal(t, 5, result.BillableCharacterCount)
	require.Equal(t, BillableUnitTypeCharacter, result.BillableUnitType)

	groupID := int64(2)
	unitPrice := 10.0
	pricingSvc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:        PlatformZhipu,
		Models:          []string{"glm-tts"},
		BillingMode:     BillingModeCharacter,
		PerRequestPrice: &unitPrice,
	})
	cost := pricingSvc.calculateRecordUsageCost(context.Background(), result, &APIKey{GroupID: &groupID}, "glm-tts", 1, 1, &recordUsageOpts{})

	require.NotNil(t, cost)
	require.Equal(t, string(BillingModeCharacter), cost.BillingMode)
	require.InDelta(t, 0.05, cost.TotalCost, 1e-12)
}

func TestNewAPIStyleZhipuTTSPerRequestPricingStillUsesRequestCount(t *testing.T) {
	body := []byte(`{"model":"glm-tts","input":"hello","voice":"tongtong","response_format":"wav"}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("audio/wav", "RIFFxxxxWAVEaudio")}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/speech",
		ContentType: "application/json",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.RequestCount)
	require.Equal(t, 5, result.BillableCharacterCount)

	groupID := int64(2)
	perRequestPrice := 0.02
	pricingSvc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:        PlatformZhipu,
		Models:          []string{"glm-tts"},
		BillingMode:     BillingModePerRequest,
		PerRequestPrice: &perRequestPrice,
	})
	cost := pricingSvc.calculateRecordUsageCost(context.Background(), result, &APIKey{GroupID: &groupID}, "glm-tts", 1, 1, &recordUsageOpts{})

	require.NotNil(t, cost)
	require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
	require.InDelta(t, 0.02, cost.TotalCost, 1e-12)
}

func TestNewAPIStyleZhipuTTSHTTP200ErrorPayloadReturnsNilResult(t *testing.T) {
	body := []byte(`{"model":"glm-tts","input":"hello","voice":"tongtong","response_format":"wav"}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"error":"business failed"}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/speech",
		ContentType: "application/json",
	})

	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
}

func TestNewAPIStyleZhipuTTSHTTP200SSEErrorPayloadReturnsNilResult(t *testing.T) {
	body := []byte(`{"model":"glm-tts","input":"hello","voice":"tongtong","response_format":"wav"}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("text/event-stream", "event: error\ndata: {\"error\":{\"message\":\"stream failed\"}}\n\n")}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformZhipu, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/speech",
		ContentType: "application/json",
	})

	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
}

func TestNewAPIStyleAliQwenTTSOfficialRoutePreservesClientDashScopeSSEHeader(t *testing.T) {
	body := []byte(`{"model":"qwen3-tts-flash","input":{"text":"hello","voice":"Cherry","language_type":"English"}}`)
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"output":{"audio":{"url":"https://example.test/audio.wav"}}}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
	headers := make(http.Header)
	headers.Set("X-DashScope-SSE", "disable")

	_, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformAli, nil), NewAPIStyleForwardOptions{
		Route:        NewAPIStyleRouteQwenTTS,
		RequestBody:  body,
		InboundPath:  aliQwenTTSGenerationPath,
		ContentType:  "application/json",
		HeaderSource: headers,
	})

	require.NoError(t, err)
	require.Equal(t, "disable", upstream.lastReq.Header.Get("X-DashScope-SSE"))
}

func TestExtractNewAPIStyleModelReadsQwenTTSOfficialBody(t *testing.T) {
	body := []byte(`{"model":"qwen3-tts-flash","input":{"text":"hello","voice":"Cherry","language_type":"English"}}`)

	if got := ExtractNewAPIStyleModel(body, "application/json"); got != "qwen3-tts-flash" {
		t.Fatalf("ExtractNewAPIStyleModel() = %q, want qwen3-tts-flash", got)
	}
}

func TestNewAPIStyleAudioMultipartModelExtractionAndRewrite(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr", []byte("fake-audio-bytes"))
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"text":"ok"}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
	account := newAPIStyleAudioAccount(PlatformZhipu, map[string]any{"glm-asr": "glm-asr-upstream"})

	result, endpoint, err := svc.Forward(context.Background(), newAPIStyleTestContext(), account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteAudio,
		RequestBody: body,
		InboundPath: "/v1/audio/transcriptions",
		ContentType: contentType,
		HeaderSource: http.Header{
			"Authorization": []string{"Bearer client-token"},
		},
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if endpoint != zhipuAudioTranscriptionsPath {
		t.Fatalf("upstream endpoint = %q, want %q", endpoint, zhipuAudioTranscriptionsPath)
	}
	if result == nil || result.Model != "glm-asr-upstream" {
		t.Fatalf("result model = %#v, want mapped model", result)
	}
	if got := upstream.lastReq.Header.Get("Authorization"); got != "Bearer zhipu-token" {
		t.Fatalf("authorization = %q, want zhipu token", got)
	}
	upstreamContentType := upstream.lastReq.Header.Get("Content-Type")
	if !isMultipartFormData(upstreamContentType) {
		t.Fatalf("upstream content-type = %q, want multipart/form-data", upstreamContentType)
	}

	model, fileBytes := readNewAPIStyleAudioMultipart(t, upstream.lastBody, upstreamContentType)
	if model != "glm-asr-upstream" {
		t.Fatalf("multipart model = %q, want mapped model", model)
	}
	if string(fileBytes) != "fake-audio-bytes" {
		t.Fatalf("multipart file bytes = %q, want original file bytes", string(fileBytes))
	}
}

func TestNewAPIStyleAudioMultipartChannelMappedModelRewrite(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "public-asr", []byte("fake-audio-bytes"))
	upstream := &httpUpstreamRecorder{resp: newAPIStyleAudioResponse("application/json", `{"text":"ok"}`)}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}
	account := newAPIStyleAudioAccount(PlatformZhipu, nil)

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), account, NewAPIStyleForwardOptions{
		Route:              NewAPIStyleRouteAudio,
		RequestBody:        body,
		Model:              "public-asr",
		ChannelMappedModel: "glm-asr-upstream",
		InboundPath:        "/v1/audio/transcriptions",
		ContentType:        contentType,
		HeaderSource: http.Header{
			"Authorization": []string{"Bearer client-token"},
		},
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if result == nil || result.Model != "glm-asr-upstream" {
		t.Fatalf("result model = %#v, want channel mapped model", result)
	}
	model, fileBytes := readNewAPIStyleAudioMultipart(t, upstream.lastBody, upstream.lastReq.Header.Get("Content-Type"))
	if model != "glm-asr-upstream" {
		t.Fatalf("multipart model = %q, want channel mapped model", model)
	}
	if string(fileBytes) != "fake-audio-bytes" {
		t.Fatalf("multipart file bytes = %q, want original file bytes", string(fileBytes))
	}
}

func TestExtractNewAPIStyleModelReadsMultipartFormField(t *testing.T) {
	body, contentType := buildNewAPIStyleAudioMultipart(t, "glm-asr", []byte("audio"))

	if got := ExtractNewAPIStyleModel(body, contentType); got != "glm-asr" {
		t.Fatalf("ExtractNewAPIStyleModel() = %q, want glm-asr", got)
	}
}

func TestExtractNewAPIStyleModelReadsModelNameFallback(t *testing.T) {
	body := []byte(`{"model_name":"kling-v1"}`)

	if got := ExtractNewAPIStyleModel(body, "application/json"); got != "kling-v1" {
		t.Fatalf("ExtractNewAPIStyleModel() = %q, want kling-v1", got)
	}
}

func TestNewAPIStyleAudioTokenChannelPricingWinsOverRequestGuardrail(t *testing.T) {
	groupID := int64(2)
	inputPrice := 0.000018
	svc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:    PlatformZhipu,
		Models:      []string{"glm-asr-2512"},
		BillingMode: BillingModeToken,
		InputPrice:  &inputPrice,
	})

	cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
		Model:            "glm-asr-2512",
		Usage:            ClaudeUsage{InputTokens: 119, OutputTokens: 11},
		RequestCount:     1,
		BillableUnitType: BillableUnitTypeRequest,
	}, &APIKey{GroupID: &groupID}, "glm-asr-2512", 1, 1, &recordUsageOpts{})

	if cost == nil {
		t.Fatalf("cost is nil")
		return
	}
	if cost != nil && cost.BillingMode != string(BillingModeToken) {
		t.Fatalf("billing mode = %q, want token", cost.BillingMode)
	}
	want := float64(119) * inputPrice
	if cost != nil && math.Abs(cost.TotalCost-want) > 1e-12 {
		t.Fatalf("total cost = %.12f, want %.12f", cost.TotalCost, want)
	}
}

func TestNewAPIStyleAudioNoChannelPricingFallsBackToTokenUsage(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	svc := &GatewayService{
		billingService: billing,
		resolver:       NewModelPricingResolver(nil, billing),
	}
	tokens := ClaudeUsage{InputTokens: 1000, OutputTokens: 500}

	cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
		Model:            "claude-sonnet-4",
		Usage:            tokens,
		RequestCount:     1,
		BillableUnitType: BillableUnitTypeRequest,
	}, &APIKey{}, "claude-sonnet-4", 1, 1, &recordUsageOpts{})

	if cost == nil {
		t.Fatalf("cost is nil")
	}
	expected, err := billing.CalculateCost("claude-sonnet-4", UsageTokens{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens}, 1)
	if err != nil {
		t.Fatalf("expected cost: %v", err)
	}
	if cost != nil && cost.TotalCost <= 0 {
		t.Fatalf("total cost = %.12f, want positive token fallback", cost.TotalCost)
	}
	if cost != nil && math.Abs(cost.TotalCost-expected.TotalCost) > 1e-12 {
		t.Fatalf("total cost = %.12f, want %.12f", cost.TotalCost, expected.TotalCost)
	}
}

func TestNewAPIStyleAudioPerRequestChannelPricingStillUsesRequestCount(t *testing.T) {
	groupID := int64(2)
	perRequestPrice := 0.02
	svc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:        PlatformZhipu,
		Models:          []string{"glm-tts"},
		BillingMode:     BillingModePerRequest,
		PerRequestPrice: &perRequestPrice,
	})

	cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
		Model:            "glm-tts",
		Usage:            ClaudeUsage{InputTokens: 32},
		RequestCount:     1,
		BillableUnitType: BillableUnitTypeRequest,
	}, &APIKey{GroupID: &groupID}, "glm-tts", 1, 1, &recordUsageOpts{})

	if cost == nil {
		t.Fatalf("cost is nil")
	}
	if cost != nil && cost.BillingMode != string(BillingModePerRequest) {
		t.Fatalf("billing mode = %q, want per_request", cost.BillingMode)
	}
	if cost != nil && math.Abs(cost.TotalCost-perRequestPrice) > 1e-12 {
		t.Fatalf("total cost = %.12f, want %.12f", cost.TotalCost, perRequestPrice)
	}
}

func TestNewAPIStyleAudioTokenChannelPricingWithoutUsageStaysZero(t *testing.T) {
	groupID := int64(2)
	inputPrice := 0.000018
	svc := newAudioPricingGatewayService(t, groupID, PlatformZhipu, ChannelModelPricing{
		Platform:    PlatformZhipu,
		Models:      []string{"glm-asr-2512", "glm-tts"},
		BillingMode: BillingModeToken,
		InputPrice:  &inputPrice,
	})

	for _, model := range []string{"glm-asr-2512", "glm-tts"} {
		model := model
		t.Run(model, func(t *testing.T) {
			cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
				Model:            model,
				RequestCount:     1,
				BillableUnitType: BillableUnitTypeRequest,
			}, &APIKey{GroupID: &groupID}, model, 1, 1, &recordUsageOpts{})

			if cost == nil {
				t.Fatalf("cost is nil")
			}
			if cost != nil && cost.BillingMode != string(BillingModeToken) {
				t.Fatalf("billing mode = %q, want token", cost.BillingMode)
			}
			if cost != nil && cost.TotalCost != 0 {
				t.Fatalf("total cost = %.12f, want 0", cost.TotalCost)
			}
			if cost != nil && cost.ActualCost != 0 {
				t.Fatalf("actual cost = %.12f, want 0", cost.ActualCost)
			}
		})
	}
}

func newAPIStyleAudioAccount(platform string, modelMapping map[string]any) *Account {
	credentials := map[string]any{
		"api_key":  "account-token",
		"base_url": "http://upstream.example",
	}
	if modelMapping != nil {
		credentials["model_mapping"] = modelMapping
	}
	if platform == PlatformZhipu {
		credentials["token"] = "zhipu-token"
	}
	return &Account{
		ID:          7,
		Name:        "audio-account",
		Platform:    platform,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: credentials,
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}
}

func newAudioPricingGatewayService(t *testing.T, groupID int64, platform string, pricing ChannelModelPricing) *GatewayService {
	t.Helper()
	cfg := &config.Config{}
	billing := NewBillingService(cfg, nil)
	channelSvc := &ChannelService{}
	channel := Channel{
		ID:                 42,
		Status:             StatusActive,
		GroupIDs:           []int64{groupID},
		BillingModelSource: BillingModelSourceRequested,
		ModelPricing:       []ChannelModelPricing{pricing},
	}
	channelSvc.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{groupID: platform}))
	return &GatewayService{
		billingService: billing,
		channelService: channelSvc,
		resolver:       NewModelPricingResolver(channelSvc, billing),
	}
}

func newAPIStyleAudioResponse(contentType string, body string) *http.Response {
	header := make(http.Header)
	if strings.TrimSpace(contentType) != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func buildNewAPIStyleAudioMultipart(t *testing.T, model string, fileBytes []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", model); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	fileWriter, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("create file field: %v", err)
	}
	if _, err := fileWriter.Write(fileBytes); err != nil {
		t.Fatalf("write file field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func readNewAPIStyleAudioMultipart(t *testing.T, body []byte, contentType string) (string, []byte) {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse content-type: %v", err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var model string
	var fileBytes []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read multipart part: %v", err)
		}
		data, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			t.Fatalf("read part body: %v", err)
		}
		switch part.FormName() {
		case "model":
			model = strings.TrimSpace(string(data))
		case "file":
			fileBytes = data
		}
	}
	return model, fileBytes
}
