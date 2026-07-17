package service

import (
	"encoding/base64"
	"strings"
	"testing"
)

func managedProxyVMessURI(name, server, port string) string {
	payload := `{"v":"2","ps":"` + name + `","add":"` + server + `","port":"` + port +
		`","id":"11111111-1111-1111-1111-111111111111","aid":"0","scy":"auto",` +
		`"net":"ws","host":"` + server + `","path":"/ray","tls":"tls","sni":"` + server + `"}`
	return "vmess://" + base64.StdEncoding.EncodeToString([]byte(payload))
}

func TestProxyURIToClashProxyAllSchemes(t *testing.T) {
	cases := []struct {
		name     string
		uri      string
		wantType string
		wantSrv  string
		wantPort int
	}{
		{"vmess-ws-tls", managedProxyVMessURI("vm-node", "vm.example.com", "443"), "vmess", "vm.example.com", 443},
		{"vless-reality", "vless://22222222-2222-2222-2222-222222222222@vl.example.com:443?encryption=none&security=reality&type=grpc&serviceName=grpcsvc&sni=vl.example.com&pbk=PUBKEY&sid=SID&fp=chrome#vl-node", "vless", "vl.example.com", 443},
		{"trojan-ws", "trojan://trojanpass@tj.example.com:443?sni=tj.example.com&type=ws&host=tj.example.com&path=%2Ftj&allowInsecure=1#tj-node", "trojan", "tj.example.com", 443},
		{"hysteria2", "hysteria2://hy2pass@hy.example.com:8443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=zzz#hy-node", "hysteria2", "hy.example.com", 8443},
		{"hy2", "hy2://hy2pass@hy2.example.com:8443?sni=hy2.example.com#hy2-node", "hysteria2", "hy2.example.com", 8443},
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

func TestProxyURIToClashProxyMalformedDoesNotPanic(t *testing.T) {
	cases := []string{
		"vless://vl.example.com:443?security=tls#missing-userinfo",
		"trojan://tj.example.com:443#missing-userinfo",
		"hysteria2://hy.example.com:8443#missing-userinfo",
		"hy2://hy.example.com:8443#missing-userinfo",
		"tuic://tc.example.com:443#missing-userinfo",
		"vmess://not-base64",
		"ss://not-base64",
	}
	for _, uri := range cases {
		t.Run(uri, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("proxyURIToClashProxy panicked for %q: %v", uri, r)
				}
			}()
			if proxy, ok := proxyURIToClashProxy(uri); ok || proxy != nil {
				t.Fatalf("malformed uri should be skipped, got ok=%v proxy=%#v", ok, proxy)
			}
		})
	}
}

func TestProxyURIToClashProxyRejectsMissingRequiredCredentials(t *testing.T) {
	cases := []string{
		"trojan://@tj.example.com:443#empty-password",
		"hysteria2://@hy.example.com:8443#empty-password",
		"tuic://33333333-3333-3333-3333-333333333333@tc.example.com:443#empty-password",
		"ss://aes-128-gcm:@ss.example.com:8388#empty-password",
		"vless://@vl.example.com:443#empty-uuid",
		"vless://22222222-2222-2222-2222-222222222222@vl.example.com#missing-port",
	}
	for _, uri := range cases {
		if proxy, ok := proxyURIToClashProxy(uri); ok || proxy != nil {
			t.Fatalf("invalid credentials should be skipped for %q, got ok=%v proxy=%#v", uri, ok, proxy)
		}
	}
}

func TestParseProxyURIListSubscriptionBase64AndPlaintext(t *testing.T) {
	list := strings.Join([]string{
		managedProxyVMessURI("vm-node", "vm.example.com", "443"),
		"trojan://trojanpass@tj.example.com:443?sni=tj.example.com#tj-node",
		"invalid://not-a-supported-scheme",
	}, "\n")
	body := base64.StdEncoding.EncodeToString([]byte(list))

	proxies := parseProxyURIListSubscription([]byte(body))
	if len(proxies) != 2 {
		t.Fatalf("base64 proxy count = %d, want 2", len(proxies))
	}

	plain := "trojan://trojanpass@tj.example.com:443#tj\nvless://44444444-4444-4444-4444-444444444444@vl.example.com:443?security=tls#vl\n"
	proxies = parseProxyURIListSubscription([]byte(plain))
	if len(proxies) != 2 {
		t.Fatalf("plaintext proxy count = %d, want 2", len(proxies))
	}
}

func TestParseProxySubscriptionFallsBackToBase64V2Ray(t *testing.T) {
	list := strings.Join([]string{
		managedProxyVMessURI("vm-node-1", "8.8.8.8", "443"),
		managedProxyVMessURI("vm-node-2", "1.1.1.1", "8443"),
	}, "\n")
	body := base64.StdEncoding.EncodeToString([]byte(list))

	parsed, err := ParseProxySubscription([]byte(body))
	if err != nil {
		t.Fatalf("parse base64 v2ray subscription: %v", err)
	}
	if len(parsed.Nodes) != 2 {
		t.Fatalf("node count = %d, want 2", len(parsed.Nodes))
	}
	if parsed.Nodes[0].Type != "vmess" || parsed.Nodes[0].Server != "8.8.8.8" {
		t.Fatalf("unexpected first node: type=%s server=%s", parsed.Nodes[0].Type, parsed.Nodes[0].Server)
	}
	if parsed.RawDNSConfig != "" {
		t.Fatalf("uri subscription must not carry dns config: %q", parsed.RawDNSConfig)
	}
}

func TestParseProxySubscriptionBase64RejectsPrivateAndLocalNodes(t *testing.T) {
	blocked := []string{
		"trojan://pw@10.0.0.5:443#internal",
		"trojan://pw@127.0.0.1:443#loopback",
		"trojan://pw@169.254.169.254:443#metadata",
		"trojan://pw@localhost:443#localhost",
	}
	for _, uri := range blocked {
		body := base64.StdEncoding.EncodeToString([]byte(uri))
		if _, err := ParseProxySubscription([]byte(body)); err == nil {
			t.Fatalf("expected blocked node in base64 subscription to be rejected: %s", uri)
		}
	}
}

func TestDecodeMaybeBase64Subscription(t *testing.T) {
	plain := "vmess://xxxx\ntrojan://yyyy"
	if got := decodeMaybeBase64Subscription([]byte(plain)); got != plain {
		t.Fatalf("plaintext uri list should be returned as-is, got: %q", got)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("trojan://pw@tj.example.com:443#n"))
	if got := decodeMaybeBase64Subscription([]byte(encoded)); !strings.Contains(got, "trojan://") {
		t.Fatalf("base64 body should decode to uri list, got: %q", got)
	}

	if got := decodeMaybeBase64Subscription([]byte("not base64 !!!")); got == "" {
		t.Fatal("non-empty input should not decode to empty")
	}
}
