package service

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func parseProxyURIListSubscription(data []byte) []map[string]any {
	text := decodeMaybeBase64Subscription(data)
	if text == "" {
		return nil
	}
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	proxies := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "://") {
			continue
		}
		if proxy, ok := proxyURIToClashProxy(line); ok {
			proxies = append(proxies, proxy)
		}
	}
	return proxies
}

func decodeMaybeBase64Subscription(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		return s
	}
	compact := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, s)
	if decoded, ok := decodeBase64Flexible(compact); ok {
		if ds := strings.TrimSpace(string(decoded)); strings.Contains(ds, "://") {
			return ds
		}
	}
	return s
}

func proxyURIToClashProxy(uri string) (map[string]any, bool) {
	switch {
	case strings.HasPrefix(uri, "vmess://"):
		return parseVMessURI(uri)
	case strings.HasPrefix(uri, "vless://"):
		return parseVLESSURI(uri)
	case strings.HasPrefix(uri, "trojan://"):
		return parseTrojanURI(uri)
	case strings.HasPrefix(uri, "ss://"):
		return parseShadowsocksURI(uri)
	case strings.HasPrefix(uri, "hysteria2://"), strings.HasPrefix(uri, "hy2://"):
		return parseHysteria2URI(uri)
	case strings.HasPrefix(uri, "tuic://"):
		return parseTUICURI(uri)
	default:
		return nil, false
	}
}

func parseVMessURI(uri string) (map[string]any, bool) {
	payload := strings.TrimPrefix(uri, "vmess://")
	if i := strings.IndexAny(payload, "#?"); i >= 0 {
		payload = payload[:i]
	}
	decoded, ok := decodeBase64Flexible(payload)
	if !ok {
		return nil, false
	}
	var v map[string]any
	if err := json.Unmarshal(decoded, &v); err != nil {
		return nil, false
	}
	server := strings.TrimSpace(proxySubscriptionConfigString(v["add"]))
	port := proxySubscriptionConfigInt(v["port"])
	id := strings.TrimSpace(proxySubscriptionConfigString(v["id"]))
	if server == "" || port <= 0 || id == "" {
		return nil, false
	}
	name := strings.TrimSpace(proxySubscriptionConfigString(v["ps"]))
	if name == "" {
		name = server
	}
	proxy := map[string]any{
		"name":    name,
		"type":    "vmess",
		"server":  server,
		"port":    port,
		"uuid":    id,
		"alterId": proxySubscriptionConfigInt(v["aid"]),
		"cipher":  firstNonEmptyURIString(proxySubscriptionConfigString(v["scy"]), "auto"),
		"udp":     true,
	}
	network := strings.ToLower(strings.TrimSpace(proxySubscriptionConfigString(v["net"])))
	host := strings.TrimSpace(proxySubscriptionConfigString(v["host"]))
	path := strings.TrimSpace(proxySubscriptionConfigString(v["path"]))
	switch network {
	case "ws":
		proxy["network"] = "ws"
		if opts := proxyURIWSOpts(path, host); len(opts) > 0 {
			proxy["ws-opts"] = opts
		}
	case "grpc":
		proxy["network"] = "grpc"
		if path != "" {
			proxy["grpc-opts"] = map[string]any{"grpc-service-name": path}
		}
	case "h2":
		proxy["network"] = "h2"
		h2 := map[string]any{}
		if path != "" {
			h2["path"] = path
		}
		if host != "" {
			h2["host"] = []any{host}
		}
		if len(h2) > 0 {
			proxy["h2-opts"] = h2
		}
	}
	if tls := strings.ToLower(strings.TrimSpace(proxySubscriptionConfigString(v["tls"]))); tls == "tls" || tls == "reality" {
		proxy["tls"] = true
		if sni := firstNonEmptyURIString(proxySubscriptionConfigString(v["sni"]), host); sni != "" {
			proxy["servername"] = sni
		}
		if alpn := splitCSVToList(proxySubscriptionConfigString(v["alpn"])); len(alpn) > 0 {
			proxy["alpn"] = alpn
		}
	}
	return proxy, true
}

