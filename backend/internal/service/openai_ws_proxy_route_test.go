package service

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSProxyRouteLeaseCountsOnlyActiveTurns(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
	defer tracker.Stop()
	ctx, observation := NewProxyRouteObservation(context.Background(), tracker)
	defer observation.Close()
	proxy := &Proxy{ID: 31, Name: "original", Protocol: "http", Host: "proxy.test", Port: 8080}
	proxyURL := ResolveProxyURL(ctx, proxy)
	pool := newOpenAIWSConnPool(&config.Config{})
	pool.clientDialer = &openAIWSCountingDialer{}
	conn, err := pool.dialConn(ctx, openAIWSAcquireRequest{Account: &Account{ID: 71}, WSURL: "wss://upstream.test/ws", ProxyURL: proxyURL})
	require.NoError(t, err)
	defer conn.close()
	waitForProxyActiveUsageCount(t, tracker, 31, 0)
	ResetProxyRouteUsage(ctx)
	proxy.Name = "renamed"
	proxy.Host = "changed.test"
	ResolveProxyURL(ctx, proxy)

	lease := &openAIWSConnLease{accountID: 71, conn: conn}
	lease.BeginProxyTurn(ctx)
	lease.BeginProxyTurn(ctx)
	waitForProxyActiveUsageCount(t, tracker, 31, 1)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 31, Name: "original", Host: "proxy.test", Port: 8080}}, ProxyRoutesFromContext(ctx))
	lease.EndProxyTurn()
	lease.EndProxyTurn()
	waitForProxyActiveUsageCount(t, tracker, 31, 0)

	// Keeping the connection leased between client turns must remain idle.
	ResetProxyRouteUsage(ctx)
	require.Empty(t, ProxyRoutesFromContext(ctx))
	lease.BeginProxyTurn(ctx)
	waitForProxyActiveUsageCount(t, tracker, 31, 1)
	lease.Release()
	waitForProxyActiveUsageCount(t, tracker, 31, 0)
	lease.BeginProxyTurn(ctx)
	waitForProxyActiveUsageCount(t, tracker, 31, 0)
}

func TestOpenAIWSProxyRouteSystemProxyUsesActualTransportDecision(t *testing.T) {
	original := http.DefaultClient.Transport
	defer func() { http.DefaultClient.Transport = original }()
	http.DefaultClient.Transport = &http.Transport{Proxy: func(req *http.Request) (*url.URL, error) {
		if req.URL.Scheme == "https" && req.URL.Host == "proxied.test" {
			return url.Parse("http://system-proxy.test:8080")
		}
		return nil, nil
	}}
	dialer := newDefaultOpenAIWSClientDialer()
	require.True(t, openAIWSUsesEnvironmentProxy(dialer, "wss://proxied.test/ws", ""))
	require.False(t, openAIWSUsesEnvironmentProxy(dialer, "wss://bypassed.test/ws", ""))
	require.False(t, openAIWSUsesEnvironmentProxy(dialer, "wss://proxied.test/ws", "http://explicit.test:8080"))
}

func TestOpenAIWSProxyRoutePassthroughStopsDuringInterTurnIdle(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
	defer tracker.Stop()
	ctx, observation := NewProxyRouteObservation(context.Background(), tracker)
	defer observation.Close()
	upstream := newStagedPassthroughConn()
	account := passthroughLifecycleAccount()
	account.Proxy = &Proxy{ID: 33, Name: "passthrough", Protocol: "http", Host: "proxy.test", Port: 8080}
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	server, _ := startPassthroughLifecycleServer(t, ctx, svc, account)
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer client.CloseNow()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	waitForProxyActiveUsageCount(t, tracker, 33, 1)
	upstream.Send(`{"type":"response.completed","response":{"id":"first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	_, err := readPassthroughLifecycleFrame(t, client, time.Second)
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 33, 0)

	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`)))
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	waitForProxyActiveUsageCount(t, tracker, 33, 1)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 33, Name: "passthrough", Host: "proxy.test", Port: 8080}}, ProxyRoutesFromContext(ctx))
	upstream.Send(`{"type":"response.completed","response":{"id":"second","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	_, err = readPassthroughLifecycleFrame(t, client, time.Second)
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 33, 0)
	// A terminal still ends the actual turn if upstream omitted its ID.
	require.NoError(t, client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`)))
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	waitForProxyActiveUsageCount(t, tracker, 33, 1)
	upstream.Send(`{"type":"response.completed","response":{"model":"gpt-5.1"}}`)
	_, err = readPassthroughLifecycleFrame(t, client, time.Second)
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 33, 0)
}

