import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import ModelListPage from './ModelListPage.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn()
  }
}))

vi.mock('@/services/control-plane', () => ({
  getModels: vi.fn(),
  deleteModel: vi.fn(),
  updateModel: vi.fn(),
  getProviders: vi.fn(),
  getModelTags: vi.fn(),
  batchGetModelTags: vi.fn(),
  getModelTagsForModel: vi.fn(),
  resetModelOverride: vi.fn(),
  batchDeleteModels: vi.fn(),
  batchUpdateModelStatus: vi.fn()
}))

import { ElMessage } from 'element-plus'
import {
  getModels,
  deleteModel,
  updateModel,
  getProviders,
  getModelTags,
  batchGetModelTags,
  getModelTagsForModel,
  resetModelOverride
} from '@/services/control-plane'

const mockedGetModels = vi.mocked(getModels)
const mockedDelete = vi.mocked(deleteModel)
const mockedUpdate = vi.mocked(updateModel)
const mockedGetProviders = vi.mocked(getProviders)
const mockedGetTags = vi.mocked(getModelTags)
const mockedBatchGetModelTags = vi.mocked(batchGetModelTags)
const mockedGetForModel = vi.mocked(getModelTagsForModel)
const mockedResetOverride = vi.mocked(resetModelOverride)

const sampleModels = [
  {
    id: 'm1',
    name: 'gpt-4o',
    provider_id: 'p1',
    enabled: true,
    upstream_model_id: 'gpt-4o-2024',
    description: 'desc',
    context_length: 128000,
    max_output_tokens: 4096,
    input_price: 2.5,
    output_price: 10,
    weight: 1,
    created_at: '2026-05-23T10:20:30Z',
    updated_at: '2026-05-23T11:21:31Z'
  },
  {
    id: 'm2',
    name: 'text-embedding-3',
    provider_id: 'p2',
    enabled: false,
    upstream_model_id: '',
    description: '',
    context_length: 8192,
    max_output_tokens: 0,
    input_price: 0.13,
    output_price: 0,
    weight: 1,
    created_at: '2026-05-22T08:09:10Z',
    updated_at: '2026-05-22T09:10:11Z'
  }
]

const sampleProviders = [
  { id: 'p1', name: 'OpenAI' },
  { id: 'p2', name: 'OpenAI Embedding' }
]

const sampleTags = [
  { id: 't1', name: 'fast', color: '#0f0', description: '', sort_order: 0, created_at: '', updated_at: '' },
  { id: 't2', name: 'cheap', color: '#00f', description: '', sort_order: 1, created_at: '', updated_at: '' }
]

const ModelDialogStub = defineComponent({
  name: 'ModelDialog',
  props: ['visible', 'model', 'providers'],
  emits: ['saved', 'close', 'update:visible'],
  setup(props) {
    return () =>
      h('div', {
        class: 'model-dialog-stub',
        'data-visible': String(!!props.visible),
        'data-model-id': props.model?.id || ''
      })
  }
})

function mountPage() {
  return mount(ModelListPage, {
    global: {
      stubs: {
        ...elementStubsKebab,
        ModelDialog: ModelDialogStub
      }
    }
  })
}

