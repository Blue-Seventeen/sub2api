package websearch

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagerAttemptObserverCoversActualSearch(t *testing.T) {
	var active, observed atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		observed.Store(active.Load())
		_, _ = w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer server.Close()
	original := *braveSearchURL
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	*braveSearchURL = *request.URL
	defer func() { *braveSearchURL = original }()
	manager := NewManager([]ProviderConfig{{Type: "brave", APIKey: "fixture"}}, nil)
	manager.clientCache[""] = server.Client()
	_, _, err = manager.SearchWithBestProvider(t.Context(), SearchRequest{Query: "fixture", BeforeAttempt: func(provider ProviderConfig, raw string) func() {
		require.Equal(t, "brave", provider.Type)
		require.Empty(t, raw)
		active.Add(1)
		return func() { active.Add(-1) }
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), observed.Load())
	require.Zero(t, active.Load())
}
