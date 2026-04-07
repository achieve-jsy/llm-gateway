import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import MyBudgetPage from '../MyBudgetPage.vue'
import type { BudgetChainItem } from '@/types/budget'
import type { ExpansionRequest } from '@/types/expansion'
import type { MyBudgetTiersResponse } from '@/types/model-tier'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
  Setting: { template: '<i />' },
}))

// ── Mock stores ────────────────────────────────────────────────────
const mockAuthStore = {
  userName: '张三',
  user: { id: 'user-1' },
  isAdmin: false,
  isManager: false,
  effectiveRole: 'member',
  orgRoles: [],
}

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => mockAuthStore,
}))

// ── Mock vue-router ────────────────────────────────────────────────
const mockPush = vi.fn()
const mockReplace = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  useRoute: () => ({ query: {} }),
}))

// ── Mock API services ──────────────────────────────────────────────
const mockGetMyBudget = vi.fn()
const mockGetMyBudgetTiers = vi.fn()
const mockGetMyExpansionRequests = vi.fn()
const mockGetMyKeyRequests = vi.fn()
const mockCreateExpansionRequest = vi.fn()
const mockDecideExpansion = vi.fn()
const mockPrecheckExpansion = vi.fn()
const mockNotifyBudgetOwner = vi.fn()
const mockCancelKeyRequest = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getMyBudget: (...args: unknown[]) => mockGetMyBudget(...args),
  getMyBudgetTiers: (...args: unknown[]) => mockGetMyBudgetTiers(...args),
  getMyExpansionRequests: (...args: unknown[]) => mockGetMyExpansionRequests(...args),
  getMyKeyRequests: (...args: unknown[]) => mockGetMyKeyRequests(...args),
  createExpansionRequest: (...args: unknown[]) => mockCreateExpansionRequest(...args),
  decideExpansion: (...args: unknown[]) => mockDecideExpansion(...args),
  precheckExpansion: (...args: unknown[]) => mockPrecheckExpansion(...args),
  notifyBudgetOwner: (...args: unknown[]) => mockNotifyBudgetOwner(...args),
  cancelKeyRequest: (...args: unknown[]) => mockCancelKeyRequest(...args),
}))

