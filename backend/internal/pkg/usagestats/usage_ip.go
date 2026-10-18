package usagestats

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

const MaxIPAddresses = 100

// ParseIPAddresses preserves stored spellings so SQL matches the selected values exactly.
func ParseIPAddresses(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > MaxIPAddresses {
		return nil, fmt.Errorf("ip_addresses supports at most %d values", MaxIPAddresses)
	}
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, part := range parts {
		ip := strings.TrimSpace(part)
		addr, err := netip.ParseAddr(ip)
		if err != nil || addr.Zone() != "" {
			return nil, fmt.Errorf("Invalid ip_addresses: use IPv4 or IPv6 literals") //nolint:staticcheck // ST1005: Preserve the public API validation message for compatibility.
		}
		if !seen[ip] {
			out = append(out, ip)
			seen[ip] = true
		}
	}
	sort.Strings(out)
	return out, nil
}

func IPAddressCacheKey(ips []string) []string {
	out := append([]string(nil), ips...)
	sort.Strings(out)
	compact := out[:0]
	for _, ip := range out {
		if len(compact) == 0 || compact[len(compact)-1] != ip {
			compact = append(compact, ip)
		}
	}
	return compact
}

type UsageIPOptions struct {
	IPs     []string `json:"ips"`
	HasMore bool     `json:"has_more"`
}

// AppendIPWhere uses one placeholder per literal, never interpolation of request data.
func AppendIPWhere(conditions []string, args []any, ips []string, column string) ([]string, []any) {
	if len(ips) == 0 {
		return conditions, args
	}
	placeholders := make([]string, len(ips))
	for i, ip := range ips {
		placeholders[i] = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, ip)
	}
	return append(conditions, column+" IN ("+strings.Join(placeholders, ", ")+")"), args
}
