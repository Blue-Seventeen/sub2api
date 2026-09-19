package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type legacyCNRouteAccounts struct {
	service.AccountRepository
	account service.Account
}

func (r *legacyCNRouteAccounts) ListSchedulableByPlatform(context.Context, string) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

func (r *legacyCNRouteAccounts) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

func (r *legacyCNRouteAccounts) GetByID(context.Context, int64) (*service.Account, error) {
	return &r.account, nil
}

type legacyCNRouteGroups struct {
	service.GroupRepository
	group *service.Group
}

func (r legacyCNRouteGroups) GetByID(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}

func (r legacyCNRouteGroups) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}

type legacyCNRouteUpstream struct {
	service.HTTPUpstream
	urls []string
}

func (u *legacyCNRouteUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.urls = append(u.urls, req.URL.String())
	return &http.Response{
		StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"test upstream rejection","type":"invalid_request_error"}}`)),
	}, nil
}

func (u *legacyCNRouteUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestGatewayRoutesLegacyCNUnprobedAccountsKeepProviderPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []struct{ platform, baseURL, model, path string }{
		{service.PlatformZhipu, "https://open.bigmodel.cn", "glm-4-plus", "/api/paas/v4/chat/completions"},
		{service.PlatformDeepseek, "https://api.deepseek.com", "deepseek-chat", "/chat/completions"},
	} {
		for _, newAPI := range []bool{false, true} {
			for _, route := range []string{"/v1/responses", "/responses", "/v1/chat/completions"} {
				name := provider.platform + route
				if newAPI {
					name += "/newapi"
				}
				t.Run(name, func(t *testing.T) {
					group := &service.Group{ID: 1, Platform: provider.platform, Status: service.StatusActive, NewAPIStyleInterfaceEnabled: newAPI}
					accounts := &legacyCNRouteAccounts{account: service.Account{
						ID: 7, Platform: provider.platform, Type: service.AccountTypeAPIKey,
						Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{1},
						Credentials: map[string]any{"api_key": "test-only", "base_url": provider.baseURL},
					}}
					require.Empty(t, accounts.account.Extra, "legacy account has no capability probe metadata")
					cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20}}
					upstream := &legacyCNRouteUpstream{}
					billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
					t.Cleanup(billing.Stop)
					gateway := service.NewGatewayService(accounts, legacyCNRouteGroups{group: group}, nil, nil, nil, nil, nil, nil, cfg,
						nil, nil, nil, nil, billing, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
					newAPIService := service.NewNewAPIStyleGatewayService(gateway, upstream, cfg, nil, nil)
					base := handler.NewGatewayHandler(gateway, newAPIService, nil, nil, nil, nil, service.NewConcurrencyService(nil), billing,
						nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil)
					router := gin.New()
					RegisterGatewayRoutes(router, &handler.Handlers{
						Gateway: base, OpenAIGateway: &handler.OpenAIGatewayHandler{},
						CompatibleGateway:  handler.NewCompatibleGatewayHandler(service.NewCompatibleGatewayService(gateway, upstream, cfg, nil), base),
						NewAPIStyleGateway: handler.NewNewAPIStyleGatewayHandler(base), AsyncImage: handler.NewAsyncImageHandler(nil, nil),
					}, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
						c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group, User: &service.User{ID: 1}})
						c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 1})
						c.Next()
					}), nil, nil, nil, nil, nil, cfg)
					body := `{"model":"` + provider.model + `","input":"hello","messages":[{"role":"user","content":"hello"}]}`
					req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					router.ServeHTTP(w, req)
					require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
					wantPath := provider.path
					if newAPI && route == "/v1/chat/completions" {
						wantPath = "/v1/chat/completions"
					}
					require.Equal(t, []string{provider.baseURL + wantPath}, upstream.urls)
				})
			}
		}
	}
}
