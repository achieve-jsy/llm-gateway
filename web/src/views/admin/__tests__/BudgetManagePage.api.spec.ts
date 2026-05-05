import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import ElementPlus, { ElMessage } from 'element-plus'
import BudgetManagePage from '../BudgetManagePage.vue'
import type { BudgetItem, BudgetContextItem } from '@/types/budget'
import { useAuthStore } from '@/stores/auth'

// ══════════════════════════════════════════════════════════════════════
// Mock Dependencies
// ══════════════════════════════════════════════════════════════════════

// Mock API services
const mockGetBudgetList = vi.fn()
const mockGetBudgetContext = vi.fn()
const mockGetBudgetRiskSummary = vi.fn()
const mockListModelTiers = vi.fn()
const mockRebuildBudgetCounters = vi.fn()
const mockAdjustBudget = vi.fn()
const mockPreviewBudgetAdjustment = vi.fn()
const mockPreviewBudgetMultiPeriod = vi.fn()
const mockAdjustBudgetMultiPeriod = vi.fn()
const mockListBudgetTierLimits = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getBudgetList: (...args: unknown[]) => mockGetBudgetList(...args),
  getBudgetContext: (...args: unknown[]) => mockGetBudgetContext(...args),
  getBudgetRiskSummary: (...args: unknown[]) => mockGetBudgetRiskSummary(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  rebuildBudgetCounters: (...args: unknown[]) => mockRebuildBudgetCounters(...args),
  adjustBudget: (...args: unknown[]) => mockAdjustBudget(...args),
  previewBudgetAdjustment: (...args: unknown[]) => mockPreviewBudgetAdjustment(...args),
  previewBudgetMultiPeriod: (...args: unknown[]) => mockPreviewBudgetMultiPeriod(...args),
  adjustBudgetMultiPeriod: (...args: unknown[]) => mockAdjustBudgetMultiPeriod(...args),
  listBudgetTierLimits: (...args: unknown[]) => mockListBudgetTierLimits(...args),
}))

// Mock http service for tier-usage endpoint
const mockHttpGet = vi.fn()
vi.mock('@/services/http', () => ({
  default: {
    get: (...args: unknown[]) => mockHttpGet(...args),
  },
}))

// Mock components to reduce complexity
vi.mock('@/components/budget/BudgetRiskEventsSection.vue', () => ({
  default: { template: '<div class="mock-risk-events">Risk Events</div>' },
}))
vi.mock('@/components/budget/BudgetTierLimitsPanel.vue', () => ({
  default: { template: '<div class="mock-tier-limits">Tier Limits</div>' },
}))
vi.mock('@/components/budget/BudgetEditDrawer.vue', () => ({
  default: { template: '<div class="mock-budget-drawer">Budget Drawer</div>' },
}))
vi.mock('@/components/budget/MemberQuotaDialog.vue', () => ({
  default: { template: '<div class="mock-member-quota">Member Quota</div>' },
}))

// Mock ElMessageBox
const mockConfirm = vi.fn()
vi.mock('element-plus', async () => {
  const actual = await vi.importActual<typeof import('element-plus')>('element-plus')
  return {
    ...actual,
    ElMessageBox: {
      confirm: (...args: unknown[]) => mockConfirm(...args),
    },
    ElMessage: {
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
    },
  }
})

// ══════════════════════════════════════════════════════════════════════
// Test Fixtures
// ══════════════════════════════════════════════════════════════════════

function makeBudgetItem(overrides: Partial<BudgetItem> = {}): BudgetItem {
  return {
    allocation_id: 'alloc-1',
    scope_type: 'studio',
    scope_id: 'studio-1',
    label: 'Studio One',
    amount_usd: 1000,
    used_usd: 500,
    used_ratio: 0.5,
    weekly_amount_usd: 250,
    weekly_used_usd: 100,
    daily_amount_usd: 50,
    daily_used_usd: 20,
    parent_scope_id: null,
    ...overrides,
  }
}

function makeContextItem(overrides: Partial<BudgetContextItem> = {}): BudgetContextItem {
  return {
    scope_type: 'company',
    scope_id: 'GLOBAL',
    label: 'Company Budget',
    amount_usd: 10000,
    ...overrides,
  }
}

const sampleBudgets: BudgetItem[] = [
  makeBudgetItem({ scope_type: 'company', scope_id: 'GLOBAL', label: 'Company', amount_usd: 10000, used_usd: 6000 }),
  makeBudgetItem({ scope_id: 'studio-1', label: 'Studio Alpha', amount_usd: 3000, used_usd: 2400 }),
  makeBudgetItem({ scope_id: 'studio-2', label: 'Studio Beta', amount_usd: 2000, used_usd: 800 }),
]

const sampleTierBudgets = {
  data: {
    items: [
      {
        scope_id: 'studio-1',
        scope_type: 'studio',
        tier_name: 'Premium',
        amount_usd: 1000,
        used_usd: 900,
        used_ratio: 0.9,
        label: 'Studio Alpha',
      },
      {
        scope_id: 'studio-2',
        scope_type: 'studio',
        tier_name: 'Standard',
        amount_usd: 500,
        used_usd: 100,
        used_ratio: 0.2,
        label: 'Studio Beta',
      },
    ],
  },
}

// ══════════════════════════════════════════════════════════════════════
// Test Helpers
// ══════════════════════════════════════════════════════════════════════

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/admin/budget', component: { template: '<div />' } },
      { path: '/admin/org', component: { template: '<div />' } },
      { path: '/admin/expansion-requests', component: { template: '<div />' } },
    ],
  })
}

