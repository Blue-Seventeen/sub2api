package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
)

type newAPIStyleOpsHTTPUpstream struct {
	resp *http.Response
	err  error
}

type newAPIStyleTempUnschedCall struct {
	accountID int64
	until     time.Time
	reason    string
}

type newAPIStyleOpenAIAccountRepoStub struct {
	AccountRepository
	tempUnschedCalls []newAPIStyleTempUnschedCall
}

func (r *newAPIStyleOpenAIAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempUnschedCalls = append(r.tempUnschedCalls, newAPIStyleTempUnschedCall{accountID: id, until: until, reason: reason})
	return nil
}

type newAPIStyleRuntimeBlockerStub struct {
	blocks []newAPIStyleTempUnschedCall
}

func (b *newAPIStyleRuntimeBlockerStub) BlockAccountScheduling(account *Account, until time.Time, reason string) {
	if account == nil {
		return
	}
	b.blocks = append(b.blocks, newAPIStyleTempUnschedCall{accountID: account.ID, until: until, reason: reason})
}

func (b *newAPIStyleRuntimeBlockerStub) ClearAccountSchedulingBlock(accountID int64) {}

func (u *newAPIStyleOpsHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (u *newAPIStyleOpsHTTPUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.resp, u.err
}

func TestNewAPIStyleGatewayRecordsOpsEventOnRequestError(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{
		httpUpstream: &newAPIStyleOpsHTTPUpstream{err: errors.New("dial tcp: connection reset by peer")},
	}
	account := newAPIStyleOpsAccount()

	_, _, err := svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[]}`),
		InboundPath: "/v1/chat/completions",
	})
	if err == nil {
		t.Fatalf("Forward() error = nil, want request error")
	}

	events := newAPIStyleOpsEvents(t, c)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Kind != "request_error" {
		t.Fatalf("event kind = %q, want request_error", events[0].Kind)
	}
	if events[0].Platform != PlatformOpenRouter || events[0].AccountID != account.ID {
		t.Fatalf("event account context = platform %q account %d", events[0].Platform, events[0].AccountID)
	}
	if events[0].UpstreamURL == "" {
		t.Fatalf("event upstream URL is empty")
	}
}

func TestNewAPIStyleOpenAITransportErrorFailsOverWithoutEviction(t *testing.T) {
	c := newAPIStyleTestContext()
	repo := &newAPIStyleOpenAIAccountRepoStub{}
	blocker := &newAPIStyleRuntimeBlockerStub{}
	gatewaySvc := &GatewayService{accountRepo: repo}
	svc := &NewAPIStyleGatewayService{
		gatewayService: gatewaySvc,
		httpUpstream:   &newAPIStyleOpsHTTPUpstream{err: errors.New("context deadline exceeded while awaiting headers")},
		runtimeBlocker: blocker,
	}
	account := newAPIStyleOpenAIAccount()

	_, _, err := svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"gpt-5.1","input":"hello"}`),
		InboundPath: "/v1/responses",
	})

	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("Forward() error = %T, want *UpstreamFailoverError", err)
	}
	if failoverErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("failover status = %d, want %d", failoverErr.StatusCode, http.StatusBadGateway)
	}
	if len(repo.tempUnschedCalls) != 0 {
		t.Fatalf("temp unschedule calls = %d, want 0", len(repo.tempUnschedCalls))
	}
	if len(blocker.blocks) != 0 {
		t.Fatalf("runtime block calls = %d, want 0", len(blocker.blocks))
	}

	events := newAPIStyleOpsEvents(t, c)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Kind != "request_error" || events[0].Platform != PlatformOpenAI {
		t.Fatalf("event = kind %q platform %q, want request_error/openai", events[0].Kind, events[0].Platform)
	}
	if events[0].UpstreamURL != "https://api.openai.com/v1/responses" {
		t.Fatalf("event upstream url = %q, want OpenAI responses url", events[0].UpstreamURL)
	}
}

