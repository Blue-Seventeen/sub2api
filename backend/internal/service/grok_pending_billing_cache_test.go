package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type grokPendingCache struct {
	GatewayCache
	payloads   map[string][]byte
	claimed    map[string]bool
	bindingTTL time.Duration
}

func (c *grokPendingCache) SetGrokVideoPendingBilling(_ context.Context, key string, payload []byte, ttl time.Duration) error {
	c.payloads[key] = append([]byte(nil), payload...)
	return nil
}

func (c *grokPendingCache) GetGrokVideoPendingBilling(_ context.Context, key string) ([]byte, error) {
	return c.payloads[key], nil
}

func (c *grokPendingCache) ClaimGrokVideoBilled(_ context.Context, key string, _ time.Duration) (bool, error) {
	if c.claimed[key] {
		return false, nil
	}
	c.claimed[key] = true
	return true, nil
}

func (c *grokPendingCache) ReleaseGrokVideoBilled(_ context.Context, key string) error {
	delete(c.claimed, key)
	return nil
}

func (c *grokPendingCache) SetSessionAccountID(_ context.Context, _ int64, _ string, _ int64, ttl time.Duration) error {
	c.bindingTTL = ttl
	return nil
}

func TestGrokPendingBillingOwnerIsolationAndRetry(t *testing.T) {
	ctx := context.Background()
	cache := &grokPendingCache{payloads: map[string][]byte{}, claimed: map[string]bool{}}
	svc := &OpenAIGatewayService{cache: cache}
	require.NoError(t, svc.StoreGrokVideoPendingBilling(ctx, " task ", 1, 2, GrokVideoPendingBilling{Model: " video ", VideoResolution: "720p", VideoDurationSeconds: 6}))
	pending, err := svc.LoadGrokVideoPendingBilling(ctx, "task", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "video", pending.Model)
	require.Equal(t, "720p", pending.VideoResolution)
	require.NotEmpty(t, pending.CreatedAt)
	for _, owner := range [][2]int64{{2, 2}, {1, 3}} {
		other, err := svc.LoadGrokVideoPendingBilling(ctx, "task", owner[0], owner[1])
		require.NoError(t, err)
		require.Nil(t, other)
	}
	claimed, err := svc.ClaimGrokVideoBilling(ctx, "task", 1, 2)
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = svc.ClaimGrokVideoBilling(ctx, "task", 1, 2)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, svc.ReleaseGrokVideoBilling(ctx, "task", 1, 2))
	claimed, err = svc.ClaimGrokVideoBilling(ctx, "task", 1, 2)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, svc.BindGrokMediaVideoRequestAccount(ctx, nil, "task", 1, 2, 3))
	require.GreaterOrEqual(t, cache.bindingTTL, 24*time.Hour)
	require.Equal(t, "grok-video:task", StableGrokVideoBillingRequestID(" task "))
	require.Equal(t, "grok-video:task", StableGrokVideoBillingRequestID("grok-video:task"))
}

func TestGrokPendingBillingRejectsInvalidOwnerAndUnavailableCache(t *testing.T) {
	ctx := context.Background()
	for _, svc := range []*OpenAIGatewayService{nil, {}} {
		require.Error(t, svc.StoreGrokVideoPendingBilling(ctx, "task", 1, 2, GrokVideoPendingBilling{}))
		claimed, err := svc.ClaimGrokVideoBilling(ctx, "task", 1, 2)
		require.Error(t, err)
		require.False(t, claimed)
	}
	svc := &OpenAIGatewayService{cache: &grokPendingCache{}}
	for _, owner := range [][2]int64{{0, 2}, {1, 0}, {-1, 2}} {
		require.Error(t, svc.StoreGrokVideoPendingBilling(ctx, "task", owner[0], owner[1], GrokVideoPendingBilling{}))
	}
	require.Error(t, svc.StoreGrokVideoPendingBilling(ctx, " ", 1, 2, GrokVideoPendingBilling{}))
}
