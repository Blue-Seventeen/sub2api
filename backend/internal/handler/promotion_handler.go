package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type PromotionHandler struct {
	promotionService *service.PromotionService
}

func NewPromotionHandler(promotionService *service.PromotionService) *PromotionHandler {
	return &PromotionHandler{promotionService: promotionService}
}

func (h *PromotionHandler) PreviewReferrer(c *gin.Context) {
	inviteCode := strings.TrimSpace(c.Param("invite_code"))
	referrer, err := h.promotionService.PreviewReferrer(c.Request.Context(), inviteCode)
	if err != nil {
		response.Success(c, gin.H{"valid": false, "invite_code": inviteCode})
		return
	}
	response.Success(c, gin.H{
		"valid":       true,
		"invite_code": inviteCode,
		"referrer": gin.H{
			"user_id":      referrer.UserID,
			"masked_email": referrer.MaskedEmail,
			"level_name":   referrer.LevelName,
		},
	})
}

func (h *PromotionHandler) BindReferrer(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req struct {
		InviteCode string `json:"invite_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	user, err := h.promotionService.BindReferrer(c.Request.Context(), subject.UserID, req.InviteCode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"user_id":        user.UserID,
		"parent_user_id": user.ParentUserID,
		"bound_at":       user.BoundAt,
		"invite_code":    user.InviteCode,
	})
}

func (h *PromotionHandler) GetOverview(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	overview, err := h.promotionService.GetMyOverview(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, overview)
}

func (h *PromotionHandler) ListTeam(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.promotionService.ListMyTeam(c.Request.Context(), subject.UserID, service.PromotionTeamFilter{
		Page:      page,
		PageSize:  pageSize,
		Keyword:   c.Query("keyword"),
		Status:    c.DefaultQuery("status", "all"),
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	safeItems := make([]gin.H, 0, len(items))
	for _, item := range items {
		safeItems = append(safeItems, gin.H{
			"username":           item.Username,
			"masked_email":       item.MaskedEmail,
			"relation_depth":     item.RelationDepth,
			"level_name":         item.LevelName,
			"activated":          item.Activated,
			"today_contribution": item.TodayContribution,
			"total_contribution": item.TotalContribution,
			"joined_at":          item.JoinedAt,
			"activated_at":       item.ActivatedAt,
		})
	}
	response.Paginated(c, safeItems, total, page, pageSize)
}

func (h *PromotionHandler) ListEarnings(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.promotionService.ListMyEarnings(c.Request.Context(), subject.UserID, service.PromotionCommissionFilter{
		Page:     page,
		PageSize: pageSize,
		Keyword:  c.Query("keyword"),
		Type:     c.Query("type"),
		Status:   c.Query("status"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *PromotionHandler) ListScripts(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	items, err := h.promotionService.ListMyScripts(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *PromotionHandler) MarkScriptUsed(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid script id")
		return
	}
	if err := h.promotionService.TrackScriptUse(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "used_at": time.Now().UTC()})
}
