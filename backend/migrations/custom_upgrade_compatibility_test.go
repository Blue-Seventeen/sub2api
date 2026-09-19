package migrations

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func migrationSQL(t *testing.T, name string) string {
	t.Helper()
	content, err := FS.ReadFile(name)
	require.NoError(t, err)
	return strings.Join(strings.Fields(string(content)), " ")
}

func TestCustomApplied194And195ChecksumsUnchanged(t *testing.T) {
	for name, checksum := range map[string]string{
		"194_add_usage_log_upstream_response_model.sql":            "cad520cbfcf7af7ea9acae92e5bcbe27501fd9e3ad5b02e306f4f97be4410a82",
		"195_add_usage_log_upstream_model_mismatch_index_notx.sql": "692f2a75f0c62670b4d68986912bf24eb92f6377ec904d3806ff7d62b0da8355",
	} {
		t.Run(name, func(t *testing.T) {
			content, err := FS.ReadFile(name)
			require.NoError(t, err)
			require.Equal(t, checksum, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(string(content))))))
		})
	}
}

func TestIntermediatePlatformConstraintsPreserveCustomPlatforms(t *testing.T) {
	platforms := []string{
		"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax",
		"volcengine", "ali", "moonshot", "perplexity", "mistral", "siliconflow", "openrouter", "suno", "kling", "midjourney",
	}
	checks := regexp.MustCompile(`CHECK \((?:platform|target_platform|provider) IN \(([^)]+)\)\)`)
	for _, name := range []string{
		"224_user_platform_quotas_add_cn_providers.sql",
		"226_channel_monitor_quota_mode.sql",
		"227_composite_routes_add_cn_providers.sql",
		"237_add_minimax_platform.sql",
	} {
		t.Run(name, func(t *testing.T) {
			matches := checks.FindAllStringSubmatch(migrationSQL(t, name), -1)
			require.NotEmpty(t, matches)
			for _, match := range matches {
				for _, platform := range platforms {
					require.Contains(t, match[1], "'"+platform+"'", "intermediate constraint must preserve %s", platform)
				}
			}
		})
	}
}

func TestModelAdmissionMigrationsNeverConsumeDisplayConfiguration(t *testing.T) {
	for _, name := range []string{"235_group_model_allowlist.sql", "236_group_model_allowlist_repair.sql"} {
		t.Run(name, func(t *testing.T) {
			sql := migrationSQL(t, name)
			require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS model_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb")
			require.NotContains(t, sql, "RENAME COLUMN")
			require.NotContains(t, sql, "SET model_allowlist = models_list_config")
			require.NotContains(t, sql, "SET models_list_config = model_allowlist")
		})
	}
}

func TestMigration220PreservesEveryPlatformsVideoPrices(t *testing.T) {
	sql := migrationSQL(t, "220_clear_non_grok_video_generation_config.sql")
	require.NotContains(t, sql, "UPDATE groups")
	require.NotContains(t, sql, "DELETE FROM groups")
	require.Contains(t, sql, "SELECT 1;")
}

func TestChannelMonitorFactoryMigrationsPreserveOperatorChoices(t *testing.T) {
	seed := migrationSQL(t, "197_channel_monitor_v2_seed_popular_models.sql")
	require.Contains(t, seed, "AND version = 1")
	require.Contains(t, seed, "AND updated_by IS NULL")
	refresh := migrationSQL(t, "201_channel_monitor_v2_refresh_5m.sql")
	require.Contains(t, refresh, "AND version IN (1, 2)")
	require.Contains(t, refresh, "AND updated_by IS NULL")
	require.Contains(t, refresh, "AND refresh_interval_seconds = 60")
	ignore := migrationSQL(t, "203_channel_monitor_v2_default_ignore_and_cache.sql")
	require.Contains(t, ignore, "AND version IN (1, 2)")
	require.Contains(t, ignore, "AND updated_by IS NULL")
	require.NotContains(t, ignore, "SET health_thresholds")
	require.NotContains(t, migrationSQL(t, "205_channel_monitor_v2_reset_factory_cache_thresholds.sql"), "UPDATE channel_monitor_v2_config")
	for _, name := range []string{"204_channel_monitor_hide_throughput.sql", "206_channel_monitor_v2_privacy_defaults.sql"} {
		sql := migrationSQL(t, name)
		require.Contains(t, sql, "VALUES ('channel_monitor_hide_throughput', 'true')")
		require.Contains(t, sql, "ON CONFLICT (key) DO NOTHING")
		require.NotContains(t, sql, "UPDATE settings")
	}
}
