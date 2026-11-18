package admin

import (
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// adminBalanceViews 返回带最新派生余额字段（RealBalance / DisplayBalance）的用户副本。
//
// 服务层只在部分读取路径（仓储扫描、缓存回填）刷新这两个派生字段；管理端响应是
// 财务口径的出口，必须自己按 unified multiplier 重算一次，避免直接改写服务返回对象，
// 也让没有走仓储的调用点（如批量聚合、跨表联查）仍然给出正确的展示余额。
func adminBalanceViews(user *service.User) *service.User {
	if user == nil {
		return nil
	}
	out := *user
	out.RefreshBalanceViews()
	return &out
}

// adminUserResponse 对受限密钥使用字段白名单，避免完整管理员 DTO 新增字段时隐式扩大授权。
// 订阅及用量由各自的独立接口提供，用户资料接口不嵌入这些关联数据。
func adminUserResponse(c *gin.Context, user *service.User) any {
	principal := adminaccess.RestrictedFromContext(c.Request.Context())
	if principal == nil {
		return dto.UserFromServiceAdmin(adminBalanceViews(user))
	}
	if user == nil {
		return nil
	}
	out := gin.H{"id": user.ID}
	permissions := principal.Policy.GlobalPermissions
	if slices.Contains(permissions, "users.read") {
		out["email"] = user.Email
		out["username"] = user.Username
		out["role"] = user.Role
		out["status"] = user.Status
		out["concurrency"] = user.Concurrency
		out["rpm_limit"] = user.RPMLimit
		out["last_active_at"] = user.LastActiveAt
		out["created_at"] = user.CreatedAt
		out["updated_at"] = user.UpdatedAt
		out["deleted_at"] = user.DeletedAt
	}
	if slices.Contains(permissions, "users.balance.read") {
		out["balance"] = user.Balance
		out["frozen_balance"] = user.FrozenBalance
		out["total_recharged"] = user.TotalRecharged
	}
	if slices.Contains(permissions, "groups.read") {
		out["allowed_groups"] = user.AllowedGroups
		out["restrict_public_groups"] = user.RestrictPublicGroups
		out["group_rates"] = user.GroupRates
	}
	return out
}
