//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration239GroupPolicyAuthInvalidation(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	var groupID, userID int64
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO groups (name, platform) VALUES ('migration-239', 'openai') RETURNING id`).Scan(&groupID))
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO users (email, password_hash) VALUES ('migration-239@example.test', 'hash') RETURNING id`).Scan(&userID))
	for _, key := range []struct {
		value, status string
		deleted       bool
	}{
		{"migration-239-active", "active", false},
		{"migration-239-disabled", "disabled", false},
		{"migration-239-deleted", "active", true},
		{"", "active", false},
	} {
		_, err := tx.ExecContext(ctx, `INSERT INTO api_keys (user_id, group_id, key, name, status, deleted_at)
			VALUES ($1, $2, $3, 'migration-239', $4, CASE WHEN $5 THEN NOW() ELSE NULL END)`, userID, groupID, key.value, key.status, key.deleted)
		require.NoError(t, err)
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox`)
	require.NoError(t, err)

	// Reproduce an upgrade from the immutable 193 function, then install 239 twice.
	for _, name := range []string{"193_group_profit_control_auth_cache_invalidation.sql", "239_group_model_policy_auth_cache_invalidation.sql", "239_group_model_policy_auth_cache_invalidation.sql"} {
		body, readErr := dbmigrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	for _, tc := range []struct {
		name, change string
		invalidates  bool
	}{
		{"canonical", `models_list_config = '{"enabled":true,"models":["gpt-*"]}'::jsonb`, true},
		{"mirror", `model_allowlist = '{"enabled":false,"models":["gpt-5"]}'::jsonb`, true},
		{"both", `models_list_config = '{"enabled":true,"models":["gpt-*"]}'::jsonb, model_allowlist = '{"enabled":true,"models":["gpt-*"]}'::jsonb`, true},
		{"same policy", `models_list_config = models_list_config, model_allowlist = model_allowlist`, false},
		{"cosmetic", `name = name || '-renamed', updated_at = NOW()`, false},
		{"status", `status = 'inactive'`, true},
		{"exclusive", `is_exclusive = NOT is_exclusive`, true},
		{"image permission", `allow_image_generation = NOT allow_image_generation`, true},
		{"platform", `platform = 'anthropic'`, true},
		{"subscription type", `subscription_type = 'subscription'`, true},
		{"rate", `rate_multiplier = rate_multiplier + 1`, true},
		{"peak enabled", `peak_rate_enabled = NOT peak_rate_enabled`, true},
		{"peak start", `peak_start = '09:15'`, true},
		{"peak end", `peak_end = '21:15'`, true},
		{"peak rate", `peak_rate_multiplier = peak_rate_multiplier + 1`, true},
		{"profit enabled", `profit_control_enabled = NOT profit_control_enabled`, true},
		{"profit margin", `profit_min_margin = profit_min_margin + 0.01`, true},
		{"profit buffer", `profit_safety_buffer = profit_safety_buffer + 0.01`, true},
		{"soft delete", `deleted_at = NOW()`, true},
		{"hard delete", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tx.ExecContext(ctx, `SAVEPOINT policy_change`)
			require.NoError(t, err)
			query := "UPDATE groups SET " + tc.change + " WHERE id = $1"
			if tc.change == "" {
				query = "DELETE FROM groups WHERE id = $1"
			}
			_, err = tx.ExecContext(ctx, query, groupID)
			require.NoError(t, err)
			rows, err := tx.QueryContext(ctx, `SELECT cache_key FROM auth_cache_invalidation_outbox ORDER BY cache_key`)
			require.NoError(t, err)
			var keys []string
			for rows.Next() {
				var key string
				require.NoError(t, rows.Scan(&key))
				keys = append(keys, key)
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			if tc.invalidates {
				expected := []string{
					fmt.Sprintf("%x", sha256.Sum256([]byte("migration-239-active"))),
					fmt.Sprintf("%x", sha256.Sum256([]byte("migration-239-disabled"))),
				}
				if tc.change == "" {
					// ON DELETE SET NULL also invokes the existing API-key trigger,
					// which invalidates a deleted key when its group binding changes.
					expected = append(expected, fmt.Sprintf("%x", sha256.Sum256([]byte("migration-239-deleted"))))
				}
				require.ElementsMatch(t, expected, keys)
			} else {
				require.Empty(t, keys)
			}
			_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT policy_change`)
			require.NoError(t, err)
			var count int
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_cache_invalidation_outbox`).Scan(&count))
			require.Zero(t, count, "outbox entries must roll back with the group change")
		})
	}
}
