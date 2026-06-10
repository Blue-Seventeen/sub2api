package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type errAfterBytesReadCloser struct {
	data string
	err  error
	done bool
}

func (r *errAfterBytesReadCloser) Read(p []byte) (int, error) {
	if r == nil {
		return 0, errors.New("nil reader")
	}
	if !r.done {
		r.done = true
		return copy(p, []byte(r.data)), nil
	}
	if r.err != nil {
		return 0, r.err
	}
	return 0, errors.New("stream read error")
}

func (r *errAfterBytesReadCloser) Close() error { return nil }

func TestCompatibleGatewayService_StreamReadErrorSkipsUsageBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: &errAfterBytesReadCloser{
			data: "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n",
			err:  errors.New("upstream stream read failed"),
		},
	}

	svc := &CompatibleGatewayService{gatewayService: &GatewayService{}}
	prepared := &compatiblePreparedRequest{
		OriginalModel: "claude-sonnet-4",
		UpstreamModel: "kimi-k2.5",
		ClientStream:  true,
	}

	result := svc.handleMessagesResponse(resp, c, prepared, time.Now().Add(-25*time.Millisecond))

	if result == nil {
		t.Fatal("result is nil")
	}
	if !result.SkipUsageBilling {
		t.Fatalf("SkipUsageBilling = %v, want true", result.SkipUsageBilling)
	}
	if result.Usage.InputTokens != 3 {
		t.Fatalf("InputTokens = %d, want 3", result.Usage.InputTokens)
	}
}

func TestNewAPIStyleGatewayService_StreamCopyErrorSkipsUsageBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
			},
			Body: &errAfterBytesReadCloser{
				data: strings.Join([]string{
					`data: {"id":"chatcmpl_test","choices":[{"delta":{"content":"hi"}}]}`,
					`data: {"id":"chatcmpl_test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":4}}`,
					``,
				}, "\n\n"),
				err: errors.New("upstream stream read failed"),
			},
		},
	}
	svc := &NewAPIStyleGatewayService{httpUpstream: upstream}

	result, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), newAPIStyleAudioAccount(PlatformDeepSeek, nil), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		Stream:      true,
		RequestBody: []byte(`{"model":"deepseek-chat","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
		InboundPath: "/v1/chat/completions",
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if !result.SkipUsageBilling {
		t.Fatalf("SkipUsageBilling = %v, want true", result.SkipUsageBilling)
	}
}
