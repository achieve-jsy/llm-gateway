import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { oidcCallback, refreshToken as refreshTokenApi, logout as logoutApi } from '@/services/auth'
import type { User, DeptStatus } from '@/types/auth'

// org 角色（来自 user_roles 表）
interface OrgRole {
  role: string
  scope_type: string
  scope_id: string
}

function parseJWT(token: string): Record<string, any> | null {
  try {
    const base64 = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')
    const json = decodeURIComponent(atob(base64).split('').map(c => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2)).join(''))
    return JSON.parse(json)
  } catch { return null }
}

function restoreUser(): { user: User | null; role: string | null } {
  const token = localStorage.getItem('access_token')
  if (!token) return { user: null, role: null }

  // JWT 是 role 的权威来源，始终解析；save_user 仅补充 user profile
  const claims = parseJWT(token)
  const jwtRole = claims?.role || null

  const save = localStorage.getItem('save_user')
  if (save) {
    const saved = JSON.parse(save) as { user: User | null; role: string | null }
    // role 以 JWT 为准，避免缓存过期导致权限丢失
    return { user: saved.user, role: jwtRole ?? saved.role }
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

  // 登录时后端返回的部门映射状态，供前端展示引导提示
  const deptStatus = ref<DeptStatus | null>(null)
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
    } catch {}
  }

  function setAuth(data: { accessToken: string; refreshToken: string; user: User; role?: UserRole | string; dept_status?: DeptStatus; is_bootstrap_admin?: boolean }) {
    accessToken.value = data.accessToken
    refreshTokenValue.value = data.refreshToken
    user.value = data.user
    // role 优先用传入值，若没有则从 JWT 解析
    const resolvedRole = (data.role || parseJWT(data.accessToken)?.role || null) as UserRole | null
    role.value = resolvedRole
    effectiveRole.value = resolvedRole  // 先用 JWT role，fetchOrgRoles 后会覆盖
    if (data.dept_status) deptStatus.value = data.dept_status
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
    const currentRefreshToken = localStorage.getItem('refresh_token')
    if (!currentRefreshToken) throw new Error('no refresh token')
    const data = await refreshTokenApi(currentRefreshToken)
    accessToken.value = data.accessToken
    refreshTokenValue.value = data.refreshToken
    localStorage.setItem('access_token', data.accessToken)
    localStorage.setItem('refresh_token', data.refreshToken)
    const newClaims = parseJWT(data.accessToken)
    if (newClaims?.role) role.value = newClaims.role as UserRole
    scheduleRefresh(data.accessToken, refresh)
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
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    localStorage.removeItem('save_user')
  }

  // 页面刷新时恢复 org 角色（如果已登录）
  if (accessToken.value) {
    fetchOrgRoles()
  }

  return {
    accessToken, refreshTokenValue, user, role, orgRoles, effectiveRole,
    isLoggedIn, isAdmin, isManager, userName, departmentName, deptStatus, isBootstrapAdmin,
    setAuth, handleCallback, refresh, handleLogout, fetchOrgRoles,
  }
})
