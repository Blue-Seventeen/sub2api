//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func cachedContentAllowlistRouter(env *cachedBillingEnv) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, env.group))
		c.Set(string(middleware.ContextKeyAPIKey), env.apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: env.apiKey.UserID, Concurrency: 10})
		c.Next()
	})
	router.Use(middleware.GroupModelAllowlist())
	router.PATCH("/v1beta/cachedContents/:cacheID", env.h.GeminiCachedContentsPatch)
	router.GET("/v1beta/cachedContents/:cacheID", env.h.GeminiCachedContentsGet)
	router.DELETE("/v1beta/cachedContents/:cacheID", env.h.GeminiCachedContentsDelete)
	return router
}

func serveCachedContentAllowlist(router *gin.Engine, method, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/v1beta/cachedContents/abc?updateMask=ttl", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestGeminiCachedContentsPatch_DisallowedModelHasNoSideEffects(t *testing.T) {
	env := newCachedBillingEnv(t, 1000, 900)
	env.group.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"gemini-allowed-only"}}
	baseline := time.Now().Add(time.Hour)
	env.seed(baseline)
	router := cachedContentAllowlistRouter(env)

	response := serveCachedContentAllowlist(router, http.MethodPatch, `{"ttl":"86400s"}`)

	require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "not available for this group")
	require.Zero(t, env.upstream.methodCalls(http.MethodPatch), "a removed model must not extend upstream storage")
	require.Empty(t, env.usage.totals(), "a rejected renewal must not create usage charges")
	require.Equal(t, baseline, env.repo.records["abc"].ExpireTime)
	require.False(t, env.repo.pendingMutation(100), "rejected requests must not acquire mutation leases")
}

func TestGeminiCachedContentsPatch_AllowsPublicAliasOutsideMappedModelAllowlist(t *testing.T) {
	env := newCachedBillingEnv(t, 1000, 900)
	env.group.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"gemini-alias"}}
	baseline := time.Now().Add(time.Hour)
	env.seed(baseline)
	env.repo.records["abc"].ChannelUsage = service.ChannelUsageFields{
		ChannelID: 5, OriginalModel: "gemini-alias", ChannelMappedModel: "gemini-2.5-pro",
		BillingModelSource: service.BillingModelSourceRequested,
	}
	require.False(t, env.group.AllowsModel(env.repo.records["abc"].Model), "the canonical upstream model is deliberately absent")
	router := cachedContentAllowlistRouter(env)

	response := serveCachedContentAllowlist(router, http.MethodPatch, `{"ttl":"86400s"}`)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodPatch))
	require.Len(t, env.waitForUsage(t, 1), 1)
	require.True(t, env.repo.records["abc"].ExpireTime.After(baseline))
	require.False(t, env.repo.pendingMutation(100))
}

func TestGeminiCachedContentsGetAndDelete_AllowOwnerCleanupAfterModelRemoved(t *testing.T) {
	env := newCachedBillingEnv(t, 1000, 900)
	env.group.ModelsListConfig = service.GroupModelsListConfig{Enabled: true, Models: []string{"gemini-allowed-only"}}
	env.seed(time.Now().Add(time.Hour))
	router := cachedContentAllowlistRouter(env)

	get := serveCachedContentAllowlist(router, http.MethodGet, "")
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	require.Contains(t, get.Body.String(), "cachedContents/abc")
	require.NotContains(t, get.Body.String(), "cachedContents/up1")

	deleted := serveCachedContentAllowlist(router, http.MethodDelete, "")
	require.Equal(t, http.StatusOK, deleted.Code, deleted.Body.String())
	require.Equal(t, 1, env.upstream.methodCalls(http.MethodDelete))
	require.Contains(t, env.repo.deleted, int64(100))
	require.Empty(t, env.usage.totals(), "reading and removing existing storage must not create a new charge")
}
