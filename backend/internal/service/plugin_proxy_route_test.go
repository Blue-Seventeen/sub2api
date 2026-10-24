package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type proxyRoutePluginStream struct {
	grpc.ClientStream
	ctx      context.Context
	frames   chan *pluginv1.ForwardResponse
	startErr error
	recvErr  error
}

func (s *proxyRoutePluginStream) Context() context.Context { return s.ctx }
func (s *proxyRoutePluginStream) CloseSend() error         { return nil }
func (s *proxyRoutePluginStream) Send(frame *pluginv1.ForwardRequest) error {
	if frame.GetStart() != nil {
		return s.startErr
	}
	return nil
}
func (s *proxyRoutePluginStream) Recv() (*pluginv1.ForwardResponse, error) {
	if s.recvErr != nil {
		return nil, s.recvErr
	}
	select {
	case frame, ok := <-s.frames:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

type proxyRoutePluginClient struct {
	pluginv1.TransportPluginClient
	stream *proxyRoutePluginStream
	err    error
}

func (c *proxyRoutePluginClient) Forward(ctx context.Context, _ ...grpc.CallOption) (pluginv1.TransportPlugin_ForwardClient, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.stream.ctx = ctx
	return c.stream, nil
}

func TestProxyRoutePluginResponseLifetime(t *testing.T) {
	for _, ending := range []string{"EOF", "Close", "cancel", "body error"} {
		t.Run(ending, func(t *testing.T) {
			store := newMemoryProxyActiveUsageStore()
			tracker := NewProxyActiveUsageTracker(store)
			defer tracker.Stop()
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, observation := NewProxyRouteObservation(parent, tracker)
			defer observation.Close()
			proxy := &Proxy{ID: 81, Name: "plugin-node", Protocol: "http", Host: "proxy.example", Port: 3128}
			raw := ResolveProxyURL(ctx, proxy)
			stream := &proxyRoutePluginStream{frames: make(chan *pluginv1.ForwardResponse, 2)}
			stream.frames <- &pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: 200}}}
			runtime := &pluginRuntime{api: &proxyRoutePluginClient{stream: stream}}
			require.True(t, runtime.beginRequest())
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/v1/responses", nil)
			require.NoError(t, err)
			resp, err := runtime.roundTrip(ctx, req, raw, &Account{ID: 52})
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 81, Name: "plugin-node", Host: "proxy.example", Port: 3128}}, ProxyRoutesFromContext(ctx))
			require.Eventually(t, func() bool {
				count := tracker.GetActiveUsageDetails(context.Background(), []int64{81})[81]
				return count.RequestCount != nil && *count.RequestCount == 1
			}, time.Second, time.Millisecond)
			switch ending {
			case "EOF":
				stream.frames <- &pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{}}}
				_, err = io.ReadAll(resp.Body)
				require.NoError(t, err)
			case "Close":
				require.NoError(t, resp.Body.Close())
			case "cancel":
				cancel()
			case "body error":
				stream.frames <- &pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "fixture", Message: "body failed", RequestSent: true}}}
				_, err = io.ReadAll(resp.Body)
				require.Error(t, err)
			}
			require.Eventually(t, func() bool {
				count := tracker.GetActiveUsageDetails(context.Background(), []int64{81})[81]
				return count.RequestCount != nil && *count.RequestCount == 0 && count.AccountCount != nil && *count.AccountCount == 0
			}, time.Second, time.Millisecond)
		})
	}
}

func TestProxyRoutePluginFailuresReleaseAttempt(t *testing.T) {
	for _, phase := range []string{"stream open", "send", "receive", "invalid headers", "plugin error"} {
		t.Run(phase, func(t *testing.T) {
			tracker := NewProxyActiveUsageTracker(newMemoryProxyActiveUsageStore())
			defer tracker.Stop()
			ctx, observation := NewProxyRouteObservation(t.Context(), tracker)
			defer observation.Close()
			raw := ResolveProxyURL(ctx, &Proxy{ID: 82, Name: "plugin-node", Protocol: "socks5", Host: "proxy.example", Port: 1080})
			stream := &proxyRoutePluginStream{frames: make(chan *pluginv1.ForwardResponse, 1)}
			api := &proxyRoutePluginClient{stream: stream}
			switch phase {
			case "stream open":
				api.err = errors.New("stream unavailable")
			case "send":
				stream.startErr = errors.New("send failed")
			case "receive":
				stream.recvErr = errors.New("receive failed")
			case "invalid headers":
				stream.frames <- &pluginv1.ForwardResponse{}
			case "plugin error":
				stream.frames <- &pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "fixture", Message: "forward failed"}}}
			}
			runtime := &pluginRuntime{api: api}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/v1/responses", nil)
			require.NoError(t, err)
			_, err = runtime.roundTrip(ctx, req, raw, &Account{ID: 52})
			require.Error(t, err)
			if phase == "stream open" {
				require.Empty(t, ProxyRoutesFromContext(ctx), "opening the local RPC stream alone is not an outbound attempt")
			} else {
				require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 82, Name: "plugin-node", Host: "proxy.example", Port: 1080}}, ProxyRoutesFromContext(ctx))
			}
			require.Eventually(t, func() bool {
				count := tracker.GetActiveUsageDetails(context.Background(), []int64{82})[82]
				return count.RequestCount != nil && *count.RequestCount == 0
			}, time.Second, time.Millisecond)
		})
	}
}
