package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimitFailureMode Redis 故障策略
type RateLimitFailureMode int

const (
	RateLimitFailOpen RateLimitFailureMode = iota
	RateLimitFailClose
)

// RateLimitOptions 限流可选配置
type RateLimitOptions struct {
	FailureMode RateLimitFailureMode
	KeySuffix   func(*gin.Context) string
}

var rateLimitScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
local repaired = 0
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
elseif ttl == -1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  repaired = 1
elseif ttl > tonumber(ARGV[1]) then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  repaired = 1
end
return {current, repaired}
`)

// rateLimitRun 允许测试覆写脚本执行逻辑
var rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
	values, err := rateLimitScript.Run(ctx, client, []string{key}, windowMillis).Slice()
	if err != nil {
		return 0, false, err
	}
	if len(values) < 2 {
		return 0, false, fmt.Errorf("rate limit script returned %d values", len(values))
	}
	count, err := parseInt64(values[0])
	if err != nil {
		return 0, false, err
	}
	repaired, err := parseInt64(values[1])
	if err != nil {
		return 0, false, err
	}
	return count, repaired == 1, nil
}

// RateLimiter Redis 速率限制器
type RateLimiter struct {
	redis  *redis.Client
	prefix string
}

// NewRateLimiter 创建速率限制器实例
func NewRateLimiter(redisClient *redis.Client) *RateLimiter {
	return &RateLimiter{
		redis:  redisClient,
		prefix: "rate_limit:",
	}
}

// Limit 返回速率限制中间件
// key: 限制类型标识
// limit: 时间窗口内最大请求数
// window: 时间窗口
func (r *RateLimiter) Limit(key string, limit int, window time.Duration) gin.HandlerFunc {
	return r.LimitWithOptions(key, limit, window, RateLimitOptions{})
}

// LimitWithOptions 返回速率限制中间件（带可选配置）
func (r *RateLimiter) LimitWithOptions(key string, limit int, window time.Duration, opts RateLimitOptions) gin.HandlerFunc {
	failureMode := opts.FailureMode
	if failureMode != RateLimitFailClose {
		failureMode = RateLimitFailOpen
	}

	return func(c *gin.Context) {
		keyParts := []string{r.prefix + key, c.ClientIP()}
		if opts.KeySuffix != nil {
			if suffix := strings.TrimSpace(opts.KeySuffix(c)); suffix != "" {
				keyParts = append(keyParts, suffix)
			}
		}
		redisKey := strings.Join(keyParts, ":")

		ctx := c.Request.Context()

		windowMillis := windowTTLMillis(window)

		// 使用 Lua 脚本原子操作增加计数并设置过期
		count, repaired, err := rateLimitRun(ctx, r.redis, redisKey, windowMillis)
		if err != nil {
			log.Printf("[RateLimit] redis error: key=%s mode=%s err=%v", redisKey, failureModeLabel(failureMode), err)
			if failureMode == RateLimitFailClose {
				abortRateLimitUnavailable(c)
				return
			}
			// Redis 错误时放行，避免影响正常服务
			c.Next()
			return
		}
		if repaired {
			log.Printf("[RateLimit] ttl repaired: key=%s window_ms=%d", redisKey, windowMillis)
		}

		// 超过限制
		if count > int64(limit) {
			abortRateLimit(c)
			return
		}

		c.Next()
	}
}

func windowTTLMillis(window time.Duration) int64 {
	ttl := window.Milliseconds()
	if ttl < 1 {
		return 1
	}
	return ttl
}

// JSONFieldHashSuffix builds a non-reversible suffix from a JSON string field.
// It restores the request body so downstream handlers can bind JSON normally.
func JSONFieldHashSuffix(field string) func(*gin.Context) string {
	return func(c *gin.Context) string {
		value := readJSONFieldForRateLimit(c, field)
		if value == "" {
			return ""
		}
		sum := sha256.Sum256([]byte(strings.ToLower(value)))
		return field + ":" + hex.EncodeToString(sum[:])[:24]
	}
}

func readJSONFieldForRateLimit(c *gin.Context, field string) string {
	if c == nil || c.Request == nil || c.Request.Body == nil || strings.TrimSpace(field) == "" {
		return ""
	}
	limited := io.LimitReader(c.Request.Body, (64<<10)+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		c.Request.Body = io.NopCloser(strings.NewReader(""))
		return ""
	}
	if len(body) > 64<<10 {
		c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), c.Request.Body))
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	raw, ok := payload[field].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}

func abortRateLimit(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":   "rate limit exceeded",
		"message": "Too many requests, please try again later",
	})
}

func abortRateLimitUnavailable(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"error":   "rate limit unavailable",
		"message": "Login protection is temporarily unavailable, please try again later",
	})
}

func failureModeLabel(mode RateLimitFailureMode) string {
	if mode == RateLimitFailClose {
		return "fail-close"
	}
	return "fail-open"
}

func parseInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unexpected value type %T", value)
	}
}
