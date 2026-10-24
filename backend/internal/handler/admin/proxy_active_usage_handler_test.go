package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestProxyActiveUsageDetailsHandlerReportsLiveCountsAndUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	tracker := service.NewProxyActiveUsageTracker(repository.NewRedisProxyActiveUsageStore(client))
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
	server.Close()
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
