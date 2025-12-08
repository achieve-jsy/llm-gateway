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

// OrgCheckMiddleware 检查用户是否处于 pending_studio 状态
// 触发条件：dept_id 非空 AND studio_id 为空（已归集到部门但部门未映射工作室）
// 放置位置：AuthMiddleware 之后、Scheduler 之前（路由链 /v1/* 上）
// 性能影响：仅内存字段读取（O(1)），无 IO，对 P99 延迟无影响
func OrgCheckMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		deptID := c.GetString(CtxKeyDeptID)
		studioID := c.GetString(CtxKeyStudioID)

		// 员工已归集到部门，但部门尚未映射工作室 → 拒绝调用
		if deptID != "" && studioID == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, pendingStudioError)
			return
		}
		c.Next()
	}
}
