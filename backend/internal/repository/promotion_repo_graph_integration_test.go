//go:build integration

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func promotionGraphUsers(t *testing.T, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	r := &promotionRepository{db: integrationDB}
	for i := range ids {
		u := mustCreateUser(t, testEntClient(t), &service.User{Email: "promotion-graph-" + uuid.NewString() + "@example.com"})
		ids[i] = u.ID
		_, err := r.EnsurePromotionUser(context.Background(), u.ID)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM promotion_activations WHERE user_id = ANY($1) OR promoter_user_id = ANY($1)`, pq.Array(ids))
		_, _ = integrationDB.Exec(`DELETE FROM promotion_users WHERE user_id = ANY($1)`, pq.Array(ids))
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id = ANY($1)`, pq.Array(ids))
	})
	return ids
}

// Separate single-connection pools make concurrency tests use distinct PostgreSQL sessions.
func promotionGraphRepo(t *testing.T) *promotionRepository {
	t.Helper()
	cfg, err := loadIntegrationHarnessConfig(os.Getenv)
	require.NoError(t, err)
	connector, err := pq.NewConnector(cfg.dsn)
	require.NoError(t, err)
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return &promotionRepository{db: db}
}

func promotionGraphBind(r *promotionRepository, ctx context.Context, child, parent int64, source string) error {
	_, err := r.SetPromotionParent(ctx, child, &parent, source, "graph test", time.Now())
	return err
}

func TestPromotionGraphConcurrentCycles(t *testing.T) {
	for _, n := range []int{2, 3, 5} {
		t.Run(fmt.Sprintf("%d_sessions", n), func(t *testing.T) {
			ids := promotionGraphUsers(t, n)
			start := make(chan struct{})
			results := make(chan error, n)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for i := range ids {
				r := promotionGraphRepo(t)
				child, parent := ids[i], ids[(i+1)%n]
				go func() {
					<-start
					results <- promotionGraphBind(r, ctx, child, parent, service.PromotionBindingSourceSelf)
				}()
			}
			close(start)
			success, cycles := 0, 0
			for range ids {
				err := <-results
				if err == nil {
					success++
				} else if errors.Is(err, service.ErrPromotionBindCycle) {
					cycles++
				} else {
					t.Errorf("unexpected bind error: %v", err)
				}
			}
			require.Equal(t, n-1, success, "exactly one edge must be rejected to leave an acyclic graph")
			require.Equal(t, 1, cycles)
		})
	}
}

func TestPromotionGraphConcurrentClaim(t *testing.T) {
	ids := promotionGraphUsers(t, 3)
	start := make(chan struct{})
	results := make(chan error, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, parent := range ids[1:] {
		r := promotionGraphRepo(t)
		go func(parent int64) {
			<-start
			results <- promotionGraphBind(r, ctx, ids[0], parent, service.PromotionBindingSourceSelf)
		}(parent)
	}
	close(start)
	success, conflicts := 0, 0
	for range 2 {
		var err error
		select {
		case err = <-results:
		case <-ctx.Done():
			t.Fatalf("concurrent parent claim timed out: %v", ctx.Err())
		}
		if err == nil {
			success++
		} else if errors.Is(err, service.ErrPromotionAlreadyBound) {
			conflicts++
		} else {
			t.Errorf("unexpected claim error: %v", err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflicts)
}

func promotionGraphUsage(t *testing.T, userID int64) {
	t.Helper()
	client := testEntClient(t)
	a := mustCreateAccount(t, client, &service.Account{Name: "promotion-consumption-" + uuid.NewString()})
	k := mustCreateApiKey(t, client, &service.APIKey{UserID: userID, Key: "sk-promotion-" + uuid.NewString(), Name: "k"})
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM usage_logs WHERE api_key_id = $1`, k.ID)
		_, _ = integrationDB.Exec(`DELETE FROM api_keys WHERE id = $1`, k.ID)
		_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id = $1`, a.ID)
	})
	createPromotionUsageLog(t, newUsageLogRepositoryWithSQL(client, integrationDB), userID, k.ID, a.ID, 1, time.Now())
}

