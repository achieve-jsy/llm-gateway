import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

function makeToken(role = 'admin') {
  const encode = (value: unknown) => Buffer.from(JSON.stringify(value), 'utf8').toString('base64url')
  const header = encode({ alg: 'none', typ: 'JWT' })
  const payload = encode({
    user_id: 'admin-user',
    name: '平台管理员',
    email: 'admin@example.com',
    role,
    exp: Math.floor(Date.now() / 1000) + 3600,
  })
  return `${header}.${payload}.sig`
}

vi.mock('@/services/auth', () => ({
  oidcCallback: vi.fn(),
  logout: vi.fn(),
}))

vi.mock('@/services/tokenRefresh', () => ({
  refreshOnce: vi.fn().mockResolvedValue(null),
}))

describe('auth store startup restore', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('clears malformed cached user profile instead of breaking app mount', async () => {
    localStorage.setItem('access_token', makeToken())
    localStorage.setItem('refresh_token', 'refresh-token')
    localStorage.setItem('save_user', '{bad-json')

    const { useAuthStore } = await import('../auth')
    const auth = useAuthStore()

    expect(auth.isLoggedIn).toBe(false)
    expect(localStorage.getItem('access_token')).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
    expect(localStorage.getItem('save_user')).toBeNull()
  })

  it('backfills profile fields missing from a stale save_user snapshot using the JWT claims', async () => {
    // 旧版本前端 / 历史登录路径可能写下一个缺 email 的快照；JWT 是服务端每次登录新签发的，
    // 必定带 user_id/name/email。快照缺字段时必须用 JWT 补齐，否则界面会一直显示「未提供邮箱」。
    localStorage.setItem('access_token', makeToken())
    localStorage.setItem('refresh_token', 'refresh-token')
    localStorage.setItem('save_user', JSON.stringify({
      user: { id: 'admin-user', name: '平台管理员' },
      role: 'admin',
    }))

    const { useAuthStore } = await import('../auth')
    const auth = useAuthStore()

    expect(auth.user?.email).toBe('admin@example.com')
    expect(auth.user?.name).toBe('平台管理员')
    expect(auth.user?.id).toBe('admin-user')
  })

  it('keeps values already present in save_user instead of overwriting them with JWT claims', async () => {
    localStorage.setItem('access_token', makeToken())
    localStorage.setItem('refresh_token', 'refresh-token')
    localStorage.setItem('save_user', JSON.stringify({
      user: { id: 'cached-id', name: '缓存里的名字', email: 'cached@corp.com' },
      role: 'admin',
    }))

    const { useAuthStore } = await import('../auth')
    const auth = useAuthStore()

    expect(auth.user?.email).toBe('cached@corp.com')
    expect(auth.user?.name).toBe('缓存里的名字')
    expect(auth.user?.id).toBe('cached-id')
  })

  it('summarizes every directly assigned approval role instead of only the effective role', async () => {
    const { useAuthStore } = await import('../auth')
    const auth = useAuthStore()
    auth.orgRoles = [
      { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-a' },
      { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-b' },
      { role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-a' },
      { role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-b' },
    ]

    expect(auth.departmentAdminRoles).toHaveLength(2)
    expect(auth.studioAdminRoles).toHaveLength(2)
    expect(auth.hasMultipleApprovalRoles).toBe(true)
    expect(auth.approvalRoleSummary).toBe('部门负责人：2 个部门；工作室负责人：2 个工作室')
  })
})
