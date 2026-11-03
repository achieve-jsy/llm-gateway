// org.ts — 组织架构相关类型定义

export type DeptStatus = 'active' | 'pending_studio'

export interface Studio {
  id: string
  name: string
  owner_id?: string
  created_at: string
}

export interface OrgDepartment {
  id: string
  dept_id: string
  dept_name: string
  studio_id?: string
  studio_name?: string
  status: DeptStatus
  owner_id?: string
  member_count?: number
  created_at: string
}

export interface UserRole {
  id?: number
  user_id?: string
  role: 'user' | 'dept_admin' | 'studio_admin' | 'admin'
  scope_type: 'self' | 'dept' | 'studio' | 'company'
  scope_id: string
  scope_name?: string
  granted_by?: string
  created_at?: string
}

// OrgTree 组织树结构
export interface OrgTreeDept {
  id: string
  name: string
  status: DeptStatus
  member_count?: number
}

export interface OrgTreeStudio {
  id: string
  name: string
  departments: OrgTreeDept[]
}

export interface OrgTree {
  company: {
    studios: OrgTreeStudio[]
    unmapped_departments: OrgTreeDept[]
  }
}

// ─── EHR 同步状态（管理页同步卡片）────────────────────────────────────────────
export interface ProjectionDiff {
  inserted: number
  updated: number
  deleted: number
}

export interface SyncDiff {
  studios: ProjectionDiff
  departments: ProjectionDiff
  mappings: ProjectionDiff
}

export interface HubSyncStatus {
  last_sync_at: string
  status: '' | 'success' | 'failed'
  scope: string
  error: string
  departments: number
  employees: number
  unmapped_departments: number
  resigned_accounts_disabled: number
  diff: SyncDiff
  running: boolean
}

// ─── 组织负责人（增量覆盖与保留：只增不删）──────────────────────────────────
export interface OrgNodeLeader {
  id: string
  node_type: 'studio' | 'dept'
  node_id: string
  name: string
  passport: string
  employee_id: string
  source: 'ehr' | 'manual'
}


// ─── 角色 / 账号（原型：一人一行，角色以徽标呈现）─────────────────────────
export type RoleKey = 'admin' | 'studio_admin' | 'dept_admin' | 'user'

export interface RoleGrantItem {
  role: RoleKey
  scope_type: 'company' | 'studio' | 'dept'
  scope_id: string
  scope_name: string
}

// AccountRow 一个"人"一行：组织属性 + 主角色 + 全部授权明细。
export interface AccountRow {
  user_id: string
  name: string
  employee_id: string
  job_title: string
  email: string
  created_at: string
  studio_id: string
  studio_name: string
  dept_id: string // departments.dept_id（EHR 部门 id，团队的业务键）
  dept_name: string
  primary_role: RoleKey
  roles: RoleGrantItem[]
}

export interface AccountListResult {
  accounts: AccountRow[]
  total: number
  page: number
  page_size: number
  role_counts: Record<RoleKey, number>
}

// ─── 工作室 / 团队概览（原型图1、图3 的左树 + 头部）────────────────────────
export interface AccountBrief {
  user_id: string
  name: string
  employee_id?: string
}

export interface OrgLeaderBrief {
  name: string
  source: 'ehr' | 'manual'
}

export interface TeamCard {
  dept_id: string
  dept_uuid: string
  dept_name: string
  member_count: number
  admins: AccountBrief[]
  leaders: OrgLeaderBrief[]
}

export interface StudioOverview {
  studio_id: string
  studio_name: string
  member_count: number
  admins: AccountBrief[]
  leaders: OrgLeaderBrief[]
  teams: TeamCard[]
  pending_dept_count: number
}

// ─── 成员行上的三块摘要（原型图3：配额 / 预算规则 / 模型授权）────────────────
export interface MemberConfigSummary {
  is_dept_admin: boolean
  quota: { rpm: number; max_concurrency: number; source: 'inherit' | 'override' }
  budget: { monthly: number; daily: number; weekly: number; source: 'member' | 'team' | 'none' }
  entitlement: { group_count: number; source: 'studio_dept' | 'none' }
}
