import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import ExpansionPrecheckPanel from '../ExpansionPrecheckPanel.vue'
import type { ExpansionPrecheck } from '@/types/expansion'

vi.mock('@/components/common/DataFreshnessNotice.vue', () => ({
  default: { name: 'DataFreshnessNotice', template: '<div />', props: ['stale', 'updatedAt'] },
}))

const value: ExpansionPrecheck = {
  can_submit: true,
  can_notify_owner: false,
  current_limit_usd: '100.000000',
  current_used_usd: '0.000000',
  target_amount_usd: '150.000000',
  increase_usd: '50.000000',
  effective_after_usd: '150.000000',
  precheck_version: 'v1',
  data_as_of: '2026-07-15T08:00:00Z',
  stale: false,
  affected_groups: [],
  visible_scopes: [],
  parent_constraints: [
    { source_level: 'company', status: 'available', freshness: 'fresh' },
    { source_level: 'studio', status: 'available', freshness: 'fresh' },
    { source_level: 'dept', status: 'available', freshness: 'fresh' },
  ],
}

describe('ExpansionPrecheckPanel', () => {
  it('不展示工程化的上级约束列表', () => {
    const wrapper = mount(ExpansionPrecheckPanel, {
      props: { value },
      global: { plugins: [ElementPlus] },
    })

    expect(wrapper.text()).not.toContain('上级约束')
    expect(wrapper.text()).not.toContain('company、studio、dept')
  })
})
