import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { oidcCallback, logout as logoutApi } from '@/services/auth'
import { refreshOnce } from '@/services/tokenRefresh'
import type { User, DeptStatus } from '@/types/auth'

// org 角色（来自 user_roles 表）
interface OrgRole {
  role: string
  scope_type: string
  scope_id: string
  scope_name?: string
}

function parseJWT(token: string): Record<string, any> | null {
  try {
    const payload = token.split('.')[1]
    if (!payload) return null
    const base64 = payload.replace(/-/g, '+').replace(/_/g, '/')
    const json = decodeURIComponent(atob(base64).split('').map(c => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2)).join(''))
    return JSON.parse(json)
  } catch { return null }
}

function clearStoredAuth() {
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
  localStorage.removeItem('save_user')
  localStorage.removeItem('dept_status')
}

function restoreUser(): { user: User | null; role: string | null } {
  const token = localStorage.getItem('access_token')
  if (!token) return { user: null, role: null }

  // JWT 是 role 的权威来源，始终解析；save_user 仅补充 user profile
  const claims = parseJWT(token)
  const jwtRole = claims?.role || null

  const save = localStorage.getItem('save_user')
  if (save) {
    try {
      const saved = JSON.parse(save) as { user: User | null; role: string | null }
      // role 以 JWT 为准，避免缓存过期导致权限丢失
      return { user: saved.user, role: jwtRole ?? saved.role }
    } catch {
      clearStoredAuth()
      return { user: null, role: null }
    }
  }

  if (claims) {
    const u = {
      id: claims.user_id || '',
      name: claims.name || '',
      email: claims.email || '',
      departmentId: claims.dept_id || '',
      departmentName: claims.dept_name || '',
    }
    return { user: u, role: jwtRole }
  }
  clearStoredAuth()
  return { user: null, role: null }
}

const restored = restoreUser()

let refreshTimer: ReturnType<typeof setTimeout> | null = null

function scheduleRefresh(token: string, refreshFn: () => Promise<void>) {
  clearRefreshTimer()
  const claims = parseJWT(token)
  const DEFAULT_REFRESH_MS = 23 * 60 * 60 * 1000
  let delay: number

  if (claims?.exp) {
    const nowSec = Math.floor(Date.now() / 1000)
    const remaining = claims.exp - nowSec
    if (remaining <= 300) {
      delay = 0 // 已过期或即将过期，立即刷新
    } else {
      delay = (remaining - 300) * 1000 // 过期前5分钟刷新
    }
  } else {
    delay = DEFAULT_REFRESH_MS
  }

  refreshTimer = setTimeout(() => {
    refreshFn().catch(() => {})
  }, delay)
}

function clearRefreshTimer() {
  if (refreshTimer) {
    clearTimeout(refreshTimer)
    refreshTimer = null
  }
}

export type UserRole = 'user' | 'admin' | 'dept_admin' | 'studio_admin'

