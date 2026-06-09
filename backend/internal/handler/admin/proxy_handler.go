package admin

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ProxyHandler handles admin proxy management
type ProxyHandler struct {
	adminService     service.AdminService
	autoProbe        *service.ProxyAutoProbeService
	activeUsageStats *service.ProxyActiveUsageTracker
}

// NewProxyHandler creates a new admin proxy handler
func NewProxyHandler(adminService service.AdminService, autoProbe *service.ProxyAutoProbeService, activeUsageStats *service.ProxyActiveUsageTracker) *ProxyHandler {
	return &ProxyHandler{
		adminService:     adminService,
		autoProbe:        autoProbe,
		activeUsageStats: activeUsageStats,
	}
}

// CreateProxyRequest represents create proxy request
type CreateProxyRequest struct {
	Name           string `json:"name" binding:"required"`
	Protocol       string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host           string `json:"host" binding:"required"`
	Port           int    `json:"port" binding:"required,min=1,max=65535"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	ExpiresAt      *int64 `json:"expires_at"`
	FallbackMode   string `json:"fallback_mode" binding:"omitempty,oneof=none proxy direct"`
	BackupProxyID  *int64 `json:"backup_proxy_id"`
	ExpiryWarnDays int    `json:"expiry_warn_days" binding:"omitempty,min=0"`
}

// UpdateProxyRequest represents update proxy request
type UpdateProxyRequest struct {
	Name           string `json:"name"`
	Protocol       string `json:"protocol" binding:"omitempty,oneof=http https socks5 socks5h"`
	Host           string `json:"host"`
	Port           int    `json:"port" binding:"omitempty,min=1,max=65535"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Status         string `json:"status" binding:"omitempty,oneof=active inactive"`
	ExpiresAt      *int64 `json:"expires_at"`
	FallbackMode   string `json:"fallback_mode" binding:"omitempty,oneof=none proxy direct"`
	BackupProxyID  *int64 `json:"backup_proxy_id"`
	ExpiryWarnDays int    `json:"expiry_warn_days" binding:"omitempty,min=0"`
}

type ProxyAutoProbeConfigRequest struct {
	Enabled            bool  `json:"enabled"`
	DefaultIntervalSec int   `json:"default_interval_sec"`
	RetryIntervalSec   int   `json:"retry_interval_sec"`
	StickyEnabled      *bool `json:"sticky_enabled"`
	StickyTTLSeconds   int   `json:"sticky_ttl_seconds"`
}

type CreateProxySubscriptionRequest struct {
	Name               string `json:"name" binding:"required"`
	SubscriptionURL    string `json:"subscription_url" binding:"required"`
	RefreshIntervalSec int    `json:"refresh_interval_sec"`
	TestURL            string `json:"test_url"`
}

type UpdateProxySubscriptionRequest struct {
	Name               string `json:"name"`
	SubscriptionURL    string `json:"subscription_url"`
	Status             string `json:"status" binding:"omitempty,oneof=active inactive"`
	RefreshIntervalSec int    `json:"refresh_interval_sec"`
	TestURL            string `json:"test_url"`
}

// List handles listing all proxies with pagination
// GET /api/v1/admin/proxies
func (h *ProxyHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	protocol := c.Query("protocol")
	status := c.Query("status")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort_by", "id")
	sortOrder := c.DefaultQuery("sort_order", "desc")
	// 标准化和验证 search 参数
	search = strings.TrimSpace(search)
	if len(search) > 100 {
		search = search[:100]
	}

	if isProxyRuntimeSort(sortBy) {
		proxies, total, err := h.listProxiesWithRuntimeSort(c.Request.Context(), page, pageSize, protocol, status, search, sortBy, sortOrder)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		out := make([]dto.AdminProxyWithAccountCount, 0, len(proxies))
		for i := range proxies {
			out = append(out, *dto.ProxyWithAccountCountFromServiceAdmin(&proxies[i]))
		}
		response.Paginated(c, out, total, page, pageSize)
		return
	}

	proxies, total, err := h.adminService.ListProxiesWithAccountCount(c.Request.Context(), page, pageSize, protocol, status, search, sortBy, sortOrder)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.applyActiveEgressAccountCounts(c.Request.Context(), proxies)

	out := make([]dto.AdminProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		out = append(out, *dto.ProxyWithAccountCountFromServiceAdmin(&proxies[i]))
	}
	response.Paginated(c, out, total, page, pageSize)
}

