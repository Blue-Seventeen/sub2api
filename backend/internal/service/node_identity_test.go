package service

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeNodeID(t *testing.T) {
	require.Equal(t, "node-a_1", NormalizeNodeID(" node-a:1 "))
	require.Equal(t, "node.name-01", NormalizeNodeID("node.name-01"))
	require.Empty(t, NormalizeNodeID(" : "))
}

func TestResolveNodeIDUsesExplicitBeforeEnv(t *testing.T) {
	t.Setenv(NodeIDEnvName, "env-node")

	require.Equal(t, "explicit-node", ResolveNodeID("explicit-node"))
}

func TestResolveNodeIDUsesEnvWhenExplicitEmpty(t *testing.T) {
	t.Setenv(NodeIDEnvName, " env-node:01 ")

	require.Equal(t, "env-node_01", ResolveNodeID(""))
}

func TestPublicIPNodeIDFromIPsUsesPublicIP(t *testing.T) {
	got := publicIPNodeIDFromIPs([]net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("10.0.0.8"),
		net.ParseIP("8.8.8.8"),
	})

	require.Equal(t, "ip-8.8.8.8", got)
}

func TestPublicIPNodeIDFromIPsSkipsNonPublicIPs(t *testing.T) {
	got := publicIPNodeIDFromIPs([]net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("172.16.0.8"),
		net.ParseIP("192.168.1.8"),
		net.ParseIP("169.254.1.8"),
	})

	require.Empty(t, got)
}
