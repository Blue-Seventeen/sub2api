package service

import (
	"context"
	"time"
)

const compatClaudeKimiToolRestoreTTL = stickySessionTTL

func CompatClaudeKimiToolRestoreTTL() time.Duration {
	return compatClaudeKimiToolRestoreTTL
}

type ClaudeKimiToolRestoreLedgerEntry struct {
	CallID        string    `json:"call_id"`
	ToolName      string    `json:"tool_name"`
	ArgumentsJSON string    `json:"arguments_json"`
	AccountID     int64     `json:"account_id"`
	CreatedAt     time.Time `json:"created_at"`
}

type ClaudeKimiToolRestoreLedger interface {
	GetClaudeKimiToolRestoreEntry(ctx context.Context, groupID int64, sessionHash, callID string) (*ClaudeKimiToolRestoreLedgerEntry, error)
	PutClaudeKimiToolRestoreEntry(ctx context.Context, groupID int64, sessionHash string, entry ClaudeKimiToolRestoreLedgerEntry, ttl time.Duration) error
}
