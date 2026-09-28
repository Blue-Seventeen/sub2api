package service

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// GroupModelOperation describes one ordered global model-list mutation.
type GroupModelOperation struct {
	Operation string `json:"operation"`
	Model     string `json:"model"`
}

// GlobalModelOperationSummary reports the committed scope and effects of a
// global model-list mutation.
type GlobalModelOperationSummary struct {
	TargetPlatform     string   `json:"target_platform"`
	AffectedGroupCount int      `json:"affected_group_count"`
	AddedModels        []string `json:"added_models"`
	RemovedModels      []string `json:"removed_models"`
	AffectedGroupIDs   []int64  `json:"-"`
}

func (s GlobalModelOperationSummary) MarshalJSON() ([]byte, error) {
	type summary GlobalModelOperationSummary
	out := summary(s)
	if out.AddedModels == nil {
		out.AddedModels = []string{}
	}
	if out.RemovedModels == nil {
		out.RemovedModels = []string{}
	}
	return json.Marshal(out)
}

// ApplyGlobalModelOperations applies ordered exact model operations to one
// group configuration. Matching is case-insensitive, while stored additions
// retain the requested spelling and wildcard entries are never expanded.
func ApplyGlobalModelOperations(config GroupModelsListConfig, operations []GroupModelOperation) (GroupModelsListConfig, bool, []string, []string, error) {
	updated := normalizeGroupModelsListConfig(config)
	original := append([]string(nil), updated.Models...)
	for _, operation := range operations {
		model := strings.TrimSpace(operation.Model)
		if model == "" {
			return GroupModelsListConfig{}, false, nil, nil, infraerrors.BadRequest("INVALID_GLOBAL_MODEL_OPERATION", "global model operation model is required")
		}
		operationName := strings.ToLower(strings.TrimSpace(operation.Operation))
		index := -1
		for i, existing := range updated.Models {
			if strings.EqualFold(existing, model) {
				index = i
				break
			}
		}
		switch operationName {
		case "add":
			if index < 0 {
				updated.Models = append(updated.Models, model)
			}
		case "remove":
			if index >= 0 {
				updated.Models = append(updated.Models[:index], updated.Models[index+1:]...)
			}
		default:
			return GroupModelsListConfig{}, false, nil, nil, infraerrors.Newf(http.StatusBadRequest, "INVALID_GLOBAL_MODEL_OPERATION", "unsupported global model operation: %s", operation.Operation)
		}
	}
	if err := ValidateModelsListConfig(updated); err != nil {
		return GroupModelsListConfig{}, false, nil, nil, err
	}
	updated = normalizeGroupModelsListConfig(updated)
	added, removed := []string{}, []string{}
	for _, model := range updated.Models {
		if !containsExactModel(original, model) {
			added = appendUniqueFold(added, model)
		}
	}
	for _, model := range original {
		if !containsExactModel(updated.Models, model) {
			removed = appendUniqueFold(removed, model)
		}
	}
	return updated, !stringSlicesEqual(original, updated.Models), added, removed, nil
}

func containsExactModel(models []string, model string) bool {
	for _, existing := range models {
		if strings.EqualFold(existing, model) {
			return true
		}
	}
	return false
}

func appendUniqueFold(models []string, model string) []string {
	if containsExactModel(models, model) {
		return models
	}
	return append(models, model)
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// GroupModelAllowlist 是 service 层的分组模型白名单（与 domain.GroupModelAllowlist
// 字段一致，ent 持久化用 domain 类型，边界处显式转换）。
type GroupModelAllowlist struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models,omitempty"`
}

// GroupModelPolicy is the canonical group model policy plus its persisted
// compatibility mirror. ModelsListConfig is the business source of truth;
// ModelAllowlist is kept equal to it for old projections and cache readers.
type GroupModelPolicy struct {
	ModelsListConfig GroupModelsListConfig
	ModelAllowlist   GroupModelAllowlist
}

// DomainGroupModelAllowlist 把 service 白名单转换为 ent 持久化使用的 domain 类型。
func DomainGroupModelAllowlist(cfg GroupModelAllowlist) domain.GroupModelAllowlist {
	return domain.GroupModelAllowlist{Enabled: cfg.Enabled, Models: cfg.Models}
}

// GroupModelAllowlistFromDomain 把 ent 读出的 domain 白名单转换为 service 类型。
func GroupModelAllowlistFromDomain(cfg domain.GroupModelAllowlist) GroupModelAllowlist {
	return GroupModelAllowlist{Enabled: cfg.Enabled, Models: cfg.Models}
}

