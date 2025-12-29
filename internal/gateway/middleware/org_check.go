package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Context keys for org identity（与 auth.go 中的 c.Set("dept_id"/"studio_id") 一致）
const (
	CtxKeyDeptID   = "dept_id"
	CtxKeyStudioID = "studio_id"
)

// pendingStudioError 403 响应体（org_pending_studio）
var pendingStudioError = gin.H{
	"error": gin.H{
		"code":    "org_pending_studio",
		"message": "您所在部门尚未配置工作室归属，请联系管理员",
		"type":    "organization_error",
	},
}

// noDeptError 403 响应体（v0.7 FR-1.8：无部门用户拒绝调用）
var noDeptError = gin.H{
	"error": gin.H{
		"code":    "budget_not_configured",
		"message": "您尚未归属任何部门，无法使用服务，请联系管理员",
		"type":    "organization_error",
		"gate":    "identity",
	},
}

// orgCheckPasslist 列出不受 OrgCheck 拦截的只读非计费路径。
// 这些接口是无害查询，用户需要能访问模型列表才能了解当前配置状态。
var orgCheckPasslist = map[string]struct{}{
	"/v1/models":        {},
	"/v1/model/info":    {},
	"/v1/health/models": {},
}

// OrgCheckMiddleware 检查用户组织归属状态：
//  1. 无部门用户（dept_id 为空）→ 403 budget_not_configured（v0.7 FR-1.8）
//  2. pending_studio 状态（dept_id 非空 + studio_id 为空）→ 403 org_pending_studio
//
// 放置位置：AuthMiddleware 之后、Scheduler 之前（/v1/* 链上）。
func OrgCheckMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只读非计费接口直接放行
		if _, skip := orgCheckPasslist[c.Request.URL.Path]; skip {
			c.Next()
			return
		}

		deptID := c.GetString(CtxKeyDeptID)
		studioID := c.GetString(CtxKeyStudioID)

		// v0.7 FR-1.8：无部门用户拒绝调用（原逻辑是放行，现改为 403）
		if deptID == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, noDeptError)
			return
		}

		// 员工已归集到部门，但部门尚未映射工作室 → 拒绝调用
		if studioID == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, pendingStudioError)
			return
		}
		c.Next()
	}
}
