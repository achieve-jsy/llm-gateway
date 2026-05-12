import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus from 'element-plus'
import BudgetBatchAdjustWizard from '../BudgetBatchAdjustWizard.vue'
import type { BudgetItem } from '@/types/budget'
import { previewBudgetTierBatch, previewTotalBudgetBatch } from '@/services/control-plane'

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

  it('未创建独立额度的成员批量设置时使用 scope 作为 item_id', async () => {
    vi.mocked(previewTotalBudgetBatch).mockResolvedValueOnce({
      items: [{
        item_id: 'member:user-b',
        target_limit_usd: '120.000000',
        impact: {
          before_limit_usd: '-1.000000', after_limit_usd: '120.000000', current_used_usd: '0.000000',
          below_used: false, zero_block: false, affected_members: 0, affected_groups: [], replaces_pending_schedule: false,
          preview_version: 'pv-1', stale: false, data_as_of: '2026-07-17T00:00:00Z',
        },
      }],
    })
    const member = {
      scope_type: 'member', scope_id: 'user-b', label: '李四', amount_usd: 0,
      daily_amount_usd: -1, weekly_amount_usd: -1, period: '', enabled: true, used_usd: 0, used_ratio: 0,
    } as BudgetItem
    const wrapper = mount(BudgetBatchAdjustWizard, {
      props: { modelValue: true, items: [member], tiers: [], initialMode: 'absolute' },
      global: { plugins: [ElementPlus] },
    })

    await flushPromises()
    ;(wrapper.vm as any).form.value = '120'
    ;(wrapper.vm as any).form.reason = '批量成员额度'
    await (wrapper.vm as any).runPreview()
    await flushPromises()

    expect(previewTotalBudgetBatch).toHaveBeenCalledWith(expect.objectContaining({
      items: [{ item_id: 'member:user-b', scope_type: 'member', scope_id: 'user-b' }],
    }))
  })

  it('等级额度批量预览包含未创建独立额度的成员并回显名称', async () => {
    vi.mocked(previewBudgetTierBatch).mockResolvedValueOnce({
      preview_token: 'tier-preview',
      expires_at: '2026-07-17T00:05:00Z',
      data_as_of: '2026-07-17T00:00:00Z',
      stale: false,
      items: [{
        item_id: 'member:user-b',
        old_limit_usd: undefined,
        new_limit_usd: '80.000000',
        current_used_usd: '0.000000',
        below_used: false,
        zero_block: false,
        stale: false,
        status: 'valid',
      }],
    })
    const member = {
      scope_type: 'member', scope_id: 'user-b', label: '李四', amount_usd: 0,
      daily_amount_usd: -1, weekly_amount_usd: -1, period: '', enabled: true, used_usd: 0, used_ratio: 0,
    } as BudgetItem
    const wrapper = mount(BudgetBatchAdjustWizard, {
      props: {
        modelValue: false,
        items: [member],
        tiers: [{ id: 'tier-high', name: '高级' } as any],
        initialMode: 'tier',
      },
      global: { plugins: [ElementPlus] },
    })

    await wrapper.setProps({ modelValue: true })
    await flushPromises()
    ;(wrapper.vm as any).form.modelTierId = 'tier-high'
    ;(wrapper.vm as any).form.value = '80'
    ;(wrapper.vm as any).form.reason = '批量成员等级额度'
    await (wrapper.vm as any).runPreview()
    await flushPromises()

    expect(previewBudgetTierBatch).toHaveBeenCalledWith(expect.objectContaining({
      items: [expect.objectContaining({
        item_id: 'member:user-b',
        allocation_id: undefined,
        scope_type: 'member',
        scope_id: 'user-b',
        model_tier_id: 'tier-high',
      })],
    }))
    expect(wrapper.text()).toContain('李四')
  })
})