func groupModelAllowlistFromModelsListConfig(cfg GroupModelsListConfig) GroupModelAllowlist {
	return GroupModelAllowlist{Enabled: cfg.Enabled, Models: append([]string(nil), cfg.Models...)}
}

// NormalizeGroupModelPolicy normalizes the canonical models_list_config and
// returns the exact compatibility mirror that must be persisted alongside it.
func NormalizeGroupModelPolicy(cfg GroupModelsListConfig) (GroupModelPolicy, error) {
	if err := ValidateModelsListConfig(cfg); err != nil {
		return GroupModelPolicy{}, err
	}
	normalized := normalizeGroupModelsListConfig(cfg)
	return GroupModelPolicy{
		ModelsListConfig: normalized,
		ModelAllowlist:   groupModelAllowlistFromModelsListConfig(normalized),
	}, nil
}

func hasCanonicalGroupModelsListConfig(cfg GroupModelsListConfig) bool {
	return cfg.Enabled || len(cfg.Models) > 0
}

// supplementUnmappedOpenAIModels ensures a partial mapping catalog does not
// hide models from unmapped or passthrough OpenAI accounts (passthrough routing
// ignores model_mapping, so it serves the same default set as an unmapped
// account). An empty catalog is left unchanged so callers retain their existing
// discovery fallback.
func supplementUnmappedOpenAIModels(accounts []Account, models []string) []string {
	if len(models) == 0 {
		return models
	}
	for i := range accounts {
		account := &accounts[i]
		if account.Platform == PlatformOpenAI && (account.IsOpenAIPassthroughEnabled() || len(account.GetModelMapping()) == 0) {
			return dedupeAndSortModelIDs(slices.Concat(models, openai.DefaultModelIDs()))
		}
	}
	return models
}

// normalizeGroupModelAllowlist 归一化管理端提交的分组模型白名单：
// 条目 TrimSpace、按小写去重保序；`*` 可出现在任意位置；
// enabled=true 且列表为空视为配置错误，返回 400 而不是运行时静默放行/拒绝。
func normalizeGroupModelAllowlist(cfg GroupModelAllowlist) (GroupModelAllowlist, error) {
	out := GroupModelAllowlist{Enabled: cfg.Enabled}
	if len(cfg.Models) == 0 {
		if out.Enabled {
			return out, infraerrors.New(http.StatusBadRequest, "INVALID_MODEL_ALLOWLIST", "model allowlist cannot be enabled with an empty model list")
		}
		return out, nil
	}

	seen := make(map[string]struct{}, len(cfg.Models))
	out.Models = make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.Models = append(out.Models, model)
	}
	if len(out.Models) == 0 {
		if out.Enabled {
			return out, infraerrors.New(http.StatusBadRequest, "INVALID_MODEL_ALLOWLIST", "model allowlist cannot be enabled with an empty model list")
		}
		out.Models = nil
	}
	return out, nil
}

// ModelAllowlistEnabled 报告该分组是否启用了模型白名单。
// 开启后所有携带模型的网关请求与模型列表接口都受白名单约束。
func (g *Group) ModelAllowlistEnabled() bool {
	return g != nil && g.EffectiveModelPolicy().Enabled
}

// EffectiveGroupModelPolicy returns the canonical models_list_config policy.
// Hydrated records always have a persisted canonical field, including an
// explicitly empty configuration. The legacy mirror is used only by old
// in-memory fixtures that were never hydrated from repository/cache storage.
func (g *Group) EffectiveGroupModelPolicy() GroupModelAllowlist {
	if g == nil {
		return GroupModelAllowlist{}
	}
	if g.Hydrated || hasCanonicalGroupModelsListConfig(g.ModelsListConfig) {
		return groupModelAllowlistFromModelsListConfig(normalizeGroupModelsListConfig(g.ModelsListConfig))
	}
	return g.ModelAllowlist
}

// EffectiveModelPolicy is retained as a compatibility wrapper.
func (g *Group) EffectiveModelPolicy() GroupModelAllowlist {
	return g.EffectiveGroupModelPolicy()
}

// AllowsModel applies the canonical group model policy to a requested model.
func (g *Group) AllowsModel(model string) bool {
	return g.EffectiveModelPolicy().Allows(model)
}

