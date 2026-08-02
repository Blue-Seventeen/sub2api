package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RegisterAuthRoutes registers authentication-related routes.
func RegisterAuthRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth servermiddleware.JWTAuthMiddleware,
	auditLog servermiddleware.AuditLogMiddleware,
	redisClient *redis.Client,
	settingService *service.SettingService,
	cfg *config.Config,
	panelRateLimiter *servermiddleware.PanelRateLimiter,
) {
	rateLimiter := middleware.NewRateLimiter(redisClient)
	authFailureMode := middleware.RateLimitFailClose
	if cfg != nil && cfg.RateLimit.AuthRedisFailureMode == "fail-open" {
		authFailureMode = middleware.RateLimitFailOpen
	}
	authRateLimitOptions := middleware.RateLimitOptions{FailureMode: authFailureMode}
	loginRateLimitOptions := middleware.RateLimitOptions{
		FailureMode: authFailureMode,
		KeySuffix:   middleware.JSONFieldHashSuffix("email"),
	}

	auth := v1.Group("/auth")
	auth.Use(servermiddleware.BackendModeAuthGuard(settingService))
	// 认证事件（登录/注册/2FA/token 刷新失败）入审计
	auth.Use(gin.HandlerFunc(auditLog))
	{
		auth.POST("/register", rateLimiter.LimitWithOptions("auth-register", 5, time.Minute, authRateLimitOptions), h.Auth.Register)
		auth.POST("/login",
			rateLimiter.LimitWithOptions("auth-login-ip", 500, time.Minute, authRateLimitOptions),
			rateLimiter.LimitWithOptions("auth-login", 20, time.Minute, loginRateLimitOptions),
			h.Auth.Login,
		)
		auth.POST("/login/2fa", rateLimiter.LimitWithOptions("auth-login-2fa", 20, time.Minute, authRateLimitOptions), h.Auth.Login2FA)
		auth.POST("/passkey/login/begin", rateLimiter.LimitWithOptions("passkey-login-begin", 20, time.Minute, authRateLimitOptions), h.Passkey.BeginLogin)
		auth.POST("/passkey/login/finish", rateLimiter.LimitWithOptions("passkey-login-finish", 20, time.Minute, authRateLimitOptions), h.Passkey.FinishLogin)
		auth.POST("/send-verify-code", rateLimiter.LimitWithOptions("auth-send-verify-code", 5, time.Minute, authRateLimitOptions), h.Auth.SendVerifyCode)
		auth.POST("/refresh", rateLimiter.LimitWithOptions("refresh-token", 30, time.Minute, authRateLimitOptions), h.Auth.RefreshToken)
		auth.POST("/logout", h.Auth.Logout)
		auth.POST("/validate-promo-code", rateLimiter.LimitWithOptions("validate-promo", 10, time.Minute, authRateLimitOptions), h.Auth.ValidatePromoCode)
		auth.POST("/validate-invitation-code", rateLimiter.LimitWithOptions("validate-invitation", 10, time.Minute, authRateLimitOptions), h.Auth.ValidateInvitationCode)
		auth.POST("/forgot-password", rateLimiter.LimitWithOptions("forgot-password", 5, time.Minute, authRateLimitOptions), h.Auth.ForgotPassword)
		auth.POST("/reset-password", rateLimiter.LimitWithOptions("reset-password", 10, time.Minute, authRateLimitOptions), h.Auth.ResetPassword)
		auth.GET("/oauth/linuxdo/start", h.Auth.LinuxDoOAuthStart)
		auth.GET("/oauth/linuxdo/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.LinuxDoOAuthStart(c)
		})
		auth.GET("/oauth/linuxdo/callback", h.Auth.LinuxDoOAuthCallback)
		auth.GET("/oauth/wechat/start", h.Auth.WeChatOAuthStart)
		auth.GET("/oauth/wechat/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.WeChatOAuthStart(c)
		})
		auth.GET("/oauth/wechat/callback", h.Auth.WeChatOAuthCallback)
		auth.GET("/oauth/wechat/payment/start", h.Auth.WeChatPaymentOAuthStart)
		auth.GET("/oauth/wechat/payment/callback", h.Auth.WeChatPaymentOAuthCallback)
		auth.POST("/oauth/pending/exchange",
			rateLimiter.LimitWithOptions("oauth-pending-exchange", 20, time.Minute, authRateLimitOptions),
			h.Auth.ExchangePendingOAuthCompletion,
		)
		auth.POST("/oauth/pending/send-verify-code",
			rateLimiter.LimitWithOptions("oauth-pending-send-verify-code", 5, time.Minute, authRateLimitOptions),
			h.Auth.SendPendingOAuthVerifyCode,
		)
		auth.POST("/oauth/pending/create-account",
			rateLimiter.LimitWithOptions("oauth-pending-create-account", 10, time.Minute, authRateLimitOptions),
			h.Auth.CreatePendingOAuthAccount,
		)
		auth.POST("/oauth/pending/bind-login",
			rateLimiter.LimitWithOptions("oauth-pending-bind-login", 10, time.Minute, authRateLimitOptions),
			h.Auth.BindPendingOAuthLogin,
		)
		auth.POST("/oauth/linuxdo/complete-registration",
			rateLimiter.LimitWithOptions("oauth-linuxdo-complete", 10, time.Minute, authRateLimitOptions),
			h.Auth.CompleteLinuxDoOAuthRegistration,
		)
		auth.POST("/oauth/linuxdo/bind-login",
			rateLimiter.LimitWithOptions("oauth-linuxdo-bind-login", 20, time.Minute, authRateLimitOptions),
			h.Auth.BindLinuxDoOAuthLogin,
		)
		auth.POST("/oauth/linuxdo/create-account",
			rateLimiter.LimitWithOptions("oauth-linuxdo-create-account", 10, time.Minute, authRateLimitOptions),
			h.Auth.CreateLinuxDoOAuthAccount,
		)
		auth.POST("/oauth/wechat/complete-registration",
			rateLimiter.LimitWithOptions("oauth-wechat-complete", 10, time.Minute, authRateLimitOptions),
			h.Auth.CompleteWeChatOAuthRegistration,
		)
		auth.POST("/oauth/wechat/bind-login",
			rateLimiter.LimitWithOptions("oauth-wechat-bind-login", 20, time.Minute, authRateLimitOptions),
			h.Auth.BindWeChatOAuthLogin,
		)
		auth.POST("/oauth/wechat/create-account",
			rateLimiter.LimitWithOptions("oauth-wechat-create-account", 10, time.Minute, authRateLimitOptions),
			h.Auth.CreateWeChatOAuthAccount,
		)
		auth.GET("/oauth/oidc/start", h.Auth.OIDCOAuthStart)
		auth.GET("/oauth/oidc/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.OIDCOAuthStart(c)
		})
		auth.GET("/oauth/oidc/callback", h.Auth.OIDCOAuthCallback)
		auth.GET("/oauth/github/start", h.Auth.GitHubOAuthStart)
		auth.GET("/oauth/github/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.GitHubOAuthStart(c)
		})
		auth.GET("/oauth/github/callback", h.Auth.GitHubOAuthCallback)
		auth.GET("/oauth/google/start", h.Auth.GoogleOAuthStart)
		auth.GET("/oauth/google/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.GoogleOAuthStart(c)
		})
		auth.GET("/oauth/google/callback", h.Auth.GoogleOAuthCallback)
		auth.POST("/oauth/oidc/complete-registration",
			rateLimiter.LimitWithOptions("oauth-oidc-complete", 10, time.Minute, authRateLimitOptions),
			h.Auth.CompleteOIDCOAuthRegistration,
		)
		auth.POST("/oauth/oidc/bind-login",
			rateLimiter.LimitWithOptions("oauth-oidc-bind-login", 20, time.Minute, authRateLimitOptions),
			h.Auth.BindOIDCOAuthLogin,
		)
		auth.POST("/oauth/oidc/create-account",
			rateLimiter.LimitWithOptions("oauth-oidc-create-account", 10, time.Minute, authRateLimitOptions),
			h.Auth.CreateOIDCOAuthAccount,
		)
		auth.GET("/oauth/dingtalk/start", h.Auth.DingTalkOAuthStart)
		auth.GET("/oauth/dingtalk/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.DingTalkOAuthStart(c)
		})
		auth.GET("/oauth/dingtalk/callback", h.Auth.DingTalkOAuthCallback)
		auth.POST("/oauth/dingtalk/complete-registration",
			rateLimiter.LimitWithOptions("oauth-dingtalk-complete", 10, time.Minute, authRateLimitOptions),
			h.Auth.CompleteDingTalkOAuthRegistration,
		)
		auth.POST("/oauth/dingtalk/bind-login",
			rateLimiter.LimitWithOptions("oauth-dingtalk-bind-login", 20, time.Minute, authRateLimitOptions),
			h.Auth.BindDingTalkOAuthLogin,
		)
		auth.POST("/oauth/dingtalk/create-account",
			rateLimiter.LimitWithOptions("oauth-dingtalk-create-account", 10, time.Minute, authRateLimitOptions),
			h.Auth.CreateDingTalkOAuthAccount,
		)
		auth.POST("/oauth/github/complete-registration",
			rateLimiter.LimitWithOptions("oauth-github-complete", 10, time.Minute, authRateLimitOptions),
			h.Auth.CompleteGitHubOAuthRegistration,
		)
		auth.POST("/oauth/google/complete-registration",
			rateLimiter.LimitWithOptions("oauth-google-complete", 10, time.Minute, authRateLimitOptions),
			h.Auth.CompleteGoogleOAuthRegistration,
		)
	}

	settings := v1.Group("/settings")
	settings.Use(panelRateLimiter.PublicIP())
	{
		settings.GET("/public", h.Setting.GetPublicSettings)
		settings.GET("/email-unsubscribe", h.Setting.UnsubscribeNotificationEmail)
	}

	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(servermiddleware.BackendModeUserGuard(settingService))
	// 面板全局按用户限流
	authenticated.Use(panelRateLimiter.Global())
	{
		authenticated.GET("/auth/me", h.Auth.GetCurrentUser)
		authenticated.POST("/auth/revoke-all-sessions", h.Auth.RevokeAllSessions)
		authenticated.POST("/auth/oauth/bind-token", h.Auth.PrepareOAuthBindAccessTokenCookie)
	}
}
