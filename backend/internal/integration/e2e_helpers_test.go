//go:build e2e

package integration

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func strictE2E() bool {
	return os.Getenv("E2E_STRICT") == "1"
}

func requireE2EEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		if strictE2E() {
			t.Fatalf("strict E2E requires %s", name)
		}
		t.Skipf("fixture not configured: %s", name)
	}
	return value
}

func e2eModelFixtures(t *testing.T, name string, defaults []string) []string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		if strictE2E() {
			t.Fatalf("strict E2E requires explicit model fixtures in %s", name)
		}
		return defaults
	}
	var models []string
	seen := make(map[string]bool)
	for _, entry := range strings.Split(raw, ",") {
		model := strings.TrimSpace(entry)
		if model == "" || strings.ContainsAny(model, "?#\r\n\t") {
			t.Fatalf("invalid model fixture in %s", name)
		}
		if !seen[model] {
			models = append(models, model)
			seen[model] = true
		}
	}
	return models
}

func requireE2EStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Fatalf("unexpected HTTP status: got %d, want %d (response body omitted)", resp.StatusCode, expected)
	}
}

func decodeE2EJSON(t *testing.T, reader io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(io.LimitReader(reader, 8<<20)).Decode(target); err != nil {
		t.Fatal("response is not valid JSON (response body omitted)")
	}
}

func decodeE2EAPIData(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	requireE2EStatus(t, resp, http.StatusOK)
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	decodeE2EJSON(t, resp.Body, &envelope)
	if envelope.Code == nil || *envelope.Code != 0 {
		t.Fatal("API response must contain code=0 (response body omitted)")
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		t.Fatal("API response is missing data")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatal("API response data has an unexpected shape (response body omitted)")
	}
}

func requireE2EAnthropicMessage(t *testing.T, result map[string]any) {
	t.Helper()
	content, ok := result["content"].([]any)
	id, _ := result["id"].(string)
	if result["type"] != "message" || id == "" || !ok || len(content) == 0 || result["error"] != nil {
		t.Fatal("expected a successful Anthropic message with ID and non-empty content (response body omitted)")
	}
}

func requireE2ESSE(t *testing.T, resp *http.Response, protocol string) {
	t.Helper()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("streaming response must use text/event-stream")
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	started, completed, content := false, false, false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			t.Fatal("SSE data is not valid JSON (response body omitted)")
		}
		if frame["type"] == "error" || frame["error"] != nil {
			t.Fatal("SSE stream contains an error event (response body omitted)")
		}
		if protocol == "anthropic" {
			started = started || frame["type"] == "message_start"
			completed = completed || frame["type"] == "message_stop"
			content = content || frame["type"] == "content_block_delta"
		} else if candidates, ok := frame["candidates"].([]any); ok && len(candidates) > 0 {
			started = true
			for _, raw := range candidates {
				candidate, _ := raw.(map[string]any)
				if body, ok := candidate["content"].(map[string]any); ok {
					parts, _ := body["parts"].([]any)
					content = content || len(parts) > 0
				}
				finish, _ := candidate["finishReason"].(string)
				if finish != "" && finish != "STOP" && finish != "MAX_TOKENS" {
					t.Fatal("Gemini SSE ended with a non-success finish reason (response body omitted)")
				}
				completed = completed || finish == "STOP" || finish == "MAX_TOKENS"
			}
		}
	}
	if scanner.Err() != nil {
		t.Fatal("failed while reading the complete SSE stream")
	}
	if !started || !completed || !content {
		t.Fatal("SSE stream must contain response content and a successful terminal event")
	}
}

// =============================================================================
// E2E Mock 模式支持
// =============================================================================
// E2E_MOCK is a legacy opt-in flag, not an HTTP transport replacement.
// Strict acceptance must exercise the application with explicit upstream fixtures.

// isMockMode 检查是否启用 Mock 模式
func isMockMode() bool {
	return strings.EqualFold(os.Getenv("E2E_MOCK"), "true")
}

// skipIfNoRealAPI 如果未配置真实 API Key 且不在 Mock 模式，则跳过测试
func skipIfNoRealAPI(t *testing.T) {
	t.Helper()
	if isMockMode() {
		if strictE2E() {
			t.Fatal("strict E2E does not permit E2E_MOCK; configure a real application with isolated upstream fixtures")
		}
		return // Mock 模式下不跳过
	}
	claudeKey := strings.TrimSpace(os.Getenv(claudeAPIKeyEnv))
	geminiKey := strings.TrimSpace(os.Getenv(geminiAPIKeyEnv))
	if claudeKey == "" && geminiKey == "" {
		if strictE2E() {
			t.Fatal("strict E2E requires configured gateway API key fixtures")
		}
		t.Skip("未设置 API Key 且未启用 Mock 模式，跳过测试")
	}
}

// =============================================================================
// API Key 脱敏（Task 6.10）
// =============================================================================

// safeLogKey never records any credential bytes.
func safeLogKey(t *testing.T, prefix string, key string) {
	t.Helper()
	key = strings.TrimSpace(key)
	t.Logf("%s: [redacted] (length: %d)", prefix, len(key))
}
