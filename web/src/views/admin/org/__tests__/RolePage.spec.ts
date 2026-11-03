import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import RolePage from '../RolePage.vue'

const mockFetchAccounts = vi.fn()
const mockFetchStudios = vi.fn()
const mockFetchDepartments = vi.fn()
const mockGrantRole = vi.fn()
const mockRevokeRole = vi.fn()

const accountsResponse = {
  accounts: [
    {
      user_id: 'user-a',
      name: 'Alice',
      employee_id: 'G001',
      job_title: '前端工程师',
      email: 'alice@example.com',
      created_at: '2026-09-01T00:00:00Z',
      studio_id: 'studio-1',
      studio_name: '平台工作室',
      dept_id: 'dept-platform',
      dept_name: '平台研发部',
      primary_role: 'studio_admin',
      roles: [
        { role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-1', scope_name: '平台工作室' },
        { role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-platform', scope_name: '平台研发部' },
      ],
    },
    {
      user_id: 'user-b',
      name: 'Bob',
      employee_id: 'G002',
      job_title: '测试工程师',
      email: 'bob@example.com',
      created_at: '2026-09-02T00:00:00Z',
      studio_id: 'studio-1',
      studio_name: '平台工作室',
      dept_id: 'dept-platform',
      dept_name: '平台研发部',
      primary_role: 'user',
      roles: [],
    },
  ],
  total: 31,
  page: 1,
  page_size: 20,
  role_counts: { admin: 2, studio_admin: 5, dept_admin: 24, user: 480 },
}

vi.mock('@/stores/org', () => ({
  useOrgStore: () => ({
    studios: [{ id: 'studio-1', name: '平台工作室', created_at: '2026-01-01T00:00:00Z' }],
    departments: [
      { id: 'uuid-1', dept_id: 'dept-platform', dept_name: '平台研发部', studio_id: 'studio-1', studio_name: '平台工作室', status: 'active', created_at: '2026-01-01T00:00:00Z' },
    ],
    fetchAccounts: (...args: unknown[]) => mockFetchAccounts(...args),
    fetchStudios: (...args: unknown[]) => mockFetchStudios(...args),
    fetchDepartments: (...args: unknown[]) => mockFetchDepartments(...args),
    grantRole: (...args: unknown[]) => mockGrantRole(...args),
    revokeRole: (...args: unknown[]) => mockRevokeRole(...args),
    searchUsers: vi.fn(),
  }),
}))

async function mountPage() {
  const wrapper = mount(RolePage, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

describe('RolePage（账号一人一行）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    mockFetchAccounts.mockResolvedValue(accountsResponse)
    mockFetchStudios.mockResolvedValue({ studios: [] })
    mockFetchDepartments.mockResolvedValue({ departments: [] })
  })

  it('首屏请求账号分页接口，一人一行并展示四个角色人数', async () => {
    const wrapper = await mountPage()

    expect(mockFetchAccounts).toHaveBeenCalledWith({
      keyword: '', role: '', studioId: '', deptId: '', createdFrom: undefined, createdTo: undefined, page: 1, pageSize: 20,
    })
    const text = wrapper.text()
    expect(text).toContain('31 人')
    expect(text).toContain('Alice')
    expect(text).toContain('G001')
    expect(text).toContain('平台研发部')
    // 主角色取最高权限：Alice 是工作室管理员，多出来的团队管理员折叠成 +1
    expect(text).toContain('工作室管理员')
    expect(text).toContain('+1')
    expect(text).toContain('员工')
    expect(text).toContain('480')
  })

  it('输入关键词后 300ms 防抖从第一页重新查询', async () => {
    const wrapper = await mountPage()
    mockFetchAccounts.mockClear()

    await wrapper.get('[data-test="role-search"]').setValue('alice')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(mockFetchAccounts).toHaveBeenCalledWith({
      keyword: 'alice', role: '', studioId: '', deptId: '', createdFrom: undefined, createdTo: undefined, page: 1, pageSize: 20,
    })
  })

  it('点击角色统计卡按该角色筛选，再点一次取消', async () => {
    const wrapper = await mountPage()
    mockFetchAccounts.mockClear()

    await wrapper.get('[data-test="stat-dept_admin"]').trigger('click')
    await flushPromises()
    expect(mockFetchAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ role: 'dept_admin', page: 1 }))

    await wrapper.get('[data-test="stat-dept_admin"]').trigger('click')
    await flushPromises()
    expect(mockFetchAccounts).toHaveBeenLastCalledWith(expect.objectContaining({ role: '' }))
  })

  it('创建时间区间走服务端 created_from/created_to，并从第一页重新查询', async () => {
    const wrapper = await mountPage()
    mockFetchAccounts.mockClear()

    const picker = wrapper.findComponent({ name: 'ElDatePicker' })
    await picker.vm.$emit('update:modelValue', ['2026-09-01', '2026-09-30'])
    await picker.vm.$emit('change', ['2026-09-01', '2026-09-30'])
    await flushPromises()

    // 不再前端过滤当前页：一次请求带时间参数（右界含当日由后端处理）
    expect(mockFetchAccounts).toHaveBeenCalledWith(expect.objectContaining({
      createdFrom: '2026-09-01', createdTo: '2026-09-30', page: 1, pageSize: 20,
    }))
  })

  it('接口未就绪时给中文空态兜底，不抛错', async () => {
    mockFetchAccounts.mockRejectedValue(new Error('404'))
    const wrapper = await mountPage()

    expect(wrapper.text()).toContain('账号接口暂未就绪')
  })
})
