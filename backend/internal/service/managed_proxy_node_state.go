package service

import (
	"context"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

// Optional repository capability keeps older repository implementations usable.
type proxySubscriptionNodeStateRepository interface {
	GetNodeStatesByProxyIDs(ctx context.Context, ids []int64) (map[int64]ProxySubscriptionNode, error)
}

type proxyNodeStatusUpdater interface {
	UpdateWithNodeStatus(context.Context, *Proxy, *string) error
}

var ErrManagedProxyConfigurationChanged = infraerrors.Conflict("PROXY_CONFIGURATION_CHANGED", "managed proxy configuration changed; retry the probe")

func (s *adminServiceImpl) captureProxyNodeEpoch(ctx context.Context, proxy *Proxy) (*time.Time, error) {
	if err := s.attachManagedProxyNodeStates(ctx, []*Proxy{proxy}); err != nil {
		return nil, err
	}
	if proxy.nodeUpdatedAt.IsZero() {
		return nil, nil
	}
	epoch := proxy.nodeUpdatedAt
	return &epoch, nil
}

func (s *adminServiceImpl) validateProxyNodeEpoch(ctx context.Context, proxyID int64, epoch *time.Time, capturedStatus ...string) error {
	if epoch == nil {
		return nil
	}
	repo, ok := s.proxySubRepo.(proxySubscriptionNodeStateRepository)
	if !ok {
		return ErrManagedProxyConfigurationChanged
	}
	states, err := repo.GetNodeStatesByProxyIDs(ctx, []int64{proxyID})
	if err != nil {
		return fmt.Errorf("verify managed proxy measurement epoch: %w", err)
	}
	node, ok := states[proxyID]
	expectedStatus := ProxySubscriptionNodeStatusActive
	if len(capturedStatus) > 0 {
		expectedStatus = capturedStatus[0]
	}
	if !ok || !node.UpdatedAt.Equal(*epoch) || node.Status != expectedStatus {
		return ErrManagedProxyConfigurationChanged
	}
	return nil
}

func isManagedProxyNodeUnavailable(proxy *Proxy) bool {
	return proxy != nil && proxy.IsManagedMihomoSubscription() && (proxy.Status == "inactive" || proxy.Status == "expired" || (proxy.NodeStatus != "" && proxy.NodeStatus != ProxySubscriptionNodeStatusActive))
}

func isManagedProxyNodePickerUnavailable(proxy *Proxy) bool {
	if isManagedProxyNodeUnavailable(proxy) {
		return true
	}
	if proxy == nil || !proxy.IsManagedMihomoSubscription() || proxy.NodeStatus == "" || proxy.RuntimeStatus == nil {
		return false
	}
	switch proxy.RuntimeStatus.Status {
	case ManagedProxyRuntimeStatusStopped, ManagedProxyRuntimeStatusDisabled, ManagedProxyRuntimeStatusError:
		return true
	default:
		return false
	}
}

func (s *adminServiceImpl) attachManagedProxyNodeStates(ctx context.Context, proxies []*Proxy) error {
	ids := make([]int64, 0, len(proxies))
	seen := make(map[int64]struct{}, len(proxies))
	for _, proxy := range proxies {
		if proxy == nil || !proxy.IsManagedMihomoSubscription() {
			continue
		}
		proxy.NodeStatus = ""
		proxy.nodeUpdatedAt = time.Time{}
		if _, ok := seen[proxy.ID]; !ok {
			ids = append(ids, proxy.ID)
			seen[proxy.ID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	repo, ok := s.proxySubRepo.(proxySubscriptionNodeStateRepository)
	if !ok {
		return nil
	}
	states, err := repo.GetNodeStatesByProxyIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("load managed proxy node state: %w", err)
	}
	for _, proxy := range proxies {
		if proxy == nil || !proxy.IsManagedMihomoSubscription() {
			continue
		}
		if node, ok := states[proxy.ID]; ok {
			proxy.NodeStatus = node.Status
			proxy.nodeUpdatedAt = node.UpdatedAt
		}
	}
	return nil
}

func clearProxyMeasurements(proxy *ProxyWithAccountCount) {
	proxy.LatencyMs = nil
	proxy.LatencyStatus = ""
	proxy.LatencyMessage = ""
	proxy.IPAddress = ""
	proxy.Country = ""
	proxy.CountryCode = ""
	proxy.Region = ""
	proxy.City = ""
	proxy.QualityStatus = ""
	proxy.QualityScore = nil
	proxy.QualityGrade = ""
	proxy.QualitySummary = ""
	proxy.QualityChecked = nil
}
