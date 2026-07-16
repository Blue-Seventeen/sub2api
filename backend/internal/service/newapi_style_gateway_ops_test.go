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

	"github.com/Wei-Shaw/sub2api/internal/config"
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
	transportErr := errors.New("dial tcp api.private.example:443 (10.20.30.40): Authorization: Bearer sk-upstream-secret")
	svc := &NewAPIStyleGatewayService{
		httpUpstream: &newAPIStyleOpsHTTPUpstream{err: transportErr},
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
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("Forward() error = %T, want *UpstreamFailoverError", err)
	}
	if failoverErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("failover status = %d, want %d", failoverErr.StatusCode, http.StatusBadGateway)
	}
	for _, secret := range []string{"api.private.example", "10.20.30.40", "sk-upstream-secret"} {
		if strings.Contains(string(failoverErr.ResponseBody), secret) {
			t.Fatalf("failover response leaked %q: %s", secret, failoverErr.ResponseBody)
		}
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
	if !strings.Contains(events[0].Message, "api.private.example") || !strings.Contains(events[0].Message, "10.20.30.40") {
		t.Fatalf("ops event message = %q, want existing network diagnostics", events[0].Message)
	}
	if strings.Contains(events[0].Message, "sk-upstream-secret") {
		t.Fatalf("ops event message leaked credential: %q", events[0].Message)
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
		t.Fatalf("event upstream url = %q, want diagnostic OpenAI responses url", events[0].UpstreamURL)
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
	if events[0].Kind != "failover" {
		t.Fatalf("event kind = %q, want failover", events[0].Kind)
	}
	if events[0].UpstreamStatusCode != http.StatusBadGateway {
		t.Fatalf("event status = %d, want %d", events[0].UpstreamStatusCode, http.StatusBadGateway)
	}
	if events[0].UpstreamRequestID != "req_123" {
		t.Fatalf("event request id = %q, want req_123", events[0].UpstreamRequestID)
	}
}

func TestNewAPIStyleNonRetryable4xxDoesNotFailOver(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid input"}}`)),
	}}}

	_, _, err := svc.Forward(context.Background(), c, newAPIStyleOpsAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"image-model","prompt":"hello"}`),
		InboundPath: "/v1/chat/completions",
	})
	var clientErr *CompatibleClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("Forward() error = %T, want *CompatibleClientError", err)
	}
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		t.Fatalf("non-retryable 422 must not fail over")
	}
}

func TestNewAPIStyleBaseURLUsesGatewaySecurityPolicy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"allowed.example"}
	svc := &NewAPIStyleGatewayService{
		gatewayService: &GatewayService{cfg: cfg},
		httpUpstream:   &newAPIStyleOpsHTTPUpstream{},
		cfg:            cfg,
	}
	account := newAPIStyleOpsAccount()
	account.Credentials["base_url"] = "http://127.0.0.1:8080"

	_, _, err := svc.Forward(context.Background(), newAPIStyleTestContext(), account, NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[]}`),
		InboundPath: "/v1/chat/completions",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid base_url") {
		t.Fatalf("Forward() error = %v, want URL security rejection", err)
	}
}

func TestNewAPIStyleStreamReadErrorBeforeOutputFailsOver(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(&newAPIStyleFailingReader{}),
	}}}

	_, _, err := svc.Forward(context.Background(), c, newAPIStyleOpsAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/chat/completions",
	})
	var failoverErr *UpstreamFailoverError
	if !errors.As(err, &failoverErr) {
		t.Fatalf("Forward() error = %T, want *UpstreamFailoverError", err)
	}
}

func TestNewAPIStyleStreamReadErrorAfterOutputReturnsError(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(&newAPIStyleFailingReader{payload: []byte(
			"event: response.created\ndata: {\"type\":\"response.created\"}\n\n",
		)}),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/chat/completions",
	})
	if err == nil {
		t.Fatal("Forward() error = nil, want stream read error")
	}
	if result != nil {
		t.Fatalf("Forward() result = %+v, want nil", result)
	}
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		t.Fatalf("partial stream must not fail over")
	}
}

