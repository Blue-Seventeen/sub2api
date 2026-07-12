package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const maxManagedProxySubscriptionBytes = 10 << 20

// defaultManagedProxySubscriptionUserAgent 默认抓取订阅使用的 User-Agent。
// 部分机场（V2Board/Xboard 等）按 UA 决定返回 Clash YAML 还是 base64 v2ray 订阅，
// 使用主流 Clash-Meta 客户端标识可确保拿到 Clash YAML（并尽量拿到 hysteria2 等 meta 协议节点）。
const defaultManagedProxySubscriptionUserAgent = "clash-verge/v2.0.0"

var (
	managedProxySubscriptionFetchMu         sync.RWMutex
	managedProxySubscriptionUserAgent       = defaultManagedProxySubscriptionUserAgent
	managedProxySubscriptionAppendClashFlag = false
)

// SetManagedProxySubscriptionFetchOptions 在启动装配时由配置注入抓取参数。
// userAgent 为空时回退到默认 Clash-Meta 标识。
func SetManagedProxySubscriptionFetchOptions(userAgent string, appendClashFlag bool) {
	managedProxySubscriptionFetchMu.Lock()
	defer managedProxySubscriptionFetchMu.Unlock()
	if ua := strings.TrimSpace(userAgent); ua != "" {
		managedProxySubscriptionUserAgent = ua
	} else {
		managedProxySubscriptionUserAgent = defaultManagedProxySubscriptionUserAgent
	}
	managedProxySubscriptionAppendClashFlag = appendClashFlag
}

func managedProxySubscriptionFetchOptions() (userAgent string, appendClashFlag bool) {
	managedProxySubscriptionFetchMu.RLock()
	defer managedProxySubscriptionFetchMu.RUnlock()
	return managedProxySubscriptionUserAgent, managedProxySubscriptionAppendClashFlag
}

// ensureClashProxyFormatFlag 在订阅 URL 未显式携带 flag 时追加 &flag=clash。
// 仅改动 query，不改 scheme/host（订阅 URL 的 SSRF 校验因此不受影响）；已含 flag 时保持原样，尊重用户设置。
func ensureClashProxyFormatFlag(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Has("flag") {
		return raw
	}
	q.Set("flag", "clash")
	u.RawQuery = q.Encode()
	return u.String()
}

var managedProxySubscriptionBlockedCIDRs = mustParseManagedProxySubscriptionCIDRs([]string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"192.31.196.0/24",
	"192.52.193.0/24",
	"192.88.99.0/24",
	"192.175.48.0/24",
	"192.168.0.0/16",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"::1/128",
	"::ffff:0:0/96",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001::/23",
	"2001:db8::/32",
	"2002::/16",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
})

// ParsedProxySubscription 保存从 Clash/Mihomo 订阅解析出的节点与顶层 DNS 配置。
type ParsedProxySubscription struct {
	Nodes []ProxySubscriptionNode
	// RawDNSConfig 是订阅顶层 dns 配置的 YAML 文本（保留完整 map，含 nameserver-policy）。
	// 订阅不含 dns 时为空字符串。
	RawDNSConfig string
}

// FetchProxySubscription 拉取订阅并解析出节点与 DNS 配置。
// 订阅 URL 的 SSRF 校验（validateManagedProxySubscriptionURL + 定制 DialContext）保持不变。
func FetchProxySubscription(ctx context.Context, subscriptionURL string) (*ParsedProxySubscription, error) {
	if err := validateHTTPURL(subscriptionURL, "subscription_url"); err != nil {
		return nil, err
	}
	if err := validateManagedProxySubscriptionURL(subscriptionURL); err != nil {
		return nil, err
	}
	userAgent, appendClashFlag := managedProxySubscriptionFetchOptions()
	fetchURL := subscriptionURL
	if appendClashFlag {
		// 仅追加 query（host/scheme 不变），SSRF 校验结果不受影响。
		fetchURL = ensureClashProxyFormatFlag(subscriptionURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	client := managedProxySubscriptionHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("subscription fetch failed with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManagedProxySubscriptionBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxManagedProxySubscriptionBytes {
		return nil, errors.New("subscription is too large")
	}
	return ParseProxySubscription(body)
}

// FetchProxySubscriptionNodes 保留旧签名，只返回解析出的节点。
func FetchProxySubscriptionNodes(ctx context.Context, subscriptionURL string) ([]ProxySubscriptionNode, error) {
	parsed, err := FetchProxySubscription(ctx, subscriptionURL)
	if err != nil {
		return nil, err
	}
	return parsed.Nodes, nil
}

func managedProxySubscriptionHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := resolveManagedProxySubscriptionHost(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return validateManagedProxySubscriptionURL(req.URL.String())
		},
	}
}

func validateManagedProxySubscriptionURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Hostname() == "" {
		return errors.New("subscription_url is invalid")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("subscription_url must use http or https")
	}
	return validateManagedProxySubscriptionHost(context.Background(), u.Hostname())
}

func validateManagedProxySubscriptionHost(ctx context.Context, host string) error {
	_, err := resolveManagedProxySubscriptionHost(ctx, host)
	return err
}

func resolveManagedProxySubscriptionHost(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("subscription_url must include host")
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return nil, fmt.Errorf("subscription_url host is not allowed: %s", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isManagedProxySubscriptionAllowedIP(ip) {
			return nil, fmt.Errorf("subscription_url resolved ip is not allowed: %s", ip.String())
		}
		return []net.IP{ip}, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(lookupCtx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("subscription_url dns resolution failed: %w", err)
	}
	if len(ips) == 0 {
		return nil, errors.New("subscription_url dns resolution returned no addresses")
	}
	for _, ip := range ips {
		if !isManagedProxySubscriptionAllowedIP(ip) {
			return nil, fmt.Errorf("subscription_url resolved ip is not allowed: %s", ip.String())
		}
	}
	return ips, nil
}

func isManagedProxySubscriptionAllowedIP(ip net.IP) bool {
	if ip == nil ||
		!ip.IsGlobalUnicast() ||
		ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return false
	}
	isIPv4 := false
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		isIPv4 = true
	}
	for _, cidr := range managedProxySubscriptionBlockedCIDRs {
		_, bits := cidr.Mask.Size()
		cidrIsIPv4 := bits == 32
		if cidrIsIPv4 != isIPv4 {
			continue
		}
		if cidr.Contains(ip) {
			return false
		}
	}
	return true
}

func mustParseManagedProxySubscriptionCIDRs(values []string) []*net.IPNet {
	cidrs := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, cidr, err := net.ParseCIDR(value)
		if err != nil {
			panic(fmt.Sprintf("invalid managed proxy subscription CIDR %q: %v", value, err))
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs
}

// ParseProxySubscriptionNodes 保留旧签名，只返回解析出的节点。
func ParseProxySubscriptionNodes(data []byte) ([]ProxySubscriptionNode, error) {
	parsed, err := ParseProxySubscription(data)
	if err != nil {
		return nil, err
	}
	return parsed.Nodes, nil
}

// ParseProxySubscription 解析 Clash/Mihomo 订阅的 proxies 与顶层 dns 配置。
// 顶层 dns 完整保留（re-marshal 为 YAML），后续用于恢复订阅自带的 nameserver-policy。
func ParseProxySubscription(data []byte) (*ParsedProxySubscription, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
		DNS     map[string]any   `yaml:"dns"`
	}
	yamlErr := yaml.Unmarshal(data, &doc)
	if yamlErr != nil || len(doc.Proxies) == 0 {
		// 回退：可能是 base64 / 明文 URI 列表订阅（vmess/vless/ss/trojan/hysteria2/tuic），
		// 而非 Clash YAML。此类订阅不带顶层 dns。
		if uriProxies := parseProxyURIListSubscription(data); len(uriProxies) > 0 {
			doc.Proxies = uriProxies
			doc.DNS = nil
		} else if yamlErr != nil {
			return nil, fmt.Errorf("parse clash subscription: %w", yamlErr)
		}
	}
	if len(doc.Proxies) == 0 {
		return nil, errors.New("subscription contains no Clash proxies")
	}

	seen := make(map[string]struct{}, len(doc.Proxies))
	nodes := make([]ProxySubscriptionNode, 0, len(doc.Proxies))
	for _, raw := range doc.Proxies {
		node, ok, err := proxySubscriptionNodeFromConfig(raw)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if _, exists := seen[node.NodeKey]; exists {
			continue
		}
		seen[node.NodeKey] = struct{}{}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("subscription contains no usable Clash proxies")
	}

	parsed := &ParsedProxySubscription{Nodes: nodes}
	if len(doc.DNS) > 0 {
		rawDNS, err := yaml.Marshal(doc.DNS)
		if err != nil {
			return nil, fmt.Errorf("encode subscription dns config: %w", err)
		}
		parsed.RawDNSConfig = string(rawDNS)
	}
	return parsed, nil
}

