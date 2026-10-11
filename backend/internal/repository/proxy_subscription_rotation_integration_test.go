//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func rotationNode(key, server string, port int) service.ProxySubscriptionNode {
	return service.ProxySubscriptionNode{NodeKey: key, Name: "HK-01", ProviderName: "provider-" + key, Type: "ss", Server: server, Port: port, Username: "user-" + key, Password: "pass-" + key, RawConfig: "server: " + server + "\n", Status: service.ProxySubscriptionNodeStatusActive}
}

func TestProxySubscriptionRepository_RotationPreservesBindingAndInvalidatesEvidence(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)
	old := rotationNode("old-key", "old.example", 8388)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "rotation", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{old})
	require.NoError(t, err)
	require.Len(t, proxies, 1)
	before, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	var accountID int64
	require.NoError(t, scanSingleRow(ctx, tx, `INSERT INTO accounts (name, platform, type, proxy_id, extra) VALUES ('rotation-account', 'openai', 'apikey', $1, '{"upstream_billing_probe":{"status":"ok"},"keep":true}'::jsonb) RETURNING id`, []any{proxies[0].ID}, &accountID))
	incoming := rotationNode("new-key", "new.example", 443)
	created, err := repo.SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{incoming})
	require.NoError(t, err)
	require.Empty(t, created, "an endpoint rotation must reuse the logical node")
	after, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.ProxyID, after.ProxyID)
	require.Equal(t, before.NodeKey, after.NodeKey)
	require.Equal(t, before.Username, after.Username)
	require.Equal(t, before.Password, after.Password)
	require.Equal(t, incoming.Server, after.Server)
	require.Equal(t, incoming.Port, after.Port)
	require.Equal(t, incoming.RawConfig, after.RawConfig)
	require.Equal(t, service.ProxySubscriptionNodeStatusActive, after.Status)
	var bound int64
	var probe, keep bool
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT proxy_id, extra ? 'upstream_billing_probe', (extra->>'keep')::boolean FROM accounts WHERE id=$1`, []any{accountID}, &bound, &probe, &keep))
	require.Equal(t, proxies[0].ID, bound)
	require.False(t, probe, "the previous exit's billing evidence must be invalidated")
	require.True(t, keep)
	var count int
	require.NoError(t, scanSingleRow(ctx, tx, `SELECT count(*) FROM proxies WHERE subscription_id=$1`, []any{sub.ID}, &count))
	require.Equal(t, 1, count)
}

func TestProxySubscriptionRepository_RotationKeepsDisabledAndHistoricalCanonicalNode(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)
	old := rotationNode("history", "historical.example", 8388)
	current := rotationNode("current", "current.example", 8388)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "canonical", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{old, current})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE proxy_subscription_nodes SET status='source_missing' WHERE proxy_id=$1`, proxies[0].ID)
	require.NoError(t, err)
	require.NoError(t, repo.SetNodeStatusByProxyID(ctx, proxies[1].ID, service.ProxySubscriptionNodeStatusInactive))
	created, err := repo.SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{old})
	require.NoError(t, err)
	require.Empty(t, created)
	canonical, err := repo.GetNodeByProxyID(ctx, proxies[1].ID)
	require.NoError(t, err)
	require.Equal(t, "current", canonical.NodeKey)
	require.Equal(t, "historical.example", canonical.Server)
	require.Equal(t, service.ProxySubscriptionNodeStatusInactive, canonical.Status)
	historical, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusSourceMissing, historical.Status)
	states, err := repo.GetNodeStatesByProxyIDs(ctx, []int64{proxies[0].ID, proxies[1].ID, -1})
	require.NoError(t, err)
	require.Len(t, states, 2)
	require.Equal(t, service.ProxySubscriptionNodeStatusInactive, states[proxies[1].ID].Status)
}

func TestProxySubscriptionRepository_NoOpRefreshRetainsMeasurementEpoch(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)
	node := rotationNode("same", "same.example", 443)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "no-op", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{node})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE proxy_subscription_nodes SET updated_at='2000-01-01' WHERE proxy_id=$1`, proxies[0].ID)
	require.NoError(t, err)
	before, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	created, err := repo.SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{node})
	require.NoError(t, err)
	require.Empty(t, created)
	after, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, before.UpdatedAt, after.UpdatedAt)
}

func openRotationConnection(t *testing.T) *sql.DB {
	t.Helper()
	cfg, err := loadIntegrationHarnessConfig(os.Getenv)
	require.NoError(t, err)
	db, err := sql.Open("postgres", cfg.dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func requireRotationWaiter(t *testing.T, holderPID int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked bool
		err := integrationDB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1 = ANY(pg_blocking_pids(a.pid)))`, holderPID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond, "independent connection must wait on the subscription row lock")
}

