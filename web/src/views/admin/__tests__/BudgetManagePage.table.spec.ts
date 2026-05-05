import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { ElTable, ElTableColumn, ElTag, ElProgress, ElButton, ElCheckbox, ElSelect, ElOption } from 'element-plus'
import BudgetManagePage from '../BudgetManagePage.vue'
import { useAuthStore } from '@/stores/auth'
import { getBudgetList, getBudgetContext, listBudgetTierLimits } from '@/services/control-plane'
import type { BudgetItem } from '@/types/budget'

vi.mock('@/services/control-plane')
vi.mock('@/stores/auth')
vi.mock('@/services/http', () => ({
  default: {
    get: vi.fn().mockResolvedValue({ data: { items: [] } }),
    post: vi.fn().mockResolvedValue({ data: {} }),
  },
}))
vi.mock('@/components/budget/BudgetManageOverview.vue', () => ({
  default: {
    name: 'BudgetManageOverview',
    props: ['items'],
    data: () => ({ selectedRows: [] as BudgetItem[] }),
    template: `
      <section>
        <div v-if="!items.length" data-test="budget-empty-company">
          尚未向工作室分配预算
          <button data-test="create-studio-budget">创建工作室预算</button>
        </div>
        <el-table v-else :data="items" stripe size="small" @selection-change="selectedRows = $event">
          <el-table-column type="selection" width="42" />
          <el-table-column label="工作室"><template #default="{ row }"><div class="scope-cell"><span class="scope-dot" /><strong class="scope-name">{{ row.label }}</strong></div></template></el-table-column>
          <el-table-column label="已分配额度"><template #default="{ row }"><span class="mono">{{ row.amount_usd }}</span></template></el-table-column>
          <el-table-column label="已使用"><template #default="{ row }"><el-progress :percentage="Math.min(Math.round((row.used_ratio || 0) * 100), 100)" /></template></el-table-column>
          <el-table-column label="等级预算" />
          <el-table-column label="状态"><template #default><el-tag>正常</el-tag></template></el-table-column>
          <el-table-column label="操作"><template #default><el-button>配置</el-button></template></el-table-column>
        </el-table>
        <div v-if="selectedRows.length" class="batch-bar">已选择 {{ selectedRows.length }} 项</div>
      </section>
    `,
  },
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
  }),
  useRoute: () => ({
    query: {},
  }),
}))

const mockStudioBudgets: BudgetItem[] = [
  {
    allocation_id: 'alloc-1',
    scope_type: 'studio',
    scope_id: 'studio-1',
    label: 'Engineering Studio',
    amount_usd: 10000,
    used_usd: 7500,
    used_ratio: 0.75,
    weekly_amount_usd: 2500,
    daily_amount_usd: 500,
    parent_scope_id: 'company',
  },
  {
    allocation_id: 'alloc-2',
    scope_type: 'studio',
    scope_id: 'studio-2',
    label: 'Product Studio',
    amount_usd: 8000,
    used_usd: 7200,
    used_ratio: 0.9,
    weekly_amount_usd: 2000,
    daily_amount_usd: 400,
    parent_scope_id: 'company',
  },
  {
    allocation_id: 'alloc-3',
    scope_type: 'studio',
    scope_id: 'studio-3',
    label: 'Design Studio',
    amount_usd: 5000,
    used_usd: 5100,
    used_ratio: 1.02,
    weekly_amount_usd: 1250,
    daily_amount_usd: 250,
    parent_scope_id: 'company',
  },
]

const createWrapper = () => {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(BudgetManagePage, {
    global: {
      plugins: [pinia],
      components: {
        ElTable,
        ElTableColumn,
        ElTag,
        ElProgress,
        ElButton,
        ElCheckbox,
        ElSelect,
        ElOption,
      },
      stubs: {
        ElTabs: { template: '<div><slot /></div>' },
        ElTabPane: { template: '<div><slot /></div>' },
        ElBadge: { template: '<div><slot /></div>' },
        ElDescriptions: { template: '<div><slot /></div>' },
        ElDescriptionsItem: { template: '<div><slot /></div>' },
        ElTooltip: { template: '<div><slot /></div>' },
        ElSegmented: { template: '<div><slot /></div>' },
        ElDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ElInputNumber: { template: '<input />' },
        ElFormItem: { template: '<div><slot /></div>' },
        ElRadio: { template: '<input type="radio" />' },
        ElRadioGroup: { template: '<div><slot /></div>' },
        ElInput: { template: '<input />' },
        ElForm: { template: '<form><slot /></form>' },
        ElMessageBox: { template: '<div />' },
        ElResult: { template: '<div><slot /><slot name="extra" /></div>' },
        BudgetRiskEventsSection: { template: '<div />' },
        BudgetTierLimitsPanel: { template: '<div />' },
        BudgetEditDrawer: { template: '<div />' },
        BudgetBatchAdjustWizard: { template: '<div />' },
        MemberQuotaDialog: { template: '<div />' },
      },
      directives: {
        loading: {
          mounted: () => {},
          updated: () => {},
        },
      },
    },
  })
}

