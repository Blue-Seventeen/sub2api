package service

import (
	"context"
	"time"
)

// ScopedReasoningContentCache is optional to preserve GatewayCache implementer
// compatibility. Callers must skip caching when unavailable, never fall back to
// the legacy unscoped methods. Both IDs must come from authenticated identity.
type ScopedReasoningContentCache interface {
	SetScopedReasoningContent(ctx context.Context, userID, apiKeyID int64, itemID, content string, ttl time.Duration) error
	GetScopedReasoningContent(ctx context.Context, userID, apiKeyID int64, itemID string) (string, error)
}
