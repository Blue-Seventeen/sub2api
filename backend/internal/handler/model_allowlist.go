package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func blockedModelAllowlistCandidate(group *service.Group, candidates []string) string {
	if !group.ModelAllowlistEnabled() {
		return ""
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && !group.ModelAllowlist.Allows(candidate) {
			return candidate
		}
	}
	return ""
}