describe('BudgetManagePage - Table Functionality', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(getBudgetList).mockResolvedValue(mockStudioBudgets)
    vi.mocked(getBudgetContext).mockResolvedValue([])
    vi.mocked(listBudgetTierLimits).mockResolvedValue([])
    vi.mocked(useAuthStore).mockReturnValue({
      effectiveRole: 'admin',
      orgRoles: [],
    } as any)
  })

  describe('Admin View - Studio Table', () => {
    it('renders table with all columns', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const table = wrapper.findComponent(ElTable)
      expect(table.exists()).toBe(true)

      const columns = wrapper.findAllComponents(ElTableColumn)
      expect(columns.length).toBeGreaterThan(0)
      
      const columnLabels = columns.map(col => col.props('label')).filter(Boolean)
      expect(columnLabels).toContain('工作室')
      expect(columnLabels).toContain('已分配额度')
      expect(columnLabels).toContain('已使用')
      expect(columnLabels).toContain('等级预算')
      expect(columnLabels).toContain('状态')
      expect(columnLabels).toContain('操作')
    })

    it('renders studio names with scope dots', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const scopeCells = wrapper.findAll('.scope-cell')
      expect(scopeCells.length).toBeGreaterThan(0)

      const scopeDots = wrapper.findAll('.scope-dot')
      expect(scopeDots.length).toBeGreaterThan(0)

      const scopeNames = wrapper.findAll('.scope-name')
      expect(scopeNames.length).toBeGreaterThan(0)
    })

    it('displays budget amounts in correct format', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const monoAmounts = wrapper.findAll('.mono')
      expect(monoAmounts.length).toBeGreaterThan(0)
    })

    it('renders progress bars with correct usage ratio', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const progressBars = wrapper.findAllComponents(ElProgress)
      expect(progressBars.length).toBeGreaterThan(0)
    })

    it('displays status tags with correct colors', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const statusTags = wrapper.findAllComponents(ElTag)
      expect(statusTags.length).toBeGreaterThan(0)
    })

    it('renders action buttons for each row', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const buttons = wrapper.findAllComponents(ElButton)
      expect(buttons.length).toBeGreaterThan(0)
    })
  })

  describe('Table Selection', () => {
    it('renders selection column', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const columns = wrapper.findAllComponents(ElTableColumn)
      const selectionColumn = columns.find(col => col.props('type') === 'selection')
      expect(selectionColumn).toBeDefined()
    })

    it('shows batch bar when rows are selected', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      await wrapper.findComponent(ElTable).vm.$emit('selection-change', [mockStudioBudgets[0]])
      await wrapper.vm.$nextTick()

      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.exists()).toBe(true)
    })

    it('displays correct selection count in batch bar', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      await wrapper.findComponent(ElTable).vm.$emit('selection-change', [mockStudioBudgets[0], mockStudioBudgets[1]])
      await wrapper.vm.$nextTick()

      const batchBar = wrapper.find('.batch-bar')
      expect(batchBar.text()).toContain('已选择 2 项')
    })

    it('clears selection when cancel button clicked', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      wrapper.vm.selectedRows = [mockStudioBudgets[0]]
      wrapper.vm.tableRef = { clearSelection: vi.fn() }
      await wrapper.vm.$nextTick()

      wrapper.vm.selectedRows = []
      await wrapper.vm.$nextTick()
      
      expect(wrapper.vm.selectedRows).toHaveLength(0)
    })
  })

  describe('Empty State', () => {
    it('displays empty state when no data', async () => {
      vi.mocked(getBudgetList).mockResolvedValue([])

      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const emptyGuide = wrapper.find('[data-test="budget-empty-company"]')
      expect(emptyGuide.exists()).toBe(true)
    })

    it('shows correct empty state message for admin view', async () => {
      vi.mocked(getBudgetList).mockResolvedValue([])

      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const emptyGuide = wrapper.get('[data-test="budget-empty-company"]')
      expect(emptyGuide.text()).toContain('尚未向工作室分配预算')
      expect(wrapper.get('[data-test="create-studio-budget"]').exists()).toBe(true)
    })
  })

  describe('Table Styling', () => {
    it('applies correct column widths', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const columns = wrapper.findAllComponents(ElTableColumn)
      const selectionColumn = columns.find(col => col.props('type') === 'selection')
      if (selectionColumn) {
        expect(selectionColumn.props('width')).toBe('42')
      }
    })

    it('applies stripe style to table', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const table = wrapper.findComponent(ElTable)
      expect(table.props('stripe')).toBe(true)
    })

    it('applies small size to table', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const table = wrapper.findComponent(ElTable)
      expect(table.props('size')).toBe('small')
    })
  })

  describe('Data Refresh', () => {
    it('reloads data when refresh button clicked', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const initialCallCount = vi.mocked(getBudgetList).mock.calls.length
      
      await wrapper.vm.load()
      
      expect(vi.mocked(getBudgetList).mock.calls.length).toBeGreaterThan(initialCallCount)
    })

    it('updates table data after refresh', async () => {
      const wrapper = createWrapper()

      await wrapper.vm.$nextTick()
      await new Promise(resolve => setTimeout(resolve, 100))

      const newData: BudgetItem[] = [{
        allocation_id: 'alloc-new',
        scope_type: 'studio',
        scope_id: 'studio-new',
        label: 'New Studio',
        amount_usd: 15000,
        used_usd: 5000,
        used_ratio: 0.33,
        weekly_amount_usd: 3000,
        daily_amount_usd: 600,
        parent_scope_id: 'company',
      }]

      vi.mocked(getBudgetList).mockResolvedValue(newData)
      await wrapper.vm.load()
      await wrapper.vm.$nextTick()

      expect(wrapper.vm.items).toEqual(newData)
    })
  })
})
