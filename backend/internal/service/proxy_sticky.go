package service

import (
	"context"
	"time"
)

type ProxyStickyStore interface {
	Get(ctx context.Context, accountID int64) (int64, bool, error)
	Set(ctx context.Context, accountID, proxyID int64, ttl time.Duration) error
	Refresh(ctx context.Context, accountID int64, ttl time.Duration) error
	DeleteIfMatch(ctx context.Context, accountID, proxyID int64) error
}