// Mock MyQuotaPanel
vi.mock('@/components/budget/MyQuotaPanel.vue', () => ({
  default: {
    name: 'MyQuotaPanel',
    template: '<div class="mock-quota-panel" />',
    methods: { loadQuota: vi.fn() },
  },
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
const sampleBudgetChain: BudgetChainItem[] = [
  {
    scope_type: 'member',
    scope_id: 'user-1',
    label: '张三的配额',
    amount_usd: 500,
    daily_amount_usd: 30,
    weekly_amount_usd: 150,
    used_usd: 200,
    used_ratio: 0.4,
    period: '2026-07',
  },
]

const sampleTierBudget: MyBudgetTiersResponse = {
  source_summary: {
    mode: 'personal',
    primary_scope_type: 'member',
    has_shared_pool: false,
    has_temporary_override: false,
  },
  effective_total: [
    {
      period_type: 'monthly',
      configured: true,
      base_limit_usd: '500.000000',
      effective_limit_usd: '500.000000',
      used_usd: '200.000000',
      remaining_usd: '300.000000',
      reset_at: '2026-08-01T00:00:00Z',
      status: 'active',
    },
    {
      period_type: 'daily',
      configured: true,
      base_limit_usd: '30.000000',
      effective_limit_usd: '30.000000',
      used_usd: '10.000000',
      remaining_usd: '20.000000',
      reset_at: '2026-07-14T00:00:00Z',
      status: 'active',
    },
  ],
  budget_chain: [
    { scope_type: 'member', label: '张三的配额', visible_amounts: true },
  ],
  tier_limits: [
    {
      model_tier_id: 'tier-1',
      model_tier_name: 'Standard',
      enabled: true,
      periods: [
        {
          period_type: 'monthly',
          configured: true,
          base_limit_usd: '200.000000',
          effective_limit_usd: '200.000000',
          used_usd: '50.000000',
          remaining_usd: '150.000000',
          reset_at: '2026-08-01T00:00:00Z',
          status: 'enabled',
          limiting_source: { scope_type: 'member' },
        },
      ],
    },
  ],
  data_updated_at: '2026-07-13T10:00:00Z',
  stale: false,
}

// ── Helpers ────────────────────────────────────────────────────────
function mountPage() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(MyBudgetPage, {
    global: {
      plugins: [pinia, ElementPlus],
    },
  })
}

// ── Tests ──────────────────────────────────────────────────────────
describe('MyBudgetPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetMyBudget.mockResolvedValue([...sampleBudgetChain])
    mockGetMyBudgetTiers.mockResolvedValue({ ...sampleTierBudget })
    mockGetMyExpansionRequests.mockResolvedValue([])
    mockGetMyKeyRequests.mockResolvedValue({ items: [] })
    mockCreateExpansionRequest.mockResolvedValue({ id: 'req-1' })
    mockDecideExpansion.mockResolvedValue(undefined)
    mockPrecheckExpansion.mockResolvedValue({ can_submit: true, can_notify_owner: false })
    mockNotifyBudgetOwner.mockResolvedValue(undefined)
    mockCancelKeyRequest.mockResolvedValue(undefined)

    // Reset auth store to default
    mockAuthStore.isAdmin = false
    mockAuthStore.isManager = false
    mockAuthStore.effectiveRole = 'member'
    mockAuthStore.userName = '张三'
    mockAuthStore.user = { id: 'user-1' }
  })

  describe('页面渲染', () => {
    it('渲染页面标题"我的预算"', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('我的预算')
    })

    it('显示用户名', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('张三')
    })

    it('加载预算链路数据', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(mockGetMyBudget).toHaveBeenCalled()
    })

    it('加载等级预算数据', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(mockGetMyBudgetTiers).toHaveBeenCalled()
    })
  })

  describe('等级预算显示', () => {
    it('显示"当前有效预算"区块', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('当前有效预算')
    })

    it('显示有效周期预算卡片', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      // Monthly total budget
      expect(html).toContain('$500.000000')
      expect(html).toContain('$200.000000')
    })

    it('显示模型等级额度表格', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('Standard')
      expect(html).toContain('模型等级额度')
    })

    it('等级预算加载失败时显示错误状态', async () => {
      mockGetMyBudgetTiers.mockRejectedValue(new Error('Network Error'))
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('预算加载失败')
    })

    it('无有效预算时显示空状态', async () => {
      mockGetMyBudgetTiers.mockResolvedValue({
        ...sampleTierBudget,
        effective_total: [],
        tier_limits: [],
      })
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('暂无有效预算配置')
    })
  })

  describe('KPI 指标条', () => {
    it('显示可用余额、已用、额度、使用率', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('可用余额')
      expect(html).toContain('本月已用')
      expect(html).toContain('月度额度')
      expect(html).toContain('使用率')
    })

    it('使用率超过 80% 时显示危险样式', async () => {
      mockGetMyBudget.mockResolvedValue([{
        ...sampleBudgetChain[0],
        used_ratio: 0.9,
        used_usd: 450,
      }])
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('is-danger')
    })
  })

  describe('无预算配置', () => {
    it('无预算且非管理员时显示"暂无预算配置"', async () => {
      mockGetMyBudget.mockResolvedValue([])
      mockGetMyBudgetTiers.mockResolvedValue({
        ...sampleTierBudget,
        effective_total: [],
        tier_limits: [],
      })
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('暂无预算配置')
    })

    it('管理员无预算时显示管理入口', async () => {
      mockAuthStore.isAdmin = true
      mockGetMyBudget.mockResolvedValue([])
      mockGetMyBudgetTiers.mockResolvedValue({
        ...sampleTierBudget,
        effective_total: [],
        tier_limits: [],
      })
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('前往预算管理')
    })
  })

  describe('额度相关申请', () => {
    it('显示"额度相关申请"面板', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('额度相关申请')
    })

    it('无申请时显示空状态', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('暂无额度相关申请')
    })

    it('非管理员显示"新建申请"按钮', async () => {
      mockAuthStore.isManager = false
      const wrapper = mountPage()
      await flushPromises()

      const newBtn = wrapper.findAll('button').find(b => b.text() === '新建申请')
      expect(newBtn).toBeDefined()
    })

    it('管理员不显示"新建申请"按钮', async () => {
      mockAuthStore.isManager = true
      const wrapper = mountPage()
      await flushPromises()

      const newBtn = wrapper.findAll('button').find(b => b.text() === '新建申请')
      expect(newBtn).toBeUndefined()
    })
  })

  describe('Tab 切换', () => {
    it('默认显示"我的预算" tab', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('我的预算')
    })
  })

  describe('API 错误处理', () => {
    it('预算链路加载失败不阻断页面渲染', async () => {
      mockGetMyBudget.mockRejectedValue(new Error('500'))
      const wrapper = mountPage()
      await flushPromises()

      // Page should still render
      expect(wrapper.html()).toContain('我的预算')
    })

    it('扩容请求列表加载失败时清空列表', async () => {
      mockGetMyExpansionRequests.mockRejectedValue(new Error('API Error'))
      const wrapper = mountPage()
      await flushPromises()

      // Should still render, requests list treated as empty
      expect(wrapper.html()).toContain('额度相关申请')
    })
  })

  describe('月度分析', () => {
    it('显示月度分析区块', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('月度分析')
    })

    it('显示日均消耗和预计月末', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('日均消耗')
      expect(wrapper.html()).toContain('预计月末')
    })
  })

  describe('周期额度显示', () => {
    it('显示今日、本周、本月额度', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('今日额度')
      expect(html).toContain('本周额度')
      expect(html).toContain('本月额度')
    })
  })

  describe('管理员提示', () => {
    it('dept_admin 角色显示直接调整提示', async () => {
      mockAuthStore.isManager = true
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.orgRoles = [{ role: 'dept_admin', scope_id: 'dept-1' }]
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('您可直接调整额度')
    })
  })
})