func TestProxySubscriptionRepository_RefreshLockRechecksCommittedRotationAndDisable(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%v", disable), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
			sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "lock-recheck", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("first", "first.example", 443)})
			require.NoError(t, err)
			holderDB, waiterDB := openRotationConnection(t), openRotationConnection(t)
			holder, err := holderDB.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer holder.Rollback()
			var pid int
			require.NoError(t, holder.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM proxy_subscriptions WHERE id=$1 FOR UPDATE`, sub.ID).Scan(&pid))
			result := make(chan error, 1)
			go func() {
				created, err := newProxySubscriptionRepositoryWithSQL(waiterDB).SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{rotationNode("third", "third.example", 443)})
				if err == nil && len(created) != 0 {
					err = fmt.Errorf("refresh created %d proxies", len(created))
				}
				result <- err
			}()
			requireRotationWaiter(t, pid)
			holderRepo := newProxySubscriptionRepositoryWithSQL(holder)
			if disable {
				require.NoError(t, holderRepo.SetNodeStatusByProxyID(ctx, proxies[0].ID, service.ProxySubscriptionNodeStatusInactive))
			} else {
				created, err := holderRepo.SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{rotationNode("second", "second.example", 443)})
				require.NoError(t, err)
				require.Empty(t, created)
			}
			require.NoError(t, holder.Commit())
			require.NoError(t, <-result)
			got, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
			require.NoError(t, err)
			require.Equal(t, "third.example", got.Server)
			if disable {
				require.Equal(t, service.ProxySubscriptionNodeStatusInactive, got.Status)
			} else {
				require.Equal(t, service.ProxySubscriptionNodeStatusActive, got.Status)
			}
			var count int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM proxies WHERE subscription_id=$1`, sub.ID).Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestProxySubscriptionRepository_CanceledRefreshAndDisableRollBack(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%v", disable), func(t *testing.T) {
			ctx := context.Background()
			repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
			sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "cancel-lock", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("first", "first.example", 443)})
			require.NoError(t, err)
			holder, err := openRotationConnection(t).BeginTx(ctx, nil)
			require.NoError(t, err)
			defer holder.Rollback()
			var pid int
			require.NoError(t, holder.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM proxy_subscriptions WHERE id=$1 FOR UPDATE`, sub.ID).Scan(&pid))
			waiterRepo := newProxySubscriptionRepositoryWithSQL(openRotationConnection(t))
			waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if disable {
					result <- waiterRepo.SetNodeStatusByProxyID(waitCtx, proxies[0].ID, service.ProxySubscriptionNodeStatusInactive)
				} else {
					_, err := waiterRepo.SyncNodes(waitCtx, sub.ID, "", []service.ProxySubscriptionNode{rotationNode("second", "second.example", 443)})
					result <- err
				}
			}()
			requireRotationWaiter(t, pid)
			cancel()
			require.Error(t, <-result)
			require.NoError(t, holder.Rollback())
			got, err := repo.Get(ctx, sub.ID)
			require.NoError(t, err)
			require.Equal(t, int64(1), got.Revision)
			require.Len(t, got.Nodes, 1)
			require.Equal(t, "first.example", got.Nodes[0].Server)
			require.Equal(t, service.ProxySubscriptionNodeStatusActive, got.Nodes[0].Status)
		})
	}
}

func TestProxySubscriptionRepository_FailedRefreshRollsBackPartialChanges(t *testing.T) {
	ctx := context.Background()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "rollback", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("first", "first.example", 443)})
	require.NoError(t, err)
	duplicate := rotationNode("first", "second.example", 443)
	_, err = repo.SyncNodes(ctx, sub.ID, "dns: changed", []service.ProxySubscriptionNode{duplicate, duplicate})
	require.Error(t, err, "duplicate node keys must reject the entire refresh")
	got, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, "first.example", got.Server)
	var count, revision int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM proxies WHERE subscription_id=$1`, sub.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT revision FROM proxy_subscriptions WHERE id=$1`, sub.ID).Scan(&revision))
	require.Equal(t, 1, revision)
}
