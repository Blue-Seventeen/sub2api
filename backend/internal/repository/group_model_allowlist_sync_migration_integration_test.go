//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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

type migration238RepresentativeData struct {
	userID             int64
	promoterUserID     int64
	accountID          int64
	apiKeyID           int64
	subscriptionID     int64
	usageLogID         int64
	paymentOrderID     int64
	promotionLevelID   int64
	commissionRecordID int64
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
	representative := seedMigration238RepresentativeData(t, ctx, tx, ids["migration-238-enabled"])
	assertMigration238RepresentativeData(t, ctx, tx, representative, ids["migration-238-enabled"])
	beforeNonGroupState := snapshotMigration238NonGroupTables(t, ctx, tx)

	var firstState map[string]migration238PolicyState
	for run := 1; run <= 2; run++ {
		_, err = tx.ExecContext(ctx, string(migrationSQL))
		require.NoError(t, err, "execute migration 238, run %d", run)

		state := readMigration238PolicyState(t, ctx, tx, cases, ids)
		require.Equal(t, beforeNonGroupState, snapshotMigration238NonGroupTables(t, ctx, tx),
			"migration 238 must not change non-group business records")
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

func seedMigration238RepresentativeData(t *testing.T, ctx context.Context, tx *sql.Tx, groupID int64) migration238RepresentativeData {
	t.Helper()

	var data migration238RepresentativeData

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO users (email, password_hash, role, balance, concurrency, status)
VALUES ('migration-238-user@example.test', 'migration-238-password-hash', 'user', 42.50000000, 5, 'active')
RETURNING id
`).Scan(&data.userID))

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO users (email, password_hash, role, balance, concurrency, status)
VALUES ('migration-238-promoter@example.test', 'migration-238-promoter-hash', 'user', 7.25000000, 5, 'active')
RETURNING id
`).Scan(&data.promoterUserID))

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, credentials, extra, status)
VALUES ('migration-238-account', 'anthropic', 'apikey', '{}'::jsonb, '{}'::jsonb, 'active')
RETURNING id
`).Scan(&data.accountID))

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO api_keys (user_id, key, name, group_id, status)
VALUES ($1, 'sk-migration-238-representative', 'migration-238-key', $2, 'active')
RETURNING id
`, data.userID, groupID).Scan(&data.apiKeyID))

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO user_subscriptions (
    user_id, group_id, starts_at, expires_at, status,
    daily_usage_usd, weekly_usage_usd, monthly_usage_usd
)
VALUES ($1, $2, TIMESTAMPTZ '2026-01-01 00:00:00+00', TIMESTAMPTZ '2026-02-01 00:00:00+00',
        'active', 1.25, 2.50, 3.75)
RETURNING id
`, data.userID, groupID).Scan(&data.subscriptionID))

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO usage_logs (
    user_id, api_key_id, account_id, request_id, model,
    input_tokens, output_tokens, input_cost, output_cost,
    total_cost, actual_cost, group_id, subscription_id, created_at
)
VALUES ($1, $2, $3, 'migration-238-request', 'claude-3-5-sonnet',
        100, 40, 0.0100000000, 0.0200000000,
        0.0300000000, 0.0300000000, $4, $5, TIMESTAMPTZ '2026-01-02 03:04:05+00')
RETURNING id
`, data.userID, data.apiKeyID, data.accountID, groupID, data.subscriptionID).Scan(&data.usageLogID))

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO payment_orders (
    user_id, user_email, user_name, amount, pay_amount,
    order_type, status, expires_at, out_trade_no, created_at, updated_at
)
VALUES ($1, 'migration-238-user@example.test', 'migration-238-user',
        12.50, 12.50, 'balance', 'PENDING',
        TIMESTAMPTZ '2026-02-01 00:00:00+00', 'migration-238-order',
        TIMESTAMPTZ '2026-01-02 03:04:05+00', TIMESTAMPTZ '2026-01-02 03:04:05+00')
RETURNING id
`, data.userID).Scan(&data.paymentOrderID))

	_, err := tx.ExecContext(ctx, `
INSERT INTO promotion_settings (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING
`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
INSERT INTO promotion_users (user_id, invite_code, binding_source, created_at, updated_at)
VALUES ($1, 'migration-238-promoter', 'self', TIMESTAMPTZ '2026-01-02 03:04:05+00', TIMESTAMPTZ '2026-01-02 03:04:05+00')
`, data.promoterUserID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
INSERT INTO promotion_users (user_id, invite_code, parent_user_id, binding_source, bound_at, created_at, updated_at)
VALUES ($1, 'migration-238-invitee', $2, 'admin', TIMESTAMPTZ '2026-01-02 03:04:05+00',
        TIMESTAMPTZ '2026-01-02 03:04:05+00', TIMESTAMPTZ '2026-01-02 03:04:05+00')
`, data.userID, data.promoterUserID)
	require.NoError(t, err)

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO promotion_level_configs (
    level_no, level_name, required_activated_invites,
    direct_rate, indirect_rate, sort_order, enabled
)
VALUES (238, 'migration-238-level', 0, 0.1000, 0.0500, 238, TRUE)
RETURNING id
`).Scan(&data.promotionLevelID))

	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO promotion_commission_records (
    beneficiary_user_id, source_user_id, business_date,
    commission_type, relation_depth, level_id, level_snapshot,
    rate_snapshot, base_amount, amount, status, created_by_user_id
)
VALUES ($1, $2, DATE '2026-01-02', 'commission', 0, $3,
        'migration-238-level', 0.1000, 10.00000000, 1.00000000,
        'pending', $1)