func isProxyRuntimeSort(sortBy string) bool {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "active_egress_account_count", "latency", "latency_ms":
		return true
	default:
		return false
	}
}

func (h *ProxyHandler) listProxiesWithRuntimeSort(ctx context.Context, page, pageSize int, protocol, status, search, sortBy, sortOrder string) ([]service.ProxyWithAccountCount, int64, error) {
	const batchSize = 1000
	var all []service.ProxyWithAccountCount
	var total int64

	for currentPage := 1; ; currentPage++ {
		batch, batchTotal, err := h.adminService.ListProxiesWithAccountCount(ctx, currentPage, batchSize, protocol, status, search, "id", "desc")
		if err != nil {
			return nil, 0, err
		}
		if currentPage == 1 {
			total = batchTotal
			if total == 0 {
				return []service.ProxyWithAccountCount{}, 0, nil
			}
			all = make([]service.ProxyWithAccountCount, 0, int(total))
		}
		all = append(all, batch...)
		if int64(len(all)) >= total || len(batch) == 0 {
			break
		}
	}

	h.applyActiveEgressAccountCounts(ctx, all)
	sortProxyRuntimeRows(all, sortBy, sortOrder)
	return paginateProxyRuntimeRows(all, page, pageSize), total, nil
}

func sortProxyRuntimeRows(rows []service.ProxyWithAccountCount, sortBy, sortOrder string) {
	sortKey := strings.ToLower(strings.TrimSpace(sortBy))
	order := strings.ToLower(strings.TrimSpace(sortOrder))
	ascending := order == "asc"

	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i]
		right := rows[j]
		switch sortKey {
		case "active_egress_account_count":
			if left.ActiveEgressAccountCount == right.ActiveEgressAccountCount {
				return left.ID > right.ID
			}
			if ascending {
				return left.ActiveEgressAccountCount < right.ActiveEgressAccountCount
			}
			return left.ActiveEgressAccountCount > right.ActiveEgressAccountCount
		case "latency", "latency_ms":
			leftMissing := left.LatencyMs == nil
			rightMissing := right.LatencyMs == nil
			if leftMissing || rightMissing {
				if leftMissing == rightMissing {
					return left.ID > right.ID
				}
				return !leftMissing
			}
			if *left.LatencyMs == *right.LatencyMs {
				return left.ID > right.ID
			}
			if ascending {
				return *left.LatencyMs < *right.LatencyMs
			}
			return *left.LatencyMs > *right.LatencyMs
		default:
			return left.ID > right.ID
		}
	})
}

func paginateProxyRuntimeRows(rows []service.ProxyWithAccountCount, page, pageSize int) []service.ProxyWithAccountCount {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	start := (page - 1) * pageSize
	if start >= len(rows) {
		return []service.ProxyWithAccountCount{}
	}
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

// GetAll handles getting all active proxies without pagination
// GET /api/v1/admin/proxies/all
// Optional query param: with_count=true to include account count per proxy
func (h *ProxyHandler) GetAll(c *gin.Context) {
	withCount := c.Query("with_count") == "true"

	if withCount {
		proxies, err := h.adminService.GetAllProxiesWithAccountCount(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		h.applyActiveEgressAccountCounts(c.Request.Context(), proxies)
		out := make([]dto.AdminProxyWithAccountCount, 0, len(proxies))
		for i := range proxies {
			out = append(out, *dto.ProxyWithAccountCountFromServiceAdmin(&proxies[i]))
		}
		response.Success(c, out)
		return
	}

	proxies, err := h.adminService.GetAllProxies(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminProxy, 0, len(proxies))
	for i := range proxies {
		out = append(out, *dto.ProxyFromServiceAdmin(&proxies[i]))
	}
	response.Success(c, out)
}

func (h *ProxyHandler) GetActiveUsage(c *gin.Context) {
	idsParam := strings.TrimSpace(c.Query("ids"))
	if idsParam == "" {
		response.Success(c, map[int64]int64{})
		return
	}
	parts := strings.Split(idsParam, ",")
	proxyIDs := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "Invalid proxy ID")
			return
		}
		proxyIDs = append(proxyIDs, id)
	}
	response.Success(c, h.activeUsageCounts(c.Request.Context(), proxyIDs))
}

