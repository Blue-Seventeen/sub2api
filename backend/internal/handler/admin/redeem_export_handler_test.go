package admin

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupRedeemExportRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	h := NewRedeemHandler(adminSvc, nil)
	router.GET("/api/v1/admin/redeem-codes/export", h.Export)
	return router, adminSvc
}

func TestRedeemExportPassesSearchAndSort(t *testing.T) {
	router, adminSvc := setupRedeemExportRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/export?type=balance&status=unused&search=ABC&sort_by=value&sort_order=asc", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListRedeemCodes.calls)
	require.Equal(t, "balance", adminSvc.lastListRedeemCodes.codeType)
	require.Equal(t, "unused", adminSvc.lastListRedeemCodes.status)
	require.Equal(t, "ABC", adminSvc.lastListRedeemCodes.search)
	require.Equal(t, "value", adminSvc.lastListRedeemCodes.sortBy)
	require.Equal(t, "asc", adminSvc.lastListRedeemCodes.sortOrder)
}

func TestRedeemExportSortDefaults(t *testing.T) {
	router, adminSvc := setupRedeemExportRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/export", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListRedeemCodes.calls)
	require.Equal(t, "id", adminSvc.lastListRedeemCodes.sortBy)
	require.Equal(t, "desc", adminSvc.lastListRedeemCodes.sortOrder)
}

func TestRedeemExportEscapesFormulaCells(t *testing.T) {
	router, adminSvc := setupRedeemExportRouter()
	adminSvc.redeems = []service.RedeemCode{{
		ID:     1,
		Code:   "=cmd|test",
		Type:   "+balance",
		Value:  10,
		Status: "@unused",
		User:   &service.User{Email: "-user@example.com"},
	}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/export", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "'=cmd|test", rows[1][1])
	require.Equal(t, "'+balance", rows[1][2])
	require.Equal(t, "'@unused", rows[1][4])
	require.Equal(t, "'-user@example.com", rows[1][6])
}

func TestRedeemStatsReturnsServiceStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	adminSvc.redeemStats = &service.RedeemCodeStats{
		TotalCodes:            4,
		ActiveCodes:           1,
		UsedCodes:             2,
		ExpiredCodes:          1,
		TotalValueDistributed: 23.5,
		ByType: map[string]int64{
			service.RedeemTypeBalance:      2,
			service.RedeemTypeSubscription: 1,
			service.RedeemTypeInvitation:   1,
		},
	}

	h := NewRedeemHandler(adminSvc, nil)
	router.GET("/api/v1/admin/redeem-codes/stats", h.GetStats)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Data struct {
			TotalCodes            int64            `json:"total_codes"`
			ActiveCodes           int64            `json:"active_codes"`
			UsedCodes             int64            `json:"used_codes"`
			ExpiredCodes          int64            `json:"expired_codes"`
			TotalValueDistributed float64          `json:"total_value_distributed"`
			ByType                map[string]int64 `json:"by_type"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, int64(4), body.Data.TotalCodes)
	require.Equal(t, int64(1), body.Data.ActiveCodes)
	require.Equal(t, int64(2), body.Data.UsedCodes)
	require.Equal(t, int64(1), body.Data.ExpiredCodes)
	require.Equal(t, 23.5, body.Data.TotalValueDistributed)
	require.Equal(t, int64(2), body.Data.ByType[service.RedeemTypeBalance])
	require.Equal(t, int64(1), body.Data.ByType[service.RedeemTypeSubscription])
	require.Equal(t, int64(1), body.Data.ByType[service.RedeemTypeInvitation])
	require.NotContains(t, body.Data.ByType, "trial")
}
