import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import CatalogSyncPage from './CatalogSyncPage.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn()
  },
  ElMessageBox: {
    confirm: vi.fn(() => Promise.resolve())
  }
}))

vi.mock('@/services/control-plane', () => ({
  getCatalogDiff: vi.fn(),
  applyCatalogDiff: vi.fn(),
  triggerCatalogSync: vi.fn(),
  searchCatalog: vi.fn(),
  autoBindCatalog: vi.fn(),
  batchBindModelCatalog: vi.fn()
}))

import { ElMessage } from 'element-plus'
import {
  getCatalogDiff,
  applyCatalogDiff,
  triggerCatalogSync,
  searchCatalog,
  autoBindCatalog,
  batchBindModelCatalog
} from '@/services/control-plane'

const mockedGetDiff = vi.mocked(getCatalogDiff)
const mockedApply = vi.mocked(applyCatalogDiff)
const mockedTrigger = vi.mocked(triggerCatalogSync)
const mockedSearch = vi.mocked(searchCatalog)
const mockedAutoBind = vi.mocked(autoBindCatalog)
const mockedBatchBind = vi.mocked(batchBindModelCatalog)

const sampleDiff = [
  {
    model_id: 'm1',
    model_name: 'gpt-4o',
    catalog_id: 'cat-1',
    catalog_source_model_id: 'gpt-4o',
    local_input_price: 2.5,
    local_output_price: 10,
    catalog_input_price: 3.0,
    catalog_output_price: 12,
    is_overridden: false,
    input_delta_pct: 20,
    output_delta_pct: 20,
    catalog_deprecation_date: null
  },
  {
    model_id: 'm2',
    model_name: 'claude-3-opus',
    catalog_id: 'cat-2',
    catalog_source_model_id: 'claude-3-opus-20240229',
    local_input_price: 15,
    local_output_price: 75,
    catalog_input_price: 15,
    catalog_output_price: 75,
    is_overridden: true,
    input_delta_pct: 0,
    output_delta_pct: 0,
    catalog_deprecation_date: '2026-12-31T00:00:00Z'
  },
  {
    model_id: 'm3',
    model_name: 'old-model',
    catalog_id: 'cat-3',
    catalog_source_model_id: 'old-model-v1',
    local_input_price: 1,
    local_output_price: 1,
    catalog_input_price: 0.8,
    catalog_output_price: 0.8,
    is_overridden: false,
    input_delta_pct: -20,
    output_delta_pct: -20,
    catalog_deprecation_date: '2026-09-01T00:00:00Z'
  }
]

function mountPage() {
  return mount(CatalogSyncPage, {
    global: { stubs: elementStubsKebab }
  })
}