describe('ModelListPage.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGetModels.mockImplementation(async () => ({
      data: sampleModels.map((m) => ({ ...m })),
      total: sampleModels.length
    }) as any)
    mockedGetProviders.mockResolvedValue({ data: sampleProviders, total: sampleProviders.length } as any)
    mockedGetTags.mockResolvedValue(sampleTags as any)
    mockedBatchGetModelTags.mockResolvedValue({} as any)
    mockedGetForModel.mockResolvedValue([] as any)
    mockedDelete.mockResolvedValue(undefined as any)
    mockedUpdate.mockResolvedValue(undefined as any)
    mockedResetOverride.mockResolvedValue({ id: 'm1' } as any)
  })

  describe('initial load', () => {
    it('loads models, providers, and tags on mount', async () => {
      mountPage()
      await flushPromises()
      expect(mockedGetModels).toHaveBeenCalled()
      expect(mockedGetProviders).toHaveBeenCalled()
      expect(mockedGetTags).toHaveBeenCalled()
    })

    it('shows error when models loading fails', async () => {
      mockedGetModels.mockRejectedValueOnce(new Error('boom'))
      mountPage()
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('加载模型列表失败')
    })

    it('renders model rows', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.text()).toContain('gpt-4o')
      expect(wrapper.text()).toContain('text-embedding-3')
    })

    it('keeps backend model order without frontend re-sorting', async () => {
      mockedGetModels.mockResolvedValueOnce({
        data: [
          { ...sampleModels[1], name: 'newest-model' },
          { ...sampleModels[0], name: 'older-model' }
        ],
        total: 2
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      const text = wrapper.text()
      expect(text.indexOf('newest-model')).toBeGreaterThan(-1)
      expect(text.indexOf('older-model')).toBeGreaterThan(-1)
      expect(text.indexOf('newest-model')).toBeLessThan(text.indexOf('older-model'))
    })

    it('renders created_at and updated_at columns', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.text()).toContain('2026-05-23 18:20:30')
      expect(wrapper.text()).toContain('2026-05-23 19:21:31')
    })

    it('shows error when providers loading fails', async () => {
      mockedGetProviders.mockRejectedValueOnce(new Error('boom'))
      mountPage()
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('加载渠道列表失败')
    })
  })

  describe('filters', () => {
    it('reloads models when provider filter changes', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGetModels.mockClear()
      ;(wrapper.vm as any).filterProviderId = 'p1'
      ;(wrapper.vm as any).handleFilterChange()
      await flushPromises()
      expect(mockedGetModels).toHaveBeenCalledWith('p1', expect.objectContaining({ page: 1 }))
    })

    it('reloads models when enabled filter changes', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGetModels.mockClear()
      ;(wrapper.vm as any).filterEnabled = 'false'
      ;(wrapper.vm as any).handleFilterChange()
      await flushPromises()
      expect(mockedGetModels).toHaveBeenCalledWith(undefined, expect.objectContaining({ page: 1, enabled: false }))
    })

    it('clears enabled filter to undefined when status select is cleared', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGetModels.mockClear()
      ;(wrapper.vm as any).filterEnabled = undefined
      ;(wrapper.vm as any).handleFilterChange()
      await flushPromises()
      expect(mockedGetModels).toHaveBeenCalledWith(undefined, expect.objectContaining({ page: 1, enabled: undefined }))
    })

    it('filters models by tag when tag filter changes', async () => {
      mockedBatchGetModelTags.mockResolvedValue({
        m1: [sampleTags[0]],
        m2: []
      } as any)
      const wrapper = mountPage()
      await flushPromises()
      ;(wrapper.vm as any).filterTagIds = ['t1']
      await flushPromises()
      expect(wrapper.findAll('.el-table-row')).toHaveLength(1)
      expect(wrapper.text()).toContain('gpt-4o')
      expect(wrapper.text()).not.toContain('text-embedding-3')
    })
  })

  describe('create/edit dialog', () => {
    it('opens dialog with no model on new button click', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const newBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '注册模型')!
      await newBtn.trigger('click')
      await flushPromises()
      const d = wrapper.find('.model-dialog-stub')
      expect(d.attributes('data-visible')).toBe('true')
      expect(d.attributes('data-model-id')).toBe('')
    })

    it('opens dialog with model on edit click', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const editBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '编辑')!
      await editBtn.trigger('click')
      await flushPromises()
      const d = wrapper.find('.model-dialog-stub')
      expect(d.attributes('data-model-id')).toBe('m1')
    })

    it('reloads models when dialog saved emits', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGetModels.mockClear()
      const dlg = wrapper.findComponent(ModelDialogStub)
      dlg.vm.$emit('saved')
      await flushPromises()
      expect(mockedGetModels).toHaveBeenCalled()
    })
  })

  describe('toggle enabled', () => {
    it('calls updateModel when switch is toggled', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const switches = wrapper.findAll('.el-switch-stub')
      expect(switches.length).toBe(2)
      await switches[0].setValue(false)
      await flushPromises()
      expect(mockedUpdate).toHaveBeenCalledWith('m1', expect.any(Object))
    })

    it('shows error when toggle fails', async () => {
      mockedUpdate.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()
      const switches = wrapper.findAll('.el-switch-stub')
      await switches[0].setValue(false)
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('状态切换失败')
    })
  })

  describe('catalog override (T10)', () => {
    const catalogModel = {
      id: 'm-cat',
      name: 'gpt-4o-cat',
      provider_id: 'p1',
      enabled: true,
      upstream_model_id: 'gpt-4o',
      description: '',
      context_length: 128000,
      max_output_tokens: 4096,
      input_price: 2.5,
      output_price: 10,
      weight: 1,
      created_at: '',
      updated_at: '',
      catalog_id: 'cat-1',
      ctx_overridden: true,
      max_output_overridden: false,
      catalog: {
        id: 'cat-1',
        source: 'litellm',
        source_model_id: 'gpt-4o',
        display_name: 'GPT-4o',
        provider: 'openai',
        mode: 'chat',
        context_length: 128000,
        max_output_tokens: 4096,
        input_price_per_million: 2.5,
        output_price_per_million: 10,
        cache_read_price: null,
        cache_write_price: null,
        supports_function_calling: true,
        supports_vision: true,
        supports_prompt_caching: true,
        supports_response_schema: false,
        supports_tool_choice: true,
        deprecation_date: '2026-12-31T00:00:00Z',
        synced_at: '2026-05-22T00:00:00Z'
      }
    }

    beforeEach(() => {
      mockedGetModels.mockImplementation(async () => ({
        data: [{ ...catalogModel }],
        total: 1
      }) as any)
      // 基线里 getProviders mock 返回纯数组（没有 {data,total} 包装），渲染时
      // 会让 providerOptions.value 变成 undefined，进而触发表格列里 getProviderName
      // 的 NPE。这里仅在 T10 套件里把 mock 调成 BE 真实返回形态，避免无关失败。
      mockedGetProviders.mockResolvedValue({ data: sampleProviders, total: sampleProviders.length } as any)
    })

    it('shows "已脱离目录" label next to context_length when ctx_overridden=true', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.text()).toContain('已脱离目录')
    })

    it('calls resetModelOverride when the restore button is clicked', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const restoreBtn = wrapper.find('.restore-btn')
      expect(restoreBtn.exists()).toBe(true)
      await restoreBtn.trigger('click')
      await flushPromises()
      expect(mockedResetOverride).toHaveBeenCalledWith('m-cat', 'context_length')
    })
  })

  describe('delete', () => {
    it('calls deleteModel on popconfirm confirm', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const pop = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await pop.trigger('click')
      await flushPromises()
      expect(mockedDelete).toHaveBeenCalledWith('m1')
      expect(ElMessage.success).toHaveBeenCalledWith('删除成功')
    })

    it('shows error on delete failure', async () => {
      mockedDelete.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()
      const pop = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await pop.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('删除失败')
    })
  })
})
