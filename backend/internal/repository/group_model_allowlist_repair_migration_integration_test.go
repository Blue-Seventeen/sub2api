//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

const groupModelAllowlistRepairMigration = "236_group_model_allowlist_repair.sql"

func TestMigration235And236PreserveLegacyDisplayWithoutEnablingAdmission(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()

	_, err := tx.ExecContext(ctx, "ALTER TABLE groups DROP COLUMN model_allowlist")
	require.NoError(t, err)

	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, rate_multiplier, status, models_list_config)
VALUES ('migration-236-display', 'anthropic', 1, 'active', '{"enabled":true,"models":["claude-sonnet-5"]}'::jsonb)
RETURNING id
`).Scan(&groupID))

	for _, name := range []string{"235_group_model_allowlist.sql", groupModelAllowlistRepairMigration, groupModelAllowlistRepairMigration} {
		content, err := dbmigrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(content))
		require.NoError(t, err)
		var display, allowlist string
		require.NoError(t, tx.QueryRowContext(ctx,
			"SELECT models_list_config::text, model_allowlist::text FROM groups WHERE id = $1", groupID).Scan(&display, &allowlist))
		require.JSONEq(t, `{"enabled":true,"models":["claude-sonnet-5"]}`, display)
		require.JSONEq(t, `{}`, allowlist)
		requireModelAllowlistColumnShape(ctx, t, tx)
	}
}

func TestMigration236PreservesIndependentPoliciesWhenBothColumnsExist(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()

	// An empty admission policy must stay disabled even with a nonempty display list.
	var disabledID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, rate_multiplier, status, model_allowlist, models_list_config)
VALUES ('migration-236-disabled', 'anthropic', 1, 'active', '{}'::jsonb, '{"enabled":true,"models":["legacy-model"]}'::jsonb)
RETURNING id
`).Scan(&disabledID))

	// 新列已有配置：不能被旧列覆盖。
	var currentID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, rate_multiplier, status, model_allowlist, models_list_config)
VALUES ('migration-236-keep', 'anthropic', 1, 'active', '{"enabled":true,"models":["current-model"]}'::jsonb, '{"enabled":true,"models":["legacy-model"]}'::jsonb)
RETURNING id
`).Scan(&currentID))

	applyGroupModelAllowlistRepair(ctx, t, tx)

	var disabled, kept, display string
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text FROM groups WHERE id = $1", disabledID).Scan(&disabled))
	require.JSONEq(t, `{}`, disabled)
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text, models_list_config::text FROM groups WHERE id = $1", currentID).Scan(&kept, &display))
	require.JSONEq(t, `{"enabled":true,"models":["current-model"]}`, kept)
	require.JSONEq(t, `{"enabled":true,"models":["legacy-model"]}`, display)
}

func TestMigration236RecreatesBothMissingPolicyColumns(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()

	_, err := tx.ExecContext(ctx, "ALTER TABLE groups DROP COLUMN model_allowlist, DROP COLUMN models_list_config")
	require.NoError(t, err)

	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, rate_multiplier, status)
VALUES ('migration-236-recreate', 'anthropic', 1, 'active')
RETURNING id
`).Scan(&groupID))

	applyGroupModelAllowlistRepair(ctx, t, tx)

	var allowlist, display string
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT model_allowlist::text, models_list_config::text FROM groups WHERE id = $1", groupID).Scan(&allowlist, &display))
	require.JSONEq(t, `{}`, allowlist)
	require.JSONEq(t, `{}`, display)
	requireModelAllowlistColumnShape(ctx, t, tx)
}

func TestMigration236AddsDisplayColumnWithoutCopyingAdmission(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	_, err := tx.ExecContext(ctx, "ALTER TABLE groups DROP COLUMN models_list_config")
	require.NoError(t, err)
	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, model_allowlist)
VALUES ('migration-236-admission-only', 'openai', '{"enabled":true,"models":["gpt-5"]}'::jsonb)
RETURNING id`).Scan(&groupID))
	applyGroupModelAllowlistRepair(ctx, t, tx)
	var display, admission string
	require.NoError(t, tx.QueryRowContext(ctx,
		"SELECT models_list_config::text, model_allowlist::text FROM groups WHERE id = $1", groupID).Scan(&display, &admission))
	require.JSONEq(t, `{}`, display)
	require.JSONEq(t, `{"enabled":true,"models":["gpt-5"]}`, admission)
}

func TestCustomUpgradeGroupPoliciesReadableByEnt(t *testing.T) {
	client := testEntTx(t).Client()
	ctx := context.Background()
	display := domain.GroupModelsListConfig{Enabled: true, Models: []string{"display-model"}}
	created, err := client.Group.Create().SetName("custom-upgrade-ent-policies").SetModelsListConfig(display).Save(ctx)
	require.NoError(t, err)
	loaded, err := client.Group.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, display, loaded.ModelsListConfig)
	require.False(t, loaded.ModelAllowlist.Enabled)
}

func applyGroupModelAllowlistRepair(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	migrationSQL, err := dbmigrations.FS.ReadFile(groupModelAllowlistRepairMigration)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
}

func requireModelAllowlistColumnShape(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()

	for _, column := range []string{"models_list_config", "model_allowlist"} {
		var isNullable, columnDefault string
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = 'groups' AND column_name = $1
`, column).Scan(&isNullable, &columnDefault))
		require.Equal(t, "NO", isNullable)
		require.Contains(t, columnDefault, "'{}'::jsonb")
	}
}
