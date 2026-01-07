// Package rbac 提供组织服务的权限判断层。
//
// 设计目标：把"谁能做什么"的配置集中到 registry.go 一处，
// handler 只需一行 if !h.rbac.Require(c, rbac.PermXxx) { return }，
// 无需在每个 handler 里重复写「查 DB → 判角色 → 判范围」的模式。
package rbac

// Permission 操作权限标识符（类型安全，IDE 可自动补全）
type Permission string

const (
	// ── 组织结构 ────────────────────────────────────────────────────────────
	PermOrgTreeView    Permission = "org:tree:view"      // 查看组织树
	PermStudioCreate   Permission = "org:studio:create"  // 新建工作室
	PermStudioDelete   Permission = "org:studio:delete"  // 删除工作室
	PermDeptCreate     Permission = "org:dept:create"    // 新建部门
	PermDeptDelete     Permission = "org:dept:delete"    // 删除部门
	PermEmployeeImport Permission = "org:employee:import" // 批量导入花名册
	PermDeptChange     Permission = "org:dept:change"    // 修改用户所属部门
	PermSSORevert      Permission = "org:sso:revert"     // 恢复跟随 SSO 归属
	PermMemberView     Permission = "org:member:view"    // 查看部门成员列表

	// ── 角色管理 ────────────────────────────────────────────────────────────
	PermRoleGrant  Permission = "org:role:grant"  // 授予角色
	PermRoleRevoke Permission = "org:role:revoke" // 撤销角色
	PermRoleList   Permission = "org:role:list"   // 查看角色列表

	// ── 映射管理 ────────────────────────────────────────────────────────────
	PermMappingEdit Permission = "org:mapping:edit" // 编辑部门-工作室映射

	// ── 用量看板 ────────────────────────────────────────────────────────────
	PermUsageViewAll    Permission = "usage:view:all"    // 查看全公司用量
	PermUsageViewScoped Permission = "usage:view:scoped" // 查看自己管辖范围用量

	// ── 模型组授权 ──────────────────────────────────────────────────────────────
	PermEntitlementManage   Permission = "entitlement:manage"    // 平台管理员：设开放模式 + 授权工作室
	PermEntitlementGrantDept Permission = "entitlement:grant_dept" // 工作室负责人：下放/开通部门
)

// ScopeConstraint 权限的范围约束级别（从宽到严）
type ScopeConstraint int

const (
	// ScopeAny：所有已登录用户均可，无需任何组织角色
	ScopeAny ScopeConstraint = iota

	// ScopeManage：需持有任意管理角色（dept_admin / studio_admin / admin）
	ScopeManage

	// ScopeStudio：需是 studio_admin（在自己工作室范围内操作）或 admin（无范围限制）
	ScopeStudio

	// ScopeGlobal：仅 admin，无范围限制
	ScopeGlobal
)

// PolicyEntry 一条权限策略
type PolicyEntry struct {
	Constraint ScopeConstraint
}
