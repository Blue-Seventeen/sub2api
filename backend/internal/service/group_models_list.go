package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ValidateModelsListConfig validates canonical policy without changing enabled.
func ValidateModelsListConfig(cfg GroupModelsListConfig) error {
	if cfg.Enabled && len(normalizeGroupModelsListConfig(cfg).Models) == 0 {
		return infraerrors.BadRequest("INVALID_MODEL_ALLOWLIST", "model allowlist cannot be enabled with an empty model list")
	}
	return nil
}

func normalizeGroupModelsListConfig(cfg GroupModelsListConfig) GroupModelsListConfig {
	out := GroupModelsListConfig{Enabled: cfg.Enabled}
	if len(cfg.Models) == 0 {
		return out
	}

	seen := make(map[string]struct{}, len(cfg.Models))
	out.Models = make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := normalizeModelsListMatchKey(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.Models = append(out.Models, model)
	}
	if len(out.Models) == 0 {
		out.Models = nil
	}
	return out
}

func (g *Group) CustomModelsListEnabled() bool {
	return g != nil && g.ModelsListConfig.Enabled
}

func GroupAllowsRequestedModel(group *Group, model string) bool {
	if group == nil {
		return true
	}
	return group.EffectiveGroupModelPolicy().Allows(model)
}

func ModelsListAllowsModel(patterns []string, model string) bool {
	model = normalizeModelsListMatchKey(model)
	if model == "" {
		return false
	}
	for _, rawPattern := range patterns {
		pattern := normalizeModelsListMatchKey(rawPattern)
		if pattern == "" {
			continue
		}
		if groupAllowlistPatternMatches(pattern, model) {
			return true
		}
	}
	return false
}

func normalizeModelsListMatchKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
