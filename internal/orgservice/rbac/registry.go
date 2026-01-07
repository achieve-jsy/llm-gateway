package rbac

// Registry 权限策略配置表
// ──────────────────────────────────────────────────────────────────────────────
// 只改这里就能调整「谁能做什么」，不需要动任何 handler 代码。
//
// ScopeConstraint 说明：
//   ScopeAny     — 所有已登录用户
//   ScopeManage  — dept_admin / studio_admin / admin
//   ScopeStudio  — studio_admin（限自己工作室）或 admin（全局）
//   ScopeGlobal  — 仅 admin
// ──────────────────────────────────────────────────────────────────────────────
var Registry = map[Permission]PolicyEntry{
	// 组织结构查看：全员可见
	PermOrgTreeView: {ScopeAny},

	// 工作室增删：仅 admin（工作室是最高组织单元，修改影响面大）
	PermStudioCreate: {ScopeGlobal},
	PermStudioDelete: {ScopeGlobal},

	// 部门增删：studio_admin 可在自己工作室内操作
	PermDeptCreate: {ScopeStudio},
	PermDeptDelete: {ScopeStudio},

	// 员工花名册批量导入：仅 admin（会创建部门/工作室映射，影响全局）
	PermEmployeeImport: {ScopeGlobal},

	// 修改成员部门归属：studio_admin（限同工作室内调整）
	PermDeptChange: {ScopeStudio},

	// 恢复 SSO 归属：与 DeptChange 同级
	PermSSORevert: {ScopeStudio},

	// 查看部门成员列表：任意管理角色均可（只看自己范围）
	PermMemberView: {ScopeManage},

	// 角色授予/撤销：studio_admin 可授 dept_admin，admin 可授任意
	PermRoleGrant:  {ScopeStudio},
	PermRoleRevoke: {ScopeStudio},

	// 角色列表查看：任意管理角色可看（自己范围内）
	PermRoleList: {ScopeManage},

	// 部门-工作室映射编辑：仅 admin
	PermMappingEdit: {ScopeGlobal},

	// 用量看板（全公司）：仅 admin
	PermUsageViewAll: {ScopeGlobal},

	// 用量看板（管辖范围）：任意管理角色
	PermUsageViewScoped: {ScopeManage},

	// 模型组授权管理：仅 admin（设开放模式 + 授权工作室）
	PermEntitlementManage: {ScopeGlobal},

	// 下放/开通部门：studio_admin（自己工作室范围）或 admin（全局）
	// dept_admin 也可调用（但只能操作自己部门，由 Policy 层校验）
	PermEntitlementGrantDept: {ScopeManage},
}
