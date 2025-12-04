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

export interface DeptStudioMapping {
  dept_id: string
  dept_name?: string
  studio_id: string
  studio_name?: string
  source: 'import' | 'manual'
  updated_at: string
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

export interface ImportRow {
  dept_id: string
  studio_id: string
}

export interface ImportDiff {
  added: ImportRow[]
  updated: Array<{
    dept_id: string
    old_studio_id: string
    new_studio_id: string
  }>
  unchanged: number
}

export interface ImportResult {
  updated: number
  skipped: number
  backfilled: number
}

// ─── 员工花名册导入 ─────────────────────────────────────────────────────────────

export type EmployeeImportStatus = 'linked' | 'unmatched' | 'new_studio' | 'new_dept'

export interface EmployeeImportPreviewRow {
  row_index: number
  employee_id: string
  name: string
  email: string
  studio_name: string
  dept_name: string
  job_title: string
  status: EmployeeImportStatus
  new_studio: boolean
  new_dept: boolean
  matched_user_id?: string
  matched_user_name?: string
  manual_user_id?: string   // 管理员手动指定
}

export interface EmployeeImportSummary {
  total: number
  studios_created: number
  depts_created: number
  depts_mapped: number
  users_linked: number
  unmatched_emails: string[]
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
