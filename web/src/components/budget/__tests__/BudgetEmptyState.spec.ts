import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import BudgetEmptyState from '../BudgetEmptyState.vue'

describe('BudgetEmptyState', () => {
  it.each([
    ['company', '尚未向工作室分配预算'],
    ['studio', '暂无部门预算'],
    ['dept', '暂无成员独立额度'],
    ['member', '暂无个人预算限制'],
  ] as const)('显示 %s 层级空状态', (level, title) => {
    const wrapper = mount(BudgetEmptyState, {
      props: { level },
      global: { plugins: [ElementPlus] },
    })

    expect(wrapper.text()).toContain(title)
    expect(wrapper.attributes('data-test')).toBe(`budget-empty-${level}`)
  })

  it('主操作使用稳定标识并发出 action', async () => {
    const wrapper = mount(BudgetEmptyState, {
      props: {
        level: 'studio',
        actionLabel: '新增部门预算',
        actionTest: 'create-department-budget',
      },
      global: { plugins: [ElementPlus] },
    })

    await wrapper.get('[data-test="create-department-budget"]').trigger('click')

    expect(wrapper.emitted('action')).toHaveLength(1)
  })
})
