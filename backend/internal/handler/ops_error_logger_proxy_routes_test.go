package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func opsProxyRoutesForTest(t *testing.T, entry *service.OpsInsertErrorLogInput) []service.ProxyRouteSnapshot {
	t.Helper()
	payload, err := json.Marshal(entry)
	require.NoError(t, err)
	var decoded struct {
		ProxyRoutes []service.ProxyRouteSnapshot `json:"proxy_routes"`
	}
	require.NoError(t, json.Unmarshal(payload, &decoded))
	return decoded.ProxyRoutes
}

func TestOpsErrorProxyRoutes_QueuedRequestSnapshots(t *testing.T) {
	for _, mode := range []string{"http_error", "sse_error", "recovered_upstream"} {
		t.Run(mode, func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			gin.SetMode(gin.TestMode)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			routes := []service.ProxyRouteSnapshot{
				{Kind: "proxy", ProxyID: 152, Name: "request-time-node", Host: "192.0.2.10", Port: 1080},
				{Kind: "direct"},
				{Kind: "system_proxy"},
			}
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/responses", func(c *gin.Context) {
				c.Request = c.Request.WithContext(service.WithProxyRoutesSnapshot(c.Request.Context(), routes))
				switch mode {
				case "sse_error":
					setOpsRequestContext(c, "model-test", true)
					c.Header("Content-Type", "text/event-stream")
					c.String(http.StatusOK, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"upstream failed\"}}}\n\n")
				case "recovered_upstream":
					c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{UpstreamStatusCode: 429, Message: "earlier attempt failed"}})
					c.JSON(http.StatusOK, gin.H{"status": "completed"})
				default:
					c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "upstream failed"}})
				}
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			require.Equal(t, int64(1), OpsErrorLogQueueLength())
			job := <-opsErrorLogQueue
			require.Equal(t, routes, opsProxyRoutesForTest(t, job.entry))
			routes[0].Name = "changed-after-request"
			require.Equal(t, "request-time-node", opsProxyRoutesForTest(t, job.entry)[0].Name)
		})
	}
}

func TestOpsErrorProxyRoutes_MissingObservationStaysUnknown(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 1)
	gin.SetMode(gin.TestMode)
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "failed before outbound"}})
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	require.Equal(t, int64(1), OpsErrorLogQueueLength())
	require.Nil(t, opsProxyRoutesForTest(t, (<-opsErrorLogQueue).entry))
}

func TestOpsErrorProxyRoutes_CyberPolicyUsesCapturedMetadata(t *testing.T) {
	var meta cyberPolicyOpsErrorMeta
	require.NoError(t, json.Unmarshal([]byte(`{"proxy_routes":[{"kind":"proxy","proxy_id":152,"name":"request-time-node","host":"192.0.2.10","port":1080}]}`), &meta))
	entry := buildCyberPolicyOpsErrorEntry(meta, &service.CyberPolicyMark{UpstreamStatus: 400, Message: "blocked"})
	routes := opsProxyRoutesForTest(t, entry)
	require.Len(t, routes, 1)
	require.Equal(t, int64(152), routes[0].ProxyID)
	require.Equal(t, "request-time-node", routes[0].Name)
	require.Nil(t, opsProxyRoutesForTest(t, buildCyberSessionBlockedOpsEntry(cyberPolicyOpsErrorMeta{})))
}

func TestOpsErrorProxyRoutes_WebSocketTurnSnapshot(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 3)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	service.SetOpenAIClientTransport(c, service.OpenAIClientTransportWS)
	firstRoutes := []service.ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 152, Name: "first-turn-node", Host: "192.0.2.10", Port: 1080}}
	c.Request = c.Request.WithContext(service.WithProxyRoutesSnapshot(c.Request.Context(), firstRoutes))
	service.BeginOpsStreamTurn(c, 1)
	service.MarkOpsStreamFailure(c, "upstream_error", "server_error", "turn one failed", http.StatusBadGateway)
	// The connection continues with a new route after the failed turn. Logging
	// must use the route observed when each error was marked, including nil.
	c.Request = c.Request.WithContext(service.WithProxyRoutesSnapshot(c.Request.Context(), nil))
	service.BeginOpsStreamTurn(c, 2)
	service.MarkOpsStreamFailure(c, "permission_error", "permission_denied", "turn two rejected locally", http.StatusForbidden)
	c.Request = c.Request.WithContext(service.WithProxyRoutesSnapshot(c.Request.Context(), []service.ProxyRouteSnapshot{{Kind: "direct"}}))
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	logOpsStreamError(c, ops, http.StatusSwitchingProtocols)
	require.Equal(t, int64(2), OpsErrorLogQueueLength())
	require.Equal(t, firstRoutes, opsProxyRoutesForTest(t, (<-opsErrorLogQueue).entry))
	require.Nil(t, opsProxyRoutesForTest(t, (<-opsErrorLogQueue).entry))
}
