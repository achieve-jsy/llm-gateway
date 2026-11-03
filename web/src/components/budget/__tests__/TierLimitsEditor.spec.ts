import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import TierLimitsEditor from '../TierLimitsEditor.vue'
import type {
  BudgetTierLimit,
  BudgetTierUsage,
  ModelTier,
  TierLimitsDraft,
} from '@/types/model-tier'
import type { BudgetItem } from '@/types/budget'

const allocation: BudgetItem = {
  allocation_id: 'allocation-1',
  scope_type: 'studio',
  scope_id: 'studio-1',
  label: '创作工作室',
  amount_usd: 1000,
  daily_amount_usd: -1,
  weekly_amount_usd: 500,
  period: 'monthly',
  enabled: true,
  used_usd: 420,
  daily_used_usd: 18,
  weekly_used_usd: 96,
  used_ratio: 0.42,
}

const tiers: ModelTier[] = [
  {
    id: 'premium',
    name: '高级',
    description: '高性能模型',
    color: '#8b5cf6',
    sort_order: 1,
    enabled: true,
    archived_at: null,
    group_count: 3,
    budget_count: 1,
    history_count: 0,
    reference_count: 4,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
  },
  {
    id: 'standard',
    name: '标准',
    description: '通用模型',
    color: '#3b82f6',
    sort_order: 2,
    enabled: true,
    archived_at: null,
    group_count: 5,
    budget_count: 1,
    history_count: 0,
    reference_count: 6,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
  },
  {
    id: 'economy',
    name: '经济',
    description: '低成本模型',
    color: '#10b981',
    sort_order: 3,
    enabled: false,
    archived_at: null,
    group_count: 2,
    budget_count: 0,
    history_count: 0,
    reference_count: 2,
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
  },
]

const limits: BudgetTierLimit[] = [
  {
    id: 'limit-premium-monthly',
    budget_allocation_id: 'allocation-1',
    model_tier_id: 'premium',
    model_tier_name: '高级',
    period_type: 'monthly',
    limit_usd: '300.000000',
    enabled: true,
  },
  {
    id: 'limit-standard-monthly',
    budget_allocation_id: 'allocation-1',
    model_tier_id: 'standard',
    model_tier_name: '标准',
    period_type: 'monthly',
    limit_usd: '0',
    enabled: true,
  },
]

const usages: BudgetTierUsage[] = [
  {
    allocation_id: 'allocation-1',
    model_tier_id: 'premium',
    period_type: 'monthly',
    used_usd: '120.000000',
    data_as_of: '2026-07-14T00:00:00Z',
    stale: false,
  },
  {
    allocation_id: 'allocation-1',
    model_tier_id: 'premium',
    period_type: 'daily',
    used_usd: '12.000000',
    data_as_of: '2026-07-14T00:00:00Z',
    stale: false,
  },
  {
    allocation_id: 'allocation-1',
    model_tier_id: 'premium',
    period_type: 'weekly',
    used_usd: '80.000000',
    data_as_of: '2026-07-14T00:00:00Z',
    stale: false,
  },
]

const draft: TierLimitsDraft = {
  total: {
    daily: { configured: true, value: '-1' },
    weekly: { configured: true, value: '500' },
    monthly: { configured: true, value: '1000' },
  },
  tiers: {
    premium: {
      daily: { enabled: true, configured: false, value: '' },
      weekly: { enabled: true, configured: false, value: '' },
      monthly: { enabled: true, configured: true, value: '300' },
    },
    standard: {
      daily: { enabled: true, configured: false, value: '' },
      weekly: { enabled: true, configured: false, value: '' },
      monthly: { enabled: true, configured: true, value: '0' },
    },
    economy: {
      daily: { enabled: false, configured: false, value: '' },
      weekly: { enabled: false, configured: false, value: '' },
      monthly: { enabled: false, configured: false, value: '' },
    },
  },
  effectiveMode: 'immediate',
  reason: '管理员调整预算',
}

function mountEditor(modelValue: TierLimitsDraft = draft) {
  return mount(TierLimitsEditor, {
    props: { allocation, tiers, limits, usages, modelValue },
    global: { plugins: [ElementPlus] },
  })
}

