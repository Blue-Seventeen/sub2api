package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type proxySubscriptionRepository struct {
	sql sqlExecutor
}

func NewProxySubscriptionRepository(sqlDB *sql.DB) service.ProxySubscriptionRepository {
	return newProxySubscriptionRepositoryWithSQL(sqlDB)
}

func newProxySubscriptionRepositoryWithSQL(sqlq sqlExecutor) *proxySubscriptionRepository {
	return &proxySubscriptionRepository{sql: sqlq}
}

func (r *proxySubscriptionRepository) withTransaction(ctx context.Context, fn func(sqlExecutor) error) error {
	if r == nil || r.sql == nil {
		return errors.New("proxy subscription repository unavailable")
	}
	txStarter, ok := r.sql.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return fn(r.sql)
	}
	dbtx, err := txStarter.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = dbtx.Rollback()
		}
	}()
	if err := fn(dbtx); err != nil {
		return err
	}
	if err := dbtx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *proxySubscriptionRepository) CreateWithNodes(ctx context.Context, sub *service.ProxySubscription, nodes []service.ProxySubscriptionNode) (*service.ProxySubscription, []service.Proxy, error) {
	if r == nil || r.sql == nil {
		return nil, nil, errors.New("proxy subscription repository unavailable")
	}
	now := time.Now()
	var created service.ProxySubscription
	var proxies []service.Proxy
	err := r.withTransaction(ctx, func(q sqlExecutor) error {
		if err := scanSingleRow(ctx, q, `
		INSERT INTO proxy_subscriptions (
			name, subscription_url, status, refresh_interval_sec, test_url, revision, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 1, $6, $6)
		RETURNING id, name, subscription_url, status, refresh_interval_sec, test_url, revision, COALESCE(last_error, ''), created_at, updated_at
	`, []any{sub.Name, sub.SubscriptionURL, service.StatusActive, sub.RefreshIntervalSec, sub.TestURL, now},
			&created.ID,
			&created.Name,
			&created.SubscriptionURL,
			&created.Status,
			&created.RefreshIntervalSec,
			&created.TestURL,
			&created.Revision,
			&created.LastError,
			&created.CreatedAt,
			&created.UpdatedAt,
		); err != nil {
			return err
		}

		proxies = make([]service.Proxy, 0, maxInt(1, len(nodes)))
		if len(nodes) == 0 {
			proxyOut, err := r.createManagedProxyRow(ctx, q, created.ID, created.Name, "", "", now)
			if err != nil {
				return err
			}
			proxies = append(proxies, proxyOut)
		} else {
			created.Nodes = make([]service.ProxySubscriptionNode, 0, len(nodes))
			for _, node := range nodes {
				node.SubscriptionID = created.ID
				if node.Status == "" {
					node.Status = service.ProxySubscriptionNodeStatusActive
				}
				proxyOut, err := r.createManagedProxyRow(ctx, q, created.ID, managedProxyNodeProxyName(created.Name, node.Name), node.Username, node.Password, now)
				if err != nil {
					return err
				}
				node.ProxyID = &proxyOut.ID
				createdNode, err := r.insertNode(ctx, q, node, now)
				if err != nil {
					return err
				}
				created.Nodes = append(created.Nodes, createdNode)
				proxies = append(proxies, proxyOut)
			}
		}

		if len(proxies) > 0 {
			firstID := proxies[0].ID
			created.ProxyID = &firstID
			created.ProxyIDs = make([]int64, 0, len(proxies))
			for i := range proxies {
				created.ProxyIDs = append(created.ProxyIDs, proxies[i].ID)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &created, proxies, nil
}

func (r *proxySubscriptionRepository) createManagedProxyRow(ctx context.Context, q sqlExecutor, subscriptionID int64, proxyName, username, password string, now time.Time) (service.Proxy, error) {
	proxyOut := service.Proxy{
		Name:           managedProxyName(proxyName),
		Protocol:       "socks5h",
		Host:           "managed.local",
		Port:           1,
		Username:       username,
		Password:       password,
		Status:         service.StatusActive,
		SourceType:     service.ProxySourceMihomoSubscription,
		SubscriptionID: &subscriptionID,
	}
	if err := scanSingleRow(ctx, q, `
		INSERT INTO proxies (
			name, protocol, host, port, username, password, status, source_type, subscription_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10, $10)
		RETURNING id, name, protocol, host, port, COALESCE(username, ''), COALESCE(password, ''), status, source_type, subscription_id, created_at, updated_at
	`, []any{proxyOut.Name, proxyOut.Protocol, proxyOut.Host, proxyOut.Port, proxyOut.Username, proxyOut.Password, proxyOut.Status, proxyOut.SourceType, subscriptionID, now},
		&proxyOut.ID,
		&proxyOut.Name,
		&proxyOut.Protocol,
		&proxyOut.Host,
		&proxyOut.Port,
		&proxyOut.Username,
		&proxyOut.Password,
		&proxyOut.Status,
		&proxyOut.SourceType,
		&proxyOut.SubscriptionID,
		&proxyOut.CreatedAt,
		&proxyOut.UpdatedAt,
	); err != nil {
		return service.Proxy{}, err
	}
	return proxyOut, nil
}

func (r *proxySubscriptionRepository) insertNode(ctx context.Context, q sqlExecutor, node service.ProxySubscriptionNode, now time.Time) (service.ProxySubscriptionNode, error) {
	var created service.ProxySubscriptionNode
	var proxyIDNull sql.NullInt64
	var proxyIDArg any
	if node.ProxyID != nil {
		proxyIDArg = *node.ProxyID
	}
	if err := scanSingleRow(ctx, q, `
		INSERT INTO proxy_subscription_nodes (
			subscription_id, proxy_id, node_key, name, provider_name, type, server, port,
			username, password, raw_config, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, 0), $9, $10, $11, $12, $13, $13)
		RETURNING id, subscription_id, proxy_id, node_key, name, provider_name, type,
			COALESCE(server, ''), COALESCE(port, 0), username, password, raw_config, status, created_at, updated_at
	`, []any{
		node.SubscriptionID,
		proxyIDArg,
		node.NodeKey,
		node.Name,
		node.ProviderName,
		node.Type,
		node.Server,
		node.Port,
		node.Username,
		node.Password,
		node.RawConfig,
		node.Status,
		now,
	}, &created.ID,
		&created.SubscriptionID,
		&proxyIDNull,
		&created.NodeKey,
		&created.Name,
		&created.ProviderName,
		&created.Type,
		&created.Server,
		&created.Port,
		&created.Username,
		&created.Password,
		&created.RawConfig,
		&created.Status,
		&created.CreatedAt,
		&created.UpdatedAt,
	); err != nil {
		return service.ProxySubscriptionNode{}, err
	}
	if proxyIDNull.Valid {
		v := proxyIDNull.Int64
		created.ProxyID = &v
	}
	return created, nil
}

func (r *proxySubscriptionRepository) List(ctx context.Context) ([]service.ProxySubscription, error) {
	if r == nil || r.sql == nil {
		return []service.ProxySubscription{}, nil
	}
	return r.list(ctx, "")
}

func (r *proxySubscriptionRepository) ListActive(ctx context.Context) ([]service.ProxySubscription, error) {
	if r == nil || r.sql == nil {
		return []service.ProxySubscription{}, nil
	}
	return r.list(ctx, "WHERE ps.status = 'active'")
}

func (r *proxySubscriptionRepository) list(ctx context.Context, where string) ([]service.ProxySubscription, error) {
	query := `
		SELECT
			ps.id, ps.name, ps.subscription_url, ps.status, ps.refresh_interval_sec, ps.test_url,
			ps.revision, COALESCE(ps.last_error, ''), ps.created_at, ps.updated_at,
			(
				SELECT p.id
				FROM proxies p
				WHERE p.subscription_id = ps.id
					AND p.source_type = 'mihomo_subscription'
					AND p.deleted_at IS NULL
				ORDER BY p.id ASC
				LIMIT 1
			) AS proxy_id
		FROM proxy_subscriptions ps
		` + where + `
		ORDER BY ps.id DESC
	`
	rows, err := r.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}

	out := make([]service.ProxySubscription, 0)
	for rows.Next() {
		sub, err := scanProxySubscriptionRow(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := r.attachNodes(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *proxySubscriptionRepository) Get(ctx context.Context, id int64) (*service.ProxySubscription, error) {
	if id <= 0 {
		return nil, service.ErrProxySubscriptionNotFound
	}
	return r.getWithWhere(ctx, "ps.id = $1", id)
}

func (r *proxySubscriptionRepository) GetByProxyID(ctx context.Context, proxyID int64) (*service.ProxySubscription, error) {
	if proxyID <= 0 {
		return nil, service.ErrProxySubscriptionNotFound
	}
	return r.getWithWhere(ctx, `EXISTS (
		SELECT 1 FROM proxies p
		WHERE p.id = $1
			AND p.subscription_id = ps.id
			AND p.source_type = 'mihomo_subscription'
			AND p.deleted_at IS NULL
	)`, proxyID)
}

func (r *proxySubscriptionRepository) getWithWhere(ctx context.Context, where string, arg any) (*service.ProxySubscription, error) {
	if r == nil || r.sql == nil {
		return nil, service.ErrProxySubscriptionNotFound
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT
			ps.id, ps.name, ps.subscription_url, ps.status, ps.refresh_interval_sec, ps.test_url,
			ps.revision, COALESCE(ps.last_error, ''), ps.created_at, ps.updated_at,
			(
				SELECT p.id
				FROM proxies p
				WHERE p.subscription_id = ps.id
					AND p.source_type = 'mihomo_subscription'
					AND p.deleted_at IS NULL
				ORDER BY p.id ASC
				LIMIT 1
			) AS proxy_id
		FROM proxy_subscriptions ps
		WHERE `+where+`
		LIMIT 1
	`, arg)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		_ = rows.Close()
		return nil, service.ErrProxySubscriptionNotFound
	}
	sub, err := scanProxySubscriptionRow(rows)
	if err != nil {
		_ = rows.Close()
		return nil, err
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	subs := []service.ProxySubscription{sub}
	if err := r.attachNodes(ctx, subs); err != nil {
		return nil, err
	}
	return &subs[0], nil
}

func (r *proxySubscriptionRepository) Update(ctx context.Context, sub *service.ProxySubscription) error {
	if r == nil || r.sql == nil || sub == nil || sub.ID <= 0 {
		return service.ErrProxySubscriptionNotFound
	}
	res, err := r.sql.ExecContext(ctx, `
		UPDATE proxy_subscriptions
		SET
			name = $2,
			subscription_url = $3,
			status = $4,
			refresh_interval_sec = $5,
			test_url = $6,
			revision = revision + 1,
			updated_at = NOW()
		WHERE id = $1
	`, sub.ID, sub.Name, sub.SubscriptionURL, sub.Status, sub.RefreshIntervalSec, sub.TestURL)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return service.ErrProxySubscriptionNotFound
	}
	if _, err := r.sql.ExecContext(ctx, `
		UPDATE proxies
		SET status = $2, updated_at = NOW()
		WHERE subscription_id = $1 AND source_type = 'mihomo_subscription' AND deleted_at IS NULL
	`, sub.ID, sub.Status); err != nil {
		return err
	}
	return nil
}

func (r *proxySubscriptionRepository) DeleteWithProxy(ctx context.Context, id int64) error {
	if r == nil || r.sql == nil || id <= 0 {
		return service.ErrProxySubscriptionNotFound
	}
	return r.withTransaction(ctx, func(q sqlExecutor) error {
		if _, err := q.ExecContext(ctx, `
		UPDATE proxies
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE subscription_id = $1 AND source_type = 'mihomo_subscription' AND deleted_at IS NULL
	`, id); err != nil {
			return err
		}
		res, err := q.ExecContext(ctx, `DELETE FROM proxy_subscriptions WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			return service.ErrProxySubscriptionNotFound
		}
		return nil
	})
}

func (r *proxySubscriptionRepository) IncrementRevision(ctx context.Context, id int64) (*service.ProxySubscription, error) {
	if r == nil || r.sql == nil || id <= 0 {
		return nil, service.ErrProxySubscriptionNotFound
	}
	res, err := r.sql.ExecContext(ctx, `
		UPDATE proxy_subscriptions
		SET revision = revision + 1, last_error = NULL, updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, service.ErrProxySubscriptionNotFound
	}
	return r.Get(ctx, id)
}

func (r *proxySubscriptionRepository) SyncNodes(ctx context.Context, subscriptionID int64, nodes []service.ProxySubscriptionNode) ([]service.Proxy, error) {
	if r == nil || r.sql == nil || subscriptionID <= 0 {
		return nil, service.ErrProxySubscriptionNotFound
	}
	if len(nodes) == 0 {
		return []service.Proxy{}, nil
	}
	var subscriptionName string
	createdProxies := make([]service.Proxy, 0)
	err := r.withTransaction(ctx, func(q sqlExecutor) error {
		if err := scanSingleRow(ctx, q, `SELECT name FROM proxy_subscriptions WHERE id = $1`, []any{subscriptionID}, &subscriptionName); err != nil {
			return err
		}
		existing, err := r.listAllNodesForSubscription(ctx, q, subscriptionID)
		if err != nil {
			return err
		}
		existingByKey := make(map[string]service.ProxySubscriptionNode, len(existing))
		for _, node := range existing {
			existingByKey[node.NodeKey] = node
		}

		now := time.Now()
		var firstActiveNodeProxyID *int64
		for _, node := range nodes {
			node.SubscriptionID = subscriptionID
			if node.Status == "" {
				node.Status = service.ProxySubscriptionNodeStatusActive
			}
			if current, ok := existingByKey[node.NodeKey]; ok {
				if firstActiveNodeProxyID == nil && current.ProxyID != nil && current.Status == service.ProxySubscriptionNodeStatusActive {
					id := *current.ProxyID
					firstActiveNodeProxyID = &id
				}
				if _, err := q.ExecContext(ctx, `
				UPDATE proxy_subscription_nodes
				SET name = $2,
					provider_name = $3,
					type = $4,
					server = NULLIF($5, ''),
					port = NULLIF($6, 0),
					raw_config = $7,
					updated_at = NOW()
				WHERE id = $1
			`, current.ID, node.Name, node.ProviderName, node.Type, node.Server, node.Port, node.RawConfig); err != nil {
					return err
				}
				if current.ProxyID != nil && current.Status == service.ProxySubscriptionNodeStatusActive {
					_, err := q.ExecContext(ctx, `
					UPDATE proxies
					SET name = $2, updated_at = NOW()
					WHERE id = $1 AND deleted_at IS NULL
				`, *current.ProxyID, managedProxyNodeProxyName(subscriptionName, node.Name))
					if err != nil {
						return err
					}
				}
				continue
			}

			proxyOut, err := r.createManagedProxyRow(ctx, q, subscriptionID, managedProxyNodeProxyName(subscriptionName, node.Name), node.Username, node.Password, now)
			if err != nil {
				return err
			}
			node.ProxyID = &proxyOut.ID
			if _, err := r.insertNode(ctx, q, node, now); err != nil {
				return err
			}
			if firstActiveNodeProxyID == nil {
				id := proxyOut.ID
				firstActiveNodeProxyID = &id
			}
			createdProxies = append(createdProxies, proxyOut)
		}
		if err := r.migrateLegacyManagedProxyRows(ctx, q, subscriptionID, firstActiveNodeProxyID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return createdProxies, nil
}

func (r *proxySubscriptionRepository) migrateLegacyManagedProxyRows(ctx context.Context, q sqlExecutor, subscriptionID int64, targetProxyID *int64) error {
	if targetProxyID != nil {
		if _, err := q.ExecContext(ctx, `
			UPDATE accounts a
			SET proxy_id = $2, updated_at = NOW()
			WHERE a.proxy_id IN (
				SELECT p.id
				FROM proxies p
				WHERE p.subscription_id = $1
					AND p.source_type = 'mihomo_subscription'
					AND p.deleted_at IS NULL
					AND NOT EXISTS (
						SELECT 1
						FROM proxy_subscription_nodes n
						WHERE n.proxy_id = p.id
					)
			)
				AND a.deleted_at IS NULL
		`, subscriptionID, *targetProxyID); err != nil {
			return err
		}
	}
	_, err := q.ExecContext(ctx, `
		UPDATE proxies p
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE p.subscription_id = $1
			AND p.source_type = 'mihomo_subscription'
			AND p.deleted_at IS NULL
			AND NOT EXISTS (
				SELECT 1
				FROM proxy_subscription_nodes n
				WHERE n.proxy_id = p.id
			)
			AND NOT EXISTS (
				SELECT 1
				FROM accounts a
				WHERE a.proxy_id = p.id
					AND a.deleted_at IS NULL
			)
	`, subscriptionID)
	return err
}

func (r *proxySubscriptionRepository) GetNodeByProxyID(ctx context.Context, proxyID int64) (*service.ProxySubscriptionNode, error) {
	if r == nil || r.sql == nil || proxyID <= 0 {
		return nil, service.ErrProxySubscriptionNotFound
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT n.id, n.subscription_id, n.proxy_id, n.node_key, n.name, n.provider_name, n.type,
			COALESCE(n.server, ''), COALESCE(n.port, 0), n.username, n.password, n.raw_config,
			n.status, n.created_at, n.updated_at
		FROM proxy_subscription_nodes n
		WHERE n.proxy_id = $1
		LIMIT 1
	`, proxyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, service.ErrProxySubscriptionNotFound
	}
	node, err := scanProxySubscriptionNodeRow(rows)
	if err != nil {
		return nil, err
	}
	return &node, rows.Err()
}

func (r *proxySubscriptionRepository) SetNodeStatusByProxyID(ctx context.Context, proxyID int64, status string) error {
	if r == nil || r.sql == nil || proxyID <= 0 {
		return service.ErrProxySubscriptionNotFound
	}
	if status == "" {
		status = service.ProxySubscriptionNodeStatusInactive
	}
	res, err := r.sql.ExecContext(ctx, `
		UPDATE proxy_subscription_nodes
		SET status = $2, updated_at = NOW()
		WHERE proxy_id = $1
	`, proxyID, status)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return service.ErrProxySubscriptionNotFound
	}
	return nil
}

func (r *proxySubscriptionRepository) ListProxyIDsBySubscriptionID(ctx context.Context, subscriptionID int64) ([]int64, error) {
	if r == nil || r.sql == nil || subscriptionID <= 0 {
		return []int64{}, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id
		FROM proxies
		WHERE subscription_id = $1 AND source_type = 'mihomo_subscription' AND deleted_at IS NULL
		ORDER BY id
	`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *proxySubscriptionRepository) SetLastError(ctx context.Context, id int64, message string) error {
	if r == nil || r.sql == nil || id <= 0 {
		return nil
	}
	if len(message) > 2000 {
		message = message[:2000]
	}
	var value any
	if strings.TrimSpace(message) != "" {
		value = message
	}
	_, err := r.sql.ExecContext(ctx, `
		UPDATE proxy_subscriptions
		SET last_error = $2, updated_at = NOW()
		WHERE id = $1
	`, id, value)
	return err
}

func (r *proxySubscriptionRepository) attachNodes(ctx context.Context, subs []service.ProxySubscription) error {
	if len(subs) == 0 {
		return nil
	}
	for i := range subs {
		nodes, err := r.listNodesForSubscription(ctx, r.sql, subs[i].ID)
		if err != nil {
			return err
		}
		subs[i].Nodes = nodes
		subs[i].ProxyIDs = make([]int64, 0, len(nodes))
		for _, node := range nodes {
			if node.ProxyID != nil {
				subs[i].ProxyIDs = append(subs[i].ProxyIDs, *node.ProxyID)
			}
		}
		if subs[i].ProxyID == nil && len(subs[i].ProxyIDs) > 0 {
			id := subs[i].ProxyIDs[0]
			subs[i].ProxyID = &id
		}
	}
	return nil
}

func (r *proxySubscriptionRepository) listNodesForSubscription(ctx context.Context, q sqlExecutor, subscriptionID int64) ([]service.ProxySubscriptionNode, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT n.id, n.subscription_id, n.proxy_id, n.node_key, n.name, n.provider_name, n.type,
			COALESCE(n.server, ''), COALESCE(n.port, 0), n.username, n.password, n.raw_config,
			n.status, n.created_at, n.updated_at
		FROM proxy_subscription_nodes n
		JOIN proxies p ON p.id = n.proxy_id AND p.deleted_at IS NULL
		WHERE n.subscription_id = $1
			AND n.status = 'active'
		ORDER BY n.id ASC
	`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	nodes := make([]service.ProxySubscriptionNode, 0)
	for rows.Next() {
		node, err := scanProxySubscriptionNodeRow(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (r *proxySubscriptionRepository) listAllNodesForSubscription(ctx context.Context, q sqlExecutor, subscriptionID int64) ([]service.ProxySubscriptionNode, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT n.id, n.subscription_id, n.proxy_id, n.node_key, n.name, n.provider_name, n.type,
			COALESCE(n.server, ''), COALESCE(n.port, 0), n.username, n.password, n.raw_config,
			n.status, n.created_at, n.updated_at
		FROM proxy_subscription_nodes n
		WHERE n.subscription_id = $1
		ORDER BY n.id ASC
	`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	nodes := make([]service.ProxySubscriptionNode, 0)
	for rows.Next() {
		node, err := scanProxySubscriptionNodeRow(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func scanProxySubscriptionRow(scanner interface {
	Scan(dest ...any) error
}) (service.ProxySubscription, error) {
	var sub service.ProxySubscription
	var proxyID sql.NullInt64
	if err := scanner.Scan(
		&sub.ID,
		&sub.Name,
		&sub.SubscriptionURL,
		&sub.Status,
		&sub.RefreshIntervalSec,
		&sub.TestURL,
		&sub.Revision,
		&sub.LastError,
		&sub.CreatedAt,
		&sub.UpdatedAt,
		&proxyID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sub, service.ErrProxySubscriptionNotFound
		}
		return sub, err
	}
	if proxyID.Valid {
		v := proxyID.Int64
		sub.ProxyID = &v
	}
	return sub, nil
}

func scanProxySubscriptionNodeRow(scanner interface {
	Scan(dest ...any) error
}) (service.ProxySubscriptionNode, error) {
	var node service.ProxySubscriptionNode
	var proxyID sql.NullInt64
	if err := scanner.Scan(
		&node.ID,
		&node.SubscriptionID,
		&proxyID,
		&node.NodeKey,
		&node.Name,
		&node.ProviderName,
		&node.Type,
		&node.Server,
		&node.Port,
		&node.Username,
		&node.Password,
		&node.RawConfig,
		&node.Status,
		&node.CreatedAt,
		&node.UpdatedAt,
	); err != nil {
		return node, err
	}
	if proxyID.Valid {
		v := proxyID.Int64
		node.ProxyID = &v
	}
	return node, nil
}

func managedProxyNodeProxyName(subscriptionName, nodeName string) string {
	name := strings.TrimSpace(nodeName)
	if sub := strings.TrimSpace(subscriptionName); sub != "" {
		name = sub + " / " + name
	}
	return managedProxyName(name)
}

func managedProxyName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "managed proxy"
	}
	runes := []rune(value)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ service.ProxySubscriptionRepository = (*proxySubscriptionRepository)(nil)
