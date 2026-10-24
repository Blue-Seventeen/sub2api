package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProxyRouteSnapshotAddress(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{
		{"proxy.example", 8080, "proxy.example:8080"},
		{"192.0.2.10", 1080, "192.0.2.10:1080"},
		{"2001:db8::7", 1080, "[2001:db8::7]:1080"},
		{"[2001:db8::7]", 1080, "[2001:db8::7]:1080"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			snapshot := ProxyRouteSnapshot{Kind: "proxy", Host: tc.host, Port: tc.port}
			addressable, ok := any(snapshot).(interface{ Address() string })
			require.True(t, ok, "proxy attribution must format logical node addresses")
			require.Equal(t, tc.want, addressable.Address())
		})
	}
}

func TestProxyRouteSnapshotJSONContainsOnlyAttribution(t *testing.T) {
	snapshot := ProxyRouteSnapshot{Kind: "proxy", ProxyID: 4, Name: "node", Host: "2001:db8::7", Port: 1080}
	payload, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"proxy","proxy_id":4,"name":"node","host":"2001:db8::7","port":1080}`, string(payload))
}
