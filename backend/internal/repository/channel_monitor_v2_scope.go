package repository

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// A request uses one immutable canonical-policy snapshot for every rollup,
// histogram, error sample and zero-traffic row. Never cache this across requests:
// disabling a policy must immediately hide historical facts, not delete them.
type channelMonitorV2ScopeKey struct{}

type channelMonitorV2Scope struct {
	groups map[int64]channelMonitorV2GroupInfo
}

type channelMonitorV2LocalRoute struct {
	PublicModel    string `json:"public_model"`
	MatchType      string `json:"match_type"`
	TargetPlatform string `json:"target_platform"`
	Priority       int    `json:"priority"`
}

const channelMonitorV2CanonicalPoliciesSQL = `SELECT g.id, COALESCE(g.name, ''), lower(COALESCE(NULLIF(TRIM(g.platform), ''), 'unknown')),
 COALESCE(g.models_list_config, '{}'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('public_model', r.public_model, 'match_type', r.match_type,
 'target_platform', r.target_platform, 'priority', r.priority) ORDER BY r.priority, r.id)
 FROM composite_model_routes r WHERE r.group_id = g.id AND r.deleted_at IS NULL AND r.enabled), '[]'::jsonb)
 FROM groups g WHERE g.deleted_at IS NULL AND g.status = 'active' ORDER BY g.id`

func channelMonitorV2ScopeFromContext(ctx context.Context) *channelMonitorV2Scope {
	scope, _ := ctx.Value(channelMonitorV2ScopeKey{}).(*channelMonitorV2Scope)
	return scope
}

func (r *channelMonitorV2Repository) resolveChannelMonitorV2Scope(ctx context.Context, filter service.ChannelMonitorV2Filter, cfg service.ChannelMonitorV2Config) (context.Context, service.ChannelMonitorV2Filter, service.ChannelMonitorV2Config, error) {
	scope := &channelMonitorV2Scope{groups: map[int64]channelMonitorV2GroupInfo{}}
	groupIDs, empty := channelMonitorV2ScopedGroupIDs(filter, cfg)
	if !empty {
		rows, err := r.db.QueryContext(ctx, channelMonitorV2CanonicalPoliciesSQL)
		if err != nil {
			return ctx, filter, cfg, err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			var name, platform string
			var policyJSON, routesJSON []byte
			if err := rows.Scan(&id, &name, &platform, &policyJSON, &routesJSON); err != nil {
				return ctx, filter, cfg, err
			}
			if len(groupIDs) > 0 && !containsInt64Monitor(groupIDs, id) {
				continue
			}
			var policy service.GroupModelAllowlist
			if err := json.Unmarshal(policyJSON, &policy); err != nil {
				return ctx, filter, cfg, err
			}
			policy.Models = dedupeMonitorModels(policy.Models)
			if !policy.Enabled || len(policy.Models) == 0 {
				continue
			}
			var routes []channelMonitorV2LocalRoute
			if err := json.Unmarshal(routesJSON, &routes); err != nil {
				return ctx, filter, cfg, err
			}
			info := channelMonitorV2GroupInfo{name: name, platform: platform, policy: policy, modelsByPlatform: map[string][]string{}}
			for _, model := range policy.Models {
				if strings.Contains(model, "*") {
					continue
				}
				modelPlatform := platform
				if platform == service.PlatformComposite {
					modelPlatform = channelMonitorV2LocalModelPlatform(model, routes)
				}
				if len(filter.Platforms) > 0 && !containsString(filter.Platforms, modelPlatform) {
					continue
				}
				if !monitorModelFilterMatches(filter.Models, model) {
					continue
				}
				info.modelsByPlatform[modelPlatform] = append(info.modelsByPlatform[modelPlatform], model)
			}
			scope.groups[id] = info
		}
		if err := rows.Err(); err != nil {
			return ctx, filter, cfg, err
		}
	}
	// An explicit empty allowed scope is different from the admin "all" input.
	filter.RestrictGroups = true
	filter.AllowedGroupIDs = []int64{}
	for id := range scope.groups {
		filter.AllowedGroupIDs = append(filter.AllowedGroupIDs, id)
	}
	sort.Slice(filter.AllowedGroupIDs, func(i, j int) bool { return filter.AllowedGroupIDs[i] < filter.AllowedGroupIDs[j] })
	filter.CanonicalScope = true
	cfg.GroupIDs = append([]int64{}, filter.AllowedGroupIDs...)
	cfg.Platforms = scope.catalog(filter)
	return context.WithValue(ctx, channelMonitorV2ScopeKey{}, scope), filter, cfg, nil
}

