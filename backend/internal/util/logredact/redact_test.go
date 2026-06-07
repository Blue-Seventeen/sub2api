package logredact

import (
	"strings"
	"testing"
)

func TestRedactText_JSONLike(t *testing.T) {
	in := `{"access_token":"ya29.a0AfH6SMDUMMY","refresh_token":"1//0gDUMMY","other":"ok"}`
	out := RedactText(in)
	if out == in {
		t.Fatalf("expected redaction, got unchanged")
	}
	if want := `"access_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
	if want := `"refresh_token":"***"`; !strings.Contains(out, want) {
		t.Fatalf("expected %q in %q", want, out)
	}
}

func TestRedactText_QueryLike(t *testing.T) {
	in := "access_token=ya29.a0AfH6SMDUMMY refresh_token=1//0gDUMMY"
	out := RedactText(in)
	if strings.Contains(out, "ya29") || strings.Contains(out, "1//0") {
		t.Fatalf("expected tokens redacted, got %q", out)
	}
}

func TestRedactText_AuthorizationBearer(t *testing.T) {
	in := "upstream said Authorization: Bearer sk-secret-token access_token=plain-secret"
	out := RedactText(in)
	if strings.Contains(out, "sk-secret-token") || strings.Contains(out, "plain-secret") {
		t.Fatalf("expected bearer and query-like secrets redacted, got %q", out)
	}
	if !strings.Contains(out, "Authorization: ***") {
		t.Fatalf("expected authorization marker to be redacted, got %q", out)
	}
}

func TestRedactText_AuthorizationBasicAndProxy(t *testing.T) {
	in := "Authorization: Basic abc123-secret proxy-authorization=Bearer upstream-secret"
	out := RedactText(in)
	for _, secret := range []string{"abc123-secret", "upstream-secret", "Basic", "Bearer"} {
		if strings.Contains(out, secret) {
			t.Fatalf("expected %q redacted, got %q", secret, out)
		}
	}
	if !strings.Contains(out, "Authorization: ***") || !strings.Contains(out, "proxy-authorization=***") {
		t.Fatalf("expected auth headers redacted, got %q", out)
	}
}

func TestRedactText_APIKeyHeadersAndFields(t *testing.T) {
	in := "api_key=sk-api-secret apikey:sk-alt-secret x-api-key=sk-header-secret proxy-authorization=upstream-secret"
	out := RedactText(in)
	for _, secret := range []string{"sk-api-secret", "sk-alt-secret", "sk-header-secret", "upstream-secret"} {
		if strings.Contains(out, secret) {
			t.Fatalf("expected %q redacted, got %q", secret, out)
		}
	}
}

func TestRedactText_CookieAndCustomTokenHeaders(t *testing.T) {
	in := strings.Join([]string{
		"Set-Cookie: sid=session-secret; HttpOnly",
		"Cookie: sid=session-secret; theme=dark",
		"X-Auth-Token: custom-token-secret",
		"x-access-token=access-token-secret",
	}, "\n")
	out := RedactText(in)
	for _, secret := range []string{"session-secret", "custom-token-secret", "access-token-secret"} {
		if strings.Contains(out, secret) {
			t.Fatalf("expected %q redacted, got %q", secret, out)
		}
	}
	for _, marker := range []string{"Set-Cookie: ***", "Cookie: ***", "X-Auth-Token: ***", "x-access-token=***"} {
		if !strings.Contains(out, marker) {
			t.Fatalf("expected marker %q in %q", marker, out)
		}
	}
}

func TestRedactText_JSONStringValuesUseExtraKeys(t *testing.T) {
	in := `{"error":{"message":"custom_secret=abc api_key=sk-api-secret"},"other":"ok"}`
	out := RedactText(in, "custom_secret")
	if strings.Contains(out, "abc") || strings.Contains(out, "sk-api-secret") {
		t.Fatalf("expected secrets inside JSON string values redacted, got %q", out)
	}
	if !strings.Contains(out, `"other":"ok"`) {
		t.Fatalf("expected safe values preserved, got %q", out)
	}
}

func TestRedactText_JSONStringValues(t *testing.T) {
	in := `{"error":{"message":"Authorization: Bearer sk-secret-token access_token=plain-secret"},"other":"ok"}`
	out := RedactText(in)
	if strings.Contains(out, "sk-secret-token") || strings.Contains(out, "plain-secret") {
		t.Fatalf("expected secrets inside JSON string values redacted, got %q", out)
	}
	if !strings.Contains(out, `"other":"ok"`) {
		t.Fatalf("expected safe values preserved, got %q", out)
	}
}

func TestRedactText_GOCSPX(t *testing.T) {
	in := "client_secret=GOCSPX-your-client-secret"
	out := RedactText(in)
	if strings.Contains(out, "your-client-secret") {
		t.Fatalf("expected secret redacted, got %q", out)
	}
	if !strings.Contains(out, "client_secret=***") {
		t.Fatalf("expected key redacted, got %q", out)
	}
}

func TestRedactText_ExtraKeyCacheUsesNormalizedSortedKey(t *testing.T) {
	clearExtraTextPatternCache()

	out1 := RedactText("custom_secret=abc", "Custom_Secret", " custom_secret ")
	out2 := RedactText("custom_secret=xyz", "custom_secret")
	if !strings.Contains(out1, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in first call, got %q", out1)
	}
	if !strings.Contains(out2, "custom_secret=***") {
		t.Fatalf("expected custom key redacted in second call, got %q", out2)
	}

	if got := countExtraTextPatternCacheEntries(); got != 1 {
		t.Fatalf("expected 1 cached pattern set, got %d", got)
	}
}

func TestRedactText_DefaultPathDoesNotUseExtraCache(t *testing.T) {
	clearExtraTextPatternCache()

	out := RedactText("access_token=abc")
	if !strings.Contains(out, "access_token=***") {
		t.Fatalf("expected default key redacted, got %q", out)
	}
	if got := countExtraTextPatternCacheEntries(); got != 0 {
		t.Fatalf("expected extra cache to remain empty, got %d", got)
	}
}

func clearExtraTextPatternCache() {
	extraTextPatternCache.Range(func(key, value any) bool {
		extraTextPatternCache.Delete(key)
		return true
	})
}

func countExtraTextPatternCacheEntries() int {
	count := 0
	extraTextPatternCache.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}
