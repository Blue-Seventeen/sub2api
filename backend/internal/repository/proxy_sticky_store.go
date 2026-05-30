package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const proxyStickyAccountPrefix = "proxy_sticky_account:"

var proxyStickyDeleteIfMatchScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

type proxyStickyStore struct {
	rdb    *redis.Client
	nodeID string
}

func NewProxyStickyStore(rdb *redis.Client) service.ProxyStickyStore {
	return NewProxyStickyStoreWithNodeID(rdb, service.CurrentNodeID())
}

func NewProxyStickyStoreWithNodeID(rdb *redis.Client, nodeID string) service.ProxyStickyStore {
	return &proxyStickyStore{
		rdb:    rdb,
		nodeID: service.ResolveNodeID(nodeID),
	}
}

func (s *proxyStickyStore) Get(ctx context.Context, accountID int64) (int64, bool, error) {
	if s == nil || s.rdb == nil || accountID <= 0 {
		return 0, false, nil
	}
	value, err := s.rdb.Get(ctx, proxyStickyAccountNodeKey(s.nodeID, accountID)).Int64()
	if err == redis.Nil {
		value, err = s.rdb.Get(ctx, proxyStickyAccountKey(accountID)).Int64()
		if err == redis.Nil {
			return 0, false, nil
		}
	}
	if err != nil {
		return 0, false, err
	}
	if value <= 0 {
		return 0, false, nil
	}
	return value, true, nil
}

func (s *proxyStickyStore) Set(ctx context.Context, accountID, proxyID int64, ttl time.Duration) error {
	if s == nil || s.rdb == nil || accountID <= 0 || proxyID <= 0 {
		return nil
	}
	return s.rdb.Set(ctx, proxyStickyAccountNodeKey(s.nodeID, accountID), proxyID, ttl).Err()
}

func (s *proxyStickyStore) Refresh(ctx context.Context, accountID int64, ttl time.Duration) error {
	if s == nil || s.rdb == nil || accountID <= 0 {
		return nil
	}
	return s.rdb.Expire(ctx, proxyStickyAccountNodeKey(s.nodeID, accountID), ttl).Err()
}

func (s *proxyStickyStore) DeleteIfMatch(ctx context.Context, accountID, proxyID int64) error {
	if s == nil || s.rdb == nil || accountID <= 0 || proxyID <= 0 {
		return nil
	}
	if err := proxyStickyDeleteIfMatchScript.Run(
		ctx,
		s.rdb,
		[]string{proxyStickyAccountNodeKey(s.nodeID, accountID)},
		strconv.FormatInt(proxyID, 10),
	).Err(); err != nil {
		return err
	}
	return proxyStickyDeleteIfMatchScript.Run(
		ctx,
		s.rdb,
		[]string{proxyStickyAccountKey(accountID)},
		strconv.FormatInt(proxyID, 10),
	).Err()
}

func proxyStickyAccountKey(accountID int64) string {
	return fmt.Sprintf("%s%d", proxyStickyAccountPrefix, accountID)
}

func proxyStickyAccountNodeKey(nodeID string, accountID int64) string {
	return fmt.Sprintf("%s%s:%d", proxyStickyAccountPrefix, service.ResolveNodeID(nodeID), accountID)
}
