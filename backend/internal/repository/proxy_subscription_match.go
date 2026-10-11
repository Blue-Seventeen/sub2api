package repository

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type subscriptionNodeIdentity struct{ name, protocol string }

func subscriptionNodeGroup(node service.ProxySubscriptionNode) subscriptionNodeIdentity {
	return subscriptionNodeIdentity{strings.TrimSpace(node.Name), strings.ToLower(strings.TrimSpace(node.Type))}
}

// Reserve exact current identities before logical matches, so an incoming node
// cannot steal a row that belongs to a later exact match. A unique current row
// takes priority over historical endpoints when an address rotates back.
func matchSubscriptionNodes(existing, incoming []service.ProxySubscriptionNode) []int {
	matches := make([]int, len(incoming))
	for i := range matches {
		matches[i] = -1
	}
	used := make([]bool, len(existing))
	byKey := make(map[string]int, len(existing))
	current := make(map[subscriptionNodeIdentity][]int)
	historical := make(map[subscriptionNodeIdentity][]int)
	incomingCount := make(map[subscriptionNodeIdentity]int)
	for i, node := range existing {
		byKey[node.NodeKey] = i
		key := subscriptionNodeGroup(node)
		if node.Status == service.ProxySubscriptionNodeStatusSourceMissing {
			historical[key] = append(historical[key], i)
		} else {
			current[key] = append(current[key], i)
		}
	}
	for _, node := range incoming {
		incomingCount[subscriptionNodeGroup(node)]++
	}
	exact := func(history bool) {
		for i, node := range incoming {
			if matches[i] != -1 {
				continue
			}
			j, ok := byKey[node.NodeKey]
			if !ok || used[j] || (existing[j].Status == service.ProxySubscriptionNodeStatusSourceMissing) != history {
				continue
			}
			if subscriptionNodeGroup(existing[j]).protocol != subscriptionNodeGroup(node).protocol {
				continue
			}
			matches[i], used[j] = j, true
		}
	}
	logical := func(history bool) {
		candidates := current
		if history {
			candidates = historical
		}
		for i, node := range incoming {
			key := subscriptionNodeGroup(node)
			if matches[i] != -1 || key.name == "" || key.protocol == "" || incomingCount[key] != 1 || len(candidates[key]) != 1 {
				continue
			}
			if history && len(current[key]) != 0 {
				continue
			}
			j := candidates[key][0]
			if used[j] {
				continue
			}
			matches[i], used[j] = j, true
		}
	}
	exact(false)
	logical(false)
	exact(true)
	logical(true)
	return matches
}
