package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxManagedProxySubscriptionBytes = 10 << 20

func FetchProxySubscriptionNodes(ctx context.Context, subscriptionURL string) ([]ProxySubscriptionNode, error) {
	if err := validateHTTPURL(subscriptionURL, "subscription_url"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscriptionURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sub2api-managed-proxy/1.0")
	client := &http.Client{Timeout: 20 * time.Second}
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
	return ParseProxySubscriptionNodes(body)
}

func ParseProxySubscriptionNodes(data []byte) ([]ProxySubscriptionNode, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse clash subscription: %w", err)
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
	return nodes, nil
}

func proxySubscriptionNodeFromConfig(raw map[string]any) (ProxySubscriptionNode, bool, error) {
	name := strings.TrimSpace(proxySubscriptionConfigString(raw["name"]))
	nodeType := strings.TrimSpace(proxySubscriptionConfigString(raw["type"]))
	if name == "" || nodeType == "" {
		return ProxySubscriptionNode{}, false, nil
	}
	server := strings.TrimSpace(proxySubscriptionConfigString(raw["server"]))
	port := proxySubscriptionConfigInt(raw["port"])

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