describe('TierLimitsEditor', () => {
  it('默认展示月度主路径，不平铺日/周 Tab', () => {
    const wrapper = mountEditor()

    expect(wrapper.text()).toContain('月度上限')
    expect(wrapper.text()).toContain('等级子预算')
    expect(wrapper.text()).toContain('高级')
    expect(wrapper.find('[data-test="period-daily"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="advanced-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="daily-limit-input"]').exists()).toBe(false)
  })

  it('月度 -1 显示为不限而不是裸数字', () => {
    const unlimitedDraft: TierLimitsDraft = {
      ...draft,
      total: {
        ...draft.total,
        monthly: { configured: true, value: '-1' },
      },
    }
    const wrapper = mountEditor(unlimitedDraft)
    expect(wrapper.find('[data-test="monthly-unlimited"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('不限')
    expect(wrapper.find('[data-test="monthly-limit-input"]').exists()).toBe(false)
  })

  it('展开高级设置后可见总预算日/周、等级日/周与原因', async () => {
    const wrapper = mountEditor()
    await wrapper.get('[data-test="advanced-toggle"]').trigger('click')
    expect(wrapper.find('[data-test="daily-unlimited"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="tier-period-advanced"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="tier-premium-daily-input"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="tier-premium-weekly-input"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="tier-premium-daily-used"]').text()).toContain('已用 $12.00')
    expect(wrapper.get('[data-test="tier-premium-weekly-used"]').text()).toContain('已用 $80.00')
    expect(wrapper.text()).toContain('已用 $18.00')
    expect(wrapper.text()).toContain('已用 $96.00')
    expect(wrapper.find('[data-test="adjust-reason"]').exists()).toBe(true)
  })

  it('默认不展示等级日/周控件', () => {
    const wrapper = mountEditor()
    expect(wrapper.find('[data-test="tier-premium-daily-input"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="tier-premium-weekly-input"]').exists()).toBe(false)
  })

  it('高级设置可编辑等级日/周额度', async () => {
    const wrapper = mountEditor()
    await wrapper.get('[data-test="advanced-toggle"]').trigger('click')

    await wrapper.get('[data-test="tier-premium-daily-input"]').setValue('12.5')
    const dailyUpdated = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as TierLimitsDraft
    expect(dailyUpdated.tiers.premium.daily).toEqual({
      enabled: true,
      configured: true,
      value: '12.5',
    })

    await wrapper.get('[data-test="tier-standard-weekly-zero"]').trigger('click')
    const weeklyZeroed = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as TierLimitsDraft
    expect(weeklyZeroed.tiers.standard.weekly).toEqual({
      enabled: true,
      configured: true,
      value: '0',
    })
  })

  it('停用等级的日/周控件不可编辑', async () => {
    const wrapper = mountEditor()
    await wrapper.get('[data-test="advanced-toggle"]').trigger('click')

    const dailyInput = wrapper.get('[data-test="tier-economy-daily-input"]')
    const weeklyInput = wrapper.get('[data-test="tier-economy-weekly-input"]')
    expect(dailyInput.attributes('disabled')).toBeDefined()
    expect(weeklyInput.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="tier-economy-daily-zero"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="tier-economy-weekly-unconfigure"]').attributes('disabled')).toBeDefined()
  })

  it('区分移除限制与设置为零', async () => {
    const wrapper = mountEditor()

    await wrapper.get('[data-test="tier-premium-unconfigure"]').trigger('click')
    const unconfigured = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as TierLimitsDraft
    expect(unconfigured.tiers.premium.monthly.configured).toBe(false)
    expect(draft.tiers.premium.monthly.configured).toBe(true)

    await wrapper.get('[data-test="tier-standard-zero"]').trigger('click')
    const zeroed = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as TierLimitsDraft
    expect(zeroed.tiers.standard.monthly.value).toBe('0')
    expect(wrapper.emitted('dirty-change')).toContainEqual([true])
  })

  it('停用等级的异常用量不会显示 NaN', () => {
    const invalidUsages: BudgetTierUsage[] = [
      ...usages,
      {
        allocation_id: 'allocation-1',
        model_tier_id: 'economy',
        period_type: 'monthly',
        used_usd: 'not-a-number',
        data_as_of: '2026-07-14T00:00:00Z',
        stale: false,
      },
    ]
    const invalidDraft: TierLimitsDraft = {
      ...draft,
      tiers: {
        ...draft.tiers,
        economy: {
          ...draft.tiers.economy,
          monthly: { enabled: false, configured: true, value: '' },
        },
      },
    }
    const wrapper = mount(TierLimitsEditor, {
      props: { allocation, tiers, limits, usages: invalidUsages, modelValue: invalidDraft },
      global: { plugins: [ElementPlus] },
    })

    expect(wrapper.text()).toContain('已停用')
    expect(wrapper.text()).not.toContain('NaN')
  })
})
