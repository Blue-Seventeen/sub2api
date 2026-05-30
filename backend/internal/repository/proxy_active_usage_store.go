package repository

import (
	"context"
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
	expireAtMs := time.Now().Add(time.Duration(ttlMs) * time.Millisecond).UnixMilli()
	keys := proxyActiveUsageKeys(entry)
	payload := strconv.FormatInt(entry.ProxyID, 10) + ":" + strconv.FormatInt(entry.AccountID, 10)
	return s.rdb.Eval(ctx, service.ProxyActiveUsageUpsertScript(), keys, entry.Token, ttlMs, expireAtMs, strconv.FormatInt(entry.AccountID, 10), ttlMs*2, payload).Err()
}

func (s *RedisProxyActiveUsageStore) RemoveProxyActiveUsage(ctx context.Context, entry service.ProxyActiveUsageEntry, ttl time.Duration) error {
	if s == nil || s.rdb == nil || !entry.Valid() {
		return nil
	}
	keys := proxyActiveUsageKeys(entry)
	return s.rdb.Eval(ctx, service.ProxyActiveUsageRemoveScript(), keys, entry.Token, proxyActiveUsageTTLMillis(ttl), time.Now().UnixMilli(), strconv.FormatInt(entry.AccountID, 10)).Err()
}

func (s *RedisProxyActiveUsageStore) CountProxyActiveAccounts(ctx context.Context, proxyIDs []int64) (map[int64]int64, error) {
	out := zeroProxyActiveUsageCounts(proxyIDs)
	if s == nil || s.rdb == nil || len(out) == 0 {
		return out, nil
	}
	nowMs := strconv.FormatInt(time.Now().UnixMilli(), 10)
	pipe := s.rdb.Pipeline()
	cardCmds := make(map[int64]*redis.IntCmd, len(out))
	for proxyID := range out {
		key := proxyActiveUsageAccountsKey(proxyID)
		pipe.ZRemRangeByScore(ctx, key, "-inf", nowMs)
		cardCmds[proxyID] = pipe.ZCard(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return out, err
	}
	for proxyID, cmd := range cardCmds {
		count, err := cmd.Result()
		if err == nil && count > 0 {
			out[proxyID] = count
		}
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
	}
}

func proxyActiveUsageAccountsKey(proxyID int64) string {
	return "proxy_active_usage:proxy:" + strconv.FormatInt(proxyID, 10) + ":accounts"
}

func proxyActiveUsageAccountRequestsKey(proxyID, accountID int64) string {
	return "proxy_active_usage:proxy:" + strconv.FormatInt(proxyID, 10) + ":account:" + strconv.FormatInt(accountID, 10) + ":requests"
}

func proxyActiveUsageRequestKey(token string) string {
	return "proxy_active_usage:req:" + token
}

func proxyActiveUsageEndedKey(token string) string {
	return "proxy_active_usage:req_ended:" + token
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