func (h *ProxyHandler) GetSnapshots(c *gin.Context) {
	proxyIDs, err := parseProxyIDs(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if len(proxyIDs) == 0 {
		response.Success(c, []dto.AdminProxyWithAccountCount{})
		return
	}

	proxies, err := h.adminService.GetProxySnapshots(c.Request.Context(), proxyIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.applyActiveEgressAccountCounts(c.Request.Context(), proxies)

	out := make([]dto.AdminProxyWithAccountCount, 0, len(proxies))
	for i := range proxies {
		out = append(out, *dto.ProxyWithAccountCountFromServiceAdmin(&proxies[i]))
	}
	response.Success(c, out)
}

func (h *ProxyHandler) applyActiveEgressAccountCounts(ctx context.Context, proxies []service.ProxyWithAccountCount) {
	if len(proxies) == 0 {
		return
	}
	proxyIDs := make([]int64, 0, len(proxies))
	for i := range proxies {
		if proxies[i].ID > 0 {
			proxyIDs = append(proxyIDs, proxies[i].ID)
		}
	}
	counts := h.activeUsageCounts(ctx, proxyIDs)
	for i := range proxies {
		proxies[i].ActiveEgressAccountCount = counts[proxies[i].ID]
	}
}

func (h *ProxyHandler) activeUsageCounts(ctx context.Context, proxyIDs []int64) map[int64]int64 {
	out := make(map[int64]int64, len(proxyIDs))
	for _, proxyID := range proxyIDs {
		if proxyID > 0 {
			out[proxyID] = 0
		}
	}
	if h == nil || h.activeUsageStats == nil || len(out) == 0 {
		return out
	}
	counts := h.activeUsageStats.GetActiveAccountCounts(ctx, proxyIDs)
	for proxyID, count := range counts {
		out[proxyID] = count
	}
	return out
}

// GetAutoProbeConfig handles getting proxy auto probe config and runtime status.
// GET /api/v1/admin/proxies/auto-probe/config
func (h *ProxyHandler) GetAutoProbeConfig(c *gin.Context) {
	if h.autoProbe == nil {
		response.Error(c, http.StatusServiceUnavailable, "proxy auto probe service unavailable")
		return
	}
	response.Success(c, h.autoProbe.GetStatus())
}

// UpdateAutoProbeConfig handles updating proxy auto probe config.
// PUT /api/v1/admin/proxies/auto-probe/config
func (h *ProxyHandler) UpdateAutoProbeConfig(c *gin.Context) {
	if h.autoProbe == nil {
		response.Error(c, http.StatusServiceUnavailable, "proxy auto probe service unavailable")
		return
	}

	var req ProxyAutoProbeConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	status, err := h.autoProbe.UpdateConfig(c.Request.Context(), &service.ProxyAutoProbeUpdateInput{
		Enabled:            req.Enabled,
		DefaultIntervalSec: req.DefaultIntervalSec,
		RetryIntervalSec:   req.RetryIntervalSec,
		StickyEnabled:      req.StickyEnabled,
		StickyTTLSeconds:   req.StickyTTLSeconds,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, status)
}

func (h *ProxyHandler) ListClashSubscriptions(c *gin.Context) {
	subs, err := h.adminService.ListProxySubscriptions(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, subs)
}

func (h *ProxyHandler) CreateClashSubscription(c *gin.Context) {
	var req CreateProxySubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeAdminIdempotentJSON(c, "admin.proxies.clash_subscriptions.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		sub, proxies, err := h.adminService.CreateProxySubscription(ctx, &service.CreateProxySubscriptionInput{
			Name:               strings.TrimSpace(req.Name),
			SubscriptionURL:    strings.TrimSpace(req.SubscriptionURL),
			RefreshIntervalSec: req.RefreshIntervalSec,
			TestURL:            strings.TrimSpace(req.TestURL),
		})
		if err != nil {
			return nil, err
		}
		out := make([]dto.AdminProxy, 0, len(proxies))
		for i := range proxies {
			out = append(out, *dto.ProxyFromServiceAdmin(&proxies[i]))
		}
		var firstProxy *dto.AdminProxy
		if len(proxies) > 0 {
			firstProxy = dto.ProxyFromServiceAdmin(&proxies[0])
		}
		return gin.H{
			"subscription": sub,
			"proxy":        firstProxy,
			"proxies":      out,
		}, nil
	})
}

