//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokBoundLookupAdmissionCache struct {
	*grokMediaSlotsCache
	waitCalls int
}

func (c *grokBoundLookupAdmissionCache) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWaiting int) (bool, error) {
	c.waitCalls++
	return c.grokMediaSlotsCache.IncrementAccountWaitCount(ctx, accountID, maxWaiting)
}

func TestGrokBoundVideoLookupBusyStopsWithoutRepeatingOwnerAdmission(t *testing.T) {
	for _, queueFull := range []bool{false, true} {
		name := "wait_timeout"
		if queueFull {
			name = "queue_full"
		}
		t.Run(name, func(t *testing.T) {
			h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
			h.maxAccountSwitches = 3
			slots.full, slots.queueFull = true, queueFull
			admission := &grokBoundLookupAdmissionCache{grokMediaSlotsCache: slots}
			h.concurrencyHelper.concurrencyService = service.NewConcurrencyService(admission)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c, recorder := grokMediaSlotContext(ctx, false)

			h.GrokVideoStatus(c)

			require.NoError(t, ctx.Err(), "bound-owner rejection must finish without waiting for client cancellation")
			require.Equal(t, http.StatusTooManyRequests, recorder.Code, recorder.Body.String())
			require.Equal(t, 1, admission.waitCalls, "an unavailable task owner must not be selected and admitted repeatedly")
			require.Zero(t, upstream.calls)
			require.Zero(t, bindings.writes, "lookup rejection must preserve the authenticated task owner and its TTL")
			slots.assertReleased(t)
		})
	}
}

func TestGrokVoiceStopsAfterCommittedAdmissionFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Keep scheduler probes unavailable so the real handler owns the final
	// admission attempt. Scheduler probes are not response-committing failures.
	schedulerSlots := &poolTraversalSlotCache{}
	schedulerSlots.acquireAccountSlotFn = func(context.Context, int64, int, string) (bool, error) { return false, nil }
	upstreamRecorder := httptest.NewRecorder()
	upstream := &poolAvailabilityUpstream{goodID: 803, recorder: upstreamRecorder}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	h, _, router := newPoolTraversalHandler(t, newPoolTraversalAccounts(3), upstream, cfg, schedulerSlots)
	var admittedIDs []int64
	admissionSlots := &concurrencyCacheMock{acquireAccountSlotFn: func(_ context.Context, id int64, _ int, _ string) (bool, error) {
		admittedIDs = append(admittedIDs, id)
		if id == 802 {
			return false, errors.New("fixture admission cache unavailable")
		}
		return true, nil
	}}
	h.concurrencyHelper.concurrencyService = service.NewConcurrencyService(admissionSlots)
	router.POST("/v1/stt", func(c *gin.Context) { h.GrokVoice(c, "stt") })
	request := httptest.NewRequest(http.MethodPost, "/v1/stt", bytes.NewBufferString(`{"audio":"fixture"}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(upstreamRecorder, request)

	require.Equal(t, []int64{801, 802}, admittedIDs, "the committed infrastructure error must stop admission before C")
	require.Equal(t, []int64{801}, upstream.hits, "no upstream request or billing may follow B's committed admission error")
	require.Equal(t, http.StatusServiceUnavailable, upstreamRecorder.Code, upstreamRecorder.Body.String())
	require.Contains(t, upstreamRecorder.Body.String(), "Service temporarily unavailable")
	require.NotContains(t, upstreamRecorder.Body.String(), "healthy-voice")
}
