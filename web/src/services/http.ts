import axios from 'axios'
import type { AxiosInstance, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import { ElMessage } from 'element-plus'
import { refreshOnce } from './tokenRefresh'

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
  EXPANSION_PRECHECK_STALE: '预算数据已变化，请重新预检',
  EXPANSION_PRECHECK_BLOCKED: '当前额度受上级预算限制',
  BUDGET_LIMIT_BELOW_USED: '新额度低于当前已用，请确认影响',
  BUDGET_ZERO_BLOCK_CONFIRMATION_REQUIRED: '$0 会阻断后续请求，请明确确认',
  BUDGET_USAGE_UNAVAILABLE: '实时用量暂不可用，请稍后重试',
  BUDGET_PENDING_SCHEDULE_EXISTS: '该额度已有待生效变更',
}

export type GatewayBudgetErrorCode =
  | 'EXPANSION_PRECHECK_STALE'
  | 'EXPANSION_PRECHECK_BLOCKED'
  | 'BUDGET_LIMIT_BELOW_USED'
  | 'BUDGET_ZERO_BLOCK_CONFIRMATION_REQUIRED'
  | 'BUDGET_USAGE_UNAVAILABLE'
  | 'BUDGET_PENDING_SCHEDULE_EXISTS'
  | 'BUDGET_CONFIG_SYNC_PENDING'

export interface APIProblem {
  code: string
  message: string
  next_action?: 'reprecheck' | 'raise_target' | 'retry' | 'notify_owner'
  field_errors?: Record<string, string>
}

export function asAPIProblem(error: unknown): APIProblem {
  const body = (error as any)?.response?.data || {}
  const source = body.error && typeof body.error === 'object' ? body.error : body
  return {
    code: String(source.code || 'UNKNOWN_ERROR'),
    message: String(source.message || (error as Error)?.message || '请求失败'),
    next_action: source.next_action,
    field_errors: source.field_errors,
  }
}

export function budgetErrorMessage(code: GatewayBudgetErrorCode | string): string {
  return ERROR_MESSAGES[code] || '预算操作失败，请刷新后重试'
}

/**
 * 解析 BE 错误响应中的机器码，返回对应的友好中文提示。
 * 若没有机器码或码未知则返回 null。
 */
export function resolveErrorMessage(error: unknown): string | null {
  const problem = asAPIProblem(error)
  const code = problem.code
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

function clearAuthAndRedirect(message: string) {
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
  ElMessage.error(message)
  if (window.location.pathname !== '/login') {
    window.location.href = '/login'
  }
}

/** 响应拦截器：统一错误处理 */
http.interceptors.response.use(
  (response: AxiosResponse) => {
    return response
  },
  async (error) => {
    const status = error.response?.status
    const requestUrl = String(error.config?.url || '')
    const friendlyMsg = resolveErrorMessage(error)
    const backendMessage = error.response?.data?.error?.message || error.response?.data?.message
    const message = friendlyMsg ?? backendMessage ?? error.message ?? '请求失败'
    const config = error.config as InternalAxiosRequestConfig & { _retrying?: boolean; _silent?: boolean }

    // 全局 401 处理：通过共享锁 refreshOnce 续期，避免多路并发双重消耗 RT
    if (status === 401 && !isAuthEndpoint(requestUrl) && !config._retrying) {
      const pair = await refreshOnce()
      const newToken = pair?.accessToken ?? null

      if (newToken) {
        config._retrying = true
        config.headers.Authorization = `Bearer ${newToken}`
        return http.request(config)
      }

      clearAuthAndRedirect(backendMessage || '登录已失效，请重新登录')
      return new Promise(() => {})
    }

    // _silent 标记：通知类接口静默失败，不弹 toast
    if (config._silent) {
      return Promise.reject(error)
    }

    // 统一错误出口：拦截器处理所有错误，业务代码只做状态清理，不再弹重复消息
    if (status !== 409 || friendlyMsg) {
      ElMessage.error(`[${status || 'ERR'}] ${message}`)
    }

    return Promise.reject(error)
  }
)

export default http
