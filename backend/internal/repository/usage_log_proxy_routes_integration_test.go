//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUsageLogProxyRoutesPersistenceAllInsertPaths(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newUsageLogRepositoryWithSQL(client, integrationDB)
	user := mustCreateUser(t, client, &service.User{Email: "proxy-routes-" + uuid.NewString() + "@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-proxy-routes-" + uuid.NewString(), Name: "route-fixture"})
	account := mustCreateAccount(t, client, &service.Account{Name: "proxy-routes-" + uuid.NewString()})
	routes := []service.ProxyRouteSnapshot{
		{Kind: "proxy", ProxyID: 8181, Name: "original-node", Host: "2001:db8::7", Port: 1080},
		{Kind: "direct"},
		{Kind: "proxy", ProxyID: 8282, Name: "retry-node", Host: "proxy.example", Port: 3128},
	}
	for _, mode := range []string{"single", "batch", "best_effort_batch", "billing_no_result", "historical"} {
		t.Run(mode, func(t *testing.T) {
			log := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "route-model", InputTokens: 3, ProxyRoutes: routes, CreatedAt: time.Now().UTC()}
			requestID, apiKeyID := log.RequestID, log.APIKeyID
			t.Cleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				// These insert paths commit outside testEntTx, including on assertion failures.
				_, err := integrationDB.ExecContext(cleanupCtx, "DELETE FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", requestID, apiKeyID)
				require.NoError(t, err, "delete proxy route usage fixture %q", requestID)
				var remaining int
				err = integrationDB.QueryRowContext(cleanupCtx, "SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", requestID, apiKeyID).Scan(&remaining)
				require.NoError(t, err, "verify proxy route usage fixture cleanup %q", requestID)
				require.Zero(t, remaining, "proxy route usage fixture %q must not leak into later tests", requestID)
			})
			if mode == "historical" {
				log.ProxyRoutes = nil
			}
			prepared := prepareUsageLogInsert(log)
			switch mode {
			case "single", "historical":
				inserted, err := repo.createSingle(ctx, integrationDB, log)
				require.NoError(t, err)
				require.True(t, inserted)
			case "batch":
				batchKey := usageLogBatchKey(log.RequestID, key.ID)
				inserted, state, _, err := repo.batchInsertUsageLogs(integrationDB, []string{batchKey}, map[string]usageLogInsertPrepared{batchKey: prepared})
				require.NoError(t, err)
				require.True(t, inserted[batchKey])
				log.ID = state[batchKey].ID
			case "best_effort_batch":
				query, args := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
				_, err := integrationDB.ExecContext(ctx, query, args...)
				require.NoError(t, err)
			case "billing_no_result":
				require.NoError(t, execUsageLogInsertNoResult(ctx, integrationDB, prepared))
			}
			if log.ID == 0 {
				require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT id FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", log.RequestID, key.ID).Scan(&log.ID))
			}
			got, err := repo.GetByID(ctx, log.ID)
			require.NoError(t, err)
			require.Equal(t, log.ProxyRoutes, got.ProxyRoutes)
			var raw sql.NullString
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT proxy_routes FROM usage_logs WHERE id = $1", log.ID).Scan(&raw))
			if mode == "historical" {
				require.False(t, raw.Valid, "old rows must retain SQL NULL")
			} else {
				require.True(t, raw.Valid)
				require.JSONEq(t, `[{"kind":"proxy","proxy_id":8181,"name":"original-node","host":"2001:db8::7","port":1080},{"kind":"direct"},{"kind":"proxy","proxy_id":8282,"name":"retry-node","host":"proxy.example","port":3128}]`, raw.String)
				// The generated Ent reader must agree with the raw repository reader.
				entity, err := client.UsageLog.Get(ctx, log.ID)
				require.NoError(t, err)
				var fromEnt []service.ProxyRouteSnapshot
				require.NoError(t, json.Unmarshal(entity.ProxyRoutes, &fromEnt))
				require.Equal(t, routes, fromEnt)
			}
		})
	}
}

func TestUsageLogProxyRoutesMigrationRemainsNullableWithoutDefault(t *testing.T) {
	var dataType, nullable string
	var defaultValue sql.NullString
	err := integrationDB.QueryRowContext(context.Background(), `SELECT data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'usage_logs' AND column_name = 'proxy_routes'`).Scan(&dataType, &nullable, &defaultValue)
	require.NoError(t, err)
	require.Equal(t, "jsonb", dataType)
	require.Equal(t, "YES", nullable)
	require.False(t, defaultValue.Valid, "unknown historical routing must not receive an inferred default")
}
