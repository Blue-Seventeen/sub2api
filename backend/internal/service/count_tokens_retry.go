package service

import (
	"context"
	"net/http"
)

func gatewayRequestAttemptsEnabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	state, _ := ctx.Value(gatewayRequestAttemptContextKey{}).(*gatewayRequestAttemptState)
	return state != nil
}

// Counting has no model-generation or billing side effects, so even an
// interrupted read can safely retry before any response reaches the client.
func countTokensTransientError(ctx context.Context, account *Account) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(`{"error":{"type":"upstream_error","message":"Token counting upstream unavailable"}}`), RetryableOnSameAccount: account != nil && account.IsPoolMode() && account.IsPoolModeRetryableStatus(http.StatusBadGateway)}
}
