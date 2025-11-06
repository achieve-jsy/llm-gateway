import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { oidcCallback, refreshToken as refreshTokenApi, logout as logoutApi } from '@/services/auth'
import type { User } from '@/types/auth'

export const useAuthStore = defineStore('auth', () => {
  const accessToken = ref<string | null>(localStorage.getItem('access_token') || null)
  const refreshTokenValue = ref<string | null>(localStorage.getItem('refresh_token') || null)
  const user = ref<User | null>(null)
  const role = ref<'employee' | 'admin' | null>(null)

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
  }

  return { accessToken, refreshTokenValue, user, role, isLoggedIn, userName, departmentName, setAuth, handleCallback, refresh, handleLogout }
})
