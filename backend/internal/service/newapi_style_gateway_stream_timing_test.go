package service

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type liveHTTPUpstream struct {
	client *http.Client
}

func (u liveHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}

func (u liveHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.client.Do(req)
}

type timedRelayResponse struct {
	status int
	first  string
	body   string
	err    error
}

type timedRelayFirstEvent struct {
	status int
	first  string
	err    error
}

func TestNewAPIStyleStreamingRelayEmitsBeforeUpstreamCompletes(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	firstUpstreamEvent := make(chan struct{})
	releaseTerminalEvent := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseTerminalEvent) }) }
	defer release()

	firstEvent := "event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"first"}` + "\n\n"
	terminalEvent := "event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_timing","usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}` + "\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("upstream test server does not support HTTP flushing")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, firstEvent)
		flusher.Flush()
		close(firstUpstreamEvent)
		<-releaseTerminalEvent
		_, _ = io.WriteString(w, terminalEvent)
		flusher.Flush()
	}))
	defer upstream.Close()

	svc := NewNewAPIStyleGatewayService(nil, liveHTTPUpstream{client: upstream.Client()}, nil, nil, nil)
	account := &Account{
		ID:          91,
		Name:        "stream-timing",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "test-key", "base_url": upstream.URL},
		Extra:       map[string]any{AccountExtraNewAPIStyleInterfaceEnabled: true},
	}

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		_, _, err := svc.Forward(r.Context(), c, account, NewAPIStyleForwardOptions{
			Route:       NewAPIStyleRouteResponses,
			RequestBody: []byte(`{"model":"gpt-5.4","stream":true,"input":"hello"}`),
			Stream:      true,
			Model:       "gpt-5.4",
			Method:      http.MethodPost,
			InboundPath: "/v1/responses",
			ContentType: "application/json",
		})
		if err != nil {
			return
		}
	}))
	defer relay.Close()

	clientFirstEvent := make(chan timedRelayFirstEvent, 1)
	clientResult := make(chan timedRelayResponse, 1)
	go func() {
		resp, err := relay.Client().Post(relay.URL, "application/json", strings.NewReader(`{}`))
		if err != nil {
			clientFirstEvent <- timedRelayFirstEvent{err: err}
			clientResult <- timedRelayResponse{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		reader := bufio.NewReader(resp.Body)
		var first bytes.Buffer
		for {
			line, readErr := reader.ReadString('\n')
			_, _ = first.WriteString(line)
			if line == "\n" || line == "\r\n" || readErr != nil {
				if readErr != nil {
					clientFirstEvent <- timedRelayFirstEvent{status: resp.StatusCode, first: first.String(), err: readErr}
					clientResult <- timedRelayResponse{status: resp.StatusCode, err: readErr}
					return
				}
				break
			}
		}
		clientFirstEvent <- timedRelayFirstEvent{status: resp.StatusCode, first: first.String()}
		body, readErr := io.ReadAll(reader)
		clientResult <- timedRelayResponse{status: resp.StatusCode, first: first.String(), body: string(body), err: readErr}
	}()

	select {
	case <-firstUpstreamEvent:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not emit the first SSE event")
	}

	var firstGot timedRelayFirstEvent
	select {
	case firstGot = <-clientFirstEvent:
		if firstGot.err != nil {
			t.Fatalf("client failed before receiving the first event: %v", firstGot.err)
		}
	case <-time.After(500 * time.Millisecond):
		release()
		var got timedRelayResponse
		select {
		case got = <-clientResult:
		case <-time.After(3 * time.Second):
			t.Fatal("relay did not finish after releasing the upstream terminal event")
		}
		t.Fatalf("client did not receive the first SSE event while upstream was still open; status=%d first=%q body=%q err=%v", got.status, got.first, got.body, got.err)
	}

	require.Equal(t, http.StatusOK, firstGot.status)
	require.Equal(t, firstEvent, firstGot.first)
	require.NotContains(t, firstGot.first, "response.completed", "terminal event must remain behind the upstream gate")

	release()
	var got timedRelayResponse
	select {
	case got = <-clientResult:
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not finish after the upstream terminal event was released")
	}
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.status)
	require.Equal(t, terminalEvent, got.body)
}

var _ HTTPUpstream = liveHTTPUpstream{}