func TestOpenAIWSProxyRouteFailedDialStillAttributed(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	proxyURL := ResolveProxyURL(ctx, &Proxy{ID: 32, Name: "failed", Protocol: "http", Host: "proxy.test", Port: 8080})
	pool := newOpenAIWSConnPool(&config.Config{})
	pool.clientDialer = &openAIWSAlwaysFailDialer{}
	_, err := pool.dialConn(ctx, openAIWSAcquireRequest{Account: &Account{ID: 71}, WSURL: "wss://upstream.test/ws", ProxyURL: proxyURL})
	require.Error(t, err)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 32, Name: "failed", Host: "proxy.test", Port: 8080}}, ProxyRoutesFromContext(ctx))
}

func TestOpenAIWSProxyRoutePassthroughBinaryResponseCreateCountsActiveTurn(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
	defer tracker.Stop()
	ctx, observation := NewProxyRouteObservation(context.Background(), tracker)
	defer observation.Close()
	upstream := newStagedPassthroughConn()
	account := passthroughLifecycleAccount()
	account.Proxy = &Proxy{ID: 39, Name: "binary-create", Protocol: "http", Host: "proxy.test", Port: 8080}
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	server, _ := startPassthroughLifecycleServer(t, ctx, svc, account)
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer client.CloseNow()
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(`{"type":"response.completed","response":{"id":"first","model":"gpt-5.1"}}`)
	_, err := readPassthroughLifecycleFrame(t, client, time.Second)
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 39, 0)

	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, client.Write(writeCtx, coderws.MessageBinary, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`)))
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	waitForProxyActiveUsageCount(t, tracker, 39, 1)
	upstream.Send(`{"type":"response.completed","response":{"id":"second","model":"gpt-5.1"}}`)
	_, err = readPassthroughLifecycleFrame(t, client, time.Second)
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 39, 0)
}

func TestOpenAIWSProxyRoutePassthroughBinaryTerminalEndsActiveTurn(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
	}{
		{name: "completed_without_id", payload: `{"type":"response.completed","response":{"model":"gpt-5.1"}}`},
		{name: "bare_error", payload: `{"type":"error","error":{"code":"invalid_request_error","message":"failed"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryProxyActiveUsageStore()
			tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
			defer tracker.Stop()
			ctx, observation := NewProxyRouteObservation(context.Background(), tracker)
			defer observation.Close()
			upstream := newStagedPassthroughConn()
			account := passthroughLifecycleAccount()
			account.Proxy = &Proxy{ID: 40, Name: "binary-terminal", Protocol: "http", Host: "proxy.test", Port: 8080}
			cfg := passthroughLifecycleConfig()
			// Cleanup on the existing text-only first-output timeout must not make
			// the active-count assertion pass in place of observing the terminal.
			cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
			svc := newPassthroughLifecycleService(cfg, upstream)
			server, _ := startPassthroughLifecycleServer(t, ctx, svc, account)
			defer server.Close()
			client := dialPassthroughLifecycleClient(t, server)
			defer client.CloseNow()
			requirePassthroughUpstreamWrite(t, upstream, time.Second)
			waitForProxyActiveUsageCount(t, tracker, 40, 1)
			terminal := []byte(test.payload)
			upstream.frames <- stagedPassthroughFrame{messageType: coderws.MessageBinary, payload: terminal}
			readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			messageType, payload, err := client.Read(readCtx)
			require.NoError(t, err)
			require.Equal(t, coderws.MessageBinary, messageType)
			require.Equal(t, terminal, payload)
			waitForProxyActiveUsageCount(t, tracker, 40, 0)
		})
	}
}

