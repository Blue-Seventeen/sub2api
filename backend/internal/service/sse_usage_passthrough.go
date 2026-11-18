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
	switch parsed.Get("type").String() {
	case "message_start":
		msgUsage := parsed.Get("message.usage")
		if msgUsage.Exists() {
			usage.InputTokens = int(msgUsage.Get("input_tokens").Int())
			usage.CacheCreationInputTokens = int(msgUsage.Get("cache_creation_input_tokens").Int())
			usage.cacheCreationInputTokensFromDetails = false
			usage.CacheReadInputTokens = int(msgUsage.Get("cache_read_input_tokens").Int())

			// Start events replace both TTL buckets when either detail is present.
			cc5m := msgUsage.Get("cache_creation.ephemeral_5m_input_tokens")
			cc1h := msgUsage.Get("cache_creation.ephemeral_1h_input_tokens")
			if cc5m.Exists() || cc1h.Exists() {
				usage.CacheCreation5mTokens = int(cc5m.Int())
				usage.CacheCreation1hTokens = int(cc1h.Int())
			}
		}
	case "message_delta":
		deltaUsage := parsed.Get("usage")
		if deltaUsage.Exists() {
			if v := deltaUsage.Get("input_tokens").Int(); v > 0 {
				usage.InputTokens = int(v)
			}
			if v := deltaUsage.Get("output_tokens").Int(); v > 0 {
				usage.OutputTokens = int(v)
			}
			if v := deltaUsage.Get("cache_creation_input_tokens").Int(); v > 0 {
				usage.CacheCreationInputTokens = int(v)
				usage.cacheCreationInputTokensFromDetails = false
			}
			if v := deltaUsage.Get("cache_read_input_tokens").Int(); v > 0 {
				usage.CacheReadInputTokens = int(v)
			}

			// Delta details are independently authoritative, including explicit zero.
			cc5m := deltaUsage.Get("cache_creation.ephemeral_5m_input_tokens")
			cc1h := deltaUsage.Get("cache_creation.ephemeral_1h_input_tokens")
			if cc5m.Exists() {
				usage.CacheCreation5mTokens = int(cc5m.Int())
			}
			if cc1h.Exists() {
				usage.CacheCreation1hTokens = int(cc1h.Int())
			}
		}
	default:
		// Other events only contribute cache-write TTL details; their canonical
		// input/output/cache counters are ignored so a non-terminal event cannot
		// overwrite counts accumulated from start/delta snapshots.
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
		}
	}

	// Kimi reports total input at start but uncached input in deltas. Its stable
	// prompt total keeps ClaudeUsage buckets disjoint without subtracting twice.
	usageNode := parsed.Get("usage")
	if parsed.Get("type").String() == "message_start" {
		usageNode = parsed.Get("message.usage")
	}
	normalizeAnthropicCompatiblePromptBuckets(usageNode, usage)
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
// It is intentionally stateless: it never stores the prompt total on usage, so the
// caller can compare accumulated usage snapshots field by field. Providers that
// keep repeating prompt_tokens on every event still normalize identically.
func normalizeAnthropicCompatiblePromptBuckets(usageNode gjson.Result, usage *ClaudeUsage) bool {
	if usage == nil || !usageNode.Exists() {
		return false
	}
	promptTokens := usageNode.Get("prompt_tokens")
	promptCacheHitTokens := usageNode.Get("prompt_cache_hit_tokens")
	promptCacheMissTokens := usageNode.Get("prompt_cache_miss_tokens")
	if (!promptTokens.Exists() || promptTokens.Int() <= 0) &&
		!promptCacheHitTokens.Exists() && !promptCacheMissTokens.Exists() {
		return false
	}

	cacheReadTokens := usage.CacheReadInputTokens
	if v := usageNode.Get("cache_read_input_tokens"); v.Exists() {
		cacheReadTokens = int(v.Int())
	}
	if cacheReadTokens == 0 {
		if v := usageNode.Get("cached_tokens"); v.Exists() {
			cacheReadTokens = int(v.Int())
		}
	}
	if cacheReadTokens == 0 {
		if v := usageNode.Get("prompt_tokens_details.cached_tokens"); v.Exists() {
			cacheReadTokens = int(v.Int())
		}
	}
	if cacheReadTokens == 0 && promptCacheHitTokens.Exists() {
		cacheReadTokens = max(int(promptCacheHitTokens.Int()), 0)
	}

	cacheCreationTokens := usage.CacheCreationInputTokens
	if v := usageNode.Get("cache_creation_input_tokens"); v.Exists() {
		cacheCreationTokens = int(v.Int())
	}
	if cacheCreationTokens == 0 {
		cc5m := usageNode.Get("cache_creation.ephemeral_5m_input_tokens").Int()
		cc1h := usageNode.Get("cache_creation.ephemeral_1h_input_tokens").Int()
		if cc5m > 0 || cc1h > 0 {
			cacheCreationTokens = int(cc5m + cc1h)
		}
	}

	if promptCacheMissTokens.Exists() {
		usage.InputTokens = max(int(promptCacheMissTokens.Int()), 0)
	} else {
		usage.InputTokens = max(int(promptTokens.Int())-cacheReadTokens-cacheCreationTokens, 0)
	}
	usage.CacheReadInputTokens = cacheReadTokens
	usage.CacheCreationInputTokens = cacheCreationTokens
	return true
}
