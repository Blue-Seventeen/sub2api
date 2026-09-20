//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

const groupModelAllowlistSyncMigration = "238_group_model_allowlist_sync.sql"

type migration238PolicyCase struct {
	name        string
	canonical   string
	mirror      string
	softDeleted bool
}

type migration238PolicyState struct {
	modelsListConfig string
	modelAllowlist   string
	softDeleted      bool
}

func TestMigration238SynchronizesCanonicalGroupModelPolicyIdempotently(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	cases := []migration238PolicyCase{
		{
			name:      "migration-238-enabled",
			canonical: `{"enabled":true,"models":["canonical-enabled"]}`,
			mirror:    `{"enabled":true,"models":["stale-enabled"]}`,
		},
		{
			name:      "migration-238-empty",
			canonical: `{}`,
			mirror:    `{"enabled":true,"models":["stale-empty"]}`,
		},
		{
			name:      "migration-238-disabled",
			canonical: `{"enabled":false,"models":["canonical-disabled"]}`,
			mirror:    `{"enabled":true,"models":["stale-disabled"]}`,
		},
		{
			name:      "migration-238-equal",
			canonical: `{"enabled":true,"models":["already-equal"]}`,
			mirror:    `{"enabled":true,"models":["already-equal"]}`,
		},
		{
			name:        "migration-238-soft-deleted",
			canonical:   `{"enabled":true,"models":["canonical-deleted"]}`,
			mirror:      `{"enabled":true,"models":["stale-deleted"]}`,
			softDeleted: true,
		},
	}

	ids := make(map[string]int64, len(cases))
	for _, policy := range cases {
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO groups (name, platform, rate_multiplier, status, models_list_config, model_allowlist, deleted_at)
VALUES ($1, 'anthropic', 1, 'active', $2::jsonb, $3::jsonb,
        CASE WHEN $4 THEN NOW() ELSE NULL END)
RETURNING id
`, policy.name, policy.canonical, policy.mirror, policy.softDeleted).Scan(&id), policy.name)
		ids[policy.name] = id
	}

	migrationSQL, err := dbmigrations.FS.ReadFile(groupModelAllowlistSyncMigration)
	require.NoError(t, err)

	var firstState map[string]migration238PolicyState
	for run := 1; run <= 2; run++ {
		_, err = tx.ExecContext(ctx, string(migrationSQL))
		require.NoError(t, err, "execute migration 238, run %d", run)

		state := readMigration238PolicyState(t, ctx, tx, cases, ids)
		for _, policy := range cases {
			actual := state[policy.name]
			require.JSONEq(t, policy.canonical, actual.modelsListConfig, policy.name+" models_list_config")
			require.JSONEq(t, policy.canonical, actual.modelAllowlist, policy.name+" model_allowlist")
			require.Equal(t, policy.softDeleted, actual.softDeleted, policy.name+" soft-delete state")
		}

		if run == 1 {
			firstState = state
		} else {
			require.Equal(t, firstState, state, "migration 238 second execution must leave all values unchanged")
		}
	}
}

func readMigration238PolicyState(t *testing.T, ctx context.Context, tx *sql.Tx, cases []migration238PolicyCase, ids map[string]int64) map[string]migration238PolicyState {
	t.Helper()

	state := make(map[string]migration238PolicyState, len(cases))
	for _, policy := range cases {
		var actual migration238PolicyState
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT models_list_config::text, model_allowlist::text, deleted_at IS NOT NULL
FROM groups
WHERE id = $1
`, ids[policy.name]).Scan(&actual.modelsListConfig, &actual.modelAllowlist, &actual.softDeleted), policy.name)
		state[policy.name] = actual
	}
	return state
}
