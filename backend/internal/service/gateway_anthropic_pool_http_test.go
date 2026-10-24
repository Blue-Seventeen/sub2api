//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type anthropicPoolHTTPUpstream struct {
	hits   int
	status int
}

func (u *anthropicPoolHTTPUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.hits++
	body := `{"type":"error","error":{"type":"api_error","message":"fixture unavailable"}}`
	if u.status == http.StatusBadRequest {
		body = `{"type":"error","error":{"type":"invalid_request_error","message":"Missing required parameter: fixture"}}`
	}
	return &http.Response{
		StatusCode: u.status,
		Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"pool-http-attempt"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (u *anthropicPoolHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

type anthropicPoolHTTPSettings struct{ SettingRepository }

func (*anthropicPoolHTTPSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyRectifierSettings {
		return `{"enabled":false}`, nil
	}
	return "", ErrSettingNotFound
}

func (*anthropicPoolHTTPSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func newAnthropicPoolHTTPService(upstream HTTPUpstream) *GatewayService {
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	return &GatewayService{
		cfg: cfg, httpUpstream: upstream, deferredService: &DeferredService{},
		rateLimitService:     &RateLimitService{},
		settingService:       &SettingService{settingRepo: &anthropicPoolHTTPSettings{}},
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
	}
}

func callAnthropicPoolHTTPForward(t *testing.T, route string, svc *GatewayService, account *Account, c *gin.Context) (*ForwardResult, error) {
	t.Helper()
	if route == "chat" {
		return svc.ForwardAsChatCompletions(context.Background(), c, account,
			[]byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hello"}],"stream":true}`), nil)
	}
	if route == "responses" {
		return svc.ForwardAsResponses(context.Background(), c, account,
			[]byte(`{"model":"claude-3-7-sonnet-20250219","input":"hello","stream":true}`), nil)
	}
	body := []byte(`{"model":"claude-3-7-sonnet-20250219","messages":[{"role":"user","content":"hello"}],"max_tokens":16,"stream":true}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)
	return svc.Forward(context.Background(), c, account, parsed)
}

func TestAnthropicPoolHTTPAttemptsBelongToHandler(t *testing.T) {
	for _, route := range []string{"messages", "passthrough", "chat", "responses"} {
		for _, status := range []int{http.StatusServiceUnavailable, http.StatusNotFound} {
			t.Run(route+"/"+strconv.Itoa(status), func(t *testing.T) {
				upstream := &anthropicPoolHTTPUpstream{status: status}
				svc := newAnthropicPoolHTTPService(upstream)
				account := &Account{ID: 9701, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Concurrency: 1,
					Credentials: map[string]any{
						"api_key": "fixture-only", "base_url": "https://api.anthropic.com", "pool_mode": true,
						"pool_mode_retry_count": 10, "pool_mode_retry_status_codes": []any{float64(status)},
					}, Extra: map[string]any{"anthropic_passthrough": route == "passthrough"}}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+route, nil)
				result, err := callAnthropicPoolHTTPForward(t, route, svc, account, c)
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, 1, upstream.hits, "the handler must own every configured same-account attempt")
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, status, failover.StatusCode)
				require.True(t, failover.RetryableOnSameAccount)
				require.False(t, c.Writer.Written(), "a failed attempt must not commit the downstream response")
				require.Empty(t, recorder.Body.String())
			})
		}
	}
}

func TestAnthropicPoolHTTPInvalid400DoesNotReplay(t *testing.T) {
	for _, route := range []string{"messages", "passthrough", "chat", "responses"} {
		t.Run(route, func(t *testing.T) {
			upstream := &anthropicPoolHTTPUpstream{status: http.StatusBadRequest}
			account := &Account{ID: 9702, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "fixture-only", "pool_mode": true,
					"pool_mode_retry_status_codes": []any{float64(400), float64(503)}},
				Extra: map[string]any{"anthropic_passthrough": route == "passthrough"}}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+route, nil)
			result, err := callAnthropicPoolHTTPForward(t, route, newAnthropicPoolHTTPService(upstream), account, c)
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, 1, upstream.hits)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "a real invalid-request 400 stays terminal even when configured in pool retry codes")
			require.True(t, c.Writer.Written())
		})
	}
}

func TestAnthropicPoolHTTPCustomExclusionRemainsTerminal(t *testing.T) {
	for _, route := range []string{"messages", "passthrough", "chat", "responses"} {
		t.Run(route, func(t *testing.T) {
			upstream := &anthropicPoolHTTPUpstream{status: http.StatusServiceUnavailable}
			account := preflightPoolAccount(PlatformAnthropic)
			account.Credentials["custom_error_codes_enabled"] = true
			account.Credentials["custom_error_codes"] = []any{float64(401)}
			account.Extra = map[string]any{"anthropic_passthrough": route == "passthrough"}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+route, nil)
			result, err := callAnthropicPoolHTTPForward(t, route, newAnthropicPoolHTTPService(upstream), account, c)
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, 1, upstream.hits)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "excluded custom codes must not spend pool retries or rotate accounts")
			require.True(t, c.Writer.Written())
		})
	}
}

func TestSystemOnePoolHTTPCustom404ReachesHandler(t *testing.T) {
	upstream := &anthropicPoolHTTPUpstream{status: http.StatusNotFound}
	account := &Account{ID: 9703, Platform: PlatformTypeSafe, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "fixture-only", "base_url": "https://api.example.test", "pool_mode": true,
			"pool_mode_retry_status_codes": []any{float64(404)}}}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", nil)
	result, err := newAnthropicPoolHTTPService(upstream).ForwardSystemOne(context.Background(), c, account,
		[]byte(`{"model":"jev-latest","state":{"text":"hello"},"questions":{"q":{"type":"noul","instructions":"reply"}}}`))
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 1, upstream.hits)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusNotFound, failover.StatusCode)
	require.True(t, failover.RetryableOnSameAccount)
	require.False(t, c.Writer.Written())
}

type anthropicPoolSSEUpstream struct {
	HTTPUpstream
	body string
}

func (u *anthropicPoolSSEUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"failed-prefix"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}
func (u *anthropicPoolSSEUpstream) DoWithTLS(req *http.Request, proxy string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, n)
}
func TestAnthropicPoolHTTPSSEFailureBeforeOutputUsesConfiguredBudget(t *testing.T) {
	for _, route := range []string{"messages", "passthrough", "chat", "responses"} {
		for _, metadata := range []bool{false, true} {
			t.Run(route+"/metadata="+strconv.FormatBool(metadata), func(t *testing.T) {
				body := "event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":503,\"type\":\"api_error\",\"message\":\"busy\"}}\n\n"
				if metadata {
					body = "event: ping\ndata: {\"type\":\"ping\"}\n\n" + body
				}
				upstream := &anthropicPoolSSEUpstream{body: body}
				account := preflightPoolAccount(PlatformAnthropic)
				account.Extra = map[string]any{"anthropic_passthrough": route == "passthrough"}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+route, nil)
				result, err := callAnthropicPoolHTTPForward(t, route, newAnthropicPoolHTTPService(upstream), account, c)
				assertPreflightFailover(t, c, rec, result, err, http.StatusServiceUnavailable)
			})
		}
	}
}
