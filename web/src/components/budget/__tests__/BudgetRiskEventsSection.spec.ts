import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import BudgetRiskEventsSection from '../BudgetRiskEventsSection.vue'
import type { BudgetRiskEvent, BudgetRiskPage, BudgetRiskSummary } from '@/types/budget-risk'

const mockPush = vi.fn()
const mockReplace = vi.fn()
const routeQuery: Record<string, string> = {}

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  useRoute: () => ({ query: routeQuery }),
}))

vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
}))

// 用量统计仅 admin 可见：桩掉 auth store，测试内可切换角色
const authMock = vi.hoisted(() => ({ isAdmin: true }))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authMock,
}))

const mockListBudgetRisks = vi.fn()
const mockGetBudgetRiskSummary = vi.fn()
const mockNotifyBudgetOwner = vi.fn()

vi.mock('@/services/control-plane', () => ({
  listBudgetRisks: (...args: unknown[]) => mockListBudgetRisks(...args),
  getBudgetRiskSummary: (...args: unknown[]) => mockGetBudgetRiskSummary(...args),
  notifyBudgetOwner: (...args: unknown[]) => mockNotifyBudgetOwner(...args),
}))

vi.mock('@/components/common/DataFreshnessNotice.vue', () => ({
  default: {
    name: 'DataFreshnessNotice',
    template: '<div class="mock-freshness" />',
    props: ['stale', 'updatedAt'],
  },
}))

function makeRiskEvent(overrides: Partial<BudgetRiskEvent> = {}): BudgetRiskEvent {
  return {
    id: 'risk-1',
    risk_type: 'threshold_reached',
    severity: 'warning',
    scope_type: 'member',
    scope_id: 'user-1',
    scope_name: '张三',
    period_type: 'monthly',
    dimension_type: 'total',
    model_tier_id: null,
    model_tier_name: '',
    effective_limit_usd: '100.000000',
    used_usd: '90.000000',
    usage_ratio: '0.90',
    limit_source: '部门月预算',
    blocked_requests: 0,
    detected_at: '2026-07-13T10:00:00Z',
    data_as_of: '2026-07-13T10:00:00Z',
    stale: false,
    ...overrides,
  }
}

const samplePage: BudgetRiskPage = {
  items: [
    makeRiskEvent({ id: 'risk-1', severity: 'warning', scope_name: '张三', usage_ratio: '0.90' }),
    makeRiskEvent({
      id: 'risk-2',
      severity: 'critical',
      scope_name: '李四',
      risk_type: 'request_blocked',
      blocked_requests: 5,
      usage_ratio: '1.11',
    }),
  ],
  total: 2,
  data_as_of: '2026-07-13T10:00:00Z',
  stale: false,
}

const sampleSummary: BudgetRiskSummary = {
  total: 3,
  warning: 2,
  critical: 1,
  by_type: { threshold_reached: 2, request_blocked: 1 },
  blocked_requests_total: 12,
  data_as_of: '2026-07-13T10:00:00Z',
  stale: false,
}

function mountSection() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(BudgetRiskEventsSection, {
    global: {
      plugins: [pinia, ElementPlus],
    },
  })
}

async function openFirstRisk(wrapper: ReturnType<typeof mountSection>) {
  await flushPromises()
  const firstRow = wrapper.findAll('.el-table__row')[0]
  expect(firstRow).toBeDefined()
  await firstRow!.trigger('click')
  await flushPromises()
}

