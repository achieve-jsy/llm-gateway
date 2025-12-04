// Package policy 封装 orgservice 各操作的数据权限策略。
// OrgPolicy 是无状态值对象，直接零值实例化即可使用，无需注入依赖。
package policy

import (
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"gorm.io/gorm"
)

// OrgPolicy 组织服务权限策略（无状态值对象）
type OrgPolicy struct{}

// RoleListScope 返回 GORM Scope 函数，将角色列表查询过滤至 actor 可见范围：
//   - admin（IsGlobal）：返回全部非 user 角色
//   - studio_admin：可见自己工作室的 studio_admin 条目 + 工作室内各部门的 dept_admin 条目
//   - dept_admin：仅可见自己管辖部门的 dept_admin 条目
//   - 无管理角色：不可见任何记录
func (p OrgPolicy) RoleListScope(actor rbac.Actor) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if actor.IsGlobal {
			return db.Where("ur.role != 'user'")
		}
		hasStudios := len(actor.StudioIDs) > 0
		hasDepts := len(actor.DeptIDs) > 0
		if !hasStudios && !hasDepts {
			return db.Where("1 = 0")
		}
		if hasStudios && hasDepts {
			return db.Where(
				"(ur.role = 'studio_admin' AND ur.scope_id::text IN ?) OR (ur.role = 'dept_admin' AND ur.scope_id::text IN ?)",
				actor.StudioIDs, actor.DeptIDs,
			)
		}
		if hasStudios {
			return db.Where("ur.role = 'studio_admin' AND ur.scope_id::text IN ?", actor.StudioIDs)
		}
		return db.Where("ur.role = 'dept_admin' AND ur.scope_id::text IN ?", actor.DeptIDs)
	}
}

// CanViewDeptMembers 判断 actor 是否有权查看指定部门的成员列表。
//   - admin：始终允许
//   - studio_admin：目标部门所属工作室（deptStudioID）在管辖范围内
//   - dept_admin：目标部门 UUID（deptID）在管辖范围内
func (p OrgPolicy) CanViewDeptMembers(actor rbac.Actor, deptID, deptStudioID string) bool {
	if actor.IsGlobal {
		return true
	}
	if deptStudioID != "" && actor.HasStudio(deptStudioID) {
		return true
	}
	return actor.HasDept(deptID)
}

// CanGrant 判断 actor 是否有权授予 targetRole。
//   - admin：可授任意角色
//   - studio_admin：仅可授 dept_admin
//   - 其他：无权
func (p OrgPolicy) CanGrant(actor rbac.Actor, targetRole string) bool {
	if actor.IsGlobal {
		return true
	}
	if len(actor.StudioIDs) > 0 {
		return targetRole == "dept_admin"
	}
	return false
}

// CanChangeMemberDept 判断 actor 是否有权将成员调入 targetStudioID 的部门，
// 同时要求成员当前所在工作室（currentStudioID）也在 actor 管辖范围内（跨工作室调整需 admin）。
func (p OrgPolicy) CanChangeMemberDept(actor rbac.Actor, targetStudioID, currentStudioID string) bool {
	if actor.IsGlobal {
		return true
	}
	if !actor.HasStudio(targetStudioID) {
		return false
	}
	if currentStudioID != "" && !actor.HasStudio(currentStudioID) {
		return false
	}
	return true
}
