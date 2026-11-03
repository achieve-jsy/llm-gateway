import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ModelDialog from './ModelDialog.vue'
import { elementStubsKebab, findInputByLabel } from '@/test-utils/element-stubs'

vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn()
  },
  ElMessageBox: {
    confirm: vi.fn()
  }
}))

vi.mock('@/services/control-plane', () => ({
  createModel: vi.fn(),
  updateModel: vi.fn(),
  checkModelNameUnique: vi.fn(),
  getModelTags: vi.fn(),
  getModelTagsForModel: vi.fn(),
  setModelTags: vi.fn(),
  searchCatalog: vi.fn(),
  createModelFromCatalog: vi.fn(),
  bindModelCatalog: vi.fn(),
  unbindModelCatalog: vi.fn()
}))

vi.mock('@/services/http', () => ({
  default: {},
  resolveErrorMessage: vi.fn((err: any) => {
    const code = err?.response?.data?.error?.code
    const map: Record<string, string> = {
      model_name_conflict: '模型名称已存在，请换一个',
    }
    return code ? (map[code] ?? null) : null
  })
}))

import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createModel,
  updateModel,
  checkModelNameUnique,
  getModelTags,
  getModelTagsForModel,
  setModelTags,
  searchCatalog,
  createModelFromCatalog,
  bindModelCatalog,
  unbindModelCatalog
} from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'

const mockedCreate = vi.mocked(createModel)
const mockedUpdate = vi.mocked(updateModel)
const mockedCheck = vi.mocked(checkModelNameUnique)
const mockedGetTags = vi.mocked(getModelTags)
const mockedGetForModel = vi.mocked(getModelTagsForModel)
const mockedSetTags = vi.mocked(setModelTags)
const mockedResolveError = vi.mocked(resolveErrorMessage)
const mockedSearchCatalog = vi.mocked(searchCatalog)
const mockedCreateFromCatalog = vi.mocked(createModelFromCatalog)
const mockedBindCatalog = vi.mocked(bindModelCatalog)
const mockedUnbindCatalog = vi.mocked(unbindModelCatalog)
const mockedConfirm = vi.mocked(ElMessageBox.confirm)

const sampleProviders = [
  { id: 'pa', name: 'Provider A' } as any,
  { id: 'pb', name: 'Provider B' } as any
]

const sampleTags = [
  { id: 't1', name: 'fast', color: '#0f0', description: '', sort_order: 0, created_at: '', updated_at: '' },
  { id: 't2', name: 'cheap', color: '#00f', description: '', sort_order: 1, created_at: '', updated_at: '' }
]

function mountDialog(props: any = {}) {
  return mount(ModelDialog, {
    props: {
      visible: true,
      model: null,
      providers: sampleProviders,
      ...props
    },
    global: { stubs: elementStubsKebab }
  })
}

const sampleCatalogItem = {
  id: 'cat-1',
  source: 'litellm' as const,
  source_model_id: 'gpt-4o-2024-08-06',
  display_name: 'GPT-4o (2024-08-06)',
  provider: 'openai',
  mode: 'chat',
  context_length: 128000,
  max_output_tokens: 16384,
  input_price_per_million: 2.5,
  output_price_per_million: 10,
  cache_read_price: null,
  cache_write_price: null,
  supports_function_calling: true,
  supports_vision: true,
  supports_prompt_caching: false,
  supports_response_schema: true,
  supports_tool_choice: true,
  deprecation_date: null,
  synced_at: '2026-05-22T00:00:00Z'
}

