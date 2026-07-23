package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveOpsPlatformPrefersResolvedCompositeTarget(t *testing.T) {
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
	ctx := service.WithResolvedTargetPlatform(context.Background(), service.PlatformOpenAI)

	require.Equal(t, service.PlatformOpenAI, resolveOpsPlatform(ctx, apiKey, service.PlatformAnthropic))
}

func TestResolveOpsPlatformFallsBackToSelectedAccountPlatform(t *testing.T) {
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
	ctx := context.WithValue(context.Background(), ctxkey.Platform, service.PlatformMoonshot)

	require.Equal(t, service.PlatformMoonshot, resolveOpsPlatform(ctx, apiKey, service.PlatformAnthropic))
}