func TestOpenAIWSProxyRouteHandshakeCountsButPrewarmDoesNot(t *testing.T) {
	for _, prewarm := range []bool{false, true} {
		name := "business"
		if prewarm {
			name = "prewarm"
		}
		t.Run(name, func(t *testing.T) {
			store := newMemoryProxyActiveUsageStore()
			tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
			defer tracker.Stop()
			ctx, observation := NewProxyRouteObservation(context.Background(), tracker)
			defer observation.Close()
			proxyURL := ResolveProxyURL(ctx, &Proxy{ID: 38, Name: "handshake", Protocol: "http", Host: "proxy.test", Port: 8080})
			pool := newOpenAIWSConnPool(&config.Config{})
			dialer := newOpenAIWSFirstDialBlockingCaptureDialer()
			pool.clientDialer = dialer
			var release sync.Once
			defer release.Do(func() { close(dialer.releaseFirst) })
			dialCtx := ctx
			if prewarm {
				dialCtx = context.Background()
			}
			done := make(chan error, 1)
			go func() {
				conn, err := pool.dialConn(dialCtx, openAIWSAcquireRequest{Account: &Account{ID: 71}, WSURL: "wss://upstream.test/ws", ProxyURL: proxyURL, proxyRoute: ProxyRouteSnapshot{Kind: "proxy", ProxyID: 38, Name: "handshake", Host: "proxy.test", Port: 8080}})
				if conn != nil {
					conn.close()
				}
				done <- err
			}()
			select {
			case <-dialer.firstStarted:
			case <-time.After(time.Second):
				t.Fatal("dial did not start")
			}
			want := int64(1)
			if prewarm {
				want = 0
			}
			require.Eventually(t, func() bool {
				details := tracker.GetActiveUsageDetails(context.Background(), []int64{38})[38]
				return details.RequestCount != nil && *details.RequestCount == want
			}, time.Second, time.Millisecond)
			release.Do(func() { close(dialer.releaseFirst) })
			require.NoError(t, <-done)
			waitForProxyActiveUsageCount(t, tracker, 38, 0)
		})
	}
}

func TestOpenAIWSProxyRoutePoolReuseKeepsDialSnapshot(t *testing.T) {
	ctx, observation := NewProxyRouteObservation(context.Background(), nil)
	defer observation.Close()
	proxy := &Proxy{ID: 37, Name: "at-dial", Protocol: "http", Host: "proxy.test", Port: 8080}
	proxyURL := ResolveProxyURL(ctx, proxy)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	defer pool.Close()
	pool.clientDialer = &openAIWSCountingDialer{}
	req := openAIWSAcquireRequest{Account: &Account{ID: 71, Concurrency: 1}, WSURL: "wss://upstream.test/ws", ProxyURL: proxyURL}
	first, err := pool.Acquire(ctx, req)
	require.NoError(t, err)
	id := first.ConnID()
	first.BeginProxyTurn(ctx)
	first.EndProxyTurn()
	first.Release()
	ResetProxyRouteUsage(ctx)
	proxy.Name = "after-dial"
	ResolveProxyURL(ctx, proxy)
	second, err := pool.Acquire(ctx, req)
	require.NoError(t, err)
	defer second.Release()
	require.Equal(t, id, second.ConnID())
	require.True(t, second.Reused())
	require.Empty(t, ProxyRoutesFromContext(ctx), "connection acquire does not imply a business turn")
	second.BeginProxyTurn(ctx)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 37, Name: "at-dial", Host: "proxy.test", Port: 8080}}, ProxyRoutesFromContext(ctx))
}

