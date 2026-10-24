package service

import (
	"net"
	"strconv"
	"strings"
)

// ProxyRouteSnapshot records the logical route selected for an actual outbound
// attempt. It intentionally excludes proxy URLs and authentication credentials.
// Keep snapshots independent of mutable account and proxy configuration.
type ProxyRouteSnapshot struct {
	Kind    string `json:"kind"`
	ProxyID int64  `json:"proxy_id,omitempty"`
	Name    string `json:"name,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
}

// Address formats the logical proxy endpoint without any credentials or scheme.
func (s ProxyRouteSnapshot) Address() string {
	return net.JoinHostPort(strings.Trim(s.Host, "[]"), strconv.Itoa(s.Port))
}
