//go:build unit

package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Pool-mode custom codes govern account-state mutation, not safe request replay.
func TestPoolHTTPExcludedCustomStatusStillFailsOverWithoutRetiringAccount(t *testing.T) {
	for _, status := range []int{403, 429, 500, 502, 503, 504} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			account := preflightPoolAccount(PlatformZhipu)
			account.Credentials["pool_mode_retry_status_codes"] = []any{float64(status)}
			account.Credentials["custom_error_codes_enabled"] = true
			account.Credentials["custom_error_codes"] = []any{float64(http.StatusUnauthorized)}
			body := []byte(`{"error":{"message":"upstream temporarily unavailable"}}`)
			repo := &errorPolicyRepoStub{}
			rateLimit := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			failure := poolModeHTTPFailover(context.Background(), rateLimit, account, status, http.Header{}, body, "glm-4.6")
			require.NotNil(t, failure, "custom exclusions must not strand an otherwise replay-safe pool request")
			require.True(t, failure.RetryableOnSameAccount, "the handler still owns the configured same-account budget")
			require.Zero(t, repo.setErrCalls, "excluded codes must not retire the selected account")
			account.Credentials["pool_mode"] = false
			require.Nil(t, poolModeHTTPFailover(context.Background(), rateLimit, account, status, http.Header{}, body, "glm-4.6"), "non-pool custom exclusions retain the old terminal contract")
		})
	}
}
