import axios from 'axios'
import type { AxiosInstance, InternalAxiosRequestConfig, AxiosResponse } from 'axios'

export const ERROR_MESSAGES: Record<string, string> = {
  provider_name_conflict:   '渠道名称已存在，请换一个',
  model_name_conflict:      '模型名称已存在，请换一个',
  provider_has_models:      '该渠道下仍有模型，无法删除',
  provider_disabled:        '渠道已禁用，请先启用',
  provider_not_found:       '渠道不存在或已被删除',
  model_not_found:          '模型不存在或已被删除',
  model_provider_invalid:   '所选渠道无效或已禁用',
  sync_not_supported:       '该渠道类型不支持模型自动同步',
  upstream_error:           '上游服务暂时不可用，请稍后重试',
  invalid_tag_id:           '提交的标签 ID 不是有效 UUID',
  invalid_model_id:         '提交的模型 ID 不是有效 UUID',
  bad_request:              '请求参数有误',
  unauthorized:             '登录已失效，请重新登录',
  internal_error:           '服务器开小差了，请稍后重试',
}

export function resolveErrorMessage(error: unknown): string | null {
  const code = (error as any)?.response?.data?.error?.code as string | undefined
  return code ? (ERROR_MESSAGES[code] ?? null) : null
}

const refreshClient = axios.create({ timeout: 10000 })

const http: AxiosInstance = axios.create({
  timeout: 15000,
  headers: { 'Content-Type': 'application/json' },
})

http.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  if (typeof window !== 'undefined') {
    const token = localStorage.getItem('access_token')
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`
    }
  }
  return config
})

function isAuthEndpoint(url: string): boolean {
  return url.startsWith('/auth/') || url.startsWith('/api/v1/me')
}

let refreshPromise: Promise<string | null> | null = null

function clearAuthAndRedirect() {
  if (typeof window === 'undefined') return
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
  if (window.location.pathname !== '/login') {
    window.location.href = '/login'
  }
}

async function tryRefreshToken(): Promise<string | null> {
  if (typeof window === 'undefined') return null
  const refreshToken = localStorage.getItem('refresh_token')
  if (!refreshToken) return null
  try {
    const res = await refreshClient.post('/auth/refresh', { refresh_token: refreshToken })
    localStorage.setItem('access_token', res.data.accessToken)
    localStorage.setItem('refresh_token', res.data.refreshToken)
    return res.data.accessToken
  } catch {
    return null
  }
}

http.interceptors.response.use(
  (response: AxiosResponse) => response,
  async (error) => {
    const status = error.response?.status
    const requestUrl = String(error.config?.url || '')
    const config = error.config as InternalAxiosRequestConfig & { _retrying?: boolean }

    if (status === 401 && !isAuthEndpoint(requestUrl) && !config._retrying) {
      if (!refreshPromise) {
        refreshPromise = tryRefreshToken()
      }
      const newToken = await refreshPromise
      refreshPromise = null

      if (newToken) {
        config._retrying = true
        config.headers.Authorization = `Bearer ${newToken}`
        return http.request(config)
      }

      clearAuthAndRedirect()
      return new Promise(() => {})
    }

    return Promise.reject(error)
  }
)

export default http