func TestPromotionGraphSelfAndAdminSemantics(t *testing.T) {
	ids := promotionGraphUsers(t, 4)
	r := promotionGraphRepo(t)
	ctx := context.Background()
	require.NoError(t, promotionGraphBind(r, ctx, ids[0], ids[1], service.PromotionBindingSourceSelf))
	before, err := r.GetPromotionUserByUserID(ctx, ids[0])
	require.NoError(t, err)
	promotionGraphUsage(t, ids[0])
	after, err := r.SetPromotionParent(ctx, ids[0], &ids[1], service.PromotionBindingSourceSelf, "must not replace note", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, before, after, "same-parent self binding is a strict no-op even after consumption")
	require.ErrorIs(t, promotionGraphBind(r, ctx, ids[0], ids[2], service.PromotionBindingSourceSelf), service.ErrPromotionAlreadyBound)
	after, err = r.SetPromotionParent(ctx, ids[0], &ids[2], service.PromotionBindingSourceAdmin, " new parent ", time.Now())
	require.NoError(t, err)
	require.Equal(t, ids[2], *after.ParentUserID)
	require.Equal(t, "new parent", after.BoundNote)
	after, err = r.SetPromotionParent(ctx, ids[0], &ids[2], service.PromotionBindingSourceAdmin, " updated note ", time.Now())
	require.NoError(t, err)
	require.Equal(t, "updated note", after.BoundNote)
	promotionGraphUsage(t, ids[3])
	require.ErrorIs(t, promotionGraphBind(r, ctx, ids[3], ids[1], service.PromotionBindingSourceSelf), service.ErrPromotionBindAfterConsumption)
	require.NoError(t, promotionGraphBind(r, ctx, ids[3], ids[1], service.PromotionBindingSourceAdmin))
	_, err = r.SetPromotionParent(ctx, ids[0], nil, service.PromotionBindingSourceAdmin, "unbind", time.Now())
	require.NoError(t, err)
	require.NoError(t, r.RemovePromotionDirectDownline(ctx, ids[1], ids[3], "remove"))
	require.ErrorIs(t, r.RemovePromotionDirectDownline(ctx, ids[1], ids[3], "again"), service.ErrPromotionDownlineNotDirect)
}

func promotionGraphLock(t *testing.T) *sql.Tx {
	t.Helper()
	tx, err := integrationDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.Exec(`SELECT pg_advisory_xact_lock($1)`, advisoryLockHash("sub2api:promotion:parent-graph"))
	require.NoError(t, err)
	return tx
}

func promotionGraphWait(t *testing.T, results <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-results:
			t.Fatalf("mutation completed while global graph lock held: %v", err)
		default:
		}
		var count int
		err := integrationDB.QueryRow(`SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event = 'advisory'`).Scan(&count)
		require.NoError(t, err)
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("mutation never waited for graph advisory lock")
}

func TestPromotionGraphRevalidatesAfterLock(t *testing.T) {
	for _, scenario := range []string{"consumption", "parent", "ancestor", "direct_downline"} {
		t.Run(scenario, func(t *testing.T) {
			ids := promotionGraphUsers(t, 3)
			r := promotionGraphRepo(t)
			if scenario == "direct_downline" {
				require.NoError(t, promotionGraphBind(r, context.Background(), ids[0], ids[1], service.PromotionBindingSourceAdmin))
			}
			lock := promotionGraphLock(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			results := make(chan error, 1)
			go func() {
				if scenario == "direct_downline" {
					results <- r.RemovePromotionDirectDownline(ctx, ids[1], ids[0], "remove")
				} else {
					results <- promotionGraphBind(r, ctx, ids[0], ids[1], service.PromotionBindingSourceSelf)
				}
			}()
			promotionGraphWait(t, results)
			want := service.ErrPromotionAlreadyBound
			switch scenario {
			case "consumption":
				promotionGraphUsage(t, ids[0])
				want = service.ErrPromotionBindAfterConsumption
			case "parent", "direct_downline":
				_, err := integrationDB.Exec(`UPDATE promotion_users SET parent_user_id = $2 WHERE user_id = $1`, ids[0], ids[2])
				require.NoError(t, err)
				if scenario == "direct_downline" {
					want = service.ErrPromotionDownlineNotDirect
				}
			case "ancestor":
				_, err := integrationDB.Exec(`UPDATE promotion_users SET parent_user_id = $2 WHERE user_id = $1`, ids[1], ids[0])
				require.NoError(t, err)
				want = service.ErrPromotionBindCycle
			}
			require.NoError(t, lock.Commit())
			require.ErrorIs(t, <-results, want)
		})
	}
}

func TestPromotionGraphLockCancellation(t *testing.T) {
	ids := promotionGraphUsers(t, 2)
	r := promotionGraphRepo(t)
	lock := promotionGraphLock(t)
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan error, 1)
	go func() { results <- promotionGraphBind(r, ctx, ids[0], ids[1], service.PromotionBindingSourceSelf) }()
	promotionGraphWait(t, results)
	cancel()
	select {
	case err := <-results:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("lock cancellation did not return")
	}
	require.NoError(t, lock.Commit())
	u, err := r.GetPromotionUserByUserID(context.Background(), ids[0])
	require.NoError(t, err)
	require.Nil(t, u.ParentUserID)
	require.NoError(t, promotionGraphBind(r, context.Background(), ids[0], ids[1], service.PromotionBindingSourceSelf), "cancelled transaction releases connection and lock")
}

