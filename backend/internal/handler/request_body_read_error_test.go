package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type timeoutReadError struct{}

func (timeoutReadError) Error() string   { return "read tcp: i/o timeout" }
func (timeoutReadError) Timeout() bool   { return true }
func (timeoutReadError) Temporary() bool { return true }

func TestClassifyRequestBodyReadError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "unexpected_eof", err: io.ErrUnexpectedEOF, want: requestBodyUnexpectedEOF},
		{name: "context_canceled", err: context.Canceled, want: requestBodyContextCanceled},
		{name: "connection_reset", err: errors.New("read tcp 127.0.0.1:1->127.0.0.1:2: read: connection reset by peer"), want: requestBodyConnectionReset},
		{name: "timeout", err: timeoutReadError{}, want: requestBodyTimeout},
		{name: "unknown", err: errors.New("some reader failure"), want: requestBodyReadError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyRequestBodyReadError(tt.err))
		})
	}
}

type bodyReadErrorCloser struct {
	payload string
	err     error
	read    bool
}

func (r *bodyReadErrorCloser) Read(p []byte) (int, error) {
	if r.read {
		return 0, r.err
	}
	r.read = true
	return copy(p, r.payload), nil
}

func (r *bodyReadErrorCloser) Close() error {
	return nil
}

func TestReadRequestBodyWithObservability_KeepsClientResponseAndStoresOpsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotType string
	var gotDetail string
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		_, err := readRequestBodyWithObservability(c, requestLogger(c, "test.request_body"))
		require.Error(t, err)
		gotType, gotDetail = getOpsRequestBodyReadError(c)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": "Failed to read request body",
			},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(""))
	req.Body = &bodyReadErrorCloser{
		payload: `{"model":"gpt-5"`,
		err:     io.ErrUnexpectedEOF,
	}
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Failed to read request body")
	require.Equal(t, requestBodyUnexpectedEOF, gotType)
	require.Contains(t, gotDetail, "unexpected EOF")
}

func TestApplyRequestBodyReadErrorOpsFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(opsRequestBodyReadErrorTypeKey, requestBodyConnectionReset)
	c.Set(opsRequestBodyReadErrorDetailKey, "read: connection reset by peer")

	entry := &service.OpsInsertErrorLogInput{
		ErrorPhase:  "request",
		ErrorSource: "gateway",
		ErrorOwner:  "platform",
	}

	require.True(t, applyRequestBodyReadErrorOpsFields(c, entry))
	require.Equal(t, "network", entry.ErrorPhase)
	require.Equal(t, "client_request", entry.ErrorSource)
	require.Equal(t, "client", entry.ErrorOwner)
	require.Equal(t, requestBodyConnectionReset, entry.NetworkErrorType)
	require.NotNil(t, entry.UpstreamErrorDetail)
	require.Contains(t, *entry.UpstreamErrorDetail, "connection reset")
}
