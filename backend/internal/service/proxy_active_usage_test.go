package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryProxyActiveUsageStore struct {
	mu       sync.Mutex
	requests map[string]ProxyActiveUsageEntry
	accounts map[int64]map[int64]map[string]struct{}
	err      error
}

func newMemoryProxyActiveUsageStore() *memoryProxyActiveUsageStore {
	return &memoryProxyActiveUsageStore{
		requests: make(map[string]ProxyActiveUsageEntry),
		accounts: make(map[int64]map[int64]map[string]struct{}),
	}
}

func (s *memoryProxyActiveUsageStore) UpsertProxyActiveUsage(_ context.Context, entry ProxyActiveUsageEntry, _ time.Duration) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ended := s.requests["ended:"+entry.Token]; ended {
		return nil
	}
	s.requests[entry.Token] = entry
	if s.accounts[entry.ProxyID] == nil {
		s.accounts[entry.ProxyID] = make(map[int64]map[string]struct{})
	}
	if s.accounts[entry.ProxyID][entry.AccountID] == nil {
		s.accounts[entry.ProxyID][entry.AccountID] = make(map[string]struct{})
	}
	s.accounts[entry.ProxyID][entry.AccountID][entry.Token] = struct{}{}
	return nil
}

func (s *memoryProxyActiveUsageStore) RemoveProxyActiveUsage(_ context.Context, entry ProxyActiveUsageEntry, _ time.Duration) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests["ended:"+entry.Token] = entry
	delete(s.requests, entry.Token)
	if byAccount := s.accounts[entry.ProxyID]; byAccount != nil {
		if tokens := byAccount[entry.AccountID]; tokens != nil {
			delete(tokens, entry.Token)
			if len(tokens) == 0 {
				delete(byAccount, entry.AccountID)
			}
		}
		if len(byAccount) == 0 {
			delete(s.accounts, entry.ProxyID)
		}
	}
	return nil
}

func (s *memoryProxyActiveUsageStore) CountProxyActiveAccounts(_ context.Context, proxyIDs []int64) (map[int64]int64, error) {
	if s.err != nil {
		return zeroProxyActiveUsageCounts(proxyIDs), s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := zeroProxyActiveUsageCounts(proxyIDs)
	for _, proxyID := range proxyIDs {
		out[proxyID] = int64(len(s.accounts[proxyID]))
	}
	return out, nil
}

func waitForProxyActiveUsageCount(t *testing.T, tracker *ProxyActiveUsageTracker, proxyID int64, want int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		counts := tracker.GetActiveAccountCounts(context.Background(), []int64{proxyID})
		return counts[proxyID] == want
	}, time.Second, 10*time.Millisecond)
}

func TestProxyActiveUsageTrackerDistinctAccountCounts(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{
		WorkerCount:       2,
		QueueSize:         16,
		TaskTimeout:       100 * time.Millisecond,
		TTL:               time.Second,
		HeartbeatInterval: 200 * time.Millisecond,
	})
	defer tracker.Stop()

	first := tracker.BeginProxy(10, 100)
	secondSameAccount := tracker.BeginProxy(10, 100)
	thirdDifferentAccount := tracker.BeginProxy(10, 101)
	require.NotNil(t, first)
	require.NotNil(t, secondSameAccount)
	require.NotNil(t, thirdDifferentAccount)
	waitForProxyActiveUsageCount(t, tracker, 10, 2)

	first.End()
	waitForProxyActiveUsageCount(t, tracker, 10, 2)

	secondSameAccount.End()
	waitForProxyActiveUsageCount(t, tracker, 10, 1)

	thirdDifferentAccount.End()
	waitForProxyActiveUsageCount(t, tracker, 10, 0)
}

func TestProxyActiveUsageTrackerBestEffortErrorsDoNotAffectCaller(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	store.err = errors.New("redis unavailable")
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{
		WorkerCount:       1,
		QueueSize:         4,
		TaskTimeout:       20 * time.Millisecond,
		TTL:               time.Second,
		HeartbeatInterval: 200 * time.Millisecond,
	})
	defer tracker.Stop()

	handle := tracker.BeginProxy(10, 100)
	require.NotNil(t, handle)
	require.NotPanics(t, handle.End)
	counts := tracker.GetActiveAccountCounts(context.Background(), []int64{10})
	require.Equal(t, int64(0), counts[10])
}

func TestProxyActiveUsageTrackerInvalidInputNoop(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{})
	defer tracker.Stop()

	require.Nil(t, tracker.BeginProxy(0, 100))
	require.Nil(t, tracker.BeginProxy(10, 0))
	require.Nil(t, tracker.Begin(nil))
	counts := tracker.GetActiveAccountCounts(context.Background(), []int64{10})
	require.Equal(t, int64(0), counts[10])
}
