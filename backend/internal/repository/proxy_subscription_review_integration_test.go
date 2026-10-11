//go:build integration

package repository

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type reviewReloadResolver struct{ reloads []int64 }

func (r *reviewReloadResolver) ResolveProxyURL(context.Context, int64) (string, error) {
	return "", nil
}
func (r *reviewReloadResolver) GetStatus(id int64) service.ManagedProxyRuntimeStatus {
	return service.ManagedProxyRuntimeStatus{Enabled: true, Status: service.ManagedProxyRuntimeStatusRunning, SubscriptionID: id}
}
func (r *reviewReloadResolver) Reload(id int64) { r.reloads = append(r.reloads, id) }

func reviewProxyAdmin(repo *proxyRepository, subs *proxySubscriptionRepository, resolver service.ManagedProxyResolver) service.AdminService {
	return service.NewAdminService(nil, nil, nil, nil, repo, nil, subs, resolver, nil, nil, nil, nil, nil, nil, nil, nil, integrationEntClient, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestProxySubscriptionRepository_OperatorAPIUpdatesNodeAtomically(t *testing.T) {
	ctx := context.Background()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "operator-api", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("operator", "operator.example", 443)})
	require.NoError(t, err)
	proxyRepo := newProxyRepositoryWithSQL(integrationEntClient, integrationDB)
	resolver := &reviewReloadResolver{}
	admin := reviewProxyAdmin(proxyRepo, repo, resolver)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/proxies/:id", adminhandler.NewProxyHandler(admin, nil, nil).Update)
	put := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/proxies/%d", proxies[0].ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,proxy_id,extra) VALUES ('operator-api-account','openai','apikey',$1,'{"upstream_billing_probe":{"status":"ok"},"keep":true}') RETURNING id`, proxies[0].ID).Scan(&accountID))
	put(`{"status":"inactive"}`)
	node, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusInactive, node.Status)
	require.Equal(t, []int64{sub.ID}, resolver.reloads)
	var bound int64
	var probe, keep, outbox bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT proxy_id,extra ? 'upstream_billing_probe',(extra->>'keep')::boolean FROM accounts WHERE id=$1`, accountID).Scan(&bound, &probe, &keep))
	require.Equal(t, proxies[0].ID, bound)
	require.False(t, probe)
	require.True(t, keep)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scheduler_outbox WHERE payload->'account_ids' @> jsonb_build_array($1::bigint))`, accountID).Scan(&outbox))
	require.True(t, outbox)
	put(`{"name":"renamed only"}`)
	node, err = repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusInactive, node.Status, "metadata edits preserve operator state")
	put(`{"status":"active"}`)
	node, err = repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusActive, node.Status)
	require.Len(t, resolver.reloads, 2, "each explicit transition must reload the runtime")
	_, err = integrationDB.ExecContext(ctx, `UPDATE proxy_subscription_nodes SET status='source_missing' WHERE proxy_id=$1`, proxies[0].ID)
	require.NoError(t, err)
	put(`{"name":"still missing","status":"active"}`)
	node, err = repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusSourceMissing, node.Status, "saving active cannot invent an absent provider node")
	require.Equal(t, "user-operator", node.Username)
	require.Equal(t, "pass-operator", node.Password)
}

