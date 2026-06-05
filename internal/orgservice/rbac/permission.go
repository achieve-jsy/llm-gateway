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
	PermOrgTreeView    Permission = "org:tree:view"       // 查看组织树
	PermStudioCreate   Permission = "org:studio:create"   // 新建工作室
	PermStudioDelete   Permission = "org:studio:delete"   // 删除工作室
	PermDeptCreate     Permission = "org:dept:create"     // 新建部门
	PermDeptDelete     Permission = "org:dept:delete"     // 删除部门
	PermEmployeeImport Permission = "org:employee:import" // 批量导入花名册
	PermDeptChange     Permission = "org:dept:change"     // 修改用户所属部门
	PermSSORevert      Permission = "org:sso:revert"      // 恢复跟随 SSO 归属
	PermMemberView     Permission = "org:member:view"     // 查看部门成员列表

	// ── 角色管理 ────────────────────────────────────────────────────────────
	PermRoleGrant  Permission = "org:role:grant"  // 授予角色
	PermRoleRevoke Permission = "org:role:revoke" // 撤销角色
	PermRoleList   Permission = "org:role:list"   // 查看角色列表

	// ── 映射管理 ────────────────────────────────────────────────────────────
	PermMappingEdit Permission = "org:mapping:edit" // 编辑部门-工作室映射

	// ── 用量看板 ────────────────────────────────────────────────────────────
	PermUsageViewAll    Permission = "usage:view:all"    // 查看全公司用量
	PermUsageViewScoped Permission = "usage:view:scoped" // 查看自己管辖范围用量

	// ── v0.7 模型组授权 ──────────────────────────────────────────────────────
	PermGroupOpenModeSet   Permission = "group:open_mode:set"  // 设置组开放模式（仅平台 admin）
	PermGroupEntitleGrant  Permission = "group:entitle:grant"  // 下放受控组给工作室/部门（studio_admin or admin）
	PermGroupEntitleRevoke Permission = "group:entitle:revoke" // 撤销授权（同上）

	// ── v0.7 预算管理 ────────────────────────────────────────────────────────
	PermBudgetViewAll    Permission = "budget:view:all"    // 查看全公司预算（admin）
	PermBudgetViewScoped Permission = "budget:view:scoped" // 查看自己管辖范围预算（dept_admin/studio_admin）
	PermBudgetSet        Permission = "budget:set"         // 设置/修改预算额度（按管辖域 RBAC 过滤）
	PermModelTierView    Permission = "model_tier:view"
	PermModelTierManage  Permission = "model_tier:manage"

	// ── v0.7 Service Key ────────────────────────────────────────────────────
	PermServiceKeyCreate Permission = "service_key:create" // 创建 Service Key（仅部门负责人）
	PermServiceKeyRevoke Permission = "service_key:revoke" // 吊销 Service Key

	// ── v0.8 审计日志 ────────────────────────────────────────────────────────
	PermAuditViewAll    Permission = "audit:view:all"    // 查看全公司审计日志（admin）
	PermAuditViewScoped Permission = "audit:view:scoped" // 查看自己管辖范围审计日志（dept_admin/studio_admin）

	// ── v0.7 扩容审批 ────────────────────────────────────────────────────────
	PermExpansionRequest Permission = "expansion:request" // 发起扩容申请（dept_admin）
	PermExpansionApprove Permission = "expansion:approve" // 审批扩容申请（studio_admin/admin）

	// ── 模型组授权 ──────────────────────────────────────────────────────────────
	PermEntitlementManage    Permission = "entitlement:manage"     // 平台管理员：设开放模式 + 授权工作室
	PermEntitlementGrantDept Permission = "entitlement:grant_dept" // 工作室负责人：下放/开通部门

	// ── feat/key-approval: Key 创建申请与审批 ──────────────────────────────────
	PermKeyRequestApprove   Permission = "key_request:approve"    // 部门负责人审批 Key 申请
	PermKeyRequestSetPolicy Permission = "key_request:set_policy" // 设置部门 Key 创建策略（dept_admin / admin）

	// ── v0.15 四层配额体系 ──────────────────────────────────────────────────
	PermQuotaDefaultSet    Permission = "quota:default:set"     // 修改两套全局默认（admin）
	PermQuotaUserSet       Permission = "quota:user:set"        // 设置用户配额（service 层校验 dept 范围）
	PermQuotaServiceKeySet Permission = "quota:service_key:set" // 设置 Service Key 配额（按 Key.dept_id 校验）
	PermQuotaGroupSet      Permission = "quota:group:set"       // 设置模型组配额（admin）
	PermQuotaModelSet      Permission = "quota:model:set"       // 设置模型配额（admin）
	PermQuotaViewScoped    Permission = "quota:view:scoped"     // 查看管辖范围内配额
	PermQuotaViewAll       Permission = "quota:view:all"        // 查看全公司配额（admin）

	// ── 管理面敏感操作（默认仅 admin）────────────────────────────────────────
	PermProviderManage       Permission = "provider:manage"        // 渠道管理（含上游凭证）
	PermSettingsManage       Permission = "settings:manage"        // 系统设置读写（含导出/导入）
	PermOpsView              Permission = "ops:view"               // 运维看板查看
	PermModelManage          Permission = "model:manage"           // 模型管理
	PermModelCatalogManage   Permission = "model_catalog:manage"   // 模型 Catalog 同步/绑定
	PermGroupManage          Permission = "group:manage"           // 模型组管理
	PermChannelMonitorManage Permission = "channel_monitor:manage" // 渠道监控管理
	PermHealthCheckManage    Permission = "health_check:manage"    // 健康检测配置/触发
	PermAnnouncementManage   Permission = "announcement:manage"    // 公告管理（写操作）
	PermOrgUserSearch        Permission = "org:user:search"        // 用户搜索（任意管理角色）
	PermRoleView             Permission = "org:role:view"          // 查看用户角色（任意管理角色）
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