RETURNING id
`, data.promoterUserID, data.userID, data.promotionLevelID).Scan(&data.commissionRecordID))

	return data
}

func assertMigration238RepresentativeData(t *testing.T, ctx context.Context, tx *sql.Tx, data migration238RepresentativeData, groupID int64) {
	t.Helper()

	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "user with non-zero balance",
			query: `SELECT COUNT(*) FROM users WHERE id = $1 AND balance <> 0`,
			args:  []any{data.userID},
		},
		{
			name:  "api key linked to user and group",
			query: `SELECT COUNT(*) FROM api_keys WHERE id = $1 AND user_id = $2 AND group_id = $3`,
			args:  []any{data.apiKeyID, data.userID, groupID},
		},
		{
			name:  "account",
			query: `SELECT COUNT(*) FROM accounts WHERE id = $1`,
			args:  []any{data.accountID},
		},
		{
			name:  "subscription linked to user and group",
			query: `SELECT COUNT(*) FROM user_subscriptions WHERE id = $1 AND user_id = $2 AND group_id = $3`,
			args:  []any{data.subscriptionID, data.userID, groupID},
		},
		{
			name:  "usage linked to user, key, account, group, and subscription",
			query: `SELECT COUNT(*) FROM usage_logs WHERE id = $1 AND user_id = $2 AND api_key_id = $3 AND account_id = $4 AND group_id = $5 AND subscription_id = $6`,
			args:  []any{data.usageLogID, data.userID, data.apiKeyID, data.accountID, groupID, data.subscriptionID},
		},
		{
			name:  "payment order linked to user",
			query: `SELECT COUNT(*) FROM payment_orders WHERE id = $1 AND user_id = $2 AND amount > 0`,
			args:  []any{data.paymentOrderID, data.userID},
		},
		{
			name:  "promotion settings dependency",
			query: `SELECT COUNT(*) FROM promotion_settings WHERE id = 1`,
		},
		{
			name:  "promotion user dependency",
			query: `SELECT COUNT(*) FROM promotion_users WHERE user_id = $1`,
			args:  []any{data.userID},
		},
		{
			name:  "promotion level dependency",
			query: `SELECT COUNT(*) FROM promotion_level_configs WHERE id = $1`,
			args:  []any{data.promotionLevelID},
		},
		{
			name:  "promotion commission record",
			query: `SELECT COUNT(*) FROM promotion_commission_records WHERE id = $1 AND beneficiary_user_id = $2 AND source_user_id = $3 AND level_id = $4`,
			args:  []any{data.commissionRecordID, data.promoterUserID, data.userID, data.promotionLevelID},
		},
	}

	for _, check := range checks {
		var count int
		require.NoError(t, tx.QueryRowContext(ctx, check.query, check.args...).Scan(&count), check.name)
		require.Equal(t, 1, count, check.name+" must be non-empty")
	}
}

func snapshotMigration238NonGroupTables(t *testing.T, ctx context.Context, tx *sql.Tx) map[string]string {
	t.Helper()

	// These tables cover user, API-key, balance, subscription, usage, order,
	// and promotion records. Snapshot their complete rows so the migration's
	// actual database effects stay constrained to the group policy mirror.
	tables := []string{
		"users",
		"api_keys",
		"user_subscriptions",
		"usage_logs",
		"usage_cleanup_tasks",
		"payment_orders",
		"payment_audit_logs",
		"payment_provider_instances",
		"subscription_plans",
		"promo_codes",
		"promo_code_usages",
		"promotion_users",
		"promotion_settings",
		"promotion_level_configs",
		"promotion_commission_records",
		"promotion_activations",
		"promotion_settlement_batches",
		"promotion_scripts",
		"user_allowed_groups",
		"user_attribute_definitions",
		"user_attribute_values",
		"user_platform_quotas",
	}

	state := make(map[string]string, len(tables))
	for _, table := range tables {
		quotedTable := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		query := fmt.Sprintf(`
SELECT COALESCE(
    jsonb_agg(to_jsonb(row_data) ORDER BY to_jsonb(row_data)::text),
    '[]'::jsonb
)::text
FROM (SELECT * FROM public.%s) AS row_data
`, quotedTable)

		var snapshot string
		require.NoError(t, tx.QueryRowContext(ctx, query).Scan(&snapshot), table)
		state[table] = snapshot
	}
	return state
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
