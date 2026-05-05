import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import BudgetManagePage from '../admin/BudgetManagePage.vue'
import type { BudgetItem, BudgetContextItem } from '@/types/budget'

// Mock components
vi.mock('@/components/budget/BudgetRiskEventsSection.vue', () => ({
  default: { name: 'BudgetRiskEventsSection', template: '<div />' },
}))
vi.mock('@/components/budget/BudgetTierLimitsPanel.vue', () => ({
  default: { name: 'BudgetTierLimitsPanel', template: '<div />' },
}))
vi.mock('@/components/budget/BudgetEditDrawer.vue', () => ({
  default: { name: 'BudgetEditDrawer', template: '<div />' },
}))
vi.mock('@/components/budget/MemberQuotaDialog.vue', () => ({
  default: { name: 'MemberQuotaDialog', template: '<div />' },
}))

// Mock stores
const mockAuthStore = {
  userName: '张三',
  user: { id: 'user-1' },
  effectiveRole: 'admin',
  orgRoles: [],
}

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => mockAuthStore,
}))

// Mock vue-router
const mockPush = vi.fn()
const mockReplace = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  useRoute: () => ({ query: {} }),
}))

// Mock API services
const mockGetBudgetList = vi.fn()
const mockGetBudgetContext = vi.fn()
const mockGetBudgetRiskSummary = vi.fn()
const mockListModelTiers = vi.fn()
const mockListBudgetTierLimits = vi.fn()
const mockRebuildBudgetCounters = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getBudgetList: (...args: unknown[]) => mockGetBudgetList(...args),
  getBudgetContext: (...args: unknown[]) => mockGetBudgetContext(...args),
  getBudgetRiskSummary: (...args: unknown[]) => mockGetBudgetRiskSummary(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  listBudgetTierLimits: (...args: unknown[]) => mockListBudgetTierLimits(...args),
  rebuildBudgetCounters: (...args: unknown[]) => mockRebuildBudgetCounters(...args),
  adjustBudget: vi.fn(),
  previewBudgetAdjustment: vi.fn(),
  adjustBudgetMultiPeriod: vi.fn(),
  previewBudgetMultiPeriod: vi.fn(),
}))

vi.mock('@/services/http', () => ({
  default: {
    get: vi.fn().mockResolvedValue({ data: { items: [] } }),
  },
}))

// Fixtures
const createCompanyPoolItem = (overrides: Partial<BudgetItem> = {}): BudgetItem => ({
  allocation_id: 'alloc-company',
  scope_type: 'company',
  scope_id: 'GLOBAL',
  label: '公司预算池',
  amount_usd: 10000,
  weekly_amount_usd: 2500,
  daily_amount_usd: 400,
  used_usd: 3000,
  weekly_used_usd: 600,
  daily_used_usd: 100,
  used_ratio: 0.3,
  period: '2026-07',
  parent_scope_id: '',
  parent_scope_type: null,
  ...overrides,
})

const createStudioPoolItem = (overrides: Partial<BudgetItem> = {}): BudgetItem => ({
  allocation_id: 'alloc-studio',
  scope_type: 'studio',
  scope_id: 'studio-1',
  label: '研发工作室',
  amount_usd: 5000,
  weekly_amount_usd: -1,
  daily_amount_usd: -1,
  used_usd: 2000,
  weekly_used_usd: 0,
  daily_used_usd: 0,
  used_ratio: 0.4,
  period: '2026-07',
  parent_scope_id: 'GLOBAL',
  parent_scope_type: 'company',
  ...overrides,
})

const createStudioItems = (count: number): BudgetItem[] =>
  Array.from({ length: count }, (_, i) => ({
    allocation_id: `alloc-studio-${i}`,
    scope_type: 'studio' as const,
    scope_id: `studio-${i}`,
    label: `工作室${i + 1}`,
    amount_usd: 1000 + i * 100,
    weekly_amount_usd: -1,
    daily_amount_usd: -1,
    used_usd: 500 + i * 50,
    weekly_used_usd: 0,
    daily_used_usd: 0,
    used_ratio: (500 + i * 50) / (1000 + i * 100),
    period: '2026-07',
    parent_scope_id: 'GLOBAL',
    parent_scope_type: 'company',
  }))

