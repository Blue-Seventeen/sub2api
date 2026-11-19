package service

import "github.com/tidwall/gjson"

// parseSSEUsagePassthrough accumulates Anthropic SSE usage snapshots into usage.
//
// message_start is a full snapshot (missing canonical counts reset), message_delta
// carries independently authoritative counts (explicit zeros keep previously known
// values because providers reuse the field for other meanings), and every other
// event may only contribute cache details — it never carries cross-event canonical
// counts.
func parseSSEUsagePassthrough(data string, usage *ClaudeUsage) {
	if usage == nil || data == "" || data == "[DONE]" {
		return
	}

	parsed := gjson.Parse(data)
	eventType := parsed.Get("type").String()
	switch eventType {
	case "message_start":
		msgUsage := parsed.Get("message.usage")
		if msgUsage.Exists() {
			usage.InputTokens = int(msgUsage.Get("input_tokens").Int())
			usage.CacheReadInputTokens = int(msgUsage.Get("cache_read_input_tokens").Int())

			// 起始快照整体替换缓存写入口径：聚合未声明时先置为未知，再由 TTL 明细推导；
			// 任一 5m/1h 明细出现即替换两档（含显式 0）。
			usage.CacheCreationInputTokens = 0
			usage.cacheCreationInputTokensPresent = false
			usage.cacheCreationInputTokensFromDetails = false
			cc5m := msgUsage.Get("cache_creation.ephemeral_5m_input_tokens")
			cc1h := msgUsage.Get("cache_creation.ephemeral_1h_input_tokens")
			if cc5m.Exists() || cc1h.Exists() {
				usage.CacheCreation5mTokens = max(int(cc5m.Int()), 0)
				usage.CacheCreation1hTokens = max(int(cc1h.Int()), 0)
			}
			// 与 typed 口径共用同一合并规则：保留"聚合是否显式声明"（presence）以及
			// "聚合是否由明细推导而来"两个标记，供下游计费区分显式 0 与缺失。
			mergeAnthropicCacheCreationSnapshot(msgUsage, usage)
		}
	case "message_delta":
		deltaUsage := parsed.Get("usage")
		if deltaUsage.Exists() && deltaUsage.Type != gjson.Null {
			if v := deltaUsage.Get("input_tokens").Int(); v > 0 {
				usage.InputTokens = int(v)
			}
			if v := deltaUsage.Get("output_tokens").Int(); v > 0 {
				usage.OutputTokens = int(v)
			}
			if v := deltaUsage.Get("cache_read_input_tokens").Int(); v > 0 {
				usage.CacheReadInputTokens = int(v)
			}
			// Delta 明细各自权威（含显式 0）；聚合仅在其自身声明时更新，
			// 已由明细推导的聚合继续跟随 TTL 明细。
			mergeAnthropicCacheCreationSnapshot(deltaUsage, usage)
		}
	default:
		// 非快照事件只贡献缓存写入口径（聚合 + TTL 明细），它们的
		// input/output/cache_read 一律忽略，避免中途事件覆盖快照累计值。
		// 聚合与官方同口径：只接受显式正数；目标桶本来就空时才接受 0。
		if nested := parsed.Get("usage"); nested.IsObject() {
			mergeAnthropicCacheCreationSnapshot(nested, usage)
		} else if nested := parsed.Get("message.usage"); nested.IsObject() {
			mergeAnthropicCacheCreationSnapshot(nested, usage)
		}
		mergeAnthropicCacheCreationDetailsOutsideSnapshots(parsed, usage)
	}

	if usage.CacheReadInputTokens == 0 {
		if cached := parsed.Get("message.usage.cached_tokens").Int(); cached > 0 {
			usage.CacheReadInputTokens = int(cached)
		}
		if cached := parsed.Get("usage.cached_tokens").Int(); usage.CacheReadInputTokens == 0 && cached > 0 {
			usage.CacheReadInputTokens = int(cached)
		}
	}
	if usage.CacheCreationInputTokens == 0 {
		cc5m := parsed.Get("message.usage.cache_creation.ephemeral_5m_input_tokens").Int()
		cc1h := parsed.Get("message.usage.cache_creation.ephemeral_1h_input_tokens").Int()
		if cc5m == 0 && cc1h == 0 {
			cc5m = parsed.Get("usage.cache_creation.ephemeral_5m_input_tokens").Int()
			cc1h = parsed.Get("usage.cache_creation.ephemeral_1h_input_tokens").Int()
		}
		if total := cc5m + cc1h; total > 0 {
			usage.CacheCreationInputTokens = int(total)
			usage.cacheCreationInputTokensFromDetails = true
		}
	}

	// Kimi reports total input at start but uncached input in deltas. Its stable
	// prompt total keeps ClaudeUsage buckets disjoint without subtracting twice.
	// 只有快照事件（message_start / message_delta）允许重新推导非缓存输入桶；
	// 其余事件的权威计数一律忽略，避免覆盖已累计的快照口径。
	if eventType == "message_start" || eventType == "message_delta" {
		usageNode := parsed.Get("usage")
		if eventType == "message_start" {
			usageNode = parsed.Get("message.usage")
		}
		if !usageNode.Exists() || usageNode.Type == gjson.Null {
			usageNode = parsed.Get("message.usage")
		}
		normalizeAnthropicCompatiblePromptBuckets(usageNode, usage)
	}
}

