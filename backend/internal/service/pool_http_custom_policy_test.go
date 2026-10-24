//go:build unit

package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// Compatible/NewAPI must not silently override the custom error-code gate
// that native HTTP forwarding applies before configured pool retries.
func TestPoolHTTPExcludedCustomStatusDoesNotRetry(t *testing.T) {
	for _, status := range []int{403, 429, 500, 502, 503, 504} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			account := preflightPoolAccount(PlatformZhipu)
			account.Credentials["pool_mode_retry_status_codes"] = []any{float64(status)}
			account.Credentials["custom_error_codes_enabled"] = true
			account.Credentials["custom_error_codes"] = []any{float64(http.StatusUnauthorized)}
			body := []byte(`{"error":{"message":"upstream temporarily unavailable"}}`)
			failure := poolModeHTTPFailover(context.Background(), nil, account, status, http.Header{}, body, "glm-4.6")
			require.Nil(t, failure, "explicitly excluded custom statuses must not trigger retry or account switch")
		})
	}
}
