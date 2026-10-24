package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPassthroughLifecycle_OpsProxyRouteLocalRejectionStartsEmptyTurn(t *testing.T) {
	for _, test := range []struct {
		name        string
		messageType coderws.MessageType
		beforeTurn  bool
	}{
		{name: "before_request_text", messageType: coderws.MessageText},
		{name: "before_turn_binary", messageType: coderws.MessageBinary, beforeTurn: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, observation := NewProxyRouteObservation(context.Background(), nil)
			defer observation.Close()
			upstream := newStagedPassthroughConn()
			account := passthroughLifecycleAccount()
			account.Proxy = &Proxy{ID: 41, Name: "upstream-only", Protocol: "http", Host: "proxy.test", Port: 8080}
			rejected := make(chan OpsStreamError, 1)
			rejection := errors.New("local turn policy rejected")
			server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), account, func(c *gin.Context) *OpenAIWSIngressHooks {
				SetOpenAIClientTransport(c, OpenAIClientTransportWS)
				reject := func() error {
					MarkOpsStreamFailure(c, "permission_error", "local_policy", rejection.Error(), http.StatusForbidden)
					errorsForRequest := GetOpsStreamErrors(c)
					rejected <- errorsForRequest[len(errorsForRequest)-1]
					return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, rejection.Error(), rejection)
				}
				return &OpenAIWSIngressHooks{
					BeforeRequest: func(_ int, _ []byte, _ string) error {
						if !test.beforeTurn {
							return reject()
						}
						return nil
					},
					BeforeTurn: func(_ int) error {
						if test.beforeTurn {
							return reject()
						}
						return nil
					},
				}
			})
			defer server.Close()
			client := dialPassthroughLifecycleClient(t, server)
			defer func() { _ = client.CloseNow() }()
			requirePassthroughUpstreamWrite(t, upstream, time.Second)
			upstream.Send(`{"type":"response.completed","response":{"id":"first","model":"gpt-5.1"}}`)
			_, err := readPassthroughLifecycleFrame(t, client, time.Second)
			require.NoError(t, err)
			require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 41, Name: "upstream-only", Host: "proxy.test", Port: 8080}}, ProxyRoutesFromContext(ctx))
			writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			err = client.Write(writeCtx, test.messageType, []byte(`{"type":"response.create","model":"gpt-5.1"}`))
			cancel()
			require.NoError(t, err)
			select {
			case marked := <-rejected:
				require.True(t, marked.ProxyRoutesCaptured)
				require.Empty(t, marked.ProxyRoutes, "a rejected turn made no outbound attempt and must not inherit the preceding turn's route")
				require.Equal(t, 2, marked.Turn)
			case <-time.After(time.Second):
				t.Fatal("follow-up request did not reach its local rejection hook")
			}
			_, _ = readPassthroughLifecycleFrame(t, client, time.Second)
			select {
			case err := <-serverErr:
				require.ErrorIs(t, err, rejection)
			case <-time.After(time.Second):
				t.Fatal("local rejection did not terminate the relay")
			}
			select {
			case payload := <-upstream.writes:
				t.Fatalf("locally rejected turn was forwarded upstream: %s", payload)
			default:
			}
		})
	}
}

