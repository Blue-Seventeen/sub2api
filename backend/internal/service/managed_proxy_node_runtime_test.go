package service

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestManagedProxyNodeRoutingRejectsUnavailableAndObsoleteIdentity(t *testing.T) {
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{Enabled: true, MihomoBinaryPath: "mihomo", BindHost: "127.0.0.1", WorkDir: t.TempDir()}, nil, &capturingManagedProxyRunner{})
	sub := managedProxyRuntimeTestSubscription(27, 1)
	activeID, missingID, disabledID := int64(101), int64(102), int64(103)
	sub.Nodes[0].ProxyID = &activeID
	missing, disabled := sub.Nodes[0], sub.Nodes[0]
	missing.ProxyID, missing.Status = &missingID, ProxySubscriptionNodeStatusSourceMissing
	missing.Username, missing.Password = "missing-user", "missing-password"
	disabled.ProxyID, disabled.Status = &disabledID, ProxySubscriptionNodeStatusInactive
	disabled.Username, disabled.Password = "disabled-user", "disabled-password"
	sub.Nodes = append(sub.Nodes, missing, disabled)
	if err := runtime.startEntry(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	previous := GetDefaultManagedProxyResolver()
	SetDefaultManagedProxyResolver(runtime)
	t.Cleanup(func() { runtime.Stop(); SetDefaultManagedProxyResolver(previous) })
	tests := []struct {
		name           string
		id             int64
		user, password string
		available      bool
	}{
		{"current identity", activeID, "mpu_hk", "mpp_hk", true},
		{"missing node", missingID, "missing-user", "missing-password", false},
		{"disabled node", disabledID, "disabled-user", "disabled-password", false},
		{"obsolete password", activeID, "mpu_hk", "obsolete-password", false},
		{"unknown proxy with valid auth", 999, "mpu_hk", "mpp_hk", false},
		{"node with missing auth", activeID, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Proxy{ID: tt.id, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &sub.ID, Username: tt.user, Password: tt.password}
			resolved := ResolveProxyURL(context.Background(), p)
			if !tt.available {
				if !strings.HasPrefix(resolved, "managed-proxy://unavailable/") {
					t.Error("unavailable node was routed to local SOCKS listener")
				}
				admin := &adminServiceImpl{managedProxyResolver: runtime}
				if value, err := admin.resolveProxyURL(context.Background(), p); err == nil || value != "" {
					t.Error("admin probe accepted unavailable node")
				}
				return
			}
			parsed, err := url.Parse(resolved)
			if err != nil || parsed.Scheme != "socks5h" || parsed.User == nil {
				t.Fatal("current node did not resolve with SOCKS authentication")
			}
			password, ok := parsed.User.Password()
			if parsed.User.Username() != tt.user || !ok || password != tt.password {
				t.Fatal("wrong current authentication")
			}
		})
	}
}

func TestManagedProxyNodeRoutingReplacesInstalledIdentityOnRestart(t *testing.T) {
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{Enabled: true, MihomoBinaryPath: "mihomo", BindHost: "127.0.0.1", WorkDir: t.TempDir()}, nil, &capturingManagedProxyRunner{})
	sub := managedProxyRuntimeTestSubscription(28, 1)
	id := int64(101)
	sub.Nodes[0].ProxyID = &id
	if err := runtime.startEntry(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	previous := GetDefaultManagedProxyResolver()
	SetDefaultManagedProxyResolver(runtime)
	t.Cleanup(func() { runtime.Stop(); SetDefaultManagedProxyResolver(previous) })
	obsolete := &Proxy{ID: id, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &sub.ID, Username: "mpu_hk", Password: "mpp_hk"}
	sub.Revision++
	sub.Nodes[0].Password = "replacement-password"
	if err := runtime.startEntry(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ResolveProxyURL(context.Background(), obsolete), "managed-proxy://unavailable/") {
		t.Fatal("old authentication survived runtime replacement")
	}
	current := *obsolete
	current.Password = "replacement-password"
	if !strings.HasPrefix(ResolveProxyURL(context.Background(), &current), "socks5h://") {
		t.Fatal("new identity unavailable after restart")
	}
	runtime.stopEntry(context.Background(), sub.ID)
	if !strings.HasPrefix(ResolveProxyURL(context.Background(), &current), "managed-proxy://unavailable/") {
		t.Fatal("stopped identity still available")
	}
}

type managedProxyLegacyResolver struct{ managedProxyNoopResolver }

func (managedProxyLegacyResolver) ResolveProxyURL(context.Context, int64) (string, error) {
	return "socks5h://127.0.0.1:1080", nil
}

func TestManagedProxyNodeRoutingKeepsLegacyAndManualBehavior(t *testing.T) {
	previous := GetDefaultManagedProxyResolver()
	SetDefaultManagedProxyResolver(managedProxyLegacyResolver{})
	t.Cleanup(func() { SetDefaultManagedProxyResolver(previous) })
	id := int64(7)
	legacy := &Proxy{ID: 5, SourceType: ProxySourceMihomoSubscription, SubscriptionID: &id, Username: "legacy-user", Password: "legacy-password"}
	if !strings.HasPrefix(ResolveProxyURL(context.Background(), legacy), "socks5h://") {
		t.Fatal("legacy resolver no longer resolves subscription proxy")
	}
	manual := &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8080, Username: "manual-user", Password: "manual-password"}
	if ResolveProxyURL(context.Background(), manual) != "http://manual-user:manual-password@127.0.0.1:8080" {
		t.Fatal("manual proxy behavior changed")
	}
	legacy.NodeStatus = ProxySubscriptionNodeStatusSourceMissing
	if !strings.HasPrefix(ResolveProxyURL(context.Background(), legacy), "managed-proxy://unavailable/") {
		t.Fatal("legacy resolver bypassed known missing node state")
	}
	runtime := newManagedProxyRuntimeWithRunner(config.ManagedProxyConfig{Enabled: true, MihomoBinaryPath: "mihomo", BindHost: "127.0.0.1", WorkDir: t.TempDir()}, nil, &capturingManagedProxyRunner{})
	sub := managedProxyRuntimeTestSubscription(id, 1)
	if err := runtime.startEntry(context.Background(), sub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Stop)
	legacy.Username, legacy.Password, legacy.NodeStatus = "", "", ""
	if value, err := runtime.ResolveProxyURLForProxy(context.Background(), legacy); err != nil || !strings.HasPrefix(value, "socks5h://") {
		t.Fatal("legacy aggregate proxy lost subscription-level route")
	}
}
