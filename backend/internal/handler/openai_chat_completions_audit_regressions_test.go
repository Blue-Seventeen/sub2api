//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type auditStreamReadErrorUpstream struct {
	service.HTTPUpstream
	paths []string
}

func (u *auditStreamReadErrorUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.paths = append(u.paths, req.URL.Path)
	body := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-read-error\",\"status\":\"in_progress\",\"model\":\"gpt-5.4\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"chat-read-error-rid"}},
		Body:       io.NopCloser(auditUnexpectedEOFReader{Reader: strings.NewReader(body)}),
	}, nil
}

func (u *auditStreamReadErrorUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

type auditUnexpectedEOFReader struct {
	io.Reader
}

func (r auditUnexpectedEOFReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return 0, io.ErrUnexpectedEOF
	}
	return n, err
}

func newOpenAIChatAuditHandler(t *testing.T, groupID, keyID, accountID int64, newAPIStyle bool, upstream service.HTTPUpstream) (*OpenAIGatewayHandler, *service.APIKey, *grokMediaBillingRepo, *grokMediaBillingLogRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxAccountSwitches = 1
	group := &service.Group{
		ID: groupID, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive,
		RateMultiplier: 1, NewAPIStyleInterfaceEnabled: newAPIStyle,
	}
	key := &service.APIKey{
		ID: keyID, UserID: keyID + 1000, GroupID: &group.ID, Group: group,
		User: &service.User{ID: keyID + 1000, Status: service.StatusActive, Balance: 100},
	}
	accounts := []service.Account{{
		ID: accountID, Name: "openai-chat-audit", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		GroupIDs:    []int64{group.ID},
		Credentials: map[string]any{"api_key": "test-key", "base_url": "http://upstream.example"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
	}}
	users := &grokMediaBillingUserRepo{user: key.User}
	billing := &grokMediaBillingRepo{}
	logs := &grokMediaBillingLogRepo{}
	cache := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	gateway := service.NewOpenAIGatewayService(
		&openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}, logs, billing, users, nil, nil, nil,
		cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	var newAPI *service.NewAPIStyleGatewayService
	if newAPIStyle {
		newAPI = service.NewNewAPIStyleGatewayService(nil, upstream, cfg, nil, nil)
	}
	h := NewOpenAIGatewayHandler(gateway, newAPI, service.NewConcurrencyService(nil), cache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, nil, nil, cfg)
	return h, key, billing, logs
}

func newOpenAIChatAuditContext(key *service.APIKey, rec *httptest.ResponseRecorder, requestBody string) *gin.Context {
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, key.Group))
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
	return c
}

func TestOpenAIChatCompletions_StreamReadErrorWritesSpecificErrorTerminal(t *testing.T) {
	upstream := &auditStreamReadErrorUpstream{}
	h, key, _, _ := newOpenAIChatAuditHandler(t, 2311, 3311, 1311, false, upstream)
	rec := httptest.NewRecorder()
	c := newOpenAIChatAuditContext(key, rec, `{"model":"gpt-5.4","stream":true,"messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	body := rec.Body.String()
	require.Equal(t, []string{"/v1/responses"}, upstream.paths)
	require.Contains(t, body, `"code":"upstream_stream_read_error"`)
	require.NotContains(t, body, "Upstream request failed")
}

func TestOpenAIChatCompletions_AlreadyForwardedTerminalErrorIsNotDuplicated(t *testing.T) {
	upstream := &openAIChatTerminalFailureUpstream{body: "data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request_error\",\"message\":\"request rejected\"},\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}\n\n"}
	h, key, _, _ := newOpenAIChatAuditHandler(t, 2312, 3312, 1312, false, upstream)
	rec := httptest.NewRecorder()
	c := newOpenAIChatAuditContext(key, rec, `{"model":"gpt-5.4","stream":true,"messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	body := rec.Body.String()
	require.Equal(t, []string{"/v1/responses"}, upstream.paths)
	require.Equal(t, 1, strings.Count(body, "request rejected"), body)
	require.NotContains(t, body, "Upstream request failed")
}

func TestOpenAIChatCompletions_NewAPIStyleUsageStampsRequestedReasoningEffort(t *testing.T) {
	upstream := &openAIChatTerminalFailureUpstream{body: "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2,\"total_tokens\":9}}\n\ndata: [DONE]\n\n"}
	h, key, billing, logs := newOpenAIChatAuditHandler(t, 2313, 3313, 1313, true, upstream)
	rec := httptest.NewRecorder()
	c := newOpenAIChatAuditContext(key, rec, `{"model":"gpt-5.4","stream":true,"reasoning_effort":"high","messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	require.Len(t, logs.logs, 1)
	require.NotNil(t, logs.logs[0].RequestedReasoningEffort)
	require.Equal(t, "high", *logs.logs[0].RequestedReasoningEffort)
	require.Len(t, billing.commands, 1)
}