describe('ModelDialog.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGetTags.mockResolvedValue(sampleTags as any)
    mockedGetForModel.mockResolvedValue([] as any)
    mockedCheck.mockResolvedValue(true)
    mockedCreate.mockResolvedValue({ id: 'm1' } as any)
    mockedUpdate.mockResolvedValue({ id: 'm1' } as any)
    mockedSetTags.mockResolvedValue([] as any)
    mockedSearchCatalog.mockResolvedValue({ data: [sampleCatalogItem], total: 1 } as any)
    mockedCreateFromCatalog.mockResolvedValue({ id: 'm-cat-1' } as any)
  })

  describe('rendering', () => {
    it('renders create-mode title', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('注册模型')
    })

    it('renders edit-mode title', async () => {
      const wrapper = mountDialog({
        model: {
          id: 'm1',
          name: 'gpt-4',
          provider_id: 'pa',
          model_type: 'chat',
          enabled: true,
          upstream_model_id: '',
          description: '',
          context_length: 0,
          max_output_tokens: 0,
          input_price: 0,
          output_price: 0,
          weight: 1,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('编辑模型')
    })

    it('loads available tags on mount', async () => {
      mountDialog()
      await flushPromises()
      expect(mockedGetTags).toHaveBeenCalled()
    })

    it('loads model tags for the editing model', async () => {
      mountDialog({
        model: {
          id: 'm1',
          name: 'gpt-4',
          provider_id: 'pa',
          model_type: 'chat',
          enabled: true,
          upstream_model_id: '',
          description: '',
          context_length: 0,
          max_output_tokens: 0,
          input_price: 0,
          output_price: 0,
          weight: 1,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      expect(mockedGetForModel).toHaveBeenCalledWith('m1')
    })
  })

  describe('field interactions', () => {
    it('accepts text input for name', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const name = findInputByLabel(wrapper, '模型名称')!
      await name.setValue('gpt-4o')
      expect((name.element as HTMLInputElement).value).toBe('gpt-4o')
    })

    it('disables name input in edit mode', async () => {
      const wrapper = mountDialog({
        model: {
          id: 'm1',
          name: 'gpt-4',
          provider_id: 'pa',
          model_type: 'chat',
          enabled: true,
          upstream_model_id: '',
          description: '',
          context_length: 0,
          max_output_tokens: 0,
          input_price: 0,
          output_price: 0,
          weight: 1,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      const name = findInputByLabel(wrapper, '模型名称')!
      expect((name.element as HTMLInputElement).disabled).toBe(true)
    })

    it('accepts upstream_model_id input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const ups = findInputByLabel(wrapper, '上游模型ID')!
      await ups.setValue('gpt-4o-2024-08-06')
      expect((ups.element as HTMLInputElement).value).toBe('gpt-4o-2024-08-06')
    })

    it('selects provider from dropdown', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const sel = findInputByLabel(wrapper, '归属渠道')!
      await sel.setValue('pa')
      expect((sel.element as HTMLSelectElement).value).toBe('pa')
    })

    it('switches model type between chat and embedding', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const type = findInputByLabel(wrapper, '模型类型')!
      await type.setValue('embedding')
      expect((type.element as HTMLSelectElement).value).toBe('embedding')
      await type.setValue('chat')
      expect((type.element as HTMLSelectElement).value).toBe('chat')
    })

    it('accepts multi-select for model tags', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const tagSel = findInputByLabel(wrapper, '模型标签')!
      expect(tagSel.attributes('data-multiple')).toBe('true')
    })

    it('accepts description textarea input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const desc = findInputByLabel(wrapper, '描述')!
      await desc.setValue('powerful chat model')
      expect((desc.element as HTMLInputElement).value).toBe('powerful chat model')
    })

    it('accepts context_length number input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const ctx = findInputByLabel(wrapper, '上下文长度')!
      await ctx.setValue(128000)
      expect(Number((ctx.element as HTMLInputElement).value)).toBe(128000)
    })

    it('accepts max_output_tokens', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const out = findInputByLabel(wrapper, '最大输出')!
      await out.setValue(4096)
      expect(Number((out.element as HTMLInputElement).value)).toBe(4096)
    })

    it('accepts price inputs', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const ip = findInputByLabel(wrapper, '输入价格')!
      const op = findInputByLabel(wrapper, '输出价格')!
      await ip.setValue(2.5)
      await op.setValue(10)
      expect(Number((ip.element as HTMLInputElement).value)).toBe(2.5)
      expect(Number((op.element as HTMLInputElement).value)).toBe(10)
    })

    it('accepts weight number input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const w = findInputByLabel(wrapper, '权重')!
      await w.setValue(5)
      expect(Number((w.element as HTMLInputElement).value)).toBe(5)
    })
  })

  describe('submit (create)', () => {
    it('calls createModel with correct payload', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('gpt-4o')
      await findInputByLabel(wrapper, '上游模型ID')!.setValue('gpt-4o-real')
      await findInputByLabel(wrapper, '归属渠道')!.setValue('pa')
      await findInputByLabel(wrapper, '描述')!.setValue('chat model')
      await findInputByLabel(wrapper, '上下文长度')!.setValue(128000)
      await findInputByLabel(wrapper, '最大输出')!.setValue(4096)
      await findInputByLabel(wrapper, '输入价格')!.setValue(2.5)
      await findInputByLabel(wrapper, '输出价格')!.setValue(10)
      await findInputByLabel(wrapper, '权重')!.setValue(3)

      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()

      expect(mockedCreate).toHaveBeenCalledTimes(1)
      const payload = mockedCreate.mock.calls[0][0] as any
      expect(payload.name).toBe('gpt-4o')
      expect(payload.upstream_model_id).toBe('gpt-4o-real')
      expect(payload.provider_id).toBe('pa')
      expect(payload.model_type).toBe('chat')
      expect(payload.description).toBe('chat model')
      expect(payload.context_length).toBe(128000)
      expect(payload.max_output_tokens).toBe(4096)
      expect(payload.input_price).toBe(2.5)
      expect(payload.output_price).toBe(10)
      expect(payload.weight).toBe(3)
    })

    it('calls setModelTags after create', async () => {
      mockedCreate.mockResolvedValue({ id: 'mNew' } as any)
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('gpt-4o')
      await findInputByLabel(wrapper, '归属渠道')!.setValue('pa')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(mockedSetTags).toHaveBeenCalledWith('mNew', [])
    })

    it('shows success message and emits saved on create', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('gpt-4o')
      await findInputByLabel(wrapper, '归属渠道')!.setValue('pa')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(ElMessage.success).toHaveBeenCalledWith('注册成功')
      expect(wrapper.emitted('saved')).toBeTruthy()
    })

    it('shows error message on create failure', async () => {
      mockedCreate.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('gpt-4o')
      await findInputByLabel(wrapper, '归属渠道')!.setValue('pa')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('注册失败')
    })

    it('shows loading state on submit', async () => {
      let resolve: (v: any) => void = () => {}
      mockedCreate.mockReturnValueOnce(new Promise((r) => { resolve = r }) as any)
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('gpt-4o')
      await findInputByLabel(wrapper, '归属渠道')!.setValue('pa')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      submit.trigger('click')
      await flushPromises()
      expect(submit.attributes('data-loading')).toBe('true')
      resolve({ id: 'm1' })
      await flushPromises()
    })

    it('emits close on cancel button', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const cancel = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('取消'))!
      await cancel.trigger('click')
      expect(wrapper.emitted('close')).toBeTruthy()
    })
  })

  describe('submit (edit)', () => {
    it('calls updateModel with correct payload', async () => {
      const wrapper = mountDialog({
        model: {
          id: 'm1',
          name: 'gpt-4',
          provider_id: 'pa',
          model_type: 'chat',
          enabled: true,
          upstream_model_id: '',
          description: '',
          context_length: 0,
          max_output_tokens: 0,
          input_price: 0,
          output_price: 0,
          weight: 1,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(mockedUpdate).toHaveBeenCalledWith('m1', expect.objectContaining({
        name: 'gpt-4',
        provider_id: 'pa',
        model_type: 'chat'
      }))
    })

    it('shows success on edit', async () => {
      const wrapper = mountDialog({
        model: {
          id: 'm1',
          name: 'gpt-4',
          provider_id: 'pa',
          model_type: 'chat',
          enabled: true,
          upstream_model_id: '',
          description: '',
          context_length: 0,
          max_output_tokens: 0,
          input_price: 0,
          output_price: 0,
          weight: 1,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(ElMessage.success).toHaveBeenCalledWith('更新成功')
    })
  })

  describe('name uniqueness check', () => {
    it('debounces uniqueness check on name input', async () => {
      vi.useFakeTimers()
      const wrapper = mountDialog()
      await flushPromises()
      const name = findInputByLabel(wrapper, '模型名称')!
      await name.setValue('gpt-4')
      expect(mockedCheck).not.toHaveBeenCalled()
      vi.advanceTimersByTime(350)
      await flushPromises()
      expect(mockedCheck).toHaveBeenCalledWith('gpt-4')
      vi.useRealTimers()
    })

    it('does not call uniqueness check in edit mode', async () => {
      vi.useFakeTimers()
      const wrapper = mountDialog({
        model: {
          id: 'm1',
          name: 'gpt-4',
          provider_id: 'pa',
          model_type: 'chat',
          enabled: true,
          upstream_model_id: '',
          description: '',
          context_length: 0,
          max_output_tokens: 0,
          input_price: 0,
          output_price: 0,
          weight: 1,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      vi.advanceTimersByTime(500)
      await flushPromises()
      expect(mockedCheck).not.toHaveBeenCalled()
      vi.useRealTimers()
    })
  })

  describe('hidden when not visible', () => {
    it('does not render dialog body when visible=false', () => {
      const wrapper = mount(ModelDialog, {
        props: { visible: false, model: null, providers: sampleProviders },
        global: { stubs: elementStubsKebab }
      })
      expect(wrapper.find('.el-dialog-stub').exists()).toBe(false)
    })
  })

  describe('catalog autocomplete (T8)', () => {
    it('triggers searchCatalog when name input ≥ 2 chars (after debounce)', async () => {
      vi.useFakeTimers()
      const wrapper = mountDialog()
      await flushPromises()
      const name = findInputByLabel(wrapper, '模型名称')!
      await name.setValue('gp')
      expect(mockedSearchCatalog).not.toHaveBeenCalled()
      vi.advanceTimersByTime(350)
      await flushPromises()
      expect(mockedSearchCatalog).toHaveBeenCalledWith(
        expect.objectContaining({ q: 'gp', limit: 10 })
      )
      vi.useRealTimers()
    })

    it('does NOT trigger searchCatalog when name input < 2 chars', async () => {
      vi.useFakeTimers()
      const wrapper = mountDialog()
      await flushPromises()
      const name = findInputByLabel(wrapper, '模型名称')!
      await name.setValue('g')
      vi.advanceTimersByTime(400)
      await flushPromises()
      expect(mockedSearchCatalog).not.toHaveBeenCalled()
      vi.useRealTimers()
    })

    it('debounces multiple rapid keystrokes into one call (300ms)', async () => {
      vi.useFakeTimers()
      const wrapper = mountDialog()
      await flushPromises()
      const name = findInputByLabel(wrapper, '模型名称')!
      await name.setValue('gp')
      vi.advanceTimersByTime(100)
      await name.setValue('gpt')
      vi.advanceTimersByTime(100)
      await name.setValue('gpt-')
      vi.advanceTimersByTime(100)
      await name.setValue('gpt-4')
      expect(mockedSearchCatalog).not.toHaveBeenCalled()
      vi.advanceTimersByTime(350)
      await flushPromises()
      expect(mockedSearchCatalog).toHaveBeenCalledTimes(1)
      expect(mockedSearchCatalog).toHaveBeenCalledWith(
        expect.objectContaining({ q: 'gpt-4' })
      )
      vi.useRealTimers()
    })

    it('auto-fills 5 fields when a catalog suggestion is selected', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      ;(wrapper.vm as any).applyCatalogSelection(sampleCatalogItem)
      await flushPromises()
      const ups = findInputByLabel(wrapper, '上游模型ID')!
      const ctx = findInputByLabel(wrapper, '上下文长度')!
      const out = findInputByLabel(wrapper, '最大输出')!
      const ip = findInputByLabel(wrapper, '输入价格')!
      const op = findInputByLabel(wrapper, '输出价格')!
      expect((ups.element as HTMLInputElement).value).toBe('gpt-4o-2024-08-06')
      expect(Number((ctx.element as HTMLInputElement).value)).toBe(128000)
      expect(Number((out.element as HTMLInputElement).value)).toBe(16384)
      expect(Number((ip.element as HTMLInputElement).value)).toBe(2.5)
      expect(Number((op.element as HTMLInputElement).value)).toBe(10)
    })

    it('warns "此字段将不再随目录自动更新" when context_length changes after selecting a catalog', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      ;(wrapper.vm as any).applyCatalogSelection(sampleCatalogItem)
      await flushPromises()
      ;(ElMessage.warning as any).mockClear()
      const ctx = findInputByLabel(wrapper, '上下文长度')!
      await ctx.setValue(64000)
      await flushPromises()
      expect(ElMessage.warning).toHaveBeenCalledWith(
        expect.stringContaining('此字段将不再随目录自动更新')
      )
    })

    it('submits via createModelFromCatalog when a catalog is selected', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      ;(wrapper.vm as any).applyCatalogSelection(sampleCatalogItem)
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('gpt-4o')
      await findInputByLabel(wrapper, '归属渠道')!.setValue('pa')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(mockedCreateFromCatalog).toHaveBeenCalledTimes(1)
      expect(mockedCreateFromCatalog).toHaveBeenCalledWith({
        catalog_id: 'cat-1',
        provider_id: 'pa',
        name: 'gpt-4o'
      })
      expect(mockedCreate).not.toHaveBeenCalled()
    })

    it('submits via createModel when no catalog is selected (manual path)', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('my-model')
      await findInputByLabel(wrapper, '归属渠道')!.setValue('pa')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(mockedCreate).toHaveBeenCalledTimes(1)
      expect(mockedCreateFromCatalog).not.toHaveBeenCalled()
    })
  })

  describe('error code e2e (S3-2)', () => {
    it('shows Chinese copy when BE returns model_name_conflict on submit', async () => {
      mockedCreate.mockRejectedValueOnce({
        response: { status: 409, data: { error: { code: 'model_name_conflict' } } }
      })
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('gpt-4')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith(
        expect.stringContaining('模型名称已存在')
      )
    })

    it('falls back to generic error when no machine code', async () => {
      mockedCreate.mockRejectedValueOnce(new Error('network error'))
      mockedResolveError.mockReturnValueOnce(null)
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '模型名称')!.setValue('my-model')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('注册失败')
    })
  })

  // ===================== CAT-2: 编辑模式的目录绑定/换绑/解绑 =====================
  describe('catalog binding (CAT-2, edit mode)', () => {
    const editModelBound = {
      id: 'm-edit-1',
      name: 'my-deepseek',
      provider_id: 'pa',
      model_type: 'chat',
      enabled: true,
      upstream_model_id: 'old-id',
      description: '',
      context_length: 32000,
      max_output_tokens: 4096,
      input_price: 1,
      output_price: 2,
      weight: 1,
      created_at: '',
      updated_at: '',
      catalog_id: 'cat-1',
      catalog: {
        id: 'cat-1',
        source: 'litellm',
        source_model_id: 'azure_ai/deepseek-r1',
        display_name: null,
        provider: 'azure_ai',
        mode: 'chat',
        context_length: 128000,
        max_output_tokens: 4096,
        input_price_per_million: 2.5,
        output_price_per_million: 10,
        cache_read_price: null,
        cache_write_price: null,
        supports_function_calling: true,
        supports_vision: false,
        supports_prompt_caching: false,
        supports_response_schema: false,
        supports_tool_choice: false,
        deprecation_date: null,
        synced_at: ''
      }
    } as any

    const editModelUnbound = {
      ...editModelBound,
      id: 'm-edit-2',
      catalog_id: null,
      catalog: null
    } as any

    it('shows current catalog binding label in edit mode', async () => {
      const wrapper = mountDialog({ model: editModelBound })
      await flushPromises()
      const binding = wrapper.find('[data-testid="catalog-binding"]')
      expect(binding.exists()).toBe(true)
      expect(binding.text()).toContain('azure_ai/deepseek-r1')
      expect(wrapper.find('[data-testid="btn-switch-catalog"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="btn-unbind-catalog"]').exists()).toBe(true)
    })

    it('shows "select catalog" button when model is unbound', async () => {
      const wrapper = mountDialog({ model: editModelUnbound })
      await flushPromises()
      const binding = wrapper.find('[data-testid="catalog-binding"]')
      expect(binding.exists()).toBe(true)
      expect(binding.text()).toContain('未关联任何目录条目')
      expect(wrapper.find('[data-testid="btn-pick-catalog"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="btn-switch-catalog"]').exists()).toBe(false)
    })

    it('does NOT show binding section in create mode', async () => {
      const wrapper = mountDialog({ model: null })
      await flushPromises()
      expect(wrapper.find('[data-testid="catalog-binding"]').exists()).toBe(false)
    })

    it('opens picker dialog and runs search when [切换] clicked', async () => {
      const wrapper = mountDialog({ model: editModelBound })
      await flushPromises()
      await wrapper.find('[data-testid="btn-switch-catalog"]').trigger('click')
      await flushPromises()
      // 默认用模型名预填关键字 → 应该立刻发起 search（debounce 300ms）
      await new Promise((r) => setTimeout(r, 320))
      await flushPromises()
      expect(mockedSearchCatalog).toHaveBeenCalledWith(
        expect.objectContaining({ q: 'my-deepseek' })
      )
    })

    it('confirms bind with reset_fields=false by default', async () => {
      mockedBindCatalog.mockResolvedValueOnce({ id: 'm-edit-1' } as any)
      const wrapper = mountDialog({ model: editModelBound })
      await flushPromises()
      // 直接调用 exposed openCatalogPicker 简化测试
      ;(wrapper.vm as any).openCatalogPicker()
      await flushPromises()
      await new Promise((r) => setTimeout(r, 320))
      await flushPromises()
      // 模拟选中第一项
      const items = wrapper.findAll('[data-testid="picker-item"]')
      expect(items.length).toBeGreaterThan(0)
      await items[0].trigger('click')
      // 点确认
      await wrapper.find('[data-testid="picker-confirm"]').trigger('click')
      await flushPromises()
      expect(mockedBindCatalog).toHaveBeenCalledWith('m-edit-1', {
        catalog_id: 'cat-1',
        reset_fields: false
      })
      expect(ElMessage.success).toHaveBeenCalledWith('已切换目录关联')
      expect(wrapper.emitted('saved')).toBeTruthy()
    })

    it('passes reset_fields=true when checkbox checked', async () => {
      mockedBindCatalog.mockResolvedValueOnce({ id: 'm-edit-1' } as any)
      const wrapper = mountDialog({ model: editModelBound })
      await flushPromises()
      ;(wrapper.vm as any).openCatalogPicker()
      await flushPromises()
      await new Promise((r) => setTimeout(r, 320))
      await flushPromises()
      const items = wrapper.findAll('[data-testid="picker-item"]')
      await items[0].trigger('click')
      // 勾选 reset_fields：el-checkbox stub 是 label > input，setValue 需要作用到内层 input
      const cbInput = wrapper.find('[data-testid="picker-reset-fields"] input')
      await cbInput.setValue(true)
      await wrapper.find('[data-testid="picker-confirm"]').trigger('click')
      await flushPromises()
      expect(mockedBindCatalog).toHaveBeenCalledWith('m-edit-1', {
        catalog_id: 'cat-1',
        reset_fields: true
      })
      expect(ElMessage.success).toHaveBeenCalledWith('已切换并按目录重置字段')
    })

    it('unbind invokes confirm dialog and unbindModelCatalog on confirmation', async () => {
      mockedConfirm.mockResolvedValueOnce('confirm')
      mockedUnbindCatalog.mockResolvedValueOnce({ id: 'm-edit-1' } as any)
      const wrapper = mountDialog({ model: editModelBound })
      await flushPromises()
      await wrapper.find('[data-testid="btn-unbind-catalog"]').trigger('click')
      await flushPromises()
      expect(mockedConfirm).toHaveBeenCalled()
      expect(mockedUnbindCatalog).toHaveBeenCalledWith('m-edit-1')
      expect(ElMessage.success).toHaveBeenCalledWith('已解除目录关联')
      expect(wrapper.emitted('saved')).toBeTruthy()
    })

    it('unbind aborts when user cancels confirm dialog', async () => {
      mockedConfirm.mockRejectedValueOnce('cancel')
      const wrapper = mountDialog({ model: editModelBound })
      await flushPromises()
      await wrapper.find('[data-testid="btn-unbind-catalog"]').trigger('click')
      await flushPromises()
      expect(mockedUnbindCatalog).not.toHaveBeenCalled()
    })
  })
})
