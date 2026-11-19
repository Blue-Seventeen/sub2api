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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// truncationOnlyAnthropicSSE 只发 message_start，既没有 message_stop，也没有任何
// 带 stop_reason 的 message_delta。这是真正的"上游半路断流"，必须继续按截断处理。
const truncationOnlyAnthropicSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_trunc","type":"message","role":"assistant","model":"k3","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}

`

// TestNativeAnthropicStreamWithoutAnyTerminalEventFails 是"反向验证"：
// 上一处改动把 native chat / native responses 的终止判定交回上游同口径的
// anthropicCompatStream.failure()（带 stop_reason 的 message_delta 算正常收尾）。
// 本用例确保"完全没有终态迹象"的截断流仍然报错，不会因为放宽而把断流当成功。
func TestNativeAnthropicStreamWithoutAnyTerminalEventFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("native chat", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(truncationOnlyAnthropicSSE)),
		}}
		body := []byte(`{"model":"k3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))

		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		result, err := svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, nativeAnthropicTestAccount(), body, "")
		require.ErrorContains(t, err, "missing terminal event")
		require.NotNil(t, result, "already-observed usage must survive the truncation")
		require.Equal(t, 10, result.Usage.InputTokens)
	})

	t.Run("native responses", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(truncationOnlyAnthropicSSE)),
		}

		result, err := (&OpenAIGatewayService{}).handleResponsesStreamingFromNativeAnthropic(
			resp, c, "k3", "k3", "k3", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
		require.ErrorContains(t, err, "missing terminal event")
		require.NotNil(t, result, "already-observed usage must survive the truncation")
		require.Equal(t, 10, result.Usage.InputTokens)
	})
}
