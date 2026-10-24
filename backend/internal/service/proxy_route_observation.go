package service

import (
	"context"
	"crypto/sha256"
	"io"
	"net/url"
	"strconv"
	"sync"
)

type proxyRouteObservationKey struct{}
type proxyRoutesSnapshotKey struct{}

// ProxyRouteObservation belongs to one gateway request. Resolver registrations
// are not evidence of use: only an outbound attempt records a route.
type ProxyRouteObservation struct {
	mu       sync.Mutex
	tracker  *ProxyActiveUsageTracker
	resolved map[[32]byte]ProxyRouteSnapshot
	routes   []ProxyRouteSnapshot
	seen     map[string]bool
	active   map[*ProxyRouteAttempt]struct{}
	closed   bool
	stop     func() bool
}

func NewProxyRouteObservation(ctx context.Context, tracker *ProxyActiveUsageTracker) (context.Context, *ProxyRouteObservation) {
	if ctx == nil {
		ctx = context.Background()
	}
	o := &ProxyRouteObservation{tracker: tracker, resolved: make(map[[32]byte]ProxyRouteSnapshot), seen: make(map[string]bool), active: make(map[*ProxyRouteAttempt]struct{})}
	// Parent cancellation is a fallback for cancelled transports, not evidence
	// that a deliberately detached upstream body has stopped draining.
	o.mu.Lock()
	o.stop = context.AfterFunc(ctx, o.closeCancelledAttempts)
	o.mu.Unlock()
	return context.WithValue(ctx, proxyRouteObservationKey{}, o), o
}

func proxyRouteObservationFromContext(ctx context.Context) *ProxyRouteObservation {
	if ctx == nil {
		return nil
	}
	o, _ := ctx.Value(proxyRouteObservationKey{}).(*ProxyRouteObservation)
	return o
}

func registerProxyRoute(ctx context.Context, raw string, proxy *Proxy) string {
	o := proxyRouteObservationFromContext(ctx)
	if o == nil || proxy == nil || raw == "" {
		return raw
	}
	// Keep credentials out of the observation, including its lookup keys.
	key := sha256.Sum256([]byte(raw))
	o.mu.Lock()
	o.resolved[key] = ProxyRouteSnapshot{Kind: "proxy", ProxyID: proxy.ID, Name: proxy.Name, Host: proxy.Host, Port: proxy.Port}
	o.mu.Unlock()
	return raw
}

// ProxyRouteSnapshotFromContext returns the immutable logical node selected by
// the resolver, not the managed listener address or account's later binding.
func ProxyRouteSnapshotFromContext(ctx context.Context, raw string, usesEnvironmentProxy bool) ProxyRouteSnapshot {
	if raw == "" {
		if usesEnvironmentProxy {
			return ProxyRouteSnapshot{Kind: "system_proxy"}
		}
		return ProxyRouteSnapshot{Kind: "direct"}
	}
	if o := proxyRouteObservationFromContext(ctx); o != nil {
		o.mu.Lock()
		route, ok := o.resolved[sha256.Sum256([]byte(raw))]
		o.mu.Unlock()
		if ok {
			return route
		}
	}
	return ProxyRouteSnapshot{Kind: "unknown"}
}

func validProxyAttemptURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h", "socks4", "socks4a":
		return true
	default:
		return false
	}
}

func BeginProxyRouteAttempt(ctx context.Context, accountID int64, raw string, usesEnvironmentProxy bool) *ProxyRouteAttempt {
	if !validProxyAttemptURL(raw) {
		return nil
	}
	return BeginProxyRouteSnapshotAttempt(ctx, accountID, ProxyRouteSnapshotFromContext(ctx, raw, usesEnvironmentProxy))
}