func TestProxySubscriptionRepository_MetadataWaitingOnSubscriptionPreservesCommittedDisable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "metadata-lock", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("old", "old.example", 443)})
	require.NoError(t, err)
	proxyRepo := newProxyRepositoryWithSQL(integrationEntClient, integrationDB)
	stale, err := proxyRepo.GetByID(ctx, proxies[0].ID)
	require.NoError(t, err)
	stale.Name = "metadata renamed after disable"
	holder, err := openRotationConnection(t).BeginTx(ctx, nil)
	require.NoError(t, err)
	defer holder.Rollback()
	var pid int
	require.NoError(t, holder.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM proxy_subscriptions WHERE id=$1 FOR UPDATE`, sub.ID).Scan(&pid))
	result := make(chan error, 1)
	go func() { result <- proxyRepo.UpdateWithNodeStatus(ctx, stale, nil) }()
	requireRotationWaiter(t, pid)
	require.NoError(t, newProxySubscriptionRepositoryWithSQL(holder).SetNodeStatusByProxyID(ctx, proxies[0].ID, service.ProxySubscriptionNodeStatusInactive))
	_, err = holder.ExecContext(ctx, `UPDATE proxies SET status='inactive' WHERE id=$1`, proxies[0].ID)
	require.NoError(t, err)
	require.NoError(t, holder.Commit())
	require.NoError(t, <-result)
	p, err := proxyRepo.GetByID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, "inactive", p.Status, "a stale metadata edit cannot restore the operator's previous proxy status")
	node, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusInactive, node.Status)
	require.Equal(t, stale.Name, p.Name)
}

func TestProxySubscriptionRepository_CanceledOperatorUpdateRollsBackProxyNodeAndRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "operator-cancel", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("old", "old.example", 443)})
	require.NoError(t, err)
	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,proxy_id,extra) VALUES ('operator-account','openai','apikey',$1,'{"upstream_billing_probe":{"status":"ok"}}') RETURNING id`, proxies[0].ID).Scan(&accountID))
	holder, err := openRotationConnection(t).BeginTx(ctx, nil)
	require.NoError(t, err)
	defer holder.Rollback()
	var pid int
	require.NoError(t, holder.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&pid))
	admin := reviewProxyAdmin(newProxyRepositoryWithSQL(integrationEntClient, integrationDB), repo, &reviewReloadResolver{})
	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()
	result := make(chan error, 1)
	go func() {
		_, err := admin.UpdateProxy(waitCtx, proxies[0].ID, &service.UpdateProxyInput{Status: "inactive"})
		result <- err
	}()
	requireRotationWaiter(t, pid)
	waitCancel()
	require.Error(t, <-result)
	require.NoError(t, holder.Rollback())
	node, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusActive, node.Status)
	got, err := repo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.Revision)
	var status string
	var probe bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM proxies WHERE id=$1`, proxies[0].ID).Scan(&status))
	require.Equal(t, service.StatusActive, status)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT extra ? 'upstream_billing_probe' FROM accounts WHERE id=$1`, accountID).Scan(&probe))
	require.True(t, probe)
}

