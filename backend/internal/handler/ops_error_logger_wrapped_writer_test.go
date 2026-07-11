package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type testOuterResponseWriterWrapper struct {
	gin.ResponseWriter
}

func TestOpsErrorLoggerMiddleware_PreservesWrappedCaptureWriterForOuterMiddlewares(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var outerStatus int
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		outerStatus = c.Writer.Status()
	})
	r.GET("/responses", OpsErrorLoggerMiddleware(nil), func(c *gin.Context) {
		c.Writer = &testOuterResponseWriterWrapper{ResponseWriter: c.Writer}
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/responses", nil)

	require.NotPanics(t, func() {
		r.ServeHTTP(rec, req)
	})
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, http.StatusNoContent, outerStatus)
}
