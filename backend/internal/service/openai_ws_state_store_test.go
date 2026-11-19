package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWSStateStore_BindGetDeleteResponseAccount(t *testing.T) {
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	ctx := context.Background()
	groupID := int64(7)

	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_abc", 101, time.Minute))

	accountID, err := store.GetResponseAccount(ctx, groupID, "resp_abc")
	require.NoError(t, err)
	require.Equal(t, int64(101), accountID)

	require.NoError(t, store.DeleteResponseAccount(ctx, groupID, "resp_abc"))
	accountID, err = store.GetResponseAccount(ctx, groupID, "resp_abc")
	require.NoError(t, err)
	require.Zero(t, accountID)
}

func TestOpenAIWSStateStore_HTTPResponseOwnerPersistsAcrossStoreInstances(t *testing.T) {
	cache := &stubGatewayCache{}
	ctx := context.Background()
	groupID := int64(8)
	writer := NewOpenAIWSStateStore(cache)

	require.NoError(t, writer.BindHTTPResponseOwner(ctx, groupID, "resp_owned", 201, 301, time.Minute))
	userID, apiKeyID, found, err := writer.GetHTTPResponseOwner(ctx, groupID, "resp_owned")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(201), userID)
	require.Equal(t, int64(301), apiKeyID)

	reader := NewOpenAIWSStateStore(cache)
	userID, apiKeyID, found, err = reader.GetHTTPResponseOwner(ctx, groupID, "resp_owned")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(201), userID)
	require.Equal(t, int64(301), apiKeyID)
}

func TestOpenAIWSStateStore_ResponseConnTTL(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	store.BindResponseConn("resp_conn", "conn_1", 30*time.Millisecond)

	connID, ok := store.GetResponseConn("resp_conn")
	require.True(t, ok)
	require.Equal(t, "conn_1", connID)

	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetResponseConn("resp_conn")
	require.False(t, ok)
}

func TestOpenAIWSStateStore_SessionTurnStateTTL(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	const accountID = int64(131)
	store.BindSessionTurnState(9, accountID, "session_hash_1", "turn_state_1", 30*time.Millisecond)

	state, ok := store.GetSessionTurnState(9, accountID, "session_hash_1")
	require.True(t, ok)
	require.Equal(t, "turn_state_1", state)

	// group 隔离
	_, ok = store.GetSessionTurnState(10, accountID, "session_hash_1")
	require.False(t, ok)

	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetSessionTurnState(9, accountID, "session_hash_1")
	require.False(t, ok)
}

func TestOpenAIWSStateStore_SessionConnTTL(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	const accountID = int64(131)
	store.BindSessionConn(9, accountID, "session_hash_conn_1", "conn_1", 30*time.Millisecond)

	connID, ok := store.GetSessionConn(9, accountID, "session_hash_conn_1")
	require.True(t, ok)
	require.Equal(t, "conn_1", connID)

	// group 隔离
	_, ok = store.GetSessionConn(10, accountID, "session_hash_conn_1")
	require.False(t, ok)

	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetSessionConn(9, accountID, "session_hash_conn_1")
	require.False(t, ok)
}

// 账号隔离的反向验证：同一个 (group, sessionHash) 在换上游账号后必须读不到上一个
// 账号铸造的 x-codex-turn-state，否则后者会被原样转发给新账号（D4 缺陷）。
// 同一账号自身仍必须能读回，证明隔离不是"一律读不到"的假检查。
func TestOpenAIWSStateStore_SessionTurnStateIsolatedByAccount(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	const (
		groupID     = int64(7)
		accountA    = int64(129)
		accountB    = int64(130)
		sessionHash = "6794ac38c44c22fa"
	)

	store.BindSessionTurnState(groupID, accountA, sessionHash, "blob-minted-by-account-A", time.Hour)

	turnState, ok := store.GetSessionTurnState(groupID, accountA, sessionHash)
	require.True(t, ok, "铸造账号自己必须能读回")
	require.Equal(t, "blob-minted-by-account-A", turnState)

	turnState, ok = store.GetSessionTurnState(groupID, accountB, sessionHash)
	require.False(t, ok, "换账号后不得读到上一个账号铸造的工牌")
	require.Empty(t, turnState)
}