func TestProxySubscriptionRepository_DisableMissingNodeStaysDisabledOnReturn(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprintf("directSetter=%v", direct), func(t *testing.T) {
			ctx := context.Background()
			repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
			missing := rotationNode("missing", "old.example", 443)
			missing.Status = service.ProxySubscriptionNodeStatusSourceMissing
			sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "disable-missing", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{missing})
			require.NoError(t, err)
			if direct {
				require.NoError(t, repo.SetNodeStatusByProxyID(ctx, proxies[0].ID, service.ProxySubscriptionNodeStatusInactive))
			} else {
				admin := reviewProxyAdmin(newProxyRepositoryWithSQL(integrationEntClient, integrationDB), repo, &reviewReloadResolver{})
				_, err = admin.UpdateProxy(ctx, proxies[0].ID, &service.UpdateProxyInput{Status: "inactive"})
				require.NoError(t, err)
			}
			node, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
			require.NoError(t, err)
			require.Equal(t, service.ProxySubscriptionNodeStatusSourceMissing, node.Status, "operator intent does not invent provider source presence")
			var operatorStatus string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM proxies WHERE id=$1`, proxies[0].ID).Scan(&operatorStatus))
			require.Equal(t, "inactive", operatorStatus)
			admin := reviewProxyAdmin(newProxyRepositoryWithSQL(integrationEntClient, integrationDB), repo, &reviewReloadResolver{})
			_, err = admin.UpdateProxy(ctx, proxies[0].ID, &service.UpdateProxyInput{Status: "active"})
			require.NoError(t, err)
			node, err = repo.GetNodeByProxyID(ctx, proxies[0].ID)
			require.NoError(t, err)
			require.Equal(t, service.ProxySubscriptionNodeStatusSourceMissing, node.Status, "reenabling before source return cannot install an absent node")
			_, err = admin.UpdateProxy(ctx, proxies[0].ID, &service.UpdateProxyInput{Status: "inactive"})
			require.NoError(t, err)
			created, err := repo.SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{rotationNode("return", "new.example", 443)})
			require.NoError(t, err)
			require.Empty(t, created)
			node, err = repo.GetNodeByProxyID(ctx, proxies[0].ID)
			require.NoError(t, err)
			require.Equal(t, service.ProxySubscriptionNodeStatusInactive, node.Status)
			require.Equal(t, "new.example", node.Server)
			require.Equal(t, "user-missing", node.Username)
			got, err := repo.Get(ctx, sub.ID)
			require.NoError(t, err)
			require.Empty(t, got.Nodes, "a returned disabled node cannot be installed into runtime authentication")
		})
	}
}

func TestProxySubscriptionRepository_DisabledNodeDisappearanceCannotBeReenabledBeforeSourceReturn(t *testing.T) {
	ctx := context.Background()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	nodes := []service.ProxySubscriptionNode{rotationNode("disabled", "old.example", 443), rotationNode("kept", "kept.example", 443)}
	nodes[1].Name = "JP-01"
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "disabled-disappears", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, nodes)
	require.NoError(t, err)
	admin := reviewProxyAdmin(newProxyRepositoryWithSQL(integrationEntClient, integrationDB), repo, &reviewReloadResolver{})
	_, err = admin.UpdateProxy(ctx, proxies[0].ID, &service.UpdateProxyInput{Status: "inactive"})
	require.NoError(t, err)
	_, err = repo.SyncNodes(ctx, sub.ID, "", nodes[1:])
	require.NoError(t, err)
	node, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusSourceMissing, node.Status, "source disappearance is tracked even for disabled nodes")
	_, err = admin.UpdateProxy(ctx, proxies[0].ID, &service.UpdateProxyInput{Status: "active"})
	require.NoError(t, err)
	node, err = repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusSourceMissing, node.Status)
	got, err := repo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Len(t, got.Nodes, 1)
	require.Equal(t, "JP-01", got.Nodes[0].Name)
}

func TestProxySubscriptionRepository_SubscriptionUpdatesPreserveNodeOperatorState(t *testing.T) {
	ctx := context.Background()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	nodes := []service.ProxySubscriptionNode{rotationNode("disabled", "disabled.example", 443), rotationNode("active", "active.example", 443)}
	nodes[1].Name = "JP-01"
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "subscription-state", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, nodes)
	require.NoError(t, err)
	admin := reviewProxyAdmin(newProxyRepositoryWithSQL(integrationEntClient, integrationDB), repo, &reviewReloadResolver{})
	_, err = admin.UpdateProxy(ctx, proxies[0].ID, &service.UpdateProxyInput{Status: "inactive"})
	require.NoError(t, err)
	for _, status := range []string{"active", "inactive", "active"} {
		sub.Status, sub.Name = status, "metadata changed"
		require.NoError(t, repo.UpdateWithNodes(ctx, sub, nodes))
		for i, p := range proxies {
			var actual string
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM proxies WHERE id=$1`, p.ID).Scan(&actual))
			want := "active"
			if i == 0 {
				want = "inactive"
			}
			require.Equal(t, want, actual, "subscription metadata/pause/resume cannot overwrite operator intent")
		}
	}
}

