import http from '@/services/http'

// ============ 类型定义 ============

export interface EhrOrganization {
  id: string
  org_id: string
  org_name: string
  org_code: string
  ehr_create_date: string
  ehr_modify_date: string
  synced_at: string
}

export interface EhrDepartment {
  id: string
  org_id: string
  department_id: string
  department_name: string
  department_type: number
  parent_id: string
  leader_name: string
  manager_name: string
  hr_bp_name: string
  manual_override_at: string | null
  manual_override_by: string
  manual_override_reason: string
  synced_at: string
}

export interface EhrEmployee {
  id: string
  org_id: string
  employee_id: string
  person_name: string
  department_id: string
  department_name: string
  position_name: string
  manager_name: string
  person_status: number
  manual_override_at: string | null
  manual_override_by: string
  manual_override_reason: string
  synced_at: string
}

export interface EhrSyncStatus {
  data: {
    scheduler_running: boolean
    latest_snapshot: EhrSyncStatSnapshot | null
    latest_execution: EhrSyncExecutionLog | null
    override_count: number
  }
}

export interface EhrSyncStatSnapshot {
  id: string
  org_id: string
  synced_at: string
  total_organizations: number
  total_companies: number
  total_departments: number
  total_employees: number
  active_employees: number
  override_count: number
  pending_review_count: number
  last_incremental_ms: number
  last_full_reconcile_ms: number
  ehr_call_count: number
  ehr_error_count: number
}

export interface EhrSyncExecutionLog {
  id: string
  batch_id: string
  entity: string
  org_id: string
  sync_type: 'incremental' | 'full' | 'manual'
  status: 'running' | 'completed' | 'partial_success' | 'failed'
  trigger_type: 'cron' | 'manual'
  started_at: string
  finished_at: string | null
  duration_ms: number
  records_fetched: number
  records_created: number
  records_updated: number
  records_skipped: number
  records_deleted: number
  error_msg: string
}

export interface EhrSyncChangeLog {
  id: string
  batch_id: string
  entity: string
  entity_id: string
  org_id: string
  change_type: 'created' | 'modified' | 'deleted' | 'transferred'
  old_value: string
  new_value: string
  source: string
  occurred_at: string
}

export interface EhrSyncErrorLog {
  id: string
  batch_id: string
  entity: string
  org_id: string
  method: string
  status_code: number
  error_msg: string
  attempt_num: number
  occurred_at: string
}

export interface EhrOverrideItem {
  department_id?: string
  employee_id?: string
  org_id: string
  name?: string
  manual_override_at: string
  manual_override_by: string
  manual_override_reason: string
}

export interface EhrListResponse<T> {
  data: T[]
  total: number
  page?: number
  page_size?: number
}

export interface EhrTriggerResult {
  data: { batch_id: string; sync_type: string; status: string }
}

// ============ API 方法 ============

export function getEhrStatus(): Promise<EhrSyncStatus> {
  return http.get('/api/v1/ehr/status').then((r) => r.data)
}

export function getEhrOrganizations(): Promise<EhrListResponse<EhrOrganization>> {
  return http.get('/api/v1/ehr/organizations').then((r) => r.data)
}

export function getEhrDepartments(params: {
  org_id?: string
  name?: string
  page?: number
  page_size?: number
}): Promise<EhrListResponse<EhrDepartment>> {
  return http.get('/api/v1/ehr/departments', { params }).then((r) => r.data)
}

export function getEhrEmployees(params: {
  org_id?: string
  dept_id?: string
  person_name?: string
  status?: number | string
  page?: number
  page_size?: number
}): Promise<EhrListResponse<EhrEmployee>> {
  return http.get('/api/v1/ehr/employees', { params }).then((r) => r.data)
}

export function triggerEhrSync(syncType: 'incremental' | 'full' = 'incremental'): Promise<EhrTriggerResult> {
  return http.post('/api/v1/ehr/sync', { sync_type: syncType }).then((r) => r.data)
}

export function getEhrOverrides(): Promise<{
  data: { departments: EhrDepartment[]; employees: EhrEmployee[]; total: number }
}> {
  return http.get('/api/v1/ehr/overrides').then((r) => r.data)
}

export function setEhrOverride(params: {
  entity: 'department' | 'employee'
  entity_id: string
  org_id: string
  reason: string
}): Promise<{ data: any }> {
  return http.post('/api/v1/ehr/override', params).then((r) => r.data)
}

export function releaseEhrOverride(entity: string, orgId: string, entityId: string): Promise<{ data: any }> {
  return http.delete(`/api/v1/ehr/override/${entity}/${orgId}/${entityId}`).then((r) => r.data)
}

export function getEhrExecutionLogs(params?: {
  entity?: string
  status?: string
  page?: number
  page_size?: number
}): Promise<EhrListResponse<EhrSyncExecutionLog>> {
  return http.get('/api/v1/ehr/logs/executions', { params }).then((r) => r.data)
}

export function getEhrChangeLogs(params?: {
  entity?: string
  entity_id?: string
  change_type?: string
  batch_id?: string
  page?: number
  page_size?: number
}): Promise<EhrListResponse<EhrSyncChangeLog>> {
  return http.get('/api/v1/ehr/logs/changes', { params }).then((r) => r.data)
}

export function getEhrErrorLogs(params?: {
  entity?: string
  batch_id?: string
  page?: number
  page_size?: number
}): Promise<EhrListResponse<EhrSyncErrorLog>> {
  return http.get('/api/v1/ehr/logs/errors', { params }).then((r) => r.data)
}
