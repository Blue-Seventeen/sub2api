package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newAuthRoutesTestRouter(redisClient *redis.Client, cfg *config.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	RegisterAuthRoutes(
		v1,
		&handler.Handlers{
			Auth:    &handler.AuthHandler{},
			Setting: &handler.SettingHandler{},
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		redisClient,
		nil,
		cfg,
	)

	return router
}

func TestAuthRoutesRateLimitFailCloseWhenRedisUnavailable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	router := newAuthRoutesTestRouter(rdb, nil)
	paths := []string{
		"/api/v1/auth/register",
		"/api/v1/auth/login",
		"/api/v1/auth/login/2fa",
		"/api/v1/auth/send-verify-code",
		"/api/v1/auth/oauth/pending/send-verify-code",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.10:12345"

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "path=%s", path)
		require.Contains(t, w.Body.String(), "rate limit unavailable", "path=%s", path)
	}
}

func TestAuthRoutesRateLimitFailOpenWhenRedisUnavailable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	router := newAuthRoutesTestRouter(rdb, &config.Config{
		RateLimit: config.RateLimitConfig{AuthRedisFailureMode: "fail-open"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:12345"

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, http.StatusBadRequest, w.Code)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"failopen@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:12345"

	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.NotEqual(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthLoginRouteUsesEmailHashBucket(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	router := newAuthRoutesTestRouter(rdb, nil)

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"a@example.com"}`))
	req1.Header.Set("Content-Type", "application/json")
	req1.RemoteAddr = "203.0.113.10:12345"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.NotEqual(t, http.StatusTooManyRequests, w1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"b@example.com"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "203.0.113.10:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.NotEqual(t, http.StatusTooManyRequests, w2.Code)

	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"a@example.com"}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.RemoteAddr = "203.0.113.10:12345"
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	require.NotEqual(t, http.StatusTooManyRequests, w3.Code)

	keys, err := rdb.Keys(ctx, "rate_limit:auth-login*").Result()
	require.NoError(t, err)

	var ipBucketCount int
	var emailBucketCount int
	for _, key := range keys {
		require.NotContains(t, key, "a@example.com")
		require.NotContains(t, key, "b@example.com")
		if key == "rate_limit:auth-login-ip:203.0.113.10" {
			ipBucketCount++
		}
		if strings.Contains(key, "auth-login:203.0.113.10:email:") {
			require.Len(t, strings.TrimPrefix(key, "rate_limit:auth-login:203.0.113.10:email:"), 24)
			emailBucketCount++
		}
	}
	require.Equal(t, 1, ipBucketCount)
	require.Equal(t, 2, emailBucketCount)
}

func TestAuthLoginSharedRedisCountsAcrossRouters(t *testing.T) {
	server := miniredis.RunT(t)
	rdbA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	rdbB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = rdbA.Close()
		_ = rdbB.Close()
	})

	routerA := newAuthRoutesTestRouter(rdbA, nil)
	routerB := newAuthRoutesTestRouter(rdbB, nil)

	for i := 1; i <= 10; i++ {
		w := postAuthLogin(routerA, "203.0.113.20:12345", "shared@example.com")
		require.Equal(t, http.StatusBadRequest, w.Code, "routerA request %d should pass rate limit", i)
	}
	for i := 1; i <= 10; i++ {
		w := postAuthLogin(routerB, "203.0.113.20:12345", "shared@example.com")
		require.Equal(t, http.StatusBadRequest, w.Code, "routerB request %d should pass rate limit", i)
	}

	w := postAuthLogin(routerA, "203.0.113.20:12345", "shared@example.com")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "rate limit exceeded")
}

func TestAuthLoginSameEmailAndPerIPBuckets(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})
	router := newAuthRoutesTestRouter(rdb, nil)

	for i := 1; i <= 20; i++ {
		w := postAuthLogin(router, "203.0.113.30:12345", "same@example.com")
		require.Equal(t, http.StatusBadRequest, w.Code, "same-email request %d should pass", i)
	}
	w := postAuthLogin(router, "203.0.113.30:12345", "same@example.com")
	require.Equal(t, http.StatusTooManyRequests, w.Code)

	server.FlushAll()
	for i := 1; i <= 500; i++ {
		w = postAuthLogin(router, "203.0.113.30:12345", "user"+strconv.Itoa(i)+"@example.com")
		require.Equal(t, http.StatusBadRequest, w.Code, "per-IP request %d should pass", i)
	}
	w = postAuthLogin(router, "203.0.113.30:12345", "overflow@example.com")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
}

func postAuthLogin(router *gin.Engine, remoteAddr, email string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"`+email+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
