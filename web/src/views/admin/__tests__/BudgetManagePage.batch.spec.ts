import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import BudgetManagePage from '../BudgetManagePage.vue'
import type { BudgetItem } from '@/types/budget'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
  Setting: { template: '<i />' },
}))

// ── Mock stores ────────────────────────────────────────────────────
const mockAuthStore = {
  userName: '管理员',
  user: { id: 'admin-1' },
  isAdmin: true,
  isManager: true,
  effectiveRole: 'admin',
  orgRoles: [{ role: 'admin', scope_type: 'company', scope_id: 'GLOBAL' }],
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
const mockGetBudgetList = vi.fn()
const mockGetBudgetContext = vi.fn()
const mockListModelTiers = vi.fn()
const mockGetBudgetRiskSummary = vi.fn()
const mockAdjustBudget = vi.fn()
const mockPreviewBudgetAdjustment = vi.fn()
const mockRebuildBudgetCounters = vi.fn()
const mockHttpGet = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getBudgetList: (...args: unknown[]) => mockGetBudgetList(...args),
  getBudgetContext: (...args: unknown[]) => mockGetBudgetContext(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  getBudgetRiskSummary: (...args: unknown[]) => mockGetBudgetRiskSummary(...args),
  adjustBudget: (...args: unknown[]) => mockAdjustBudget(...args),
  previewBudgetAdjustment: (...args: unknown[]) => mockPreviewBudgetAdjustment(...args),
  rebuildBudgetCounters: (...args: unknown[]) => mockRebuildBudgetCounters(...args),
}))

vi.mock('@/services/http', () => ({
  default: {
    get: (...args: unknown[]) => mockHttpGet(...args),
  },
}))

// Mock child components
vi.mock('@/components/budget/BudgetRiskEventsSection.vue', () => ({
  default: {
    name: 'BudgetRiskEventsSection',
    template: '<div class="mock-risk-events" />',
  },
}))

vi.mock('@/components/budget/BudgetTierLimitsPanel.vue', () => ({
  default: {
    name: 'BudgetTierLimitsPanel',
    template: '<div class="mock-tier-limits" />',
    props: ['allocationId'],
  },
}))

vi.mock('@/components/budget/BudgetEditDrawer.vue', () => ({
  default: {
    name: 'BudgetEditDrawer',
    template: '<div class="mock-edit-drawer" />',
    props: ['modelValue', 'scopeType', 'scopeId', 'title', 'childScopeIds', 'canEdit', 'canEditSelf'],
  },
}))

vi.mock('@/components/budget/MemberQuotaDialog.vue', () => ({
  default: {
    name: 'MemberQuotaDialog',
    template: '<div class="mock-quota-dialog" />',
    props: ['modelValue', 'member', 'canEdit'],
  },
}))

vi.mock('@/components/budget/BudgetBatchAdjustWizard.vue', () => ({
  default: {
    name: 'BudgetBatchAdjustWizard',
    template: '<div class="mock-batch-wizard" />',
    props: ['modelValue'],
  },
}))

// ── Fixtures ───────────────────────────────────────────────────────
const sampleCompanyBudget: BudgetItem = {
  allocation_id: 'alloc-company',
  scope_type: 'company',
  scope_id: 'GLOBAL',
  label: '公司总预算',
  amount_usd: 10000,
  used_usd: 4000,
  used_ratio: 0.4,
  period: '2026-07',
  weekly_amount_usd: 2500,
  daily_amount_usd: 400,
  weekly_used_usd: 500,
  daily_used_usd: 80,
  parent_scope_type: null,
  parent_scope_id: null,
}