func proxySubscriptionNodeFromConfig(raw map[string]any) (ProxySubscriptionNode, bool, error) {
	name := strings.TrimSpace(proxySubscriptionConfigString(raw["name"]))
	nodeType := strings.TrimSpace(proxySubscriptionConfigString(raw["type"]))
	if name == "" || nodeType == "" {
		return ProxySubscriptionNode{}, false, nil
	}
	server := strings.TrimSpace(proxySubscriptionConfigString(raw["server"]))
	port := proxySubscriptionConfigInt(raw["port"])
	if server == "" {
		return ProxySubscriptionNode{}, false, fmt.Errorf("managed proxy node %q is missing server", name)
	}
	if port <= 0 || port > 65535 {
		return ProxySubscriptionNode{}, false, fmt.Errorf("managed proxy node %q has invalid port", name)
	}
	if err := validateManagedProxyNodeServer(server); err != nil {
		return ProxySubscriptionNode{}, false, fmt.Errorf("managed proxy node %q: %w", name, err)
	}

	rawBytes, err := yaml.Marshal(raw)
	if err != nil {
		return ProxySubscriptionNode{}, false, err
	}
	key := managedProxyNodeKey(name, nodeType, server, port)
	return ProxySubscriptionNode{
		NodeKey:      key,
		Name:         name,
		ProviderName: "node-" + key[:16],
		Type:         nodeType,
		Server:       server,
		Port:         port,
		RawConfig:    string(rawBytes),
		Status:       ProxySubscriptionNodeStatusActive,
	}, true, nil
}

func validateManagedProxyNodeConfig(nodeName string, raw map[string]any) error {
	server := strings.TrimSpace(proxySubscriptionConfigString(raw["server"]))
	if server == "" {
		return fmt.Errorf("managed proxy node %s is missing server", nodeName)
	}
	port := proxySubscriptionConfigInt(raw["port"])
	if port <= 0 || port > 65535 {
		return fmt.Errorf("managed proxy node %s has invalid port", nodeName)
	}
	if err := validateManagedProxyNodeServer(server); err != nil {
		return fmt.Errorf("managed proxy node %s: %w", nodeName, err)
	}
	return nil
}

// validateManagedProxyNodeServer 校验节点 server。
// 说明：节点 server 不同于订阅 URL，不能对域名强制走系统 DNS 判定 —— 某些订阅依赖
// 自带的 dns.nameserver-policy 才能把节点域名解析到真实公网 IP，系统 DNS 可能返回
// 内网/loopback 占位地址（如 127.127.127.5）。因此：
//   - 字面私网/loopback/元数据/保留 IP 仍然拒绝（防止明显的内网直连）；
//   - localhost 及其子域拒绝；
//   - 其余域名一律放行，交由 Mihomo 按订阅 DNS policy 解析。
//
// 订阅 URL 自身的 SSRF 防护仍在 validateManagedProxySubscriptionURL 中保留，未放宽。
func validateManagedProxyNodeServer(server string) error {
	server = strings.TrimSpace(server)
	if server == "" {
		return errors.New("proxy node server is required")
	}
	host := strings.Trim(server, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if !isManagedProxySubscriptionAllowedIP(ip) {
			return fmt.Errorf("proxy node server is not allowed: %s", ip.String())
		}
		return nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("proxy node server is not allowed: %s", host)
	}
	return nil
}

func resolveManagedProxyNodeServer(ctx context.Context, server string) ([]net.IP, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, errors.New("proxy node server is required")
	}
	return resolveManagedProxySubscriptionHost(ctx, server)
}

func managedProxyNodeKey(name, nodeType, server string, port int) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(nodeType)) + "|" +
		strings.TrimSpace(name) + "|" +
		strings.ToLower(strings.TrimSpace(server)) + "|" +
		strconv.Itoa(port)))
	return hex.EncodeToString(sum[:])
}

func proxySubscriptionConfigString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func proxySubscriptionConfigInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case uint64:
		return int(v)
	case uint32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}