func TestPassthroughLifecycle_OpsProxyRouteVisibleFailuresFreezeEachTurn(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	upstream := newStagedPassthroughConn()
	account := passthroughLifecycleAccount()
	account.Proxy = &Proxy{ID: 42, Name: "at-dial", Protocol: "http", Host: "proxy.test", Port: 8080}
	ginContext := make(chan *gin.Context, 1)
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), account, func(c *gin.Context) *OpenAIWSIngressHooks {
		SetOpenAIClientTransport(c, OpenAIClientTransportWS)
		ginContext <- c
		return nil
	})
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = client.CloseNow() }()
	c := <-ginContext
	for turn, terminal := range []string{
		`{"type":"response.failed","response":{"id":"first","model":"gpt-5.1","error":{"code":"invalid_request_error","message":"first failed"}}}`,
		`{"type":"response.failed","response":{"id":"second","model":"gpt-5.1","error":{"code":"invalid_request_error","message":"second failed"}}}`,
	} {
		if turn > 0 {
			writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`))
			cancel()
			require.NoError(t, err)
		}
		requirePassthroughUpstreamWrite(t, upstream, time.Second)
		upstream.Send(terminal)
		_, err := readPassthroughLifecycleFrame(t, client, time.Second)
		require.NoError(t, err)
	}
	// A graceful client close intentionally drains upstream for several
	// seconds. Finish the staged upstream explicitly so this test waits for
	// the observation to settle rather than depending on the drain timeout.
	upstream.Fail(io.EOF)
	_ = client.CloseNow()
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("passthrough relay did not stop")
	}
	ResetProxyRouteUsage(ctx)
	SeedProxyRouteUsage(ctx, []ProxyRouteSnapshot{{Kind: "direct"}})
	marked := GetOpsStreamErrors(c)
	require.Len(t, marked, 2, "real downstream error frames must mark distinct WS turns without hooks injecting the error records")
	for index, streamErr := range marked {
		require.Equal(t, index+1, streamErr.Turn)
		require.True(t, streamErr.ProxyRoutesCaptured)
		require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 42, Name: "at-dial", Host: "proxy.test", Port: 8080}}, streamErr.ProxyRoutes)
	}
}

func TestPassthroughLifecycle_OpsProxyRouteOverlappingCreatePreservesActiveTurn(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	upstream := newStagedPassthroughConn()
	account := passthroughLifecycleAccount()
	account.Proxy = &Proxy{ID: 43, Name: "still-active", Protocol: "http", Host: "proxy.test", Port: 8080}
	ginContext := make(chan *gin.Context, 1)
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), account, func(c *gin.Context) *OpenAIWSIngressHooks {
		SetOpenAIClientTransport(c, OpenAIClientTransportWS)
		ginContext <- c
		return nil
	})
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = client.CloseNow() }()
	c := <-ginContext
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"response.created","response":{"id":"active","model":"gpt-5.1"}}`)
	_, err := readPassthroughLifecycleFrame(t, client, time.Second)
	require.NoError(t, err)
	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	err = client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`))
	cancel()
	require.NoError(t, err)
	_, _ = readPassthroughLifecycleFrame(t, client, time.Second)
	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Contains(t, closeErr.Reason(), "overlapping response.create")
	case <-time.After(time.Second):
		t.Fatal("overlapping create did not terminate the relay")
	}
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 43, Name: "still-active", Host: "proxy.test", Port: 8080}}, ProxyRoutesFromContext(ctx))
	turn, ok := c.Get(OpsStreamTurnKey)
	require.True(t, ok)
	require.Equal(t, 1, turn, "an overlapping create must not open or reset a second business turn")
	select {
	case payload := <-upstream.writes:
		t.Fatalf("overlapping response.create was forwarded upstream: %s", payload)
	default:
	}
}

func TestPassthroughLifecycle_OpsProxyRouteCyberConsumedBeforeWriteDoesNotDuplicate(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	upstream := newStagedPassthroughConn()
	account := passthroughLifecycleAccount()
	account.Proxy = &Proxy{ID: 44, Name: "cyber-then-ordinary", Protocol: "http", Host: "proxy.test", Port: 8080}
	ginContext := make(chan *gin.Context, 1)
	consumedCyber := make(chan CyberPolicyMark, 2)
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream), account, func(c *gin.Context) *OpenAIWSIngressHooks {
		SetOpenAIClientTransport(c, OpenAIClientTransportWS)
		ginContext <- c
		return &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, _ error) {
			if mark := GetOpsCyberPolicy(c); mark != nil {
				// The real handler records the dedicated Cyber Ops row here,
				// then clears the mark before the terminal is written.
				consumedCyber <- *mark
				ClearOpsCyberPolicy(c)
			}
		}}
	})
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = client.CloseNow() }()
	c := <-ginContext
	for turn, terminal := range []string{
		`{"type":"response.failed","response":{"id":"cyber","model":"gpt-5.1","error":{"code":"cyber_policy","message":"dedicated cyber failure"}}}`,
		`{"type":"response.failed","response":{"id":"ordinary","model":"gpt-5.1","error":{"code":"invalid_request_error","message":"ordinary second failed"}}}`,
	} {
		if turn > 0 {
			writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`))
			cancel()
			require.NoError(t, err)
		}
		requirePassthroughUpstreamWrite(t, upstream, time.Second)
		upstream.Send(terminal)
		_, err := readPassthroughLifecycleFrame(t, client, time.Second)
		require.NoError(t, err)
	}
	upstream.Fail(io.EOF)
	_ = client.CloseNow()
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("passthrough relay did not stop")
	}
	require.Len(t, consumedCyber, 1)
	require.Nil(t, GetOpsCyberPolicy(c), "the ordinary marker cannot rely on a mark already consumed by AfterTurn")
	marked := GetOpsStreamErrors(c)
	require.Len(t, marked, 1, "the Cyber failure already has its dedicated Ops record")
	require.Equal(t, 2, marked[0].Turn, "skipping Cyber must not suppress a later ordinary failure")
	require.Equal(t, "ordinary second failed", marked[0].Message)
	require.True(t, marked[0].ProxyRoutesCaptured)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 44, Name: "cyber-then-ordinary", Host: "proxy.test", Port: 8080}}, marked[0].ProxyRoutes)
}
