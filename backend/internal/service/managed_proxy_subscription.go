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
const defaultManagedProxySubscriptionUserAgent = "sub2api-managed-proxy/1.0"

var (
	managedProxySubscriptionFetchMu         sync.RWMutex
	managedProxySubscriptionUserAgent       = defaultManagedProxySubscriptionUserAgent
	managedProxySubscriptionAppendClashFlag = false
)

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
	// RawDNSConfig is sanitized YAML containing only DNS fields required for node resolution.
	// Empty when the subscription does not provide usable DNS policy fields.
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

// ParseProxySubscription parses Clash/Mihomo proxies and whitelisted DNS policy fields.
func ParseProxySubscription(data []byte) (*ParsedProxySubscription, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
		DNS     map[string]any   `yaml:"dns"`
	}
	yamlErr := yaml.Unmarshal(data, &doc)
	if yamlErr != nil || len(doc.Proxies) == 0 {
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
	policyMatcher := newManagedProxyDNSPolicyMatcher(doc.DNS)
	for _, raw := range doc.Proxies {
		node, ok, err := proxySubscriptionNodeFromConfig(raw, policyMatcher)
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
	if dnsConfig := sanitizeManagedProxySubscriptionDNSConfig(doc.DNS); len(dnsConfig) > 0 {
		rawDNS, err := yaml.Marshal(dnsConfig)
		if err != nil {
			return nil, fmt.Errorf("encode subscription dns config: %w", err)
		}
		parsed.RawDNSConfig = string(rawDNS)
	}
	return parsed, nil
}

func proxySubscriptionNodeFromConfig(raw map[string]any, policyMatcher managedProxyDNSPolicyMatcher) (ProxySubscriptionNode, bool, error) {
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
	allowPolicyDomain := policyMatcher.Matches(server)
	if err := validateManagedProxyNodeServer(context.Background(), server, allowPolicyDomain); err != nil {
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

func validateManagedProxyNodeConfig(ctx context.Context, nodeName string, raw map[string]any, allowPolicyDomains bool) error {
	server := strings.TrimSpace(proxySubscriptionConfigString(raw["server"]))
	if server == "" {
		return fmt.Errorf("managed proxy node %s is missing server", nodeName)
	}
	port := proxySubscriptionConfigInt(raw["port"])
	if port <= 0 || port > 65535 {
		return fmt.Errorf("managed proxy node %s has invalid port", nodeName)
	}
	if err := validateManagedProxyNodeServer(ctx, server, allowPolicyDomains); err != nil {
		return fmt.Errorf("managed proxy node %s: %w", nodeName, err)
	}
	return nil
}

// validateManagedProxyNodeServer validates a proxy node server.
// Literal private/loopback/metadata IPs are always rejected. Domain names keep the
// legacy system-DNS validation unless the subscription supplies dns.nameserver-policy,
// where Mihomo is expected to resolve the node through that policy at runtime.
func validateManagedProxyNodeServer(ctx context.Context, server string, allowPolicyDomains bool) error {
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
	if allowPolicyDomains {
		return nil
	}
	if _, err := resolveManagedProxyNodeServer(ctx, host); err != nil {
		return fmt.Errorf("proxy node server is not allowed: %w", err)
	}
	return nil
}

func managedProxyDNSConfigValuePresent(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	case map[any]any:
		return len(v) > 0
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return true
	}
}

func sanitizeManagedProxySubscriptionDNSConfig(dnsConfig map[string]any) map[string]any {
	if len(dnsConfig) == 0 {
		return nil
	}
	allowed := make(map[string]any)
	for _, key := range []string{
		"nameserver-policy",
		"nameserver",
		"default-nameserver",
		"proxy-server-nameserver",
	} {
		if value, ok := dnsConfig[key]; ok && managedProxyDNSConfigValuePresent(value) {
			allowed[key] = value
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

type managedProxyDNSPolicyMatcher struct {
	exact    map[string]struct{}
	suffixes []string
}

func newManagedProxyDNSPolicyMatcher(dnsConfig map[string]any) managedProxyDNSPolicyMatcher {
	matcher := managedProxyDNSPolicyMatcher{exact: make(map[string]struct{})}
	if len(dnsConfig) == 0 {
		return matcher
	}
	policy, ok := dnsConfig["nameserver-policy"]
	if !ok {
		return matcher
	}
	for _, rule := range managedProxyDNSPolicyRules(policy) {
		matcher.addRule(rule)
	}
	return matcher
}

func managedProxyDNSPolicyRules(policy any) []string {
	switch v := policy.(type) {
	case map[string]any:
		rules := make([]string, 0, len(v))
		for key := range v {
			rules = append(rules, key)
		}
		return rules
	case map[any]any:
		rules := make([]string, 0, len(v))
		for key := range v {
			rules = append(rules, fmt.Sprint(key))
		}
		return rules
	default:
		return nil
	}
}

func (m *managedProxyDNSPolicyMatcher) addRule(rule string) {
	for _, part := range strings.Split(rule, ",") {
		pattern := strings.TrimSpace(strings.ToLower(part))
		if pattern == "" {
			continue
		}
		switch {
		case strings.HasPrefix(pattern, "+."):
			m.addSuffix(pattern[2:])
		case strings.HasPrefix(pattern, "*."):
			m.addSuffix(pattern[2:])
		case strings.HasPrefix(pattern, "domain-suffix:"):
			m.addSuffix(strings.TrimPrefix(pattern, "domain-suffix:"))
		case strings.HasPrefix(pattern, "domain:"):
			m.addExact(strings.TrimPrefix(pattern, "domain:"))
		default:
			if strings.Contains(pattern, ":") {
				continue
			}
			m.addExact(pattern)
		}
	}
}

func (m *managedProxyDNSPolicyMatcher) addExact(host string) {
	host = normalizeManagedProxyPolicyHost(host)
	if host == "" {
		return
	}
	m.exact[host] = struct{}{}
}

func (m *managedProxyDNSPolicyMatcher) addSuffix(host string) {
	host = normalizeManagedProxyPolicyHost(host)
	if host == "" {
		return
	}
	m.suffixes = append(m.suffixes, host)
}

func normalizeManagedProxyPolicyHost(host string) string {
	host = strings.Trim(strings.TrimSpace(strings.ToLower(host)), ".")
	if host == "" || strings.Contains(host, "/") || net.ParseIP(host) != nil {
		return ""
	}
	return host
}

func (m managedProxyDNSPolicyMatcher) Matches(server string) bool {
	host := strings.Trim(strings.TrimSpace(strings.ToLower(server)), "[]")
	host = strings.Trim(host, ".")
	if host == "" || net.ParseIP(host) != nil {
		return false
	}
	if _, ok := m.exact[host]; ok {
		return true
	}
	for _, suffix := range m.suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
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
