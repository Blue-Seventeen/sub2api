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

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesChatFallbackTerminalErrorNeverCompletes(t *testing.T) {
	for name, payload := range map[string]string{
		"error":   `{"type":"error","error":{"message":"Authorization: Bearer sk-private upstream.internal","code":"private-code"}}`,
		"failed":  `{"type":"response.failed","response":{"error":{"message":"Authorization: Bearer sk-private upstream.internal"}}}`,
		"untyped": `{"error":{"message":"Authorization: Bearer sk-private upstream.internal"}}`,
	} {
		for _, partial := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/early", true: "/partial"}[partial], func(t *testing.T) {
				body := `{"model":"deepseek-reasoner","input":"hello","stream":true}`
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
				c.Set("api_key", &APIKey{ID: 11, UserID: 1})
				stream := ""
				if partial {
					stream = "data: {\"id\":\"chatcmpl_failed\",\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"unfinished reasoning\",\"content\":\"partial\"}}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\n"
				}
				stream += "data: " + payload + "\n\n"
				// An error is terminal even when the provider sends further chunks.
				stream += "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"must-not-emit\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":999}}\n\ndata: [DONE]\n\n"
				cache := &reasoningRecordingCache{}
				svc := &OpenAIGatewayService{
					cfg: rawChatCompletionsTestConfig(), cache: cache,
					httpUpstream: &httpUpstreamRecorder{resp: &http.Response{
						StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
						Body: io.NopCloser(strings.NewReader(stream)),
					}},
				}
				result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), []byte(body))
				require.Error(t, err, "upstream failure must not enter the handler's success billing path")
				require.NotNil(t, result)
				if partial {
					require.Equal(t, 5, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					require.Contains(t, rec.Body.String(), "event: response.failed")
				} else {
					require.Zero(t, result.Usage.InputTokens)
					require.Equal(t, http.StatusBadGateway, rec.Code)
				}
				for _, forbidden := range []string{"response.completed", "response.output_text.done", "data: [DONE]", "must-not-emit", "sk-private", "upstream.internal", "private-code"} {
					require.NotContains(t, rec.Body.String(), forbidden)
					require.NotContains(t, err.Error(), forbidden)
				}
				require.Empty(t, cache.snapshotSets(), "failed reasoning must not be cached")
			})
		}
	}
}

func TestCCScannerStopsAtTerminalError(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	for _, payload := range []string{`{"type":"error"}`, `{"type":"response.failed"}`, `{"error":{"message":"failure"}}`} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: {\"choices\":[]}\n\ndata: [DONE]\n\n"))}
		chunks := 0
		scan := svc.scanCCStream(nil, resp, "test", "", time.Now(), func(*apicompat.ChatCompletionsChunk) { chunks++ })
		require.Equal(t, payload, string(scan.ErrorPayload))
		require.Zero(t, chunks)
		require.False(t, scan.SawDone)
	}
}

func TestCCScannerRecognizesNamedErrorEvents(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	for _, name := range []string{"error", "response.failed"} {
		t.Run(name, func(t *testing.T) {
			payload := `{"message":"failure"}`
			resp := &http.Response{Body: io.NopCloser(strings.NewReader("event: " + name + "\ndata: " + payload + "\n\ndata: [DONE]\n\n"))}
			chunks := 0
			scan := svc.scanCCStream(nil, resp, "test", "", time.Now(), func(*apicompat.ChatCompletionsChunk) { chunks++ })
			require.Equal(t, payload, string(scan.ErrorPayload))
			require.Zero(t, chunks)
			require.False(t, scan.SawDone)
		})
	}
}

func TestCCScannerNullErrorIsNotTerminal(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: {\"choices\":[],\"error\":null}\n\ndata: [DONE]\n\n"))}
	chunks := 0
	scan := svc.scanCCStream(nil, resp, "test", "", time.Now(), func(*apicompat.ChatCompletionsChunk) { chunks++ })
	require.Empty(t, scan.ErrorPayload)
	require.Equal(t, 1, chunks)
	require.True(t, scan.SawDone)
}

