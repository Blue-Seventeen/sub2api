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

func TestOpsErrorLoggerMiddleware_PreservesWrappedWriterForOuterWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var capture *opsCaptureWriter
	var written int
	var writeErr error
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Next()
		written, writeErr = c.Writer.WriteString("outer")
		c.Writer.Flush()
	})
	r.GET("/responses", OpsErrorLoggerMiddleware(nil), func(c *gin.Context) {
		var ok bool
		capture, ok = c.Writer.(*opsCaptureWriter)
		if !ok {
			panic("middleware did not install capture writer")
		}
		c.Writer = &testOuterResponseWriterWrapper{ResponseWriter: c.Writer}
		_, _ = c.Writer.WriteString("inner:")
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/responses", nil))
	defer releaseOpsCaptureWriter(capture)
	require.NoError(t, writeErr)
	require.Equal(t, len("outer"), written)
	require.Equal(t, "inner:outer", rec.Body.String())
	require.True(t, rec.Flushed)
}
