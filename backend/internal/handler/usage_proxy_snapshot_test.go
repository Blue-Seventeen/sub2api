package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUsageTaskFreezesActualRoutesBeforeNextTurn(t *testing.T) {
	ctx, o := service.NewProxyRouteObservation(t.Context(), nil)
	defer o.Close()
	raw := service.ResolveProxyURL(ctx, &service.Proxy{ID: 17, Protocol: "http", Host: "127.0.0.1", Port: 8081})
	service.BeginProxyRouteAttempt(ctx, 52, raw, false).End()
	var got []service.ProxyRouteSnapshot
	task, _ := wrapUsageRecordTaskContext(ctx, func(worker context.Context) { got = service.ProxyRoutesFromContext(worker) })
	service.ResetProxyRouteUsage(ctx)
	service.BeginProxyRouteAttempt(ctx, 52, "", false).End()
	task(context.Background())
	require.Len(t, got, 1)
	require.Equal(t, int64(17), got[0].ProxyID)
	require.Equal(t, "proxy", got[0].Kind)
}

func TestProxyRouteObservationMiddlewareOwnsRequestLifetime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ProxyRouteObservationMiddleware(&Handlers{}))
	var observed context.Context
	r.GET("/fixture", func(c *gin.Context) {
		observed = c.Request.Context()
		service.BeginProxyRouteAttempt(observed, 1, "", false).End()
		require.Equal(t, []service.ProxyRouteSnapshot{{Kind: "direct"}}, service.ProxyRoutesFromContext(observed))
		c.Status(204)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fixture", nil))
	require.Equal(t, 204, w.Code)
	service.ResetProxyRouteUsage(observed)
	require.Nil(t, service.BeginProxyRouteAttempt(observed, 1, "", false), "completed middleware must close its observation")
}
