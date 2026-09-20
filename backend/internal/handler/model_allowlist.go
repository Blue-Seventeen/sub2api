package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func blockedModelAllowlistCandidate(group *service.Group, candidates []string) string {
	policy := group.EffectiveModelPolicy()
	if !policy.Enabled {
		return ""
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && !policy.Allows(candidate) {
			return candidate
		}
	}
	return ""
}
