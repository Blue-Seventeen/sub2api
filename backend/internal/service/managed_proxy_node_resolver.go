package service

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// ManagedProxyNodeResolver validates a node against the configuration actually
// installed in the running process, without a repository request per forward.
// Older subscription-level resolvers remain compatible.
type ManagedProxyNodeResolver interface {
	ResolveProxyURLForProxy(ctx context.Context, proxy *Proxy) (string, error)
}

type managedProxyInstalledAuth struct {
	username, password string
	nodeEpoch          time.Time
}

type managedProxyMeasurementResolver interface {
	ResolveProxyURLForMeasurement(context.Context, *Proxy) (string, *time.Time, error)
}

func resolveManagedProxyURL(ctx context.Context, proxy *Proxy, resolver ManagedProxyResolver) (string, error) {
	if nodeResolver, ok := resolver.(ManagedProxyNodeResolver); ok {
		return nodeResolver.ResolveProxyURLForProxy(ctx, proxy)
	}
	if isManagedProxyNodeUnavailable(proxy) {
		return "", ErrManagedProxyNodeUnavailable
	}
	proxyURL, err := resolver.ResolveProxyURL(ctx, *proxy.SubscriptionID)
	if err != nil {
		return "", err
	}
	return managedProxyURLForProxy(proxyURL, proxy), nil
}

func (r *ManagedProxyRuntime) ResolveProxyURLForProxy(ctx context.Context, proxy *Proxy) (string, error) {
	value, _, err := r.ResolveProxyURLForMeasurement(ctx, proxy)
	return value, err
}

// The URL and epoch describe the same installed process generation.
func (r *ManagedProxyRuntime) ResolveProxyURLForMeasurement(ctx context.Context, proxy *Proxy) (string, *time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if r == nil || !r.cfg.Enabled {
		return "", nil, ErrManagedProxyDisabled
	}
	if proxy == nil || !proxy.IsManagedMihomoSubscription() {
		return "", nil, ErrManagedProxyUnavailable
	}
	if isManagedProxyNodeUnavailable(proxy) {
		return "", nil, ErrManagedProxyNodeUnavailable
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry := r.entries[*proxy.SubscriptionID]
	if entry == nil || entry.status != ManagedProxyRuntimeStatusRunning || entry.port <= 0 {
		return "", nil, ErrManagedProxyUnavailable
	}
	auth, installed := entry.installedNodeAuth[proxy.ID]
	_, nodeBound := entry.nodeProxyIDs[proxy.ID]
	if !installed {
		// Legacy aggregate proxies have no node or local authentication. Keep their
		// subscription-level route; every node-bound/authenticated row fails closed.
		if nodeBound || proxy.NodeStatus != "" || proxy.Username != "" || proxy.Password != "" {
			return "", nil, ErrManagedProxyNodeUnavailable
		}
	} else if auth.username != proxy.Username || auth.password != proxy.Password {
		return "", nil, ErrManagedProxyNodeUnavailable
	}
	proxyURL := fmt.Sprintf("socks5h://%s", net.JoinHostPort(r.cfg.BindHost, strconv.Itoa(entry.port)))
	var epoch *time.Time
	if installed && !auth.nodeEpoch.IsZero() {
		value := auth.nodeEpoch
		epoch = &value
	}
	return managedProxyURLForProxy(proxyURL, proxy), epoch, nil
}

func managedProxyInstalledNodes(sub ProxySubscription) (map[int64]managedProxyInstalledAuth, map[int64]struct{}) {
	auth := make(map[int64]managedProxyInstalledAuth)
	ids := make(map[int64]struct{})
	for _, node := range sub.Nodes {
		if node.ProxyID == nil || *node.ProxyID <= 0 {
			continue
		}
		id := *node.ProxyID
		ids[id] = struct{}{}
		if node.Status == "" || node.Status == ProxySubscriptionNodeStatusActive {
			auth[id] = managedProxyInstalledAuth{username: node.Username, password: node.Password, nodeEpoch: node.UpdatedAt}
		}
	}
	return auth, ids
}
