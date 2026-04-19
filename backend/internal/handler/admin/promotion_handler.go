package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type PromotionHandler struct {
	promotionService *service.PromotionService
}

func NewPromotionHandler(promotionService *service.PromotionService) *PromotionHandler {
	return &PromotionHandler{promotionService: promotionService}
}

func (h *PromotionHandler) GetDashboard(c *gin.Context) {
	data, err := h.promotionService.GetAdminDashboard(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, data)
}

func (h *PromotionHandler) ListRelations(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.promotionService.ListAdminRelations(c.Request.Context(), c.Query("keyword"), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *PromotionHandler) GetRelationChain(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user id")
		return
	}
	chain, err := h.promotionService.GetAdminRelationChain(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, chain)
}

func (h *PromotionHandler) ListDownlines(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user id")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.promotionService.ListAdminDownlines(c.Request.Context(), userID, service.PromotionTeamFilter{
		Page:      page,
		PageSize:  pageSize,
		Status:    c.DefaultQuery("status", "all"),
		SortBy:    c.Query("sort_by"),
		SortOrder: c.Query("sort_order"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *PromotionHandler) RemoveDirectDownline(c *gin.Context) {
	parentUserID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || parentUserID <= 0 {
		response.BadRequest(c, "Invalid user id")
		return
	}
	downlineUserID, err := strconv.ParseInt(c.Param("downline_user_id"), 10, 64)
	if err != nil || downlineUserID <= 0 {
		response.BadRequest(c, "Invalid downline user id")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := h.promotionService.RemoveAdminDirectDownline(c.Request.Context(), parentUserID, downlineUserID, req.Note); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"user_id":          parentUserID,
		"downline_user_id": downlineUserID,
		"removed":          true,
	})
}

func (h *PromotionHandler) BindParent(c *gin.Context) {
	var req struct {
		UserID       int64  `json:"user_id" binding:"required,gt=0"`
		ParentUserID int64  `json:"parent_user_id" binding:"required,gt=0"`
		Note         string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	user, err := h.promotionService.BindAdminParent(c.Request.Context(), req.UserID, req.ParentUserID, req.Note)
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

func (h *PromotionHandler) RemoveParent(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user id")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	user, err := h.promotionService.RemoveAdminParent(c.Request.Context(), userID, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"user_id":        user.UserID,
		"parent_user_id": user.ParentUserID,
		"removed":        true,
		"bound_at":       user.BoundAt,
	})
}

func (h *PromotionHandler) ListCommissions(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filter := service.PromotionCommissionAdminFilter{
		Page:     page,
		PageSize: pageSize,
		Keyword:  c.Query("keyword"),
		Type:     c.Query("type"),
		Status:   c.Query("status"),
	}
	if start := strings.TrimSpace(c.Query("date_from")); start != "" {
		if parsed, err := time.Parse("2006-01-02", start); err == nil {
			filter.DateFrom = &parsed
		}
	}
	if end := strings.TrimSpace(c.Query("date_to")); end != "" {
		if parsed, err := time.Parse("2006-01-02", end); err == nil {
			filter.DateTo = &parsed
		}
	}
	items, total, err := h.promotionService.ListAdminCommissions(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *PromotionHandler) ManualGrantCommission(c *gin.Context) {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	var req struct {
		UserID int64   `json:"user_id" binding:"required,gt=0"`
		Amount float64 `json:"amount" binding:"required"`
		Note   string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	operatorID := subject.UserID
	record, err := h.promotionService.ManualGrantCommission(c.Request.Context(), &operatorID, service.PromotionCommissionRecord{
		BeneficiaryUserID: req.UserID,
		CommissionType:    service.PromotionCommissionTypeAdjustment,
		RelationDepth:     0,
		BaseAmount:        0,
		Amount:            req.Amount,
		Note:              req.Note,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *PromotionHandler) UpdateCommission(c *gin.Context) {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid commission id")
		return
	}
	var req struct {
		Amount float64 `json:"amount" binding:"required"`
		Note   string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	operatorID := subject.UserID
	record, err := h.promotionService.UpdateCommission(c.Request.Context(), id, &operatorID, req.Amount, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *PromotionHandler) SettleCommission(c *gin.Context) {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid commission id")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	operatorID := subject.UserID
	record, err := h.promotionService.SettleCommission(c.Request.Context(), id, &operatorID, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *PromotionHandler) BatchSettleCommissions(c *gin.Context) {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	var req struct {
		IDs  []int64 `json:"ids" binding:"required"`
		Note string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	operatorID := subject.UserID
	summary, err := h.promotionService.BatchSettleCommissions(c.Request.Context(), req.IDs, &operatorID, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"settled_count": summary.SettledCount,
		"total_amount":  summary.TotalAmount,
	})
}

func (h *PromotionHandler) CancelCommission(c *gin.Context) {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid commission id")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&req)
	operatorID := subject.UserID
	record, err := h.promotionService.CancelCommission(c.Request.Context(), id, &operatorID, req.Note)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *PromotionHandler) GetConfig(c *gin.Context) {
	payload, tz, err := h.promotionService.GetAdminConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"settings":           payload.Settings,
		"levels":             payload.Levels,
		"effective_timezone": tz,
	})
}

func (h *PromotionHandler) UpdateConfig(c *gin.Context) {
	var req struct {
		Settings service.PromotionSettings      `json:"settings"`
		Levels   []service.PromotionLevelConfig `json:"levels"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	payload, tz, err := h.promotionService.UpdateAdminConfig(c.Request.Context(), service.PromotionConfigPayload{
		Settings: req.Settings,
		Levels:   req.Levels,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"settings":           payload.Settings,
		"levels":             payload.Levels,
		"effective_timezone": tz,
	})
}

func (h *PromotionHandler) ListScripts(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.promotionService.ListAdminScripts(c.Request.Context(), service.PromotionScriptFilter{
		Page:     page,
		PageSize: pageSize,
		Keyword:  c.Query("keyword"),
		Category: c.Query("category"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *PromotionHandler) CreateScript(c *gin.Context) {
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	var req service.PromotionScript
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	operatorID := subject.UserID
	item, err := h.promotionService.CreateAdminScript(c.Request.Context(), &operatorID, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *PromotionHandler) UpdateScript(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid script id")
		return
	}
	var req service.PromotionScript
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	req.ID = id
	item, err := h.promotionService.UpdateAdminScript(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *PromotionHandler) DeleteScript(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid script id")
		return
	}
	if err := h.promotionService.DeleteAdminScript(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"id": id, "deleted": true})
}
