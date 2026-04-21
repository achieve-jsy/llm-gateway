import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import BudgetScopeWorkspace from '../BudgetScopeWorkspace.vue'
import type { BudgetItem } from '@/types/budget'

const mockFetchDeptMembers = vi.fn()

vi.mock('@/stores/org', () => ({
  useOrgStore: () => ({
    fetchDeptMembers: (...args: unknown[]) => mockFetchDeptMembers(...args),
  }),
}))

const deptDaily: BudgetItem = {
  allocation_id: 'alloc-daily',
  scope_type: 'dept_daily',
  scope_id: 'dept-1',
  label: 'AI 研发部',
  amount_usd: 1000,
  weekly_amount_usd: -1,
  daily_amount_usd: -1,
  used_usd: 100,
  used_ratio: 0.1,
  period: '2026-07',
}

const memberAlloc: BudgetItem = {
  allocation_id: 'alloc-m1',
  scope_type: 'member',
  scope_id: 'u1',
  parent_scope_id: 'dept-1',
  label: '已配额成员',
  amount_usd: 200,
  weekly_amount_usd: -1,
  daily_amount_usd: -1,
  used_usd: 50,
  used_ratio: 0.25,
  period: '2026-07',
}

describe('BudgetScopeWorkspace — 部门成员列表', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetchDeptMembers.mockResolvedValue({ members: [], total: 0 })
  })

  function mountDept(items: BudgetItem[] = [deptDaily]) {
    return mount(BudgetScopeWorkspace, {
      props: {
        scope: { type: 'dept', id: 'dept-1', label: 'AI 研发部' },
        pool: 'daily',
        items,
        role: 'admin',
      },
      global: { plugins: [ElementPlus] },
    })
  }

  it('无人时展示暂无成员空状态', async () => {
    const wrapper = mountDept()
    await flushPromises()

    expect(wrapper.find('[data-test="dept-members-empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('暂无成员')
    expect(wrapper.text()).toContain('默认走部门个人 Key 共享池')
  })

  it('合并组织成员与独立额度：未配额显示共享池与设置额度', async () => {
    mockFetchDeptMembers.mockResolvedValue({
      members: [
        { id: 'u1', name: '已配额成员', email: 'a@x.com', joined_at: '', dept_source: 'sso', login_status: 'active', has_conflict: false },
        { id: 'u2', name: '未配额成员', email: 'b@x.com', joined_at: '', dept_source: 'sso', login_status: 'active', has_conflict: false },
      ],
      total: 2,
    })

    const wrapper = mountDept([deptDaily, memberAlloc])
    await flushPromises()

    expect(wrapper.find('[data-test="dept-members-table"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('已配额成员')
    expect(wrapper.text()).toContain('未配额成员')
    expect(wrapper.text()).toContain('个人独立额度')
    expect(wrapper.text()).toContain('共享预算池')
    expect(wrapper.find('[data-test="member-edit-quota"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="member-set-quota"]').exists()).toBe(true)

    await wrapper.get('[data-test="member-set-quota"]').trigger('click')
    const editPayload = wrapper.emitted('edit')?.[0]?.[0] as BudgetItem & { isDefaultPool?: boolean }
    expect(editPayload.scope_id).toBe('u2')
    expect(editPayload.isDefaultPool).toBe(true)
  })

  it('勾选成员后可以批量设置额度并携带未配额成员行', async () => {
    mockFetchDeptMembers.mockResolvedValue({
      members: [
        { id: 'u1', name: '已配额成员', email: 'a@x.com', joined_at: '', dept_source: 'sso', login_status: 'active', has_conflict: false },
        { id: 'u2', name: '未配额成员', email: 'b@x.com', joined_at: '', dept_source: 'sso', login_status: 'active', has_conflict: false },
      ],
      total: 2,
    })

    const wrapper = mountDept([deptDaily, memberAlloc])
    await flushPromises()

    const rows = wrapper.findAll('[data-test="member-select-row"]')
    expect(rows).toHaveLength(2)
    await rows[1].setValue(true)

    expect(wrapper.text()).toContain('已选择 1 项')
    await wrapper.get('[data-test="member-batch-absolute"]').trigger('click')

    const batchPayload = wrapper.emitted('batch')?.[0]
    expect(batchPayload?.[1]).toBe('absolute')
    expect(batchPayload?.[0]).toEqual([expect.objectContaining({
      scope_type: 'member', scope_id: 'u2', isDefaultPool: true,
    })])
  })

  it('选中未配额成员时禁用按比例批量调整', async () => {
    mockFetchDeptMembers.mockResolvedValue({
      members: [
        { id: 'u1', name: '已配额成员', email: 'a@x.com', joined_at: '', dept_source: 'sso', login_status: 'active', has_conflict: false },
        { id: 'u2', name: '未配额成员', email: 'b@x.com', joined_at: '', dept_source: 'sso', login_status: 'active', has_conflict: false },
      ],
      total: 2,
    })

    const wrapper = mountDept([deptDaily, memberAlloc])
    await flushPromises()

    const rows = wrapper.findAll('[data-test="member-select-row"]')
    await rows[1].setValue(true)
    expect(wrapper.get('[data-test="member-batch-percentage"]').attributes('disabled')).toBeDefined()

    await rows[1].setValue(false)
    await rows[0].setValue(true)
    expect(wrapper.get('[data-test="member-batch-percentage"]').attributes('disabled')).toBeUndefined()
  })
})
