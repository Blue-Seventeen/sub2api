package service

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

const claudeKimiToolRestoreContextKey = "claude_kimi_tool_restore_context"

type claudeKimiToolRestoreContextRequestKey struct{}

type ClaudeKimiToolRestoreContext struct {
	Enabled         bool
	GroupID         int64
	SessionHash     string
	AccountID       int64
	Platform        string
	ClientProfile   ClientProfile
	InboundProtocol InboundProtocol
	ToolNames       []string
}

func (c ClaudeKimiToolRestoreContext) Normalize() ClaudeKimiToolRestoreContext {
	out := c
	out.SessionHash = strings.TrimSpace(out.SessionHash)
	out.Platform = strings.TrimSpace(strings.ToLower(out.Platform))
	out.ClientProfile = out.ClientProfile.Normalize()
	out.InboundProtocol = out.InboundProtocol.Normalize()
	if len(out.ToolNames) > 0 {
		seen := make(map[string]struct{}, len(out.ToolNames))
		normalized := make([]string, 0, len(out.ToolNames))
		for _, name := range out.ToolNames {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			normalized = append(normalized, name)
		}
		out.ToolNames = normalized
	}
	return out
}

func (c ClaudeKimiToolRestoreContext) HasToolName(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false
	}
	for _, item := range c.ToolNames {
		if strings.EqualFold(strings.TrimSpace(item), name) {
			return true
		}
	}
	return false
}

func WithClaudeKimiToolRestoreContext(ctx context.Context, value ClaudeKimiToolRestoreContext) context.Context {
	return context.WithValue(ctx, claudeKimiToolRestoreContextRequestKey{}, value.Normalize())
}

func ClaudeKimiToolRestoreContextFromContext(ctx context.Context) (ClaudeKimiToolRestoreContext, bool) {
	if ctx == nil {
		return ClaudeKimiToolRestoreContext{}, false
	}
	raw := ctx.Value(claudeKimiToolRestoreContextRequestKey{})
	value, ok := raw.(ClaudeKimiToolRestoreContext)
	if !ok {
		return ClaudeKimiToolRestoreContext{}, false
	}
	return value.Normalize(), true
}

func SetClaudeKimiToolRestoreContext(c *gin.Context, value ClaudeKimiToolRestoreContext) {
	if c == nil {
		return
	}
	normalized := value.Normalize()
	c.Set(claudeKimiToolRestoreContextKey, normalized)
	if c.Request != nil {
		c.Request = c.Request.WithContext(WithClaudeKimiToolRestoreContext(c.Request.Context(), normalized))
	}
}

func GetClaudeKimiToolRestoreContext(c *gin.Context) ClaudeKimiToolRestoreContext {
	if c == nil {
		return ClaudeKimiToolRestoreContext{}
	}
	if raw, ok := c.Get(claudeKimiToolRestoreContextKey); ok {
		if value, ok := raw.(ClaudeKimiToolRestoreContext); ok {
			return value.Normalize()
		}
	}
	if c.Request != nil {
		if value, ok := ClaudeKimiToolRestoreContextFromContext(c.Request.Context()); ok {
			return value
		}
	}
	return ClaudeKimiToolRestoreContext{}
}
