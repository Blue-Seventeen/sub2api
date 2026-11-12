package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsAttemptOutcomeDoesNotCallWire200ModelSuccess(t *testing.T) {
	for _, tt := range []struct {
		name                string
		status              int
		canceled, exhausted bool
		want                string
	}{
		{"wire_200_only", 200, false, false, "response_2xx"},
		{"failed_after_failover", 502, false, true, "failed"},
		{"client_canceled", 200, true, false, "canceled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			if tt.canceled {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			c.Status(tt.status)
			c.Set("ops_candidates_exhausted", tt.exhausted)
			original := &service.OpsUpstreamErrorEvent{AccountID: 7, UpstreamStatusCode: 401, Kind: "failover"}
			c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{original})
			entry := &service.OpsInsertErrorLogInput{StatusCode: tt.status}
			applyOpsUpstreamFieldsFromContext(c, entry)
			require.NoError(t, service.SanitizeOpsUpstreamErrorsForQueue(entry))
			var events []map[string]any
			require.NotNil(t, entry.UpstreamErrorsJSON)
			require.NoError(t, json.Unmarshal([]byte(*entry.UpstreamErrorsJSON), &events))
			require.Equal(t, tt.want, events[0]["request_outcome"])
			if tt.exhausted {
				require.Equal(t, true, events[0]["candidates_exhausted"])
			} else {
				require.Empty(t, events[0]["candidates_exhausted"])
			}
			raw, _ := json.Marshal(original)
			require.NotContains(t, string(raw), "request_outcome", "final annotation must not mutate the attempt snapshot")
		})
	}
}

func TestOpsAttemptSelectionDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"available", nil, false},
		{"exhausted", fmt.Errorf("selection: %w", service.ErrNoAvailableAccounts), true},
		{"compact_exhausted", service.ErrNoAvailableCompactAccounts, true},
		{"infrastructure", errors.New("redis unavailable"), false},
		{"canceled", context.Canceled, false},
		{"retry_pin_unavailable", service.ErrSameAccountRetryUnavailable, false},
		{"untyped_text_is_not_evidence", errors.New("no available accounts: infrastructure failed"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			markOpsCandidatesExhaustedFromSelection(c, tt.err)
			require.Equal(t, tt.want, c.GetBool(service.OpsCandidatesExhaustedKey))
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	setOpsSelectedAccount(c, 7, service.PlatformOpenAI)
	setOpsSelectedAccount(c, 7, service.PlatformOpenAI)
	setOpsSelectedAccount(c, 8, service.PlatformOpenAI)
	c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{AccountID: 7, UpstreamStatusCode: 401}})
	entry := &service.OpsInsertErrorLogInput{StatusCode: 200}
	applyOpsUpstreamFieldsFromContext(c, entry)
	raw, _ := json.Marshal(entry.UpstreamErrors)
	var events []map[string]any
	require.NoError(t, json.Unmarshal(raw, &events))
	require.Equal(t, float64(8), events[0]["selected_account_id"])
	require.Equal(t, float64(1), events[0]["account_switch_count"])
}
