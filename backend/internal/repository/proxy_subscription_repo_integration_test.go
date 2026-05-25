//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxySubscriptionRepository_CreateWithNodesAndDisableNode(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)

	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{
		Name:               "managed-sub",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
	}, []service.ProxySubscriptionNode{
		{
			NodeKey:      "node-key-1",
			Name:         "HK-01",
			ProviderName: "node-hk01",
			Type:         "ss",
			Server:       "203.0.113.10",
			Port:         8388,
			Username:     "mpu_hk",
			Password:     "mpp_hk",
			RawConfig:    "name: HK-01\ntype: ss\nserver: 203.0.113.10\nport: 8388\npassword: remote-secret\n",
			Status:       service.ProxySubscriptionNodeStatusActive,
		},
		{
			NodeKey:      "node-key-2",
			Name:         "JP-01",
			ProviderName: "node-jp01",
			Type:         "trojan",
			Server:       "203.0.113.20",
			Port:         443,
			Username:     "mpu_jp",
			Password:     "mpp_jp",
			RawConfig:    "name: JP-01\ntype: trojan\nserver: 203.0.113.20\nport: 443\npassword: remote-secret\n",
			Status:       service.ProxySubscriptionNodeStatusActive,
		},
	})
	require.NoError(t, err)
	require.Len(t, proxies, 2)
	require.NotNil(t, sub.ProxyID)
	require.Len(t, sub.ProxyIDs, 2)
	require.Len(t, sub.Nodes, 2)
	require.Equal(t, "mpu_hk", proxies[0].Username)
	require.Equal(t, service.ProxySourceMihomoSubscription, proxies[0].SourceType)

	got, err := repo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Len(t, got.Nodes, 2)

	node, err := repo.GetNodeByProxyID(ctx, proxies[0].ID)
	require.NoError(t, err)
	require.Equal(t, "HK-01", node.Name)

	require.NoError(t, repo.SetNodeStatusByProxyID(ctx, proxies[0].ID, service.ProxySubscriptionNodeStatusInactive))
	got, err = repo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Len(t, got.Nodes, 1)
	require.Equal(t, "JP-01", got.Nodes[0].Name)
}

func TestProxySubscriptionRepository_SyncNodesDoesNotRecreateDisabledNode(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)

	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{
		Name:               "managed-sub-sync",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
	}, []service.ProxySubscriptionNode{
		{
			NodeKey:      "same-node",
			Name:         "HK-01",
			ProviderName: "node-hk01",
			Type:         "ss",
			Server:       "203.0.113.10",
			Port:         8388,
			Username:     "mpu_hk",
			Password:     "mpp_hk",
			RawConfig:    "name: HK-01\ntype: ss\nserver: 203.0.113.10\nport: 8388\npassword: remote-secret\n",
			Status:       service.ProxySubscriptionNodeStatusActive,
		},
	})
	require.NoError(t, err)
	require.Len(t, proxies, 1)

	require.NoError(t, repo.SetNodeStatusByProxyID(ctx, proxies[0].ID, service.ProxySubscriptionNodeStatusInactive))
	created, err := repo.SyncNodes(ctx, sub.ID, []service.ProxySubscriptionNode{
		{
			NodeKey:      "same-node",
			Name:         "HK-01-renamed",
			ProviderName: "node-hk01",
			Type:         "ss",
			Server:       "203.0.113.10",
			Port:         8388,
			Username:     "new-user",
			Password:     "new-pass",
			RawConfig:    "name: HK-01-renamed\ntype: ss\nserver: 203.0.113.10\nport: 8388\npassword: remote-secret\n",
			Status:       service.ProxySubscriptionNodeStatusActive,
		},
	})
	require.NoError(t, err)
	require.Empty(t, created)

	got, err := repo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Empty(t, got.Nodes, "disabled nodes should not become active again on refresh")
}

func TestProxySubscriptionRepository_SyncNodesDeletesUnusedLegacyManagedProxyRow(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)

	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{
		Name:               "legacy-managed-sub",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
	}, nil)
	require.NoError(t, err)
	require.Len(t, proxies, 1)
	legacyProxyID := proxies[0].ID

	created, err := repo.SyncNodes(ctx, sub.ID, []service.ProxySubscriptionNode{
		{
			NodeKey:      "node-key-1",
			Name:         "US-01",
			ProviderName: "node-us01",
			Type:         "socks5",
			Server:       "203.0.113.30",
			Port:         1080,
			Username:     "mpu_us",
			Password:     "mpp_us",
			RawConfig:    "name: US-01\ntype: socks5\nserver: 203.0.113.30\nport: 1080\n",
			Status:       service.ProxySubscriptionNodeStatusActive,
		},
	})
	require.NoError(t, err)
	require.Len(t, created, 1)

	proxyIDs, err := repo.ListProxyIDsBySubscriptionID(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{created[0].ID}, proxyIDs)

	var legacyDeleted bool
	require.NoError(t, scanSingleRow(ctx, tx, "SELECT deleted_at IS NOT NULL FROM proxies WHERE id = $1", []any{legacyProxyID}, &legacyDeleted))
	require.True(t, legacyDeleted)
}

func TestProxySubscriptionRepository_SyncNodesMigratesAccountsFromLegacyManagedProxyRow(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := newProxySubscriptionRepositoryWithSQL(tx)

	sub, proxies, err := repo.CreateWithNodes(ctx, &service.ProxySubscription{
		Name:               "legacy-managed-sub-in-use",
		SubscriptionURL:    "https://example.com/clash.yaml",
		RefreshIntervalSec: 3600,
		TestURL:            "https://example.com/health",
	}, nil)
	require.NoError(t, err)
	require.Len(t, proxies, 1)
	legacyProxyID := proxies[0].ID
	var accountID int64
	require.NoError(t, scanSingleRow(
		ctx,
		tx,
		"INSERT INTO accounts (name, platform, type, proxy_id) VALUES ($1, $2, $3, $4) RETURNING id",
		[]any{"account-with-legacy-proxy", service.PlatformAnthropic, service.AccountTypeOAuth, legacyProxyID},
		&accountID,
	))

	created, err := repo.SyncNodes(ctx, sub.ID, []service.ProxySubscriptionNode{
		{
			NodeKey:      "node-key-1",
			Name:         "SG-01",
			ProviderName: "node-sg01",
			Type:         "socks5",
			Server:       "203.0.113.40",
			Port:         1080,
			Username:     "mpu_sg",
			Password:     "mpp_sg",
			RawConfig:    "name: SG-01\ntype: socks5\nserver: 203.0.113.40\nport: 1080\n",
			Status:       service.ProxySubscriptionNodeStatusActive,
		},
	})
	require.NoError(t, err)
	require.Len(t, created, 1)

	var accountProxyID int64
	require.NoError(t, scanSingleRow(ctx, tx, "SELECT proxy_id FROM accounts WHERE id = $1", []any{accountID}, &accountProxyID))
	require.Equal(t, created[0].ID, accountProxyID)

	proxyIDs, err := repo.ListProxyIDsBySubscriptionID(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{created[0].ID}, proxyIDs)

	var legacyDeleted bool
	require.NoError(t, scanSingleRow(ctx, tx, "SELECT deleted_at IS NOT NULL FROM proxies WHERE id = $1", []any{legacyProxyID}, &legacyDeleted))
	require.True(t, legacyDeleted)
}
