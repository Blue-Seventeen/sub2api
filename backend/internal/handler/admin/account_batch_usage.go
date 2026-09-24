package admin

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
)

// GetBatchUsage returns native account usage with best-effort per-account errors.
func (h *AccountHandler) GetBatchUsage(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids"`
		Force      bool    `json:"force"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || json.Unmarshal(body, &req) != nil {
		response.BadRequest(c, "Invalid batch usage request")
		return
	}
	if len(req.AccountIDs) == 0 || len(req.AccountIDs) > 100 {
		response.BadRequest(c, "account_ids must contain between 1 and 100 IDs")
		return
	}
	for _, id := range req.AccountIDs {
		if id <= 0 {
			response.BadRequest(c, "account_ids must contain only positive IDs")
			return
		}
	}
	if h.accountUsageService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account usage service unavailable")
		return
	}

	usage, failures, err := h.accountUsageService.GetUsageBatch(c.Request.Context(), req.AccountIDs, req.Force)
	if response.ErrorFrom(c, err) {
		return
	}
	if usage == nil {
		usage = make(map[int64]*service.UsageInfo)
	}
	for id, info := range usage {
		if info == nil {
			continue
		}
		// UsageInfo may be shared with the cache; redact only a response copy.
		redacted := *info
		redacted.Error = service.SanitizeUserVisibleErrorText(logredact.RedactText(info.Error))
		redacted.ForbiddenReason = service.SanitizeUserVisibleErrorText(logredact.RedactText(info.ForbiddenReason))
		usage[id] = &redacted
	}
	if failures == nil {
		failures = make(map[int64]string)
	}
	// The service has joined its workers before these response-only map updates.
	for id, message := range failures {
		failures[id] = service.SanitizeUserVisibleErrorText(logredact.RedactText(message))
	}
	response.Success(c, gin.H{"usage": usage, "errors": failures})
}
