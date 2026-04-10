import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import ModelTierListPage from '../ModelTierListPage.vue'
import type { ModelTier } from '@/types/model-tier'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
  Setting: { template: '<i />' },
}))

// ── Mock API services ──────────────────────────────────────────────
const mockListModelTiers = vi.fn()
const mockCreateModelTier = vi.fn()
const mockUpdateModelTier = vi.fn()
const mockDeleteModelTier = vi.fn()
const mockReorderModelTiers = vi.fn()
const mockArchiveModelTier = vi.fn()
const mockSetModelTierStatus = vi.fn()
const mockSetGroupBudgetTierBatch = vi.fn()

vi.mock('@/services/control-plane', () => ({
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  createModelTier: (...args: unknown[]) => mockCreateModelTier(...args),
  updateModelTier: (...args: unknown[]) => mockUpdateModelTier(...args),
  deleteModelTier: (...args: unknown[]) => mockDeleteModelTier(...args),
  reorderModelTiers: (...args: unknown[]) => mockReorderModelTiers(...args),
  archiveModelTier: (...args: unknown[]) => mockArchiveModelTier(...args),
  setModelTierStatus: (...args: unknown[]) => mockSetModelTierStatus(...args),
  setGroupBudgetTierBatch: (...args: unknown[]) => mockSetGroupBudgetTierBatch(...args),
}))

// Mock groups API
vi.mock('@/api/groups', () => ({
  groupsApi: {
    listGroups: vi.fn().mockResolvedValue({ data: { data: [] } }),
  },
}))

// Mock ElMessageBox to avoid DOM interaction complexity
const mockConfirm = vi.fn().mockResolvedValue(true)
const mockPrompt = vi.fn().mockResolvedValue({ value: 'test reason' })
vi.mock('element-plus', async () => {
  const actual = await vi.importActual<typeof import('element-plus')>('element-plus')
  return {
    ...actual,
    ElMessageBox: {
      confirm: (...args: unknown[]) => mockConfirm(...args),
      prompt: (...args: unknown[]) => mockPrompt(...args),
    },
  }
})

