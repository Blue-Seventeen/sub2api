package openai_ws_v2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The merge materialised this package from dev's test file, which predates
// upstream v0.2.23 and therefore lacks these ten cases. They exercise relay
// behaviour the merged implementation kept from upstream (pending bare-error
// settlement, per-turn response model/service tier, usage fallback rules), so
// they are restored here instead of being lost with the file-level conflict.
// If the whole upstream passthrough_relay_internal_test.go is ever adopted,
// delete this file: both define the same test names.

func TestObserveUpstreamMessageBareErrorBeforeNextCompletedKeepsBothTurns(t *testing.T) {
	t.Parallel()

	now := time.Unix(400, 0)
	state := &relayState{}
	parseUsageAndAccumulate(state, []byte(`{"type":"response.in_progress","usage":{"input_tokens":5,"output_tokens":1}}`), "response.in_progress", nil)
	bare := observeUpstreamMessage(
		state,
		[]byte(`{"type":"error","error":{"message":"first turn failed"}}`),
		now,
		func() time.Time { return now },
		nil,
	)
	require.False(t, bare.terminal)
	bare = finalizePendingBareError(state, now)
	require.True(t, bare.terminal)
	require.Equal(t, Usage{InputTokens: 5, OutputTokens: 1}, bare.usage)

	completed := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.completed","response":{"id":"resp_next","usage":{"input_tokens":3,"output_tokens":2}}}`),
		now,
		func() time.Time { return now },
		nil,
	)
	require.True(t, completed.terminal)
	require.Equal(t, Usage{InputTokens: 3, OutputTokens: 2}, completed.usage)
	require.Equal(t, Usage{InputTokens: 8, OutputTokens: 3}, state.usage)
}

func TestObserveUpstreamMessageBareErrorClearsTurnStateAndFinalizesUsageOnce(t *testing.T) {
	t.Parallel()

	now := time.Unix(200, 0)
	state := &relayState{}
	state.setPendingTurnStartedAt(now.Add(-2 * time.Second))
	timing := openAIWSRelayGetOrInitTurnTiming(state, "resp_active", now.Add(-time.Second))
	require.NotNil(t, timing)
	state.setPendingTurnStartedAt(now.Add(-500 * time.Millisecond))
	parseUsageAndAccumulate(state, []byte(`{"type":"response.in_progress","usage":{"input_tokens":9,"output_tokens":4}}`), "response.in_progress", nil)

	observed := observeUpstreamMessage(
		state,
		[]byte(`{"type":"error","usage":{"input_tokens":3,"output_tokens":1},"error":{"message":"failed"}}`),
		now.Add(-3*time.Second),
		func() time.Time { return now },
		nil,
	)

	require.False(t, observed.terminal, "bare error settlement is deferred in case response.failed follows")
	require.Equal(t, "error", observed.eventType)
	observed = finalizePendingBareError(state, now)
	require.True(t, observed.terminal)
	require.Equal(t, Usage{InputTokens: 3, OutputTokens: 1}, observed.usage)
	require.Equal(t, observed.usage, state.usage)
	require.Equal(t, Usage{}, state.turnUsage)
	require.Nil(t, state.activeTurn)
	require.Empty(t, state.turnTimingByID)
	// The error is bound to the active turn id, so settlement must not consume a
	// later pending start that belongs to the next turn.
	require.Equal(t, now.Add(-500*time.Millisecond), state.consumePendingTurnStartedAt())

	// Re-observing and settling a later terminal without usage must not re-add the prior turn.
	observeUpstreamMessage(state, []byte(`{"type":"error","error":{"message":"again"}}`), now, func() time.Time { return now }, nil)
	finalizePendingBareError(state, now)
	require.Equal(t, Usage{InputTokens: 3, OutputTokens: 1}, state.usage)
}

