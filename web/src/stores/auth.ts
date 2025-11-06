import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import http from '@/services/http'

export interface UserInfo {
  email: string
  name: string
  passport: string
  avatar: string
  company_name: string
  dept_name: string
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem('auth_token'))
  const user = ref<UserInfo | null>(null)
  const loading = ref(false)

  const isAuthenticated = computed(() => !!token.value && token.value !== 'dev-token')

  function setToken(newToken: string) {
    token.value = newToken
    localStorage.setItem('auth_token', newToken)
  }

  function clearAuth() {
    token.value = null
    user.value = null
    localStorage.removeItem('auth_token')
  }

  async function fetchCurrentUser() {
    if (!token.value) return
    loading.value = true
    try {
      const res = await http.get('/api/v1/me')
      user.value = res.data
    } catch {
      // Token invalid, clear auth
      clearAuth()
    } finally {
      loading.value = false
    }
  }

  function loginRedirect() {
    const backendURL = import.meta.env.VITE_API_BASE_URL || ''
    window.location.href = `${backendURL}/auth/login`
  }

  function logout() {
    clearAuth()
    window.location.href = '/'
  }

  return {
    token,
    user,
    loading,
    isAuthenticated,
    setToken,
    clearAuth,
    fetchCurrentUser,
    loginRedirect,
    logout,
  }
})
