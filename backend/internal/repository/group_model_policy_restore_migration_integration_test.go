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

func TestMigration237aRestoresActualV024RenameOnlyChainThrough238And239(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	applyPolicyMigration(t, ctx, tx, "193_group_profit_control_auth_cache_invalidation.sql")

	policies := []string{`{"enabled":true,"models":["gpt-5","gpt-*"]}`, `{}`, `{"enabled":false,"models":["disabled"]}`}
	ids := make([]int64, len(policies))
	for i, policy := range policies {
		require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO groups (name, platform, models_list_config, deleted_at)
VALUES ($1, 'openai', $2::jsonb, CASE WHEN $3 THEN NOW() ELSE NULL END) RETURNING id`,
			fmt.Sprintf("migration-237a-v024-%d", i), policy, i == 2).Scan(&ids[i]))
	}
	_, err := tx.ExecContext(ctx, "ALTER TABLE groups DROP COLUMN model_allowlist")
	require.NoError(t, err)
	// These fixtures are exact git v0.2.4 bytes, checked by the checksum unit test.
	for _, name := range []string{"235_group_model_allowlist.sql", "236_group_model_allowlist_repair.sql"} {
		body, readErr := modelPolicyV024Migrations.ReadFile("testdata/model_policy_v024/" + name)
		require.NoError(t, readErr)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	var canonicalExists bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_attribute
WHERE attrelid = 'groups'::regclass AND attname = 'models_list_config' AND NOT attisdropped)`).Scan(&canonicalExists))
	require.False(t, canonicalExists, "must exercise the real rename-only schema")
	_, err = tx.ExecContext(ctx, "SAVEPOINT missing_canonical")
	require.NoError(t, err)
	syncSQL, err := dbmigrations.FS.ReadFile("238_group_model_allowlist_sync.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(syncSQL))
	require.ErrorContains(t, err, "models_list_config", "238 alone must reproduce the upgrade gap")
	_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT missing_canonical")
	require.NoError(t, err)

	for replay := 0; replay < 2; replay++ {
		for _, name := range []string{"237a_restore_group_models_list_config.sql", "238_group_model_allowlist_sync.sql", "239_group_model_policy_auth_cache_invalidation.sql"} {
			applyPolicyMigration(t, ctx, tx, name)
		}
		for i, id := range ids {
			var canonical, mirror string
			var deleted bool
			require.NoError(t, tx.QueryRowContext(ctx, `SELECT models_list_config::text, model_allowlist::text, deleted_at IS NOT NULL FROM groups WHERE id = $1`, id).
				Scan(&canonical, &mirror, &deleted))
			require.JSONEq(t, policies[i], canonical)
			require.JSONEq(t, policies[i], mirror)
			require.Equal(t, i == 2, deleted)
		}
		requireGroupModelsListConfigShape(ctx, t, tx)
	}
	var defaultPolicy string
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO groups (name, platform) VALUES ('migration-237a-default', 'openai') RETURNING models_list_config::text`).Scan(&defaultPolicy))
	require.JSONEq(t, `{}`, defaultPolicy)

	// Verify 239 is effective after this upgrade, not merely executable.
	var userID int64
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO users (email, password_hash) VALUES ('migration-237a@test.invalid', 'hash') RETURNING id`).Scan(&userID))
	_, err = tx.ExecContext(ctx, `INSERT INTO api_keys (user_id, group_id, key, name) VALUES ($1, $2, 'migration-237a-key', 'policy')`, userID, ids[0])
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE groups SET models_list_config = '{}'::jsonb WHERE id = $1`, ids[0])
	require.NoError(t, err)
	var events int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_cache_invalidation_outbox
WHERE cache_key = encode(sha256(convert_to('migration-237a-key', 'UTF8')), 'hex')`).Scan(&events))
	require.Equal(t, 1, events)
}

func TestMigration237aNeverOverwritesExistingCustomCanonicalPolicy(t *testing.T) {
	for _, policy := range []string{`{}`, `{"enabled":true,"models":["canonical-authoritative"]}`, `{"enabled":false,"models":["disabled-canonical"]}`} {
		t.Run(policy, func(t *testing.T) {
			tx := testTx(t)
			ctx := context.Background()
			var groupID int64
			require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO groups (name, platform, models_list_config, model_allowlist)
VALUES ('migration-237a-custom', 'anthropic', $1::jsonb, '{"enabled":true,"models":["stale-mirror"]}'::jsonb) RETURNING id`, policy).Scan(&groupID))
			for replay := 0; replay < 2; replay++ {
				applyPolicyMigration(t, ctx, tx, "237a_restore_group_models_list_config.sql")
				var canonical, mirror string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT models_list_config::text, model_allowlist::text FROM groups WHERE id = $1`, groupID).Scan(&canonical, &mirror))
				require.JSONEq(t, policy, canonical)
				require.JSONEq(t, `{"enabled":true,"models":["stale-mirror"]}`, mirror)
			}
			for replay := 0; replay < 2; replay++ {
				for _, name := range []string{"238_group_model_allowlist_sync.sql", "239_group_model_policy_auth_cache_invalidation.sql", "237a_restore_group_models_list_config.sql"} {
					applyPolicyMigration(t, ctx, tx, name)
				}
				var canonical, mirror string
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT models_list_config::text, model_allowlist::text FROM groups WHERE id = $1`, groupID).Scan(&canonical, &mirror))
				require.JSONEq(t, policy, canonical)
				require.JSONEq(t, policy, mirror)
			}
		})
	}
}

func applyPolicyMigration(t *testing.T, ctx context.Context, tx *sql.Tx, name string) {
	t.Helper()
	body, err := dbmigrations.FS.ReadFile(name)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(body))
	require.NoError(t, err, name)
}

func requireGroupModelsListConfigShape(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()
	var nullable, columnDefault, dataType string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT is_nullable, COALESCE(column_default, ''), data_type
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = 'groups' AND column_name = 'models_list_config'`).Scan(&nullable, &columnDefault, &dataType))
	require.Equal(t, "NO", nullable)
	require.Equal(t, "'{}'::jsonb", columnDefault)
	require.Equal(t, "jsonb", dataType)
}
