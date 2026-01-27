/**
 * tokenRefresh.ts — 全局唯一 refresh-token 锁
 *
 * 背景：前端存在两条独立的 token 刷新路径：
 *   1. stores/auth.ts scheduleRefresh —— AT 过期前 5 分钟，定时器自动触发
 *   2. services/http.ts interceptor   —— 任何 API 返回 401 时触发
 *
 * 两路同时触发时会向服务端发出两次 refresh 请求，由于服务端采用
 * "旧 RT 用完即加入黑名单"的一次性机制，第一次成功、第二次必然 401，
 * 导致新拿到的 RT 也立刻失效，用户被踢出重新登录。
 *
 * 解决方案：将实际的 HTTP 刷新调用收拢到本模块，用单例 Promise 锁
 * （inflightRefresh）保证同一时刻只有一个 refresh 请求在飞。
 * 不管有多少调用方并发调用 refreshOnce()，它们都等同一个 Promise。
 */

export interface TokenPair {
  accessToken: string
  refreshToken: string
}

/** 正在进行的 refresh 请求（共享 Promise），null 表示当前无进行中的刷新 */
let inflightRefresh: Promise<TokenPair | null> | null = null

/**
 * 执行一次 refresh，并把结果写入 localStorage。
 * 如果已经有 refresh 在进行中，直接复用同一个 Promise（不重复发请求）。
 *
 * @returns 新的 TokenPair，或 null（refresh_token 不存在 / 请求失败）
 */
export function refreshOnce(): Promise<TokenPair | null> {
  if (inflightRefresh) {
    return inflightRefresh
  }

  inflightRefresh = _doRefresh().finally(() => {
    inflightRefresh = null
  })

  return inflightRefresh
}

async function _doRefresh(): Promise<TokenPair | null> {
  const refreshToken = localStorage.getItem('refresh_token')
  if (!refreshToken) return null

  try {
    const res = await fetch('/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    })

    if (!res.ok) return null

    const data: TokenPair = await res.json()
    localStorage.setItem('access_token', data.accessToken)
    localStorage.setItem('refresh_token', data.refreshToken)
    return data
  } catch {
    return null
  }
}
