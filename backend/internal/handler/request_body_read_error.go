package handler

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	requestBodyUnexpectedEOF   = "request_body_unexpected_eof"
	requestBodyContextCanceled = "request_body_context_canceled"
	requestBodyConnectionReset = "request_body_connection_reset"
	requestBodyTimeout         = "request_body_timeout"
	requestBodyReadError       = "request_body_read_error"

	opsRequestBodyReadErrorTypeKey   = "ops_request_body_read_error_type"
	opsRequestBodyReadErrorDetailKey = "ops_request_body_read_error_detail"
)

func readRequestBodyWithObservability(c *gin.Context, reqLog *zap.Logger) ([]byte, error) {
	var req *http.Request
	if c != nil {
		req = c.Request
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(req)
	if err != nil {
		if _, ok := extractMaxBytesError(err); !ok {
			recordRequestBodyReadError(c, reqLog, err)
		}
	}
	return body, err
}

func classifyRequestBodyReadError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		return requestBodyContextCanceled
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(strings.ToLower(err.Error()), "unexpected eof") {
		return requestBodyUnexpectedEOF
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "forcibly closed by the remote host") ||
		strings.Contains(msg, "broken pipe") {
		return requestBodyConnectionReset
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return requestBodyTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "timeout") {
		return requestBodyTimeout
	}

	return requestBodyReadError
}

func recordRequestBodyReadError(c *gin.Context, reqLog *zap.Logger, err error) {
	if err == nil {
		return
	}
	networkErrorType := classifyRequestBodyReadError(err)
	if networkErrorType == "" {
		networkErrorType = requestBodyReadError
	}
	detail := strings.TrimSpace(err.Error())

	if c != nil {
		c.Set(opsRequestBodyReadErrorTypeKey, networkErrorType)
		if detail != "" {
			c.Set(opsRequestBodyReadErrorDetailKey, detail)
		}
	}

	if reqLog == nil {
		reqLog = requestLogger(c, "handler.request_body")
	}

	fields := []zap.Field{
		zap.String("network_error_type", networkErrorType),
		zap.Error(err),
	}
	if c != nil && c.Request != nil {
		req := c.Request
		fields = append(fields,
			zap.String("method", req.Method),
			zap.Int64("content_length", req.ContentLength),
			zap.String("transfer_encoding", requestTransferEncoding(req)),
			zap.String("user_agent", req.UserAgent()),
		)
		if req.URL != nil {
			fields = append(fields, zap.String("path", req.URL.Path))
		}
		if clientRequestID, _ := req.Context().Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
			fields = append(fields, zap.String("client_request_id", strings.TrimSpace(clientRequestID)))
		}
		if clientIP := strings.TrimSpace(ip.GetClientIP(c)); clientIP != "" {
			fields = append(fields, zap.String("client_ip", clientIP))
		}
	}

	reqLog.Warn("request_body.read_failed", fields...)
}

func requestTransferEncoding(req *http.Request) string {
	if req == nil {
		return ""
	}
	if len(req.TransferEncoding) > 0 {
		return strings.Join(req.TransferEncoding, ",")
	}
	return strings.TrimSpace(req.Header.Get("Transfer-Encoding"))
}

func getOpsRequestBodyReadError(c *gin.Context) (string, string) {
	if c == nil {
		return "", ""
	}
	typ, _ := c.Get(opsRequestBodyReadErrorTypeKey)
	detail, _ := c.Get(opsRequestBodyReadErrorDetailKey)
	typStr, _ := typ.(string)
	detailStr, _ := detail.(string)
	return strings.TrimSpace(typStr), strings.TrimSpace(detailStr)
}

func applyRequestBodyReadErrorOpsFields(c *gin.Context, entry *service.OpsInsertErrorLogInput) bool {
	if entry == nil {
		return false
	}
	networkErrorType, detail := getOpsRequestBodyReadError(c)
	if networkErrorType == "" {
		return false
	}
	entry.ErrorPhase = "network"
	entry.ErrorSource = "client_request"
	entry.ErrorOwner = "client"
	entry.NetworkErrorType = networkErrorType
	if detail != "" && entry.UpstreamErrorDetail == nil {
		entry.UpstreamErrorDetail = &detail
	}
	return true
}