describe('CatalogSyncPage.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGetDiff.mockResolvedValue({ data: sampleDiff, total: sampleDiff.length } as any)
    mockedApply.mockResolvedValue({ updated: 0, skipped: 0, failed_ids: [] } as any)
    mockedTrigger.mockResolvedValue({ job_id: 'job-1', status: 'queued', source: 'litellm' } as any)
    mockedSearch.mockResolvedValue({
      data: [{ synced_at: '2026-05-22T06:51:58Z' } as any],
      total: 2719
    } as any)
    mockedAutoBind.mockResolvedValue({
      matched: [],
      ambiguous: [],
      unmatched: 0
    } as any)
  })

  describe('rendering', () => {
    it('loads diff on mount and renders all rows', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(mockedGetDiff).toHaveBeenCalled()
      expect(wrapper.text()).toContain('gpt-4o')
      expect(wrapper.text()).toContain('claude-3-opus')
      expect(wrapper.text()).toContain('old-model')
    })

    it('renders override badge when is_overridden=true', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.text()).toContain('已脱离目录')
    })

    it('renders the catalog total in the header', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.text()).toContain('2719')
    })

    it('renders deprecation reminder when catalog_deprecation_date is not null', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const text = wrapper.text()
      expect(text).toContain('废弃')
      expect(text).toMatch(/2026-12-31|2026-09-01/)
    })
  })

  describe('selection + apply', () => {
    it('calls applyCatalogDiff with the selected model_ids when applying', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).selectedIds = ['m1', 'm3']
      await flushPromises()
      const applyBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('应用选中项'))!
      await applyBtn.trigger('click')
      await flushPromises()
      expect(mockedApply).toHaveBeenCalledWith(['m1', 'm3'])
    })

    it('refreshes diff table after successful apply', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGetDiff.mockClear()
      ;(wrapper.vm as any).selectedIds = ['m1']
      await flushPromises()
      const applyBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('应用选中项'))!
      await applyBtn.trigger('click')
      await flushPromises()
      expect(mockedGetDiff).toHaveBeenCalled()
    })

    it('shows a hint and does nothing when apply pressed with empty selection', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const applyBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('应用选中项'))!
      await applyBtn.trigger('click')
      await flushPromises()
      expect(mockedApply).not.toHaveBeenCalled()
      expect(ElMessage.warning).toHaveBeenCalled()
    })
  })

  describe('manual refresh', () => {
    it('calls triggerCatalogSync when "手动刷新" is clicked', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const refreshBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('手动刷新'))!
      await refreshBtn.trigger('click')
      await flushPromises()
      expect(mockedTrigger).toHaveBeenCalled()
    })
  })

  describe('filter / sort / paging', () => {
    it('keyword filters by model_name and catalog_source_model_id', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).searchKeyword = 'old-model'
      await flushPromises()
      const filtered = (wrapper.vm as any).filteredRows as Array<{
        model_name: string
      }>
      expect(filtered).toHaveLength(1)
      expect(filtered[0].model_name).toBe('old-model')
    })

    it('status filter "overridden" keeps only is_overridden=true rows', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).statusFilters = ['overridden']
      await flushPromises()
      const filtered = (wrapper.vm as any).filteredRows as Array<{
        is_overridden: boolean
      }>
      expect(filtered.length).toBeGreaterThan(0)
      expect(filtered.every((r) => r.is_overridden)).toBe(true)
    })

    it('status filter "deprecated" keeps only deprecation_date != null', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).statusFilters = ['deprecated']
      await flushPromises()
      const filtered = (wrapper.vm as any).filteredRows as Array<{
        catalog_deprecation_date: string | null
      }>
      expect(filtered.length).toBeGreaterThan(0)
      expect(filtered.every((r) => !!r.catalog_deprecation_date)).toBe(true)
    })

    it('threshold filter |Δ| ≥ N filters out smaller deltas', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).minAbsDelta = 25
      await flushPromises()
      const filtered = (wrapper.vm as any).filteredRows
      // sample data 中所有 delta 是 0/20/-20，全部被 25% 阈值剔除（除了价格未设的特例）
      expect(filtered).toHaveLength(0)
    })

    it('sort by name_asc orders by model_name ascending', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).sortKey = 'name_asc'
      await flushPromises()
      const sorted = (wrapper.vm as any).sortedRows as Array<{
        model_name: string
      }>
      const names = sorted.map((r) => r.model_name)
      const sortedCopy = [...names].sort()
      expect(names).toEqual(sortedCopy)
    })

    it('paging: pagedRows respects pageSize', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).pageSize = 2
      await flushPromises()
      const paged = (wrapper.vm as any).pagedRows
      expect(paged).toHaveLength(2)
    })

    it('changing filter resets currentPage to 1', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).currentPage = 3
      ;(wrapper.vm as any).searchKeyword = 'gpt'
      await flushPromises()
      expect((wrapper.vm as any).currentPage).toBe(1)
    })
  })

  describe('auto-bind', () => {
    it('calls autoBindCatalog when "智能绑定" button clicked', async () => {
      mockedAutoBind.mockResolvedValueOnce({
        matched: [
          {
            model_id: 'm1',
            model_name: 'gpt-4o',
            catalog_id: 'cat-1',
            catalog_source_model_id: 'gpt-4o'
          }
        ],
        ambiguous: [],
        unmatched: 5
      } as any)

      const wrapper = mountPage()
      await flushPromises()
      const btn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('智能绑定'))!
      await btn.trigger('click')
      await flushPromises()

      expect(mockedAutoBind).toHaveBeenCalled()
      expect(ElMessage.success).toHaveBeenCalled()
      // 成功后会重新拉一次 diff
      expect(mockedGetDiff).toHaveBeenCalledTimes(2)
    })

    it('shows warning toast when ambiguous candidates exist', async () => {
      mockedAutoBind.mockResolvedValueOnce({
        matched: [],
        ambiguous: [
          {
            model_id: 'm1',
            model_name: 'deepseek-r1',
            candidates: [
              { catalog_id: 'c1', catalog_source_model_id: 'azure_ai/deepseek-r1', provider: 'azure_ai' },
              { catalog_id: 'c2', catalog_source_model_id: 'fireworks_ai/deepseek-r1', provider: 'fireworks_ai' }
            ]
          }
        ],
        unmatched: 0
      } as any)

      const wrapper = mountPage()
      await flushPromises()
      const btn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('智能绑定'))!
      await btn.trigger('click')
      await flushPromises()

      expect(ElMessage.warning).toHaveBeenCalled()
    })

    // ===================== CAT-2: ambiguous 选择 + 批量绑定 =====================

    function setupAmbiguousResult() {
      mockedAutoBind.mockResolvedValueOnce({
        matched: [],
        ambiguous: [
          {
            model_id: 'm1',
            model_name: 'deepseek-r1',
            candidates: [
              { catalog_id: 'c1', catalog_source_model_id: 'azure_ai/deepseek-r1', provider: 'azure_ai' },
              { catalog_id: 'c2', catalog_source_model_id: 'fireworks_ai/deepseek-r1', provider: 'fireworks_ai' }
            ]
          },
          {
            model_id: 'm2',
            model_name: 'minimax-m2',
            candidates: [
              { catalog_id: 'c3', catalog_source_model_id: 'minimax/m2', provider: 'minimax' }
            ]
          }
        ],
        unmatched: 0
      } as any)
    }

    async function openAmbiguousDialog(wrapper: any) {
      const btn = wrapper.findAll('.el-button-stub').find((b: any) => b.text().includes('智能绑定'))!
      await btn.trigger('click')
      await flushPromises()
    }

    it('renders one row per ambiguous model with a select dropdown', async () => {
      setupAmbiguousResult()
      const wrapper = mountPage()
      await flushPromises()
      await openAmbiguousDialog(wrapper)

      const rows = wrapper.findAll('[data-testid="ambiguous-row"]')
      expect(rows.length).toBe(2)
      expect(wrapper.find('[data-testid="ambiguous-select-m1"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="ambiguous-select-m2"]').exists()).toBe(true)
    })

    it('save button is disabled when nothing selected', async () => {
      setupAmbiguousResult()
      const wrapper = mountPage()
      await flushPromises()
      await openAmbiguousDialog(wrapper)

      const saveBtn = wrapper.find('[data-testid="ambiguous-save-btn"]')
      expect(saveBtn.exists()).toBe(true)
      expect(saveBtn.attributes('disabled')).toBeDefined()
    })

    it('calls batchBindModelCatalog with selected pairs and reset_fields=false by default', async () => {
      setupAmbiguousResult()
      mockedBatchBind.mockResolvedValueOnce({ applied: 2, failed_ids: {} })

      const wrapper = mountPage()
      await flushPromises()
      await openAmbiguousDialog(wrapper)

      // 直接通过 vm 写入选择，避开 element-plus stub 内部状态
      ;(wrapper.vm as any).ambiguousSelections = { m1: 'c2', m2: 'c3' }
      await flushPromises()

      const saveBtn = wrapper.find('[data-testid="ambiguous-save-btn"]')
      await saveBtn.trigger('click')
      await flushPromises()

      expect(mockedBatchBind).toHaveBeenCalledWith([
        { model_id: 'm1', catalog_id: 'c2', reset_fields: false },
        { model_id: 'm2', catalog_id: 'c3', reset_fields: false }
      ])
      expect(ElMessage.success).toHaveBeenCalled()
      // 成功后会重新拉一次 diff
      expect(mockedGetDiff).toHaveBeenCalledTimes(2)
    })

    it('passes reset_fields=true when checkbox checked', async () => {
      setupAmbiguousResult()
      mockedBatchBind.mockResolvedValueOnce({ applied: 1, failed_ids: {} })

      const wrapper = mountPage()
      await flushPromises()
      await openAmbiguousDialog(wrapper)

      ;(wrapper.vm as any).ambiguousSelections = { m1: 'c1' }
      ;(wrapper.vm as any).ambiguousResetFields = true
      await flushPromises()

      await wrapper.find('[data-testid="ambiguous-save-btn"]').trigger('click')
      await flushPromises()

      expect(mockedBatchBind).toHaveBeenCalledWith([
        { model_id: 'm1', catalog_id: 'c1', reset_fields: true }
      ])
    })

    it('removes successfully bound rows from ambiguous and moves them to matched', async () => {
      setupAmbiguousResult()
      mockedBatchBind.mockResolvedValueOnce({ applied: 2, failed_ids: {} })

      const wrapper = mountPage()
      await flushPromises()
      await openAmbiguousDialog(wrapper)

      ;(wrapper.vm as any).ambiguousSelections = { m1: 'c2', m2: 'c3' }
      await flushPromises()
      await wrapper.find('[data-testid="ambiguous-save-btn"]').trigger('click')
      await flushPromises()

      const result = (wrapper.vm as any).autoBindResult
      expect(result.ambiguous.length).toBe(0)
      expect(result.matched.length).toBe(2)
      expect(result.matched.find((m: any) => m.model_id === 'm1').catalog_id).toBe('c2')
    })

    it('shows warning toast when some bindings fail', async () => {
      setupAmbiguousResult()
      mockedBatchBind.mockResolvedValueOnce({
        applied: 1,
        failed_ids: { m2: 'catalog not found' }
      })

      const wrapper = mountPage()
      await flushPromises()
      await openAmbiguousDialog(wrapper)

      ;(wrapper.vm as any).ambiguousSelections = { m1: 'c2', m2: 'c3' }
      await flushPromises()
      await wrapper.find('[data-testid="ambiguous-save-btn"]').trigger('click')
      await flushPromises()

      expect(ElMessage.warning).toHaveBeenCalled()
      const result = (wrapper.vm as any).autoBindResult
      // m1 被移到 matched；m2 还在 ambiguous
      expect(result.matched.length).toBe(1)
      expect(result.ambiguous.length).toBe(1)
      expect(result.ambiguous[0].model_id).toBe('m2')
    })
  })
})
