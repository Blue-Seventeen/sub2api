package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
)

type newAPIStyleOpsHTTPUpstream struct {
	resp *http.Response
	err  error
}

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