// mergeAnthropicCacheCreationSnapshot 把一条 Anthropic 快照事件（message_start /
// message_delta）里的 cache_creation 写入 usage。
//
// 口径与 typed 合并（apicompat.AnthropicUsage + mergeAnthropicCacheCreationUsage）
// 一致：显式声明的聚合最权威（显式 0 不会清掉既有正值），聚合未声明时由 5m/1h
// 明细推导，推导出的聚合继续跟随明细。唯一差异是推导条件不参考 presence：本
// 解析器允许调用方传入已被其他路径填充的 usage（例如 API-Key 直通流复用同一
// 结构），此时"未知来源的正是正数聚合"不能被当作未声明而清零。
func mergeAnthropicCacheCreationSnapshot(node gjson.Result, usage *ClaudeUsage) {
	if usage == nil || !node.IsObject() {
		return
	}
	aggregate, aggregatePresent := anthropicUsageInt(node.Get("cache_creation_input_tokens"))
	if aggregatePresent && (aggregate > 0 || usage.CacheCreationInputTokens <= 0) {
		usage.CacheCreationInputTokens = max(aggregate, 0)
		usage.cacheCreationInputTokensPresent = true
		usage.cacheCreationInputTokensFromDetails = false
	}
	details := node.Get("cache_creation")
	if !details.IsObject() {
		return
	}
	updated := false
	if tokens, ok := anthropicUsageInt(details.Get("ephemeral_5m_input_tokens")); ok {
		usage.CacheCreation5mTokens = max(tokens, 0)
		updated = true
	}
	if tokens, ok := anthropicUsageInt(details.Get("ephemeral_1h_input_tokens")); ok {
		usage.CacheCreation1hTokens = max(tokens, 0)
		updated = true
	}
	if updated && (usage.cacheCreationInputTokensFromDetails || usage.CacheCreationInputTokens <= 0) {
		usage.CacheCreationInputTokens = usage.CacheCreation5mTokens + usage.CacheCreation1hTokens
		usage.cacheCreationInputTokensFromDetails = true
	}
}

// mergeAnthropicCacheCreationDetailsOutsideSnapshots applies cache_creation TTL
// details reported by events other than message_start/message_delta. Providers
// may split a cumulative snapshot over several such events ("5m: 2" then
// "1h: 1"), so the buckets are merged and an aggregate that the provider never
// stated explicitly keeps following them.
func mergeAnthropicCacheCreationDetailsOutsideSnapshots(parsed gjson.Result, usage *ClaudeUsage) {
	node := parsed.Get("usage")
	if nested := parsed.Get("message.usage"); nested.IsObject() && nested.Get("cache_creation").IsObject() {
		node = nested
	}
	if !node.IsObject() {
		return
	}
	details := node.Get("cache_creation")
	if !details.IsObject() {
		return
	}
	updated := false
	if tokens := details.Get("ephemeral_5m_input_tokens"); tokens.Exists() && tokens.Type != gjson.Null {
		usage.CacheCreation5mTokens = max(int(tokens.Int()), 0)
		updated = true
	}
	if tokens := details.Get("ephemeral_1h_input_tokens"); tokens.Exists() && tokens.Type != gjson.Null {
		usage.CacheCreation1hTokens = max(int(tokens.Int()), 0)
		updated = true
	}
	if !updated {
		return
	}
	// An aggregate the provider reported explicitly stays authoritative; only a
	// derived (or still unknown) aggregate follows the TTL buckets.
	if usage.CacheCreationInputTokens <= 0 || usage.cacheCreationInputTokensFromDetails {
		usage.CacheCreationInputTokens = usage.CacheCreation5mTokens + usage.CacheCreation1hTokens
		usage.cacheCreationInputTokensFromDetails = true
	}
}

