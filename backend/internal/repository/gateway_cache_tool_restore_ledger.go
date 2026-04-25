package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const compatClaudeKimiToolRestoreLedgerPrefix = "compat:claude_kimi_tool_ledger:"

func buildClaudeKimiToolRestoreLedgerKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", compatClaudeKimiToolRestoreLedgerPrefix, groupID, strings.TrimSpace(sessionHash))
}

func (c *gatewayCache) GetClaudeKimiToolRestoreEntry(ctx context.Context, groupID int64, sessionHash, callID string) (*service.ClaudeKimiToolRestoreLedgerEntry, error) {
	if c == nil || c.rdb == nil {
		return nil, nil
	}
	sessionHash = strings.TrimSpace(sessionHash)
	callID = strings.TrimSpace(callID)
	if sessionHash == "" || callID == "" {
		return nil, nil
	}

	raw, err := c.rdb.HGet(ctx, buildClaudeKimiToolRestoreLedgerKey(groupID, sessionHash), callID).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var entry service.ClaudeKimiToolRestoreLedgerEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (c *gatewayCache) PutClaudeKimiToolRestoreEntry(ctx context.Context, groupID int64, sessionHash string, entry service.ClaudeKimiToolRestoreLedgerEntry, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	sessionHash = strings.TrimSpace(sessionHash)
	entry.CallID = strings.TrimSpace(entry.CallID)
	entry.ToolName = strings.TrimSpace(entry.ToolName)
	entry.ArgumentsJSON = strings.TrimSpace(entry.ArgumentsJSON)
	if sessionHash == "" || entry.CallID == "" {
		return nil
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	if ttl <= 0 {
		ttl = service.CompatClaudeKimiToolRestoreTTL()
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	key := buildClaudeKimiToolRestoreLedgerKey(groupID, sessionHash)
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, entry.CallID, payload)
	pipe.Expire(ctx, key, ttl)
	_, err = pipe.Exec(ctx)
	return err
}
