//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type managedSchedulerFixture struct {
	sub                    *service.ProxySubscription
	nodes                  []service.ProxySubscriptionNode
	proxyID                int64
	aliveIDs               []int64
	beforeAccountEpochs    map[int64]time.Time
	insertedAccountExtras  map[int64]json.RawMessage
	withSnapshotID         int64
	deletedID, unrelatedID int64
	repo                   *proxySubscriptionRepository
	admin                  service.AdminService
}

func newManagedSchedulerFixture(t *testing.T) managedSchedulerFixture {
	return createManagedSchedulerFixture(t, false)
}

func createManagedSchedulerFixture(t *testing.T, legacy bool) managedSchedulerFixture {
	t.Helper()
	ctx := context.Background()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	nodes := []service.ProxySubscriptionNode{rotationNode("cache-node", "cache.example", 443), rotationNode("kept-node", "kept.example", 443)}
	nodes[1].Name = "JP-01"
	if legacy {
		nodes = nil
	}
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "managed-cache", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, nodes)
	require.NoError(t, err)
	f := managedSchedulerFixture{sub: sub, nodes: nodes, proxyID: proxies[0].ID, repo: repo, insertedAccountExtras: make(map[int64]json.RawMessage)}
	// These cases need committed writes and independent connections. Register
	// cleanup before inserting accounts so even a partially built fixture is
	// removed without clearing any other test's rows. Later cleanup callbacks
	// stop the scheduler and release failure constraints before this runs.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tx, err := integrationDB.BeginTx(cleanupCtx, nil)
		require.NoError(t, err)
		defer tx.Rollback()
		ids := make([]int64, 0, len(f.insertedAccountExtras))
		for id := range f.insertedAccountExtras {
			ids = append(ids, id)
		}
		_, err = tx.ExecContext(cleanupCtx, `DELETE FROM accounts WHERE id=ANY($1::bigint[])`, pq.Array(ids))
		require.NoError(t, err)
		_, err = tx.ExecContext(cleanupCtx, `DELETE FROM proxies WHERE subscription_id=$1`, f.sub.ID)
		require.NoError(t, err)
		// Subscription nodes cascade from their parent subscription.
		_, err = tx.ExecContext(cleanupCtx, `DELETE FROM proxy_subscriptions WHERE id=$1`, f.sub.ID)
		require.NoError(t, err)
		_, err = tx.ExecContext(cleanupCtx, `DELETE FROM scheduler_outbox WHERE account_id=ANY($1::bigint[]) OR EXISTS (
			SELECT 1 FROM unnest($1::bigint[]) AS owned(id)
			WHERE payload->'account_ids' @> jsonb_build_array(owned.id)
		)`, pq.Array(ids))
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	})
	f.admin = reviewProxyAdmin(newProxyRepositoryWithSQL(integrationEntClient, integrationDB), repo, &reviewReloadResolver{})
	insert := func(name, accountType, extra string, proxyID any, deleted bool) int64 {
		var id int64
		var storedExtra []byte
		require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,proxy_id,extra,deleted_at)
			VALUES($1,'openai',$2,$3,$4::jsonb,CASE WHEN $5 THEN clock_timestamp() ELSE NULL END) RETURNING id,extra`, name, accountType, proxyID, extra, deleted).Scan(&id, &storedExtra))
		f.insertedAccountExtras[id] = json.RawMessage(storedExtra)
		return id
	}
	f.aliveIDs = append(f.aliveIDs, insert("cache-apikey-without-snapshot", "apikey", `{"keep":true}`, f.proxyID, false))
	f.aliveIDs = append(f.aliveIDs, insert("cache-oauth-without-snapshot", "oauth", `{"keep":true}`, f.proxyID, false))
	f.withSnapshotID = insert("cache-apikey-with-snapshot", "apikey", `{"keep":true,"upstream_billing_probe":{"status":"ok"}}`, f.proxyID, false)
	f.aliveIDs = append(f.aliveIDs, f.withSnapshotID)
	f.deletedID = insert("cache-deleted-bound", "apikey", `{"keep":true,"upstream_billing_probe":{"status":"ok"}}`, f.proxyID, true)
	f.unrelatedID = insert("cache-unrelated", "apikey", `{"keep":true,"upstream_billing_probe":{"status":"ok"}}`, nil, false)
	captureManagedSchedulerAccountEpochs(t, &f)
	return f
}

func captureManagedSchedulerAccountEpochs(t *testing.T, f *managedSchedulerFixture) {
	t.Helper()
	f.beforeAccountEpochs = make(map[int64]time.Time)
	for _, id := range append(append([]int64(nil), f.aliveIDs...), f.deletedID, f.unrelatedID) {
		var epoch time.Time
		require.NoError(t, integrationDB.QueryRow(`SELECT updated_at FROM accounts WHERE id=$1`, id).Scan(&epoch))
		f.beforeAccountEpochs[id] = epoch
	}
}

func requireManagedSchedulerAccountEpochs(t *testing.T, f managedSchedulerFixture, changed bool) {
	t.Helper()
	for _, id := range f.aliveIDs {
		var epoch time.Time
		require.NoError(t, integrationDB.QueryRow(`SELECT updated_at FROM accounts WHERE id=$1`, id).Scan(&epoch))
		if changed {
			require.True(t, epoch.After(f.beforeAccountEpochs[id]), "every bound account needs a newer epoch, including OAuth and APIKey accounts without snapshots; account=%d", id)
		} else {
			require.Equal(t, f.beforeAccountEpochs[id], epoch, "metadata/no-op/rollback cannot advance account epochs")
		}
	}
	for _, id := range []int64{f.deletedID, f.unrelatedID} {
		var epoch time.Time
		require.NoError(t, integrationDB.QueryRow(`SELECT updated_at FROM accounts WHERE id=$1`, id).Scan(&epoch))
		require.Equal(t, f.beforeAccountEpochs[id], epoch, "deleted/unrelated accounts are outside the proxy invalidation")
	}
}

func managedSchedulerOutboxMarker(t *testing.T) int64 {
	t.Helper()
	var id int64
	require.NoError(t, integrationDB.QueryRow(`SELECT COALESCE(max(id),0) FROM scheduler_outbox`).Scan(&id))
	return id
}

func requireManagedSchedulerNotifications(t *testing.T, afterID int64, want []int64) {
	t.Helper()
	rows, err := integrationDB.Query(`SELECT event_type,payload FROM scheduler_outbox WHERE id>$1 ORDER BY id`, afterID)
	require.NoError(t, err)
	defer rows.Close()
	var actual []int64
	for rows.Next() {
		var eventType string
		var payload []byte
		require.NoError(t, rows.Scan(&eventType, &payload))
		require.Equal(t, service.SchedulerOutboxEventAccountBulkChanged, eventType)
		var body struct {
			AccountIDs []int64 `json:"account_ids"`
		}
		require.NoError(t, json.Unmarshal(payload, &body))
		actual = append(actual, body.AccountIDs...)
	}
	require.NoError(t, rows.Err())
	sort.Slice(actual, func(i, j int) bool { return actual[i] < actual[j] })
	expected := append([]int64(nil), want...)
	sort.Slice(expected, func(i, j int) bool { return expected[i] < expected[j] })
	require.Equal(t, expected, actual, "scheduler invalidation must include every live bound account exactly once, independent of snapshots/account type")
}

func requireManagedSchedulerAccountState(t *testing.T, f managedSchedulerFixture, snapshotCleared bool) {
	t.Helper()
	for _, id := range f.aliveIDs {
		var bound int64
		var keep, snapshot bool
		require.NoError(t, integrationDB.QueryRow(`SELECT proxy_id,(extra->>'keep')::boolean,extra ? 'upstream_billing_probe' FROM accounts WHERE id=$1`, id).Scan(&bound, &keep, &snapshot))
		require.Equal(t, f.proxyID, bound, "notification cannot rebind the account")
		require.True(t, keep)
		if id == f.withSnapshotID {
			require.Equal(t, !snapshotCleared, snapshot)
		} else {
			require.False(t, snapshot)
		}
		requireManagedSchedulerExtra(t, id, f.insertedAccountExtras[id], id == f.withSnapshotID && snapshotCleared)
	}
	for _, id := range []int64{f.deletedID, f.unrelatedID} {
		var snapshot bool
		require.NoError(t, integrationDB.QueryRow(`SELECT extra ? 'upstream_billing_probe' FROM accounts WHERE id=$1`, id).Scan(&snapshot))
		require.True(t, snapshot, "deleted/unrelated account evidence must be preserved")
		requireManagedSchedulerExtra(t, id, f.insertedAccountExtras[id], false)
	}
}

func requireManagedSchedulerExtra(t *testing.T, id int64, inserted json.RawMessage, removeProbe bool) {
	t.Helper()
	// INSERT triggers can add legitimate default fields. Preserve the full
	// persisted baseline and allow only the fixture's probe key to disappear.
	var expected map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(inserted, &expected))
	if removeProbe {
		delete(expected, "upstream_billing_probe")
	}
	want, err := json.Marshal(expected)
	require.NoError(t, err)
	var equal bool
	require.NoError(t, integrationDB.QueryRow(`SELECT extra=$2::jsonb FROM accounts WHERE id=$1`, id, string(want)).Scan(&equal))
	require.True(t, equal, "proxy cache invalidation must preserve every persisted extra field except the allowed probe key; account=%d", id)
}

func TestManagedProxySchedulerOutbox_AllLiveBindingsAreNotified(t *testing.T) {
	for _, action := range []string{"admin-disable", "admin-reenable", "admin-identity", "node-disable", "rotation", "dns-sync", "dns-update-with-nodes", "source-removed", "source-returned"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			f := newManagedSchedulerFixture(t)
			if action == "admin-reenable" {
				_, err := f.admin.UpdateProxy(ctx, f.proxyID, &service.UpdateProxyInput{Status: "inactive"})
				require.NoError(t, err)
			}
			if action == "source-returned" {
				_, err := f.repo.SyncNodes(ctx, f.sub.ID, "", f.nodes[1:])
				require.NoError(t, err)
			}
			captureManagedSchedulerAccountEpochs(t, &f)
			marker := managedSchedulerOutboxMarker(t)
			switch action {
			case "admin-disable", "admin-reenable":
				status := "inactive"
				if action == "admin-reenable" {
					status = "active"
				}
				_, err := f.admin.UpdateProxy(ctx, f.proxyID, &service.UpdateProxyInput{Status: status})
				require.NoError(t, err)
			case "admin-identity":
				username := "cache-updated-auth"
				_, err := f.admin.UpdateProxy(ctx, f.proxyID, &service.UpdateProxyInput{Username: &username})
				require.NoError(t, err)
			case "node-disable":
				require.NoError(t, f.repo.SetNodeStatusByProxyID(ctx, f.proxyID, service.ProxySubscriptionNodeStatusInactive))
			case "rotation":
				incoming := append([]service.ProxySubscriptionNode(nil), f.nodes...)
				incoming[0] = rotationNode("cache-new-endpoint", "rotated.example", 8443)
				_, err := f.repo.SyncNodes(ctx, f.sub.ID, "", incoming)
				require.NoError(t, err)
			case "dns-sync":
				_, err := f.repo.SyncNodes(ctx, f.sub.ID, "nameserver: [8.8.8.8]", f.nodes)
				require.NoError(t, err)
			case "dns-update-with-nodes":
				f.sub.RawDNSConfig = "nameserver: [8.8.8.8]"
				require.NoError(t, f.repo.UpdateWithNodes(ctx, f.sub, f.nodes))
			case "source-removed":
				_, err := f.repo.SyncNodes(ctx, f.sub.ID, "", f.nodes[1:])
				require.NoError(t, err)
			case "source-returned":
				_, err := f.repo.SyncNodes(ctx, f.sub.ID, "", f.nodes)
				require.NoError(t, err)
			}
			requireManagedSchedulerAccountState(t, f, true)
			requireManagedSchedulerAccountEpochs(t, f, true)
			requireManagedSchedulerNotifications(t, marker, f.aliveIDs)
		})
	}
}

func TestManagedProxySchedulerOutbox_MetadataAndNoOpRefreshDoNotNotify(t *testing.T) {
	ctx := context.Background()
	f := newManagedSchedulerFixture(t)
	marker := managedSchedulerOutboxMarker(t)
	_, err := f.admin.UpdateProxy(ctx, f.proxyID, &service.UpdateProxyInput{Name: "cache metadata only"})
	require.NoError(t, err)
	_, err = f.admin.UpdateProxy(ctx, f.proxyID, &service.UpdateProxyInput{Status: "active"})
	require.NoError(t, err)
	_, err = f.repo.SyncNodes(ctx, f.sub.ID, "", f.nodes)
	require.NoError(t, err)
	requireManagedSchedulerAccountState(t, f, false)
	requireManagedSchedulerAccountEpochs(t, f, false)
	requireManagedSchedulerNotifications(t, marker, nil)
}

func requireManagedSchedulerRollback(t *testing.T, f managedSchedulerFixture, marker int64, before *service.ProxySubscriptionNode) {
	t.Helper()
	ctx := context.Background()
	after, err := f.repo.GetNodeByProxyID(ctx, f.proxyID)
	require.NoError(t, err)
	require.Equal(t, before.Status, after.Status)
	require.Equal(t, before.Server, after.Server)
	require.Equal(t, before.UpdatedAt, after.UpdatedAt)
	var proxyStatus string
	require.NoError(t, integrationDB.QueryRow(`SELECT status FROM proxies WHERE id=$1`, f.proxyID).Scan(&proxyStatus))
	require.Equal(t, "active", proxyStatus)
	sub, err := f.repo.Get(ctx, f.sub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), sub.Revision)
	require.Equal(t, "", sub.RawDNSConfig)
	requireManagedSchedulerAccountState(t, f, false)
	requireManagedSchedulerAccountEpochs(t, f, false)
	requireManagedSchedulerNotifications(t, marker, nil)
}

func TestManagedProxySchedulerOutbox_FailedRefreshDoesNotPublishEvents(t *testing.T) {
	ctx := context.Background()
	f := newManagedSchedulerFixture(t)
	before, err := f.repo.GetNodeByProxyID(ctx, f.proxyID)
	require.NoError(t, err)
	marker := managedSchedulerOutboxMarker(t)
	duplicate := f.nodes[0]
	duplicate.Server = "must-rollback.example"
	duplicate.RawConfig = "server: must-rollback.example\n"
	_, err = f.repo.SyncNodes(ctx, f.sub.ID, "nameserver: [8.8.8.8]", []service.ProxySubscriptionNode{duplicate, duplicate, f.nodes[1]})
	require.Error(t, err, "duplicate node key must reject a refresh after its first mutation")
	requireManagedSchedulerRollback(t, f, marker, before)
}

func TestManagedProxySchedulerOutbox_FailedOutboxRollsBackOperatorUpdate(t *testing.T) {
	ctx := context.Background()
	f := newManagedSchedulerFixture(t)
	before, err := f.repo.GetNodeByProxyID(ctx, f.proxyID)
	require.NoError(t, err)
	marker := managedSchedulerOutboxMarker(t)
	constraint := fmt.Sprintf("managed_cache_outbox_failure_%d", f.withSnapshotID)
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE scheduler_outbox ADD CONSTRAINT %s CHECK (NOT (payload @> '{"account_ids":[%d]}'::jsonb))`, constraint, f.withSnapshotID))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`ALTER TABLE scheduler_outbox DROP CONSTRAINT IF EXISTS ` + constraint)
	})
	_, err = f.admin.UpdateProxy(ctx, f.proxyID, &service.UpdateProxyInput{Status: "inactive"})
	require.ErrorContains(t, err, constraint)
	requireManagedSchedulerRollback(t, f, marker, before)
}