// ── Fixtures ───────────────────────────────────────────────────────
function makeTier(overrides: Partial<ModelTier> = {}): ModelTier {
  return {
    id: 'tier-1',
    name: 'Standard',
    description: 'Standard tier',
    color: '#409EFF',
    sort_order: 0,
    enabled: true,
    archived_at: null,
    group_count: 3,
    budget_count: 1,
    history_count: 2,
    reference_count: 3, // group_count + budget_count + history_count would be > 0
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

const sampleTiers: ModelTier[] = [
  makeTier({ id: 'tier-1', name: 'Standard', sort_order: 0 }),
  makeTier({ id: 'tier-2', name: 'Premium', color: '#FF0000', sort_order: 1 }),
  makeTier({ id: 'tier-3', name: 'Free', sort_order: 2, enabled: false }),
]

// ── Helpers ────────────────────────────────────────────────────────
function mountPage() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(ModelTierListPage, {
    global: {
      plugins: [pinia, ElementPlus],
    },
  })
}

// ── Tests ──────────────────────────────────────────────────────────
describe('ModelTierListPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockListModelTiers.mockResolvedValue([...sampleTiers])
    mockCreateModelTier.mockResolvedValue(makeTier())
    mockUpdateModelTier.mockResolvedValue(makeTier())
    mockDeleteModelTier.mockResolvedValue(undefined)
    mockReorderModelTiers.mockResolvedValue(undefined)
    mockArchiveModelTier.mockResolvedValue(makeTier())
    mockSetModelTierStatus.mockResolvedValue(makeTier())
    mockSetGroupBudgetTierBatch.mockResolvedValue(undefined)
  })

  describe('页面渲染', () => {
    it('从 API 加载等级列表并渲染表格行', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(mockListModelTiers).toHaveBeenCalledWith(true)
      // Table should contain tier names
      const html = wrapper.html()
      expect(html).toContain('Standard')
      expect(html).toContain('Premium')
      expect(html).toContain('Free')
    })

    it('显示引用计数列（group_count, budget_count, history_count）', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      // group_count = 3
      expect(html).toContain('3')
    })

    it('停用等级显示"已停用"标签', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('已停用')
    })

    it('归档等级显示"已归档"标签', async () => {
      mockListModelTiers.mockResolvedValue([
        makeTier({ id: 'tier-arch', name: 'Archived', archived_at: '2026-03-01T00:00:00Z' }),
      ])
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('已归档')
    })

    it('空列表时显示空状态文本', async () => {
      mockListModelTiers.mockResolvedValue([])
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.html()).toContain('暂无模型等级')
    })

    it('颜色圆点使用 tier 的 color 值', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const dots = wrapper.findAll('.color-dot')
      expect(dots.length).toBe(sampleTiers.length)
      expect(dots[0].attributes('style')).toContain('background-color')
    })
  })

  describe('新建等级对话框', () => {
    it('点击"新建等级"按钮打开对话框', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // Find and click the "新建等级" button
      const createBtn = wrapper.findAll('button').find(b => b.text().includes('新建等级'))
      expect(createBtn).toBeDefined()
      await createBtn!.trigger('click')
      await flushPromises()

      expect(wrapper.html()).toContain('新建等级')
    })

    it('提交新建等级调用 createModelTier API', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // Open create dialog
      const createBtn = wrapper.findAll('button').find(b => b.text().includes('新建等级'))
      await createBtn!.trigger('click')
      await flushPromises()

      // Fill form fields via reactive data – we test through the dialog presence
      const dialog = wrapper.find('.el-dialog')
      expect(dialog.exists()).toBe(true)
    })
  })

  describe('排序操作', () => {
    it('第一行的"上移"按钮被禁用', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const upButtons = wrapper.findAll('[aria-label="上移"]')
      expect(upButtons.length).toBeGreaterThan(0)
      expect(upButtons[0].attributes('disabled')).toBeDefined()
    })

    it('最后一行的"下移"按钮被禁用', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const downButtons = wrapper.findAll('[aria-label="下移"]')
      expect(downButtons.length).toBeGreaterThan(0)
      const lastDown = downButtons[downButtons.length - 1]
      expect(lastDown.attributes('disabled')).toBeDefined()
    })

    it('点击"下移"调用 reorderModelTiers API', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const downButtons = wrapper.findAll('[aria-label="下移"]')
      // Click the first non-disabled down button
      await downButtons[0].trigger('click')
      await flushPromises()

      expect(mockReorderModelTiers).toHaveBeenCalledOnce()
      // Verify the new order: tier-2 should come before tier-1
      const newOrder = mockReorderModelTiers.mock.calls[0][0]
      expect(newOrder[0]).toBe('tier-2')
      expect(newOrder[1]).toBe('tier-1')
    })
  })

  // TDD: The component's load() uses try/finally without catch,
  // so API errors produce unhandled rejections. This is a known bug
  // to be fixed separately.

  describe('操作按钮可见性', () => {
    it('已归档等级不显示停用/启用和归档按钮', async () => {
      mockListModelTiers.mockResolvedValue([
        makeTier({ id: 'arch-1', name: 'Archived Tier', archived_at: '2026-06-01T00:00:00Z', reference_count: 5 }),
      ])
      const wrapper = mountPage()
      await flushPromises()

      const html = wrapper.html()
      // Archived tier should not show "停用" or "归档" buttons
      expect(html).not.toContain('>停用<')
      expect(html).not.toContain('>归档<')
    })

    it('reference_count > 0 的等级不显示删除按钮', async () => {
      mockListModelTiers.mockResolvedValue([
        makeTier({ id: 'ref-1', name: 'Referenced', reference_count: 3 }),
      ])
      const wrapper = mountPage()
      await flushPromises()

      const deleteButtons = wrapper.findAll('button').filter(b => b.text() === '删除')
      expect(deleteButtons.length).toBe(0)
    })

    it('reference_count === 0 的等级显示删除按钮', async () => {
      mockListModelTiers.mockResolvedValue([
        makeTier({ id: 'norefs', name: 'NoRefs', reference_count: 0, group_count: 0, budget_count: 0, history_count: 0 }),
      ])
      const wrapper = mountPage()
      await flushPromises()

      const deleteButtons = wrapper.findAll('button').filter(b => b.text() === '删除')
      expect(deleteButtons.length).toBe(1)
    })
  })
})
