package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type batchUsageRouteAccountRepo struct {
	service.AccountRepository
	accounts []*service.Account
	ids      []int64
	err      error
}

func (r *batchUsageRouteAccountRepo) GetByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	r.ids = append([]int64(nil), ids...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.accounts, r.err
}

func (r *batchUsageRouteAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for _, account := range r.accounts {
		if account.ID == id {
			return account, nil
		}
	}
	return nil, service.ErrAccountNotFound
}

type batchUsageRouteLogRepo struct{ service.UsageLogRepository }

func (*batchUsageRouteLogRepo) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	return &usagestats.AccountStats{Requests: 3, Tokens: 42, Cost: 0.5, StandardCost: 1, UserCost: 2}, nil
}

type batchUsageRouteUserRepo struct{ service.UserRepository }

func (*batchUsageRouteUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (*batchUsageRouteUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	role := service.RoleAdmin
	if id == 2 {
		role = service.RoleUser
	}
	return &service.User{ID: id, Role: role, Status: service.StatusActive, TokenVersion: 2}, nil
}

func newBatchUsageRoute(t *testing.T, repo *batchUsageRouteAccountRepo, usageOverride ...*service.AccountUsageService) (*gin.Engine, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "batch-usage-test-secret", ExpireHour: 1}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil)
	users := service.NewUserService(&batchUsageRouteUserRepo{}, nil, nil, nil)
	var usage *service.AccountUsageService
	if repo != nil {
		usage = service.NewAccountUsageService(repo, &batchUsageRouteLogRepo{}, nil, nil, nil, nil, nil, nil, service.NewUsageCache(), nil, nil)
	}
	if len(usageOverride) > 0 {
		usage = usageOverride[0]
	}
	account := admin.NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, usage, nil, nil, nil, nil, nil, nil, nil)
	h := &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: account, OAuth: &admin.OAuthHandler{}, OpenAIOAuth: &admin.OpenAIOAuthHandler{},
	}}
	router := gin.New()
	group := router.Group("/api/v1/admin", gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, nil, nil)))
	registerAccountRoutes(group, h, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }))
	adminToken, err := auth.GenerateToken(context.Background(), &service.User{ID: 1, Role: service.RoleAdmin, TokenVersion: 2})
	require.NoError(t, err)
	userToken, err := auth.GenerateToken(context.Background(), &service.User{ID: 2, Role: service.RoleUser, TokenVersion: 2})
	require.NoError(t, err)
	return router, adminToken, userToken
}

func requestBatchUsage(router *gin.Engine, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/usage/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAccountBatchUsageRouteReturnsNativeUsageAndPartialErrors(t *testing.T) {
	repo := &batchUsageRouteAccountRepo{accounts: []*service.Account{
		{ID: 7, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "never-return-this-credential"}},
		{ID: 9, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey},
	}}
	router, token, _ := newBatchUsageRoute(t, repo)
	rec := requestBatchUsage(router, token, `{"account_ids":[7,9,7],"force":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result struct {
		Code int `json:"code"`
		Data struct {
			Usage  map[string]json.RawMessage `json:"usage"`
			Errors map[string]string          `json:"errors"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Zero(t, result.Code)
	require.Equal(t, []int64{7, 9}, repo.ids)
	require.Len(t, result.Data.Usage, 1)
	require.Len(t, result.Data.Errors, 1)
	require.Contains(t, result.Data.Errors["9"], "does not support usage query")
	require.NotContains(t, rec.Body.String(), "never-return-this-credential")
	require.JSONEq(t, `{"source":"passive","five_hour":{"utilization":0,"resets_at":null,"remaining_seconds":0,"window_stats":{"requests":3,"tokens":42,"cost":0.5,"standard_cost":1,"user_cost":2}}}`, string(result.Data.Usage["7"]))

	single := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/7/usage?source=passive", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(single, req)
	require.Equal(t, http.StatusOK, single.Code)
	var singleEnvelope struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(single.Body.Bytes(), &singleEnvelope))
	require.JSONEq(t, string(singleEnvelope.Data), string(result.Data.Usage["7"]))
}

func TestAccountBatchUsageRouteRejectsInvalidOrOversizedRequests(t *testing.T) {
	tooMany := `{"account_ids":[` + strings.Repeat("7,", 100) + `7]}`
	for _, body := range []string{
		`{`, `{}`, `{"account_ids":null}`, `{"account_ids":[]}`,
		`{"account_ids":[0]}`, `{"account_ids":[-1]}`, `{"account_ids":[7,0]}`,
		`{"account_ids":[1.5]}`, `{"account_ids":["7"]}`, `{"account_ids":[7],"force":"yes"}`,
		tooMany, `{"account_ids":[7],"padding":"` + strings.Repeat("x", 65536) + `"}`,
	} {
		t.Run(fmt.Sprintf("input_%d_bytes_%s", len(body), body[:min(len(body), 25)]), func(t *testing.T) {
			repo := &batchUsageRouteAccountRepo{}
			router, token, _ := newBatchUsageRoute(t, repo)
			rec := requestBatchUsage(router, token, body)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Nil(t, repo.ids, "invalid requests must not query accounts")
		})
	}
}

