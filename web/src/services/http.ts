import axios from 'axios'
import type { AxiosInstance, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import { ElMessage } from 'element-plus'

/** BE 机器码 → 用户友好中文提示映射 */
export const ERROR_MESSAGES: Record<string, string> = {
  provider_name_conflict:  '渠道名称已存在，请换一个',
  model_name_conflict:     '模型名称已存在，请换一个',
  provider_has_models:     '该渠道下仍有模型，无法删除',
  provider_disabled:       '渠道已禁用，请先启用',
  provider_not_found:      '渠道不存在或已被删除',
  model_not_found:         '模型不存在或已被删除',
  model_provider_invalid:  '所选渠道无效或已禁用',
  sync_not_supported:      '该渠道类型不支持模型自动同步',
  upstream_error:          '上游服务暂时不可用，请稍后重试',
  invalid_tag_id:          '提交的标签 ID 不是有效 UUID',
  invalid_model_id:        '提交的模型 ID 不是有效 UUID',
  bad_request:             '请求参数有误',
  unauthorized:           '登录已失效，请重新登录',
  internal_error:          '服务器开小差了，请稍后重试',
}

/**
 * 解析 BE 错误响应中的机器码，返回对应的友好中文提示。
 * 若没有机器码或码未知则返回 null。
 */
export function resolveErrorMessage(error: unknown): string | null {
  const code = (error as any)?.response?.data?.error?.code as string | undefined
  return code ? (ERROR_MESSAGES[code] ?? null) : null
}

/** 创建 Axios 实例 */
const http: AxiosInstance = axios.create({
  timeout: 15000,
  headers: {
    'Content-Type': 'application/json'
  }
})

/** 请求拦截器：注入认证 Token */
http.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const token = localStorage.getItem('access_token')
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

/** 判断是否为认证相关接口（登录/刷新/回调等，这些接口的 401 不触发全局跳转） */
function isAuthEndpoint(url: string): boolean {
  return url.startsWith('/auth/') || url.startsWith('/api/v1/me')
}

/** 响应拦截器：统一错误处理 */
http.interceptors.response.use(
  (response: AxiosResponse) => {
    return response
  },
  (error) => {
    const status = error.response?.status
    const requestUrl = String(error.config?.url || '')
    const friendlyMsg = resolveErrorMessage(error)
    const backendMessage = error.response?.data?.error?.message || error.response?.data?.message
    const message = friendlyMsg ?? backendMessage ?? error.message ?? '请求失败'

    // 全局 401 处理：登录态失效时直接清 token 跳登录页，不继续 reject 避免业务层重复弹错
    if (status === 401 && !isAuthEndpoint(requestUrl)) {
      localStorage.removeItem('access_token')
      localStorage.removeItem('refresh_token')
      ElMessage.error(backendMessage || '登录已失效，请重新登录')
      if (window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
      return new Promise(() => {}) // 阻塞 Promise 链，不让各页面 .catch 再弹一层错
    }

    // 统一错误出口：拦截器处理所有错误，业务代码只做状态清理，不再弹重复消息
    if (status !== 409 || friendlyMsg) {
      ElMessage.error(`[${status || 'ERR'}] ${message}`)
    }

    return Promise.reject(error)
  }
)

export default http
