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
})