func TestObserveUpstreamMessageErrorThenFailedSettlesUsageOnce(t *testing.T) {
	t.Parallel()

	now := time.Unix(300, 0)
	state := &relayState{}
	state.setPendingTurnStartedAt(now.Add(-time.Second))
	openAIWSRelayGetOrInitTurnTiming(state, "resp_1", now)
	parseUsageAndAccumulate(state, []byte(`{"type":"response.in_progress","usage":{"input_tokens":9,"output_tokens":4}}`), "response.in_progress", nil)

	errorObserved := observeUpstreamMessage(
		state,
		[]byte(`{"type":"error","error":{"code":"server_is_overloaded","message":"overloaded"}}`),
		now,
		func() time.Time { return now },
		nil,
	)
	require.False(t, errorObserved.terminal)
	require.NotNil(t, state.pendingBareError)
	require.Equal(t, Usage{}, state.usage)

	failedObserved := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.failed","response":{"id":"resp_1","usage":{"input_tokens":7,"output_tokens":2}}}`),
		now,
		func() time.Time { return now },
		nil,
	)
	require.True(t, failedObserved.terminal)
	require.Equal(t, Usage{InputTokens: 7, OutputTokens: 2}, failedObserved.usage)
	require.Equal(t, failedObserved.usage, state.usage)
	require.Nil(t, state.pendingBareError)
}

func TestObserveUpstreamMessage_ResponseModelIsTurnLocalAndTerminalWins(t *testing.T) {
	t.Parallel()

	state := &relayState{requestModel: "gpt-5.6-sol"}
	startAt := time.Unix(0, 0)
	now := startAt
	nowFn := func() time.Time {
		now = now.Add(5 * time.Millisecond)
		return now
	}

	created := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.5"}}`),
		startAt,
		nowFn,
		nil,
	)
	require.False(t, created.terminal)

	completed := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":2}}}`),
		startAt,
		nowFn,
		nil,
	)
	require.True(t, completed.terminal)
	require.Equal(t, "gpt-5.4", completed.responseModel)
	require.True(t, completed.responseConflict)

	var firstTurn RelayTurnResult
	emitTurnComplete(func(turn RelayTurnResult) { firstTurn = turn }, state, completed)
	require.Equal(t, "gpt-5.4", firstTurn.ResponseModel)
	require.True(t, firstTurn.ResponseModelConflict)

	observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.created","response":{"id":"resp_2","model":"gpt-5.3"}}`),
		startAt,
		nowFn,
		nil,
	)
	second := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.completed","response":{"id":"resp_2","model":"GPT-5.3","usage":{"input_tokens":3,"output_tokens":4}}}`),
		startAt,
		nowFn,
		nil,
	)
	require.Equal(t, "GPT-5.3", second.responseModel)
	require.False(t, second.responseConflict, "the previous turn must not contaminate this turn")
}

func TestObserveUpstreamMessage_ResponseServiceTierOnlyFromTerminalEvents(t *testing.T) {
	t.Parallel()

	state := &relayState{requestModel: "gpt-5.6-sol"}
	startAt := time.Unix(0, 0)
	now := startAt
	nowFn := func() time.Time {
		now = now.Add(5 * time.Millisecond)
		return now
	}

	created := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.6-sol","service_tier":"priority"}}`),
		startAt,
		nowFn,
		nil,
	)
	require.False(t, created.terminal)
	require.Equal(t, "", created.responseServiceTier)

	completed := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","service_tier":"default","usage":{"input_tokens":1,"output_tokens":2}}}`),
		startAt,
		nowFn,
		nil,
	)
	require.True(t, completed.terminal)
	require.Equal(t, "default", completed.responseServiceTier, "the echoed priority tier must lose to the terminal declaration")

	var turn RelayTurnResult
	emitTurnComplete(func(result RelayTurnResult) { turn = result }, state, completed)
	require.Equal(t, "default", turn.ResponseServiceTier)

	var result RelayResult
	enrichResult(&result, state, now.Sub(startAt))
	require.Equal(t, "default", result.ResponseServiceTier)

	// A later turn without any declaration must not inherit the previous one.
	observeUpstreamMessage(state, []byte(`{"type":"response.created","response":{"id":"resp_2","model":"gpt-5.6-sol"}}`), startAt, nowFn, nil)
	second := observeUpstreamMessage(
		state,
		[]byte(`{"type":"response.completed","response":{"id":"resp_2","model":"gpt-5.6-sol","usage":{"input_tokens":3,"output_tokens":4}}}`),
		startAt,
		nowFn,
		nil,
	)
	require.Equal(t, "", second.responseServiceTier)
}

