package openai_ws_v2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The WS passthrough adapter maps every TTL bucket into OpenAIUsage for billing,
// so a bucket that parses but never settles would silently under-bill cache
// writes. This pins the parse -> settle -> result chain for the OpenAI-style
// cache_write_tokens_details breakdown.
func TestRelay_CacheCreationTTLBucketsSurviveSettlement(t *testing.T) {
	t.Parallel()

	state := &relayState{}
	parsed := parseUsageAndAccumulate(
		state,
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":2402,"output_tokens":5,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":29,"cache_write_tokens_details":{"ttl_30m_tokens":9,"ttl_12h_tokens":20}}}}}`),
		"response.completed",
		nil,
	)
	require.Equal(t, 29, parsed.CacheCreationInputTokens)
	require.Equal(t, 9, parsed.CacheCreation30mTokens)
	require.Equal(t, 20, parsed.CacheCreation12hTokens)
	require.Zero(t, parsed.CacheCreation5mTokens, "OpenAI-style details never fill the Anthropic buckets")
	require.Zero(t, parsed.CacheCreation1hTokens)

	require.Equal(t, Usage{}, state.usage, "parsing does not settle the turn")
	require.Equal(t, parsed, finalizeRelayTurnUsage(state))
	require.Equal(t, 9, state.usage.CacheCreation30mTokens)
	require.Equal(t, 20, state.usage.CacheCreation12hTokens)

	result := &RelayResult{}
	enrichResult(result, state, 0)
	require.Equal(t, 9, result.Usage.CacheCreation30mTokens)
	require.Equal(t, 20, result.Usage.CacheCreation12hTokens)
}

// Non-terminal events merge into the turn usage; the TTL buckets must merge with
// them, otherwise a streamed usage delta would drop the split and only keep the
// aggregate.
func TestMergeRelayUsageNonZeroKeepsCacheCreationTTLBuckets(t *testing.T) {
	t.Parallel()

	dst := Usage{}
	mergeRelayUsageNonZero(&dst, Usage{
		CacheCreationInputTokens: 7,
		CacheCreation30mTokens:   3,
		CacheCreation12hTokens:   4,
	})
	require.Equal(t, 7, dst.CacheCreationInputTokens)
	require.Equal(t, 3, dst.CacheCreation30mTokens)
	require.Equal(t, 4, dst.CacheCreation12hTokens)

	// Zero buckets never clobber an already merged split.
	mergeRelayUsageNonZero(&dst, Usage{CacheCreationInputTokens: 1})
	require.Equal(t, 3, dst.CacheCreation30mTokens)
	require.Equal(t, 4, dst.CacheCreation12hTokens)
}
