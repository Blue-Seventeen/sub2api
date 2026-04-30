package service

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDebugEnvBool(t *testing.T) {
	t.Run("empty is false", func(t *testing.T) {
		if parseDebugEnvBool("") {
			t.Fatalf("expected false for empty string")
		}
	})

	t.Run("true-like values", func(t *testing.T) {
		for _, value := range []string{"1", "true", "TRUE", "yes", "on"} {
			t.Run(value, func(t *testing.T) {
				if !parseDebugEnvBool(value) {
					t.Fatalf("expected true for %q", value)
				}
			})
		}
	})

	t.Run("false-like values", func(t *testing.T) {
		for _, value := range []string{"0", "false", "off", "debug"} {
			t.Run(value, func(t *testing.T) {
				if parseDebugEnvBool(value) {
					t.Fatalf("expected false for %q", value)
				}
			})
		}
	})
}

func TestDebugLogGatewaySnapshot_RedactsBodyByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway_debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("open debug log: %v", err)
	}
	defer func() { _ = f.Close() }()

	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(f)

	body := []byte(`{"system":"system-secret","messages":[{"role":"user","content":"prompt-secret"}],"tools":[{"name":"x","input_schema":{"token":"tool-secret"}}],"metadata":{"user_id":"metadata-secret"},"model":"claude"}`)
	headers := http.Header{
		"Authorization": []string{"Bearer auth-secret"},
		"X-Api-Key":     []string{"key-secret"},
	}
	svc.debugLogGatewaySnapshot("CLIENT_ORIGINAL", headers, body, map[string]string{"route": "messages"})
	_ = f.Sync()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	logged := string(raw)
	for _, secret := range []string{"system-secret", "prompt-secret", "tool-secret", "metadata-secret", "auth-secret", "key-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("debug log leaked %q: %s", secret, logged)
		}
	}
	for _, want := range []string{"CLIENT_ORIGINAL", "[redacted]", "Bearer [redacted]", "route: messages"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("debug log missing %q: %s", want, logged)
		}
	}
}

func TestDebugLogGatewaySnapshot_NonJSONBodyIsNotPersistedByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway_debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("open debug log: %v", err)
	}
	defer func() { _ = f.Close() }()

	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(f)
	svc.debugLogGatewaySnapshot("UPSTREAM_FORWARD", nil, []byte("plain-secret-body"), nil)
	_ = f.Sync()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	logged := string(raw)
	if strings.Contains(logged, "plain-secret-body") {
		t.Fatalf("debug log leaked non-json body: %s", logged)
	}
	if !strings.Contains(logged, "redacted non-json body") {
		t.Fatalf("debug log missing non-json redaction marker: %s", logged)
	}
}

func TestDebugLogGatewaySnapshot_UnsafeFullBodyRequiresExplicitFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway_debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("open debug log: %v", err)
	}
	defer func() { _ = f.Close() }()

	svc := &GatewayService{}
	svc.debugGatewayBodyFile.Store(f)
	svc.debugGatewayBodyUnsafeFull.Store(true)
	svc.debugLogGatewaySnapshot("CLIENT_ORIGINAL", nil, []byte(`{"messages":[{"content":"raw-secret"}]}`), nil)
	_ = f.Sync()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	if !strings.Contains(string(raw), "raw-secret") {
		t.Fatalf("unsafe full body mode did not preserve body: %s", string(raw))
	}
}