// 连接绑定（store=false 时的会话→上游连接粘性）同源缺陷：换账号后不能再拿旧账号的
// 原生 WS 连接。
func TestOpenAIWSStateStore_SessionConnIsolatedByAccount(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	const (
		groupID     = int64(7)
		accountA    = int64(129)
		accountB    = int64(130)
		sessionHash = "6794ac38c44c22fa"
	)

	store.BindSessionConn(groupID, accountA, sessionHash, "conn-bound-by-account-A", time.Hour)

	connID, ok := store.GetSessionConn(groupID, accountA, sessionHash)
	require.True(t, ok, "绑定账号自己必须能读回")
	require.Equal(t, "conn-bound-by-account-A", connID)

	connID, ok = store.GetSessionConn(groupID, accountB, sessionHash)
	require.False(t, ok, "换账号后不得命中上一个账号的会话连接绑定")
	require.Empty(t, connID)
}

// invalid_encrypted_content lineage 同源缺陷：A 账号记录的失效密文摘要不得让 B 账号
// 莫名剥离它从未拒绝过的密文。
func TestOpenAIWSStateStore_InvalidEncryptedContentIsolatedByAccount(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	const (
		groupID     = int64(7)
		accountA    = int64(129)
		accountB    = int64(130)
		sessionHash = "6794ac38c44c22fa"
	)

	store.MarkSessionInvalidEncryptedContent(groupID, accountA, sessionHash, []string{"digest-1"}, time.Hour)

	digests := store.GetSessionInvalidEncryptedContentDigests(groupID, accountA, sessionHash)
	require.Len(t, digests, 1, "标记账号自己必须读得到")
	require.Contains(t, digests, "digest-1")

	require.Nil(t, store.GetSessionInvalidEncryptedContentDigests(groupID, accountB, sessionHash),
		"换账号后不得命中上一个账号记录的失效密文摘要")
}

