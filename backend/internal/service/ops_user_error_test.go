package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMapUserErrorCategory(t *testing.T) {
	cases := []struct {
		phase, etype, want string
	}{
		{"auth", "authentication_error", "auth"},
		{"request", "rate_limit_error", "rate_limit"},
		{"request", "billing_error", "quota"},
		{"request", "subscription_error", "quota"},
		{"request", "invalid_request_error", "invalid_request"},
		{"routing", "api_error", "service_unavailable"},
		{"account_auth", "upstream_error", "upstream"},
		{"upstream", "upstream_error", "upstream"},
		{"network", "api_error", "upstream"},
		{"internal", "api_error", "internal"},
		{"weird", "weird", "other"},
	}
	for _, c := range cases {
		if got := MapUserErrorCategory(c.phase, c.etype); got != c.want {
			t.Errorf("MapUserErrorCategory(%q,%q)=%q want %q", c.phase, c.etype, got, c.want)
		}
	}
}

func TestCategoryToFilter(t *testing.T) {
	phases, types := CategoryToFilter("rate_limit")
	if len(types) != 1 || types[0] != "rate_limit_error" || len(phases) != 0 {
		t.Fatalf("rate_limit => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("auth")
	if len(phases) != 1 || phases[0] != "auth" || len(types) != 0 {
		t.Fatalf("auth => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("service_unavailable")
	if len(phases) != 1 || phases[0] != "routing" || len(types) != 0 {
		t.Fatalf("service_unavailable => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("upstream")
	if len(phases) != 3 || phases[0] != "account_auth" || phases[1] != "upstream" || phases[2] != "network" || len(types) != 0 {
		t.Fatalf("upstream => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("internal")
	if len(phases) != 1 || phases[0] != "internal" || len(types) != 0 {
		t.Fatalf("internal => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("quota")
	if len(types) != 2 || types[0] != "billing_error" || types[1] != "subscription_error" || len(phases) != 0 {
		t.Fatalf("quota => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("invalid_request")
	if len(types) != 1 || types[0] != "invalid_request_error" || len(phases) != 0 {
		t.Fatalf("invalid_request => phases=%v types=%v", phases, types)
	}
	phases, types = CategoryToFilter("other")
	if len(phases) != 0 || len(types) != 0 {
		t.Fatalf("other => phases=%v types=%v", phases, types)
	}
}

func TestToUserErrorRequest_RedactsSensitiveFields(t *testing.T) {
	src := &OpsErrorLog{
		ID:              123,
		CreatedAt:       time.Unix(0, 0).UTC(),
		Model:           "m",
		RequestedModel:  "rm",
		InboundEndpoint: "/v1/chat/completions",
		StatusCode:      429,
		Platform:        "openai",
		Phase:           "request",
		Type:            "rate_limit_error",
		Message:         "rate limit exceeded",
		APIKeyName:      "my-key",
		APIKeyDeleted:   true,
	}
	out := ToUserErrorRequest(src)
	if out.ID != 123 {
		t.Errorf("want ID=123, got %d", out.ID)
	}
	if out.Model != "rm" {
		t.Errorf("want requested_model preferred, got %q", out.Model)
	}
	if out.Category != "rate_limit" {
		t.Errorf("category=%q", out.Category)
	}
	if out.StatusCode != 429 || out.InboundEndpoint != "/v1/chat/completions" || out.Platform != "openai" {
		t.Errorf("basic fields wrong: %+v", out)
	}
	if out.Message != "rate limit exceeded" {
		t.Errorf("want message=%q, got %q", "rate limit exceeded", out.Message)
	}
	if out.KeyName != "my-key" {
		t.Errorf("want key_name=my-key, got %q", out.KeyName)
	}
	if !out.KeyDeleted {
		t.Error("want key_deleted=true")
	}
}

func TestToUserErrorRequest_RedactsNetworkIdentifiersInUserVisibleMessage(t *testing.T) {
	message := `Post "https://japan.zelly.cn/v1/responses": dial tcp 43.165.178.21:443: connect: connection timed out while model gpt-5.4`
	src := &OpsErrorLog{
		ID:              124,
		CreatedAt:       time.Unix(0, 0).UTC(),
		Model:           "gpt-5.4",
		InboundEndpoint: "/v1/responses",
		StatusCode:      502,
		Platform:        "openai",
		Phase:           "network",
		Type:            "api_error",
		Message:         message,
	}

	out := ToUserErrorRequest(src)
	want := `Post "https://*.*.*.*/v1/responses": dial tcp *.*.*.*:443: connect: connection timed out while model gpt-5.4`
	if out.Message != want {
		t.Fatalf("message = %q, want %q", out.Message, want)
	}
	if src.Message != message {
		t.Fatalf("source message mutated: %q", src.Message)
	}
	if strings.Contains(out.Message, "japan.zelly.cn") || strings.Contains(out.Message, "43.165.178.21") {
		t.Fatalf("network identifier leaked in user message: %q", out.Message)
	}
	if !strings.Contains(out.Message, "gpt-5.4") {
		t.Fatalf("non-host model version should not be redacted: %q", out.Message)
	}
}

func TestSanitizeUserVisibleErrorText_RedactsExtendedCredentials(t *testing.T) {
	raw := "Authorization: Basic dXNlcjpwYXNzd29yZA== client_secret=client-secret-value password=super-secret Cookie=session=abcdef123456"

	got := sanitizeUserVisibleErrorText(raw)
	for _, leaked := range []string{"dXNlcjpwYXNzd29yZA==", "client-secret-value", "super-secret", "session=abcdef123456"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("sanitized text leaked %q: %q", leaked, got)
		}
	}
}

func TestToUserErrorRequest_LeavesSourceOpsLogUnchangedForAdminPath(t *testing.T) {
	message := `Post "https://japan.zelly.cn/v1/responses": dial tcp 43.165.178.21:443`
	src := &OpsErrorLog{
		ID:         125,
		Model:      "gpt-5.4",
		StatusCode: 502,
		Platform:   "openai",
		Phase:      "network",
		Type:       "api_error",
		Message:    message,
	}

	out := ToUserErrorRequest(src)
	if out == nil {
		t.Fatal("expected non-nil user view")
	}
	if out != nil && out.Message == src.Message {
		t.Fatalf("user message should be redacted, got %q", out.Message)
	}
	if src.Message != message {
		t.Fatalf("source ops message should remain unchanged for admin path, got %q", src.Message)
	}
}

func TestToUserErrorRequestDetail_WhitelistAndRedacts(t *testing.T) {
	uid := int64(42)
	upstreamStatus := 503
	src := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID:               999,
			CreatedAt:        time.Unix(1000, 0).UTC(),
			Model:            "gpt-4",
			RequestedModel:   "gpt-4-turbo",
			InboundEndpoint:  "/v1/chat/completions",
			StatusCode:       502,
			Platform:         "openai",
			Phase:            "upstream",
			Type:             "api_error",
			Message:          "upstream error",
			UserID:           &uid,
			UserEmail:        "secret@example.com",
			ClientIP:         func() *string { s := "1.2.3.4"; return &s }(),
			UpstreamEndpoint: "https://api.openai.com/v1/chat/completions",
			UserAgent:        "codex_cli_rs/0.125.0",
			GroupName:        "grp-a",
			Stream:           true,
		},
		ErrorBody:          `{"error":{"message":"upstream failed","type":"server_error"}}`,
		UpstreamStatusCode: &upstreamStatus,
	}

	out := ToUserErrorRequestDetail(src)
	if out == nil {
		t.Fatal("expected non-nil detail")
		return
	}

	// 基础字段正确映射
	if out.ID != 999 {
		t.Errorf("want ID=999, got %d", out.ID)
	}
	if out.Message != "upstream error" {
		t.Errorf("want message=%q, got %q", "upstream error", out.Message)
	}
	if out.ErrorBody != src.ErrorBody {
		t.Errorf("ErrorBody mismatch")
	}
	if out.UpstreamStatusCode == nil || *out.UpstreamStatusCode != 503 {
		t.Errorf("UpstreamStatusCode mismatch")
	}

	// client_ip 不进入用户侧错误请求 DTO；user_agent / group_name / stream 可展示。
	if out.UserAgent != "codex_cli_rs/0.125.0" {
		t.Errorf("want user_agent=codex_cli_rs/0.125.0, got %q", out.UserAgent)
	}
	if out.GroupName != "grp-a" {
		t.Errorf("want group_name=grp-a, got %q", out.GroupName)
	}
	if !out.Stream {
		t.Errorf("want stream=true")
	}

	// 序列化后不含敏感字段
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	raw := string(b)
	for _, forbidden := range []string{"user_email", "upstream_endpoint", "client_ip", "1.2.3.4"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("sensitive field %q leaked in JSON output: %s", forbidden, raw)
		}
	}
}

func TestToUserErrorRequestDetail_RedactsNetworkIdentifiersInErrorBody(t *testing.T) {
	body := `{"error":{"message":"proxy http://proxy.example.net:8080 failed via [2001:db8::1]:443 and 10.0.0.1"}}`
	src := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID:              1000,
			CreatedAt:       time.Unix(1000, 0).UTC(),
			Model:           "gpt-5.4",
			InboundEndpoint: "/v1/responses",
			StatusCode:      502,
			Platform:        "openai",
			Phase:           "network",
			Type:            "api_error",
			Message:         `retry https://upstream.example.com/v1 from 2606:4700:4700::1111`,
		},
		ErrorBody: body,
	}

	out := ToUserErrorRequestDetail(src)
	if out == nil {
		t.Fatal("expected non-nil detail")
	}
	for _, leaked := range []string{"proxy.example.net", "upstream.example.com", "10.0.0.1", "2001:db8::1", "2606:4700:4700::1111"} {
		if strings.Contains(out.Message, leaked) || strings.Contains(out.ErrorBody, leaked) {
			t.Fatalf("network identifier %q leaked: message=%q body=%q", leaked, out.Message, out.ErrorBody)
		}
	}
	for _, want := range []string{"https://*.*.*.*/v1", "http://*.*.*.*:8080", "[*.*.*.*]:443"} {
		if !strings.Contains(out.Message, want) && !strings.Contains(out.ErrorBody, want) {
			t.Fatalf("expected redacted fragment %q in message/body, got message=%q body=%q", want, out.Message, out.ErrorBody)
		}
	}
	if !strings.Contains(out.Message, "/v1") {
		t.Fatalf("upstream URL path should be preserved in user message: %q", out.Message)
	}
	if src.ErrorBody != body {
		t.Fatalf("source error body mutated: %q", src.ErrorBody)
	}
}

func TestToUserErrorRequestDetail_RedactsGenericAPIKeys(t *testing.T) {
	message := `GET https://generativelanguage.googleapis.com/v1beta/models?api_key=AIzaSyUserVisibleSecret123456&model=gpt-5.4 x-goog-api-key=goog-secret-1234567890 x-api-key=anthropic-secret-1234567890`
	body := `{"api_key":"gemini-key-secret-1234567890","authorization":"Bearer bare-token-secret-1234567890"}`
	src := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID:              1001,
			CreatedAt:       time.Unix(1000, 0).UTC(),
			Model:           "gpt-5.4",
			InboundEndpoint: "/v1/responses",
			StatusCode:      502,
			Platform:        "gemini",
			Phase:           "network",
			Type:            "api_error",
			Message:         message,
		},
		ErrorBody: body,
	}

	out := ToUserErrorRequestDetail(src)
	if out == nil {
		t.Fatal("expected non-nil detail")
	}
	for _, leaked := range []string{
		"generativelanguage.googleapis.com",
		"AIzaSyUserVisibleSecret123456",
		"goog-secret-1234567890",
		"anthropic-secret-1234567890",
		"gemini-key-secret-1234567890",
		"bare-token-secret-1234567890",
	} {
		if strings.Contains(out.Message, leaked) || strings.Contains(out.ErrorBody, leaked) {
			t.Fatalf("sensitive value %q leaked: message=%q body=%q", leaked, out.Message, out.ErrorBody)
		}
	}
	if !strings.Contains(out.Message, "https://*.*.*.*/v1beta/models?api_key=AIzaSy...3456&model=gpt-5.4") {
		t.Fatalf("path/query structure should be preserved with masked api_key, got %q", out.Message)
	}
	if !strings.Contains(out.Message, "model=gpt-5.4") {
		t.Fatalf("non-secret query value should be preserved, got %q", out.Message)
	}
	if !strings.Contains(out.ErrorBody, `"api_key":"gemini...7890"`) {
		t.Fatalf("JSON api_key should be partially masked, got %q", out.ErrorBody)
	}
	if !strings.Contains(out.ErrorBody, `"authorization":"Bearer bare-t...7890"`) {
		t.Fatalf("Bearer token should be partially masked, got %q", out.ErrorBody)
	}
}

func TestSanitizeUserVisibleErrorText_RedactsGenericTokenFields(t *testing.T) {
	input := "token=plain-token-secret access_token: access-token-secret refresh_token=refresh-token-secret admin_api_key=admin-key-secret SUB2API_ADMIN_API_KEY=sub2api-admin-secret"
	out := SanitizeUserVisibleErrorText(input)
	for _, secret := range []string{"plain-token-secret", "access-token-secret", "refresh-token-secret", "admin-key-secret", "sub2api-admin-secret"} {
		if strings.Contains(out, secret) {
			t.Fatalf("token value %q leaked in %q", secret, out)
		}
	}
}

func TestToUserErrorRequestDetail_Nil(t *testing.T) {
	if out := ToUserErrorRequestDetail(nil); out != nil {
		t.Errorf("expected nil for nil input, got %+v", out)
	}
}