func TestOpenAIWSProxyRouteLiveBackgroundConnectionCountsUntilClose(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
	defer tracker.Stop()
	account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test", "chatgpt_account_id": "acct_test"}, Proxy: &Proxy{ID: 34, Name: "live", Protocol: "http", Host: "proxy.test", Port: 8080}}
	attestationCipher := newLiveAttestationCipher(&config.Config{JWT: config.JWTConfig{Secret: "live-route-test-secret"}})
	ciphertext, err := attestationCipher.Encrypt(`{"v":1,"s":0,"t":"v1.sideband"}`)
	require.NoError(t, err)
	svc := &OpenAIGatewayService{accountRepo: &liveTestAccountRepo{account: account}, liveAttestationCipher: attestationCipher, openaiWSPassthroughDialer: &liveTestDialer{conn: newLiveTestFrameConn()}}
	svc.SetProxyActiveUsageTracker(tracker)
	conn, err := svc.dialLiveSideband(context.Background(), &LiveCallRecord{CallID: "call_routes", AccountID: 71, AttestationCiphertext: ciphertext})
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 34, 1)
	require.NoError(t, conn.Close())
	require.NoError(t, conn.Close())
	waitForProxyActiveUsageCount(t, tracker, 34, 0)
}

func TestOpenAIWSProxyRouteLiveFinalizationPreservesSnapshot(t *testing.T) {
	routes := []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 35, Name: "at-call-creation", Host: "2001:db8::35", Port: 1080}}
	record := &LiveCallRecord{CallID: "call_final_routes", CallHash: hashLiveCallID("call_final_routes"), AccountID: 71, UserID: 2, APIKeyID: 3, CreatedAt: time.Now(), ProxyRoutes: routes}
	store := &liveTestStore{}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	repo := &liveTestUsageRepo{}
	svc := &OpenAIGatewayService{cache: store, usageLogRepo: repo, concurrencyService: NewConcurrencyService(&liveTestConcurrencyCache{})}
	svc.finalizeLiveCall(record)
	require.Len(t, repo.logs, 1)
	require.Equal(t, routes, repo.logs[0].ProxyRoutes)
	// Async usage must not share its slice with a mutable live record.
	record.ProxyRoutes[0].Name = "renamed"
	require.Equal(t, "at-call-creation", repo.logs[0].ProxyRoutes[0].Name)
}

func TestOpenAIWSProxyRouteGrokRealtimeCountsUntilClose(t *testing.T) {
	store := newMemoryProxyActiveUsageStore()
	tracker := NewProxyActiveUsageTrackerWithOptions(store, ProxyActiveUsageTrackerOptions{WorkerCount: 1, QueueSize: 32, HeartbeatInterval: time.Second})
	defer tracker.Stop()
	ctx, observation := NewProxyRouteObservation(context.Background(), tracker)
	defer observation.Close()
	proxyID := int64(36)
	account := &Account{ID: 71, Platform: PlatformGrok, Type: AccountTypeAPIKey, ProxyID: &proxyID, Proxy: &Proxy{ID: 36, Name: "grok", Protocol: "http", Host: "proxy.test", Port: 8080}}
	svc := &OpenAIGatewayService{openaiWSPassthroughDialer: &stagedPassthroughDialer{conn: newStagedPassthroughConn()}}
	conn, err := svc.OpenGrokRealtime(ctx, account, "test-token", "grok-voice-test")
	require.NoError(t, err)
	waitForProxyActiveUsageCount(t, tracker, 36, 1)
	require.Equal(t, []ProxyRouteSnapshot{{Kind: "proxy", ProxyID: 36, Name: "grok", Host: "proxy.test", Port: 8080}}, ProxyRoutesFromContext(ctx))
	require.NoError(t, conn.Close())
	waitForProxyActiveUsageCount(t, tracker, 36, 0)
}
