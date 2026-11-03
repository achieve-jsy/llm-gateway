import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import RankingTable from '../RankingTable.vue'

vi.mock('@/stores/settings', () => ({
  useSettingsStore: vi.fn(() => ({
    publicSettings: { pricing_usd_to_cny_rate: 7.2 },
  })),
}))

vi.mock('@element-plus/icons-vue', () => ({
  Search: { template: '<i />' },
  ArrowRight: { template: '<i />' },
  User: { template: '<i />' },
  View: { template: '<i />' },
  Connection: { template: '<i />' },
}))

function mountComponent(props: Record<string, any> = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(RankingTable, {
    props: {
      loading: false,
      displayedBreakdown: [],
      currentScope: 'dept',
      canDrill: true,
      breadcrumbs: [],
      topLabel: '全公司',
      rankTitle: '成员成本排行',
      rankTotal: 0,
      rankPageSize: 20,
      rankSort: { field: 'cost', order: 'desc' },
      totalCost: 0,
      currency: 'USD',
      userTopModels: {},
      rankPage: 1,
      rankSearch: '',
      ...props,
    },
    global: { plugins: [ElementPlus, pinia] },
  })
}

describe('RankingTable 程序调用摘要条', () => {
  it('dept 范围且存在程序成本时显示摘要条', () => {
    const wrapper = mountComponent({
      serviceSummary: { cost_estimate: 30, call_count: 10, total_tokens: 500 },
    })

    expect(wrapper.find('.ud-service-strip').exists()).toBe(true)
    expect(wrapper.text()).toContain('程序调用')
  })

  it('非 dept 范围不显示摘要条', () => {
    const wrapper = mountComponent({
      currentScope: 'company',
      serviceSummary: { cost_estimate: 30, call_count: 10, total_tokens: 500 },
    })

    expect(wrapper.find('.ud-service-strip').exists()).toBe(false)
  })

  it('无姓名行回退显示「未关联用户」', () => {
    const wrapper = mountComponent({
      displayedBreakdown: [{ id: '', name: '', cost_estimate: 5, call_count: 1 }],
      totalCost: 5,
    })

    expect(wrapper.text()).toContain('未关联用户')
  })
})
