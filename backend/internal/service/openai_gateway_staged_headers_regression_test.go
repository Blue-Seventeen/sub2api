package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIQueuedResponseWriter struct {
	gin.ResponseWriter
	scanned <-chan struct{}
	once    sync.Once
}

func (w *openAIQueuedResponseWriter) Written() bool {
	w.once.Do(func() { <-w.scanned })
	return w.ResponseWriter.Written()
}

func runOpenAIBackloggedStageRegression(t *testing.T) (*openAIResponseFlushRecorder, http.Header, string, string) {
	t.Helper()
	previousSettings := gatewayForwardingCache.Load()
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: OpenAITTFTModeSemantic})
	t.Cleanup(func() {
		if previousSettings != nil {
			gatewayForwardingCache.Store(previousSettings)
		} else {
			gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: OpenAITTFTModeSemantic})
		}
	})
	const preamble = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_queued\"}}\n\n"
	const semantic = "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"rs_queued\",\"type\":\"reasoning\",\"summary\":[],\"encrypted_content\":\"opaque\"}}\n\n"
	const burst = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"a\"}\n\n" + "data: {\"type\":\"response.output_text.delta\",\"delta\":\"b\"}\n\n" + "data: {\"type\":\"response.output_text.delta\",\"delta\":\"c\"}\n\n" + "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_queued\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"abc\"}]}],\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"
	body := preamble + semantic + burst
	scanned := make(chan struct{})
	reader := &stagedOpenAISSEReadCloser{segments: [][]byte{[]byte(body)}, eofReached: scanned}
	upstreamHeaders := http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}, "Date": []string{"Wed, 07 Oct 2026 18:43:49 GMT"}, "Vary": []string{"Accept-Encoding", "Origin"}, "X-Request-Id": []string{"5c85b69a-a793-409a-8c84-489fafcd0849"}}
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Writer = &openAIQueuedResponseWriter{ResponseWriter: c.Writer, scanned: scanned}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 30, MaxLineSize: defaultMaxLineSize}}, responseHeaderFilter: responseheaders.CompileHeaderFilter(config.ResponseHeaderConfig{}), toolCorrector: NewCodexToolCorrector()}
	result, err := svc.handleStreamingResponse(context.Background(), &http.Response{StatusCode: http.StatusOK, Header: upstreamHeaders, Body: reader}, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, time.Now(), "gpt-5", "gpt-5")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.firstTokenMs)
	require.Equal(t, 7, result.usage.InputTokens)
	require.Equal(t, 3, result.usage.OutputTokens)
	gotBody, _ := recorder.snapshot()
	require.Equal(t, body, gotBody, "staging repair must not change SSE bytes")
	return recorder, upstreamHeaders, preamble + semantic, body
}

func TestOpenAIStagedHeaders_BackloggedSemanticEventsApplyOnce(t *testing.T) {
	recorder, upstreamHeaders, _, _ := runOpenAIBackloggedStageRegression(t)
	for _, key := range []string{"Date", "Vary", "X-Request-Id"} {
		if got, want := recorder.Header().Values(key), upstreamHeaders.Values(key); !reflect.DeepEqual(got, want) {
			t.Errorf("staged %s values repeated before first flush: got %q, want %q", key, got, want)
		}
	}
	require.Equal(t, []string{"text/event-stream"}, recorder.Header().Values("Content-Type"))
	require.Equal(t, []string{"no-cache"}, recorder.Header().Values("Cache-Control"))
}

func TestOpenAIStagedHeaders_BackloggedFirstSemanticEventFlushesAtBoundary(t *testing.T) {
	recorder, _, firstCompleteSemanticEvent, body := runOpenAIBackloggedStageRegression(t)
	_, flushes := recorder.snapshot()
	require.NotEmpty(t, flushes)
	require.Equal(t, firstCompleteSemanticEvent, flushes[0], "first semantic event must flush before draining the queued burst, independently of semantic TTFT")
	require.Equal(t, body, flushes[len(flushes)-1])
}
