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

	// ── v0.7 模型组授权 ──────────────────────────────────────────────────────
	// 设置开放模式（public/controlled）：仅平台 admin
	PermGroupOpenModeSet: {ScopeGlobal},
	// 下放/撤销受控组授权：studio_admin（在自己工作室范围内）或 admin（全局）
	PermGroupEntitleGrant:  {ScopeStudio},
	PermGroupEntitleRevoke: {ScopeStudio},

	// ── v0.7 预算管理 ────────────────────────────────────────────────────────
	// 查看全公司预算：仅 admin
	PermBudgetViewAll: {ScopeGlobal},
	// 查看自己管辖范围预算：任意管理角色
	PermBudgetViewScoped: {ScopeManage},
	// 设置预算（按管辖域过滤）：任意管理角色（service 层做数据范围校验）
	PermBudgetSet: {ScopeManage},

	// ── v0.7 Service Key ────────────────────────────────────────────────────
	// 创建/吊销 Service Key：任意管理角色（service 层校验只能操作自己部门）
	PermServiceKeyCreate: {ScopeManage},
	PermServiceKeyRevoke: {ScopeManage},

	// ── v0.8 审计日志 ────────────────────────────────────────────────────────
	// 查看全公司审计：仅 admin
	PermAuditViewAll: {ScopeGlobal},
	// 查看自己管辖范围审计：任意管理角色
	PermAuditViewScoped: {ScopeManage},

	// ── v0.7 扩容审批 ────────────────────────────────────────────────────────
	// 发起扩容申请：任意管理角色
	PermExpansionRequest: {ScopeManage},
	// 审批扩容：studio_admin 或 admin
	PermExpansionApprove: {ScopeStudio},

	// 模型组授权管理：仅 admin（设开放模式 + 授权工作室）
	PermEntitlementManage: {ScopeGlobal},

	// 下放/开通部门：studio_admin（自己工作室范围）或 admin（全局）
	PermEntitlementGrantDept: {ScopeManage},

	// ── feat/key-approval ──────────────────────────────────────────────────
	// 审批 Key 申请：任意管理角色（service 层按 deptIDs 范围校验）
	PermKeyRequestApprove: {ScopeManage},
	// 设置部门 Key 创建策略：任意管理角色（service 层按 deptIDs 过滤）
	PermKeyRequestSetPolicy: {ScopeManage},
}