// normalizeAnthropicCompatiblePromptBuckets converts provider prompt/cache aliases
// into Claude's mutually exclusive buckets, leaving native Anthropic usage alone.
//
// 口径与 typed 路径（gateway_forward_as_responses.go 的 normalizeAnthropicCompatiblePromptUsage
// → normalizeAnthropicPromptTotal）一致：最近一次的权威 prompt 总量会被记在
// ClaudeUsage 上，后续事件里 cache_read / cache_creation 增长时据此重算非缓存输入桶。
// 只保存"权威总量"这一项状态，绝不从缓存别名反推总量：没有任何权威总量可用的
// 增量事件不会改写 InputTokens（旧实现用缺失的 prompt_tokens=0 参与减法，会把
// cache 命中的事件误判成整单缓存而把输入桶清零）。缺失与显式 null 等价。
func normalizeAnthropicCompatiblePromptBuckets(usageNode gjson.Result, usage *ClaudeUsage) bool {
	if usage == nil || !usageNode.Exists() || usageNode.Type == gjson.Null {
		return false
	}
	promptTokens, hasPromptTokens := anthropicUsageInt(usageNode.Get("prompt_tokens"))
	_, hasPromptCacheHit := anthropicUsageInt(usageNode.Get("prompt_cache_hit_tokens"))
	promptCacheMissTokens, hasPromptCacheMiss := anthropicUsageInt(usageNode.Get("prompt_cache_miss_tokens"))
	if (!hasPromptTokens || promptTokens <= 0) && !hasPromptCacheHit && !hasPromptCacheMiss && !usage.promptTotalPresent {
		return false
	}

	cacheReadTokens := usage.CacheReadInputTokens
	for _, path := range []string{"cache_read_input_tokens", "cached_tokens", "prompt_tokens_details.cached_tokens", "prompt_cache_hit_tokens"} {
		value, ok := anthropicUsageInt(usageNode.Get(path))
		if !ok {
			continue
		}
		cacheReadTokens = max(value, 0)
		if cacheReadTokens > 0 {
			break
		}
	}

	// 聚合/明细由 mergeAnthropicCacheCreationSnapshot 裁决（快照事件）与顶层 usage
	// 回填（非快照事件）共同维护。与官方 v0.2.23 的 typed 路径同口径：显式 0 是
	// 供应商的占位值，不代表"缓存被清空"，只有显式正数才覆盖已有桶。
	cacheCreationTokens := usage.CacheCreationInputTokens
	if value, ok := anthropicUsageInt(usageNode.Get("cache_creation_input_tokens")); ok && value > 0 {
		cacheCreationTokens = max(value, 0)
	}
	if cacheCreationTokens == 0 {
		cc5m, _ := anthropicUsageInt(usageNode.Get("cache_creation.ephemeral_5m_input_tokens"))
		cc1h, _ := anthropicUsageInt(usageNode.Get("cache_creation.ephemeral_1h_input_tokens"))
		if cc5m > 0 || cc1h > 0 {
			cacheCreationTokens = cc5m + cc1h
		}
	}

	// 先落盘缓存桶（含"显式 0 是否清零"的裁决），再用**落盘后**的桶重算输入，
	// 否则本事件缺失的别名会被当作 0 参与减法。
	// 显式 0 落库的唯一条件：目标桶本来就是空/负（没有历史正值可保留）。
	if cacheReadTokens > 0 || usage.CacheReadInputTokens <= 0 {
		usage.CacheReadInputTokens = cacheReadTokens
	}
	if cacheCreationTokens > 0 || usage.CacheCreationInputTokens <= 0 {
		usage.CacheCreationInputTokens = cacheCreationTokens
	}

	// 与 typed 的 normalizeAnthropicPromptTotal 同口径：保存权威总量 / 由 miss 桶
	// 覆盖，然后按最新的缓存桶重算非缓存输入桶。
	if hasPromptCacheMiss {
		usage.promptTotalPresent = false
		usage.InputTokens = max(promptCacheMissTokens, 0)
	} else {
		if hasPromptTokens && promptTokens > 0 {
			usage.promptTotalTokens = promptTokens
			usage.promptTotalPresent = true
		}
		if usage.promptTotalPresent {
			usage.InputTokens = max(usage.promptTotalTokens-usage.CacheReadInputTokens-usage.CacheCreationInputTokens, 0)
		}
	}
	return true
}

// anthropicUsageInt 读取 usage 字段的整数值；字段缺失或显式 null 都视为"未提供"，
// 与 typed 口径（指针判空）保持一致。
func anthropicUsageInt(node gjson.Result) (int, bool) {
	if !node.Exists() || node.Type == gjson.Null {
		return 0, false
	}
	return int(node.Int()), true
}
