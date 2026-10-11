package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type managedProxyNodeStateRepo struct {
	ProxySubscriptionRepository
	states     map[int64]ProxySubscriptionNode
	err        error
	batchCalls int
}

func (r *managedProxyNodeStateRepo) GetNodeStatesByProxyIDs(_ context.Context, ids []int64) (map[int64]ProxySubscriptionNode, error) {
	r.batchCalls++
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[int64]ProxySubscriptionNode, len(ids))
	for _, id := range ids {
		if node, ok := r.states[id]; ok {
			out[id] = node
		}
	}
	return out, nil
}

type managedProxyNodeAdminRepo struct {
	ProxyRepository
	proxies []Proxy
}

func (r *managedProxyNodeAdminRepo) ListActive(context.Context) ([]Proxy, error) {
	return append([]Proxy(nil), r.proxies...), nil
}
func (r *managedProxyNodeAdminRepo) ListByIDs(context.Context, []int64) ([]Proxy, error) {
	return append([]Proxy(nil), r.proxies...), nil
}
func (r *managedProxyNodeAdminRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	for _, p := range r.proxies {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, ErrProxyNotFound
}
func (r *managedProxyNodeAdminRepo) CountAccountsByProxyID(context.Context, int64) (int64, error) {
	return 1, nil
}
func (r *managedProxyNodeAdminRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]Proxy, *pagination.PaginationResult, error) {
	return append([]Proxy(nil), r.proxies...), &pagination.PaginationResult{Total: int64(len(r.proxies))}, nil
}
func (r *managedProxyNodeAdminRepo) counts() []ProxyWithAccountCount {
	out := make([]ProxyWithAccountCount, len(r.proxies))
	for i, p := range r.proxies {
		out[i].Proxy = p
	}
	return out
}
func (r *managedProxyNodeAdminRepo) ListActiveWithAccountCount(context.Context) ([]ProxyWithAccountCount, error) {
	return r.counts(), nil
}
func (r *managedProxyNodeAdminRepo) ListWithFiltersAndAccountCount(context.Context, pagination.PaginationParams, string, string, string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	return r.counts(), &pagination.PaginationResult{Total: int64(len(r.proxies))}, nil
}

type managedProxyNodeLatencyCache struct{ values map[int64]*ProxyLatencyInfo }

func (r *managedProxyNodeLatencyCache) GetProxyLatencies(context.Context, []int64) (map[int64]*ProxyLatencyInfo, error) {
	return r.values, nil
}
func (r *managedProxyNodeLatencyCache) SetProxyLatency(_ context.Context, id int64, info *ProxyLatencyInfo) error {
	r.values[id] = info
	return nil
}

type managedProxyNodeRunningResolver struct{ managedProxyNoopResolver }

func (managedProxyNodeRunningResolver) GetStatus(id int64) ManagedProxyRuntimeStatus {
	return ManagedProxyRuntimeStatus{Enabled: true, Status: ManagedProxyRuntimeStatusRunning, SubscriptionID: id}
}

