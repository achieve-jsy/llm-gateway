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
  batchBindModelCatalog: vi.fn(),
  getCatalogBindingStats: vi.fn(),
  getCatalogSyncStatus: vi.fn()
}))

import { ElMessage } from 'element-plus'
import {
  getCatalogDiff,
  applyCatalogDiff,
  triggerCatalogSync,
  searchCatalog,
  autoBindCatalog,
  batchBindModelCatalog,
  getCatalogBindingStats,
  getCatalogSyncStatus
} from '@/services/control-plane'

const mockedGetDiff = vi.mocked(getCatalogDiff)
const mockedApply = vi.mocked(applyCatalogDiff)
const mockedTrigger = vi.mocked(triggerCatalogSync)
const mockedSearch = vi.mocked(searchCatalog)
const mockedAutoBind = vi.mocked(autoBindCatalog)
const mockedBatchBind = vi.mocked(batchBindModelCatalog)
const mockedBindingStats = vi.mocked(getCatalogBindingStats)
const mockedSyncStatus = vi.mocked(getCatalogSyncStatus)

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
      total: 1
    } as any)
    // 默认场景：目录有 2719 条，本地 3 个 model 全部绑定且与 catalog 一致
    // → diff 仍然渲染 sampleDiff（mockedGetDiff 返回 3 行），不会进空状态分支。
    // 各个 it 块如果想测空状态，自行 mockResolvedValueOnce 覆盖。
    mockedBindingStats.mockResolvedValue({
      catalog_total: 2719,
      model_total: 3,
      bound_count: 3,
      unbound_count: 0
    } as any)
    mockedAutoBind.mockResolvedValue({
      matched: [],
      ambiguous: [],
      unmatched: 0
    } as any)
    // 默认场景：同步从未跑过 → 横幅不显示，但 catalog_total 已有数据。
    mockedSyncStatus.mockResolvedValue({
      running: false,
      last_started_at: null,
      last_finished_at: null,
      last_duration_ms: 0,
      last_source: '',
      last_count: 0,
      last_error: '',
      catalog_total: 2719
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

    it('renders stage panel and section descriptions', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.find('[data-testid="stage-panel"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="action-board"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="tab-nav"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="tab-intro"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="workspace-grid"]').exists()).toBe(true)
      const text = wrapper.text()
      expect(text).toContain('你当前在做什么')
      expect(text).toContain('目录更新')
      expect(text).toContain('模型对应')
      expect(text).toContain('价格差异')
      expect(text).toContain('当前状态')
      expect(text).toContain('推荐动作')
      expect(text).toContain('可选动作')
      expect(text).toContain('绑定本地模型')
      expect(text).toContain('本地模型如何对应到目录参考项')
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
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()
      const applyBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('采用目录参考价格'))!
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
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()
      const applyBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('采用目录参考价格'))!
      await applyBtn.trigger('click')
      await flushPromises()
      expect(mockedGetDiff).toHaveBeenCalled()
    })

    it('shows a hint and does nothing when apply pressed with empty selection', async () => {
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()
      const applyBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('采用目录参考价格'))!
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
      ;(wrapper.vm as any).switchTab('sync')
      await flushPromises()
      const refreshBtn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('更新目录数据'))!
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
      ;(wrapper.vm as any).switchTab('bind')
      await flushPromises()
      const btn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('处理未对应模型'))!
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
      ;(wrapper.vm as any).switchTab('bind')
      await flushPromises()
      const btn = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('处理未对应模型'))!
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
      ;(wrapper.vm as any).switchTab('bind')
      await flushPromises()
      const btn = wrapper.findAll('.el-button-stub').find((b: any) => b.text().includes('处理未对应模型'))!
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

  // ===================== 修复 D：空状态语义化文案 + CTA =====================
  //
  // 历史 bug：diff 永远空（因为本地 model 没绑 catalog_id），空状态文案又把
  // catalogTotal 显示成 1（修复 A 已修），让用户以为"同步没生效"。
  // 修复 D 把"diff 为空"细分成 5 种 case，每种给出明确的下一步引导。
  describe('empty state — semantic guidance', () => {
    beforeEach(() => {
      // 这一组都测空 diff 场景
      mockedGetDiff.mockResolvedValue({ data: [], total: 0 } as any)
    })

    it('catalog_empty: when stats says catalog is empty, shows sync CTA', async () => {
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 0,
        model_total: 5,
        bound_count: 0,
        unbound_count: 5
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()

      expect((wrapper.vm as any).emptyKind).toBe('catalog_empty')
      const title = wrapper.find('[data-testid="empty-title"]').text()
      expect(title).toMatch(/目录为空|同步尚未生效/)
      expect(wrapper.find('[data-testid="empty-cta-sync"]').exists()).toBe(true)
    })

    it('no_local_models: when catalog has data but no local models, shows model-management CTA', async () => {
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 2719,
        model_total: 0,
        bound_count: 0,
        unbound_count: 0
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()

      expect((wrapper.vm as any).emptyKind).toBe('no_local_models')
      expect(wrapper.find('[data-testid="empty-cta-models"]').exists()).toBe(true)
    })

    it('all_unbound: when all local models are unbound, shows autobind CTA with count', async () => {
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 2719,
        model_total: 7,
        bound_count: 0,
        unbound_count: 7
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()

      expect((wrapper.vm as any).emptyKind).toBe('all_unbound')
      const cta = wrapper.find('[data-testid="empty-cta-autobind"]')
      expect(cta.exists()).toBe(true)
      expect(cta.text()).toContain('7')
      expect(cta.text()).toContain('未关联模型')
      expect(wrapper.text()).toContain('下一步建议')
    })

    it('mixed_no_diff: some bound (consistent) + some unbound → continue-autobind CTA', async () => {
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 2719,
        model_total: 10,
        bound_count: 4,
        unbound_count: 6
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()

      expect((wrapper.vm as any).emptyKind).toBe('mixed_no_diff')
      const cta = wrapper.find('[data-testid="empty-cta-autobind"]')
      expect(cta.exists()).toBe(true)
      expect(cta.text()).toContain('6')
      expect(cta.text()).toContain('未关联模型')
      expect(wrapper.text()).toContain('你现在不需要处理差异')
    })

    it('all_bound_no_diff: everything bound and consistent → no CTA, "all good" message', async () => {
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 2719,
        model_total: 8,
        bound_count: 8,
        unbound_count: 0
      } as any)
      const wrapper = mountPage()
      await flushPromises()

      expect((wrapper.vm as any).emptyKind).toBe('all_bound_no_diff')
      const title = wrapper.find('[data-testid="empty-title"]').text()
      expect(title).toMatch(/一切就绪/)
      // 这种状态下不应该有 CTA 按钮
      expect(wrapper.find('[data-testid="empty-cta-sync"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="empty-cta-autobind"]').exists()).toBe(false)
    })

    it('stats fetch fails → falls back to catalog_empty (safe default)', async () => {
      mockedBindingStats.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).switchTab('diff')
      await flushPromises()

      expect((wrapper.vm as any).emptyKind).toBe('catalog_empty')
      expect(wrapper.find('[data-testid="empty-cta-sync"]').exists()).toBe(true)
    })

    it('stage panel explains current step when unbound models exist', async () => {
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 2719,
        model_total: 10,
        bound_count: 4,
        unbound_count: 6
      } as any)
      const wrapper = mountPage()
      await flushPromises()

      const text = wrapper.text()
      expect(text).toContain('还有 6 个本地模型未关联 Catalog')
      expect(text).toContain('当前页面推荐你先处理“未关联模型”')
      expect(text).toContain('先处理未关联模型')
      expect(wrapper.find('[data-testid="recommended-action-bind"]').exists()).toBe(true)
    })

    it('recommends sync action when catalog is empty', async () => {
      mockedGetDiff.mockResolvedValueOnce({ data: [], total: 0 } as any)
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 0,
        model_total: 5,
        bound_count: 0,
        unbound_count: 5
      } as any)
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.text()).toContain('Catalog 目录还没有准备好')
      expect(wrapper.text()).toContain('先把目录同步下来')
      expect(wrapper.text()).toContain('阻塞')
      expect(wrapper.find('[data-testid="recommended-action-sync"]').exists()).toBe(true)
    })

    it('shows pending status for binding area when unbound models exist', async () => {
      mockedBindingStats.mockResolvedValueOnce({
        catalog_total: 2719,
        model_total: 10,
        bound_count: 4,
        unbound_count: 6
      } as any)
      const wrapper = mountPage()
      await flushPromises()

      const text = wrapper.text()
      expect(text).toContain('待处理')
      expect(text).toContain('很多“看不到差异”的情况，其实是因为模型还没绑定')
    })

    it('shows diff summary counts and risk note when diff rows exist', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('[data-testid="diff-explainer"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="deprecation-banner"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="group-followup"]').exists()).toBe(true)
      const text = wrapper.text()
      expect(text).toContain('待补价格')
      expect(text).toContain('已脱离目录')
      expect(text).toContain('废弃提醒')
      expect(text).toContain('执行后会直接把所选模型的本地价格改成目录里的参考价格')
      expect(text).toContain('采用目录参考价格')
      expect(text).toContain('选中这组')
    })

    it('group actions select price-unset and followup rows separately', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const vm = wrapper.vm as any
      expect(vm.priceUnsetRows.length).toBeGreaterThanOrEqual(0)
      expect(vm.followupRows.length).toBeGreaterThan(0)

      vm.selectFollowupRows()
      await flushPromises()
      expect(vm.selectedIds.length).toBe(vm.followupRows.length)
    })
  })

  // ===================== 修复 B：sync-status 横幅 + 轮询 =====================
  //
  // 历史 bug：手动刷新走 setTimeout(4000)，4 秒后无脑刷新页面，无论后端真的同步成不成功。
  // 修复 B 让前端轮询 sync-status，把后端 SyncStatus 的真实状态（running / 错误 / 来源）
  // 通过状态横幅明确告诉用户。
  describe('sync-status banner', () => {
    it('hides banner when sync has never run', async () => {
      // 默认 mock：running=false, last_finished_at=null, last_error="" → 不显示横幅
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.find('[data-testid="sync-status-banner"]').exists()).toBe(false)
    })

    it('shows info banner while sync is running', async () => {
      mockedSyncStatus.mockResolvedValueOnce({
        running: true,
        last_started_at: '2026-05-22T09:00:00Z',
        last_finished_at: null,
        last_duration_ms: 0,
        last_source: '',
        last_count: 0,
        last_error: '',
        catalog_total: 0
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      const banner = wrapper.find('[data-testid="sync-status-banner"]')
      expect(banner.exists()).toBe(true)
      expect(banner.attributes('data-type')).toBe('info')
      expect(banner.attributes('data-title')).toContain('正在同步')
    })

    it('shows error banner with last_error message when sync failed', async () => {
      mockedSyncStatus.mockResolvedValueOnce({
        running: false,
        last_started_at: '2026-05-22T09:00:00Z',
        last_finished_at: '2026-05-22T09:00:05Z',
        last_duration_ms: 5000,
        last_source: '',
        last_count: 0,
        last_error: 'upstream HTTP 500',
        catalog_total: 0
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      const banner = wrapper.find('[data-testid="sync-status-banner"]')
      expect(banner.exists()).toBe(true)
      expect(banner.attributes('data-type')).toBe('error')
      expect(banner.attributes('data-description')).toContain('upstream HTTP 500')
    })

    it('shows warning banner when remote failed and bundled fallback was used', async () => {
      mockedSyncStatus.mockResolvedValueOnce({
        running: false,
        last_started_at: '2026-05-22T09:00:00Z',
        last_finished_at: '2026-05-22T09:00:01Z',
        last_duration_ms: 800,
        last_source: 'bundled',
        last_count: 2719,
        last_error: '',
        catalog_total: 2719
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      const banner = wrapper.find('[data-testid="sync-status-banner"]')
      expect(banner.exists()).toBe(true)
      expect(banner.attributes('data-type')).toBe('warning')
      expect(banner.text()).toContain('内置兜底')
    })

    it('shows success banner with count + duration when remote sync succeeded', async () => {
      mockedSyncStatus.mockResolvedValueOnce({
        running: false,
        last_started_at: '2026-05-22T09:00:00Z',
        last_finished_at: '2026-05-22T09:00:02Z',
        last_duration_ms: 2300,
        last_source: 'remote',
        last_count: 2719,
        last_error: '',
        catalog_total: 2719
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      const banner = wrapper.find('[data-testid="sync-status-banner"]')
      expect(banner.exists()).toBe(true)
      expect(banner.attributes('data-type')).toBe('success')
      const desc = banner.attributes('data-description')
      expect(desc).toContain('2719')
      expect(desc).toMatch(/2\.[0-9]s/)
    })

    it('polls sync-status after manual refresh and reports success', async () => {
      // 默认 mock running=false 已经被设置；手动刷新点击后第一次轮询拿到 running=false，
      // 立即把 success toast 打出来。
      mockedSyncStatus.mockResolvedValue({
        running: false,
        last_started_at: '2026-05-22T09:00:00Z',
        last_finished_at: '2026-05-22T09:00:01Z',
        last_duration_ms: 1000,
        last_source: 'remote',
        last_count: 2719,
        last_error: '',
        catalog_total: 2719
      } as any)

      const wrapper = mountPage()
      await flushPromises()
      mockedSyncStatus.mockClear()

      const refreshBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('手动刷新'))!
      await refreshBtn.trigger('click')
      await flushPromises()

      expect(mockedTrigger).toHaveBeenCalled()
      expect(mockedSyncStatus).toHaveBeenCalled()
      expect(ElMessage.success).toHaveBeenCalled()
    })

    it('polls sync-status after manual refresh and reports error', async () => {
      mockedSyncStatus.mockResolvedValue({
        running: false,
        last_started_at: '2026-05-22T09:00:00Z',
        last_finished_at: '2026-05-22T09:00:05Z',
        last_duration_ms: 5000,
        last_source: '',
        last_count: 0,
        last_error: 'remote fetch failed and no bundled fallback',
        catalog_total: 0
      } as any)

      const wrapper = mountPage()
      await flushPromises()

      const refreshBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('手动刷新'))!
      await refreshBtn.trigger('click')
      await flushPromises()

      expect(ElMessage.error).toHaveBeenCalled()
      const errCall = (ElMessage.error as any).mock.calls[0][0]
      expect(errCall).toContain('remote fetch failed')
    })
  })
})
