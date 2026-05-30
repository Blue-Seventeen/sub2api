package service

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"strings"
	"sync"
)

const NodeIDEnvName = "NODE_ID"

var (
	defaultNodeIDOnce sync.Once
	defaultNodeID     string
)

func CurrentNodeID() string {
	defaultNodeIDOnce.Do(func() {
		defaultNodeID = ResolveNodeID("")
	})
	return defaultNodeID
}

func ResolveNodeID(explicit string) string {
	if id := NormalizeNodeID(explicit); id != "" {
		return id
	}
	if id := NormalizeNodeID(os.Getenv(NodeIDEnvName)); id != "" {
		return id
	}
	if id := publicIPNodeID(); id != "" {
		return id
	}
	if hostname, err := os.Hostname(); err == nil {
		if id := NormalizeNodeID(hostname); id != "" {
			return id
		}
	}
	if id := randomNodeID(); id != "" {
		return id
	}
	return "node-local"
}

func publicIPNodeID() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		switch v := addr.(type) {
		case *net.IPNet:
			ips = append(ips, v.IP)
		case *net.IPAddr:
			ips = append(ips, v.IP)
		}
	}
	return publicIPNodeIDFromIPs(ips)
}

func publicIPNodeIDFromIPs(ips []net.IP) string {
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if !isPublicNodeIP(ip) {
			continue
		}
		if id := NormalizeNodeID("ip-" + ip.String()); id != "" {
			return id
		}
	}
	return ""
}

func isPublicNodeIP(ip net.IP) bool {
	return ip != nil &&
		ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() &&
		!ip.IsUnspecified()
}

func NormalizeNodeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.'
		if allowed {
			_, _ = b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			_ = b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_.-")
	if len(out) > 96 {
		out = out[:96]
	}
	return out
}

func randomNodeID() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	return "node-" + hex.EncodeToString(buf[:])
}
