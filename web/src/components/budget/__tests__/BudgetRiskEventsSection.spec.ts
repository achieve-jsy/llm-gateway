import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import BudgetRiskEventsSection from '../BudgetRiskEventsSection.vue'
import type { BudgetRiskEvent, BudgetRiskPage, BudgetRiskSummary } from '@/types/budget-risk'

// Mock vue-router
const mockPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useRoute: () => ({ query: {} }),
}))

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
}))

// ── Mock API services ──────────────────────────────────────────────
const mockListBudgetRisks = vi.fn()
const mockGetBudgetRiskSummary = vi.fn()
const mockNotifyBudgetOwner = vi.fn()

vi.mock('@/services/control-plane', () => ({
  listBudgetRisks: (...args: unknown[]) => mockListBudgetRisks(...args),
  getBudgetRiskSummary: (...args: unknown[]) => mockGetBudgetRiskSummary(...args),
  notifyBudgetOwner: (...args: unknown[]) => mockNotifyBudgetOwner(...args),
}))

// Mock DataFreshnessNotice
vi.mock('@/components/common/DataFreshnessNotice.vue', () => ({
  default: {
    name: 'DataFreshnessNotice',
    template: '<div class="mock-freshness" />',
    props: ['stale', 'updatedAt'],
  },
}))

// ── Fixtures ───────────────────────────────────────────────────────
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
    makeRiskEvent({ id: 'risk-1', severity: 'warning', scope_name: '张三' }),
    makeRiskEvent({ id: 'risk-2', severity: 'critical', scope_name: '李四', risk_type: 'request_blocked', blocked_requests: 5 }),
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
  data_as_of: '2026-07-13T10:00:00Z',
  stale: false,
}

// ── Helpers ────────────────────────────────────────────────────────
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

// ── Tests ──────────────────────────────────────────────────────────
describe('BudgetRiskEventsSection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListBudgetRisks.mockResolvedValue({ ...samplePage })
    mockGetBudgetRiskSummary.mockResolvedValue({ ...sampleSummary })
    mockNotifyBudgetOwner.mockResolvedValue(undefined)
  })

  describe('风险摘要', () => {
    it('同时加载风险摘要和列表并显示 KPI', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenCalled()
      expect(mockGetBudgetRiskSummary).toHaveBeenCalled()
      expect(mockListBudgetRisks).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 50 }))
      expect(wrapper.text()).toContain('当前风险')
      expect(wrapper.text()).toContain('严重风险')
      expect(wrapper.text()).toContain('即将耗尽')
      expect(wrapper.text()).toContain('实际阻断')
      expect(wrapper.text()).toContain('阻断请求')
      expect(wrapper.get('[data-test="budget-kpi-blocked-requests"]').text()).toContain('5')
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

    it('空列表时显示"当前没有预算风险"', async () => {
      mockListBudgetRisks.mockResolvedValue({ items: [], total: 0, data_as_of: '', stale: false })
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.html()).toContain('当前没有预算风险')
    })

    it('显示额度和已用金额', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('$100.00')
      expect(html).toContain('$90.00')
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
      // We expect at least 2 tags (warning + critical from sample data)
      // warning uses type="warning", critical uses type="danger"
      const tagTypes = tags.map(t => t.classes()).flat()
      // el-tag renders with el-tag--warning or el-tag--danger classes
      expect(tagTypes.some(c => c.includes('warning'))).toBe(true)
      expect(tagTypes.some(c => c.includes('danger'))).toBe(true)
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

      // Simulate row click
      const rows = wrapper.findAll('.el-table__row')
      expect(rows.length).toBeGreaterThan(0)
      await rows[0].trigger('click')
      await flushPromises()

      // Drawer should be visible and contain details
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
    it('渲染筛选器（周期、维度、类型、严重度）', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('全部周期')
      expect(html).toContain('全部维度')
      expect(html).toContain('全部类型')
      expect(html).toContain('全部严重度')
    })

    it('点击"筛选"按钮重新加载数据', async () => {
      const wrapper = mountSection()
      await flushPromises()

      const filterBtn = wrapper.findAll('button').find(b => b.text() === '筛选')
      expect(filterBtn).toBeDefined()
      await filterBtn!.trigger('click')
      await flushPromises()

      expect(mockListBudgetRisks).toHaveBeenCalledTimes(2)
      expect(mockGetBudgetRiskSummary).toHaveBeenCalledTimes(2)
    })
  })

  describe('刷新按钮', () => {
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
  })

  describe('DataFreshnessNotice', () => {
    it('渲染数据新鲜度组件', async () => {
      const wrapper = mountSection()
      await flushPromises()

      expect(wrapper.findComponent({ name: 'DataFreshnessNotice' }).exists()).toBe(true)
    })
  })

  // TDD: The component's load() uses try/finally without catch,
  // so API errors produce unhandled rejections. This is a known bug
  // to be fixed separately.
})