func parseVLESSURI(uri string) (map[string]any, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, false
	}
	uuid := u.User.Username()
	host := u.Hostname()
	port := atoiSafe(u.Port())
	if uuid == "" || host == "" || port <= 0 {
		return nil, false
	}
	q := u.Query()
	proxy := map[string]any{
		"name":   uriFragmentName(u, host),
		"type":   "vless",
		"server": host,
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}
	if flow := strings.TrimSpace(q.Get("flow")); flow != "" {
		proxy["flow"] = flow
	}
	sni := firstNonEmptyURIString(q.Get("sni"), q.Get("peer"), host)
	switch strings.ToLower(q.Get("security")) {
	case "tls":
		proxy["tls"] = true
		if sni != "" {
			proxy["servername"] = sni
		}
	case "reality":
		proxy["tls"] = true
		if sni != "" {
			proxy["servername"] = sni
		}
		reality := map[string]any{}
		if pbk := strings.TrimSpace(q.Get("pbk")); pbk != "" {
			reality["public-key"] = pbk
		}
		if sid := strings.TrimSpace(q.Get("sid")); sid != "" {
			reality["short-id"] = sid
		}
		if len(reality) > 0 {
			proxy["reality-opts"] = reality
		}
	}
	if fp := strings.TrimSpace(q.Get("fp")); fp != "" {
		proxy["client-fingerprint"] = fp
	}
	applyTransportOpts(proxy, q)
	if alpn := splitCSVToList(q.Get("alpn")); len(alpn) > 0 {
		proxy["alpn"] = alpn
	}
	return proxy, true
}

func parseTrojanURI(uri string) (map[string]any, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, false
	}
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		password = password + ":" + pw
	}
	host := u.Hostname()
	port := atoiSafe(u.Port())
	if password == "" || host == "" || port <= 0 {
		return nil, false
	}
	q := u.Query()
	proxy := map[string]any{
		"name":     uriFragmentName(u, host),
		"type":     "trojan",
		"server":   host,
		"port":     port,
		"password": password,
		"udp":      true,
	}
	if sni := firstNonEmptyURIString(q.Get("sni"), q.Get("peer"), host); sni != "" {
		proxy["sni"] = sni
	}
	if uriSkipCertVerify(q) {
		proxy["skip-cert-verify"] = true
	}
	applyTransportOpts(proxy, q)
	if alpn := splitCSVToList(q.Get("alpn")); len(alpn) > 0 {
		proxy["alpn"] = alpn
	}
	return proxy, true
}

func parseShadowsocksURI(uri string) (map[string]any, bool) {
	rest := strings.TrimPrefix(uri, "ss://")
	name := ""
	if i := strings.Index(rest, "#"); i >= 0 {
		name = uriUnescape(rest[i+1:])
		rest = rest[:i]
	}
	if i := strings.Index(rest, "?"); i >= 0 {
		rest = rest[:i]
	}
	var method, password, host string
	var port int
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		userinfo := rest[:at]
		if decoded, ok := decodeBase64Flexible(userinfo); ok && strings.Contains(string(decoded), ":") {
			method, password = splitOnFirstColon(string(decoded))
		} else {
			method, password = splitOnFirstColon(uriUnescape(userinfo))
		}
		host, port = splitHostPortSafe(rest[at+1:])
	} else {
		decoded, ok := decodeBase64Flexible(rest)
		if !ok {
			return nil, false
		}
		s := string(decoded)
		at2 := strings.LastIndex(s, "@")
		if at2 < 0 {
			return nil, false
		}
		method, password = splitOnFirstColon(s[:at2])
		host, port = splitHostPortSafe(s[at2+1:])
	}
	if method == "" || password == "" || host == "" || port <= 0 {
		return nil, false
	}
	if name == "" {
		name = host
	}
	return map[string]any{
		"name":     name,
		"type":     "ss",
		"server":   host,
		"port":     port,
		"cipher":   method,
		"password": password,
		"udp":      true,
	}, true
}

