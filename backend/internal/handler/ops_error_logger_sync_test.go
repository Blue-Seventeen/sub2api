package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsCaptureWriter_ProductionAcquireFinalizesTerminalMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	w := acquireOpsCaptureWriter(c.Writer)
	defer releaseOpsCaptureWriter(w)
	w.setContext(c)
	w.WriteHeader(http.StatusOK)
	_, err := w.WriteString("event: response.failed\ndata: {\"padding\":\"" + strings.Repeat("x", opsCaptureWriterLimit) + "\",\"error\":{\"code\":\"service_unavailable\",\"message\":\"busy\"}}")
	require.NoError(t, err)
	w.finalizeCapture()
	terminal, found := w.capturedTerminalError()
	require.True(t, found)
	require.Equal(t, "busy", terminal.Message)
	require.Equal(t, "service_unavailable", terminal.Code)
	require.Len(t, w.capturedBytes(), opsCaptureWriterLimit)

	snapshot := w.capturedBytes()
	snapshot[0] = 'X'
	require.Equal(t, byte('e'), w.capturedBytes()[0], "capture snapshots must not alias pooled storage")
}

func TestOpsCaptureWriter_ReleasedLeaseCannotDelegateAccessors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	pool := &deterministicOpsCaptureWriterStatePool{}
	stale := acquireOpsCaptureWriterFromPool(pool, c.Writer)
	stale.WriteHeader(http.StatusCreated)
	releaseOpsCaptureWriter(stale)

	next, _ := gin.CreateTestContext(httptest.NewRecorder())
	current := acquireOpsCaptureWriterFromPool(pool, next.Writer)
	defer releaseOpsCaptureWriter(current)
	stale.WriteHeader(http.StatusBadGateway)
	stale.Header().Set("X-Stale", "must-not-reach-current")
	stale.setContext(c)
	require.Zero(t, stale.Status())
	require.Equal(t, -1, stale.Size())
	require.False(t, stale.Written())
	require.Equal(t, http.StatusOK, current.Status())
	require.Empty(t, current.Header().Get("X-Stale"))
	require.Nil(t, current.state.ctx)
}

func TestOpsErrorLoggerMiddleware_PrefersFullTerminalMetadataOverTruncatedBody(t *testing.T) {
	for _, ending := range []string{"\n\n", ""} {
		t.Run("ending="+strings.ReplaceAll(ending, "\n", "LF"), func(t *testing.T) {
			setupOpsErrorLogTestQueue(t, 2)
			gin.SetMode(gin.TestMode)
			ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.Use(OpsErrorLoggerMiddleware(ops))
			router.POST("/v1/responses", func(c *gin.Context) {
				setOpsRequestContext(c, "test-model", true)
				service.SetCompatibilityClientProfile(c, service.ClientProfileCodex)
				service.SetCompatibilityRoute(c, service.CompatibilityRouteCompatibleResponsesNative)
				service.SetOpsUpstreamError(c, http.StatusUnauthorized, "earlier attempt", "")
				c.Status(http.StatusOK)
				_, _ = c.Writer.WriteString("event: response.failed\ndata: {\"authorization\":\"Bearer must-not-persist\",\"padding\":\"" + strings.Repeat("x", opsCaptureWriterLimit))
				_, _ = c.Writer.WriteString("\",\"response\":{\"error\":{\"code\":\"provider_limit\",\"status_code\":\"429\",\"message\":\"slow down\"}}}" + ending)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, int64(1), OpsErrorLogQueueLength())
			entry := (<-opsErrorLogQueue).entry
			require.Equal(t, http.StatusTooManyRequests, entry.StatusCode)
			require.Equal(t, "slow down", entry.ErrorMessage)
			require.NotNil(t, entry.UpstreamStatusCode)
			require.Equal(t, http.StatusTooManyRequests, *entry.UpstreamStatusCode)
			require.NotNil(t, entry.UpstreamErrorMessage)
			require.Equal(t, "slow down", *entry.UpstreamErrorMessage)
			require.Equal(t, "codex", entry.ClientProfile)
			require.Equal(t, "compatible_responses_native", entry.CompatibilityRoute)
			require.True(t, entry.Stream)
			require.NotContains(t, entry.ErrorBody, "must-not-persist")
			require.Contains(t, entry.ErrorBody, `"payload_truncated":true`)
		})
	}
}

func TestOpsCaptureWriter_TerminalEventAfterDataPreservesMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		for _, split := range []int{1, 7, 1024} {
			frame := "data: {\"error\":{\"code\":\"service_unavailable\",\"message\":\"busy\"}}" + newline + "event: response.failed" + newline + newline
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			w := acquireOpsCaptureWriter(c.Writer)
			for offset := 0; offset < len(frame); offset += split {
				_, err := w.WriteString(frame[offset:min(offset+split, len(frame))])
				require.NoError(t, err)
			}
			w.finalizeCapture()
			terminal, found := w.capturedTerminalError()
			body := w.capturedBytes()
			releaseOpsCaptureWriter(w)
			require.True(t, found, "newline=%q split=%d", newline, split)
			require.Equal(t, "busy", terminal.Message, "newline=%q split=%d", newline, split)
			require.Equal(t, "service_unavailable", terminal.Code)
			require.Equal(t, "busy", parseOpsErrorResponse(body).Message)
		}
	}
}

