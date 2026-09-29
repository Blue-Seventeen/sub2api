package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheReasoningContent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := NewGatewayCache(client)
	t.Cleanup(func() { _ = client.Close() })
	scoped, ok := cache.(interface {
		SetScopedReasoningContent(context.Context, int64, int64, string, string, time.Duration) error
		GetScopedReasoningContent(context.Context, int64, int64, string) (string, error)
	})
	require.True(t, ok, "repository must support tenant-scoped reasoning")
	ctx := context.Background()

	// 未命中返回哨兵错误，区别于真实读取失败。
	_, err := scoped.GetScopedReasoningContent(ctx, 1, 11, "item_missing")
	require.ErrorIs(t, err, service.ErrReasoningContentNotFound)

	// 写入后可读回。
	require.NoError(t, scoped.SetScopedReasoningContent(ctx, 1, 11, "item_abc", "think hard", time.Minute))
	got, err := scoped.GetScopedReasoningContent(ctx, 1, 11, "item_abc")
	require.NoError(t, err)
	require.Equal(t, "think hard", got)

	// ttl<=0 时兜底为默认 7 天。
	require.NoError(t, scoped.SetScopedReasoningContent(ctx, 1, 11, "item_ttl", "x", 0))
	mr.FastForward(time.Minute)
	_, err = scoped.GetScopedReasoningContent(ctx, 1, 11, "item_abc")
	require.ErrorIs(t, err, service.ErrReasoningContentNotFound)
	got, err = scoped.GetScopedReasoningContent(ctx, 1, 11, "item_ttl")
	require.NoError(t, err)
	require.Equal(t, "x", got)
	mr.FastForward(7 * 24 * time.Hour)
	_, err = scoped.GetScopedReasoningContent(ctx, 1, 11, "item_ttl")
	require.ErrorIs(t, err, service.ErrReasoningContentNotFound)

	// 空 itemID / 空 content 是 no-op（无可缓存内容不算错误）。
	require.NoError(t, scoped.SetScopedReasoningContent(ctx, 1, 11, "", "x", time.Minute))
	require.NoError(t, scoped.SetScopedReasoningContent(ctx, 1, 11, "item_empty", "", time.Minute))
	_, err = scoped.GetScopedReasoningContent(ctx, 1, 11, "item_empty")
	require.ErrorIs(t, err, service.ErrReasoningContentNotFound)

	for _, tenant := range [][2]int64{{1, 11}, {2, 11}, {1, 12}} {
		content := fmt.Sprintf("private-%d-%d", tenant[0], tenant[1])
		require.NoError(t, scoped.SetScopedReasoningContent(ctx, tenant[0], tenant[1], "shared-id", content, time.Hour))
	}
	for _, tenant := range [][2]int64{{1, 11}, {2, 11}, {1, 12}} {
		got, err := scoped.GetScopedReasoningContent(ctx, tenant[0], tenant[1], "shared-id")
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("private-%d-%d", tenant[0], tenant[1]), got)
	}
	require.NoError(t, mr.Set("reasoning_content:legacy-id", "legacy private"))
	_, err = scoped.GetScopedReasoningContent(ctx, 1, 11, "legacy-id")
	require.ErrorIs(t, err, service.ErrReasoningContentNotFound, "never fall back to legacy keys")
	before := mr.Keys()
	for _, tenant := range [][2]int64{{0, 0}, {1, 0}, {0, 11}, {-1, 11}, {1, -11}} {
		require.NoError(t, scoped.SetScopedReasoningContent(ctx, tenant[0], tenant[1], "shared-id", "poison", time.Hour))
		_, err := scoped.GetScopedReasoningContent(ctx, tenant[0], tenant[1], "shared-id")
		require.ErrorIs(t, err, service.ErrReasoningContentNotFound)
	}
	require.Equal(t, before, mr.Keys(), "missing identity must not create cache entries")
}

func TestGatewayCacheLegacyReasoningAccessIsDisabled(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client)
	ctx := context.Background()
	require.NoError(t, mr.Set("reasoning_content:legacy-id", "legacy private"))
	_, err := cache.GetReasoningContent(ctx, "legacy-id") //nolint:staticcheck // this test verifies the deprecated unscoped API stays disabled
	require.ErrorIs(t, err, service.ErrReasoningContentNotFound)
	require.NoError(t, cache.SetReasoningContent(ctx, "new-id", "private", time.Minute)) //nolint:staticcheck // this test verifies the deprecated unscoped API stays disabled
	require.Equal(t, []string{"reasoning_content:legacy-id"}, mr.Keys())
}
