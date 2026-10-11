package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestMatchSubscriptionNodes(t *testing.T) {
	node := func(id int64, key, name, typ, status string) service.ProxySubscriptionNode {
		return service.ProxySubscriptionNode{ID: id, NodeKey: key, Name: name, Type: typ, Status: status}
	}
	active, missing, disabled := service.ProxySubscriptionNodeStatusActive, service.ProxySubscriptionNodeStatusSourceMissing, service.ProxySubscriptionNodeStatusInactive
	cases := []struct {
		name          string
		old, incoming []service.ProxySubscriptionNode
		want          []int
	}{
		{"exact before logical", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", active), node(2, "b", "JP", "ss", active)}, []service.ProxySubscriptionNode{node(0, "new", "JP", "ss", active), node(0, "b", "HK", "ss", active)}, []int{-1, 1}},
		{"unique rotation", []service.ProxySubscriptionNode{node(1, "a", " HK ", "SS", active)}, []service.ProxySubscriptionNode{node(0, "b", "HK", "ss", active)}, []int{0}},
		{"current before exact history", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", missing), node(2, "b", "HK", "ss", active)}, []service.ProxySubscriptionNode{node(0, "a", "HK", "ss", active)}, []int{1}},
		{"disabled canonical preserved", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", missing), node(2, "b", "HK", "ss", disabled)}, []service.ProxySubscriptionNode{node(0, "c", "HK", "ss", active)}, []int{1}},
		{"ambiguous incoming", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", active)}, []service.ProxySubscriptionNode{node(0, "b", "HK", "ss", active), node(0, "c", "HK", "ss", active)}, []int{-1, -1}},
		{"ambiguous current", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", active), node(2, "b", "HK", "ss", disabled)}, []service.ProxySubscriptionNode{node(0, "c", "HK", "ss", active)}, []int{-1}},
		{"protocol changed", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", active)}, []service.ProxySubscriptionNode{node(0, "b", "HK", "trojan", active)}, []int{-1}},
		{"unique historical return", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", missing)}, []service.ProxySubscriptionNode{node(0, "b", "HK", "ss", active)}, []int{0}},
		{"ambiguous historical", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", missing), node(2, "b", "HK", "ss", missing)}, []service.ProxySubscriptionNode{node(0, "c", "HK", "ss", active)}, []int{-1}},
		{"exact history in ambiguous group", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", missing), node(2, "b", "HK", "ss", missing)}, []service.ProxySubscriptionNode{node(0, "b", "HK", "ss", active)}, []int{1}},
		{"existing used once", []service.ProxySubscriptionNode{node(1, "a", "HK", "ss", active)}, []service.ProxySubscriptionNode{node(0, "a", "HK", "ss", active), node(0, "b", "HK", "ss", active)}, []int{0, -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, matchSubscriptionNodes(tc.old, tc.incoming)) })
	}
}
