package service

import (
	"context"
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
