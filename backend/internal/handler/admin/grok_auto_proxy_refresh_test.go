//go:build unit

package admin

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type grokManagedAuthResolver struct{}

func (grokManagedAuthResolver) ResolveProxyURL(context.Context, int64) (string, error) {
	return "http://proxy.fixture:3128", nil
}
func (grokManagedAuthResolver) GetStatus(int64) service.ManagedProxyRuntimeStatus {
	return service.ManagedProxyRuntimeStatus{}
}
func (grokManagedAuthResolver) Reload(int64) {}

type grokManagedAuthAdmin struct{ *stubAdminService }

func (s *grokManagedAuthAdmin) GetProxy(context.Context, int64) (*service.Proxy, error) {
	id := int64(7)
	return &service.Proxy{ID: 17, SourceType: service.ProxySourceMihomoSubscription, SubscriptionID: &id}, nil
}

type grokManagedAuthClient struct {
	grokOAuthHandlerClient
	url string
}

func (s *grokManagedAuthClient) RefreshToken(_ context.Context, _ string, proxyURL string, _ string) (*xai.TokenResponse, error) {
	s.url = proxyURL
	return &xai.TokenResponse{AccessToken: "fixture", ExpiresIn: 3600}, nil
}

func TestGrokRTImportResolvesManagedProxy(t *testing.T) {
	previous := service.GetDefaultManagedProxyResolver()
	service.SetDefaultManagedProxyResolver(grokManagedAuthResolver{})
	t.Cleanup(func() { service.SetDefaultManagedProxyResolver(previous) })
	client := &grokManagedAuthClient{}
	oauth := service.NewGrokOAuthService(nil, client)
	t.Cleanup(oauth.Stop)
	h := NewGrokOAuthHandler(oauth, &grokManagedAuthAdmin{newStubAdminService()}, nil, nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"refresh_token":"fixture-refresh","proxy_id":17}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.RefreshToken(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "http://proxy.fixture:3128", client.url)
}
