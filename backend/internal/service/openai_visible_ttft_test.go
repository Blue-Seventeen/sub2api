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
)

func TestOpenAIVisibleOutputClassification(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		eventType string
		want      bool
	}{
		{name: "keepalive", data: `{"type":"keepalive"}`, want: false},
		{name: "created", data: `{"type":"response.created"}`, want: false},
		{name: "empty output item", data: `{"type":"response.output_item.added","item":{"id":"item_test","type":"reasoning","summary":[]}}`, want: false},
		{name: "empty delta", data: `{"type":"response.output_text.delta","delta":""}`, want: false},
		{name: "text delta", data: `{"type":"response.output_text.delta","delta":"test output"}`, want: true},
		{name: "tool arguments", data: `{"type":"response.function_call_arguments.delta","delta":"{}"}`, want: true},
		{name: "partial image", data: `{"type":"response.image_generation_call.partial_image","partial_image_b64":"dGVzdA=="}`, want: true},
		{name: "completed image item", data: `{"type":"response.output_item.done","item":{"id":"item_test","type":"image_generation_call","result":"dGVzdA=="}}`, want: true},
		{name: "empty completed", data: `{"type":"response.completed","response":{"id":"resp_test","output":[]}}`, want: false},
		{name: "completed with output usage only", data: `{"type":"response.completed","response":{"id":"resp_test","usage":{"input_tokens":1,"output_tokens":2}}}`, want: false},
		{name: "completed with text", data: `{"type":"response.completed","response":{"id":"resp_test","output":[{"type":"message","content":[{"type":"output_text","text":"test output"}]}]}}`, want: true},
		{name: "done marker", data: `[DONE]`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, openAIStreamDataStartsVisibleOutput(tt.data, tt.eventType))
		})
	}
}

func TestOpenAIStreamTTFTIsIndependentFromClientStaging(t *testing.T) {
	data := `{"type":"response.output_item.added","item":{"id":"item_test","type":"reasoning","summary":[]}}`
	require.False(t, openAIStreamDataStartsClientOutput(data, "response.output_item.added"))
	require.False(t, openAIStreamDataStartsVisibleOutput(data, "response.output_item.added"))
	require.True(t, openAIStreamDataStartsTTFT(data, "response.output_item.added", false, OpenAITTFTModeSemantic))
}

func TestOpenAINativeTTFTPreservesStagedPreamble(t *testing.T) {
	for _, mode := range []string{OpenAITTFTModeSemantic, OpenAITTFTModeVisible} {
		t.Run(mode, func(t *testing.T) {
			previous, _ := gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings)
			gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: mode})
			t.Cleanup(func() { gatewayForwardingCache.Store(previous) })
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 1}}}
			preamble := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_staged\",\"model\":\"actual-model\"}}\n\n"
			metadata := "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"reasoning\",\"summary\":[]}}\n\n"
			body := preamble + metadata + "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_staged\",\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"
			c, rec := newNonStreamingFailoverContext(t)
			resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, newNonStreamingFailoverAccount(), time.Now(), "model", "model")
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), preamble+metadata)
			require.Equal(t, 7, result.usage.InputTokens)
			require.Equal(t, 3, result.usage.OutputTokens)
			require.Equal(t, "actual-model", result.upstreamResponseModel)
		})
	}
}

func TestOpenAINativeTTFTMetadataDoesNotLeakBeforeFailover(t *testing.T) {
	for _, mode := range []string{OpenAITTFTModeSemantic, OpenAITTFTModeVisible} {
		t.Run(mode, func(t *testing.T) {
			previous, _ := gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings)
			gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: mode})
			t.Cleanup(func() { gatewayForwardingCache.Store(previous) })
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 1}}}
			metadata := "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"reasoning\",\"summary\":[]}}\n\n"
			largeMetadata := "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"" + strings.Repeat("x", 8192) + "\",\"type\":\"reasoning\",\"summary\":[]}}\n\n"
			body := metadata + largeMetadata + "data: {\"type\":\"response.failed\",\"error\":{\"message\":\"temporary upstream failure\"}}\n\n"
			c, rec := newNonStreamingFailoverContext(t)
			resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, newNonStreamingFailoverAccount(), time.Now(), "model", "model")
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.False(t, c.Writer.Written())
			require.Empty(t, rec.Body.String())
		})
	}
}