describe('BudgetRiskEventsSection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    authMock.isAdmin = true
    Object.keys(routeQuery).forEach((key) => delete routeQuery[key])
    localStorage.setItem('budget-risk-auto-refresh', '0')
    mockListBudgetRisks.mockResolvedValue({ ...samplePage })
    mockGetBudgetRiskSummary.mockResolvedValue({ ...sampleSummary })
    mockNotifyBudgetOwner.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('风险摘要', () => {
    it('同时加载风险摘要和列表并显示 KPI', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenCalled()
      expect(mockGetBudgetRiskSummary).toHaveBeenCalled()
      expect(mockListBudgetRisks).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
      expect(wrapper.text()).toContain('当前风险')
      expect(wrapper.text()).toContain('严重风险')
      expect(wrapper.text()).toContain('即将耗尽')
      expect(wrapper.text()).toContain('实际阻断')
      expect(wrapper.text()).toContain('阻断请求')
      expect(wrapper.get('[data-test="budget-kpi-blocked-requests"]').text()).toContain('12')
      expect(wrapper.get('[data-test="budget-kpi-blocked-requests"]').text()).toContain('全量合计')
    })
  })

  describe('列表渲染', () => {
    it('从 API 加载风险事件并渲染表格', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenCalled()
      const html = wrapper.html()
      expect(html).toContain('张三')
      expect(html).toContain('李四')
    })

    it('显示表头"实时风险事件"', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.html()).toContain('实时风险事件')
    })

    it('空列表时显示监控中与上次刷新时间', async () => {
      mockListBudgetRisks.mockResolvedValue({ items: [], total: 0, data_as_of: '', stale: false })
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.get('[data-test="risk-empty"]').text()).toContain('系统持续监控中')
      expect(wrapper.get('[data-test="risk-empty"]').text()).toContain('上次刷新')
    })

    it('显示额度和已用金额', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('$100.00')
      expect(html).toContain('$90.00')
    })

    it('使用率以进度条和百分比展示，超额标红', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const bars = wrapper.findAll('[data-test="risk-usage-bar"]')
      expect(bars.length).toBeGreaterThanOrEqual(2)
      expect(bars.some((bar) => bar.text().includes('90%'))).toBe(true)
      expect(bars.some((bar) => bar.text().includes('111%'))).toBe(true)
      expect(bars.some((bar) => bar.classes().includes('is-over'))).toBe(true)
    })

    it('行内可直接调整额度与查看用量', async () => {
      const wrapper = mountSection()
      await flushPromises()

      await wrapper.get('[data-test="risk-inline-adjust"]').trigger('click')
      await flushPromises()
      expect(wrapper.emitted('viewBudget')?.[0]?.[0]).toMatchObject({ scope_name: '张三' })

      await wrapper.get('[data-test="risk-inline-usage"]').trigger('click')
      await flushPromises()
      expect(mockPush).toHaveBeenCalledWith(expect.objectContaining({
        path: '/admin/usage/dashboard',
        query: expect.objectContaining({ scope_type: 'member', scope_id: 'user-1' }),
      }))
    })

    it('非 admin 不显示「查看用量」入口（用量看板仅 admin 可见）', async () => {
      authMock.isAdmin = false
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.find('[data-test="risk-inline-usage"]').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('查看用量')
      // 预算调整仍可用
      expect(wrapper.find('[data-test="risk-inline-adjust"]').exists()).toBe(true)
    })
  })

  describe('严重度标签', () => {
    it('warning 级别显示"警告"标签', async () => {
      mockListBudgetRisks.mockResolvedValue({
        items: [makeRiskEvent({ severity: 'warning' })],
        total: 1,
        data_as_of: '',
        stale: false,
      })
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.html()).toContain('警告')
    })

    it('critical 级别显示"严重"标签', async () => {
      mockListBudgetRisks.mockResolvedValue({
        items: [makeRiskEvent({ severity: 'critical' })],
        total: 1,
        data_as_of: '',
        stale: false,
      })
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.html()).toContain('严重')
    })

    it('严重度标签使用不同的 el-tag type', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const tags = wrapper.findAll('.el-tag')
      const tagTypes = tags.map(t => t.classes()).flat()
      expect(tagTypes.some(c => c.includes('warning'))).toBe(true)
      expect(tagTypes.some(c => c.includes('danger'))).toBe(true)
    })
  })

  describe('限制来源显示', () => {
    it('将后端枚举映射为中文业务文案', async () => {
      mockListBudgetRisks.mockResolvedValue({
        items: [
          makeRiskEvent({ id: 'risk-base', limit_source: 'base_limit' }),
          makeRiskEvent({ id: 'risk-tier', limit_source: 'tier_base_limit' }),
          makeRiskEvent({ id: 'risk-override', limit_source: 'temporary_override' }),
          makeRiskEvent({ id: 'risk-block', limit_source: 'block_event', risk_type: 'request_blocked' }),
        ],
        total: 4,
        data_as_of: '',
        stale: false,
      })
      const wrapper = mountSection()
      await flushPromises()

      const text = wrapper.text()
      expect(text).toContain('总预算')
      expect(text).toContain('等级预算')
      expect(text).toContain('临时提额')
      expect(text).toContain('阻断事件')
      expect(text).not.toContain('base_limit')
      expect(text).not.toContain('tier_base_limit')
    })
  })

  describe('维度显示', () => {
    it('总预算维度显示"总预算"', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.html()).toContain('总预算')
    })

    it('模型等级维度显示等级名称', async () => {
      mockListBudgetRisks.mockResolvedValue({
        items: [makeRiskEvent({ dimension_type: 'tier', model_tier_name: 'Premium' })],
        total: 1,
        data_as_of: '',
        stale: false,
      })
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.html()).toContain('Premium')
    })

    it('周期类型正确翻译（日/周/月）', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.html()).toContain('月')
    })
  })

  describe('事件详情抽屉', () => {
    it('点击行打开详情抽屉', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const rows = wrapper.findAll('.el-table__row')
      expect(rows.length).toBeGreaterThan(0)
      await rows[0].trigger('click')
      await flushPromises()

      expect(wrapper.find('.mock-drawer, .el-drawer').exists()).toBe(true)
    })

    it('显示生效预算链、三种处置方式和建议缓冲', async () => {
      const wrapper = mountSection()
      await openFirstRisk(wrapper)

      expect(wrapper.text()).toContain('生效预算链')
      expect(wrapper.text()).toContain('调整额度')
      expect(wrapper.text()).toContain('通知负责人')
      expect(wrapper.text()).toContain('查看用量')
      expect(wrapper.text()).toContain('建议')
      expect(wrapper.text()).toContain('$103.50')
      expect(wrapper.text()).toContain('部门月预算')
    })

    it('stale 风险禁止直接调额', async () => {
      mockListBudgetRisks.mockResolvedValue({
        ...samplePage,
        items: [makeRiskEvent({ stale: true })],
        stale: true,
      })
      const wrapper = mountSection()
      await openFirstRisk(wrapper)

      expect(wrapper.get('[data-test="risk-adjust"]').attributes('disabled')).toBeDefined()
      expect(wrapper.text()).toContain('数据已过期')
      await wrapper.get('[data-test="risk-adjust"]').trigger('click')
      expect(wrapper.emitted('viewBudget')).toBeUndefined()
    })

    it('预算耗尽风险在抽屉中显示预算耗尽而不是已阻断', async () => {
      mockListBudgetRisks.mockResolvedValue({
        ...samplePage,
        items: [makeRiskEvent({ risk_type: 'budget_exhausted', severity: 'critical', blocked_requests: 0 })],
      })
      const wrapper = mountSection()
      await openFirstRisk(wrapper)

      expect(wrapper.text()).toContain('预算耗尽')
      expect(wrapper.text()).not.toContain('已阻断')
    })

    it('真实阻断风险在抽屉中显示已阻断', async () => {
      mockListBudgetRisks.mockResolvedValue({
        ...samplePage,
        items: [makeRiskEvent({ risk_type: 'request_blocked', severity: 'critical', blocked_requests: 3 })],
      })
      const wrapper = mountSection()
      await openFirstRisk(wrapper)

      expect(wrapper.text()).toContain('已阻断')
    })
  })

  describe('筛选功能', () => {
    it('渲染筛选器（周期、维度、类型、严重度、关键词）', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('全部周期')
      expect(html).toContain('全部维度')
      expect(html).toContain('全部类型')
      expect(html).toContain('全部严重度')
      expect(wrapper.find('[data-test="risk-filter-q"]').exists()).toBe(true)
      expect(wrapper.findAll('button').some(b => b.text() === '筛选')).toBe(false)
    })

    it('改下拉后立即重新加载并写入 URL', async () => {
      const wrapper = mountSection()
      await flushPromises()
      mockListBudgetRisks.mockClear()
      mockGetBudgetRiskSummary.mockClear()
      mockReplace.mockClear()

      const period = wrapper.findComponent('[data-test="risk-filter-period"]')
      await period.vm.$emit('update:modelValue', 'daily')
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenCalledWith(expect.objectContaining({
        period_type: 'daily',
        page: 1,
        page_size: 20,
      }))
      expect(mockReplace).toHaveBeenCalledWith(expect.objectContaining({
        query: expect.objectContaining({ rf_period: 'daily' }),
      }))
    })

    it('关键词防抖后携带 q 参数请求', async () => {
      const wrapper = mountSection()
      await flushPromises()
      mockListBudgetRisks.mockClear()

      const input = wrapper.get('[data-test="risk-filter-q"]').findComponent({ name: 'ElInput' })
      await input.setValue('研发')
      await flushPromises()
      expect(mockListBudgetRisks).not.toHaveBeenCalled()

      await vi.advanceTimersByTimeAsync(300)
      await flushPromises()
      expect(mockListBudgetRisks).toHaveBeenCalledWith(expect.objectContaining({ q: '研发', page: 1 }))
    })
  })

  describe('分页与排序', () => {
    it('渲染分页器并绑定 total', async () => {
      mockListBudgetRisks.mockResolvedValue({
        ...samplePage,
        total: 35,
        page: 1,
        page_size: 20,
      })
      const wrapper = mountSection()
      await flushPromises()

      const pagination = wrapper.findComponent({ name: 'ElPagination' })
      expect(pagination.exists()).toBe(true)
      expect(pagination.props('total')).toBe(35)
      expect(pagination.props('pageSize')).toBe(20)
    })

    it('切换页码时携带 page 参数重新请求', async () => {
      mockListBudgetRisks.mockResolvedValue({
        ...samplePage,
        total: 35,
        page: 1,
        page_size: 20,
      })
      const wrapper = mountSection()
      await flushPromises()

      const pagination = wrapper.findComponent({ name: 'ElPagination' })
      pagination.vm.$emit('update:current-page', 2)
      await flushPromises()
      pagination.vm.$emit('current-change', 2)
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 20 }))
    })

    it('表头排序携带 sort_by / sort_order', async () => {
      const wrapper = mountSection()
      await flushPromises()
      mockListBudgetRisks.mockClear()

      const table = wrapper.findComponent({ name: 'ElTable' })
      table.vm.$emit('sort-change', { prop: 'usage_ratio', order: 'descending' })
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenCalledWith(expect.objectContaining({
        sort_by: 'usage_ratio',
        sort_order: 'desc',
        page: 1,
      }))
    })
  })

  describe('刷新与自动刷新', () => {
    it('点击"刷新"重新加载数据', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const refreshBtn = wrapper.findAll('button').find(b => b.text() === '刷新')
      expect(refreshBtn).toBeDefined()
      await refreshBtn!.trigger('click')
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenCalledTimes(2)
      expect(mockGetBudgetRiskSummary).toHaveBeenCalledTimes(2)
    })

    it('开启自动刷新后按 30s 轮询', async () => {
      const wrapper = mountSection()
      await flushPromises()
      mockListBudgetRisks.mockClear()

      const auto = wrapper.findComponent('[data-test="risk-auto-refresh"] .el-switch')
      await auto.vm.$emit('update:modelValue', true)
      await flushPromises()

      await vi.advanceTimersByTimeAsync(30_000)
      await flushPromises()
      expect(mockListBudgetRisks).toHaveBeenCalled()
      expect(localStorage.getItem('budget-risk-auto-refresh')).toBe('1')
    })
  })

  describe('DataFreshnessNotice', () => {
    it('渲染数据新鲜度组件', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.findComponent({ name: 'DataFreshnessNotice' }).exists()).toBe(true)
    })
  })
})