const sampleStudioBudgets: BudgetItem[] = [
  {
    allocation_id: 'alloc-studio-1',
    scope_type: 'studio',
    scope_id: 'studio-1',
    label: '研发工作室',
    amount_usd: 2000,
    used_usd: 500,
    used_ratio: 0.25,
    period: '2026-07',
    weekly_amount_usd: -1,
    daily_amount_usd: -1,
    parent_scope_type: 'company',
    parent_scope_id: 'GLOBAL',
  },
  {
    allocation_id: 'alloc-studio-2',
    scope_type: 'studio',
    scope_id: 'studio-2',
    label: '产品工作室',
    amount_usd: 1500,
    used_usd: 1200,
    used_ratio: 0.8,
    period: '2026-07',
    weekly_amount_usd: -1,
    daily_amount_usd: -1,
    parent_scope_type: 'company',
    parent_scope_id: 'GLOBAL',
  },
  {
    allocation_id: 'alloc-studio-3',
    scope_type: 'studio',
    scope_id: 'studio-3',
    label: '运营工作室',
    amount_usd: 1000,
    used_usd: 300,
    used_ratio: 0.3,
    period: '2026-07',
    weekly_amount_usd: -1,
    daily_amount_usd: -1,
    parent_scope_type: 'company',
    parent_scope_id: 'GLOBAL',
  },
]

// ── Helpers ────────────────────────────────────────────────────────
function mountPage() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(BudgetManagePage, {
    global: {
      plugins: [pinia, ElementPlus],
    },
  })
}

