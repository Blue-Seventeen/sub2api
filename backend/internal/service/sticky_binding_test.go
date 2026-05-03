package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stickyBindingCache struct {
	bindings map[string]int64
}

func (c *stickyBindingCache) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	if c == nil || c.bindings == nil {
		return 0, errors.New("not found")
	}
	id, ok := c.bindings[sessionHash]
	if !ok {
		return 0, errors.New("not found")
	}
	return id, nil
}

func (c *stickyBindingCache) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	if c.bindings == nil {
		c.bindings = map[string]int64{}
	}
	c.bindings[sessionHash] = accountID
	return nil
}

func (c *stickyBindingCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *stickyBindingCache) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	delete(c.bindings, sessionHash)
	return nil
}

func TestBindStickySessionPreservesExistingDifferentBinding(t *testing.T) {
	cache := &stickyBindingCache{bindings: map[string]int64{"session": 101}}
	svc := &GatewayService{cache: cache}

	require.NoError(t, svc.BindStickySession(context.Background(), nil, "session", 202))
	require.Equal(t, int64(101), cache.bindings["session"])

	require.NoError(t, svc.BindStickySession(context.Background(), nil, "new-session", 202))
	require.Equal(t, int64(202), cache.bindings["new-session"])

	require.NoError(t, svc.BindStickySession(context.Background(), nil, "session", 101))
	require.Equal(t, int64(101), cache.bindings["session"])
}