func TestOpenAINativeTTFTModeDoesNotChangeFirstOutputTimeout(t *testing.T) {
	for _, mode := range []string{OpenAITTFTModeSemantic, OpenAITTFTModeVisible} {
		for _, progress := range []bool{false, true} {
			name := mode + "/metadata"
			if progress {
				name = mode + "/encrypted_progress"
			}
			t.Run(name, func(t *testing.T) {
				previous, _ := gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings)
				gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: mode})
				t.Cleanup(func() { gatewayForwardingCache.Store(previous) })
				svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 1}}}
				reader, writer := io.Pipe()
				done := make(chan struct{})
				stop := make(chan struct{})
				t.Cleanup(func() {
					close(stop)
					_ = reader.Close()
					_ = writer.Close()
					<-done
				})
				go func() {
					defer close(done)
					defer writer.Close()
					item := `{"type":"reasoning","summary":[]}`
					if progress {
						item = `{"type":"reasoning","encrypted_content":"encrypted"}`
					}
					_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.added\",\"item\":"+item+"}\n\n")
					select {
					case <-stop:
						return
					case <-time.After(1300 * time.Millisecond):
					}
					if progress {
						_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"+
							"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n")
					}
				}()
				c, rec := newNonStreamingFailoverContext(t)
				resp := &http.Response{Header: http.Header{}, Body: reader}
				result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, newNonStreamingFailoverAccount(), time.Now(), "model", "model")
				if !progress {
					var failoverErr *UpstreamFailoverError
					require.ErrorAs(t, err, &failoverErr)
					require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
					require.True(t, failoverErr.SafeToFailoverAfterWrite)
					require.Empty(t, rec.Body.String())
					return
				}
				require.NoError(t, err)
				require.Contains(t, rec.Body.String(), "encrypted_content")
				require.NotNil(t, result.firstTokenMs)
				if mode == OpenAITTFTModeVisible {
					require.GreaterOrEqual(t, *result.firstTokenMs, 1200)
				}
			})
		}
	}
}

func TestOpenAIResponsesTTFTStartsAtVisibleOutput(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			result := runSyntheticVisibleTTFTStream(t, passthrough, 120*time.Millisecond, 0, OpenAITTFTModeVisible,
				`{"type":"response.output_text.delta","delta":"test output"}`)
			require.NotNil(t, result.firstTokenMs)
			require.GreaterOrEqual(t, *result.firstTokenMs, 100)
		})
	}
}

func TestOpenAIResponsesTTFTStartsAtCompletedImage(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			result := runSyntheticVisibleTTFTStream(t, passthrough, 120*time.Millisecond, 0, OpenAITTFTModeVisible,
				`{"type":"response.output_item.done","item":{"id":"item_test","type":"image_generation_call","result":"dGVzdA=="}}`)
			require.NotNil(t, result.firstTokenMs)
			require.GreaterOrEqual(t, *result.firstTokenMs, 100)
		})
	}
}

func TestOpenAINativeMetadataDoesNotDisarmFirstOutputTimeout(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     defaultMaxLineSize,
		OpenAIFirstOutputTimeoutSeconds: 1,
	}}}
	reader, writer := io.Pipe()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item_test\",\"type\":\"reasoning\",\"summary\":[]}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"keepalive\"}\n\n")
		time.Sleep(1200 * time.Millisecond)
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	account := &Account{ID: 1, Name: "account_test", Platform: PlatformOpenAI}

	_, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "test-model", "test-model")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.SafeToFailoverAfterWrite)
	require.Empty(t, recorder.Body.String())
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("synthetic upstream writer did not exit")
	}
}

func TestOpenAIResponsesTTFTDefaultsToSemanticOutput(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			result := runSyntheticVisibleTTFTStream(t, passthrough, 120*time.Millisecond, 0, "",
				`{"type":"response.output_text.delta","delta":"test output"}`)
			require.NotNil(t, result.firstTokenMs)
			require.Less(t, *result.firstTokenMs, 100)
		})
	}
}

func runSyntheticVisibleTTFTStream(t *testing.T, passthrough bool, visibleDelay time.Duration, timeoutSeconds int, ttftMode string, visibleEvent string) *openaiStreamingResult {
	t.Helper()
	mode := ttftMode
	if mode == "" {
		mode = OpenAITTFTModeSemantic
	}
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: mode, expiresAt: time.Now().Add(time.Minute).UnixNano()})
	t.Cleanup(func() {
		gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: OpenAITTFTModeSemantic, expiresAt: time.Now().Add(time.Minute).UnixNano()})
	})
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:                     defaultMaxLineSize,
		OpenAIFirstOutputTimeoutSeconds: timeoutSeconds,
	}}}
	reader, writer := io.Pipe()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = writer.Close() }()
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item_test\",\"type\":\"reasoning\",\"summary\":[]}}\n\n")
		time.Sleep(visibleDelay)
		_, _ = io.WriteString(writer, "data: "+visibleEvent+"\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	account := &Account{ID: 1, Name: "account_test", Platform: PlatformOpenAI}
	started := time.Now()

	var result *openaiStreamingResult
	var err error
	if passthrough {
		var passthroughResult *openaiStreamingResultPassthrough
		passthroughResult, err = svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, started, "test-model", "test-model")
		if passthroughResult != nil {
			result = &openaiStreamingResult{firstTokenMs: passthroughResult.firstTokenMs}
		}
	} else {
		result, err = svc.handleStreamingResponse(context.Background(), resp, c, account, started, "test-model", "test-model")
	}
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, recorder.Body.String(), `"type":"response.output_item.added"`)
	require.Contains(t, recorder.Body.String(), visibleEvent)
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("synthetic upstream writer did not exit")
	}
	return result
}
