package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLiveDisabledReturns503EvenForPreviouslyEnabledGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformComposite} {
		for _, enabled := range []bool{false, true} {
			t.Run(platform+map[bool]string{false: "/false", true: "/true"}[enabled], func(t *testing.T) {
				group := &service.Group{Platform: platform, AllowLive: enabled}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/live", strings.NewReader(`{"sdp":"v=0","session":{"model":"gpt-realtime"}}`))
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 11, UserID: 1, Group: group})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
				// No upstream or billing dependencies: a disabled route must stop here.
				(&OpenAIGatewayHandler{}).Live(c)
				require.Equal(t, http.StatusServiceUnavailable, rec.Code)
				require.Contains(t, rec.Body.String(), "Live is disabled")
				for _, view := range []any{dto.GroupFromService(group), dto.GroupFromServiceAdmin(group)} {
					encoded, err := json.Marshal(view)
					require.NoError(t, err)
					var advertised map[string]any
					require.NoError(t, json.Unmarshal(encoded, &advertised))
					require.Equal(t, false, advertised["allow_live"])
				}
			})
		}
	}
}
