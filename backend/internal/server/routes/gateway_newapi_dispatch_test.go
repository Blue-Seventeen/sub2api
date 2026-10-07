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

type openAINewAPIRouteAccounts struct {
	service.AccountRepository
	account service.Account
}

func (r *openAINewAPIRouteAccounts) ListSchedulableByPlatform(context.Context, string) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

func (r *openAINewAPIRouteAccounts) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

func (r *openAINewAPIRouteAccounts) GetByID(context.Context, int64) (*service.Account, error) {
	return &r.account, nil
}

type openAINewAPIWireRequest struct {
	URL    string
	Header http.Header
	Body   []byte
}

type openAINewAPIRouteUpstream struct {
	service.HTTPUpstream
	requests []openAINewAPIWireRequest
}

func (u *openAINewAPIRouteUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.requests = append(u.requests, openAINewAPIWireRequest{
		URL:    req.URL.String(),
		Header: req.Header.Clone(),
		Body:   body,
	})
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"injected request rejection","type":"invalid_request_error"}}`)),
	}, nil
}

func (u *openAINewAPIRouteUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestOpenAIRoutesHonorNewAPIStyleSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	routes := []struct {
		name      string
		paths     []string
		body      string
		offPath   string
		onPath    string
		offAccept string
		onAccept  string
	}{
		{
			name:      "chat completions",
			paths:     []string{"/v1/chat/completions", "/chat/completions"},
			body:      `{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`,
			offPath:   "/v1/responses",
			onPath:    "/v1/chat/completions",
			offAccept: "",
			onAccept:  "application/json",
		},
		{
			name:      "responses",
			paths:     []string{"/v1/responses", "/responses", "/backend-api/codex/responses"},
			body:      `{"model":"gpt-5.4","input":"hello","stream":false}`,
			offPath:   "/v1/responses",
			onPath:    "/v1/responses",
			offAccept: "",
			onAccept:  "application/json",
		},
	}

	for _, route := range routes {
		for _, path := range route.paths {
			for _, hydrated := range []bool{true, false} {
				for _, groupEnabled := range []bool{false, true} {
					for _, accountEnabled := range []bool{false, true} {
						enabled := groupEnabled || accountEnabled
						name := route.name + "/newapi-disabled"
						wantPath, wantAccept := route.offPath, route.offAccept
						if enabled {
							name = route.name + "/newapi-enabled"
							wantPath, wantAccept = route.onPath, route.onAccept
						}
						if groupEnabled {
							name += "/group-switch"
						}
						if accountEnabled {
							name += "/account-switch"
						}
						if !hydrated {
							name += "/legacy-unhydrated-group"
						}
						name += path
						t.Run(name, func(t *testing.T) {
							group := &service.Group{
								ID:                          121,
								Platform:                    service.PlatformOpenAI,
								Status:                      service.StatusActive,
								Hydrated:                    hydrated,
								NewAPIStyleInterfaceEnabled: groupEnabled,
							}
							accountExtra := map[string]any{"openai_responses_supported": true}
							if accountEnabled {
								accountExtra[service.AccountExtraNewAPIStyleInterfaceEnabled] = true
							}
							accounts := &openAINewAPIRouteAccounts{account: service.Account{
								ID: 221, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
								Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{group.ID},
								Credentials: map[string]any{"api_key": "test-only", "base_url": "http://upstream.example"},
								Extra:       accountExtra,
							}}
							cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{
								MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20,
							}}
							cfg.Default.RateMultiplier = 1
							cfg.Security.URLAllowlist.AllowInsecureHTTP = true
							upstream := &openAINewAPIRouteUpstream{}
							billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
							t.Cleanup(billing.Stop)
							groups := legacyCNRouteGroups{group: group}
							gateway := service.NewGatewayService(accounts, groups, nil, nil, nil, nil, nil, nil, cfg,
								nil, nil, nil, nil, billing, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
							newAPIService := service.NewNewAPIStyleGatewayService(gateway, upstream, cfg, nil, nil)
							openAIGateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, cfg,
								nil, nil, service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{},
								nil, nil, nil, nil, nil, nil, nil)
							openAIHandler := handler.NewOpenAIGatewayHandler(
								openAIGateway, newAPIService, service.NewConcurrencyService(nil), billing,
								service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, cfg,
							)
							base := handler.NewGatewayHandler(gateway, newAPIService, nil, nil, nil, nil, service.NewConcurrencyService(nil), billing,
								nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil)
							router := gin.New()
							RegisterGatewayRoutes(router, &handler.Handlers{
								Gateway: base, OpenAIGateway: openAIHandler,
								CompatibleGateway:  handler.NewCompatibleGatewayHandler(service.NewCompatibleGatewayService(gateway, upstream, cfg, nil), base),
								NewAPIStyleGateway: handler.NewNewAPIStyleGatewayHandler(base), AsyncImage: handler.NewAsyncImageHandler(nil, nil),
							}, servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
								apiKey := &service.APIKey{
									ID: 321, UserID: 421, GroupID: &group.ID, Group: group,
									User: &service.User{ID: 421, Status: service.StatusActive, Balance: 100},
								}
								c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
								c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: apiKey.UserID})
								c.Next()
							}), nil, nil, nil, nil, nil, cfg)

							req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(route.body))
							req.Header.Set("Content-Type", "application/json")
							w := httptest.NewRecorder()
							router.ServeHTTP(w, req)

							require.Len(t, upstream.requests, 1, w.Body.String())
							require.Equal(t, wantPath, strings.TrimPrefix(upstream.requests[0].URL, "http://upstream.example"))
							require.Equal(t, wantAccept, upstream.requests[0].Header.Get("Accept"))
						})
					}
				}
			}
		}
	}
}