func TestParseUsageAndAccumulateIncludesIndependentReasoningTokens(t *testing.T) {
	t.Parallel()

	state := &relayState{}
	got := parseUsageAndAccumulate(
		state,
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":32,"output_tokens":9,"total_tokens":151,"output_tokens_details":{"reasoning_tokens":110}}}}`),
		"response.completed",
		nil,
	)
	require.Equal(t, 32, got.InputTokens)
	require.Equal(t, 119, got.OutputTokens)

	state = &relayState{}
	got = parseUsageAndAccumulate(
		state,
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":32,"output_tokens":119,"total_tokens":151,"output_tokens_details":{"reasoning_tokens":110}}}}`),
		"response.completed",
		nil,
	)
	require.Equal(t, 119, got.OutputTokens, "inclusive Responses output must not double-count reasoning")
}

func TestRelayUsageTerminalNonZeroReplacesFallbackAsWhole(t *testing.T) {
	t.Parallel()

	state := &relayState{}
	parseUsageAndAccumulate(state, []byte(`{"type":"response.in_progress","usage":{"input_tokens":9,"output_tokens":4,"input_tokens_details":{"cached_tokens":2}}}`), "response.in_progress", nil)
	parseUsageAndAccumulate(state, []byte(`{"type":"response.completed","usage":{"input_tokens":3,"output_tokens":0,"input_tokens_details":{"cached_tokens":0}}}`), "response.completed", nil)

	require.Equal(t, Usage{InputTokens: 3}, finalizeRelayTurnUsage(state))
	require.Equal(t, Usage{InputTokens: 3}, state.usage)
}

func TestRelayUsageTerminalWithoutUsageKeepsFallback(t *testing.T) {
	t.Parallel()

	state := &relayState{}
	parseUsageAndAccumulate(state, []byte(`{"type":"response.in_progress","usage":{"input_tokens":9,"output_tokens":4}}`), "response.in_progress", nil)
	parseUsageAndAccumulate(state, []byte(`{"type":"response.completed","response":{"id":"resp_1"}}`), "response.completed", nil)

	turnUsage := finalizeRelayTurnUsage(state)
	require.Equal(t, Usage{InputTokens: 9, OutputTokens: 4}, turnUsage)
	require.Equal(t, turnUsage, state.usage)
}

func TestRelayUsageTerminalZeroKeepsFallback(t *testing.T) {
	t.Parallel()

	state := &relayState{}
	parseUsageAndAccumulate(state, []byte(`{"type":"response.in_progress","usage":{"input_tokens":9,"output_tokens":4}}`), "response.in_progress", nil)
	parseUsageAndAccumulate(state, []byte(`{"type":"response.completed","usage":{"input_tokens":0,"output_tokens":0}}`), "response.completed", nil)

	require.Equal(t, Usage{InputTokens: 9, OutputTokens: 4}, finalizeRelayTurnUsage(state))
	require.Equal(t, Usage{InputTokens: 9, OutputTokens: 4}, state.usage)
}

func TestTerminalAndTokenEventSetsAreDisjoint(t *testing.T) {
	t.Parallel()

	for _, eventType := range []string{
		"response.completed",
		"response.done",
		"response.failed",
		"response.incomplete",
		"response.cancelled",
		"response.canceled",
	} {
		require.True(t, isTerminalEvent(eventType), eventType)
		require.False(t, isTokenEvent(eventType), eventType)
	}
}
