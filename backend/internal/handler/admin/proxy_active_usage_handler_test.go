package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyActiveUsageHandlerStore struct {
	mu      sync.Mutex
	entries map[string]service.ProxyActiveUsageEntry
	err     error
}

var _ service.ProxyActiveUsageStore = (*proxyActiveUsageHandlerStore)(nil)

func (s *proxyActiveUsageHandlerStore) UpsertProxyActiveUsage(_ context.Context, entry service.ProxyActiveUsageEntry, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.entries[entry.Token] = entry
	return nil
}

func (s *proxyActiveUsageHandlerStore) RemoveProxyActiveUsage(_ context.Context, entry service.ProxyActiveUsageEntry, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	delete(s.entries, entry.Token)
	return nil
}

func (s *proxyActiveUsageHandlerStore) CountProxyActiveAccounts(ctx context.Context, proxyIDs []int64) (map[int64]int64, error) {
	usage, err := s.CountProxyActiveUsage(ctx, proxyIDs)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64, len(usage))
	for proxyID, count := range usage {
		counts[proxyID] = count.AccountCount
	}
	return counts, nil
}

func (s *proxyActiveUsageHandlerStore) CountProxyActiveUsage(_ context.Context, proxyIDs []int64) (map[int64]service.ProxyActiveUsageCounts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	counts := make(map[int64]service.ProxyActiveUsageCounts, len(proxyIDs))
	for _, proxyID := range proxyIDs {
		accounts := make(map[int64]struct{})
		var requests int64
		for _, entry := range s.entries {
			if entry.ProxyID == proxyID {
				accounts[entry.AccountID] = struct{}{}
				requests++
			}
		}
		counts[proxyID] = service.ProxyActiveUsageCounts{AccountCount: int64(len(accounts)), RequestCount: requests}
	}
	return counts, nil
}

func (s *proxyActiveUsageHandlerStore) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func TestProxyActiveUsageDetailsHandlerReportsLiveCountsAndUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &proxyActiveUsageHandlerStore{entries: make(map[string]service.ProxyActiveUsageEntry)}
	tracker := service.NewProxyActiveUsageTracker(store)
	defer tracker.Stop()
	handler := NewProxyHandler(nil, nil, tracker)
	router := gin.New()
	router.GET("/details", handler.GetActiveUsageDetails)
	router.GET("/legacy", handler.GetActiveUsage)

	first := tracker.BeginProxy(10, 100)
	second := tracker.BeginProxy(10, 100)
	third := tracker.BeginProxy(10, 101)
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.NotNil(t, third)
	defer first.End()
	defer second.End()
	defer third.End()
	require.Eventually(t, func() bool {
		detail := tracker.GetActiveUsageDetails(context.Background(), []int64{10})[10]
		return detail.RequestCount != nil && *detail.RequestCount == 3
	}, time.Second, time.Millisecond)

	checkData := func(path, want string) {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.JSONEq(t, want, string(body.Data))
	}
	checkData("/details?ids=10,20,10", `{"10":{"account_count":2,"request_count":3,"status":"ok"},"20":{"account_count":0,"request_count":0,"status":"ok"}}`)
	checkData("/legacy?ids=10,20", `{"10":2,"20":0}`)
	checkData("/details", `{}`)
	store.setError(errors.New("active usage store unavailable"))
	checkData("/details?ids=10", `{"10":{"account_count":null,"request_count":null,"status":"unavailable"}}`)
	checkData("/legacy?ids=10", `{"10":0}`)
}

func TestProxyActiveUsageDetailsHandlerRejectsInvalidIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/details", NewProxyHandler(nil, nil, nil).GetActiveUsageDetails)
	for _, ids := range []string{"0", "-1", "abc", "10,", "10,,20", "9223372036854775808"} {
		t.Run(ids, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/details?ids="+ids, nil))
			require.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}
