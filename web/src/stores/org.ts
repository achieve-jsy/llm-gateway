import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Studio, OrgDepartment, DeptStudioMapping, ImportDiff } from '@/types/org'
import http from '@/services/http'

export interface DeptMember {
  id: string
  name: string
  email: string
  joined_at: string
  employee_id?: string
  job_title?: string
  job_family?: string
  job_category?: string
  dept_source: 'sso' | 'manual'
  login_status: string
  has_conflict: boolean
}

export interface UserSearchResult {
  id: string
  name: string
  email: string
  dept_name?: string
  dept_id?: string
}

export interface RoleWithUser {
  id: number
  user_id: string
  user_name: string
  email: string
  dept_name?: string
  role: string
  scope_type: string
  scope_id: string
  granted_by: string
  created_at: string
}

export interface RoleListQuery {
  page?: number
  pageSize?: number
  keyword?: string
  role?: string
}

export interface RoleListCounts {
  admin: number
  studioAdmin: number
  deptAdmin: number
}

export interface RoleListResult {
  roles: RoleWithUser[]
  total: number
  page: number
  pageSize: number
  roleCounts: RoleListCounts
}

export const useOrgStore = defineStore('org', () => {
  const studios = ref<Studio[]>([])
  const departments = ref<OrgDepartment[]>([])
  const unmappedDepts = ref<OrgDepartment[]>([])
  const unmappedCount = ref(0)
  const loading = ref(false)

  async function fetchStudios(page = 1, pageSize = 100) {
    const res = await http.get('/api/v1/org/studios', { params: { page, page_size: pageSize } })
    studios.value = res.data.studios || []
    return res.data
  }

  async function fetchDepartments(status?: string, page = 1, pageSize = 100) {
    const params: Record<string, any> = { page, page_size: pageSize }
    if (status) params.status = status
    const res = await http.get('/api/v1/org/departments', { params })
    departments.value = res.data.departments || []
    return res.data
  }

  async function fetchUnmapped(page = 1, pageSize = 100) {
    const res = await http.get('/api/v1/org/departments/unmapped', { params: { page, page_size: pageSize } })
    unmappedDepts.value = res.data.departments || []
    unmappedCount.value = res.data.total || 0
    return res.data
  }

  async function fetchMappings(page = 1, pageSize = 100) {
    const res = await http.get('/api/v1/org/dept-studio-map', { params: { page, page_size: pageSize } })
    return res.data as { mappings: DeptStudioMapping[]; total: number }
  }

  async function importMappings(file: File, dryRun = false): Promise<{ dry_run: boolean; diff?: ImportDiff; updated?: number; skipped?: number; backfilled?: number }> {
    const form = new FormData()
    form.append('file', file)
    const res = await http.post('/api/v1/org/dept-studio-map/import', form, {
      params: { dry_run: dryRun },
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    return res.data
  }

  async function setDeptStudio(deptID: string, studioID: string) {
    const res = await http.put(`/api/v1/org/departments/${deptID}/studio`, { studio_id: studioID })
    return res.data
  }

  async function createStudio(name: string) {
    const res = await http.post('/api/v1/org/studios', { name })
    return res.data
  }

  async function deleteStudio(id: string) {
    await http.delete(`/api/v1/org/studios/${id}`)
  }

  async function fetchDeptMembers(deptId: string, page = 1, pageSize = 20): Promise<{ members: DeptMember[]; total: number }> {
    const res = await http.get(`/api/v1/org/departments/${deptId}/members`, { params: { page, page_size: pageSize } })
    return { members: res.data.members || [], total: res.data.total || 0 }
  }

  // studioID 不为空时只返回该工作室下的员工（指定部门负责人时传入）
  async function searchUsers(keyword: string, limit = 20, studioID = ''): Promise<UserSearchResult[]> {
    if (!keyword.trim()) return []
    const params: Record<string, any> = { keyword, limit }
    if (studioID) params.studio_id = studioID
    const res = await http.get('/api/v1/org/users', { params })
    return res.data.users || []
  }

  async function fetchRolePage(query: RoleListQuery = {}): Promise<RoleListResult> {
    const page = query.page ?? 1
    const pageSize = query.pageSize ?? 20
    const params: Record<string, any> = { page, page_size: pageSize }
    if (query.keyword?.trim()) params.keyword = query.keyword.trim()
    if (query.role) params.role = query.role
    const res = await http.get('/api/v1/org/all-roles', { params })
    const counts = res.data.role_counts || {}
    return {
      roles: res.data.roles || [],
      total: Number(res.data.total || 0),
      page: Number(res.data.page || page),
      pageSize: Number(res.data.page_size || pageSize),
      roleCounts: {
        admin: Number(counts.admin || 0),
        studioAdmin: Number(counts.studio_admin || 0),
        deptAdmin: Number(counts.dept_admin || 0),
      },
    }
  }

  // 保持组织树和工作室视图的全量角色读取语义；角色管理页使用 fetchRolePage。
  async function fetchAllRoles(): Promise<RoleWithUser[]> {
    const first = await fetchRolePage({ page: 1, pageSize: 100 })
    const roles = [...first.roles]
    for (let page = 2; roles.length < first.total; page += 1) {
      const next = await fetchRolePage({ page, pageSize: 100 })
      roles.push(...next.roles)
      if (next.roles.length === 0) break
    }
    return roles
  }

  async function grantRole(userID: string, role: string, scopeType: string, scopeID: string) {
    const res = await http.post('/api/v1/org/roles/grant', {
      user_id: userID, role, scope_type: scopeType, scope_id: scopeID,
    })
    return res.data
  }

  async function revokeRole(userID: string, role: string, scopeID: string) {
    const res = await http.post('/api/v1/org/roles/revoke', { user_id: userID, role, scope_id: scopeID })
    return res.data
  }

  async function getUserRoles(userID: string) {
    const res = await http.get(`/api/v1/org/roles/${userID}`)
    return res.data
  }

  async function changeUserDept(userID: string, deptId: string) {
    const res = await http.put(`/api/v1/org/users/${userID}/department`, { dept_id: deptId })
    return res.data
  }

  async function followSSO(userID: string) {
    const res = await http.put(`/api/v1/org/users/${userID}/department/follow-sso`)
    return res.data
  }

  return {
    studios, departments, unmappedDepts, unmappedCount, loading,
    fetchStudios, fetchDepartments, fetchUnmapped, fetchMappings,
    importMappings, setDeptStudio, createStudio, deleteStudio,
    fetchDeptMembers, searchUsers, fetchRolePage, fetchAllRoles, grantRole, revokeRole, getUserRoles,
    changeUserDept, followSSO,
  }
})