func BeginProxyRouteSnapshotAttempt(ctx context.Context, accountID int64, route ProxyRouteSnapshot) *ProxyRouteAttempt {
	o := proxyRouteObservationFromContext(ctx)
	if o == nil {
		return nil
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.appendRouteLocked(route)
	a := &ProxyRouteAttempt{observation: o, ctx: ctx}
	if route.Kind == "proxy" {
		a.handle = o.tracker.BeginProxy(route.ProxyID, accountID)
	}
	o.active[a] = struct{}{}
	// Bind cleanup to the actual outbound context. A detached context may have
	// its own deadline, and cancellation can propagate to child contexts after
	// the observation's parent callback starts. End synchronizes with this
	// assignment through o.mu even when ctx is already cancelled.
	a.stop = context.AfterFunc(ctx, a.End)
	o.mu.Unlock()
	return a
}

func ProxyRoutesFromContext(ctx context.Context) []ProxyRouteSnapshot {
	if ctx == nil {
		return nil
	}
	if routes, ok := ctx.Value(proxyRoutesSnapshotKey{}).([]ProxyRouteSnapshot); ok {
		return append([]ProxyRouteSnapshot(nil), routes...)
	}
	o := proxyRouteObservationFromContext(ctx)
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]ProxyRouteSnapshot(nil), o.routes...)
}

func WithProxyRoutesSnapshot(ctx context.Context, routes []ProxyRouteSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, proxyRoutesSnapshotKey{}, append([]ProxyRouteSnapshot(nil), routes...))
}

func ResetProxyRouteUsage(ctx context.Context) {
	if o := proxyRouteObservationFromContext(ctx); o != nil {
		o.mu.Lock()
		o.routes = nil
		o.seen = make(map[string]bool)
		o.mu.Unlock()
	}
}

// SeedProxyRouteUsage restores already-observed routes when a durable live
// session resumes on another process. It does not acquire active leases.
func SeedProxyRouteUsage(ctx context.Context, routes []ProxyRouteSnapshot) {
	if o := proxyRouteObservationFromContext(ctx); o != nil {
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.closed {
			return
		}
		for _, route := range routes {
			o.appendRouteLocked(route)
		}
	}
}

func (o *ProxyRouteObservation) appendRouteLocked(route ProxyRouteSnapshot) {
	key := route.Kind
	if route.Kind == "proxy" {
		key += ":" + strconv.FormatInt(route.ProxyID, 10)
	}
	if !o.seen[key] {
		o.routes = append(o.routes, route)
		o.seen[key] = true
	}
}

func (o *ProxyRouteObservation) closeCancelledAttempts() {
	o.mu.Lock()
	active := make([]*ProxyRouteAttempt, 0, len(o.active))
	for a := range o.active {
		if a.ctx.Err() != nil {
			active = append(active, a)
		}
	}
	o.mu.Unlock()
	for _, a := range active {
		a.End()
	}
}

func (o *ProxyRouteObservation) Close() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.closed = true
	active := make([]*ProxyRouteAttempt, 0, len(o.active))
	for a := range o.active {
		active = append(active, a)
	}
	stop := o.stop
	o.mu.Unlock()
	if stop != nil {
		stop()
	}
	for _, a := range active {
		a.End()
	}
}

type ProxyRouteAttempt struct {
	once        sync.Once
	observation *ProxyRouteObservation
	handle      *ProxyActiveUsageHandle
	ctx         context.Context
	stop        func() bool
}

func (a *ProxyRouteAttempt) End() {
	if a == nil {
		return
	}
	a.once.Do(func() {
		var stop func() bool
		if o := a.observation; o != nil {
			o.mu.Lock()
			delete(o.active, a)
			stop = a.stop
			o.mu.Unlock()
		}
		if stop != nil {
			stop()
		}
		a.handle.End()
	})
}

// WrapBody holds the attempt through streaming, and releases at EOF, failure,
// explicit Close, or actual transport-context cancellation. A detached stream
// keeps its lease while draining after the original client context is cancelled.
func (a *ProxyRouteAttempt) WrapBody(body io.ReadCloser) io.ReadCloser {
	if a == nil {
		return body
	}
	if body == nil {
		a.End()
		return nil
	}
	return &proxyRouteBody{ReadCloser: body, attempt: a}
}

type proxyRouteBody struct {
	io.ReadCloser
	attempt *ProxyRouteAttempt
}

func (b *proxyRouteBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.attempt.End()
	}
	return n, err
}

func (b *proxyRouteBody) Close() error {
	defer b.attempt.End()
	return b.ReadCloser.Close()
}
