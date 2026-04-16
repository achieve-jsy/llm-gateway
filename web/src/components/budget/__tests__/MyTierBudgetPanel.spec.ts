import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import MyTierBudgetPanel from '../MyTierBudgetPanel.vue'
import type { MyBudgetTiersResponse } from '@/types/model-tier'

const mockGetMyBudgetTiers = vi.fn()
const mockGetAvailableGroups = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getMyBudgetTiers: (...args: unknown[]) => mockGetMyBudgetTiers(...args),
  getAvailableGroups: (...args: unknown[]) => mockGetAvailableGroups(...args),
}))

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
      model_tier_color: '#8b5cf6',
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
    {
      model_tier_id: 'upstream',
      model_tier_name: '上级约束等级',
      enabled: true,
      periods: [{
        period_type: 'monthly',
        configured: true,
        effective_limit_usd: '0.000000',
        used_usd: '0.000000',
        remaining_usd: '0.000000',
        reset_at: '2026-08-01T00:00:00Z',
        status: 'restricted',
        limiting_source: { scope_type: 'studio' },
      }],
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
    mockGetAvailableGroups.mockResolvedValue({ data: { groups: [
      { id: 1, name: 'Claude Haiku 4.5 → deepseek-v4-flash 替代', description: '', strategy: 'round_robin', budget_tier_id: 'premium', budget_tier_name: '高级' },
      { id: 2, name: 'GPT-4.1', description: '', strategy: 'round_robin', budget_tier_id: 'premium', budget_tier_name: '高级' },
      { id: 3, name: 'Gemini Pro', description: '', strategy: 'round_robin', budget_tier_id: 'premium', budget_tier_name: '高级' },
      { id: 4, name: 'DeepSeek V3', description: '', strategy: 'round_robin', budget_tier_id: 'premium', budget_tier_name: '高级' },
    ] } })
  })

  it('显示来源摘要、临时扩容横幅和真实等级颜色', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.get('[data-test="tier-summary-available"]').text()).toContain('可用 1')
    expect(wrapper.get('[data-test="tier-summary-restricted"]').text()).toContain('受限 1')
    expect(wrapper.text()).toContain('个人与共享额度')
    expect(wrapper.text()).toContain('高级额度包含临时扩容')
    expect(wrapper.text()).toContain('长期基础额度 $80.00 + 临时增量 $20.00')
    expect(wrapper.get('[data-test="tier-color-dot"]').attributes('style')).toContain('#8b5cf6')
  })

  it('上级额度不向普通用户展示具体数值', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.text()).toContain('额度不可见')
    expect(wrapper.text()).toContain('具体上级额度不在此展示')
    expect(wrapper.text()).not.toContain('工作室预算')
  })

  it('提供日周月切换、等级额度与关联模型组', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    expect(wrapper.get('[data-test="period-daily"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="period-weekly"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="period-monthly"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="tier-budget-list"]').text()).toContain('高级')
    expect(wrapper.get('[data-test="tier-groups"]').text()).toContain('4 个模型组')
    expect(wrapper.get('[data-test="tier-groups"]').text()).toContain('Claude Haiku 4.5、GPT-4.1 等')
    expect(wrapper.get('[data-test="tier-groups"]').text()).not.toContain('deepseek-v4-flash')
    expect(wrapper.get('[data-test="tier-groups"]').text()).not.toContain('替代')
    expect(wrapper.get('.tier-table').exists()).toBe(true)
    expect(wrapper.classes()).toContain('my-tier-panel')
  })

  it('查看全部展示完整关联模型组列表', async () => {
    const wrapper = mountPanel()
    await flushPromises()

    await wrapper.findAll('button').find(button => button.text().includes('查看全部'))!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('高级关联模型组')
    expect(wrapper.text()).toContain('Claude Haiku 4.5')
    expect(wrapper.text()).toContain('DeepSeek V3')
    expect(wrapper.text()).not.toContain('deepseek-v4-flash')
    expect(wrapper.text()).not.toContain('替代')
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
