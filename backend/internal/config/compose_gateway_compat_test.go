package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Render only: this test does not contact Docker Engine or start any services.
func TestLoadComposeGatewayCompatibility(t *testing.T) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("Docker Compose CLI is required for the read-only deployment contract test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, docker, "compose", "version").Run(); err != nil {
		t.Skip("Docker Compose CLI is unavailable")
	}

	const savedYAML = `timezone: Europe/London
gateway:
  force_codex_cli: true
  openai_compact_model: saved-compact-model
  openai_response_header_timeout: 1800
  openai_ws:
    force_http: true
  max_body_size: 67108864
  max_conns_per_host: 37
  max_idle_conns: 73
  max_idle_conns_per_host: 19
  scheduling:
    sticky_session_wait_timeout: 47s
    db_fallback_enabled: false
  image_nonstream_keepalive_interval: 15
`
	const explicitEnv = `TIMEZONE=UTC
GATEWAY_FORCE_CODEX_CLI=false
GATEWAY_OPENAI_COMPACT_MODEL=env-compact-model
GATEWAY_OPENAI_RESPONSE_HEADER_TIMEOUT=0
GATEWAY_OPENAI_WS_FORCE_HTTP=false
GATEWAY_MAX_BODY_SIZE=134217728
GATEWAY_MAX_CONNS_PER_HOST=0
GATEWAY_MAX_IDLE_CONNS=101
GATEWAY_MAX_IDLE_CONNS_PER_HOST=23
GATEWAY_SCHEDULING_STICKY_SESSION_WAIT_TIMEOUT=53s
GATEWAY_SCHEDULING_DB_FALLBACK_ENABLED=true
GATEWAY_IMAGE_NONSTREAM_KEEPALIVE_INTERVAL=0
SUB2API_IMAGES_MAIN_MODEL=env-image-model
`
	for _, file := range []string{
		"docker-compose.yml", "docker-compose.local.yml",
		"docker-compose.standalone.yml", "docker-compose.dev.yml",
	} {
		t.Run(file, func(t *testing.T) {
			for _, mode := range []string{"defaults", "unset", "empty", "dot_env", "shell"} {
				t.Run(mode, func(t *testing.T) {
					resetViperWithJWTSecret(t)
					for _, entry := range os.Environ() {
						key, _, _ := strings.Cut(entry, "=")
						if isComposeGatewayEnv(key) {
							t.Setenv(key, "")
						}
					}
					t.Setenv("TIMEZONE", "")
					t.Setenv("TZ", "")
					if mode != "defaults" {
						configFile := filepath.Join(t.TempDir(), "config.yaml")
						require.NoError(t, os.WriteFile(configFile, []byte(savedYAML), 0o600))
						t.Setenv("CONFIG_FILE", configFile)
					}

					envFile := ""
					shell := map[string]string{}
					switch mode {
					case "empty":
						for _, entry := range strings.Fields(explicitEnv) {
							key, _, _ := strings.Cut(entry, "=")
							envFile += key + "=\n"
						}
					case "dot_env":
						envFile = explicitEnv
					case "shell":
						envFile = "TIMEZONE=Asia/Tokyo\nGATEWAY_FORCE_CODEX_CLI=true\nGATEWAY_MAX_CONNS_PER_HOST=29\n"
						for _, entry := range strings.Fields(explicitEnv) {
							key, value, _ := strings.Cut(entry, "=")
							shell[key] = value
						}
					}
					environment := renderComposeGatewayEnv(t, docker, file, envFile, shell)
					for key, value := range environment {
						if !isComposeGatewayEnv(key) {
							continue
						}
						if mode == "defaults" || mode == "unset" || mode == "empty" {
							if key != "TZ" {
								require.Empty(t, value, "%s must not override YAML without operator intent", key)
							}
						}
						t.Setenv(key, value)
					}
					cfg, err := Load()
					require.NoError(t, err)
					switch mode {
					case "defaults":
						require.Equal(t, "Asia/Shanghai", cfg.Timezone)
						require.False(t, cfg.Gateway.ForceCodexCLI)
						require.Equal(t, "gpt-5.4", cfg.Gateway.OpenAICompactModel)
						require.Equal(t, 1024, cfg.Gateway.MaxConnsPerHost)
						require.Equal(t, 2560, cfg.Gateway.MaxIdleConns)
						require.Equal(t, 120, cfg.Gateway.MaxIdleConnsPerHost)
						require.Equal(t, 120*time.Second, cfg.Gateway.Scheduling.StickySessionWaitTimeout)
					case "unset", "empty":
						require.Equal(t, "Europe/London", cfg.Timezone)
						require.True(t, cfg.Gateway.ForceCodexCLI)
						require.Equal(t, "saved-compact-model", cfg.Gateway.OpenAICompactModel)
						require.Equal(t, 1800, cfg.Gateway.OpenAIResponseHeaderTimeout)
						require.True(t, cfg.Gateway.OpenAIWS.ForceHTTP)
						require.Equal(t, int64(67108864), cfg.Gateway.MaxBodySize)
						require.Equal(t, 37, cfg.Gateway.MaxConnsPerHost)
						require.Equal(t, 73, cfg.Gateway.MaxIdleConns)
						require.Equal(t, 19, cfg.Gateway.MaxIdleConnsPerHost)
						require.Equal(t, 47*time.Second, cfg.Gateway.Scheduling.StickySessionWaitTimeout)
						require.False(t, cfg.Gateway.Scheduling.DbFallbackEnabled)
						require.Equal(t, 15, cfg.Gateway.ImageNonstreamKeepaliveInterval)
					case "dot_env", "shell":
						require.Equal(t, "UTC", cfg.Timezone)
						require.False(t, cfg.Gateway.ForceCodexCLI)
						require.Equal(t, "env-compact-model", cfg.Gateway.OpenAICompactModel)
						require.Zero(t, cfg.Gateway.OpenAIResponseHeaderTimeout)
						require.False(t, cfg.Gateway.OpenAIWS.ForceHTTP)
						require.Equal(t, int64(134217728), cfg.Gateway.MaxBodySize)
						require.Zero(t, cfg.Gateway.MaxConnsPerHost)
						require.Equal(t, 101, cfg.Gateway.MaxIdleConns)
						require.Equal(t, 23, cfg.Gateway.MaxIdleConnsPerHost)
						require.Equal(t, 53*time.Second, cfg.Gateway.Scheduling.StickySessionWaitTimeout)
						require.True(t, cfg.Gateway.Scheduling.DbFallbackEnabled)
						require.Zero(t, cfg.Gateway.ImageNonstreamKeepaliveInterval)
						require.Equal(t, "env-image-model", environment["SUB2API_IMAGES_MAIN_MODEL"])
					}
				})
			}
		})
	}
}

func isComposeGatewayEnv(key string) bool {
	return strings.HasPrefix(key, "GATEWAY_") || key == "TZ" || key == "TIMEZONE" || key == "SUB2API_IMAGES_MAIN_MODEL"
}

func renderComposeGatewayEnv(t *testing.T, docker, file, envContent string, shell map[string]string) map[string]string {
	t.Helper()
	file, err := filepath.Abs(filepath.Join("..", "..", "..", "deploy", file))
	require.NoError(t, err)
	envFile := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(envFile, []byte("POSTGRES_PASSWORD=compose-test-only\nDATABASE_PASSWORD=compose-test-only\nDATABASE_HOST=postgres\nREDIS_HOST=redis\n"+envContent), 0o600))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, docker, "compose", "--env-file", envFile, "-f", file, "config", "--format", "json")
	// Exclude host overrides and implicit .env discovery from this fixture.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !isComposeGatewayEnv(strings.ToUpper(key)) && !strings.HasPrefix(strings.ToUpper(key), "COMPOSE_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range shell {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	require.NoError(t, err, "%s", stderr.String())
	var rendered struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(output, &rendered))
	require.NotEmpty(t, rendered.Services["sub2api"].Environment)
	return rendered.Services["sub2api"].Environment
}
