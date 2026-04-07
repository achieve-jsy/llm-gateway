import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import BudgetTierLimitsPanel from '../BudgetTierLimitsPanel.vue'
import type { BudgetTierLimit } from '@/types/model-tier'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
}))

// ── Mock API services ──────────────────────────────────────────────
const mockListBudgetTierLimits = vi.fn()

vi.mock('@/services/control-plane', () => ({
  listBudgetTierLimits: (...args: unknown[]) => mockListBudgetTierLimits(...args),
}))

// Mock BudgetAdjustDrawer
vi.mock('../BudgetAdjustDrawer.vue', () => ({
  default: {
    name: 'BudgetAdjustDrawer',
    template: '<div class="mock-drawer" />',
    props: ['modelValue', 'allocationId', 'modelTierId', 'initial'],
    emits: ['saved'],
  },
}))

// ── Fixtures ───────────────────────────────────────────────────────
function makeTierLimit(overrides: Partial<BudgetTierLimit> = {}): BudgetTierLimit {
  return {
    id: 'limit-1',
    budget_allocation_id: 'alloc-1',
    model_tier_id: 'tier-1',
    model_tier_name: 'Standard',
    period_type: 'monthly',
    limit_usd: '100.000000',
    enabled: true,
    state: 'enabled',
    used_usd: '50.000000',
    ...overrides,
  }
}

const sampleLimits: BudgetTierLimit[] = [
  makeTierLimit({ id: 'limit-1', model_tier_id: 'tier-1', model_tier_name: 'Standard', limit_usd: '100.000000' }),
  makeTierLimit({ id: 'limit-2', model_tier_id: 'tier-2', model_tier_name: 'Premium', limit_usd: '500.000000' }),
  makeTierLimit({ id: 'limit-3', model_tier_id: 'tier-3', model_tier_name: 'Free', limit_usd: '0', enabled: false }),
]

// ── Helpers ────────────────────────────────────────────────────────
function mountPanel(allocationId = 'alloc-1') {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(BudgetTierLimitsPanel, {
    props: { allocationId },
    global: {
      plugins: [pinia, ElementPlus],
    },
  })
}

// ── Tests ──────────────────────────────────────────────────────────
describe('BudgetTierLimitsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListBudgetTierLimits.mockResolvedValue([...sampleLimits])
  })

  describe('面板渲染', () => {
    it('从 API 加载等级额度列表', async () => {
      const wrapper = mountPanel()
      await flushPromises()

      expect(mockListBudgetTierLimits).toHaveBeenCalledWith('alloc-1')
      const html = wrapper.html()
      expect(html).toContain('Standard')
      expect(html).toContain('Premium')
    })

    it('显示周期类型标签（日/周/月）', async () => {
      const wrapper = mountPanel()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('月')
    })

    it('停用的额度显示"停用"标签', async () => {
      const wrapper = mountPanel()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('停用')
    })

    it('额度为 0 的显示"$0 · 禁止使用"标签', async () => {
      mockListBudgetTierLimits.mockResolvedValue([
        makeTierLimit({ limit_usd: '0', enabled: true }),
      ])
      const wrapper = mountPanel()
      await flushPromises()

      expect(wrapper.html()).toContain('$0')
      expect(wrapper.html()).toContain('禁止使用')
    })

    it('空列表时显示空状态文本', async () => {
      mockListBudgetTierLimits.mockResolvedValue([])
      const wrapper = mountPanel()
      await flushPromises()

      expect(wrapper.html()).toContain('暂无等级配置')
    })

    it('点击"刷新"按钮重新加载数据', async () => {
      const wrapper = mountPanel()
      await flushPromises()

      const refreshBtn = wrapper.findAll('button').find(b => b.text() === '刷新')
      expect(refreshBtn).toBeDefined()
      await refreshBtn!.trigger('click')
      await flushPromises()

      expect(mockListBudgetTierLimits).toHaveBeenCalledTimes(2)
    })
  })

  describe('编辑操作', () => {
    it('点击"调整"按钮打开编辑抽屉', async () => {
      const wrapper = mountPanel()
      await flushPromises()

      const editButtons = wrapper.findAll('button').filter(b => b.text() === '调整')
      expect(editButtons.length).toBeGreaterThan(0)

      await editButtons[0].trigger('click')
      await flushPromises()

      // Drawer should be rendered
      expect(wrapper.find('.mock-drawer').exists()).toBe(true)
    })

    it('编辑时传递正确的 model_tier_id', async () => {
      const wrapper = mountPanel()
      await flushPromises()

      const editButtons = wrapper.findAll('button').filter(b => b.text() === '调整')
      await editButtons[1].trigger('click')
      await flushPromises()

      const drawer = wrapper.findComponent({ name: 'BudgetAdjustDrawer' })
      expect(drawer.exists()).toBe(true)
      expect(drawer.props('modelTierId')).toBe('tier-2')
    })
  })

  // TDD: The component's load() uses try/finally without catch,
  // so API errors produce unhandled rejections. This is a known bug
  // to be fixed separately.
  describe('API 错误处理', () => {
    it('allocationId 为空时不调用 API', async () => {
      const wrapper = mountPanel('')
      await flushPromises()

      expect(mockListBudgetTierLimits).not.toHaveBeenCalled()
    })
  })
})
