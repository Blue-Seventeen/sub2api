package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
)

type compatibleGatewayHTTPUpstreamRecorder struct {
	responses []*http.Response
	urls      []string
}

func (u *compatibleGatewayHTTPUpstreamRecorder) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.DoWithTLS(req, "", 0, 0, nil)
}

func (u *compatibleGatewayHTTPUpstreamRecorder) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.urls = append(u.urls, req.URL.String())
	if len(u.responses) == 0 {
		return nil, nil
	}
	resp := u.responses[0]
	u.responses = u.responses[1:]
	return resp, nil
}

func newCompatibleGatewayHTTPResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func newCompatibleGatewayServiceForTest(upstream HTTPUpstream) *CompatibleGatewayService {
	return &CompatibleGatewayService{
		gatewayService: &GatewayService{
			cfg: &config.Config{
				Gateway: config.GatewayConfig{
					MaxLineSize:               defaultMaxLineSize,
					StreamDataIntervalTimeout: 0,
				},
			},
			rateLimitService: &RateLimitService{},
		},
		httpUpstream: upstream,
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				UpstreamResponseReadMaxBytes: 1 << 20,
			},
		},
	}
}

func TestCompatibleGatewayServiceForward_FallsBackToRelayChatEndpointForZhipu(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	upstream := &compatibleGatewayHTTPUpstreamRecorder{
		responses: []*http.Response{
			newCompatibleGatewayHTTPResponse(http.StatusNotFound, `{"error":{"message":"route not found"}}`),
			newCompatibleGatewayHTTPResponse(http.StatusOK, `{"id":"chatcmpl-1","object":"chat.completion","model":"glm-4.6v","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"input_tokens":1,"output_tokens":2}}`),
		},
	}
	svc := newCompatibleGatewayServiceForTest(upstream)
	account := &Account{
		ID:          1,
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"base_url": "https://relay.example.com",
			"api_key":  "test-key",
		},
	}

	result, upstreamEndpoint, err := svc.Forward(
		context.Background(),
		c,
		account,
		CompatibleRouteChatCompletions,
		[]byte(`{"model":"glm-4.6v","messages":[{"role":"user","content":"hi"}],"stream":false}`),
	)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if upstreamEndpoint != "/v1/chat/completions" {
		t.Fatalf("upstreamEndpoint = %q, want %q", upstreamEndpoint, "/v1/chat/completions")
	}
	if result == nil {
		t.Fatal("Forward() result is nil")
	}
	if len(upstream.urls) != 2 {
		t.Fatalf("len(upstream.urls) = %d, want 2", len(upstream.urls))
	}
	if upstream.urls[0] != "https://relay.example.com/api/paas/v4/chat/completions" {
		t.Fatalf("first URL = %q", upstream.urls[0])
	}
	if upstream.urls[1] != "https://relay.example.com/v1/chat/completions" {
		t.Fatalf("fallback URL = %q", upstream.urls[1])
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"hello"`) {
		t.Fatalf("response body = %s, want contains hello", rec.Body.String())
	}

	upstream.responses = []*http.Response{
		newCompatibleGatewayHTTPResponse(http.StatusOK, `{"id":"chatcmpl-2","object":"chat.completion","model":"glm-4.6v","choices":[{"index":0,"message":{"role":"assistant","content":"cached"},"finish_reason":"stop"}],"usage":{"input_tokens":1,"output_tokens":2}}`),
	}
	secondRec := httptest.NewRecorder()
	secondCtx, _ := gin.CreateTestContext(secondRec)
	_, _, err = svc.Forward(
		context.Background(),
		secondCtx,
		account,
		CompatibleRouteChatCompletions,
		[]byte(`{"model":"glm-4.6v","messages":[{"role":"user","content":"again"}],"stream":false}`),
	)
	if err != nil {
		t.Fatalf("second Forward() error = %v", err)
	}
	if len(upstream.urls) != 3 {
		t.Fatalf("len(upstream.urls) after cached request = %d, want 3", len(upstream.urls))
	}
	if upstream.urls[2] != "https://relay.example.com/v1/chat/completions" {
		t.Fatalf("cached URL = %q, want relay-compatible endpoint", upstream.urls[2])
	}

	svc.InvalidateEndpointModeCacheForAccount(account.ID)
	upstream.responses = []*http.Response{
		newCompatibleGatewayHTTPResponse(http.StatusNotFound, `{"error":{"message":"route not found"}}`),
		newCompatibleGatewayHTTPResponse(http.StatusOK, `{"id":"chatcmpl-3","object":"chat.completion","model":"glm-4.6v","choices":[{"index":0,"message":{"role":"assistant","content":"reprobe"},"finish_reason":"stop"}],"usage":{"input_tokens":1,"output_tokens":2}}`),
	}
	thirdRec := httptest.NewRecorder()
	thirdCtx, _ := gin.CreateTestContext(thirdRec)
	_, _, err = svc.Forward(
		context.Background(),
		thirdCtx,
		account,
		CompatibleRouteChatCompletions,
		[]byte(`{"model":"glm-4.6v","messages":[{"role":"user","content":"after invalidate"}],"stream":false}`),
	)
	if err != nil {
		t.Fatalf("third Forward() error = %v", err)
	}
	if len(upstream.urls) != 5 {
		t.Fatalf("len(upstream.urls) after invalidation = %d, want 5", len(upstream.urls))
	}
	if upstream.urls[3] != "https://relay.example.com/api/paas/v4/chat/completions" {
		t.Fatalf("reprobe first URL = %q, want native endpoint", upstream.urls[3])
	}
	if upstream.urls[4] != "https://relay.example.com/v1/chat/completions" {
		t.Fatalf("reprobe fallback URL = %q, want relay endpoint", upstream.urls[4])
	}
}

func TestCompatibleGatewayServiceForward_FallsBackToRelayMessagesEndpointForZhipu(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	upstream := &compatibleGatewayHTTPUpstreamRecorder{
		responses: []*http.Response{
			newCompatibleGatewayHTTPResponse(http.StatusNotFound, `{"error":{"message":"endpoint not found"}}`),
			newCompatibleGatewayHTTPResponse(http.StatusOK, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"glm-4.6v","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":5}}`),
		},
	}
	svc := newCompatibleGatewayServiceForTest(upstream)
	account := &Account{
		ID:          2,
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"base_url": "https://relay.example.com",
			"api_key":  "test-key",
		},
	}

	result, upstreamEndpoint, err := svc.Forward(
		context.Background(),
		c,
		account,
		CompatibleRouteMessages,
		[]byte(`{"model":"glm-4.6v","messages":[{"role":"user","content":"hi"}],"max_tokens":16,"stream":false}`),
	)
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if upstreamEndpoint != "/v1/messages" {
		t.Fatalf("upstreamEndpoint = %q, want %q", upstreamEndpoint, "/v1/messages")
	}
	if result == nil {
		t.Fatal("Forward() result is nil")
	}
	if len(upstream.urls) != 2 {
		t.Fatalf("len(upstream.urls) = %d, want 2", len(upstream.urls))
	}
	if upstream.urls[0] != "https://relay.example.com/api/anthropic/v1/messages" {
		t.Fatalf("first URL = %q", upstream.urls[0])
	}
	if upstream.urls[1] != "https://relay.example.com/v1/messages" {
		t.Fatalf("fallback URL = %q", upstream.urls[1])
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"hello"`) {
		t.Fatalf("response body = %s, want contains hello", rec.Body.String())
	}
}