func parseHysteria2URI(uri string) (map[string]any, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, false
	}
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok && pw != "" {
		password = password + ":" + pw
	}
	host := u.Hostname()
	port := atoiSafe(u.Port())
	if password == "" || host == "" || port <= 0 {
		return nil, false
	}
	q := u.Query()
	proxy := map[string]any{
		"name":     uriFragmentName(u, host),
		"type":     "hysteria2",
		"server":   host,
		"port":     port,
		"password": password,
	}
	if sni := firstNonEmptyURIString(q.Get("sni"), q.Get("peer")); sni != "" {
		proxy["sni"] = sni
	}
	if uriSkipCertVerify(q) {
		proxy["skip-cert-verify"] = true
	}
	if obfs := strings.TrimSpace(q.Get("obfs")); obfs != "" {
		proxy["obfs"] = obfs
		if op := firstNonEmptyURIString(q.Get("obfs-password"), q.Get("obfs_password")); op != "" {
			proxy["obfs-password"] = op
		}
	}
	if alpn := splitCSVToList(q.Get("alpn")); len(alpn) > 0 {
		proxy["alpn"] = alpn
	}
	return proxy, true
}

func parseTUICURI(uri string) (map[string]any, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, false
	}
	uuid := u.User.Username()
	password, _ := u.User.Password()
	host := u.Hostname()
	port := atoiSafe(u.Port())
	if uuid == "" || password == "" || host == "" || port <= 0 {
		return nil, false
	}
	q := u.Query()
	proxy := map[string]any{
		"name":     uriFragmentName(u, host),
		"type":     "tuic",
		"server":   host,
		"port":     port,
		"uuid":     uuid,
		"password": password,
		"udp":      true,
	}
	if sni := strings.TrimSpace(q.Get("sni")); sni != "" {
		proxy["sni"] = sni
	}
	if uriSkipCertVerify(q) {
		proxy["skip-cert-verify"] = true
	}
	if cc := firstNonEmptyURIString(q.Get("congestion_control"), q.Get("congestion-controller")); cc != "" {
		proxy["congestion-controller"] = cc
	}
	if mode := firstNonEmptyURIString(q.Get("udp_relay_mode"), q.Get("udp-relay-mode")); mode != "" {
		proxy["udp-relay-mode"] = mode
	}
	if alpn := splitCSVToList(q.Get("alpn")); len(alpn) > 0 {
		proxy["alpn"] = alpn
	}
	return proxy, true
}

func applyTransportOpts(proxy map[string]any, q url.Values) {
	switch strings.ToLower(q.Get("type")) {
	case "ws":
		proxy["network"] = "ws"
		if opts := proxyURIWSOpts(q.Get("path"), q.Get("host")); len(opts) > 0 {
			proxy["ws-opts"] = opts
		}
	case "grpc":
		proxy["network"] = "grpc"
		if sn := firstNonEmptyURIString(q.Get("serviceName"), q.Get("servicename")); sn != "" {
			proxy["grpc-opts"] = map[string]any{"grpc-service-name": sn}
		}
	}
}

func proxyURIWSOpts(path, host string) map[string]any {
	opts := map[string]any{}
	if p := strings.TrimSpace(path); p != "" {
		opts["path"] = p
	}
	if h := strings.TrimSpace(host); h != "" {
		opts["headers"] = map[string]any{"Host": h}
	}
	return opts
}

func uriFragmentName(u *url.URL, fallback string) string {
	if u == nil {
		return fallback
	}
	if name := strings.TrimSpace(uriUnescape(u.Fragment)); name != "" {
		return name
	}
	return fallback
}

func decodeBase64Flexible(s string) ([]byte, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, true
		}
	}
	return nil, false
}

func uriUnescape(s string) string {
	if d, err := url.QueryUnescape(s); err == nil {
		return d
	}
	return s
}

func splitOnFirstColon(s string) (string, string) {
	if i := strings.Index(s, ":"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func splitHostPortSafe(s string) (string, int) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(s))
	if err != nil {
		return "", 0
	}
	return host, atoiSafe(port)
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func firstNonEmptyURIString(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func splitCSVToList(s string) []any {
	parts := strings.Split(s, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uriSkipCertVerify(q url.Values) bool {
	for _, key := range []string{"insecure", "allowInsecure", "allow_insecure", "skip-cert-verify"} {
		switch strings.ToLower(strings.TrimSpace(q.Get(key))) {
		case "1", "true":
			return true
		}
	}
	return false
}
