import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus from 'element-plus'
import BudgetBatchAdjustWizard from '../BudgetBatchAdjustWizard.vue'
import type { BudgetItem } from '@/types/budget'

vi.mock('@/services/control-plane', () => ({
  previewTotalBudgetBatch: vi.fn(),
  applyTotalBudgetBatch: vi.fn(),
  previewBudgetTierBatch: vi.fn(),
  applyBudgetTierBatch: vi.fn(),
}))

vi.mock('@/components/common/DataFreshnessNotice.vue', () => ({
  default: { name: 'DataFreshnessNotice', template: '<div />' },
}))

const item = {
  allocation_id: 'a1',
  scope_type: 'studio',
  scope_id: 's1',
  label: 'Studio',
  amount_usd: 100,
  used_usd: 10,
  used_ratio: 0.1,
} as BudgetItem

describe('BudgetBatchAdjustWizard', () => {
  it('打开时按 initialMode=percentage 预填总预算百分比', async () => {
    const wrapper = mount(BudgetBatchAdjustWizard, {
      props: {
        modelValue: false,
        items: [item],
        tiers: [],
        initialMode: 'percentage',
      },
      global: { plugins: [ElementPlus] },
    })

    await wrapper.setProps({ modelValue: true })
    await flushPromises()
    await nextTick()

    const active = wrapper.find('.el-radio-button.is-active')
    expect(active.exists()).toBe(true)
    expect(active.text()).toContain('总预算百分比')
  })

  it('打开时按 initialMode=tier 预填等级额度', async () => {
    const wrapper = mount(BudgetBatchAdjustWizard, {
      props: {
        modelValue: false,
        items: [item],
        tiers: [{ id: 't1', name: '高级' } as any],
        initialMode: 'tier',
      },
      global: { plugins: [ElementPlus] },
    })

    await wrapper.setProps({ modelValue: true })
    await flushPromises()
    await nextTick()

    expect(wrapper.find('.el-radio-button.is-active').text()).toContain('等级额度')
  })
})
