package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestAccountTestServiceAliCompatibleAccountUsesChatProbe(t *testing.T) {
	c, recorder := accountProbeTestContext()
	upstream := &accountProbeHTTPUpstream{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"model\":\"qwen-max\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":null}]}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{},
			},
		},
	}
	account := &Account{
		ID:          8,
		Platform:    PlatformAli,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"base_url": "https://dashscope.example.com",
			"api_key":  "test-key",
		},
	}

	if err := svc.testCompatibleAccountConnection(c, account, "qwen-max"); err != nil {
		t.Fatalf("testCompatibleAccountConnection() error = %v", err)
	}
	if len(upstream.requests) != 1 {
		t.Fatalf("upstream requests = %d, want 1", len(upstream.requests))
	}
	gotURL := upstream.requests[0].URL.String()
	if gotURL != "https://dashscope.example.com/compatible-mode/v1/chat/completions" {
		t.Fatalf("request URL = %q", gotURL)
	}
	if strings.Contains(gotURL, "/responses") {
		t.Fatalf("Qwen account probe should not use Responses endpoint: %s", gotURL)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "OK") || !strings.Contains(body, "test_complete") {
		t.Fatalf("unexpected account test SSE body: %s", body)
	}
}

func TestAccountTestServiceOpenAIStreamSurfacesDashScopeSSEError(t *testing.T) {
	c, recorder := accountProbeTestContext()
	svc := &AccountTestService{}
	body := "id:1\n" +
		"event:error\n" +
		":HTTP_STATUS/400\n" +
		"data:{\"code\":\"InvalidParameter\",\"message\":\"Unsupported model: 'qwen-max'.\"}\n\n"

	err := svc.processOpenAIStream(c, strings.NewReader(body))
	if err == nil {
		t.Fatal("expected DashScope SSE error")
	}
	if !strings.Contains(err.Error(), "Unsupported model") {
		t.Fatalf("error = %q, want Unsupported model", err.Error())
	}
	output := recorder.Body.String()
	if !strings.Contains(output, "Unsupported model") {
		t.Fatalf("expected upstream message in SSE output, got %s", output)
	}
	if strings.Contains(output, "Stream ended before response.completed") {
		t.Fatalf("unexpected generic stream-ended error: %s", output)
	}
}

func TestAccountTestServiceOpenAIStreamAcceptsChatCompletionDone(t *testing.T) {
	c, recorder := accountProbeTestContext()
	svc := &AccountTestService{}
	body := "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	if err := svc.processOpenAIStream(c, strings.NewReader(body)); err != nil {
		t.Fatalf("processOpenAIStream() error = %v", err)
	}
	output := recorder.Body.String()
	if !strings.Contains(output, "OK") || !strings.Contains(output, "test_complete") {
		t.Fatalf("unexpected stream output: %s", output)
	}
}
