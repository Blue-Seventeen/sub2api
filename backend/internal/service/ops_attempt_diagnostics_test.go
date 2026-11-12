package service

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsAttemptDiagnosticsClassifiesOnlyStructuredAuthEvidence(t *testing.T) {
	cases := []struct {
		name, body, want string
		status           int
	}{
		{"revoked", ` {"error":{"code":"token_revoked"}}`, "credential_revoked", 401},
		{"expired", ` {"error":{"code":"token_expired"}}`, "credential_expired", 401},
		{"unknown", ` {"error":{"message":"Unauthorized"}}`, "authentication_unspecified", 401},
		{"echo_is_not_evidence", ` {"error":{"message":"input mentioned token_revoked and expired"}}`, "authentication_unspecified", 401},
		{"typed_provider", ` {"error":{"type":"authentication_error"}}`, "provider_auth", 403},
		{"not_auth", ` {"error":{"code":"rate_limit_exceeded"}}`, "", 429},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 7, UpstreamStatusCode: tt.status, Detail: tt.body, Kind: "failover"})
			raw, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			attemptEvents, ok := raw.([]*OpsUpstreamErrorEvent)
			require.True(t, ok)
			encoded := marshalOpsUpstreamErrors(attemptEvents)
			require.NotNil(t, encoded)
			var events []map[string]any
			require.NoError(t, json.Unmarshal([]byte(*encoded), &events))
			if tt.want == "" {
				require.Empty(t, events[0]["auth_category"])
			} else {
				require.Equal(t, tt.want, events[0]["auth_category"])
			}
			require.Equal(t, float64(1), events[0]["attempt_index"])
			require.Equal(t, false, events[0]["response_committed"])
		})
	}
}

func TestOpsAttemptDiagnosticsCapturesOrdinalAndCommitState(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 1, UpstreamStatusCode: 401, Kind: "failover"})
	MarkResponseCommitted(c)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{AccountID: 2, UpstreamStatusCode: 503, Kind: "retry_exhausted"})
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	attemptEvents, ok := raw.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	encoded := marshalOpsUpstreamErrors(attemptEvents)
	require.NotNil(t, encoded)
	var events []map[string]any
	require.NoError(t, json.Unmarshal([]byte(*encoded), &events))
	require.Equal(t, float64(1), events[0]["attempt_index"])
	require.Equal(t, float64(2), events[1]["attempt_index"])
	require.Equal(t, false, events[0]["response_committed"])
	require.Equal(t, true, events[1]["response_committed"])
}

func TestOpsAttemptDiagnosticsQueueRejectsUnboundedScalarValues(t *testing.T) {
	entry := &OpsInsertErrorLogInput{UpstreamErrors: []*OpsUpstreamErrorEvent{{
		UpstreamStatusCode: 401, AuthCategory: "Bearer sensitive-auth-value", RequestOutcome: "api_key=sensitive-outcome-value", AttemptIndex: -1,
	}}}
	require.NoError(t, SanitizeOpsUpstreamErrorsForQueue(entry))
	require.NotNil(t, entry.UpstreamErrorsJSON)
	require.NotContains(t, *entry.UpstreamErrorsJSON, "sensitive-")
	require.NotContains(t, *entry.UpstreamErrorsJSON, `"attempt_index":-1`)
}
