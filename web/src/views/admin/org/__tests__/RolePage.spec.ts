import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import RolePage from '../RolePage.vue'

const mockFetchRolePage = vi.fn()
const mockFetchAllRoles = vi.fn()
const mockFetchStudios = vi.fn()
const mockFetchDepartments = vi.fn()

const rolePageResponse = {
  roles: [{
    id: 1,
    user_id: 'user-a',
    user_name: 'Alice',
    email: 'alice@example.com',
    dept_name: '平台研发部',
    role: 'dept_admin',
    scope_type: 'dept',
    scope_id: 'dept-platform',
    granted_by: 'admin-1',
    created_at: '2026-09-01T00:00:00Z',
  }],
  total: 31,
  page: 1,
  pageSize: 20,
  roleCounts: { admin: 2, studioAdmin: 5, deptAdmin: 24 },
}

vi.mock('@/stores/org', () => ({
  useOrgStore: () => ({
    studios: [],
    departments: [],
    fetchRolePage: (...args: unknown[]) => mockFetchRolePage(...args),
    fetchAllRoles: (...args: unknown[]) => mockFetchAllRoles(...args),
    fetchStudios: (...args: unknown[]) => mockFetchStudios(...args),
    fetchDepartments: (...args: unknown[]) => mockFetchDepartments(...args),
    searchUsers: vi.fn(),
    grantRole: vi.fn(),
    revokeRole: vi.fn(),
  }),
}))

async function mountPage() {
  const wrapper = mount(RolePage, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('RolePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    mockFetchRolePage.mockResolvedValue(rolePageResponse)
    mockFetchAllRoles.mockResolvedValue([])
    mockFetchStudios.mockResolvedValue({ studios: [] })
    mockFetchDepartments.mockResolvedValue({ departments: [] })
  })

  it('首次加载请求角色分页接口，并展示完整可见范围的统计', async () => {
    const wrapper = await mountPage()

    expect(mockFetchRolePage).toHaveBeenCalledWith({ page: 1, pageSize: 20, keyword: '', role: '' })
    expect(wrapper.text()).toContain('31 条授权')
    expect(wrapper.text()).toContain('24')
  })

  it('输入关键词后从第一页按服务端搜索角色列表', async () => {
    const wrapper = await mountPage()
    mockFetchRolePage.mockClear()

    await wrapper.get('[data-test="role-search"]').setValue('alice')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(mockFetchRolePage).toHaveBeenCalledWith({ page: 1, pageSize: 20, keyword: 'alice', role: '' })
  })
})