func TestResponsesReasoningCacheLegacyImplementationIsNeverUsed(t *testing.T) {
	cache := &stubGatewayCache{reasoningContents: map[string]string{"same-id": "another tenant's private reasoning"}}
	// Legacy-only implementations remain source compatible, but cannot restore
	// or write reasoning without the optional scoped interface.
	upstream := forwardReasoningCacheTest(t, cache, &APIKey{UserID: 1, ID: 11}, `{"type":"reasoning","id":"same-id","summary":[],"encrypted_content":"opaque"}`)
	require.NotContains(t, upstream, "another tenant's private reasoning")
	forwardReasoningCacheTest(t, cache, &APIKey{UserID: 1, ID: 11}, `{"type":"reasoning","id":"new-id","summary":[{"text":"private"}]}`)
	require.NotContains(t, cache.reasoningContents, "new-id")
}

func TestResponsesReasoningCacheScopeAndMissingIdentity(t *testing.T) {
	cache := &reasoningRecordingCache{}
	a := &APIKey{UserID: 1, ID: 11}
	b := &APIKey{UserID: 2, ID: 11}
	c := &APIKey{UserID: 1, ID: 12}
	read := `{"type":"reasoning","id":"same-id","summary":[],"encrypted_content":"opaque"}`
	forwardReasoningCacheTest(t, cache, a, `{"type":"reasoning","id":"same-id","summary":[{"text":"tenant A"}]}`)
	require.Contains(t, forwardReasoningCacheTest(t, cache, a, read), "tenant A")
	for _, key := range []*APIKey{b, c, nil, {}, {UserID: 1}, {ID: 11}, {UserID: -1, ID: 11}} {
		require.NotContains(t, forwardReasoningCacheTest(t, cache, key, read), "tenant A")
	}
	forwardReasoningCacheTest(t, cache, b, `{"type":"reasoning","id":"same-id","summary":[{"text":"tenant B"}]}`)
	require.Contains(t, forwardReasoningCacheTest(t, cache, a, read), "tenant A")
	require.Contains(t, forwardReasoningCacheTest(t, cache, b, read), "tenant B")
	before := cache.snapshotSets()
	forwardReasoningCacheTest(t, cache, nil, `{"type":"reasoning","id":"same-id","summary":[{"text":"poison"}]}`)
	require.Equal(t, before, cache.snapshotSets())
}

func TestResponsesReasoningStreamCacheRequiresIdentity(t *testing.T) {
	cache := &reasoningRecordingCache{}
	svc := &OpenAIGatewayService{cache: cache}
	state := apicompat.NewChatCompletionsToResponsesStreamState("model")
	state.ReasoningItemID = "stream-id"
	state.Reasoning.WriteString("private stream reasoning")
	for _, key := range []*APIKey{nil, {}, {UserID: 1}, {ID: 11}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("api_key", key)
		svc.cacheResponsesStreamReasoning(context.Background(), c, state)
	}
	require.Empty(t, cache.snapshotSets())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("api_key", &APIKey{UserID: 1, ID: 11})
	svc.cacheResponsesStreamReasoning(context.Background(), c, state)
	require.Equal(t, map[string]string{"1:11:stream-id": "private stream reasoning"}, cache.snapshotSets())
	require.Equal(t, "private stream reasoning", svc.cachedResponsesReasoningContent(context.Background(), c)("stream-id"))
	c.Set("api_key", &APIKey{UserID: 1, ID: 12})
	require.Empty(t, svc.cachedResponsesReasoningContent(context.Background(), c)("stream-id"))
}

func forwardReasoningCacheTest(t *testing.T, cache GatewayCache, key *APIKey, item string) string {
	t.Helper()
	body := `{"model":"deepseek-reasoner","stream":false,"user":"1","metadata":{"api_key_id":11},"input":[` + item + `,{"type":"function_call","call_id":"call_1","name":"get_value","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	if key != nil {
		c.Set("api_key", key)
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_ok","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cache: cache}
	_, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), []byte(body))
	require.NoError(t, err)
	return string(upstream.lastBody)
}
