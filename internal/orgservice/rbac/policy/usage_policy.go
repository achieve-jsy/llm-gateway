package policy

import (
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"gorm.io/gorm"
)

// UsagePolicy 用量/成本数据的访问策略
type UsagePolicy struct{}

// EffectiveScope 返回用量查询应使用的 scope 字符串和 scopeID
// 用于 GetDashboard 强制覆盖前端传入的 scope/id 参数
// 返回 (scope, id, restricted)
//
//	scope:      "company" | "studio" | "dept" | "user"
//	id:         对应 scope 的 ID
//	restricted: true 表示 actor 只能看自己范围，false 表示 admin 可由前端自由指定
func (p UsagePolicy) EffectiveScope(actor rbac.Actor, reqScope, reqID, userID string) (scope, id string, restricted bool) {
	if actor.IsGlobal {
		return reqScope, reqID, false
	}
	// 允许 studio_admin / dept_admin 以 scope=user 查看指定成员（如成员详情抽屉）
	// 安全保障：组织树层面已确保只能传入自己管辖范围内的成员 ID；
	// QueryScope 同时限制 summary 上期统计仍在 actor 权限范围内
	if reqScope == "user" && reqID != "" {
		return "user", reqID, true
	}
	if len(actor.StudioIDs) > 0 {
		// 允许 studio_admin 钻取到 dept scope（如看板中点击部门下钻到成员列表）
		// 数据安全由 QueryScope 的 studio_id IN ? 保障，无需再校验 HasDept
		if reqScope == "dept" && reqID != "" {
			return "dept", reqID, true
		}
		id = reqID
		if !actor.HasStudio(reqID) {
			id = actor.StudioIDs[0]
		}
		return "studio", id, true
	}
	if len(actor.DeptIDs) > 0 {
		id = reqID
		if !actor.HasDept(reqID) {
			id = actor.DeptIDs[0]
		}
		return "dept", id, true
	}
	return "user", userID, true
}

// QueryScope 返回 GORM Scope 函数，按角色限定用量数据可见范围：
//   - admin（IsGlobal）：不加任何过滤
//   - studio_admin：只能看自己管辖工作室的数据
//   - dept_admin：只能看自己管辖部门的数据
//   - user：只能看自己的数据
func (p UsagePolicy) QueryScope(actor rbac.Actor) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if actor.IsGlobal {
			return db
		}
		if len(actor.StudioIDs) > 0 {
			return db.Where("studio_id IN ?", actor.StudioIDs)
		}
		if len(actor.DeptIDs) > 0 {
			// dept_id 可能为空（历史数据）或 SSO EhrDeptID 或 departments.id UUID
			return db.Where(`dept_id IN ? OR dept_id IN (SELECT id::text FROM departments WHERE dept_id IN ?) OR user_id IN (SELECT u2.id::text FROM users u2 JOIN departments d2 ON d2.id = u2.dept_id WHERE d2.dept_id IN ?)`, actor.DeptIDs, actor.DeptIDs, actor.DeptIDs)
		}
		return db.Where("user_id = ?", actor.UserID)
	}
}

// StudioIDsFilter 返回用量服务参数中应使用的 studio_id 过滤列表
// nil 表示无限制（admin），非 nil 表示仅限这些工作室
func (p UsagePolicy) StudioIDsFilter(actor rbac.Actor) []string {
	if actor.IsGlobal {
		return nil
	}
	return actor.StudioIDs
}
