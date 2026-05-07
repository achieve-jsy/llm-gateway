import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import BudgetManageOverview from '../BudgetManageOverview.vue'
import type { BudgetItem } from '@/types/budget'

vi.mock('@/components/budget/BudgetEmptyState.vue', () => ({
  default: { name: 'BudgetEmptyState', template: '<div />' },
}))

const studio = {
  allocation_id: 'a1',
  scope_type: 'studio',
  scope_id: 's1',
  label: '效率平台',
  amount_usd: 1000,
  used_usd: 900,
  used_ratio: 0.9,
} as BudgetItem

const company = {
  allocation_id: 'c1',
  scope_type: 'company',
  scope_id: 'GLOBAL',
  label: '全公司',
  amount_usd: 5000,
  used_usd: 1000,
  used_ratio: 0.2,
} as BudgetItem

describe('BudgetManageOverview', () => {
  it('选择条三键分别以不同 mode emit batch', async () => {
    const wrapper = mount(BudgetManageOverview, {
      props: { items: [company, studio], role: 'admin' },
    })

    await wrapper.get('input.proto-check').setValue(true)
    await wrapper.get('[data-test="batch-percentage"]').trigger('click')

    expect(wrapper.emitted('batch')?.[0]).toEqual([[studio], 'percentage'])
  })

  it('使用率列同时展示进度与状态文案', () => {
    const wrapper = mount(BudgetManageOverview, {
      props: { items: [company, studio], role: 'admin' },
    })
    expect(wrapper.find('.proto-usage-status').text()).toBe('预警')
  })

  it('明细 → 发出 kpiSelect scopes', async () => {
    const wrapper = mount(BudgetManageOverview, {
      props: { items: [company, studio], role: 'admin' },
    })
    await wrapper.get('[data-test="chain-detail"]').trigger('click')
    expect(wrapper.emitted('kpiSelect')?.[0]).toEqual(['scopes'])
  })

  it('等级预算标签展示实际用量而不是子预算百分比', () => {
    const wrapper = mount(BudgetManageOverview, {
      props: {
        items: [company, studio],
        role: 'admin',
        tierItems: [{ scope_id: 's1', tier_name: '高级模型', used_usd: 2056, used_ratio: 1.96 }],
      },
    })

    expect(wrapper.text()).toContain('高级模型 $2,056.00')
    expect(wrapper.text()).not.toContain('高级模型 196%')
  })
})