// Allows 判断客户端请求的模型是否命中白名单。
// 准入只看客户端书写的模型名，与账号映射、渠道映射、合成路由改写无关；
// 候选形式覆盖代码中已有的模型名等价规则（Gemini models/ 前缀、
// Antigravity/Claude -thinking 宽容规则、OpenAI 推理后缀），不做模糊匹配。
func (a GroupModelAllowlist) Allows(model string) bool {
	if !a.Enabled {
		return true
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	candidates := groupModelAllowlistCandidates(model)
	for _, entry := range a.Models {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		for _, candidate := range candidates {
			if groupAllowlistPatternMatches(entry, candidate) {
				return true
			}
		}
	}
	return false
}

// groupAllowlistPatternMatches treats only * as a wildcard, including in the middle
// or at either end. Matching is anchored and case-insensitive.
func groupAllowlistPatternMatches(pattern, model string) bool {
	pattern = strings.ToLower(pattern)
	model = strings.ToLower(model)
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == model
	}
	if !strings.HasPrefix(model, parts[0]) {
		return false
	}
	model = model[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		index := strings.Index(model, part)
		if index < 0 {
			return false
		}
		model = model[index+len(part):]
	}
	return strings.HasSuffix(model, parts[len(parts)-1])
}

// groupModelAllowlistCandidates 返回客户端模型名在白名单匹配中的候选形式（均已小写）。
func groupModelAllowlistCandidates(model string) []string {
	candidates := make([]string, 0, 4)
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if existing == value {
				return
			}
		}
		candidates = append(candidates, value)
	}

	add(model)
	add(strings.TrimPrefix(model, "models/"))
	add(claude.NormalizeModelID(strings.TrimSuffix(model, "-thinking")))
	add(NormalizeOpenAICompatRequestedModel(model))
	return candidates
}

// FilterForListing 按白名单条目顺序生成模型列表输出。
// 精确条目沿用既有规则：只有出现在 source（账号映射键 ∪ 平台默认列表）的模式
// 集合中才输出；通配条目展开为 source 中所有匹配项并保持 source 顺序；全局去重。
// 当 source 是前缀通配模式且白名单模式的固定前缀位于该前缀之内时，
// 可直接输出白名单模式：它的所有实例都属于 source。其他未处理的无限
// 交集不会被枚举为模型 ID，列表可能省略仍可请求的模型。
func (a GroupModelAllowlist) FilterForListing(source []string) []string {
	if !a.Enabled {
		return source
	}
	if len(a.Models) == 0 {
		// 开启但为空的配置在管理端已被拒绝；对遗留脏数据保持“看到什么 = 能调什么”，
		// 列表与准入同时返回空。
		return nil
	}

	patterns := make([]string, 0, len(source))
	for _, pattern := range source {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	if len(patterns) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(a.Models)+len(patterns))
	filtered := make([]string, 0, len(patterns))
	add := func(model string) {
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		filtered = append(filtered, model)
	}

	for _, entry := range a.Models {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "*") {
			for _, pattern := range patterns {
				matched := groupAllowlistPatternMatches(entry, pattern)
				if !matched {
					for _, candidate := range groupModelAllowlistCandidates(pattern) {
						if groupAllowlistPatternMatches(entry, candidate) {
							matched = true
							break
						}
					}
				}
				if matched {
					add(pattern)
					continue
				}
				// A source prefix wildcard can safely expose a narrower allowlist
				// pattern only when the source covers that pattern in full. Do not
				// expose an unrepresentable intersection such as gpt-5* ∩ *-codex.
				if !strings.HasSuffix(pattern, "*") || strings.Contains(strings.TrimSuffix(pattern, "*"), "*") {
					continue
				}
				sourcePrefix := strings.ToLower(strings.TrimSuffix(pattern, "*"))
				entryWildcard := strings.Index(entry, "*")
				if sourcePrefix == "" || entryWildcard <= 0 {
					continue
				}
				entryPrefix := strings.ToLower(entry[:entryWildcard])
				if strings.HasPrefix(entryPrefix, sourcePrefix) {
					add(entry)
				}
			}
			continue
		}
		if allowlistSourcePatternAllowsModel(patterns, entry) {
			add(entry)
		}
	}
	return filtered
}

// allowlistSourcePatternAllowsModel 比较 source 与白名单条目在同一组候选
// 形式下是否等价。source 通配模式仍按前缀匹配，保证「准入允许 ⇒ 列表可见」。
func allowlistSourcePatternAllowsModel(patterns []string, model string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.ToLower(strings.TrimSuffix(pattern, "*"))
			for _, candidate := range groupModelAllowlistCandidates(model) {
				if strings.HasPrefix(candidate, prefix) {
					return true
				}
			}
			continue
		}
		if allowlistModelsEquivalent(pattern, model) {
			return true
		}
	}
	return false
}

func allowlistModelsEquivalent(left, right string) bool {
	leftCandidates := groupModelAllowlistCandidates(left)
	rightCandidates := groupModelAllowlistCandidates(right)
	for _, leftCandidate := range leftCandidates {
		for _, rightCandidate := range rightCandidates {
			if leftCandidate == rightCandidate {
				return true
			}
		}
	}
	return false
}
