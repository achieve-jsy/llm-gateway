package rbac

import (
	"net/http"

	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
)

// Checker 持有 DB 访问能力，供 Guard middleware 在请求入口构建 Actor 并注入 gin.Context。
// handler 层不直接使用 Checker；改为通过 rbac.GetActor(c) 取出已注入的 Actor。
type Checker struct {
	roleRepo    *repository.RoleRepo
	userOrgRepo *repository.UserOrgRepo
}

// NewChecker 创建 Checker
func NewChecker(roleRepo *repository.RoleRepo, userOrgRepo *repository.UserOrgRepo) *Checker {
	return &Checker{roleRepo: roleRepo, userOrgRepo: userOrgRepo}
}

// BuildAndInjectActor 根据当前请求上下文构建 Actor 并注入 gin.Context，供 Guard middleware 调用。
// 若 JWT claim 中 user_role=admin，直接返回全局权限 Actor（不查 DB）。
// studio_admin 的管辖部门列表在此处预展开，handler/policy 层无需二次查询。
func (ch *Checker) BuildAndInjectActor(c *gin.Context) {
	a := ch.buildActor(c)
	SetActor(c, a)
}

func (ch *Checker) buildActor(c *gin.Context) Actor {
	userID := c.GetString("user_id")

	// JWT claim admin → 全局权限
	if c.GetString("user_role") == "admin" {
		return Actor{UserID: userID, IsGlobal: true}
	}

	rows, err := ch.roleRepo.GetByUserID(userID)
	if err != nil || len(rows) == 0 {
		return Actor{UserID: userID}
	}

	actor := Actor{UserID: userID}
	for _, r := range rows {
		switch r.Role {
		case "admin":
			// user_roles 中也存在 admin 记录 → 全局权限
			return Actor{UserID: userID, IsGlobal: true}
		case "studio_admin":
			actor.StudioIDs = appendUniq(actor.StudioIDs, r.ScopeID)
		case "dept_admin":
			actor.DeptIDs = appendUniq(actor.DeptIDs, r.ScopeID)
		}
	}

	// studio_admin 管辖工作室下的所有部门 SSO dept_id 预展开到 DeptIDs，
	// actor.DeptIDs 统一存储 SSO dept_id，与 usage_daily.dept_id / user_roles.scope_id 一致
	if len(actor.StudioIDs) > 0 {
		studioDeptIDs, _ := ch.userOrgRepo.ListDeptIDsByStudioIDs(actor.StudioIDs)
		for _, id := range studioDeptIDs {
			actor.DeptIDs = appendUniq(actor.DeptIDs, id)
		}
	}

	return actor
}

// Guard 返回一个 gin.HandlerFunc，作为路由级中间件：
//  1. 构建 Actor 并注入 gin.Context（同一请求只构建一次）
//  2. 检查 perm 的 ScopeConstraint 是否满足；不满足则 AbortWithStatusJSON(403)
//
// 用法（router.go）：
//
//	orgGroup.POST("/studios", rbac.Guard(rbac.PermStudioCreate), orgHandler.CreateStudio)
func (ch *Checker) Guard(perm Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		ch.BuildAndInjectActor(c)
		actor := GetActor(c)

		entry, ok := Registry[perm]
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"code":    "rbac.unknown_perm",
				"message": "未注册的权限标识: " + string(perm),
			})
			return
		}

		var allowed bool
		switch entry.Constraint {
		case ScopeAny:
			allowed = true
		case ScopeManage:
			allowed = actor.IsManager()
		case ScopeStudio:
			allowed = actor.IsGlobal || len(actor.StudioIDs) > 0
		case ScopeGlobal:
			allowed = actor.IsGlobal
		}

		if !allowed {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "org.forbidden",
				"message": "权限不足，当前操作需要更高级别角色",
			})
			return
		}
		c.Next()
	}
}

func appendUniq(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}
