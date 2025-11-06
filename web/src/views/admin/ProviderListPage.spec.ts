import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import ProviderListPage from './ProviderListPage.vue'
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
  getProviders: vi.fn(),
  getProviderDefinitions: vi.fn(),
  getModels: vi.fn(),
  deleteProvider: vi.fn(),
  toggleProviderStatus: vi.fn(),
  updateProvider: vi.fn(),
  testProvider: vi.fn(),
  syncModels: vi.fn()
}))

vi.mock('@/services/http', () => ({
  default: {},
  resolveErrorMessage: vi.fn((err: any) => {
    const code = err?.response?.data?.error?.code
    const map: Record<string, string> = {
      provider_has_models: '该渠道下仍有模型，无法删除',
      provider_name_conflict: '渠道名称已存在，请换一个',
    }
    return code ? (map[code] ?? null) : null
  })
}))

import { ElMessage } from 'element-plus'
import {
  getProviders,
  getProviderDefinitions,
  getModels,
  deleteProvider,
  toggleProviderStatus,
  updateProvider,
  testProvider,
  syncModels
} from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'

const mockedGetProviders = vi.mocked(getProviders)
const mockedGetDefs = vi.mocked(getProviderDefinitions)
const mockedGetModels = vi.mocked(getModels)
const mockedDelete = vi.mocked(deleteProvider)
const mockedToggle = vi.mocked(toggleProviderStatus)
const mockedResolveError = vi.mocked(resolveErrorMessage)
const mockedUpdate = vi.mocked(updateProvider)
const mockedTest = vi.mocked(testProvider)
const mockedSync = vi.mocked(syncModels)

const sampleProviders = [
  {
    id: 'p1',
    name: 'OpenAI Prod',
    definition_id: 'openai',
    type: 'LLM',
    endpoint: 'https://api.openai.com/v1',
    credentials: '',
    enabled: true,
    weight: 1,
    priority: 5,
    status: 1,
    test_model: 'gpt-4',
    response_time: 250,
    balance: 0,
    remark: '',
    max_retries: 3,
    timeout: 30,
    tag: 'production',
    created_at: '',
    updated_at: ''
  },
  {
    id: 'p2',
    name: 'Anthropic',
    definition_id: 'anthropic',
    type: 'LLM',
    endpoint: 'https://api.anthropic.com/v1',
    credentials: '',
    enabled: false,
    weight: 1,
    priority: 1,
    status: 2,
    test_model: '',
    response_time: 0,
    balance: 0,
    remark: '',
    max_retries: 3,
    timeout: 30,
    tag: 'staging',
    created_at: '',
    updated_at: ''
  }
] as any[]

const ProviderDialogStub = defineComponent({
  name: 'ProviderDialog',
  props: ['visible', 'provider'],
  emits: ['saved', 'close', 'update:visible'],
  setup(props) {
    return () =>
      h('div', {
        class: 'provider-dialog-stub',
        'data-visible': String(!!props.visible),
        'data-provider-id': props.provider?.id || ''
      })
  }
})

const SyncModelsDialogStub = defineComponent({
  name: 'SyncModelsDialog',
  props: ['visible', 'providerId', 'providerName', 'syncResult'],
  emits: ['saved', 'close'],
  setup(props) {
    return () =>
      h('div', {
        class: 'sync-dialog-stub',
        'data-visible': String(!!props.visible),
        'data-provider-id': props.providerId
      })
  }
})

function mountPage() {
  return mount(ProviderListPage, {
    global: {
      stubs: {
        ...elementStubsKebab,
        ProviderDialog: ProviderDialogStub,
        SyncModelsDialog: SyncModelsDialogStub
      }
    }
  })
}