func (h *ProxyHandler) UpdateClashSubscription(c *gin.Context) {
	subID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	var req UpdateProxySubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	sub, err := h.adminService.UpdateProxySubscription(c.Request.Context(), subID, &service.UpdateProxySubscriptionInput{
		Name:               strings.TrimSpace(req.Name),
		SubscriptionURL:    strings.TrimSpace(req.SubscriptionURL),
		Status:             strings.TrimSpace(req.Status),
		RefreshIntervalSec: req.RefreshIntervalSec,
		TestURL:            strings.TrimSpace(req.TestURL),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sub)
}

func (h *ProxyHandler) DeleteClashSubscription(c *gin.Context) {
	subID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	if err := h.adminService.DeleteProxySubscription(c.Request.Context(), subID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Subscription deleted successfully"})
}

func (h *ProxyHandler) RefreshClashSubscription(c *gin.Context) {
	subID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	sub, err := h.adminService.RefreshProxySubscription(c.Request.Context(), subID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, sub)
}

func (h *ProxyHandler) GetClashSubscriptionStatus(c *gin.Context) {
	subID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	status, err := h.adminService.GetProxySubscriptionRuntimeStatus(c.Request.Context(), subID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// GetByID handles getting a proxy by ID
// GET /api/v1/admin/proxies/:id
func (h *ProxyHandler) GetByID(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	proxy, err := h.adminService.GetProxy(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ProxyFromServiceAdmin(proxy))
}

// Create handles creating a new proxy
// POST /api/v1/admin/proxies
func (h *ProxyHandler) Create(c *gin.Context) {
	var req CreateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	executeAdminIdempotentJSON(c, "admin.proxies.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		var expiresAt *time.Time
		if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
			t := time.Unix(*req.ExpiresAt, 0).UTC()
			expiresAt = &t
		}
		proxy, err := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
			Name:           strings.TrimSpace(req.Name),
			Protocol:       strings.TrimSpace(req.Protocol),
			Host:           strings.TrimSpace(req.Host),
			Port:           req.Port,
			Username:       strings.TrimSpace(req.Username),
			Password:       strings.TrimSpace(req.Password),
			ExpiresAt:      expiresAt,
			FallbackMode:   strings.TrimSpace(req.FallbackMode),
			BackupProxyID:  req.BackupProxyID,
			ExpiryWarnDays: req.ExpiryWarnDays,
		})
		if err != nil {
			return nil, err
		}
		return dto.ProxyFromServiceAdmin(proxy), nil
	})
}

