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
