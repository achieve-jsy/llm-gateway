package rbac

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

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
// admin / studio_admin / dept_admin 权限一律以服务端 user_roles 表（含 users/dev_users 遗留 role 兜底）为准，
// 绝不信任 JWT 中客户端自证的 user_role claim。
// studio_admin 的管辖部门列表在此处预展开，handler/policy 层无需二次查询。
func (ch *Checker) BuildAndInjectActor(c *gin.Context) {
	a := ch.buildActor(c)
	SetActor(c, a)
}

func (ch *Checker) buildActor(c *gin.Context) Actor {
	userID := c.GetString("user_id")

	rows, err := ch.roleRepo.GetByUserID(userID)
	if err != nil || len(rows) == 0 {
		return Actor{UserID: userID}
	}

	actor := Actor{UserID: userID}
	for _, r := range rows {
		switch r.Role {
		case "admin":
			// 平台权限不应吞掉同一用户额外持有的部门/工作室职责。
			// 例如平台负责人兼任部门负责人时，仍需处理该部门的部门级审批待办。
			actor.IsGlobal = true
			actor.Roles = appendUniq(actor.Roles, "admin")
		case "studio_admin":
			actor.StudioIDs = appendUniq(actor.StudioIDs, r.ScopeID)
			actor.Roles = appendUniq(actor.Roles, "studio_admin")
		case "dept_admin":
			actor.DeptIDs = appendUniq(actor.DeptIDs, r.ScopeID)
			actor.DeptAdminIDs = appendUniq(actor.DeptAdminIDs, r.ScopeID)
			actor.ApprovalDeptIDs = appendUniq(actor.ApprovalDeptIDs, r.ScopeID)
			actor.Roles = appendUniq(actor.Roles, "dept_admin")
		}
	}

	// studio_admin 管辖工作室下的所有部门 SSO dept_id 预展开到 DeptIDs，
	// actor.DeptIDs 统一存储 SSO dept_id，与 usage_daily.dept_id / user_roles.scope_id 一致
	if len(actor.StudioIDs) > 0 {
		requestCtx := context.Background()
		if c.Request != nil {
			requestCtx = c.Request.Context()
		}
		var scopeErrors []string
		studioDeptIDs, resolveErr := ch.userOrgRepo.ListDeptIDsByStudioIDs(actor.StudioIDs)
		if resolveErr != nil {
			slog.WarnContext(requestCtx, "failed to expand studio departments", "user_id", userID, "studio_ids", actor.StudioIDs, "error", resolveErr)
			scopeErrors = append(scopeErrors, "studio_departments")
		} else {
			for _, id := range studioDeptIDs {
				actor.DeptIDs = appendUniq(actor.DeptIDs, id)
			}
		}

		approvalDeptIDs, approvalErr := ch.userOrgRepo.ListSSODeptIDsByStudioIDs(actor.StudioIDs)
		if approvalErr != nil {
			slog.WarnContext(requestCtx, "failed to expand approval departments", "user_id", userID, "studio_ids", actor.StudioIDs, "error", approvalErr)
			scopeErrors = append(scopeErrors, "approval_departments")
		} else {
			for _, id := range approvalDeptIDs {
				actor.ApprovalDeptIDs = appendUniq(actor.ApprovalDeptIDs, id)
			}
		}
		actor.ScopeError = strings.Join(scopeErrors, ",")
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

		if entry.Constraint != ScopeAny && RejectScopeUnavailable(c, actor) {
			return
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

// RejectScopeUnavailable 统一阻断依赖组织范围的操作。
// 工作室负责人范围由多个组织表共同展开；任一展开失败时不能使用残缺范围做审批或管理决策。
func RejectScopeUnavailable(c *gin.Context, actor Actor) bool {
	if actor.ScopeError == "" {
		return false
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
		"code":    "org.scope_unavailable",
		"message": "审批范围暂时不可用，请稍后重试",
	})
	return true
}

func appendUniq(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}