func TestNewAPIStyleStreamReadErrorAfterTerminalDoesNotAppendFailure(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(&newAPIStyleFailingReader{payload: []byte(
			"data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n",
		)}),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[],"stream":true}`),
		Stream:      true,
		InboundPath: "/v1/chat/completions",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v, want terminal stream success", err)
	}
	if result == nil || result.SkipUsageBilling {
		t.Fatalf("result = %+v, want billable terminal stream", result)
	}
}

func TestNewAPIStyleStreamTerminalStateAcceptsSpacedJSON(t *testing.T) {
	tests := []struct {
		name       string
		route      NewAPIStyleRoute
		payload    string
		terminal   bool
		successful bool
	}{
		{name: "responses completed", route: NewAPIStyleRouteResponses, payload: "data: {\"type\": \"response.completed\"}\n\n", terminal: true, successful: true},
		{name: "responses failed", route: NewAPIStyleRouteResponses, payload: "data: {\"type\": \"response.failed\"}\n\n", terminal: true, successful: false},
		{name: "messages stop", route: NewAPIStyleRouteMessages, payload: "data: {\"type\": \"message_stop\"}\n\n", terminal: true, successful: true},
		{name: "messages error", route: NewAPIStyleRouteMessages, payload: "event: error\ndata: {\"type\": \"error\"}\n\n", terminal: true, successful: false},
		{name: "chat done", route: NewAPIStyleRouteChatCompletions, payload: "data: [DONE]\n\n", terminal: true, successful: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminal, successful := newAPIStyleStreamTerminalState(tc.route, []byte(tc.payload))
			if terminal != tc.terminal || successful != tc.successful {
				t.Fatalf("terminal state = (%v, %v), want (%v, %v)", terminal, successful, tc.terminal, tc.successful)
			}
		})
	}
}

func TestNewAPIStyleStreamCleanEOFWithoutTerminalReturnsError(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.created\ndata: {\"type\": \"response.created\"}\n\n",
		)),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err == nil {
		t.Fatal("Forward() error = nil, want missing terminal error")
	}
	if result != nil {
		t.Fatalf("Forward() result = %+v, want nil", result)
	}
}

func TestNewAPIStyleStreamCleanEOFWithSpacedTerminalIsBillable(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.completed\ndata: {\"type\": \"response.completed\", \"response\": {\"usage\": {\"input_tokens\": 3, \"output_tokens\": 2}}}\n\n",
		)),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if result == nil || result.SkipUsageBilling {
		t.Fatalf("Forward() result = %+v, want billable terminal stream", result)
	}
}

func TestNewAPIStyleStreamLargeCompletedEventIsBillable(t *testing.T) {
	c := newAPIStyleTestContext()
	largeOutput := strings.Repeat("x", 300<<10)
	payload := fmt.Sprintf(
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":2},\"output\":[{\"type\":\"message\",\"content\":%q}]}}\n\n",
		largeOutput,
	)
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if result == nil || result.SkipUsageBilling {
		t.Fatalf("Forward() result = %+v, want billable terminal stream", result)
	}
	if result.Usage.InputTokens != 3 || result.Usage.OutputTokens != 2 {
		t.Fatalf("Forward() usage = %+v, want input=3 output=2", result.Usage)
	}
}

func TestNewAPIStyleStreamDataOnlyLargeCompletedEventWithCRLFIsBillable(t *testing.T) {
	c := newAPIStyleTestContext()
	payload := fmt.Sprintf(
		"data: {\"output\":%q,\"type\":\"response.completed\"}\r\n\r\n",
		strings.Repeat("x", 300<<10),
	)
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if result == nil || result.SkipUsageBilling {
		t.Fatalf("Forward() result = %+v, want billable terminal stream", result)
	}
}

func TestNewAPIStyleStreamDataOnlyTerminalAfterExactPrefixChunkIsBillable(t *testing.T) {
	c := newAPIStyleTestContext()
	prefix := "data: {\"output\":\""
	firstChunk := prefix + strings.Repeat("x", maxNewAPIStyleTerminalLinePrefixBytes-len(prefix))
	secondChunk := "\",\"type\":\"response.completed\"}\n\n"
	body := io.MultiReader(strings.NewReader(firstChunk), strings.NewReader(secondChunk))
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(body),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v", err)
	}
	if result == nil || result.SkipUsageBilling {
		t.Fatalf("Forward() result = %+v, want billable terminal stream", result)
	}
}

func TestNewAPIStyleStreamNestedTerminalTypeDoesNotCompleteEvent(t *testing.T) {
	c := newAPIStyleTestContext()
	payload := "event: response.output_item.done\n" +
		"data: {\"type\":\"response.output_item.done\",\"output\":{" +
		strings.Repeat("\"filler\":\"x\",", maxNewAPIStyleTerminalLinePrefixBytes/13) +
		"\"nested\":{\"type\":\"response.completed\"}}}\n\n"
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err == nil {
		t.Fatal("Forward() error = nil, want missing terminal error")
	}
	if result != nil {
		t.Fatalf("Forward() result = %+v, want nil", result)
	}
}

func TestNewAPIStyleStreamIncompleteCompletedEventIsNotBillable(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":3",
		)),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err == nil {
		t.Fatal("Forward() error = nil, want incomplete terminal event error")
	}
	if result != nil {
		t.Fatalf("Forward() result = %+v, want nil", result)
	}
}

func TestNewAPIStyleStreamRequestValidatesTerminalWithoutSSEContentType(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body: io.NopCloser(strings.NewReader(
			"event: response.created\ndata: {\"type\":\"response.created\"}\n\n",
		)),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpenAIAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteResponses,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","input":"hello","stream":true}`),
		Stream:      true,
		InboundPath: "/v1/responses",
	})
	if err == nil {
		t.Fatal("Forward() error = nil, want missing terminal error")
	}
	if result != nil {
		t.Fatalf("Forward() result = %+v, want nil", result)
	}
}

func TestNewAPIStyleBinaryStreamReadErrorDoesNotAppendSSE(t *testing.T) {
	c := newAPIStyleTestContext()
	svc := &NewAPIStyleGatewayService{httpUpstream: &newAPIStyleOpsHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"audio/mpeg"}},
		Body:       io.NopCloser(&newAPIStyleFailingReader{payload: []byte("audio-bytes")}),
	}}}

	result, _, err := svc.Forward(context.Background(), c, newAPIStyleOpsAccount(), NewAPIStyleForwardOptions{
		Route:       NewAPIStyleRouteChatCompletions,
		Method:      http.MethodPost,
		RequestBody: []byte(`{"model":"openai/gpt-4o-mini","messages":[],"stream":true}`),
		Stream:      true,
		InboundPath: "/v1/chat/completions",
	})
	if err != nil {
		t.Fatalf("Forward() error = %v, binary partial stream cannot carry an SSE failure", err)
	}
	if result == nil || !result.SkipUsageBilling {
		t.Fatalf("result = %+v, want abnormal binary stream billing guard", result)
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

type newAPIStyleFailingReader struct {
	payload []byte
	sent    bool
}

func (r *newAPIStyleFailingReader) Read(p []byte) (int, error) {
	if !r.sent && len(r.payload) > 0 {
		r.sent = true
		return copy(p, r.payload), io.ErrUnexpectedEOF
	}
	return 0, io.ErrUnexpectedEOF
}
