//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func applyCustomUpgradeMigration(t *testing.T, tx *sql.Tx, name string) {
	t.Helper()
	content, err := dbmigrations.FS.ReadFile(name)
	require.NoError(t, err)
	_, err = tx.ExecContext(context.Background(), string(content))
	require.NoError(t, err, name)
}

func TestCustomUpgradeIntermediateConstraintsKeepExistingRows(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	var userID, groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO users (email, password_hash) VALUES ('custom-migration-platforms@example.test', 'test-only') RETURNING id`).Scan(&userID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform) VALUES ('custom-migration-platforms', 'composite') RETURNING id`).Scan(&groupID))
	platforms := []string{"moonshot", "volcengine", "ali", "openrouter", "perplexity", "mistral", "siliconflow", "suno", "kling", "midjourney"}
	for i, platform := range platforms {
		_, err := tx.ExecContext(ctx, `INSERT INTO user_platform_quotas (user_id, platform) VALUES ($1, $2)`, userID, platform)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `
INSERT INTO composite_model_routes (group_id, public_model, target_platform, deleted_at)
VALUES ($1, $2, $3, CASE WHEN $4 THEN NOW() ELSE NULL END)`, groupID, fmt.Sprintf("model-%d", i), platform, i%2 == 0)
		require.NoError(t, err)
	}
	for _, name := range []string{"224_user_platform_quotas_add_cn_providers.sql", "227_composite_routes_add_cn_providers.sql", "237_add_minimax_platform.sql"} {
		applyCustomUpgradeMigration(t, tx, name)
		var quotas, routes int
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_platform_quotas WHERE user_id = $1", userID).Scan(&quotas))
		require.NoError(t, tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM composite_model_routes WHERE group_id = $1", groupID).Scan(&routes))
		require.Equal(t, len(platforms), quotas)
		require.Equal(t, len(platforms), routes)
	}
}

func TestCustomUpgrade220PreservesVideoPricesOnAllPlatforms(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	for _, platform := range []string{"openai", "moonshot", "grok", "composite", "kling"} {
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, video_price_480p, video_price_720p, video_price_1080p, video_model_prices)
VALUES ($1, $2, 0, 0.25, 0.5, '{"custom-video":{"720p":0.125}}'::jsonb) RETURNING id`, "video-preserve-"+platform, platform).Scan(&id))
		applyCustomUpgradeMigration(t, tx, "220_clear_non_grok_video_generation_config.sql")
		var low, medium, high float64
		var prices string
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT video_price_480p, video_price_720p, video_price_1080p, video_model_prices::text FROM groups WHERE id = $1`, id).Scan(&low, &medium, &high, &prices))
		require.Equal(t, 0.0, low)
		require.Equal(t, 0.25, medium)
		require.Equal(t, 0.5, high)
		require.JSONEq(t, `{"custom-video":{"720p":0.125}}`, prices)
	}
}

func TestCustomUpgradeChannelMonitorFactoryAndOperatorChoices(t *testing.T) {
	for _, operatorChoice := range []bool{false, true} {
		t.Run(fmt.Sprintf("operator_choice_%t", operatorChoice), func(t *testing.T) {
			tx := testTx(t)
			ctx := context.Background()
			_, err := tx.ExecContext(ctx, `DROP TABLE channel_monitor_v2_config;
DELETE FROM settings WHERE key IN ('channel_monitor_mode', 'channel_monitor_hide_throughput')`)
			require.NoError(t, err)
			for _, name := range []string{"194_channel_monitor_v2.sql", "195_channel_monitor_mode.sql", "196_channel_monitor_v2_ignored_error_categories.sql"} {
				applyCustomUpgradeMigration(t, tx, name)
			}
			var originalPlatforms string
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT platforms::text FROM channel_monitor_v2_config WHERE id = 1").Scan(&originalPlatforms))
			if operatorChoice {
				_, err := tx.ExecContext(ctx, `UPDATE channel_monitor_v2_config SET version = 2, updated_by = 42;
INSERT INTO settings (key, value) VALUES ('channel_monitor_hide_throughput', 'false')`)
				require.NoError(t, err)
			}
			applyCustomUpgradeMigration(t, tx, "197_channel_monitor_v2_seed_popular_models.sql")
			applyCustomUpgradeMigration(t, tx, "198_channel_monitor_v2_health_thresholds.sql")
			if operatorChoice {
				_, err := tx.ExecContext(ctx, `UPDATE channel_monitor_v2_config SET health_thresholds =
health_thresholds || '{"warning_cache_rate":0.85,"critical_cache_rate":0.60}'::jsonb`)
				require.NoError(t, err)
			}
			for _, name := range []string{"201_channel_monitor_v2_refresh_5m.sql", "203_channel_monitor_v2_default_ignore_and_cache.sql", "204_channel_monitor_hide_throughput.sql", "205_channel_monitor_v2_reset_factory_cache_thresholds.sql", "206_channel_monitor_v2_privacy_defaults.sql"} {
				applyCustomUpgradeMigration(t, tx, name)
			}
			var interval, exclusions int
			var warning, critical float64
			var platforms, hideThroughput, mode string
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT refresh_interval_seconds,
cardinality(ignored_error_categories), (health_thresholds->>'warning_cache_rate')::float8,
(health_thresholds->>'critical_cache_rate')::float8, platforms::text
FROM channel_monitor_v2_config WHERE id = 1`).Scan(&interval, &exclusions, &warning, &critical, &platforms))
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'channel_monitor_hide_throughput'").Scan(&hideThroughput))
			require.NoError(t, tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'channel_monitor_mode'").Scan(&mode))
			require.Equal(t, "v1", mode)
			if operatorChoice {
				require.Equal(t, 60, interval)
				require.Zero(t, exclusions)
				require.Equal(t, 0.85, warning)
				require.Equal(t, 0.60, critical)
				require.Equal(t, "false", hideThroughput)
				require.JSONEq(t, originalPlatforms, platforms)
			} else {
				require.Equal(t, 300, interval)
				require.Equal(t, 8, exclusions)
				require.Zero(t, warning)
				require.Zero(t, critical)
				require.Equal(t, "true", hideThroughput)
				require.NotEqual(t, originalPlatforms, platforms)
			}
		})
	}
}