export const useAuthStore = defineStore('auth', () => {
  const accessToken = ref<string | null>(localStorage.getItem('access_token') || null)
  const refreshTokenValue = ref<string | null>(localStorage.getItem('refresh_token') || null)
  const user = ref<User | null>(restored.user)
  const role = ref<UserRole | null>(restored.role as any || null)

  // org 角色（从 /api/v1/org/roles/me 拉取，login 后异步刷新）
  const orgRoles = ref<OrgRole[]>([])
  const effectiveRole = ref<UserRole | null>(restored.role as any || null)
  const orgRolesLoaded = ref(false)

  // 登录时后端返回的部门映射状态，供前端展示引导提示（localStorage 持久化）
  const deptStatus = ref<DeptStatus | null>(
    (localStorage.getItem('dept_status') as DeptStatus | null) ?? null
  )
  // 本次登录是否通过 bootstrap 自动授予了 admin（一次性标志，用完即清）
  const isBootstrapAdmin = ref(false)

  const isLoggedIn = computed(() => !!accessToken.value)
  const userName = computed(() => user.value?.name || '')
  const departmentName = computed(() => user.value?.departmentName || '')

  // 角色权限 computed（基于 effectiveRole，包含 org 层角色）
  const isAdmin = computed(() => effectiveRole.value === 'admin')
  const isManager = computed(() => ['admin', 'studio_admin', 'dept_admin'].includes(effectiveRole.value || ''))

  if (accessToken.value) {
    scheduleRefresh(accessToken.value, refresh)
  }

  // 从后端拉取 org 角色并更新 effectiveRole
  async function fetchOrgRoles() {
    const token = localStorage.getItem('access_token')
    if (!token) return
    try {
      const res = await fetch('/api/v1/org/roles/me', {
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) return
      const data = await res.json()
      orgRoles.value = data.roles || []
      if (data.effective_role) {
        effectiveRole.value = data.effective_role as UserRole
      }
      orgRolesLoaded.value = true
    } catch {}
  }

  async function ensureOrgRoles() {
    if (orgRolesLoaded.value) return
    await fetchOrgRoles()
  }

  function setAuth(data: { accessToken: string; refreshToken: string; user: User; role?: UserRole | string; dept_status?: DeptStatus; is_bootstrap_admin?: boolean }) {
    accessToken.value = data.accessToken
    refreshTokenValue.value = data.refreshToken
    user.value = data.user
    // role 优先用传入值，若没有则从 JWT 解析
    const resolvedRole = (data.role || parseJWT(data.accessToken)?.role || null) as UserRole | null
    role.value = resolvedRole
    effectiveRole.value = resolvedRole  // 先用 JWT role，fetchOrgRoles 后会覆盖
    if (data.dept_status) {
      deptStatus.value = data.dept_status
      localStorage.setItem('dept_status', data.dept_status)
    }
    isBootstrapAdmin.value = !!data.is_bootstrap_admin
    localStorage.setItem('access_token', data.accessToken)
    localStorage.setItem('refresh_token', data.refreshToken)
    localStorage.setItem('save_user', JSON.stringify({ user: data.user, role: resolvedRole }))
    scheduleRefresh(data.accessToken, refresh)
    // 异步拉取 org 角色更新 effectiveRole
    fetchOrgRoles()
  }

  async function handleCallback(code: string, state: string) {
    const data = await oidcCallback(code, state)
    setAuth(data)
  }

  async function refresh() {
    // 通过共享锁 refreshOnce 刷新，与 http.ts 401 拦截器共用同一 Promise，
    // 避免定时器和拦截器并发双重消耗 refresh_token。
    const pair = await refreshOnce()
    if (!pair) throw new Error('refresh failed')
    accessToken.value = pair.accessToken
    refreshTokenValue.value = pair.refreshToken
    // localStorage 已由 refreshOnce 写入，此处只更新 store 内存状态
    const newClaims = parseJWT(pair.accessToken)
    if (newClaims?.role) role.value = newClaims.role as UserRole
    scheduleRefresh(pair.accessToken, refresh)
    fetchOrgRoles()
  }

  async function handleLogout() {
    try { await logoutApi() } catch {}
    clearRefreshTimer()
    accessToken.value = null
    refreshTokenValue.value = null
    user.value = null
    role.value = null
    orgRoles.value = []
    effectiveRole.value = null
    orgRolesLoaded.value = false
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    localStorage.removeItem('save_user')
    localStorage.removeItem('dept_status')
    deptStatus.value = null
  }

  // 从 /api/v1/me 实时刷新 dept_status（页面刷新或登录后调用）
  async function refreshDeptStatus() {
    if (!accessToken.value) return
    try {
      const { default: axios } = await import('axios')
      const resp = await axios.get('/api/v1/me', {
        headers: { Authorization: `Bearer ${accessToken.value}` },
      })
      const ds = resp.data?.dept_status as DeptStatus | undefined
      if (ds) {
        deptStatus.value = ds
        localStorage.setItem('dept_status', ds)
      }
    } catch {
      // 网络异常时静默降级，沿用 localStorage 的旧值
    }
  }

  // 页面刷新时恢复 org 角色（如果已登录）
  if (accessToken.value) {
    fetchOrgRoles()
    refreshDeptStatus()
  }

  return {
    accessToken, refreshTokenValue, user, role, orgRoles, effectiveRole, orgRolesLoaded,
    isLoggedIn, isAdmin, isManager, userName, departmentName, deptStatus, isBootstrapAdmin,
    setAuth, handleCallback, refresh, handleLogout, fetchOrgRoles, ensureOrgRoles, refreshDeptStatus,
  }
})