func TestManagedProxySchedulerOutbox_CanceledOperatorUpdateDoesNotPublishEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f := newManagedSchedulerFixture(t)
	before, err := f.repo.GetNodeByProxyID(ctx, f.proxyID)
	require.NoError(t, err)
	marker := managedSchedulerOutboxMarker(t)
	holder, err := openRotationConnection(t).BeginTx(ctx, nil)
	require.NoError(t, err)
	defer holder.Rollback()
	var pid int
	require.NoError(t, holder.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR UPDATE`, f.withSnapshotID).Scan(&pid))
	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()
	result := make(chan error, 1)
	go func() {
		_, err := f.admin.UpdateProxy(waitCtx, f.proxyID, &service.UpdateProxyInput{Status: "inactive"})
		result <- err
	}()
	requireRotationWaiter(t, pid)
	requireManagedSchedulerNotifications(t, marker, nil)
	waitCancel()
	require.Error(t, <-result)
	require.NoError(t, holder.Rollback())
	requireManagedSchedulerRollback(t, f, marker, before)
}

func TestManagedProxySchedulerOutbox_LegacyStatusChangeNotifiesAndMetadataDoesNot(t *testing.T) {
	for _, action := range []string{"pause", "resume", "metadata"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			f := createManagedSchedulerFixture(t, true)
			if action == "resume" {
				f.sub.Status = "inactive"
				require.NoError(t, f.repo.Update(ctx, f.sub))
			}
			captureManagedSchedulerAccountEpochs(t, &f)
			marker := managedSchedulerOutboxMarker(t)
			f.sub.Name = "legacy metadata changed"
			f.sub.Status = "active"
			if action == "pause" {
				f.sub.Status = "inactive"
			}
			require.NoError(t, f.repo.Update(ctx, f.sub))
			changed := action != "metadata"
			requireManagedSchedulerAccountState(t, f, changed)
			requireManagedSchedulerAccountEpochs(t, f, changed)
			var want []int64
			if changed {
				want = f.aliveIDs
			}
			requireManagedSchedulerNotifications(t, marker, want)
		})
	}
}

// Only record token/write completion; all snapshot/cache persistence is real.
// Startup captures its write tasks before publishing any snapshot, so completing
// every captured bucket proves no initial rebuild can mask a later proxy change.
type managedSchedulerStartupCache struct {
	service.SchedulerCache
	mu                sync.Mutex
	captured, written map[service.SchedulerBucket]struct{}
	ready             chan struct{}
	once              sync.Once
}

func (c *managedSchedulerStartupCache) CaptureBucketWriteToken(ctx context.Context, bucket service.SchedulerBucket) (service.SchedulerBucketWriteToken, error) {
	token, err := c.SchedulerCache.CaptureBucketWriteToken(ctx, bucket)
	if err == nil {
		c.mu.Lock()
		c.captured[bucket] = struct{}{}
		c.mu.Unlock()
	}
	return token, err
}

func (c *managedSchedulerStartupCache) SetSnapshot(ctx context.Context, bucket service.SchedulerBucket, token service.SchedulerBucketWriteToken, accounts []service.Account) error {
	if err := c.SchedulerCache.SetSnapshot(ctx, bucket, token, accounts); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written[bucket] = struct{}{}
	if len(c.captured) > 0 && len(c.captured) == len(c.written) {
		c.once.Do(func() { close(c.ready) })
	}
	return nil
}

func TestManagedProxySchedulerOutbox_ReplaysDisableAndReenableWithoutPeriodicRebuild(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			ctx := context.Background()
			f := createManagedSchedulerFixture(t, legacy)
			rdb := testRedis(t)
			cache := &managedSchedulerStartupCache{SchedulerCache: NewSchedulerCache(rdb), captured: make(map[service.SchedulerBucket]struct{}), written: make(map[service.SchedulerBucket]struct{}), ready: make(chan struct{})}
			// Ignore prior tests' events; only this test's proxy changes can refresh
			// an already-built cache after startup. No accounts methods emit events.
			require.NoError(t, cache.SetOutboxWatermark(ctx, managedSchedulerOutboxMarker(t)))
			accountRepo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
			cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{Scheduling: config.GatewaySchedulingConfig{OutboxPollIntervalSeconds: 1, FullRebuildIntervalSeconds: 0, DbFallbackEnabled: false}}}
			snapshot := service.NewSchedulerSnapshotService(cache, NewSchedulerOutboxRepository(integrationDB), accountRepo, nil, cfg)
			snapshot.Start()
			t.Cleanup(snapshot.Stop)
			require.Eventually(t, func() bool {
				select {
				case <-cache.ready:
					return true
				default:
					return false
				}
			}, 10*time.Second, 20*time.Millisecond, "initial rebuild must finish publishing all captured buckets")
			matches := func(status string) bool {
				for _, id := range f.aliveIDs {
					cached, err := cache.GetAccount(ctx, id)
					if err != nil || cached == nil || cached.Proxy == nil || cached.Proxy.Status != status {
						return false
					}
				}
				return true
			}
			require.True(t, matches("active"), "startup must contain actual active proxy objects for APIKey/OAuth accounts")
			for _, status := range []string{"inactive", "active"} {
				if legacy {
					f.sub.Status = status
					require.NoError(t, f.repo.Update(ctx, f.sub))
				} else {
					_, err := f.admin.UpdateProxy(ctx, f.proxyID, &service.UpdateProxyInput{Status: status})
					require.NoError(t, err)
				}
				require.Eventually(t, func() bool { return matches(status) }, 5*time.Second, 50*time.Millisecond, "proxy state %s must reach the real scheduler cache using outbox alone", status)
			}
		})
	}
}

func TestManagedProxySchedulerOutbox_FixtureCleanupPreservesOtherFixtures(t *testing.T) {
	ctx := context.Background()
	kept := newManagedSchedulerFixture(t)
	var owned managedSchedulerFixture
	t.Run("owned fixture", func(t *testing.T) {
		owned = newManagedSchedulerFixture(t)
		_, err := owned.admin.UpdateProxy(ctx, owned.proxyID, &service.UpdateProxyInput{Status: "inactive"})
		require.NoError(t, err)
	})

	var count int
	for _, query := range []string{
		`SELECT count(*) FROM proxy_subscriptions WHERE id=$1`,
		`SELECT count(*) FROM proxies WHERE subscription_id=$1`,
		`SELECT count(*) FROM proxy_subscription_nodes WHERE subscription_id=$1`,
	} {
		require.NoError(t, integrationDB.QueryRowContext(ctx, query, owned.sub.ID).Scan(&count))
		require.Zero(t, count, "completed fixture must not leave managed proxies or subscriptions")
	}
	for id := range owned.insertedAccountExtras {
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE id=$1`, id).Scan(&count))
		require.Zero(t, count, "completed fixture must remove its live, deleted and unrelated accounts")
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM scheduler_outbox WHERE account_id=$1 OR payload->'account_ids' @> jsonb_build_array($1::bigint)`, id).Scan(&count))
		require.Zero(t, count, "completed fixture must not leave scheduler events for its accounts")
	}

	_, err := kept.repo.Get(ctx, kept.sub.ID)
	require.NoError(t, err, "cleanup must preserve another fixture's subscription")
	_, err = kept.repo.GetNodeByProxyID(ctx, kept.proxyID)
	require.NoError(t, err, "cleanup must preserve another fixture's proxy nodes")
	requireManagedSchedulerAccountState(t, kept, false)
}