func TestOpsCaptureWriter_TerminalProbeCapacityBoundedAcrossWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	w := acquireOpsCaptureWriter(c.Writer)
	defer releaseOpsCaptureWriter(w)
	_, err := w.WriteString("event: response.failed\ndata: {\"padding\":\"")
	require.NoError(t, err)
	for range opsTerminalSSEFrameProbeLimit * 2 {
		_, err = w.WriteString("x")
		require.NoError(t, err)
	}
	w.finalizeCapture()
	require.LessOrEqual(t, cap(w.state.probe), opsTerminalSSEFrameProbeLimit)
	require.Len(t, w.capturedBytes(), opsCaptureWriterLimit)
	terminal, found := w.capturedTerminalError()
	require.True(t, found)
	require.True(t, terminal.StreamFailure)
	require.Equal(t, "upstream stream failed", terminal.Message)
}

func TestOpsSelectedAccount_ClearsPreviousAttemptModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	setOpsEndpointContext(c, "previous-model", 1)
	setOpsSelectedAccount(c, 42)
	require.Empty(t, c.GetString(service.OpsUpstreamModelKey))
	require.Equal(t, int64(42), c.GetInt64(opsAccountIDKey))

	setOpsEndpointContext(c, "current-model", 1)
	require.Equal(t, "current-model", c.GetString(service.OpsUpstreamModelKey))
}

func TestOpsErrorLoggerMiddleware_RecoveredAccountAuthPreservesCustomAttribution(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 2)
	gin.SetMode(gin.TestMode)
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Platform, service.PlatformGemini))
		service.SetCompatibilityClientProfile(c, service.ClientProfileCodex)
		service.SetCompatibilityRoute(c, service.CompatibilityRouteCompatibleResponsesNative)
		c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{
			{AccountID: 17, Stage: string(service.GatewayFailureStageAccountAuth), Message: "token refresh failed"},
			{AccountID: 23, UpstreamStatusCode: http.StatusBadGateway, Message: "hidden attempt", SkipMonitoring: true},
		})
		c.JSON(http.StatusOK, gin.H{"status": "completed"})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	require.Equal(t, int64(1), OpsErrorLogQueueLength())
	entry := (<-opsErrorLogQueue).entry
	require.Equal(t, http.StatusOK, entry.StatusCode)
	require.Equal(t, "account_auth", entry.ErrorPhase)
	require.Equal(t, "provider", entry.ErrorOwner)
	require.Equal(t, "gateway", entry.ErrorSource)
	require.NotNil(t, entry.AccountID)
	require.Equal(t, int64(17), *entry.AccountID)
	require.NotNil(t, entry.UpstreamStatusCode)
	require.Zero(t, *entry.UpstreamStatusCode)
	require.Equal(t, service.PlatformGemini, entry.Platform)
	require.Equal(t, "codex", entry.ClientProfile)
	require.Equal(t, "compatible_responses_native", entry.CompatibilityRoute)
}