func (s *channelMonitorV2Scope) catalog(filter service.ChannelMonitorV2Filter) []service.ChannelMonitorV2PlatformConfig {
	byPlatform := map[string][]string{}
	ids := make([]int64, 0, len(s.groups))
	for id := range s.groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		info := s.groups[id]
		// A policy authorizes facts, but does not itself create a waiting row.
		// Only intersected exact entries can seed a zero-traffic catalog;
		// wildcard entries materialize later from observed eligible tuples.
		for platform, models := range info.modelsByPlatform {
			byPlatform[platform] = append(byPlatform[platform], models...)
		}
	}
	out := []service.ChannelMonitorV2PlatformConfig{}
	for platform, models := range byPlatform {
		out = append(out, service.ChannelMonitorV2PlatformConfig{Platform: platform, Enabled: true, Models: dedupeMonitorModels(models)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Platform < out[j].Platform })
	return out
}

// GetDerivedConfig is the admin compatibility catalog; ordinary snapshots derive
// the same catalog from their already-intersected request scope instead.
func (r *channelMonitorV2Repository) GetDerivedConfig(ctx context.Context, cfg service.ChannelMonitorV2Config) (*service.ChannelMonitorV2Config, error) {
	groupIDs := cfg.GroupIDs
	// Config has no range parameter. The longest product window (30d) is the
	// conservative default for observed wildcard names across retained tiers.
	filter, err := service.ParseChannelMonitorV2FilterAt(time.Now(), "30d", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	ctx, filter, derived, err := r.resolveChannelMonitorV2Scope(ctx, filter, cfg)
	if err != nil {
		return nil, err
	}
	hasWildcard := false
	for _, info := range channelMonitorV2ScopeFromContext(ctx).groups {
		for _, model := range info.policy.Models {
			if strings.Contains(model, "*") {
				hasWildcard = true
				break
			}
		}
	}
	if hasWildcard {
		coverage, err := r.loadCoverage(ctx, filter)
		if err != nil {
			return nil, err
		}
		filter = channelMonitorV2CommonCoverageFilter(filter, *coverage)
		if filter.Start.Before(filter.End) {
			if err := r.expandChannelMonitorV2ObservedCatalog(ctx, filter, &derived); err != nil {
				return nil, err
			}
		}
	}
	derived.GroupIDs = groupIDs // preserve editable empty=all settings semantics
	return &derived, nil
}

func (r *channelMonitorV2Repository) expandChannelMonitorV2ObservedCatalog(ctx context.Context, filter service.ChannelMonitorV2Filter, cfg *service.ChannelMonitorV2Config) error {
	// Settings are an observation catalog, not a metric-window response. Read
	// one DISTINCT batch across every retained fixed rollup tier so a concrete
	// wildcard model is visible immediately in the 5m/1h rollup, even before
	// the 12h/1d trailing bucket is rebuilt. Include buckets overlapping the
	// coverage start: bootstrap may start after midnight, inside an existing
	// daily bucket. The one-day lookback caps the index scan to the largest
	// allowed bucket; the overlap predicate excludes buckets ending at/before
	// coverage. This reads catalog names only, never counts or raw logs.
	where, args := channelMonitorV2Where(filter, *cfg, "m")
	where = strings.Replace(where, "m.bucket_start >= $1",
		"m.bucket_start >= $1::timestamptz - INTERVAL '1 day' AND m.bucket_start + m.bucket_seconds * INTERVAL '1 second' > $1::timestamptz", 1)
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT m.platform,m.group_id,m.model FROM channel_monitor_v2_metrics_rollup m `+where+` AND m.bucket_seconds IN (300,3600,43200,86400) AND (m.success_requests + m.error_requests) > 0`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	observed := map[string][]string{}
	for rows.Next() {
		var platform, model string
		var groupID int64
		if err := rows.Scan(&platform, &groupID, &model); err != nil {
			return err
		}
		if !channelMonitorV2TupleSelected(ctx, filter, *cfg, platform, groupID, model) {
			continue
		}
		model = channelMonitorV2ScopedDisplayModel(ctx, *cfg, platform, groupID, model)
		observed[platform] = append(observed[platform], model)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	byPlatform := map[string][]string{}
	for _, p := range cfg.Platforms {
		byPlatform[p.Platform] = append([]string{}, p.Models...)
	}
	for platform, models := range observed {
		sort.Slice(models, func(i, j int) bool { return strings.ToLower(models[i]) < strings.ToLower(models[j]) })
		byPlatform[platform] = dedupeMonitorModels(append(byPlatform[platform], models...))
	}
	cfg.Platforms = []service.ChannelMonitorV2PlatformConfig{}
	for platform, models := range byPlatform {
		cfg.Platforms = append(cfg.Platforms, service.ChannelMonitorV2PlatformConfig{Platform: platform, Enabled: true, Models: models})
	}
	sort.Slice(cfg.Platforms, func(i, j int) bool { return cfg.Platforms[i].Platform < cfg.Platforms[j].Platform })
	return nil
}

func channelMonitorV2TupleSelected(ctx context.Context, filter service.ChannelMonitorV2Filter, cfg service.ChannelMonitorV2Config, platform string, groupID int64, model string) bool {
	scope := channelMonitorV2ScopeFromContext(ctx)
	if scope == nil {
		return false
	}
	info, ok := scope.groups[groupID]
	if !ok || strings.TrimSpace(model) == "" || strings.Contains(model, "*") || !info.policy.Allows(model) {
		return false
	}
	if len(filter.Platforms) > 0 && !containsString(filter.Platforms, platform) {
		return false
	}
	return monitorModelFilterMatches(filter.Models, channelMonitorV2ScopedDisplayModel(ctx, cfg, platform, groupID, model))
}

func channelMonitorV2ScopedDisplayModel(ctx context.Context, cfg service.ChannelMonitorV2Config, platform string, groupID int64, model string) string {
	scope := channelMonitorV2ScopeFromContext(ctx)
	if scope != nil {
		if info, ok := scope.groups[groupID]; ok {
			// An explicitly configured public ID owns its traffic, even when an
			// earlier entry (or another group's entry) accepts it as an alias.
			for _, entry := range info.policy.Models {
				if !strings.Contains(entry, "*") && strings.EqualFold(entry, strings.TrimSpace(model)) {
					return channelMonitorV2DisplayModel(cfg, platform, entry)
				}
			}
			for _, entry := range info.policy.Models {
				if !strings.Contains(entry, "*") && (service.GroupModelAllowlist{Enabled: true, Models: []string{entry}}).Allows(model) {
					return channelMonitorV2DisplayModel(cfg, platform, entry)
				}
			}
		}
	}
	return channelMonitorV2DisplayModel(cfg, platform, model)
}

func channelMonitorV2LocalModelPlatform(model string, routes []channelMonitorV2LocalRoute) string {
	best := -1
	for i, route := range routes {
		match := strings.EqualFold(route.PublicModel, model)
		if route.MatchType == "prefix" {
			match = strings.HasPrefix(strings.ToLower(model), strings.ToLower(route.PublicModel))
		}
		if !match {
			continue
		}
		if best < 0 || (route.MatchType == "exact" && routes[best].MatchType != "exact") ||
			(route.MatchType == routes[best].MatchType && len(route.PublicModel) > len(routes[best].PublicModel)) {
			best = i
		}
	}
	if best >= 0 {
		return strings.ToLower(strings.TrimSpace(routes[best].TargetPlatform))
	}
	return service.PlatformComposite
}

func monitorModelFilterMatches(selected []string, model string) bool {
	if len(selected) == 0 {
		return true
	}
	for _, v := range selected {
		if strings.EqualFold(v, model) {
			return true
		}
	}
	return false
}

func containsInt64Monitor(values []int64, target int64) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func dedupeMonitorModels(models []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		key := strings.ToLower(model)
		if model != "" && !seen[key] {
			seen[key] = true
			out = append(out, model)
		}
	}
	return out
}