// 同一 accountID 内的行为必须与修复前完全一致：写入后能读、Delete 后读不到、
// TTL 过期读不到（三类会话级状态各验一遍）。
func TestOpenAIWSStateStore_SessionStateSameAccountBehaviorUnchanged(t *testing.T) {
	const (
		groupID     = int64(7)
		accountID   = int64(129)
		sessionHash = "6794ac38c44c22fa"
	)
	store := NewOpenAIWSStateStore(nil)

	// turn state：写 → 读 → Delete 后读不到 → TTL 过期后读不到。
	store.BindSessionTurnState(groupID, accountID, sessionHash, "turn-state", time.Hour)
	_, ok := store.GetSessionTurnState(groupID, accountID, sessionHash)
	require.True(t, ok)
	store.DeleteSessionTurnState(groupID, accountID, sessionHash)
	_, ok = store.GetSessionTurnState(groupID, accountID, sessionHash)
	require.False(t, ok, "Delete 后同一账号也不得读回")
	store.BindSessionTurnState(groupID, accountID, sessionHash, "turn-state-ttl", 30*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetSessionTurnState(groupID, accountID, sessionHash)
	require.False(t, ok, "TTL 过期后同一账号不得读回")

	// session conn：同上。
	store.BindSessionConn(groupID, accountID, sessionHash, "conn-1", time.Hour)
	_, ok = store.GetSessionConn(groupID, accountID, sessionHash)
	require.True(t, ok)
	store.DeleteSessionConn(groupID, accountID, sessionHash)
	_, ok = store.GetSessionConn(groupID, accountID, sessionHash)
	require.False(t, ok, "Delete 后同一账号也不得读回")
	store.BindSessionConn(groupID, accountID, sessionHash, "conn-ttl", 30*time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	_, ok = store.GetSessionConn(groupID, accountID, sessionHash)
	require.False(t, ok, "TTL 过期后同一账号不得读回")

	// invalid_encrypted lineage：写 → 读 → TTL 过期后读不到（该类型没有 Delete API，
	// 过期是唯一的清理路径）。
	store.MarkSessionInvalidEncryptedContent(groupID, accountID, sessionHash, []string{"digest-ttl"}, 30*time.Millisecond)
	require.Len(t, store.GetSessionInvalidEncryptedContentDigests(groupID, accountID, sessionHash), 1)
	time.Sleep(60 * time.Millisecond)
	require.Nil(t, store.GetSessionInvalidEncryptedContentDigests(groupID, accountID, sessionHash),
		"TTL 过期后同一账号不得读回")
}

// 键格式：账号维度必须在键里，且旧格式（group:sessionHash）不得与新键碰撞，
// 否则滚动升级期间旧版本写入的条目会被新版本读到（违反"安全降级"）。
func TestOpenAIWSSessionAccountScopeKeyIncludesAccountAndDoesNotCollideWithLegacy(t *testing.T) {
	require.Equal(t, "7:129:6794ac38c44c22fa", openAIWSSessionAccountScopeKey(7, 129, "6794ac38c44c22fa"))
	require.Equal(t, "", openAIWSSessionAccountScopeKey(7, 129, "  "))
	require.NotEqual(t,
		openAIWSSessionAccountScopeKey(7, 129, "6794ac38c44c22fa"),
		openAIWSSessionAccountScopeKey(7, 130, "6794ac38c44c22fa"),
		"换账号必须换键",
	)
	require.NotEqual(t,
		fmt.Sprintf("%d:%s", 7, "6794ac38c44c22fa"),
		openAIWSSessionAccountScopeKey(7, 129, "6794ac38c44c22fa"),
		"同一 group+hash 的旧格式键与新键必须不同，否则旧条目会被新版本读到",
	)
}

// 滚动升级安全降级：旧版本（无账号维度）留下的条目在新版本读路径上不可见，
// 等价于"该会话没有工牌"，而不是被误当作本账号的状态。
func TestOpenAIWSStateStore_LegacyAccountAgnosticEntryNotVisible(t *testing.T) {
	raw := NewOpenAIWSStateStore(nil)
	store, ok := raw.(*defaultOpenAIWSStateStore)
	require.True(t, ok)
	legacyKey := fmt.Sprintf("%d:%s", 7, "6794ac38c44c22fa")
	require.NotEqual(t, legacyKey, openAIWSSessionAccountScopeKey(7, 129, "6794ac38c44c22fa"))

	store.sessionInvalidEncryptedMu.Lock()
	store.sessionInvalidEncrypted[legacyKey] = openAIWSInvalidEncryptedBinding{
		digests:   map[string]struct{}{"legacy-digest": {}},
		expiresAt: time.Now().Add(time.Hour),
	}
	store.sessionInvalidEncryptedMu.Unlock()
	store.sessionToTurnStateMu.Lock()
	store.sessionToTurnState[legacyKey] = openAIWSTurnStateBinding{
		turnState: "legacy-turn-state",
		expiresAt: time.Now().Add(time.Hour),
	}
	store.sessionToTurnStateMu.Unlock()
	store.sessionToConnMu.Lock()
	store.sessionToConn[legacyKey] = openAIWSSessionConnBinding{
		connID:    "legacy-conn",
		expiresAt: time.Now().Add(time.Hour),
	}
	store.sessionToConnMu.Unlock()

	for _, accountID := range []int64{129, 130} {
		_, ok := store.GetSessionTurnState(7, accountID, "6794ac38c44c22fa")
		require.False(t, ok, "旧格式条目对新版本不可见（account=%d）", accountID)
		_, ok = store.GetSessionConn(7, accountID, "6794ac38c44c22fa")
		require.False(t, ok, "旧格式条目对新版本不可见（account=%d）", accountID)
		require.Nil(t, store.GetSessionInvalidEncryptedContentDigests(7, accountID, "6794ac38c44c22fa"),
			"旧格式条目对新版本不可见（account=%d）", accountID)
	}
}

func TestOpenAIWSStateStore_GetResponseAccount_NoStaleAfterCacheMiss(t *testing.T) {
	cache := &stubGatewayCache{sessionBindings: map[string]int64{}}
	store := NewOpenAIWSStateStore(cache)
	ctx := context.Background()
	groupID := int64(17)
	responseID := "resp_cache_stale"
	cacheKey := openAIWSResponseAccountCacheKey(responseID)

	cache.sessionBindings[cacheKey] = 501
	accountID, err := store.GetResponseAccount(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Equal(t, int64(501), accountID)

	delete(cache.sessionBindings, cacheKey)
	accountID, err = store.GetResponseAccount(ctx, groupID, responseID)
	require.NoError(t, err)
	require.Zero(t, accountID, "上游缓存失效后不应继续命中本地陈旧映射")
}

func TestOpenAIWSStateStore_MaybeCleanupRemovesExpiredIncrementally(t *testing.T) {
	raw := NewOpenAIWSStateStore(nil)
	store, ok := raw.(*defaultOpenAIWSStateStore)
	require.True(t, ok)

	expiredAt := time.Now().Add(-time.Minute)
	total := 2048
	store.responseToConnMu.Lock()
	for i := 0; i < total; i++ {
		store.responseToConn[fmt.Sprintf("resp_%d", i)] = openAIWSConnBinding{
			connID:    "conn_incremental",
			expiresAt: expiredAt,
		}
	}
	store.responseToConnMu.Unlock()

	store.lastCleanupUnixNano.Store(time.Now().Add(-2 * openAIWSStateStoreCleanupInterval).UnixNano())
	store.maybeCleanup()

	store.responseToConnMu.RLock()
	remainingAfterFirst := len(store.responseToConn)
	store.responseToConnMu.RUnlock()
	require.Less(t, remainingAfterFirst, total, "单轮 cleanup 应至少有进展")
	require.Greater(t, remainingAfterFirst, 0, "增量清理不要求单轮清空全部键")

	for i := 0; i < 8; i++ {
		store.lastCleanupUnixNano.Store(time.Now().Add(-2 * openAIWSStateStoreCleanupInterval).UnixNano())
		store.maybeCleanup()
	}

	store.responseToConnMu.RLock()
	remaining := len(store.responseToConn)
	store.responseToConnMu.RUnlock()
	require.Zero(t, remaining, "多轮 cleanup 后应逐步清空全部过期键")
}

func TestEnsureBindingCapacity_EvictsOneWhenMapIsFull(t *testing.T) {
	bindings := map[string]int{
		"a": 1,
		"b": 2,
	}

	ensureBindingCapacity(bindings, "c", 2)
	bindings["c"] = 3

	require.Len(t, bindings, 2)
	require.Equal(t, 3, bindings["c"])
}

func TestEnsureBindingCapacity_DoesNotEvictWhenUpdatingExistingKey(t *testing.T) {
	bindings := map[string]int{
		"a": 1,
		"b": 2,
	}

	ensureBindingCapacity(bindings, "a", 2)
	bindings["a"] = 9

	require.Len(t, bindings, 2)
	require.Equal(t, 9, bindings["a"])
}

type openAIWSStateStoreTimeoutProbeCache struct {
	setHasDeadline    bool
	getHasDeadline    bool
	deleteHasDeadline bool
	setDeadlineDelta  time.Duration
	getDeadlineDelta  time.Duration
	delDeadlineDelta  time.Duration
}

func (c *openAIWSStateStoreTimeoutProbeCache) GetSessionAccountID(ctx context.Context, _ int64, _ string) (int64, error) {
	if deadline, ok := ctx.Deadline(); ok {
		c.getHasDeadline = true
		c.getDeadlineDelta = time.Until(deadline)
	}
	return 123, nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) SetSessionAccountID(ctx context.Context, _ int64, _ string, _ int64, _ time.Duration) error {
	if deadline, ok := ctx.Deadline(); ok {
		c.setHasDeadline = true
		c.setDeadlineDelta = time.Until(deadline)
	}
	return errors.New("set failed")
}

func (c *openAIWSStateStoreTimeoutProbeCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) DeleteSessionAccountID(ctx context.Context, _ int64, _ string) error {
	if deadline, ok := ctx.Deadline(); ok {
		c.deleteHasDeadline = true
		c.delDeadlineDelta = time.Until(deadline)
	}
	return nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) SetGrokVideoPendingBilling(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}
func (c *openAIWSStateStoreTimeoutProbeCache) GetGrokVideoPendingBilling(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}
func (c *openAIWSStateStoreTimeoutProbeCache) ClaimGrokVideoBilled(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) ReleaseGrokVideoBilled(_ context.Context, _ string) error {
	return nil
}

func (c *openAIWSStateStoreTimeoutProbeCache) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (c *openAIWSStateStoreTimeoutProbeCache) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", ErrReasoningContentNotFound
}

func TestOpenAIWSStateStore_RedisOpsUseShortTimeout(t *testing.T) {
	probe := &openAIWSStateStoreTimeoutProbeCache{}
	store := NewOpenAIWSStateStore(probe)
	ctx := context.Background()
	groupID := int64(5)

	err := store.BindResponseAccount(ctx, groupID, "resp_timeout_probe", 11, time.Minute)
	require.Error(t, err)

	accountID, getErr := store.GetResponseAccount(ctx, groupID, "resp_timeout_probe")
	require.NoError(t, getErr)
	require.Equal(t, int64(11), accountID, "本地缓存命中应优先返回已绑定账号")

	require.NoError(t, store.DeleteResponseAccount(ctx, groupID, "resp_timeout_probe"))

	require.True(t, probe.setHasDeadline, "SetSessionAccountID 应携带独立超时上下文")
	require.True(t, probe.deleteHasDeadline, "DeleteSessionAccountID 应携带独立超时上下文")
	require.False(t, probe.getHasDeadline, "GetSessionAccountID 本用例应由本地缓存命中，不触发 Redis 读取")
	require.Greater(t, probe.setDeadlineDelta, 2*time.Second)
	require.LessOrEqual(t, probe.setDeadlineDelta, 3*time.Second)
	require.Greater(t, probe.delDeadlineDelta, 2*time.Second)
	require.LessOrEqual(t, probe.delDeadlineDelta, 3*time.Second)

	probe2 := &openAIWSStateStoreTimeoutProbeCache{}
	store2 := NewOpenAIWSStateStore(probe2)
	accountID2, err2 := store2.GetResponseAccount(ctx, groupID, "resp_cache_only")
	require.NoError(t, err2)
	require.Equal(t, int64(123), accountID2)
	require.True(t, probe2.getHasDeadline, "GetSessionAccountID 在缓存未命中时应携带独立超时上下文")
	require.Greater(t, probe2.getDeadlineDelta, 2*time.Second)
	require.LessOrEqual(t, probe2.getDeadlineDelta, 3*time.Second)
}

func TestWithOpenAIWSStateStoreRedisTimeout_WithParentContext(t *testing.T) {
	ctx, cancel := withOpenAIWSStateStoreRedisTimeout(context.Background())
	defer cancel()
	require.NotNil(t, ctx)
	_, ok := ctx.Deadline()
	require.True(t, ok, "应附加短超时")
}
