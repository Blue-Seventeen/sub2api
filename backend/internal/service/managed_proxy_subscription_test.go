package service

import (
	"context"
	"strings"
	"testing"
)

func TestValidateManagedProxySubscriptionURLBlocksLocalAndPrivateTargets(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/sub.yaml",
		"http://[::1]/sub.yaml",
		"http://localhost/sub.yaml",
		"http://192.168.1.10/sub.yaml",
		"http://10.0.0.1/sub.yaml",
		"http://172.16.0.1/sub.yaml",
		"http://100.64.0.1/sub.yaml",
		"http://169.254.169.254/latest/meta-data",
		"http://198.18.0.1/sub.yaml",
		"http://192.0.2.1/sub.yaml",
		"http://[fc00::1]/sub.yaml",
		"http://224.0.0.1/sub.yaml",
		"http://0.0.0.0/sub.yaml",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if err := validateManagedProxySubscriptionURL(raw); err == nil {
				t.Fatalf("expected %q to be blocked", raw)
			}
		})
	}
}

func TestValidateManagedProxySubscriptionURLAllowsPublicIP(t *testing.T) {
	if err := validateManagedProxySubscriptionURL("https://8.8.8.8/clash.yaml"); err != nil {
		t.Fatalf("expected public IP to be allowed: %v", err)
	}
}

func TestValidateManagedProxySubscriptionURLRejectsUnsupportedScheme(t *testing.T) {
	err := validateManagedProxySubscriptionURL("file:///etc/passwd")
	if err == nil {
		t.Fatal("expected unsupported scheme to be rejected")
	}
}

func TestValidateManagedProxySubscriptionHostUsesDNSResolution(t *testing.T) {
	if err := validateManagedProxySubscriptionHost(context.Background(), "localhost"); err == nil {
		t.Fatal("expected localhost to be blocked")
	}
}

func TestParseProxySubscriptionNodesBlocksPrivateNodeServer(t *testing.T) {
	_, err := ParseProxySubscriptionNodes([]byte(`
proxies:
  - name: local-node
    type: ss
    server: 127.0.0.1
    port: 8388
    cipher: aes-128-gcm
    password: remote-secret
`))
	if err == nil {
		t.Fatal("expected private node server to be blocked")
	}
}

func TestParseProxySubscriptionNodesBlocksMetadataNodeServer(t *testing.T) {
	_, err := ParseProxySubscriptionNodes([]byte(`
proxies:
  - name: metadata-node
    type: trojan
    server: 169.254.169.254
    port: 443
    password: remote-secret
`))
	if err == nil {
		t.Fatal("expected metadata node server to be blocked")
	}
}

// TestParseProxySubscriptionPreservesDNSNameserverPolicy 覆盖验收标准 1/5a/5b：
// 带 dns.nameserver-policy 且节点为域名的订阅可以解析成功，即使系统 DNS 会把该域名
// 解析成 loopback 占位地址 —— 因为域名节点不再走系统 DNS 判定，而是保留 DNS policy
// 交给 Mihomo 解析。
func TestParseProxySubscriptionPreservesDNSNameserverPolicy(t *testing.T) {
	parsed, err := ParseProxySubscription([]byte(`
dns:
  enable: true
  listen: 0.0.0.0:53
  nameserver-policy:
    "+.entry.example.qpon": tcp://dns.example:8080
  nameserver_policy:
    "+.unsafe.example": tcp://unsafe.example:8080
proxies:
  - name: HK-01
    type: ss
    server: t.hk01.entry.example.qpon
    port: 8388
    cipher: aes-128-gcm
    password: remote-secret
`))
	if err != nil {
		t.Fatalf("parse subscription with dns policy: %v", err)
	}
	if len(parsed.Nodes) != 1 {
		t.Fatalf("node count mismatch: got=%d want=1", len(parsed.Nodes))
	}
	if parsed.Nodes[0].Server != "t.hk01.entry.example.qpon" {
		t.Fatalf("unexpected node server: %q", parsed.Nodes[0].Server)
	}
	if !strings.Contains(parsed.RawDNSConfig, "nameserver-policy") {
		t.Fatalf("raw dns config missing nameserver-policy: %q", parsed.RawDNSConfig)
	}
	if !strings.Contains(parsed.RawDNSConfig, "entry.example.qpon") {
		t.Fatalf("raw dns config missing policy domain: %q", parsed.RawDNSConfig)
	}
	for _, dropped := range []string{"listen", "nameserver_policy", "unsafe.example"} {
		if strings.Contains(parsed.RawDNSConfig, dropped) {
			t.Fatalf("raw dns config should drop %q: %q", dropped, parsed.RawDNSConfig)
		}
	}
}