func TestAccountBatchUsageRouteKeepsAdminAuthentication(t *testing.T) {
	repo := &batchUsageRouteAccountRepo{}
	router, _, userToken := newBatchUsageRoute(t, repo)
	for _, tc := range []struct {
		token  string
		status int
	}{{"", http.StatusUnauthorized}, {userToken, http.StatusForbidden}} {
		rec := requestBatchUsage(router, tc.token, `{"account_ids":[7]}`)
		require.Equal(t, tc.status, rec.Code)
		require.Nil(t, repo.ids)
	}
}

func TestAccountBatchUsageRouteEmptyMapsAndMissingAccounts(t *testing.T) {
	repo := &batchUsageRouteAccountRepo{}
	router, token, _ := newBatchUsageRoute(t, repo)
	rec := requestBatchUsage(router, token, `{"account_ids":[404]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	data := result["data"].(map[string]any)
	require.Equal(t, map[string]any{}, data["usage"])
	require.Contains(t, data["errors"].(map[string]any)["404"], "account not found")

	repo.accounts = []*service.Account{{ID: 7, Platform: service.PlatformAnthropic, Type: service.AccountTypeSetupToken}}
	rec = requestBatchUsage(router, token, `{"account_ids":[7]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, map[string]any{}, result["data"].(map[string]any)["errors"])
}

func TestAccountBatchUsageRouteFailsClosedOnUnavailableServiceOrRepository(t *testing.T) {
	router, token, _ := newBatchUsageRoute(t, nil)
	rec := requestBatchUsage(router, token, `{"account_ids":[7]}`)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	repo := &batchUsageRouteAccountRepo{err: errors.New("database failure password=private-database-password")}
	router, token, _ = newBatchUsageRoute(t, repo)
	rec = requestBatchUsage(router, token, `{"account_ids":[7]}`)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "private-database-password")
	require.NotContains(t, rec.Body.String(), `"usage"`)
}

func TestAccountBatchUsageRouteRedactsPerAccountErrors(t *testing.T) {
	repo := &batchUsageRouteAccountRepo{accounts: []*service.Account{{
		ID: 9, Platform: service.PlatformOpenAI, Type: "unsupported access_token=private-upstream-token",
	}}}
	router, token, _ := newBatchUsageRoute(t, repo)
	rec := requestBatchUsage(router, token, `{"account_ids":[9]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "private-upstream-token")
	require.Contains(t, rec.Body.String(), "does not support usage query")
}

func TestAccountBatchUsageRouteRedactsCachedUsageWithoutMutation(t *testing.T) {
	repo := &batchUsageRouteAccountRepo{accounts: []*service.Account{{
		ID: 7, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-only-token"},
		// Invalid proxy protocol fails locally, populating the real degraded-usage cache.
		Proxy: &service.Proxy{Protocol: "invalid", Host: "127.0.0.1", Port: 1},
	}}}
	usageService := service.NewAccountUsageService(repo, nil, nil, nil,
		service.NewAntigravityQuotaFetcher(nil, nil), nil, nil, nil, service.NewUsageCache(), nil, nil)
	cached, err := usageService.GetUsage(context.Background(), 7)
	require.NoError(t, err)
	require.NotEmpty(t, cached.Error)
	cached.Error = "usage API error: password=private-cached-password"
	cached.ForbiddenReason = "forbidden access_token=private-cached-token"
	cached.IsForbidden = true
	original := *cached
	router, token, _ := newBatchUsageRoute(t, repo, usageService)
	for attempt := 0; attempt < 2; attempt++ {
		rec := requestBatchUsage(router, token, `{"account_ids":[7]}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "private-cached-password")
		require.NotContains(t, rec.Body.String(), "private-cached-token")
		var result struct {
			Data struct {
				Usage  map[string]*service.UsageInfo `json:"usage"`
				Errors map[string]string             `json:"errors"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		require.Empty(t, result.Data.Errors)
		require.NotNil(t, result.Data.Usage["7"])
		require.Contains(t, result.Data.Usage["7"].Error, "usage API error")
		require.Contains(t, result.Data.Usage["7"].ForbiddenReason, "forbidden")
		require.True(t, result.Data.Usage["7"].IsForbidden)
		require.Equal(t, original, *cached, "response redaction must not mutate cached usage")
		after, err := usageService.GetUsage(context.Background(), 7)
		require.NoError(t, err)
		require.Same(t, cached, after, "exercise the shared cached pointer")
		require.Equal(t, original, *after)
	}
}
