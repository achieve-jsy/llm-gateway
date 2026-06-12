import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import KpiCards from '../KpiCards.vue'

vi.mock('@/stores/settings', () => ({
  useSettingsStore: vi.fn(() => ({
    publicSettings: { pricing_usd_to_cny_rate: 7.2 },
  })),
}))

vi.mock('@element-plus/icons-vue', () => ({
  Money: { template: '<i />' },
  DataLine: { template: '<i />' },
  Coin: { template: '<i />' },
  User: { template: '<i />' },
  ArrowUp: { template: '<i />' },
  ArrowDown: { template: '<i />' },
  CircleCheck: { template: '<i />' },
}))

function mountComponent(props: Record<string, any> = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(KpiCards, {
    props: {
      summary: null,
      loading: false,
      currency: 'USD',
      costChange: null,
      callsChange: null,
      prevPeriodLabel: '',
      currentBudget: null,
      budgetLevel: 'ok',
      currentScope: 'dept',
      avgCostPerCall: '0.00',
      ...props,
    },
    global: { plugins: [ElementPlus, pinia] },
  })
}

describe('KpiCards 成员/程序构成', () => {
  it('dept 范围且存在程序成本时显示构成条', () => {
    const wrapper = mountComponent({
      summary: {
        cost_estimate: 100,
        service_cost_estimate: 30,
        service_call_count: 10,
        service_total_tokens: 500,
      },
    })

    expect(wrapper.find('.ud-kpi__split').exists()).toBe(true)
    expect(wrapper.text()).toContain('成员')
    expect(wrapper.text()).toContain('程序')
  })

  it('非 dept 范围不显示构成条', () => {
    const wrapper = mountComponent({
      currentScope: 'company',
      summary: { cost_estimate: 100, service_cost_estimate: 30 },
    })

    expect(wrapper.find('.ud-kpi__split').exists()).toBe(false)
  })

  it('程序成本为 0 且无程序调用时不显示构成条', () => {
    const wrapper = mountComponent({
      summary: {
        cost_estimate: 100,
        service_cost_estimate: 0,
        service_call_count: 0,
      },
    })

    expect(wrapper.find('.ud-kpi__split').exists()).toBe(false)
  })
})