func TestParseProxySubscriptionDNSPolicyOnlyAllowsMatchingDomains(t *testing.T) {
	_, err := ParseProxySubscription([]byte(`
dns:
  nameserver-policy:
    "+.entry.example.invalid": tcp://dns.example:8080
proxies:
  - name: policy-covered
    type: ss
    server: t.hk01.entry.example.invalid
    port: 8388
    cipher: aes-128-gcm
    password: remote-secret
  - name: not-covered
    type: ss
    server: other.example.invalid
    port: 8389
    cipher: aes-128-gcm
    password: remote-secret
`))
	if err == nil {
		t.Fatal("expected non-policy domain to keep DNS validation and be rejected")
	}
}

// TestParseProxySubscriptionWithoutDNSHasEmptyConfig 确认无 dns 段的订阅不会产生 DNS 配置。
func TestParseProxySubscriptionWithoutDNSHasEmptyConfig(t *testing.T) {
	parsed, err := ParseProxySubscription([]byte(`
proxies:
  - name: HK-01
    type: ss
    server: 8.8.8.8
    port: 8388
    cipher: aes-128-gcm
    password: remote-secret
`))
	if err != nil {
		t.Fatalf("parse subscription without dns: %v", err)
	}
	if parsed.RawDNSConfig != "" {
		t.Fatalf("expected empty raw dns config, got: %q", parsed.RawDNSConfig)
	}
}

func TestEnsureClashProxyFormatFlag(t *testing.T) {
	got := ensureClashProxyFormatFlag("https://sub.example.com/api/v1/client/subscribe?token=abc")
	if !strings.Contains(got, "flag=clash") || !strings.Contains(got, "token=abc") {
		t.Fatalf("expected flag=clash appended and token kept, got: %q", got)
	}

	got = ensureClashProxyFormatFlag("https://sub.example.com/s?token=abc&flag=meta")
	if strings.Contains(got, "flag=clash") || !strings.Contains(got, "flag=meta") {
		t.Fatalf("existing flag must be respected, got: %q", got)
	}

	got = ensureClashProxyFormatFlag("https://sub.example.com/s")
	if !strings.Contains(got, "flag=clash") {
		t.Fatalf("expected flag=clash on query-less url, got: %q", got)
	}
}

func TestManagedProxySubscriptionFetchOptionsKeepHistoricalDefault(t *testing.T) {
	origUA, origFlag := managedProxySubscriptionFetchOptions()
	t.Cleanup(func() { SetManagedProxySubscriptionFetchOptions(origUA, origFlag) })

	SetManagedProxySubscriptionFetchOptions("", false)
	if ua, flag := managedProxySubscriptionFetchOptions(); ua != defaultManagedProxySubscriptionUserAgent || flag {
		t.Fatalf("default options = (%q,%v), want (%q,false)", ua, flag, defaultManagedProxySubscriptionUserAgent)
	}
	if defaultManagedProxySubscriptionUserAgent != "sub2api-managed-proxy/1.0" {
		t.Fatalf("historical default UA changed: %q", defaultManagedProxySubscriptionUserAgent)
	}

	SetManagedProxySubscriptionFetchOptions("clash-verge/v2.0.0", true)
	if ua, flag := managedProxySubscriptionFetchOptions(); ua != "clash-verge/v2.0.0" || !flag {
		t.Fatalf("configured options = (%q,%v), want (clash-verge/v2.0.0,true)", ua, flag)
	}
}

func TestValidateManagedProxyNodeServer(t *testing.T) {
	ctx := context.Background()
	allowedWithoutPolicy := []string{
		"8.8.8.8",
		"1.1.1.1",
		"2606:4700:4700::1111",
	}
	for _, server := range allowedWithoutPolicy {
		if err := validateManagedProxyNodeServer(ctx, server, false); err != nil {
			t.Errorf("expected server %q to be allowed, got: %v", server, err)
		}
	}
	if err := validateManagedProxyNodeServer(ctx, "t.hk01.entry.example.invalid", true); err != nil {
		t.Fatalf("expected policy-managed domain to be allowed, got: %v", err)
	}
	if err := validateManagedProxyNodeServer(ctx, "t.hk01.entry.example.invalid", false); err == nil {
		t.Fatal("expected unresolved domain without dns policy to keep legacy DNS validation and be blocked")
	}

	blocked := []string{
		"127.0.0.1",
		"127.127.127.5",
		"10.0.0.1",
		"192.168.1.10",
		"172.16.0.1",
		"169.254.169.254",
		"::1",
		"localhost",
		"foo.localhost",
		"",
	}
	for _, server := range blocked {
		if err := validateManagedProxyNodeServer(ctx, server, true); err == nil {
			t.Errorf("expected server %q to be blocked even with dns policy", server)
		}
	}
}