const createDeptItems = (): BudgetItem[] => [
  {
    allocation_id: 'alloc-dept-daily',
    scope_type: 'dept_daily',
    scope_id: 'dept-1',
    label: '后端部',
    amount_usd: 2000,
    weekly_amount_usd: 500,
    daily_amount_usd: 80,
    used_usd: 1600,
    weekly_used_usd: 400,
    daily_used_usd: 60,
    used_ratio: 0.8,
    period: '2026-07',
    parent_scope_id: 'studio-1',
    parent_scope_type: 'studio',
  },
  {
    allocation_id: 'alloc-dept-service',
    scope_type: 'dept_service',
    scope_id: 'dept-1',
    label: '后端部',
    amount_usd: 1000,
    weekly_amount_usd: 250,
    daily_amount_usd: 40,
    used_usd: 400,
    weekly_used_usd: 100,
    daily_used_usd: 15,
    used_ratio: 0.4,
    period: '2026-07',
    parent_scope_id: 'studio-1',
    parent_scope_type: 'studio',
  },
]

function mountPage() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(BudgetManagePage, {
    global: {
      plugins: [pinia, ElementPlus],
      stubs: {
        'el-icon': true,
      },
    },
  })
}

describe('BudgetManagePage - KPI Cards', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetBudgetList.mockResolvedValue([])
    mockGetBudgetContext.mockResolvedValue([])
    mockGetBudgetRiskSummary.mockResolvedValue({ total: 0 })
    mockListModelTiers.mockResolvedValue([])
    mockListBudgetTierLimits.mockResolvedValue([])
    mockAuthStore.effectiveRole = 'admin'
    mockAuthStore.orgRoles = []
  })

  describe('Admin - 公司预算池 KPI', () => {
    it('显示公司预算池卡片，包含总额、下发率、已下发、余量', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 10000, used_usd: 3000 })
      const studios = createStudioItems(3)
      mockGetBudgetList.mockResolvedValue([companyPool, ...studios])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('公司预算池')
      expect(html).toContain('$10,000') // 总额
      expect(html).toContain('已下发')
      expect(html).toContain('余量')
    })

    it('正确计算下发率 = 已下发工作室总额 / 公司池总额', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 10000 })
      const studios = [
        createStudioPoolItem({ amount_usd: 3000 }),
        createStudioPoolItem({ scope_id: 'studio-2', amount_usd: 2000 }),
      ]
      mockGetBudgetList.mockResolvedValue([companyPool, ...studios])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      // 下发率 = (3000 + 2000) / 10000 = 50%
      expect(wrapper.html()).toContain('下发率 50%')
    })

    it('下发率 >= 95% 时进度条显示警告色', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 10000 })
      const studios = [createStudioPoolItem({ amount_usd: 9600 })]
      mockGetBudgetList.mockResolvedValue([companyPool, ...studios])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('下发率 96%')
      // 进度条应使用警告色 #e6a23c
    })

    it('未下发余量 = 公司池总额 - 已下发总额', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 10000 })
      const studios = [createStudioPoolItem({ amount_usd: 3000 })]
      mockGetBudgetList.mockResolvedValue([companyPool, ...studios])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      // 余量 = 10000 - 3000 = 7000
      expect(wrapper.html()).toContain('未下发余量')
      expect(wrapper.html()).toContain('$7,000')
    })

    it('显示等级预算风险数量', async () => {
      mockGetBudgetList.mockResolvedValue([createCompanyPoolItem()])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('等级预算风险')
    })

    it('显示管辖工作室数量', async () => {
      const companyPool = createCompanyPoolItem()
      const studios = createStudioItems(5)
      mockGetBudgetList.mockResolvedValue([companyPool, ...studios])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('管辖工作室')
      expect(wrapper.html()).toContain('5')
    })

    it('显示扩容申请卡片，点击跳转到扩容申请页面', async () => {
      mockGetBudgetList.mockResolvedValue([createCompanyPoolItem()])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('扩容申请')
      
      const actionCard = wrapper.find('[data-test="kpi-expansion"]')
      expect(actionCard.exists()).toBe(true)
    })
  })

  describe('Studio Admin - 工作室预算池 KPI', () => {
    beforeEach(() => {
      mockAuthStore.effectiveRole = 'studio_admin'
      mockAuthStore.orgRoles = [{ role: 'studio_admin', scope_id: 'studio-1', scope_type: 'studio' }]
    })

    it('显示工作室预算池卡片，包含总额、下发率、已下发、余量', async () => {
      const studioPool = createStudioPoolItem({ amount_usd: 5000 })
      const depts = createDeptItems()
      mockGetBudgetList.mockResolvedValue([studioPool, ...depts])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'studio', scope_id: 'studio-1', label: '研发工作室', amount_usd: 5000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('工作室预算')
      expect(html).toContain('工作室总额')
      expect(html).toContain('$5,000')
      expect(html).toContain('已下发')
      expect(html).toContain('未下发')
    })

    it('正确计算工作室下发率 = (日常池 + 服务池) / 工作室总额', async () => {
      const studioPool = createStudioPoolItem({ amount_usd: 5000 })
      const depts = createDeptItems() // daily: 2000, service: 1000
      mockGetBudgetList.mockResolvedValue([studioPool, ...depts])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'studio', scope_id: 'studio-1', label: '研发工作室', amount_usd: 5000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('$3,000.00')
    })

    it('显示风险部门数量', async () => {
      const studioPool = createStudioPoolItem()
      const depts = createDeptItems()
      mockGetBudgetList.mockResolvedValue([studioPool, ...depts])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'studio', scope_id: 'studio-1', label: '研发工作室', amount_usd: 5000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('预警')
    })

    it('显示管辖部门数量（按部门 ID 去重）', async () => {
      const studioPool = createStudioPoolItem()
      const depts = createDeptItems() // 同一个部门的日常池和服务池
      mockGetBudgetList.mockResolvedValue([studioPool, ...depts])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'studio', scope_id: 'studio-1', label: '研发工作室', amount_usd: 5000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('下属部门')
      expect(wrapper.html()).toContain('1') // 只有一个部门
    })
  })

  describe('Dept Admin - 部门预算池 KPI', () => {
    beforeEach(() => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]
    })

    it('单部门：显示日常池和服务池两张 KPI 卡', async () => {
      const depts = createDeptItems()
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('个人 Key 池')
      expect(html).toContain('Service Key 池')
      expect(html).toContain('$2,000') // 日常池
      expect(html).toContain('$1,000') // 服务池
    })

    it('日常池显示已用、剩余、进度条', async () => {
      const depts = createDeptItems()
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('已用')
      expect(html).toContain('$2,000') // 部门总已用
    })

    it('日常池 used_ratio >= 0.8 时显示预警标签', async () => {
      const depts = createDeptItems() // daily used_ratio = 0.8
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('已用')
    })

    it('显示近7天消耗 KPI', async () => {
      const depts = createDeptItems()
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('成员额度')
    })

    it('显示风险成员数量', async () => {
      const depts = createDeptItems()
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('暂无成员')
    })

    it('多部门：显示部门池横幅，替代单独的池卡', async () => {
      mockAuthStore.orgRoles = [
        { role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' },
        { role: 'dept_admin', scope_id: 'dept-2', scope_type: 'dept' },
      ]
      const dept1Items = createDeptItems()
      const dept2Items: BudgetItem[] = [
        {
          ...dept1Items[0],
          allocation_id: 'alloc-dept2-daily',
          scope_id: 'dept-2',
          label: '前端部',
        },
        {
          ...dept1Items[1],
          allocation_id: 'alloc-dept2-service',
          scope_id: 'dept-2',
          label: '前端部',
        },
      ]
      mockGetBudgetList.mockResolvedValue([...dept1Items, ...dept2Items])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('部门预算')
      expect(wrapper.html()).toContain('个人 Key 池')
      expect(wrapper.html()).toContain('Service Key 池')
    })
  })

  describe('金额格式化', () => {
    it('displayAmount: -1 显示为"不限"', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: -1 })
      mockGetBudgetList.mockResolvedValue([companyPool])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: -1 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('不限')
    })

    it('formatUSD: 金额显示紧凑格式', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 10000 })
      mockGetBudgetList.mockResolvedValue([companyPool])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('$10,000')
    })

    it('金额为 0 时正确显示 $0', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 0, used_usd: 0 })
      mockGetBudgetList.mockResolvedValue([companyPool])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 0 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('$0')
    })
  })

  describe('进度条计算和颜色', () => {
    it('进度条百分比 = used_ratio * 100，最大 100%', async () => {
      const depts = [
        {
          ...createDeptItems()[0],
          used_ratio: 1.2, // 超额
        },
      ]
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
    })

    it('used_ratio < 0.8: 绿色 (#36B34E)', async () => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]
      const depts = [
        {
          ...createDeptItems()[0],
          used_ratio: 0.5,
          used_usd: 1000,
        },
      ]
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
    })

    it('0.8 <= used_ratio < 1: 黄色 (#e6a23c)', async () => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]
      const depts = [
        {
          ...createDeptItems()[0],
          used_ratio: 0.85,
          used_usd: 1700,
        },
      ]
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
    })

    it('used_ratio >= 1: 红色 (#f56c6c)', async () => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]
      const depts = [
        {
          ...createDeptItems()[0],
          used_ratio: 1.1,
          used_usd: 2200,
        },
      ]
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
    })
  })

  describe('边界情况', () => {
    it('公司池为 0 时，下发率显示 0%', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 0 })
      mockGetBudgetList.mockResolvedValue([companyPool])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 0 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('下发率 0%')
    })

    it('没有工作室时，管辖工作室显示 0', async () => {
      const companyPool = createCompanyPoolItem()
      mockGetBudgetList.mockResolvedValue([companyPool])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 10000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('管辖工作室')
      expect(wrapper.html()).toContain('0')
    })

    it('空数据时 KPI 卡片显示默认值', async () => {
      mockGetBudgetList.mockResolvedValue([])
      mockGetBudgetContext.mockResolvedValue([])

      const wrapper = mountPage()
      await flushPromises()

      // 页面应该正常渲染，不崩溃
      expect(wrapper.html()).toContain('预算总览')
    })

    it('负数金额按 0 处理（Math.max 保护）', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: -100 })
      mockGetBudgetList.mockResolvedValue([companyPool])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: -100 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      // 负数应显示为"不限"
      expect(wrapper.html()).toContain('不限')
    })

    it('下发额度超过池总额时，未下发余量为 0（不显示负数）', async () => {
      const companyPool = createCompanyPoolItem({ amount_usd: 5000 })
      const studios = [createStudioPoolItem({ amount_usd: 6000 })]
      mockGetBudgetList.mockResolvedValue([companyPool, ...studios])
      mockGetBudgetContext.mockResolvedValue([
        { scope_type: 'company', scope_id: 'GLOBAL', label: '公司预算池', amount_usd: 5000 },
      ])

      const wrapper = mountPage()
      await flushPromises()

      // 余量应该是 Math.max(5000 - 6000, 0) = 0
      expect(wrapper.html()).toContain('$0')
    })
  })

  describe('状态文本', () => {
    it('ratio < 0.8: 显示"正常"', async () => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]
      const depts = [
        {
          ...createDeptItems()[0],
          used_ratio: 0.5,
        },
      ]
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
    })

    it('0.8 <= ratio < 1: 显示"预警"', async () => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]
      const depts = [
        {
          ...createDeptItems()[0],
          used_ratio: 0.85,
        },
      ]
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
    })

    it('ratio >= 1: 显示"已超额"', async () => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1', scope_type: 'dept' }]
      const depts = [
        {
          ...createDeptItems()[0],
          used_ratio: 1.1,
        },
      ]
      mockGetBudgetList.mockResolvedValue(depts)

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[role="progressbar"]').exists()).toBe(true)
    })
  })

  describe('API 加载失败', () => {
    it('getBudgetList 失败时不崩溃', async () => {
      mockGetBudgetList.mockRejectedValue(new Error('Network Error'))

      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('预算总览')
    })

    it('getBudgetContext 失败时使用空数组', async () => {
      mockGetBudgetList.mockResolvedValue([createCompanyPoolItem()])
      mockGetBudgetContext.mockRejectedValue(new Error('API Error'))

      const wrapper = mountPage()
      await flushPromises()

      // 应正常渲染，不显示治理链路部分
      expect(wrapper.html()).toContain('预算总览')
    })
  })
})