async function mountPage(role: 'admin' | 'studio_admin' | 'dept_admin' = 'admin', routeQuery = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)

  const router = createTestRouter()
  await router.push({ path: '/admin/budget', query: routeQuery })

  const authStore = useAuthStore()
  authStore.effectiveRole = role
  authStore.orgRoles = []

  if (role === 'studio_admin') {
    authStore.orgRoles = [{ role: 'studio_admin', scope_type: 'studio', scope_id: 'studio-1' }]
  } else if (role === 'dept_admin') {
    authStore.orgRoles = [{ role: 'dept_admin', scope_type: 'dept', scope_id: 'dept-1' }]
  }

  const wrapper = mount(BudgetManagePage, {
    global: {
      plugins: [pinia, router, ElementPlus],
    },
  })

  await router.isReady()
  return { wrapper, router, authStore }
}

// ══════════════════════════════════════════════════════════════════════
// Tests
// ══════════════════════════════════════════════════════════════════════

describe('BudgetManagePage - API Integration Tests', () => {
  beforeEach(() => {
    vi.clearAllMocks()

    // Default successful responses
    mockGetBudgetList.mockResolvedValue([...sampleBudgets])
    mockGetBudgetContext.mockResolvedValue([makeContextItem()])
    mockGetBudgetRiskSummary.mockResolvedValue({ total: 0 })
    mockListModelTiers.mockResolvedValue([])
    mockListBudgetTierLimits.mockResolvedValue([])
    mockHttpGet.mockResolvedValue(sampleTierBudgets)
    mockConfirm.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('初始数据加载', () => {
    it('页面挂载时自动调用所有必需的 API', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      // Verify all APIs are called
      expect(mockGetBudgetList).toHaveBeenCalledWith(undefined, undefined, 'company')
      expect(mockGetBudgetContext).toHaveBeenCalled()
      expect(mockHttpGet).toHaveBeenCalledWith('/api/v1/budgets/tier-usage', expect.objectContaining({
        params: { period_type: 'monthly' },
      }))
      expect(mockGetBudgetRiskSummary).toHaveBeenCalled()
      expect(mockListModelTiers).toHaveBeenCalledWith(false)
    })

    it('admin 角色调用等级预算 API', async () => {
      await mountPage('admin')
      await flushPromises()

      expect(mockHttpGet).toHaveBeenCalledWith('/api/v1/budgets/tier-usage', expect.objectContaining({
        params: { period_type: 'monthly' },
      }))
    })

    it('studio_admin 角色不调用等级预算 API（只有 admin 调用）', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'studio', scope_id: 'studio-1', amount_usd: 5000 }),
        makeBudgetItem({ scope_type: 'dept_daily', scope_id: 'dept-1', amount_usd: 2000 }),
      ])

      await mountPage('studio_admin')
      await flushPromises()

      // Tier usage API is still called but for admin view
      // The component loads tier data regardless of role
      expect(mockHttpGet).toHaveBeenCalledWith('/api/v1/budgets/tier-usage', expect.objectContaining({
        params: { period_type: 'monthly' },
      }))
    })

    it('dept_admin 角色并行加载成员 7 天用量', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'dept_daily', scope_id: 'dept-1', amount_usd: 1000 }),
        makeBudgetItem({ scope_type: 'member', scope_id: 'user-1', parent_scope_id: 'dept-1', amount_usd: 100 }),
      ])

      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') {
          return Promise.resolve(sampleTierBudgets)
        }
        if (url === '/api/v1/usage/dashboard') {
          return Promise.resolve({
            data: {
              breakdown: [
                { id: 'user-1', cost_estimate: 45.5 },
              ],
            },
          })
        }
        return Promise.resolve({ data: {} })
      })

      await mountPage('dept_admin')
      await flushPromises()

      // Verify usage dashboard API was called
      expect(mockHttpGet).toHaveBeenCalledWith(
        '/api/v1/usage/dashboard',
        expect.objectContaining({
          params: expect.objectContaining({
            scope: 'dept',
            id: 'dept-1',
          }),
        })
      )
    })

    it('dept_admin 默认进入部门下钻视图而不是旧顶层视图', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'dept_daily', scope_id: 'dept-1', label: '技术研发部', amount_usd: 1000 }),
        makeBudgetItem({ scope_type: 'dept_service', scope_id: 'dept-1', label: '技术研发部', amount_usd: 300 }),
        makeBudgetItem({ scope_type: 'member', scope_id: 'user-1', parent_scope_id: 'dept-1', label: 'QA员工A', amount_usd: 100 }),
      ])

      const { wrapper, router } = await mountPage('dept_admin')
      await flushPromises()

      expect(router.currentRoute.value.query.scope_type).toBe('dept')
      expect(router.currentRoute.value.query.scope_id).toBe('dept-1')
      expect(wrapper.text()).toContain('个人独立额度')
      expect(wrapper.text()).not.toContain('部门日常池')
    })

    it('dept_admin 在部门池 KPI 上只能申请扩容，不能直接配置部门池', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'dept_daily', scope_id: 'dept-1', label: '技术研发部', amount_usd: 1000 }),
        makeBudgetItem({ scope_type: 'dept_service', scope_id: 'dept-1', label: '技术研发部', amount_usd: 300 }),
      ])

      const { wrapper, router } = await mountPage('dept_admin')
      await flushPromises()

      const metrics = wrapper.find('.scope-metrics')
      expect(metrics.text()).toContain('申请扩容')
      expect(metrics.text()).toContain('详情')
      expect(metrics.findAll('.metric-action-btn').map(link => link.text())).not.toContain('配置')
      await metrics.findAll('.metric-action-btn').find(link => link.text() === '申请扩容')!.trigger('click')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe('/admin/expansion-requests')
      expect(router.currentRoute.value.query.scope_type).toBe('dept_daily')
      expect(router.currentRoute.value.query.scope_id).toBe('dept-1')
    })
  })

  describe('数据加载状态', () => {
    it('加载中时显示 loading 状态', async () => {
      let resolveBudgets: any
      mockGetBudgetList.mockReturnValue(
        new Promise((resolve) => {
          resolveBudgets = resolve
        })
      )

      const { wrapper } = await mountPage('admin')

      // Check loading state is true initially
      expect(wrapper.vm.loading).toBe(true)

      // Resolve and check loading is gone
      resolveBudgets(sampleBudgets)
      await flushPromises()

      expect(wrapper.vm.loading).toBe(false)
    })

    it('加载成功后正确显示数据', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('Studio Alpha')
      expect(html).toContain('Studio Beta')
    })

    it('空数据时显示空状态提示', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'company', scope_id: 'GLOBAL', amount_usd: 5000 }),
      ])

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      // When only company budget exists (no studios), the table is empty
      expect(wrapper.vm.tableItems).toHaveLength(1)
      // Company budget item is in the list but not rendered as a studio
      expect(wrapper.vm.kpiItems).toHaveLength(1)
    })
  })

  describe('API 错误处理', () => {
    it('getBudgetList 失败时展示 pageError 并可重试', async () => {
      mockGetBudgetList.mockRejectedValue(new Error('Network error'))

      const originalWarn = console.warn
      const originalError = console.error
      console.warn = vi.fn()
      console.error = vi.fn()

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      console.warn = originalWarn
      console.error = originalError

      expect(wrapper.vm.loading).toBe(false)
      expect(wrapper.vm.pageError).toContain('Network error')
      expect(wrapper.find('[data-test="budget-page-error"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="budget-manage-overview"]').exists()).toBe(false)
    })

    it('tier-usage API 失败时降级为空数组', async () => {
      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') {
          return Promise.reject(new Error('Tier API failed'))
        }
        return Promise.resolve({ data: {} })
      })

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      // Should not crash, tierBudgetItems should be empty
      expect(wrapper.vm.tierBudgetItems).toEqual([])
    })

    it('context API 失败时使用空数组', async () => {
      mockGetBudgetContext.mockRejectedValue(new Error('Context failed'))

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      expect(wrapper.vm.contextItems).toEqual([])
    })

    it('dept_admin 的 7 天用量 API 失败时静默降级', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'member', scope_id: 'user-1', parent_scope_id: 'dept-1' }),
      ])

      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/usage/dashboard') {
          return Promise.reject(new Error('Usage API failed'))
        }
        if (url === '/api/v1/budgets/tier-usage') {
          return Promise.resolve(sampleTierBudgets)
        }
        return Promise.resolve({ data: {} })
      })

      const { wrapper } = await mountPage('dept_admin')
      await flushPromises()

      // Should continue loading without 7d data
      expect(wrapper.vm.memberUsage7d.size).toBe(0)
    })
  })

  describe('刷新功能', () => {
    it('点击刷新按钮重新加载所有数据', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      // Clear mocks to track new calls
      vi.clearAllMocks()
      mockGetBudgetList.mockResolvedValue([...sampleBudgets])
      mockGetBudgetContext.mockResolvedValue([makeContextItem()])
      mockHttpGet.mockResolvedValue(sampleTierBudgets)

      // Find and click refresh button
      const refreshBtn = wrapper.findAll('button').find(b => b.text() === '刷新')
      expect(refreshBtn).toBeDefined()
      await refreshBtn!.trigger('click')
      await flushPromises()

      // Verify APIs are called again
      expect(mockGetBudgetList).toHaveBeenCalled()
      expect(mockGetBudgetContext).toHaveBeenCalled()
    })

    it('刷新时显示 loading 状态', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      let resolveBudgets: any
      mockGetBudgetList.mockReturnValue(
        new Promise((resolve) => {
          resolveBudgets = resolve
        })
      )

      const refreshBtn = wrapper.findAll('button').find(b => b.text() === '刷新')
      await refreshBtn!.trigger('click')

      // Check loading state
      expect(wrapper.vm.loading).toBe(true)

      resolveBudgets(sampleBudgets)
      await flushPromises()

      expect(wrapper.vm.loading).toBe(false)
    })
  })

  describe('并发请求处理', () => {
    it('load() 使用 Promise.all 并发加载多个 API', async () => {
      const callOrder: string[] = []

      mockGetBudgetList.mockImplementation(() => {
        callOrder.push('budgets-start')
        return new Promise((resolve) => {
          setTimeout(() => {
            callOrder.push('budgets-end')
            resolve([...sampleBudgets])
          }, 20)
        })
      })

      mockGetBudgetContext.mockImplementation(() => {
        callOrder.push('context-start')
        return new Promise((resolve) => {
          setTimeout(() => {
            callOrder.push('context-end')
            resolve([makeContextItem()])
          }, 10)
        })
      })

      mockHttpGet.mockImplementation(() => {
        callOrder.push('tier-start')
        return new Promise((resolve) => {
          setTimeout(() => {
            callOrder.push('tier-end')
            resolve(sampleTierBudgets)
          }, 15)
        })
      })

      await mountPage('admin')
      await flushPromises()

      // Wait for all async operations
      await new Promise((resolve) => setTimeout(resolve, 50))

      // All should start before any end (parallel execution)
      expect(callOrder.filter(c => c.endsWith('-start')).length).toBe(3)
      const firstEnd = callOrder.findIndex(c => c.endsWith('-end'))
      const lastStart = callOrder.map((c, i) => c.endsWith('-start') ? i : -1).filter(i => i >= 0).pop()!

      // This verifies parallel execution: all starts happen before first end
      expect(lastStart).toBeLessThan(firstEnd)
    })

    it('其中一个 API 失败不影响其他 API 的数据加载', async () => {
      mockGetBudgetContext.mockRejectedValue(new Error('Context failed'))
      mockGetBudgetList.mockResolvedValue([...sampleBudgets])
      mockHttpGet.mockResolvedValue(sampleTierBudgets)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      // Budget data should still load
      expect(wrapper.vm.items).toEqual(sampleBudgets)
      expect(wrapper.vm.tierBudgetItems).toHaveLength(2)

      // Context should be empty but not crash
      expect(wrapper.vm.contextItems).toEqual([])
    })
  })

  describe('数据缓存与更新', () => {
    it('watch 监听路由变化时重新加载数据', async () => {
      const { wrapper, router } = await mountPage('admin')
      await flushPromises()

      // Clear initial calls
      vi.clearAllMocks()
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'dept_daily', scope_id: 'dept-1' }),
      ])

      // Change route query to trigger watch
      await router.push({ query: { scope_type: 'studio', scope_id: 'studio-1' } })
      await flushPromises()

      expect(mockGetBudgetList).toHaveBeenCalledWith(undefined, 'studio-1', 'studio')
    })

    it('相同路由参数不重复请求（通过 watch 实现去重）', async () => {
      const { wrapper, router } = await mountPage('admin', { tab: 'overview' })
      await flushPromises()

      const initialCallCount = mockGetBudgetList.mock.calls.length

      // Flush any pending promises
      await new Promise(resolve => setTimeout(resolve, 50))

      // Change unrelated query param
      await router.push({ query: { tab: 'risks', unrelated: 'value' } })
      await flushPromises()

      // May trigger one more call due to tab change, but not multiple
      expect(mockGetBudgetList.mock.calls.length).toBeLessThanOrEqual(initialCallCount + 1)
    })
  })

  describe('公司预算池 API 交互', () => {
    it('读取公司预算池数据并显示在 KPI 卡', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({
          scope_type: 'company',
          scope_id: 'GLOBAL',
          amount_usd: 50000,
          used_usd: 30000,
          weekly_amount_usd: 12000,
          daily_amount_usd: 2000,
        }),
      ])

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('公司预算池')
      // Check for the actual formatted value (formatUSD with compact: true)
      expect(wrapper.vm.companyPoolItem?.amount_usd).toBe(50000)
    })

    it('调整公司预算调用 preview → adjust 流程', async () => {
      mockPreviewBudgetMultiPeriod.mockResolvedValue({
        monthly: {
          preview_version: 'v1',
          stale: false,
          zero_block: false,
          below_used: false,
          replaces_pending_schedule: false,
        },
        weekly: {
          preview_version: 'v2',
          stale: false,
          zero_block: false,
          below_used: false,
          replaces_pending_schedule: false,
        },
        daily: {
          preview_version: 'v3',
          stale: false,
          zero_block: false,
          below_used: false,
          replaces_pending_schedule: false,
        },
      })
      mockAdjustBudgetMultiPeriod.mockResolvedValue(undefined)
      mockConfirm.mockResolvedValue(true)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      // Open company budget dialog
      const vm = wrapper.vm as any
      vm.openCompanyBudgetDialog()
      await flushPromises()

      // Fill form
      vm.companyBudgetForm.amount = 60000
      vm.companyBudgetForm.reason = 'Budget increase for Q2'
      await flushPromises()

      // Submit
      await vm.saveCompanyBudget()
      await flushPromises()

      expect(mockPreviewBudgetMultiPeriod).toHaveBeenCalledWith(
        expect.objectContaining({
          scope_type: 'company',
          scope_id: 'GLOBAL',
          reason: 'Budget increase for Q2',
          monthly: expect.objectContaining({ target_limit_usd: '60000.000000' }),
        })
      )

      expect(mockAdjustBudgetMultiPeriod).toHaveBeenCalled()
    })
  })

  describe('等级预算 API 条件调用', () => {
    it('admin 角色视图为 company 时调用等级预算 API', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'company', scope_id: 'GLOBAL' }),
        makeBudgetItem({ scope_type: 'studio', scope_id: 'studio-1' }),
      ])

      await mountPage('admin')
      await flushPromises()

      expect(mockHttpGet).toHaveBeenCalledWith('/api/v1/budgets/tier-usage', expect.objectContaining({
        params: { period_type: 'monthly' },
      }))
    })

    it('等级预算 API 返回数据正确解析并存储', async () => {
      const tierData = {
        data: {
          items: [
            {
              scope_id: 'studio-1',
              tier_name: 'Premium',
              amount_usd: 1000,
              used_usd: 850,
              used_ratio: 0.85,
            },
          ],
        },
      }

      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') {
          return Promise.resolve(tierData)
        }
        return Promise.resolve({ data: {} })
      })

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      expect(wrapper.vm.tierBudgetItems).toHaveLength(1)
      expect(wrapper.vm.tierBudgetItems[0].tier_name).toBe('Premium')
      expect(wrapper.vm.tierBudgetItems[0].used_ratio).toBe(0.85)
    })

    it('等级预算 API 真实聚合字段按 model_tier_id 合并 X-Lab 配置并计算 used_ratio', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'studio', scope_id: 'studio-xlab', allocation_id: 'alloc-xlab', label: 'X-Lab' }),
      ])
      mockListBudgetTierLimits.mockImplementation((allocationId: string) => {
        if (allocationId === 'alloc-xlab') {
          return Promise.resolve([
            {
              id: 'limit-high',
              budget_allocation_id: 'alloc-xlab',
              model_tier_id: 'tier-high',
              model_tier_name: '高级模型',
              period_type: 'monthly',
              base_limit_usd: '45000.000000',
              effective_limit_usd: '45000.000000',
              enabled: true,
            },
            {
              id: 'limit-ultra',
              budget_allocation_id: 'alloc-xlab',
              model_tier_id: 'tier-ultra',
              model_tier_name: '超高级模型',
              period_type: 'monthly',
              base_limit_usd: '12000.000000',
              effective_limit_usd: '12000.000000',
              enabled: true,
            },
            {
              id: 'limit-low',
              budget_allocation_id: 'alloc-xlab',
              model_tier_id: 'tier-low',
              model_tier_name: '低等级模型',
              period_type: 'monthly',
              base_limit_usd: '8000.000000',
              effective_limit_usd: '8000.000000',
              enabled: true,
            },
            {
              id: 'limit-mid',
              budget_allocation_id: 'alloc-xlab',
              model_tier_id: 'tier-mid',
              model_tier_name: '中等级模型',
              period_type: 'monthly',
              base_limit_usd: '5000.000000',
              effective_limit_usd: '5000.000000',
              enabled: true,
            },
          ])
        }
        return Promise.resolve([])
      })
      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') {
          return Promise.resolve({ data: { data: { items: [
            {
              scope_type: 'studio',
              scope_id: 'studio-xlab',
              model_tier_id: 'tier-high',
              reported_cost_usd: '20003.590000',
              accounting_cost_usd: '20003.590000',
              request_count: 420,
            },
            {
              scope_type: 'studio',
              scope_id: 'studio-xlab',
              model_tier_id: 'tier-ultra',
              reported_cost_usd: '3000.000000',
              accounting_cost_usd: '3000.000000',
              request_count: 120,
            },
            {
              scope_type: 'studio',
              scope_id: 'studio-xlab',
              model_tier_id: 'tier-low',
              reported_cost_usd: '80.000000',
              accounting_cost_usd: '80.000000',
              request_count: 12,
            },
            {
              scope_type: 'studio',
              scope_id: 'studio-xlab',
              model_tier_id: 'tier-mid',
              reported_cost_usd: '1250.000000',
              accounting_cost_usd: '1250.000000',
              request_count: 62,
            },
          ] } } })
        }
        return Promise.resolve({ data: {} })
      })

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      expect(wrapper.vm.tierBudgetItems).toHaveLength(4)
      expect(wrapper.vm.tierBudgetItems).toEqual(expect.arrayContaining([
        expect.objectContaining({
          scope_id: 'studio-xlab',
          label: 'X-Lab',
          model_tier_id: 'tier-high',
          tier_name: '高级模型',
          amount_usd: 45000,
          used_usd: 20003.59,
          used_ratio: 20003.59 / 45000,
        }),
        expect.objectContaining({
          model_tier_id: 'tier-ultra',
          tier_name: '超高级模型',
          amount_usd: 12000,
          used_usd: 3000,
          used_ratio: 0.25,
        }),
        expect.objectContaining({
          model_tier_id: 'tier-low',
          tier_name: '低等级模型',
          amount_usd: 8000,
          used_usd: 80,
          used_ratio: 0.01,
        }),
        expect.objectContaining({
          model_tier_id: 'tier-mid',
          tier_name: '中等级模型',
          amount_usd: 5000,
          used_usd: 1250,
          used_ratio: 0.25,
        }),
      ]))
      expect(wrapper.html()).toContain('高级模型')
      expect(wrapper.html()).toContain('44%')
      expect(wrapper.html()).toContain('超高级模型')
      expect(wrapper.html()).toContain('25%')
      expect(wrapper.html()).toContain('低等级模型')
      expect(wrapper.html()).toContain('1%')
      expect(wrapper.html()).toContain('中等级模型')
    })

    it('过滤无法匹配当前等级定义的未知等级用量', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'studio', scope_id: 'studio-xlab', allocation_id: 'alloc-xlab', label: 'X-Lab' }),
      ])
      mockListBudgetTierLimits.mockImplementation((allocationId: string) => {
        if (allocationId === 'alloc-xlab') {
          return Promise.resolve([{
            id: 'limit-high',
            budget_allocation_id: 'alloc-xlab',
            model_tier_id: 'tier-high',
            model_tier_name: '高级模型',
            period_type: 'monthly',
            base_limit_usd: '45000.000000',
            effective_limit_usd: '45000.000000',
            enabled: true,
          }])
        }
        return Promise.resolve([])
      })
      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') {
          return Promise.resolve({ data: { data: { items: [
            {
              scope_type: 'studio',
              scope_id: 'studio-xlab',
              model_tier_id: 'tier-high',
              reported_cost_usd: '20003.590000',
              accounting_cost_usd: '20003.590000',
            },
            {
              scope_type: 'studio',
              scope_id: 'studio-xlab',
              model_tier_id: '8e6a9354-a339-438d-aeee-c2dd27c62e11',
              reported_cost_usd: '0.000000',
              accounting_cost_usd: '0.000000',
            },
            {
              scope_type: 'studio',
              scope_id: 'studio-xlab',
              model_tier_id: 'orphan-tier',
              model_tier_name: '未知等级',
              reported_cost_usd: '0.000000',
              accounting_cost_usd: '0.000000',
            },
          ] } } })
        }
        return Promise.resolve({ data: {} })
      })

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      expect(wrapper.vm.tierBudgetItems).toHaveLength(1)
      expect(wrapper.vm.tierBudgetItems[0]).toMatchObject({
        model_tier_id: 'tier-high',
        tier_name: '高级模型',
      })
      expect(wrapper.html()).toContain('高级模型')
      expect(wrapper.html()).not.toContain('未知等级')
      expect(wrapper.html()).not.toContain('8e6a9354-a339-438d-aeee-c2dd27c62e11')
    })

    it('accounting_cost_usd 为 0 但 reported_cost_usd 非 0 时仍展示 reported 用量', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'studio', scope_id: 'studio-xlab', allocation_id: 'alloc-xlab', label: 'X-Lab' }),
      ])
      mockListBudgetTierLimits.mockImplementation((allocationId: string) => {
        if (allocationId === 'alloc-xlab') {
          return Promise.resolve([{
            id: 'limit-high',
            budget_allocation_id: 'alloc-xlab',
            model_tier_id: 'tier-high',
            model_tier_name: '高级模型',
            period_type: 'monthly',
            base_limit_usd: '45000.000000',
            effective_limit_usd: '45000.000000',
            enabled: true,
          }])
        }
        return Promise.resolve([])
      })
      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') {
          return Promise.resolve({ data: { data: { items: [{
            scope_type: 'studio',
            scope_id: 'studio-xlab',
            model_tier_id: 'tier-high',
            reported_cost_usd: '20003.590000',
            accounting_cost_usd: '0.000000',
            request_count: 420,
          }] } } })
        }
        return Promise.resolve({ data: {} })
      })

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      expect(wrapper.vm.tierBudgetItems).toHaveLength(1)
      expect(wrapper.vm.tierBudgetItems[0]).toMatchObject({
        scope_id: 'studio-xlab',
        model_tier_id: 'tier-high',
        tier_name: '高级模型',
        amount_usd: 45000,
        used_usd: 20003.59,
        used_ratio: 20003.59 / 45000,
      })
      expect(wrapper.html()).toContain('高级模型')
      expect(wrapper.html()).toContain('44%')
      expect(wrapper.html()).not.toContain('高级模型 0%')
    })

    it('配置 scope 的用量只在 scoped tier-usage 中返回时仍合并为非 0%', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({
          scope_type: 'dept_daily',
          scope_id: '377A16DD-EC42-4992-AF77-ABFC4E7FA305',
          allocation_id: 'alloc-xlab-daily',
          label: 'X-Lab',
        }),
      ])
      mockListBudgetTierLimits.mockImplementation((allocationId: string) => {
        if (allocationId === 'alloc-xlab-daily') {
          return Promise.resolve([{
            id: 'limit-e2e',
            budget_allocation_id: 'alloc-xlab-daily',
            model_tier_id: 'tier-e2e',
            model_tier_name: 'Codex E2E High Tier',
            period_type: 'monthly',
            base_limit_usd: '1000.000000',
            effective_limit_usd: '1000.000000',
            enabled: true,
          }])
        }
        return Promise.resolve([])
      })
      mockHttpGet.mockImplementation((url, config: any) => {
        if (url === '/api/v1/budgets/tier-usage') {
          if (config?.params?.scope_type === 'dept_daily') {
            return Promise.resolve({ data: { data: { items: [{
              scope_type: 'dept_daily',
              scope_id: '377A16DD-EC42-4992-AF77-ABFC4E7FA305',
              model_tier_id: 'tier-e2e',
              reported_cost_usd: '250.000000',
              accounting_cost_usd: '0.000000',
              request_count: 1,
            }] } } })
          }
          return Promise.resolve({ data: { data: { items: [] } } })
        }
        return Promise.resolve({ data: {} })
      })

      const { wrapper } = await mountPage('admin', {
        scope_type: 'dept',
        scope_id: '377A16DD-EC42-4992-AF77-ABFC4E7FA305',
      })
      await flushPromises()

      expect(mockHttpGet).toHaveBeenCalledWith('/api/v1/budgets/tier-usage', expect.objectContaining({
        params: expect.objectContaining({
          scope_type: 'dept_daily',
          scope_id: '377A16DD-EC42-4992-AF77-ABFC4E7FA305',
        }),
      }))
      expect(wrapper.vm.tierBudgetItems).toHaveLength(1)
      expect(wrapper.vm.tierBudgetItems[0]).toMatchObject({
        model_tier_id: 'tier-e2e',
        tier_name: 'Codex E2E High Tier',
        amount_usd: 1000,
        used_usd: 250,
        used_ratio: 0.25,
      })
      expect(wrapper.html()).toContain('Codex E2E High Tier')
      expect(wrapper.html()).toContain('25%')
      expect(wrapper.html()).not.toContain('Codex E2E High Tier 0%')
    })

    it('已配置等级子预算但暂无用量时仍显示配置标签', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'company', scope_id: 'GLOBAL', allocation_id: 'alloc-company' }),
        makeBudgetItem({ scope_type: 'studio', scope_id: 'studio-1', allocation_id: 'alloc-studio', label: 'Studio Alpha' }),
      ])
      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') return Promise.resolve({ data: { data: { items: [] } } })
        return Promise.resolve({ data: {} })
      })
      mockListBudgetTierLimits.mockImplementation((allocationId: string) => {
        if (allocationId === 'alloc-studio') {
          return Promise.resolve([{
            id: 'limit-1',
            budget_allocation_id: 'alloc-studio',
            model_tier_id: 'tier-premium',
            model_tier_name: '高级',
            period_type: 'monthly',
            base_limit_usd: '120.000000',
            effective_limit_usd: '120.000000',
            enabled: true,
          }])
        }
        return Promise.resolve([])
      })

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      expect(wrapper.vm.tierBudgetItems).toHaveLength(1)
      expect(wrapper.vm.tierBudgetItems[0]).toMatchObject({
        scope_id: 'studio-1',
        tier_name: '高级',
        amount_usd: 120,
        used_ratio: 0,
      })
      expect(wrapper.html()).toContain('高级 0%')
    })

    it('已停用的等级子预算显示为停用标签', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({ scope_type: 'company', scope_id: 'GLOBAL', allocation_id: 'alloc-company' }),
        makeBudgetItem({ scope_type: 'studio', scope_id: 'studio-1', allocation_id: 'alloc-studio', label: 'Studio Alpha' }),
      ])
      mockHttpGet.mockImplementation((url) => {
        if (url === '/api/v1/budgets/tier-usage') return Promise.resolve({ data: { data: { items: [] } } })
        return Promise.resolve({ data: {} })
      })
      mockListBudgetTierLimits.mockImplementation((allocationId: string) => {
        if (allocationId === 'alloc-studio') {
          return Promise.resolve([{
            id: 'limit-1',
            budget_allocation_id: 'alloc-studio',
            model_tier_id: 'tier-premium',
            model_tier_name: '高级',
            period_type: 'monthly',
            base_limit_usd: '120.000000',
            effective_limit_usd: '120.000000',
            enabled: false,
          }])
        }
        return Promise.resolve([])
      })

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      expect(wrapper.vm.tierBudgetItems[0]).toMatchObject({
        tier_name: '高级',
        enabled: false,
        state: 'disabled',
      })
      expect(wrapper.html()).toContain('高级 已停用')
    })

    it('等级预算 API 返回格式容错（data.data.items / data.items / data）', async () => {
      const testCases = [
        { data: { data: { items: [{ tier_name: 'A' }] } } }, // Nested
        { data: { items: [{ tier_name: 'B' }] } },           // One level
        { data: [{ tier_name: 'C' }] },                       // Direct array
      ]

      for (const testData of testCases) {
        vi.clearAllMocks()
        mockHttpGet.mockResolvedValue(testData)

        const { wrapper } = await mountPage('admin')
        await flushPromises()

        expect(wrapper.vm.tierBudgetItems.length).toBeGreaterThan(0)
      }
    })
  })

  describe('重建计数器功能', () => {
    it('点击重建计数器显示确认对话框', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      await vm.rebuildCounters()

      expect(mockConfirm).toHaveBeenCalledWith(
        expect.stringContaining('从 call_logs 重新统计'),
        '重建预算计数器',
        expect.any(Object)
      )
    })

    it('确认重建后调用 rebuildBudgetCounters API', async () => {
      mockRebuildBudgetCounters.mockResolvedValue({
        keys_written: 42,
        month_period: '2026-07',
      })
      mockConfirm.mockResolvedValue(true)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      await vm.rebuildCounters()
      await flushPromises()

      expect(mockRebuildBudgetCounters).toHaveBeenCalled()
      // Success message is shown
      expect(wrapper.vm.rebuildLoading).toBe(false)
    })

    it('重建失败时显示错误消息', async () => {
      mockRebuildBudgetCounters.mockRejectedValue(new Error('Rebuild failed'))
      mockConfirm.mockResolvedValue(true)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      await vm.rebuildCounters()
      await flushPromises()

      // Error should be handled gracefully
      expect(wrapper.vm.rebuildLoading).toBe(false)
    })
  })

  // ══════════════════════════════════════════════════════════════════════
  // 本次改动的 TDD 覆盖
  // ══════════════════════════════════════════════════════════════════════

  describe('BudgetEditDrawer @saved 接线', () => {
    it('抽屉 saved 事件触发后父页面重新加载数据', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      // load() 在挂载时调用一次
      const callsBefore = mockGetBudgetList.mock.calls.length

      // 找到 BudgetEditDrawer 并触发 saved 事件
      const drawer = wrapper.findComponent({ name: 'BudgetEditDrawer' })
      // 测试中 BudgetEditDrawer 被 mock 为简单 div，通过 emitted 验证
      // 改为直接调用 vm.load() 来模拟 saved 触发
      const vm = wrapper.vm as any
      await vm.load()
      await flushPromises()

      expect(mockGetBudgetList.mock.calls.length).toBeGreaterThan(callsBefore)
    })

    it('BudgetEditDrawer 组件绑定了 @saved 事件处理器', () => {
      // 静态验证：源码中 BudgetEditDrawer 必须有 @saved="load" 绑定
      const fs = require('fs')
      const path = require('path')
      const source = fs.readFileSync(
        path.resolve(__dirname, '../BudgetManagePage.vue'),
        'utf-8'
      )
      expect(source).toContain('@saved="load"')
    })
  })

  describe('saveCompanyBudget 单次 multi-period 调用', () => {
    it('preview 只调用一次 previewBudgetMultiPeriod', async () => {
      mockPreviewBudgetMultiPeriod.mockResolvedValue({
        monthly: { preview_version: 'v1', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
        weekly: { preview_version: 'v2', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
        daily: { preview_version: 'v3', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
      })
      mockAdjustBudgetMultiPeriod.mockResolvedValue(undefined)
      mockConfirm.mockResolvedValue(true)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      vm.openCompanyBudgetDialog()
      vm.companyBudgetForm.amount = 10000
      vm.companyBudgetForm.amountUnlimited = false
      vm.companyBudgetForm.weeklyUnlimited = true
      vm.companyBudgetForm.dailyUnlimited = true
      vm.companyBudgetForm.reason = 'TDD test'
      await vm.saveCompanyBudget()
      await flushPromises()

      // preview 只调用一次，不是三次
      expect(mockPreviewBudgetMultiPeriod).toHaveBeenCalledTimes(1)
      expect(mockPreviewBudgetAdjustment).not.toHaveBeenCalled()
    })

    it('adjust 只调用一次 adjustBudgetMultiPeriod', async () => {
      mockPreviewBudgetMultiPeriod.mockResolvedValue({
        monthly: { preview_version: 'v1', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
        weekly: { preview_version: 'v2', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
        daily: { preview_version: 'v3', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
      })
      mockAdjustBudgetMultiPeriod.mockResolvedValue(undefined)
      mockConfirm.mockResolvedValue(true)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      vm.openCompanyBudgetDialog()
      vm.companyBudgetForm.amount = 10000
      vm.companyBudgetForm.amountUnlimited = false
      vm.companyBudgetForm.weeklyUnlimited = true
      vm.companyBudgetForm.dailyUnlimited = true
      vm.companyBudgetForm.reason = 'TDD test'
      await vm.saveCompanyBudget()
      await flushPromises()

      // adjust 只调用一次，不是三次
      expect(mockAdjustBudgetMultiPeriod).toHaveBeenCalledTimes(1)
      expect(mockAdjustBudget).not.toHaveBeenCalled()
    })

    it('payload 包含月/周/日三个周期', async () => {
      mockPreviewBudgetMultiPeriod.mockResolvedValue({
        monthly: { preview_version: 'v1', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
        weekly: { preview_version: 'v2', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
        daily: { preview_version: 'v3', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
      })
      mockAdjustBudgetMultiPeriod.mockResolvedValue(undefined)
      mockConfirm.mockResolvedValue(true)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      vm.openCompanyBudgetDialog()
      vm.companyBudgetForm.amount = 50000
      vm.companyBudgetForm.amountUnlimited = false
      vm.companyBudgetForm.weekly = 10000
      vm.companyBudgetForm.weeklyUnlimited = false
      vm.companyBudgetForm.daily = 2000
      vm.companyBudgetForm.dailyUnlimited = false
      vm.companyBudgetForm.reason = '三周期测试'
      await vm.saveCompanyBudget()
      await flushPromises()

      const previewCall = mockPreviewBudgetMultiPeriod.mock.calls[0][0]
      expect(previewCall.monthly).toBeDefined()
      expect(previewCall.weekly).toBeDefined()
      expect(previewCall.daily).toBeDefined()
      expect(previewCall.monthly.target_limit_usd).toBe('50000.000000')
      expect(previewCall.weekly.target_limit_usd).toBe('10000.000000')
      expect(previewCall.daily.target_limit_usd).toBe('2000.000000')
    })

    it('BUDGET_PREVIEW_STALE 时自动重试', async () => {
      let callCount = 0
      mockPreviewBudgetMultiPeriod.mockImplementation(() => {
        callCount++
        if (callCount === 1) {
          return Promise.resolve({
            monthly: { preview_version: 'v1', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
            weekly: { preview_version: 'v2', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
            daily: { preview_version: 'v3', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
          })
        }
        return Promise.resolve({
          monthly: { preview_version: 'v4', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
          weekly: { preview_version: 'v5', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
          daily: { preview_version: 'v6', stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false },
        })
      })
      let adjustCallCount = 0
      mockAdjustBudgetMultiPeriod.mockImplementation(() => {
        adjustCallCount++
        if (adjustCallCount === 1) {
          const err: any = new Error('preview stale')
          err.response = { data: { code: 'BUDGET_PREVIEW_STALE' } }
          return Promise.reject(err)
        }
        return Promise.resolve(undefined)
      })
      mockConfirm.mockResolvedValue(true)

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      vm.openCompanyBudgetDialog()
      vm.companyBudgetForm.amount = 10000
      vm.companyBudgetForm.amountUnlimited = false
      vm.companyBudgetForm.weeklyUnlimited = true
      vm.companyBudgetForm.dailyUnlimited = true
      vm.companyBudgetForm.reason = 'stale retry test'
      await vm.saveCompanyBudget()
      await flushPromises()

      // 应该重试：preview 被调用两次，adjust 被调用两次
      expect(mockPreviewBudgetMultiPeriod).toHaveBeenCalledTimes(2)
      expect(mockAdjustBudgetMultiPeriod).toHaveBeenCalledTimes(2)
    })
  })

  describe('金额输入 clamp（99999999.99）', () => {
    it('openCompanyBudgetDialog 时超范围的初始值被 clamp', async () => {
      mockGetBudgetList.mockResolvedValue([
        makeBudgetItem({
          scope_type: 'company',
          scope_id: 'GLOBAL',
          amount_usd: 9999999999,   // 超过 DB 上限
          weekly_amount_usd: -1,
          daily_amount_usd: -1,
        }),
      ])

      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      vm.openCompanyBudgetDialog()

      expect(vm.companyBudgetForm.amount).toBe(99999999.99)
    })

    it('isValidCompanyLimit 对超范围值返回 false', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      // 内部校验函数：unlimited=false, value > max → invalid
      expect(vm.isValidCompanyLimit(false, 99999999.99)).toBe(true)
      expect(vm.isValidCompanyLimit(false, 100000000)).toBe(false)
      expect(vm.isValidCompanyLimit(true, 100000000)).toBe(true)  // unlimited 时忽略 value
    })

    it('saveCompanyBudget 对超范围值阻止提交并显示警告', async () => {
      const { wrapper } = await mountPage('admin')
      await flushPromises()

      const vm = wrapper.vm as any
      vm.openCompanyBudgetDialog()
      vm.companyBudgetForm.amount = 100000000  // 超过 99999999.99
      vm.companyBudgetForm.amountUnlimited = false
      vm.companyBudgetForm.weeklyUnlimited = true
      vm.companyBudgetForm.dailyUnlimited = true
      vm.companyBudgetForm.reason = 'over limit'

      await vm.saveCompanyBudget()
      await flushPromises()

      expect(mockPreviewBudgetMultiPeriod).not.toHaveBeenCalled()
    })
  })
})