func managedProxyNodeAdminFixture() (*adminServiceImpl, *managedProxyNodeStateRepo) {
	subID := int64(7)
	states := &managedProxyNodeStateRepo{states: map[int64]ProxySubscriptionNode{1: {Status: ProxySubscriptionNodeStatusActive}, 2: {Status: ProxySubscriptionNodeStatusInactive}, 3: {Status: ProxySubscriptionNodeStatusSourceMissing}}}
	proxies := []Proxy{
		{ID: 1, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID},
		{ID: 2, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID},
		{ID: 3, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID},
		{ID: 4, Status: StatusActive, SourceType: ProxySourceManual},
		{ID: 5, Status: StatusActive, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &subID},
	}
	return &adminServiceImpl{proxyRepo: &managedProxyNodeAdminRepo{proxies: proxies}, proxySubRepo: states, managedProxyResolver: managedProxyNodeRunningResolver{}}, states
}
func TestManagedProxyNodeAdminPickerExcludesMissingAndDisabled(t *testing.T) {
	svc, _ := managedProxyNodeAdminFixture()
	proxies, err := svc.GetAllProxies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, len(proxies))
	for i, p := range proxies {
		ids[i] = p.ID
	}
	if !reflect.DeepEqual(ids, []int64{1, 4, 5}) {
		t.Errorf("picker ids = %v, want active, manual and legacy only", ids)
	}
	counted, err := svc.GetAllProxiesWithAccountCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids = nil
	for _, p := range counted {
		ids = append(ids, p.ID)
	}
	if !reflect.DeepEqual(ids, []int64{1, 4, 5}) {
		t.Errorf("counted picker ids = %v, want active, manual and legacy only", ids)
	}
}
func TestManagedProxyNodeAdminSnapshotsHideUnavailableAndStaleMeasurements(t *testing.T) {
	svc, states := managedProxyNodeAdminFixture()
	now := time.Now()
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: now}
	latency, score, checked := int64(20), 100, now.Add(-time.Hour).Unix()
	values := make(map[int64]*ProxyLatencyInfo)
	for _, id := range []int64{1, 2, 3, 4, 5} {
		values[id] = &ProxyLatencyInfo{Success: true, LatencyMs: &latency, QualityScore: &score, QualityStatus: "healthy", QualityGrade: "A", QualityCheckedAt: &checked, IPAddress: "198.51.100.1", UpdatedAt: now.Add(-time.Minute)}
	}
	svc.proxyLatencyCache = &managedProxyNodeLatencyCache{values: values}
	out, err := svc.GetProxySnapshots(context.Background(), []int64{1, 2, 3, 4, 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out {
		if p.ID <= 3 && (p.LatencyStatus != "" || p.LatencyMs != nil || p.QualityScore != nil || p.QualityGrade != "" || p.QualityChecked != nil || p.IPAddress != "") {
			t.Errorf("proxy %d exposes stale connectivity/quality proof", p.ID)
		}
		if p.ID >= 4 && (p.LatencyStatus != "success" || p.QualityGrade != "A") {
			t.Errorf("legacy/manual proxy %d lost evidence", p.ID)
		}
		if p.ID == 3 && (p.RuntimeStatus == nil || p.RuntimeStatus.Status != ManagedProxyRuntimeStatusRunning) {
			t.Error("subscription process state was replaced by node state")
		}
	}
}
func TestManagedProxyNodeAdminStateLookupErrorIsReturned(t *testing.T) {
	svc, states := managedProxyNodeAdminFixture()
	states.err = errors.New("node lookup failed")
	if _, err := svc.GetAllProxies(context.Background()); !errors.Is(err, states.err) {
		t.Error("picker accepted unverified state after lookup error")
	}
	if _, err := svc.GetProxySnapshots(context.Background(), []int64{1}); !errors.Is(err, states.err) {
		t.Error("snapshots accepted unverified state after lookup error")
	}
}

func TestManagedProxyNodeAdminFreshLatencyDoesNotReviveOldQuality(t *testing.T) {
	svc, states := managedProxyNodeAdminFixture()
	now := time.Now()
	states.states[1] = ProxySubscriptionNode{Status: ProxySubscriptionNodeStatusActive, UpdatedAt: now.Add(-time.Minute)}
	latency, score, checked := int64(20), 100, now.Add(-time.Hour).Unix()
	epoch := states.states[1].UpdatedAt
	svc.proxyLatencyCache = &managedProxyNodeLatencyCache{values: map[int64]*ProxyLatencyInfo{1: {Success: true, LatencyMs: &latency, NodeEpoch: &epoch, QualityScore: &score, QualityStatus: "healthy", QualityGrade: "A", QualityCheckedAt: &checked, UpdatedAt: now}}}
	out, err := svc.GetProxySnapshots(context.Background(), []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || out[0].LatencyStatus != "success" {
		t.Fatal("fresh latency was hidden")
	}
	if out[0].QualityScore != nil || out[0].QualityGrade != "" || out[0].QualityChecked != nil {
		t.Fatal("new latency revived quality from an earlier node configuration")
	}
}

func TestManagedProxyNodeAdminListGetAndSnapshotsCarryNodeState(t *testing.T) {
	svc, states := managedProxyNodeAdminFixture()
	assertStates := func(proxies []Proxy) {
		t.Helper()
		for _, p := range proxies {
			expected := ""
			if node, ok := states.states[p.ID]; ok {
				expected = node.Status
			}
			if p.NodeStatus != expected {
				t.Errorf("proxy %d node state = %q, want %q", p.ID, p.NodeStatus, expected)
			}
			if p.IsManagedMihomoSubscription() && (p.RuntimeStatus == nil || p.RuntimeStatus.Status != ManagedProxyRuntimeStatusRunning) {
				t.Errorf("proxy %d lost separate running runtime state", p.ID)
			}
		}
	}
	proxies, _, err := svc.ListProxies(context.Background(), 1, 10, "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	assertStates(proxies)
	counted, _, err := svc.ListProxiesWithAccountCount(context.Background(), 1, 10, "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	proxies = nil
	for _, p := range counted {
		proxies = append(proxies, p.Proxy)
	}
	assertStates(proxies)
	p, err := svc.GetProxy(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	assertStates([]Proxy{*p})
	out, err := svc.GetProxySnapshots(context.Background(), []int64{1, 2, 3, 4, 5})
	if err != nil {
		t.Fatal(err)
	}
	proxies = nil
	for _, p := range out {
		proxies = append(proxies, p.Proxy)
	}
	assertStates(proxies)
	if states.batchCalls != 4 {
		t.Fatalf("node state lookup requests = %d, want one batch per API call", states.batchCalls)
	}
	svc.proxySubRepo = &managedProxyRuntimeRepoStub{}
	proxies, err = svc.GetAllProxies(context.Background())
	if err != nil || len(proxies) != 5 {
		t.Fatal("repository without optional batch method lost legacy behavior")
	}
}
