package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxySubscriptionRepositorySyncNodesDeactivatesMissingActiveNode(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := newProxySubscriptionRepositoryWithSQL(db)
	now := time.Now()
	mock.ExpectQuery(`SELECT name FROM proxy_subscriptions WHERE id = \$1`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("managed-sub"))
	mock.ExpectQuery(`(?s)SELECT n\.id, n\.subscription_id, n\.proxy_id.*FROM proxy_subscription_nodes n.*WHERE n\.subscription_id = \$1`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "subscription_id", "proxy_id", "node_key", "name", "provider_name", "type",
			"server", "port", "username", "password", "raw_config", "status", "created_at", "updated_at",
		}).
			AddRow(int64(11), int64(7), int64(21), "kept-node", "Kept", "provider-kept", "ss", "kept.example", 8388, "user", "pass", "raw", service.ProxySubscriptionNodeStatusActive, now, now).
			AddRow(int64(12), int64(7), int64(22), "missing-node", "Missing", "provider-missing", "trojan", "missing.example", 443, "user", "pass", "raw", service.ProxySubscriptionNodeStatusActive, now, now))
	mock.ExpectExec(`(?s)UPDATE proxy_subscription_nodes\s+SET name = \$2.*WHERE id = \$1`).
		WithArgs(int64(11), "Kept-updated", "provider-kept", "ss", "kept.example", 8388, "raw-updated", service.ProxySubscriptionNodeStatusSourceMissing, service.ProxySubscriptionNodeStatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE proxies\s+SET name = \$2.*WHERE id = \$1 AND deleted_at IS NULL`).
		WithArgs(int64(21), "managed-sub / Kept-updated").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE proxy_subscription_nodes\s+SET status = \$2.*WHERE id = \$1 AND status = \$3`).
		WithArgs(int64(12), service.ProxySubscriptionNodeStatusSourceMissing, service.ProxySubscriptionNodeStatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE accounts a`).
		WithArgs(int64(7), int64(21)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)UPDATE proxies p`).
		WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	created, err := repo.syncNodes(context.Background(), db, 7, []service.ProxySubscriptionNode{
		{
			NodeKey:      "kept-node",
			Name:         "Kept-updated",
			ProviderName: "provider-kept",
			Type:         "ss",
			Server:       "kept.example",
			Port:         8388,
			RawConfig:    "raw-updated",
			Status:       service.ProxySubscriptionNodeStatusActive,
		},
	})

	require.NoError(t, err)
	require.Empty(t, created)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxySubscriptionRepositorySyncNodesReappearancePreservesOperatorDisable(t *testing.T) {
	for _, status := range []string{"source_missing", service.ProxySubscriptionNodeStatusInactive} {
		t.Run(status, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := newProxySubscriptionRepositoryWithSQL(db)
			now := time.Now()
			mock.ExpectQuery(`SELECT name FROM proxy_subscriptions WHERE id = \$1`).WithArgs(int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("managed-sub"))
			mock.ExpectQuery(`(?s)SELECT n\.id, n\.subscription_id, n\.proxy_id.*WHERE n\.subscription_id = \$1`).WithArgs(int64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"id", "subscription_id", "proxy_id", "node_key", "name", "provider_name", "type", "server", "port", "username", "password", "raw_config", "status", "created_at", "updated_at"}).
					AddRow(int64(11), int64(7), int64(21), "node", "Node", "provider", "ss", "node.example", 8388, "user", "pass", "raw", status, now, now))
			mock.ExpectExec(`(?s)UPDATE proxy_subscription_nodes\s+SET name = \$2.*status = CASE.*WHERE id = \$1`).
				WithArgs(int64(11), "Returned", "provider", "ss", "node.example", 8388, "updated", "source_missing", service.ProxySubscriptionNodeStatusActive).
				WillReturnResult(sqlmock.NewResult(0, 1))
			if status == "source_missing" {
				mock.ExpectExec(`(?s)UPDATE proxies\s+SET name = \$2`).WithArgs(int64(21), "managed-sub / Returned").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`(?s)UPDATE accounts a`).WithArgs(int64(7), int64(21)).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectExec(`(?s)UPDATE proxies p`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
			created, err := repo.syncNodes(context.Background(), db, 7, []service.ProxySubscriptionNode{{
				NodeKey: "node", Name: "Returned", ProviderName: "provider", Type: "ss", Server: "node.example", Port: 8388, RawConfig: "updated",
			}})
			require.NoError(t, err)
			require.Empty(t, created, "reappearance must keep the proxy ID and account bindings")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