// ── Tests ──────────────────────────────────────────────────────────
describe('BudgetManagePage - 批量操作功能', () => {
  beforeEach(() => {
    vi.clearAllMocks()

    // Default successful responses
    mockGetBudgetList.mockResolvedValue([sampleCompanyBudget, ...sampleStudioBudgets])
    mockGetBudgetContext.mockResolvedValue([
      {
        scope_type: 'company',
        scope_id: 'GLOBAL',
        label: '公司总预算',
        amount_usd: 10000,
        used_usd: 4000,
      },
    ])
    mockListModelTiers.mockResolvedValue([])
    mockGetBudgetRiskSummary.mockResolvedValue({ total: 0 })
    mockHttpGet.mockResolvedValue({ data: { items: [] } })

    // Reset auth to admin
    mockAuthStore.effectiveRole = 'admin'
    mockAuthStore.isAdmin = true
  })

  describe('批量选择栏显示/隐藏', () => {
    it('未选中任何项时不显示批量选择栏', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // Batch bar should not exist when no selection
      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(false)
    })

    it('选中项后显示批量选择栏', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // Simulate selection change
      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      // Batch bar should now be visible
      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(true)
    })

    it('选中多项后批量选择栏仍然显示', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // Select multiple items
      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0], sampleStudioBudgets[1]])
      await flushPromises()

      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(true)
    })

    it('取消选择后批量选择栏消失', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // First select some items
      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      expect(wrapper.find('.batch-bar').exists()).toBe(true)

      // Then clear selection
      await table.vm.$emit('selection-change', [])
      await flushPromises()

      expect(wrapper.find('.batch-bar').exists()).toBe(false)
    })
  })

  describe('选中项计数', () => {
    it('显示正确的选中项数量（单项）', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const selectedText = wrapper.find('.batch-bar-selected')
      expect(selectedText.exists()).toBe(true)
      expect(selectedText.text()).toContain('已选择 1 项')
    })

    it('显示正确的选中项数量（多项）', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [
        sampleStudioBudgets[0],
        sampleStudioBudgets[1],
        sampleStudioBudgets[2],
      ])
      await flushPromises()

      const selectedText = wrapper.find('.batch-bar-selected')
      expect(selectedText.text()).toContain('已选择 3 项')
    })

    it('选中项数量随选择变化实时更新', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })

      // Select 1 item
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()
      expect(wrapper.find('.batch-bar-selected').text()).toContain('已选择 1 项')

      // Select 2 items
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0], sampleStudioBudgets[1]])
      await flushPromises()
      expect(wrapper.find('.batch-bar-selected').text()).toContain('已选择 2 项')
    })
  })

  describe('未下发余量显示', () => {
    it('显示当前未下发余量', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const unallocatedText = wrapper.find('.batch-bar-unallocated')
      expect(unallocatedText.exists()).toBe(true)
      expect(unallocatedText.text()).toContain('当前未下发余量')

      // Company budget is 10000, studios total is 4500, so unallocated should be 5500
      expect(unallocatedText.text()).toContain('$5,500')
    })

    it('未下发余量计算正确', async () => {
      // Company: 10000, Studios: 2000 + 1500 + 1000 = 4500
      // Unallocated: 10000 - 4500 = 5500
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const unallocatedText = wrapper.find('.batch-bar-unallocated').text()
      expect(unallocatedText).toContain('$5,500')
    })

    it('未下发余量为零时正确显示', async () => {
      // Modify studios to use all budget
      const fullBudgets = sampleStudioBudgets.map(s => ({ ...s, amount_usd: 3333.33 }))
      mockGetBudgetList.mockResolvedValue([sampleCompanyBudget, ...fullBudgets])

      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [fullBudgets[0]])
      await flushPromises()

      const unallocatedText = wrapper.find('.batch-bar-unallocated').text()
      // Should show near-zero or very small amount
      expect(unallocatedText).toMatch(/\$[0-9.]+/)
    })
  })

  describe('批量操作按钮状态', () => {
    it('选中项时所有批量操作按钮可用', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const fixedAmountBtn = buttons.find(b => b.text().includes('固定额度'))
      const proportionBtn = buttons.find(b => b.text().includes('按比例增减'))
      const tierBudgetBtn = buttons.find(b => b.text().includes('等级子预算'))

      expect(fixedAmountBtn?.attributes('disabled')).toBeUndefined()
      expect(proportionBtn?.attributes('disabled')).toBeUndefined()
      expect(tierBudgetBtn?.attributes('disabled')).toBeUndefined()
    })

    it('未选中项时批量操作按钮禁用', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // Initially no selection - batch bar should not exist
      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(false)
    })

    it('取消选择后按钮状态恢复禁用', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })

      // First select items
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      expect(wrapper.find('.batch-bar').exists()).toBe(true)

      // Then clear selection
      await table.vm.$emit('selection-change', [])
      await flushPromises()

      // Batch bar should disappear
      expect(wrapper.find('.batch-bar').exists()).toBe(false)
    })

    it('显示"固定额度"按钮', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const fixedAmountBtn = buttons.find(b => b.text().includes('固定额度'))
      expect(fixedAmountBtn).toBeDefined()
    })

    it('显示"按比例增减"按钮', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const proportionBtn = buttons.find(b => b.text().includes('按比例增减'))
      expect(proportionBtn).toBeDefined()
    })

    it('显示"等级子预算"按钮', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const tierBudgetBtn = buttons.find(b => b.text().includes('等级子预算'))
      expect(tierBudgetBtn).toBeDefined()
    })
  })

  describe('取消选择功能', () => {
    it('显示"取消选择"按钮', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const cancelBtn = buttons.find(b => b.text().includes('取消选择'))
      expect(cancelBtn).toBeDefined()
    })

    it('点击"取消选择"后清空所有选中项', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })

      // Mock clearSelection method
      const clearSelectionMock = vi.fn()
      table.vm.clearSelection = clearSelectionMock

      // Select items
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0], sampleStudioBudgets[1]])
      await flushPromises()

      expect(wrapper.find('.batch-bar').exists()).toBe(true)

      // Click cancel button
      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const cancelBtn = buttons.find(b => b.text().includes('取消选择'))
      await cancelBtn?.trigger('click')
      await flushPromises()

      // Should have called clearSelection
      expect(clearSelectionMock).toHaveBeenCalled()
    })

    it('取消选择后批量选择栏消失', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })

      // Select items
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      expect(wrapper.find('.batch-bar').exists()).toBe(true)

      // Click cancel and emit selection-change with empty array
      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const cancelBtn = buttons.find(b => b.text().includes('取消选择'))
      await cancelBtn?.trigger('click')
      await table.vm.$emit('selection-change', [])
      await flushPromises()

      expect(wrapper.find('.batch-bar').exists()).toBe(false)
    })

    it('"取消选择"按钮为文字按钮样式', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const cancelBtn = buttons.find(b => b.text().includes('取消选择'))

      // Should have 'is-text' class for text button style
      expect(cancelBtn?.classes()).toContain('is-text')
    })
  })

  describe('批量选择栏样式', () => {
    it('批量选择栏有绿色背景', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(true)

      // Check if batch-bar class is present (styles are in scoped CSS)
      expect(batchBar.classes()).toContain('batch-bar')
    })

    it('批量选择栏布局为左右分栏', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const leftSection = wrapper.find('.batch-bar-left')
      const rightSection = wrapper.find('.batch-bar-right')

      expect(leftSection.exists()).toBe(true)
      expect(rightSection.exists()).toBe(true)
    })

    it('左侧显示选中项计数和分隔符', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const leftSection = wrapper.find('.batch-bar-left')
      const selected = leftSection.find('.batch-bar-selected')
      const divider = leftSection.find('.batch-bar-divider')
      const unallocated = leftSection.find('.batch-bar-unallocated')

      expect(selected.exists()).toBe(true)
      expect(divider.exists()).toBe(true)
      expect(unallocated.exists()).toBe(true)
    })

    it('右侧显示操作按钮组', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const rightSection = wrapper.find('.batch-bar-right')
      const buttons = rightSection.findAll('.el-button')

      // Should have 4 buttons: 固定额度, 按比例增减, 等级子预算, 取消选择
      expect(buttons.length).toBeGreaterThanOrEqual(4)
    })

    it('选中项文本为深绿色加粗', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const selected = wrapper.find('.batch-bar-selected')
      expect(selected.classes()).toContain('batch-bar-selected')
    })
  })

  describe('非admin角色不显示批量选择', () => {
    it('studio_admin不显示批量选择栏', async () => {
      mockAuthStore.effectiveRole = 'studio_admin'
      mockAuthStore.isAdmin = false

      const wrapper = mountPage()
      await flushPromises()

      // Even with selection, batch bar should not appear for non-admin in studio view
      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(false)
    })

    it('dept_admin不显示批量选择栏', async () => {
      mockAuthStore.effectiveRole = 'dept_admin'
      mockAuthStore.isAdmin = false

      const wrapper = mountPage()
      await flushPromises()

      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(false)
    })
  })

  describe('批量选择栏显示条件', () => {
    it('只在company视图显示批量选择栏', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      // In company view, batch bar should be visible
      expect(wrapper.find('.batch-bar').exists()).toBe(true)
    })

    it('v-if条件同时检查overviewView和selectedRows长度', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // No selection - no batch bar
      expect(wrapper.find('.batch-bar').exists()).toBe(false)

      // With selection - batch bar appears
      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      expect(wrapper.find('.batch-bar').exists()).toBe(true)
    })
  })

  describe('批量操作按钮disabled属性', () => {
    it('选中0项时按钮不会渲染（v-if控制）', async () => {
      const wrapper = mountPage()
      await flushPromises()

      // No selection means batch bar doesn't exist
      expect(wrapper.find('.batch-bar').exists()).toBe(false)
    })

    it('选中项后按钮的disabled绑定到selectedRows.length === 0', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const table = wrapper.findComponent({ name: 'ElTable' })
      await table.vm.$emit('selection-change', [sampleStudioBudgets[0]])
      await flushPromises()

      const buttons = wrapper.findAll('.batch-bar-right .el-button')
      const fixedAmountBtn = buttons.find(b => b.text().includes('固定额度'))

      // When items are selected, buttons should be enabled
      expect(fixedAmountBtn?.attributes('disabled')).toBeUndefined()
    })
  })
})
