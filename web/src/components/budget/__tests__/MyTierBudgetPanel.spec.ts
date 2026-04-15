import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import MyTierBudgetPanel from '../MyTierBudgetPanel.vue'
import type { ModelTier, MyBudgetTiersResponse } from '@/types/model-tier'

const mockGetMyBudgetTiers = vi.fn()
const mockListModelTiers = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getMyBudgetTiers: (...args: unknown[]) => mockGetMyBudgetTiers(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
}))

const tiers: ModelTier[] = [
  {
    id: 'premium',
    name: '高级',
    description: '',
    color: '#8b5cf6',
    sort_order: 1,
    enabled: true,
    archived_at: null,
    group_count: 2,
    budget_count: 1,
    history_count: 0,
    reference_count: 3,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
]

const fixture: MyBudgetTiersResponse = {
  source_summary: {
    mode: 'mixed',
    primary_scope_type: 'member',
    has_shared_pool: true,
    has_temporary_override: true,
  },
  effective_total: [],
  budget_chain: [],
  tier_limits: [
    {
      model_tier_id: 'premium',
      model_tier_name: '高级',
      enabled: true,
      periods: [
        {
          period_type: 'monthly',
          configured: true,
          base_limit_usd: '80.000000',
          temporary_target_usd: '100.000000',
          effective_limit_usd: '100.000000',
          used_usd: '85.000000',
          remaining_usd: '15.000000',
          reset_at: '2026-07-31T16:00:00Z',
          status: 'enabled',
          limiting_source: { scope_type: 'member' },
        },
        {
          period_type: 'daily',
          configured: false,
          used_usd: '0.000000',
          reset_at: '2026-07-15T00:00:00Z',
          status: 'unconfigured',
          limiting_source: { scope_type: 'dept_daily' },
        },
      ],
    },
  ],
  data_updated_at: '2026-07-14T06:58:00Z',
  stale: false,
}

function mountPanel() {
  return mount(MyTierBudgetPanel, {
    global: { plugins: [ElementPlus] },
  })
}

describe('MyTierBudgetPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetMyBudgetTiers.mockResolvedValue(structuredClone(fixture))
    mockListModelTiers.mockResolvedValue(structuredClone(tiers))
  })

  it('显示来源摘要、临时扩容横幅和真实等级颜色', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.findAll('[data-test="tier-source-summary-item"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('混合预算来源')
    expect(wrapper.text()).toContain('高级额度包含临时扩容')
    expect(wrapper.text()).toContain('长期基础额度 $80.00 + 临时增量 $20.00')
    expect(wrapper.get('[data-test="tier-color-dot"]').attributes('style')).toContain('#8b5cf6')
  })

  it('提供日周月切换、左侧有效额度与右侧四条帮助', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.get('[data-test="period-daily"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="period-weekly"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="period-monthly"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="tier-budget-list"]').text()).toContain('高级')
    expect(wrapper.findAll('[data-test="tier-help-item"]')).toHaveLength(4)
    expect(wrapper.get('.my-tier-grid').exists()).toBe(true)
    expect(wrapper.classes()).toContain('my-tier-panel')
  })

  it('申请调整额度向页面级统一表单发送预填信息', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.get('[data-test="tier-create-expansion"]').trigger('click')
    expect(wrapper.emitted('requestExpansion')).toEqual([[
      {
        dimension: 'tier',
        tierId: 'premium',
        period: 'monthly',
        targetAmount: '100.000000',
      },
    ]])
  })
})