func TestPromotionGraphMissingDirectDownline(t *testing.T) {
	ids := promotionGraphUsers(t, 1)
	r := promotionGraphRepo(t)
	require.ErrorIs(t, r.RemovePromotionDirectDownline(context.Background(), ids[0], 9223372036854775807, "missing"), service.ErrPromotionDownlineNotDirect)
}

func TestPromotionGraphLegacyCycleQueriesAndRepair(t *testing.T) {
	ids := promotionGraphUsers(t, 4)
	r := promotionGraphRepo(t)
	_, err := integrationDB.Exec(`UPDATE promotion_users SET parent_user_id = CASE user_id WHEN $1 THEN $2 WHEN $2 THEN $1 WHEN $3 THEN $1 END WHERE user_id = ANY($4)`, ids[0], ids[1], ids[2], pq.Array(ids[:3]))
	require.NoError(t, err)
	// Both the root and one descendant are active. Only the descendant counts,
	// even though the legacy cycle makes the root reachable from itself.
	_, err = integrationDB.Exec(`
		INSERT INTO promotion_activations (user_id, promoter_user_id, activated_at, threshold_amount, trigger_usage_amount)
		VALUES ($1, $2, NOW(), 5, 6), ($2, $1, NOW(), 5, 6)
	`, ids[0], ids[1])
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	count, err := r.CountPromotionDescendants(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, 2, count)
	count, err = r.CountPromotionActivatedDescendants(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, 1, count, "exclude the activated root, count the activated descendant once")
	counts, err := r.countPromotionDescendantsByUserIDs(ctx, []int64{ids[0], ids[1], ids[0], ids[3]})
	require.NoError(t, err)
	require.Equal(t, 2, counts[ids[0]])
	require.Equal(t, 2, counts[ids[1]])
	require.Zero(t, counts[ids[3]])
	found, err := r.HasPromotionDescendant(ctx, ids[0], ids[3])
	require.NoError(t, err)
	require.False(t, found)
	found, err = r.HasPromotionDescendant(ctx, ids[0], ids[0])
	require.NoError(t, err)
	require.False(t, found, "root itself is never its own descendant")
	for _, admin := range []bool{false, true} {
		var items []service.PromotionTeamItem
		var total int64
		if admin {
			items, total, err = r.ListPromotionDownlines(ctx, ids[0], service.PromotionTeamFilter{PageSize: 100}, time.Now(), time.Now().Add(time.Hour))
		} else {
			items, total, err = r.ListPromotionTeam(ctx, ids[0], service.PromotionTeamFilter{PageSize: 100}, time.Now())
		}
		require.NoError(t, err)
		require.EqualValues(t, 2, total)
		require.Len(t, items, 2)
		for _, item := range items {
			require.NotEqual(t, ids[0], item.UserID)
			require.Equal(t, 1, item.RelationDepth)
		}
	}
	chain, err := r.GetPromotionRelationChain(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, ids[1], chain.Parent.UserID)
	require.Nil(t, chain.Grandparent)
	require.ErrorIs(t, promotionGraphBind(r, ctx, ids[3], ids[0], service.PromotionBindingSourceAdmin), service.ErrPromotionBindCycle, "cannot attach to an already cyclic ancestor chain")
	require.NoError(t, r.RemovePromotionDirectDownline(ctx, ids[1], ids[0], "repair cycle"))
	require.NoError(t, promotionGraphBind(r, ctx, ids[3], ids[0], service.PromotionBindingSourceAdmin))
}

func TestPromotionGraphServiceCanRepairHistoricalCycle(t *testing.T) {
	ids := promotionGraphUsers(t, 3)
	r := promotionGraphRepo(t)
	_, err := integrationDB.Exec(`UPDATE promotion_users SET parent_user_id = CASE user_id WHEN $1 THEN $2 ELSE $1 END WHERE user_id = ANY($3)`, ids[0], ids[1], pq.Array(ids[:2]))
	require.NoError(t, err)
	s := service.NewPromotionService(r, nil, nil, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = s.BindAdminParent(ctx, ids[0], ids[2], "repair by reparenting")
	require.NoError(t, err, "service must not run an unbounded descendant precheck")
	chain, err := r.GetPromotionRelationChain(ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, ids[2], chain.Parent.UserID)
}
