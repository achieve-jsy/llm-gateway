import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { oidcCallback, refreshToken as refreshTokenApi, logout as logoutApi } from '@/services/auth'
import type { User } from '@/types/auth'

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
  const save = localStorage.getItem('save_user')
  if (save) return JSON.parse(save)
  const claims = parseJWT(token)
  if (claims) {
    const u = { id: claims.user_id || '', name: claims.name || '', email: claims.email || '', departmentId: claims.dept_id || '', departmentName: claims.dept_name || '' }
    return { user: u, role: claims.role || null }
  }
  return { user: null, role: null }
}

const restored = restoreUser()

export const useAuthStore = defineStore('auth', () => {
  const accessToken = ref<string | null>(localStorage.getItem('access_token') || null)
  const refreshTokenValue = ref<string | null>(localStorage.getItem('refresh_token') || null)
  const user = ref<User | null>(restored.user)
  const role = ref<'employee' | 'admin' | null>(restored.role as any || null)

  const isLoggedIn = computed(() => !!accessToken.value)
  const userName = computed(() => user.value?.name || '')
  const departmentName = computed(() => user.value?.departmentName || '')

  function setAuth(data: { accessToken: string; refreshToken: string; user: User; role: 'employee' | 'admin' }) {
    accessToken.value = data.accessToken
    refreshTokenValue.value = data.refreshToken
    user.value = data.user
    role.value = data.role
    localStorage.setItem('access_token', data.accessToken)
    localStorage.setItem('refresh_token', data.refreshToken)
    localStorage.setItem('save_user', JSON.stringify({ user: data.user, role: data.role }))
  }

  async function handleCallback(code: string, state: string) {
    const data = await oidcCallback(code, state)
    setAuth(data)
  }

  async function refresh() {
    if (!refreshTokenValue.value) throw new Error('no refresh token')
    const data = await refreshTokenApi(refreshTokenValue.value)
    accessToken.value = data.accessToken
    refreshTokenValue.value = data.refreshToken
    localStorage.setItem('access_token', data.accessToken)
    localStorage.setItem('refresh_token', data.refreshToken)
  }

  async function handleLogout() {
    try { await logoutApi() } catch {}
    accessToken.value = null
    refreshTokenValue.value = null
    user.value = null
    role.value = null
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    localStorage.removeItem('save_user')
  }

  return { accessToken, refreshTokenValue, user, role, isLoggedIn, userName, departmentName, setAuth, handleCallback, refresh, handleLogout }
})