func TestProxySubscriptionRepository_HistoricalDisableSurvivesDisappearanceAndReturn(t *testing.T) {
	ctx := context.Background()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	nodes := []service.ProxySubscriptionNode{rotationNode("legacy-disabled", "old.example", 443), rotationNode("kept", "kept.example", 443)}
	nodes[1].Name = "JP-01"
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "legacy-disable", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, nodes)
	require.NoError(t, err)
	// Prior versions stored operator disable on n while leaving p active.
	_, err = integrationDB.ExecContext(ctx, `UPDATE proxy_subscription_nodes SET status='inactive' WHERE proxy_id=$1`, proxies[0].ID)
	require.NoError(t, err)
	_, err = repo.SyncNodes(ctx, sub.ID, "", nodes[1:])
	require.NoError(t, err)
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM proxies WHERE id=$1`, proxies[0].ID).Scan(&status))
	require.Equal(t, "inactive", status, "legacy disable must be retained before replacing presence with source_missing")
	node, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusSourceMissing, node.Status)
	_, err = repo.SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{rotationNode("returned", "new.example", 443), nodes[1]})
	require.NoError(t, err)
	node, err = repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.ProxySubscriptionNodeStatusInactive, node.Status)
}

func TestProxySubscriptionRepository_LegacyAggregateStillFollowsSubscriptionStatus(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "legacy-status", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, nil)
	require.NoError(t, err)
	for _, status := range []string{"inactive", "active"} {
		sub.Status = status
		require.NoError(t, repo.Update(ctx, sub))
		var actual string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT status FROM proxies WHERE id=$1`, proxies[0].ID).Scan(&actual))
		require.Equal(t, status, actual)
	}
}

func TestProxySubscriptionRepository_NetworkEpochAdvancesInsideSingleTransaction(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "epoch-clock", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("old", "old.example", 443)})
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE proxy_subscription_nodes SET updated_at='2100-01-01' WHERE proxy_id=$1`, proxies[0].ID)
	require.NoError(t, err)
	before, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	for _, host := range []string{"second.example", "third.example"} {
		_, err = repo.SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{rotationNode(host, host, 443)})
		require.NoError(t, err)
		after, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
		require.NoError(t, err)
		require.True(t, after.UpdatedAt.After(before.UpdatedAt), "neither transaction NOW() nor wall-clock rollback can reuse an earlier configuration epoch")
		before = after
	}
}

func TestProxySubscriptionRepository_DeleteSerializesBehindRefreshWithoutReverseLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "delete-lock", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("old", "old.example", 443)})
	require.NoError(t, err)
	holder, err := openRotationConnection(t).BeginTx(ctx, nil)
	require.NoError(t, err)
	defer holder.Rollback()
	var pid int
	require.NoError(t, holder.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM proxy_subscriptions WHERE id=$1 FOR UPDATE`, sub.ID).Scan(&pid))
	result := make(chan error, 1)
	waiterDB := openRotationConnection(t)
	go func() { result <- newProxySubscriptionRepositoryWithSQL(waiterDB).DeleteWithProxy(ctx, sub.ID) }()
	requireRotationWaiter(t, pid)
	// The waiting delete must not own a proxy lock needed by this refresh.
	_, err = newProxySubscriptionRepositoryWithSQL(holder).SyncNodes(ctx, sub.ID, "", []service.ProxySubscriptionNode{rotationNode("new", "new.example", 443)})
	require.NoError(t, err, "subscription-first delete must allow the lock holder to finish a refresh")
	require.NoError(t, holder.Commit())
	require.NoError(t, <-result)
	var deleted bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL FROM proxies WHERE id=$1`, proxies[0].ID).Scan(&deleted))
	require.True(t, deleted)
	_, err = repo.Get(ctx, sub.ID)
	require.ErrorIs(t, err, service.ErrProxySubscriptionNotFound)
}

func TestProxySubscriptionRepository_CanceledDeleteDoesNotPartiallyDeleteProxies(t *testing.T) {
	ctx := context.Background()
	repo := newProxySubscriptionRepositoryWithSQL(integrationDB)
	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "delete-cancel", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600}, []service.ProxySubscriptionNode{rotationNode("old", "old.example", 443)})
	require.NoError(t, err)
	holder, err := openRotationConnection(t).BeginTx(ctx, nil)
	require.NoError(t, err)
	defer holder.Rollback()
	var pid int
	require.NoError(t, holder.QueryRowContext(ctx, `SELECT pg_backend_pid() FROM proxy_subscriptions WHERE id=$1 FOR UPDATE`, sub.ID).Scan(&pid))
	waiterDB := openRotationConnection(t)
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- newProxySubscriptionRepositoryWithSQL(waiterDB).DeleteWithProxy(waitCtx, sub.ID) }()
	requireRotationWaiter(t, pid)
	cancel()
	require.Error(t, <-result)
	require.NoError(t, holder.Rollback())
	var deleted bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL FROM proxies WHERE id=$1`, proxies[0].ID).Scan(&deleted))
	require.False(t, deleted)
	_, err = repo.Get(ctx, sub.ID)
	require.NoError(t, err)
}

