package service

import (
	"context"
	"testing"
	"time"
)

func TestPromotionSettlementRunnerSkipsTickWhenNotLeader(t *testing.T) {
	cache := &fakeLeaderLockCache{}
	_, _ = cache.TryAcquireLeaderLock(context.Background(), promotionSettlementRunnerLeaderLockKey, "peer", time.Minute)

	runner := NewPromotionSettlementRunnerService(&PromotionService{}, nil)
	runner.SetLeaderLock(cache, nil)

	runner.tick()
}
