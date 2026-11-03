import { defineStore } from 'pinia'
import { ref } from 'vue'
import type {
  Studio, OrgDepartment, HubSyncStatus, OrgNodeLeader,
  AccountListResult, StudioOverview, RoleKey,
} from '@/types/org'
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

  // ─── EHR 同步状态（同步卡片）─────────────────────────────────────────────
  const syncStatus = ref<HubSyncStatus | null>(null)

  async function fetchSyncStatus(): Promise<HubSyncStatus> {
    const res = await http.get('/api/v1/admin/ai-app-hub/sync-status')
    syncStatus.value = res.data
    return res.data
  }

  // 手动触发全量同步。同步是阻塞式 HTTP（实测 3~10s），超时给到 60s。
  // 并发由服务端锁兜住：正在跑时返回 409，调用方按"同步中"处理即可。
  async function triggerSync() {
    const res = await http.post('/api/v1/admin/ai-app-hub/sync', { scope: 'all' }, { timeout: 60000 })
    return res.data
  }

  // ─── 账号/角色（一人一行）───────────────────────────────────────────────
  async function fetchAccounts(query: {
    keyword?: string; role?: RoleKey | ''; studioId?: string; deptId?: string
    createdFrom?: string; createdTo?: string
    page?: number; pageSize?: number
  } = {}): Promise<AccountListResult> {
    const params: Record<string, any> = {
      page: query.page || 1,
      page_size: query.pageSize || 20,
    }
    if (query.keyword) params.keyword = query.keyword
    if (query.role) params.role = query.role
    if (query.studioId) params.studio_id = query.studioId
    if (query.deptId) params.dept_id = query.deptId
    if (query.createdFrom) params.created_from = query.createdFrom
    if (query.createdTo) params.created_to = query.createdTo
    const res = await http.get('/api/v1/org/accounts', { params })
    return res.data
  }

  // ─── 工作室概览（头部人数/管理员/负责人 + 下属团队卡）─────────────────────
  const studioOverview = ref<StudioOverview | null>(null)

  async function fetchStudioOverview(studioId: string): Promise<StudioOverview | null> {
    const res = await http.get(`/api/v1/org/studios/${studioId}/overview`)
    studioOverview.value = res.data
    return res.data
  }

  // ─── 组织负责人（增量覆盖与保留）─────────────────────────────────────────
  const leaders = ref<OrgNodeLeader[]>([])

  async function fetchLeaders(): Promise<OrgNodeLeader[]> {
    const res = await http.get('/api/v1/org/leaders')
    leaders.value = res.data?.data || []
    return leaders.value
  }

  // 手动新增负责人（唯一手动入口）：keyword 由后端按花名册解析。
  async function addLeader(nodeType: 'studio' | 'dept', nodeId: string, keyword: string) {
    const res = await http.post('/api/v1/org/leaders', { node_type: nodeType, node_id: nodeId, keyword })
    return res.data
  }

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

  return {
    studios, departments, unmappedDepts, unmappedCount, loading,
    syncStatus, fetchSyncStatus, triggerSync,
    leaders, fetchLeaders, addLeader,
    fetchAccounts, studioOverview, fetchStudioOverview,
    fetchStudios, fetchDepartments, fetchUnmapped,
    fetchDeptMembers, searchUsers, fetchRolePage, fetchAllRoles, grantRole, revokeRole, getUserRoles,
  }
})