func TestProxySubscriptionRepository_DNSOnlyChangeInvalidatesAllNodeEvidence(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprintf("UpdateWithNodes=%v", update), func(t *testing.T) {
			ctx := context.Background()
			tx := testTx(t)
			repo := newProxySubscriptionRepositoryWithSQL(tx)
			nodes := []service.ProxySubscriptionNode{rotationNode("one", "one.example", 443), rotationNode("two", "two.example", 443)}
			nodes[1].Name = "JP-01"
			sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{Name: "dns-only", SubscriptionURL: "https://example.com/sub", RefreshIntervalSec: 3600, RawDNSConfig: "nameserver: [1.1.1.1]"}, nodes)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, `UPDATE proxy_subscription_nodes SET updated_at='2000-01-01' WHERE subscription_id=$1`, sub.ID)
			require.NoError(t, err)
			ids := make([]int64, len(proxies))
			for i, p := range proxies {
				require.NoError(t, scanSingleRow(ctx, tx, `INSERT INTO accounts (name,platform,type,proxy_id,extra) VALUES ('dns-account','openai','apikey',$1,'{"upstream_billing_probe":{"status":"ok"},"keep":true}') RETURNING id`, []any{p.ID}, &ids[i]))
			}
			if update {
				sub.RawDNSConfig = "nameserver: [8.8.8.8]"
				require.NoError(t, repo.UpdateWithNodes(ctx, sub, nodes))
			} else {
				_, err = repo.SyncNodes(ctx, sub.ID, "nameserver: [8.8.8.8]", nodes)
				require.NoError(t, err)
			}
			for i, p := range proxies {
				node, err := repo.GetNodeByProxyID(ctx, p.ID)
				require.NoError(t, err)
				require.Greater(t, node.UpdatedAt.Year(), 2000, "DNS config belongs to every node's measurement epoch")
				var probe, keep, outbox bool
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra ? 'upstream_billing_probe', (extra->>'keep')::boolean FROM accounts WHERE id=$1`, ids[i]).Scan(&probe, &keep))
				require.False(t, probe)
				require.True(t, keep)
				require.NoError(t, tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scheduler_outbox WHERE payload->'account_ids' @> jsonb_build_array($1::bigint))`, ids[i]).Scan(&outbox))
				require.True(t, outbox)
				before := node.UpdatedAt
				if update {
					require.NoError(t, repo.UpdateWithNodes(ctx, sub, nodes))
				} else {
					_, err = repo.SyncNodes(ctx, sub.ID, "nameserver: [8.8.8.8]", nodes)
					require.NoError(t, err)
				}
				node, err = repo.GetNodeByProxyID(ctx, p.ID)
				require.NoError(t, err)
				require.Equal(t, before, node.UpdatedAt, "a true no-op keeps the measurement epoch")
			}
		})
	}
}