describe('ProviderListPage.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGetProviders.mockImplementation(async () =>
      sampleProviders.map((p) => ({ ...p }))
    )
    mockedGetDefs.mockResolvedValue([
      { id: 'openai', display_name: 'OpenAI' } as any,
      { id: 'anthropic', display_name: 'Anthropic' } as any
    ])
    mockedGetModels.mockResolvedValue({
      data: [{ id: 'm1', name: 'gpt-4', provider_id: 'p1' } as any],
      total: 1
    })
    mockedDelete.mockResolvedValue(undefined as any)
    mockedToggle.mockResolvedValue({ ...sampleProviders[0], status: 2 } as any)
    mockedUpdate.mockResolvedValue(undefined as any)
    mockedTest.mockResolvedValue({
      success: true,
      response_time: 120,
      status_code: 200,
      model_count: 5,
      chat_success: true,
      chat_model: 'gpt-4',
      chat_response_time: 200
    } as any)
    mockedSync.mockResolvedValue({
      added: [{ id: 'gpt-4o', owned_by: 'openai' }],
      existing: [],
      removed: [],
      total_upstream: 1,
      total_local: 0,
      unsupported: false
    } as any)
  })

  describe('initial load', () => {
    it('loads providers, definitions and models on mount', async () => {
      mountPage()
      await flushPromises()
      expect(mockedGetProviders).toHaveBeenCalled()
      expect(mockedGetDefs).toHaveBeenCalled()
      expect(mockedGetModels).toHaveBeenCalled()
    })

    it('shows error when providers loading fails', async () => {
      mockedGetProviders.mockRejectedValueOnce(new Error('boom'))
      mountPage()
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('加载渠道列表失败')
    })

    it('renders provider rows in the table', async () => {
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.text()).toContain('OpenAI Prod')
      expect(wrapper.text()).toContain('Anthropic')
    })
  })

  describe('tag filter', () => {
    it('filters providers by selected tag', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const tagSelect = wrapper.find('.header-actions select')
      await tagSelect.setValue('production')
      await flushPromises()
      expect(wrapper.text()).toContain('OpenAI Prod')
      expect(wrapper.text()).not.toContain('Anthropic')
    })
  })

  describe('create dialog', () => {
    it('opens dialog with no provider for new', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const newBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '新建渠道')!
      await newBtn.trigger('click')
      await flushPromises()
      const dialog = wrapper.find('.provider-dialog-stub')
      expect(dialog.attributes('data-visible')).toBe('true')
      expect(dialog.attributes('data-provider-id')).toBeFalsy()
    })

    it('opens dialog with provider for edit', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const editBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '编辑')!
      await editBtn.trigger('click')
      await flushPromises()
      const dialog = wrapper.find('.provider-dialog-stub')
      expect(dialog.attributes('data-visible')).toBe('true')
      expect(dialog.attributes('data-provider-id')).toBe('p1')
    })

    it('reloads data after dialog saved event', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGetProviders.mockClear()
      mockedGetModels.mockClear()
      const dialog = wrapper.findComponent(ProviderDialogStub)
      dialog.vm.$emit('saved')
      await flushPromises()
      expect(mockedGetProviders).toHaveBeenCalled()
      expect(mockedGetModels).toHaveBeenCalled()
    })
  })

  describe('toggle status', () => {
    it('calls toggleProviderStatus with current status flipped', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const buttons = wrapper.findAll('.el-button-stub')
      const disableBtn = buttons.find((b) => b.text() === '禁用')!
      await disableBtn.trigger('click')
      await flushPromises()
      expect(mockedToggle).toHaveBeenCalledWith('p1', false)
      expect(ElMessage.success).toHaveBeenCalledWith('已禁用')
    })

    it('uses returned Provider from toggle to update local state (S1-6)', async () => {
      const updatedProvider = { ...sampleProviders[0], status: 2, response_time: 999 }
      mockedToggle.mockResolvedValueOnce(updatedProvider as any)
      const wrapper = mountPage()
      await flushPromises()
      mockedGetProviders.mockClear()
      const disableBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '禁用')!
      await disableBtn.trigger('click')
      await flushPromises()
      // Should NOT refetch providers — uses returned value directly
      expect(mockedGetProviders).not.toHaveBeenCalled()
    })

    it('shows error when toggle fails', async () => {
      mockedToggle.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()
      const disableBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '禁用')!
      await disableBtn.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('状态切换失败')
    })
  })

  describe('test provider', () => {
    it('calls testProvider on test button click', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const testBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '测试')!
      await testBtn.trigger('click')
      await flushPromises()
      expect(mockedTest).toHaveBeenCalled()
      expect(mockedTest.mock.calls[0][0]).toBe('p1')
      expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('120ms'))
    })

    it('shows error on test failure', async () => {
      mockedTest.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()
      const testBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '测试')!
      await testBtn.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('测试请求异常，请检查网络')
    })
  })

  describe('sync models', () => {
    it('calls syncModels and opens sync dialog on success', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const syncBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '同步')!
      await syncBtn.trigger('click')
      await flushPromises()
      expect(mockedSync).toHaveBeenCalledWith('p1')
      const sd = wrapper.find('.sync-dialog-stub')
      expect(sd.attributes('data-visible')).toBe('true')
      expect(sd.attributes('data-provider-id')).toBe('p1')
    })

    it('warns when sync_not_supported error is returned', async () => {
      mockedSync.mockRejectedValueOnce({
        response: { data: { code: 'sync_not_supported' } }
      })
      const wrapper = mountPage()
      await flushPromises()
      const syncBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '同步')!
      await syncBtn.trigger('click')
      await flushPromises()
      expect(ElMessage.warning).toHaveBeenCalledWith('该渠道类型不支持自动同步')
    })

    it('shows generic sync error otherwise', async () => {
      mockedSync.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()
      const syncBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '同步')!
      await syncBtn.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('同步失败')
    })
  })

  describe('delete', () => {
    it('calls deleteProvider on confirm', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const popconfirm = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await popconfirm.trigger('click')
      await flushPromises()
      expect(mockedDelete).toHaveBeenCalledWith('p1')
      expect(ElMessage.success).toHaveBeenCalledWith('删除成功')
    })

    it('shows model-relation hint on 409', async () => {
      mockedDelete.mockRejectedValueOnce({ response: { status: 409 } })
      const wrapper = mountPage()
      await flushPromises()
      const popconfirm = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await popconfirm.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('请先删除关联模型')
    })

    it('shows generic delete error otherwise', async () => {
      mockedDelete.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()
      const popconfirm = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await popconfirm.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('删除失败')
    })
  })

  describe('error code e2e (S3-2)', () => {
    it('shows Chinese copy when BE returns provider_has_models on delete', async () => {
      mockedDelete.mockRejectedValueOnce({
        response: { status: 409, data: { error: { code: 'provider_has_models' } } }
      })
      // resolveErrorMessage mock returns the Chinese string for provider_has_models
      const wrapper = mountPage()
      await flushPromises()
      const popconfirm = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await popconfirm.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith(
        expect.stringContaining('该渠道下仍有模型')
      )
    })

    it('falls back to generic 409 message when no machine code on delete', async () => {
      mockedDelete.mockRejectedValueOnce({ response: { status: 409 } })
      mockedResolveError.mockReturnValueOnce(null)
      const wrapper = mountPage()
      await flushPromises()
      const popconfirm = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await popconfirm.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('请先删除关联模型')
    })
  })

  describe('inline update', () => {
    it('calls updateProvider when priority or weight changes', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const numberInputs = wrapper.findAll('.el-input-number-stub')
      expect(numberInputs.length).toBeGreaterThan(0)
      await numberInputs[0].setValue(99)
      await flushPromises()
      expect(mockedUpdate).toHaveBeenCalled()
    })
  })

  describe('sync dialog saved', () => {
    it('reloads models when sync dialog emits saved', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGetModels.mockClear()
      const sd = wrapper.findComponent(SyncModelsDialogStub)
      sd.vm.$emit('saved')
      await flushPromises()
      expect(mockedGetModels).toHaveBeenCalled()
    })
  })
})