// Update handles updating a proxy
// PUT /api/v1/admin/proxies/:id
func (h *ProxyHandler) Update(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	var req UpdateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt > 0 {
		t := time.Unix(*req.ExpiresAt, 0).UTC()
		expiresAt = &t
	}
	proxy, err := h.adminService.UpdateProxy(c.Request.Context(), proxyID, &service.UpdateProxyInput{
		Name:           strings.TrimSpace(req.Name),
		Protocol:       strings.TrimSpace(req.Protocol),
		Host:           strings.TrimSpace(req.Host),
		Port:           req.Port,
		Username:       strings.TrimSpace(req.Username),
		Password:       strings.TrimSpace(req.Password),
		Status:         strings.TrimSpace(req.Status),
		ExpiresAt:      expiresAt,
		FallbackMode:   strings.TrimSpace(req.FallbackMode),
		BackupProxyID:  req.BackupProxyID,
		ExpiryWarnDays: req.ExpiryWarnDays,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.ProxyFromServiceAdmin(proxy))
}

// Delete handles deleting a proxy
// DELETE /api/v1/admin/proxies/:id
func (h *ProxyHandler) Delete(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	err = h.adminService.DeleteProxy(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "Proxy deleted successfully"})
}

// BatchDelete handles batch deleting proxies
// POST /api/v1/admin/proxies/batch-delete
func (h *ProxyHandler) BatchDelete(c *gin.Context) {
	type BatchDeleteRequest struct {
		IDs []int64 `json:"ids" binding:"required,min=1"`
	}

	var req BatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	result, err := h.adminService.BatchDeleteProxies(c.Request.Context(), req.IDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// Test handles testing proxy connectivity
// POST /api/v1/admin/proxies/:id/test
func (h *ProxyHandler) Test(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	result, err := h.adminService.TestProxy(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// CheckQuality handles checking proxy quality across common AI targets.
// POST /api/v1/admin/proxies/:id/quality-check
func (h *ProxyHandler) CheckQuality(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	result, err := h.adminService.CheckProxyQuality(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// GetStats handles getting proxy statistics
// GET /api/v1/admin/proxies/:id/stats
func (h *ProxyHandler) GetStats(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	stats, err := h.adminService.GetProxyStats(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"total_accounts":  stats.TotalAccounts,
		"active_accounts": stats.ActiveAccounts,
		"total_requests":  stats.TotalRequests,
		"success_rate":    stats.SuccessRate,
		"average_latency": stats.AverageLatency,
	})
}

// GetProxyAccounts handles getting accounts using a proxy
// GET /api/v1/admin/proxies/:id/accounts
func (h *ProxyHandler) GetProxyAccounts(c *gin.Context) {
	proxyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}

	accounts, err := h.adminService.GetProxyAccounts(c.Request.Context(), proxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.ProxyAccountSummary, 0, len(accounts))
	for i := range accounts {
		out = append(out, *dto.ProxyAccountSummaryFromService(&accounts[i]))
	}
	response.Success(c, out)
}

// BatchCreateProxyItem represents a single proxy in batch create request
type BatchCreateProxyItem struct {
	Protocol string `json:"protocol" binding:"required,oneof=http https socks5 socks5h"`
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required,min=1,max=65535"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// BatchCreateRequest represents batch create proxies request
type BatchCreateRequest struct {
	Proxies []BatchCreateProxyItem `json:"proxies" binding:"required,min=1"`
}

// BatchCreate handles batch creating proxies
// POST /api/v1/admin/proxies/batch
func (h *ProxyHandler) BatchCreate(c *gin.Context) {
	var req BatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	created := 0
	skipped := 0

	for _, item := range req.Proxies {
		// Trim all string fields
		host := strings.TrimSpace(item.Host)
		protocol := strings.TrimSpace(item.Protocol)
		username := strings.TrimSpace(item.Username)
		password := strings.TrimSpace(item.Password)

		// Check for duplicates (same protocol, host, port, username, password)
		exists, err := h.adminService.CheckProxyExists(c.Request.Context(), protocol, host, item.Port, username, password)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		if exists {
			skipped++
			continue
		}

		// Create proxy with default name
		_, err = h.adminService.CreateProxy(c.Request.Context(), &service.CreateProxyInput{
			Name:     "default",
			Protocol: protocol,
			Host:     host,
			Port:     item.Port,
			Username: username,
			Password: password,
		})
		if err != nil {
			// If creation fails due to duplicate, count as skipped
			skipped++
			continue
		}

		created++
	}

	response.Success(c, gin.H{
		"created": created,
		"skipped": skipped,
	})
}