func TestNewAPIStyleOpenAITransportErrorPersistentTempUnschedulesAndFailsOver(t *testing.T) {
	c := newAPIStyleTestContext()
	repo := &newAPIStyleOpenAIAccountRepoStub{}
	blocker := &newAPIStyleRuntimeBlockerStub{}
	gatewaySvc := &GatewayService{accountRepo: repo}
	svc := &NewAPIStyleGatewayService{
		gatewayService: gatewaySvc,
		httpUpstream: &newAPIStyleOpsHTTPUpstream{err: errors.New(
			"socks connect tcp 127.0.0.1:1080->chatgpt.com:443: username/password authentication failed",
		)},
		runtimeBlocker: blocker,
	}
	account := newAPIStyleOpenAIAccount()

	before := time.Now()
	_, _, err := svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}]}`),
		InboundPath: "/v1/chat/completions",
	})
	after := time.Now()

	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("Forward() error = %T, want *UpstreamFailoverError", err)
	}
	if len(repo.tempUnschedCalls) != 1 {
		t.Fatalf("temp unschedule calls = %d, want 1", len(repo.tempUnschedCalls))
	}
	call := repo.tempUnschedCalls[0]
	if call.accountID != account.ID {
		t.Fatalf("temp unschedule account = %d, want %d", call.accountID, account.ID)
	}
	if !strings.Contains(call.reason, "authentication failed") {
		t.Fatalf("temp unschedule reason = %q, want authentication failed", call.reason)
	}
	if call.until.Before(before.Add(openAITransportErrorTempUnschedDuration-time.Second)) ||
		call.until.After(after.Add(openAITransportErrorTempUnschedDuration+time.Second)) {
		t.Fatalf("temp unschedule until = %s, outside expected window", call.until)
	}
	if len(blocker.blocks) != 1 {
		t.Fatalf("runtime block calls = %d, want 1", len(blocker.blocks))
	}
	if blocker.blocks[0].accountID != account.ID || blocker.blocks[0].reason != "transport_error" {
		t.Fatalf("runtime block = account %d reason %q, want %d transport_error", blocker.blocks[0].accountID, blocker.blocks[0].reason, account.ID)
	}
}

func TestNewAPIStyleOpenAITransportErrorContextCanceledDoesNotFailOver(t *testing.T) {
	c := newAPIStyleTestContext()
	repo := &newAPIStyleOpenAIAccountRepoStub{}
	blocker := &newAPIStyleRuntimeBlockerStub{}
	gatewaySvc := &GatewayService{accountRepo: repo}
	svc := &NewAPIStyleGatewayService{
		gatewayService: gatewaySvc,
		httpUpstream:   &newAPIStyleOpsHTTPUpstream{err: fmt.Errorf("request aborted: %w", context.Canceled)},
		runtimeBlocker: blocker,
	}
	account := newAPIStyleOpenAIAccount()

	_, _, err := svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"gpt-5.1","input":"hello"}`),
		InboundPath: "/v1/responses",
	})

	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		t.Fatalf("Forward() error = %T, want non-failover context canceled error", err)
	}
	if len(repo.tempUnschedCalls) != 0 {
		t.Fatalf("temp unschedule calls = %d, want 0", len(repo.tempUnschedCalls))
	}
	if len(blocker.blocks) != 0 {
		t.Fatalf("runtime block calls = %d, want 0", len(blocker.blocks))
	}
}

func TestNewAPIStyleGatewayRecordsOpsEventOnHTTPError(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{
		httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"X-Request-Id": []string{"req_123"}},
			Body:       io.NopCloser(io.LimitReader(&errorBodyReader{}, 1024)),
		}},
	}
	account := newAPIStyleOpsAccount()

	_, _, err := svc.Forward(context.Background(), c, account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[]}`),
		InboundPath: "/v1/chat/completions",
	})
	if err == nil {
		t.Fatalf("Forward() error = nil, want upstream failover error")
	}

	events := newAPIStyleOpsEvents(t, c)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Kind != "http_error" {
		t.Fatalf("event kind = %q, want http_error", events[0].Kind)
	}
	if events[0].UpstreamStatusCode != http.StatusBadGateway {
		t.Fatalf("event status = %d, want %d", events[0].UpstreamStatusCode, http.StatusBadGateway)
	}
	if events[0].UpstreamRequestID != "req_123" {
		t.Fatalf("event request id = %q, want req_123", events[0].UpstreamRequestID)
	}
}

func newAPIStyleTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c
}

func newAPIStyleOpsAccount() *Account {
	return &Account{
		ID:       42,
		Name:     "openrouter-test",
		Platform: PlatformOpenRouter,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key",
		},
	}
}

func newAPIStyleOpenAIAccount() *Account {
	return &Account{
		ID:       84,
		Name:     "openai-newapi-test",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
		Credentials: map[string]any{
			"access_token": "test-token",
		},
	}
}

func newAPIStyleOpsEvents(t *testing.T, c *gin.Context) []*OpsUpstreamErrorEvent {
	t.Helper()
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	if !ok {
		t.Fatalf("ops upstream events key not set")
	}
	events, ok := raw.([]*OpsUpstreamErrorEvent)
	if !ok {
		t.Fatalf("ops upstream events type = %T, want []*OpsUpstreamErrorEvent", raw)
	}
	return events
}

type errorBodyReader struct{}

func (*errorBodyReader) Read(p []byte) (int, error) {
	body := []byte(`{"error":{"message":"bad upstream"}}`)
	copy(p, body)
	return len(body), io.EOF
}
