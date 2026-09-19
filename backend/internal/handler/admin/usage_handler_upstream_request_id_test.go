package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminUsageUpstreamRequestIDRepo struct {
	service.UsageLogRepository
	log service.UsageLog
}

func (r *adminUsageUpstreamRequestIDRepo) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _ usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	return []service.UsageLog{r.log}, &pagination.PaginationResult{Total: 1, Page: 1, PageSize: 20, Pages: 1}, nil
}

func TestAdminUsageListUpstreamRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamRequestID := "provider-diagnostic-id"
	repo := &adminUsageUpstreamRequestIDRepo{log: service.UsageLog{
		ID: 42, RequestID: "client-request-id", UpstreamRequestID: &upstreamRequestID,
	}}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.GET("/admin/usage", h.List)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/admin/usage", nil))
	require.Equal(t, http.StatusOK, res.Code)
	var body struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Len(t, body.Data.Items, 1)
	require.Equal(t, "client-request-id", body.Data.Items[0]["request_id"])
	require.Equal(t, upstreamRequestID, body.Data.Items[0]["upstream_request_id"])
}
