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

// orgCheckPasslist 列出不受 OrgCheck 拦截的只读非计费路径。
// 这些接口是无害查询，用户需要能访问模型列表才能了解当前配置状态。
var orgCheckPasslist = map[string]struct{}{
	"/v1/models":        {},
	"/v1/model/info":    {},
	"/v1/health/models": {},
}

// OrgCheckMiddleware 检查用户是否处于 pending_studio 状态
// 触发条件：dept_id 非空 AND studio_id 为空（已归集到部门但部门未映射工作室）
// 放置位置：AuthMiddleware 之后、Scheduler 之前（路由链 /v1/* 上）
// 性能影响：仅内存字段读取（O(1)），无 IO，对 P99 延迟无影响
//
// 注意：dept_id 在两条鉴权路径下语义不同，但 OrgCheck 只关心"是否非空"：
//   - API Key 路径：dept_id = api_keys.dept_id，存的是 departments.id（UUID 字符串）
//   - JWT 路径：dept_id = JWT claims.dept_id，存的是 SSO ehr_dept_id（varchar）
// 两者均以"非空"表示"用户已归属部门"，OrgCheck 逻辑不依赖具体值，语义安全。
func OrgCheckMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 只读非计费接口直接放行，不做工作室检查
		if _, skip := orgCheckPasslist[c.Request.URL.Path]; skip {
			c.Next()
			return
		}

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
