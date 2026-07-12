package service

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 说明：以下用例全部使用合成的占位域名 / 假 uuid / 假密码，不含任何真实订阅敏感信息。

func vmessURI(name, server string, port string) string {
	// v2rayN 风格 vmess:// = base64(JSON)
	j := `{"v":"2","ps":"` + name + `","add":"` + server + `","port":"` + port +
		`","id":"11111111-1111-1111-1111-111111111111","aid":"0","scy":"auto",` +
		`"net":"ws","host":"` + server + `","path":"/ray","tls":"tls","sni":"` + server + `"}`
	return "vmess://" + base64.StdEncoding.EncodeToString([]byte(j))
}

func TestProxyURIToClashProxy_AllSchemes(t *testing.T) {
	cases := []struct {
		name     string
		uri      string
		wantType string
		wantSrv  string
		wantPort int
	}{
		{"vmess-ws-tls", vmessURI("vm-node", "vm.example.com", "443"), "vmess", "vm.example.com", 443},
		{"vless-reality", "vless://22222222-2222-2222-2222-222222222222@vl.example.com:443?encryption=none&security=reality&type=grpc&serviceName=grpcsvc&sni=vl.example.com&pbk=PUBKEY&sid=SID&fp=chrome#vl-node", "vless", "vl.example.com", 443},
		{"trojan-ws", "trojan://trojanpass@tj.example.com:443?sni=tj.example.com&type=ws&host=tj.example.com&path=%2Ftj&allowInsecure=1#tj-node", "trojan", "tj.example.com", 443},
		{"hysteria2", "hysteria2://hy2pass@hy.example.com:8443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=zzz#hy-node", "hysteria2", "hy.example.com", 8443},
		{"tuic", "tuic://33333333-3333-3333-3333-333333333333:tuicpass@tc.example.com:443?sni=tc.example.com&congestion_control=bbr&alpn=h3#tuic-node", "tuic", "tc.example.com", 443},
		{"ss-sip002", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:sspassword")) + "@ss.example.com:8388#ss-node", "ss", "ss.example.com", 8388},
		{"ss-legacy", "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:sspass2@ss2.example.com:8389")) + "#ss2-node", "ss", "ss2.example.com", 8389},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proxy, ok := proxyURIToClashProxy(tc.uri)
			if !ok {
				t.Fatalf("failed to parse %s uri", tc.name)
			}
			if proxy["type"] != tc.wantType {
				t.Fatalf("type = %v, want %s", proxy["type"], tc.wantType)
			}
			if proxy["server"] != tc.wantSrv {
				t.Fatalf("server = %v, want %s", proxy["server"], tc.wantSrv)
			}
			if port, _ := proxy["port"].(int); port != tc.wantPort {
				t.Fatalf("port = %v, want %d", proxy["port"], tc.wantPort)
			}
			if strings.TrimSpace(proxySubscriptionConfigString(proxy["name"])) == "" {
				t.Fatalf("name must not be empty: %#v", proxy)
			}
		})
	}
}

func TestParseProxyURIListSubscription_Base64(t *testing.T) {
	list := strings.Join([]string{
		vmessURI("vm-node", "vm.example.com", "443"),
		"trojan://trojanpass@tj.example.com:443?sni=tj.example.com#tj-node",
		"invalid://not-a-supported-scheme",
	}, "\n")
	body := base64.StdEncoding.EncodeToString([]byte(list))

	proxies := parseProxyURIListSubscription([]byte(body))
	if len(proxies) != 2 {
		t.Fatalf("parsed proxy count = %d, want 2 (unknown scheme skipped)", len(proxies))
	}
}

func TestParseProxyURIListSubscription_PlaintextList(t *testing.T) {
	list := "trojan://trojanpass@tj.example.com:443#tj\nvless://44444444-4444-4444-4444-444444444444@vl.example.com:443?security=tls#vl\n"
	proxies := parseProxyURIListSubscription([]byte(list))
	if len(proxies) != 2 {
		t.Fatalf("parsed proxy count = %d, want 2", len(proxies))
	}
}

// 端到端：base64 v2ray 订阅可被 ParseProxySubscription 直接解析为节点（Phase 2 回退）。
func TestParseProxySubscription_FallsBackToBase64V2Ray(t *testing.T) {
	list := strings.Join([]string{
		vmessURI("vm-node-1", "vm1.example.com", "443"),
		vmessURI("vm-node-2", "vm2.example.com", "8443"),
	}, "\n")
	body := base64.StdEncoding.EncodeToString([]byte(list))

	parsed, err := ParseProxySubscription([]byte(body))
	if err != nil {
		t.Fatalf("parse base64 v2ray subscription: %v", err)
	}
	if len(parsed.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(parsed.Nodes))
	}
	if parsed.Nodes[0].Type != "vmess" || parsed.Nodes[0].Server != "vm1.example.com" {
		t.Fatalf("unexpected first node: type=%s server=%s", parsed.Nodes[0].Type, parsed.Nodes[0].Server)
	}
	if parsed.RawDNSConfig != "" {
		t.Fatalf("uri subscription must not carry dns config: %q", parsed.RawDNSConfig)
	}
}

// base64 订阅里若节点写死私网 IP，仍应被拒绝（与 Clash YAML 路径一致，防内网直连）。
func TestParseProxySubscription_Base64RejectsPrivateIPNode(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("trojan://pw@10.0.0.5:443#internal"))
	_, err := ParseProxySubscription([]byte(body))
	if err == nil {
		t.Fatal("expected private-IP node in base64 subscription to be rejected")
	}
}

func TestDecodeMaybeBase64Subscription(t *testing.T) {
	// 已是 URI 列表 → 原样返回
	plain := "vmess://xxxx\ntrojan://yyyy"
	if got := decodeMaybeBase64Subscription([]byte(plain)); got != plain {
		t.Fatalf("plaintext uri list should be returned as-is, got: %q", got)
	}
	// base64 → 解码
	encoded := base64.StdEncoding.EncodeToString([]byte("trojan://pw@tj.example.com:443#n"))
	if got := decodeMaybeBase64Subscription([]byte(encoded)); !strings.Contains(got, "trojan://") {
		t.Fatalf("base64 body should decode to uri list, got: %q", got)
	}
	// 非 base64、非 URI → 原样返回（上层会再判定无节点）
	if got := decodeMaybeBase64Subscription([]byte("not base64 !!!")); got == "" {
		t.Fatal("non-empty input should not decode to empty")
	}
}
