package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const proxyLatencyKeyPrefix = "proxy:latency:"

func proxyLatencyKey(proxyID int64) string {
	return fmt.Sprintf("%s%d", proxyLatencyKeyPrefix, proxyID)
}

type proxyLatencyCache struct {
	rdb    *redis.Client
	nodeID string
}

func NewProxyLatencyCache(rdb *redis.Client) service.ProxyLatencyCache {
	return NewProxyLatencyCacheWithNodeID(rdb, service.CurrentNodeID())
}

func NewProxyLatencyCacheWithNodeID(rdb *redis.Client, nodeID string) service.ProxyLatencyCache {
	return &proxyLatencyCache{
		rdb:    rdb,
		nodeID: service.ResolveNodeID(nodeID),
	}
}

func proxyLatencyNodeKey(nodeID string, proxyID int64) string {
	return fmt.Sprintf("%s%s:%d", proxyLatencyKeyPrefix, service.ResolveNodeID(nodeID), proxyID)
}

func (c *proxyLatencyCache) GetProxyLatencies(ctx context.Context, proxyIDs []int64) (map[int64]*service.ProxyLatencyInfo, error) {
	results := make(map[int64]*service.ProxyLatencyInfo)
	if len(proxyIDs) == 0 {
		return results, nil
	}

	keys := make([]string, 0, len(proxyIDs))
	for _, id := range proxyIDs {
		keys = append(keys, proxyLatencyNodeKey(c.nodeID, id))
	}

	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return results, err
	}
	missingIDs := make([]int64, 0)

	for i, raw := range values {
		if raw == nil {
			missingIDs = append(missingIDs, proxyIDs[i])
			continue
		}
		if info := decodeProxyLatencyInfo(raw); info != nil {
			results[proxyIDs[i]] = info
		}
	}

	if len(missingIDs) > 0 {
		legacyKeys := make([]string, 0, len(missingIDs))
		for _, id := range missingIDs {
			legacyKeys = append(legacyKeys, proxyLatencyKey(id))
		}
		legacyValues, err := c.rdb.MGet(ctx, legacyKeys...).Result()
		if err != nil {
			return results, err
		}
		for i, raw := range legacyValues {
			if info := decodeProxyLatencyInfo(raw); info != nil {
				results[missingIDs[i]] = info
			}
		}
	}

	return results, nil
}

func (c *proxyLatencyCache) SetProxyLatency(ctx context.Context, proxyID int64, info *service.ProxyLatencyInfo) error {
	if info == nil {
		return nil
	}
	payload, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, proxyLatencyNodeKey(c.nodeID, proxyID), payload, 0).Err()
}

func decodeProxyLatencyInfo(raw any) *service.ProxyLatencyInfo {
	if raw == nil {
		return nil
	}
	var payload []byte
	switch v := raw.(type) {
	case string:
		payload = []byte(v)
	case []byte:
		payload = v
	default:
		return nil
	}
	var info service.ProxyLatencyInfo
	if err := json.Unmarshal(payload, &info); err != nil {
		return nil
	}
	return &info
}
