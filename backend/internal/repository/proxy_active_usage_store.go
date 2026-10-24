package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type RedisProxyActiveUsageStore struct {
	rdb *redis.Client
}

func NewRedisProxyActiveUsageStore(rdb *redis.Client) *RedisProxyActiveUsageStore {
	if rdb == nil {
		return nil
	}
	return &RedisProxyActiveUsageStore{rdb: rdb}
}

func (s *RedisProxyActiveUsageStore) UpsertProxyActiveUsage(ctx context.Context, entry service.ProxyActiveUsageEntry, ttl time.Duration) error {
	if s == nil || s.rdb == nil || !entry.Valid() {
		return nil
	}
	ttlMs := proxyActiveUsageTTLMillis(ttl)
	keys := proxyActiveUsageKeys(entry)
	payload := strconv.FormatInt(entry.ProxyID, 10) + ":" + strconv.FormatInt(entry.AccountID, 10)
	return s.rdb.Eval(ctx, service.ProxyActiveUsageUpsertScript(), keys, entry.Token, ttlMs, strconv.FormatInt(entry.AccountID, 10), payload).Err()
}

func (s *RedisProxyActiveUsageStore) RemoveProxyActiveUsage(ctx context.Context, entry service.ProxyActiveUsageEntry, ttl time.Duration) error {
	if s == nil || s.rdb == nil || !entry.Valid() {
		return nil
	}
	keys := proxyActiveUsageKeys(entry)
	return s.rdb.Eval(ctx, service.ProxyActiveUsageRemoveScript(), keys, entry.Token, proxyActiveUsageTTLMillis(ttl), strconv.FormatInt(entry.AccountID, 10)).Err()
}

func (s *RedisProxyActiveUsageStore) CountProxyActiveAccounts(ctx context.Context, proxyIDs []int64) (map[int64]int64, error) {
	out := zeroProxyActiveUsageCounts(proxyIDs)
	counts, err := s.CountProxyActiveUsage(ctx, proxyIDs)
	if err != nil {
		return out, err
	}
	for proxyID, count := range counts {
		out[proxyID] = count.AccountCount
	}
	return out, nil
}

func (s *RedisProxyActiveUsageStore) CountProxyActiveUsage(ctx context.Context, proxyIDs []int64) (map[int64]service.ProxyActiveUsageCounts, error) {
	out := make(map[int64]service.ProxyActiveUsageCounts, len(proxyIDs))
	for _, proxyID := range proxyIDs {
		if proxyID > 0 {
			out[proxyID] = service.ProxyActiveUsageCounts{}
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	if s == nil || s.rdb == nil {
		return nil, errors.New("proxy active usage store unavailable")
	}
	pipe := s.rdb.Pipeline()
	countCmds := make(map[int64]*redis.Cmd, len(out))
	for proxyID := range out {
		keys := []string{proxyActiveUsageAccountsKey(proxyID), proxyActiveUsageRequestsKey(proxyID)}
		countCmds[proxyID] = pipe.Eval(ctx, service.ProxyActiveUsageCountScript(), keys)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	for proxyID, cmd := range countCmds {
		counts, err := cmd.Int64Slice()
		if err != nil {
			return nil, err
		}
		if len(counts) != 2 {
			return nil, fmt.Errorf("invalid proxy active usage counts for proxy %d", proxyID)
		}
		out[proxyID] = service.ProxyActiveUsageCounts{AccountCount: counts[0], RequestCount: counts[1]}
	}
	return out, nil
}

func proxyActiveUsageTTLMillis(ttl time.Duration) int64 {
	ttlMs := ttl.Milliseconds()
	if ttlMs <= 0 {
		ttlMs = (2 * time.Minute).Milliseconds()
	}
	return ttlMs
}

func proxyActiveUsageKeys(entry service.ProxyActiveUsageEntry) []string {
	return []string{
		proxyActiveUsageAccountsKey(entry.ProxyID),
		proxyActiveUsageAccountRequestsKey(entry.ProxyID, entry.AccountID),
		proxyActiveUsageRequestKey(entry.Token),
		proxyActiveUsageEndedKey(entry.Token),
		proxyActiveUsageRequestsKey(entry.ProxyID),
	}
}

// v2 isolates expiring token sorted sets from legacy account SET keys during upgrades.
func proxyActiveUsageAccountsKey(proxyID int64) string {
	return "proxy_active_usage:v2:proxy:" + strconv.FormatInt(proxyID, 10) + ":accounts"
}

func proxyActiveUsageAccountRequestsKey(proxyID, accountID int64) string {
	return "proxy_active_usage:v2:proxy:" + strconv.FormatInt(proxyID, 10) + ":account:" + strconv.FormatInt(accountID, 10) + ":requests"
}

func proxyActiveUsageRequestsKey(proxyID int64) string {
	return "proxy_active_usage:v2:proxy:" + strconv.FormatInt(proxyID, 10) + ":requests"
}

func proxyActiveUsageRequestKey(token string) string {
	return "proxy_active_usage:v2:req:" + token
}

func proxyActiveUsageEndedKey(token string) string {
	return "proxy_active_usage:v2:req_ended:" + token
}

func zeroProxyActiveUsageCounts(proxyIDs []int64) map[int64]int64 {
	out := make(map[int64]int64)
	for _, proxyID := range proxyIDs {
		if proxyID > 0 {
			out[proxyID] = 0
		}
	}
	return out
}
