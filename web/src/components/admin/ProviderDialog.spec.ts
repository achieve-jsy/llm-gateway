import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ProviderDialog from './ProviderDialog.vue'
import { elementStubsKebab, findInputByLabel } from '@/test-utils/element-stubs'

vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    warning: vi.fn(),
    error: vi.fn(),
    info: vi.fn()
  }
}))

vi.mock('@/services/control-plane', () => ({
  createProvider: vi.fn(),
  updateProvider: vi.fn(),
  getProviderDefinitions: vi.fn(),
  createModel: vi.fn()
}))

import { ElMessage } from 'element-plus'
import {
  createProvider,
  updateProvider,
  getProviderDefinitions,
  createModel
} from '@/services/control-plane'

const mockedCreate = vi.mocked(createProvider)
const mockedUpdate = vi.mocked(updateProvider)
const mockedGetDefs = vi.mocked(getProviderDefinitions)
const mockedCreateModel = vi.mocked(createModel)

const sampleDefinitions = [
  {
    id: 'openai',
    display_name: 'OpenAI',
    adapter: 'openai',
    base_url: 'https://api.openai.com/v1',
    credential_schema: {
      api_key: {
        type: 'string',
        required: true,
        secret: true,
        label: 'API Key',
        placeholder: 'sk-xxxx'
      }
    },
    vendor_options: {},
    description: 'OpenAI provider',
    icon_url: '',
    doc_url: '',
    default_models: ['gpt-4o', 'gpt-4o-mini'],
    builtin: true,
    enabled: true
  },
  {
    id: 'bedrock',
    display_name: 'AWS Bedrock',
    adapter: 'bedrock',
    base_url: '',
    credential_schema: {
      access_key: {
        type: 'string',
        required: true,
        secret: false,
        label: 'Access Key'
      },
      secret_key: {
        type: 'string',
        required: true,
        secret: true,
        label: 'Secret Key'
      }
    },
    vendor_options: {
      default_region: 'us-east-1'
    },
    description: 'AWS Bedrock provider',
    icon_url: '',
    doc_url: '',
    default_models: ['anthropic.claude-3-sonnet'],
    builtin: true,
    enabled: true
  },
  {
    id: 'azure-openai',
    display_name: 'Azure OpenAI',
    adapter: 'azure-openai',
    base_url: '',
    credential_schema: {
      api_key: {
        type: 'string',
        required: true,
        secret: true,
        label: 'API Key'
      }
    },
    vendor_options: {
      default_api_version: '2024-02-01'
    },
    description: 'Azure OpenAI provider',
    icon_url: '',
    doc_url: '',
    default_models: [],
    builtin: true,
    enabled: true
  }
]

function mountDialog(props: Partial<{ visible: boolean; provider: any }> = {}) {
  return mount(ProviderDialog, {
    props: {
      visible: true,
      provider: null,
      ...props
    },
    global: {
      stubs: elementStubsKebab
    }
  })
}

describe('ProviderDialog.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGetDefs.mockResolvedValue(sampleDefinitions as any)
    mockedCreate.mockResolvedValue({ id: 'p1', name: 'p1' } as any)
    mockedUpdate.mockResolvedValue({ id: 'p1' } as any)
    mockedCreateModel.mockResolvedValue({ id: 'm1' } as any)
  })

  describe('definition loading', () => {
    it('loads provider definitions on mount', async () => {
      mountDialog()
      await flushPromises()
      expect(mockedGetDefs).toHaveBeenCalled()
    })

    it('shows error when loading definitions fails', async () => {
      mockedGetDefs.mockRejectedValueOnce(new Error('boom'))
      mountDialog()
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('加载渠道类型列表失败')
    })
  })

  describe('basic fields', () => {
    it('renders create-mode title', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('新建渠道')
    })

    it('renders edit-mode title when provider is set', async () => {
      const wrapper = mountDialog({
        provider: {
          id: 'p1',
          name: 'My Provider',
          definition_id: 'openai',
          endpoint: 'https://api.openai.com/v1',
          priority: 5,
          weight: 2,
          timeout: 60,
          max_retries: 3,
          test_model: 'gpt-4',
          tag: 'prod',
          remark: 'note'
        } as any
      })
      await flushPromises()
      expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('编辑渠道')
    })

    it('accepts input for name field', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const nameInput = findInputByLabel(wrapper, '名称')
      expect(nameInput).toBeTruthy()
      await nameInput!.setValue('OpenAI Prod')
      expect((nameInput!.element as HTMLInputElement).value).toBe('OpenAI Prod')
    })

    it('clears name field correctly', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const nameInput = findInputByLabel(wrapper, '名称')!
      await nameInput.setValue('hello')
      await nameInput.setValue('')
      expect((nameInput.element as HTMLInputElement).value).toBe('')
    })

    it('accepts very long name input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const nameInput = findInputByLabel(wrapper, '名称')!
      const longName = 'x'.repeat(200)
      await nameInput.setValue(longName)
      expect((nameInput.element as HTMLInputElement).value).toBe(longName)
    })

    it('accepts input for endpoint field', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const ep = findInputByLabel(wrapper, 'Endpoint')!
      await ep.setValue('https://my.endpoint/v1')
      expect((ep.element as HTMLInputElement).value).toBe('https://my.endpoint/v1')
    })

    it('disables endpoint input when definition is bedrock', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('bedrock')
      await flushPromises()
      const ep = findInputByLabel(wrapper, 'Endpoint')!
      expect((ep.element as HTMLInputElement).disabled).toBe(true)
    })

    it('auto-fills endpoint from definition default base_url', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      const ep = findInputByLabel(wrapper, 'Endpoint')!
      expect((ep.element as HTMLInputElement).value).toBe('https://api.openai.com/v1')
    })
  })

  describe('credentials tab', () => {
    it('renders credential schema fields after definition selection', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      const apiKey = findInputByLabel(wrapper, 'API Key')
      expect(apiKey).toBeTruthy()
    })

    it('uses password input when secret=true', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      const apiKey = findInputByLabel(wrapper, 'API Key')!
      expect((apiKey.element as HTMLInputElement).type).toBe('password')
    })

    it('renders multiple credential fields for bedrock', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('bedrock')
      await flushPromises()
      const ak = findInputByLabel(wrapper, 'Access Key')
      const sk = findInputByLabel(wrapper, 'Secret Key')
      expect(ak).toBeTruthy()
      expect(sk).toBeTruthy()
    })
  })

  describe('advanced tab fields', () => {
    it('accepts priority/weight/timeout/max_retries number inputs', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      const priority = findInputByLabel(wrapper, '优先级')!
      const weight = findInputByLabel(wrapper, '权重')!
      const timeout = findInputByLabel(wrapper, '超时(秒)')!
      const retries = findInputByLabel(wrapper, '最大重试')!

      await priority.setValue(7)
      await weight.setValue(3)
      await timeout.setValue(45)
      await retries.setValue(5)

      expect(Number((priority.element as HTMLInputElement).value)).toBe(7)
      expect(Number((weight.element as HTMLInputElement).value)).toBe(3)
      expect(Number((timeout.element as HTMLInputElement).value)).toBe(45)
      expect(Number((retries.element as HTMLInputElement).value)).toBe(5)
    })

    it('accepts text input for test_model / tag / remark fields', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const tag = findInputByLabel(wrapper, '标签')!
      const remark = findInputByLabel(wrapper, '备注')!
      const tm = findInputByLabel(wrapper, '测试模型')!
      await tag.setValue('prod')
      await remark.setValue('main pool')
      await tm.setValue('gpt-4o')
      expect((tag.element as HTMLInputElement).value).toBe('prod')
      expect((remark.element as HTMLInputElement).value).toBe('main pool')
      expect((tm.element as HTMLInputElement).value).toBe('gpt-4o')
    })

    it('shows region field only when bedrock is selected', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      expect(findInputByLabel(wrapper, 'Region')).toBeFalsy()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('bedrock')
      await flushPromises()
      expect(findInputByLabel(wrapper, 'Region')).toBeTruthy()
    })

    it('shows API Version field only when azure-openai is selected', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      expect(findInputByLabel(wrapper, 'API Version')).toBeFalsy()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('azure-openai')
      await flushPromises()
      expect(findInputByLabel(wrapper, 'API Version')).toBeTruthy()
    })

    it('updates Bedrock endpoint when region changes', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('bedrock')
      await flushPromises()
      const region = findInputByLabel(wrapper, 'Region')!
      await region.setValue('eu-west-1')
      await flushPromises()
      const ep = findInputByLabel(wrapper, 'Endpoint')!
      expect((ep.element as HTMLInputElement).value).toBe(
        'https://bedrock-runtime.eu-west-1.amazonaws.com'
      )
    })
  })

  describe('submit (create)', () => {
    it('calls createProvider with correct payload on submit', async () => {
      const wrapper = mountDialog()
      await flushPromises()

      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()

      const name = findInputByLabel(wrapper, '名称')!
      await name.setValue('My OAI')
      const apiKey = findInputByLabel(wrapper, 'API Key')!
      await apiKey.setValue('sk-secret')

      const buttons = wrapper.findAll('.el-button-stub')
      const submitBtn = buttons.find((b) => b.text().includes('确定'))!
      await submitBtn.trigger('click')
      await flushPromises()

      expect(mockedCreate).toHaveBeenCalledTimes(1)
      const payload = mockedCreate.mock.calls[0][0] as any
      expect(payload.name).toBe('My OAI')
      expect(payload.definition_id).toBe('openai')
      expect(payload.endpoint).toBe('https://api.openai.com/v1')
      expect(payload.priority).toBe(0)
      expect(payload.weight).toBe(1)
      expect(payload.timeout).toBe(30)
      expect(payload.max_retries).toBe(2)
      expect(JSON.parse(payload.credentials)).toEqual({ api_key: 'sk-secret' })
    })

    it('emits saved after successful create', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      await findInputByLabel(wrapper, '名称')!.setValue('OAI')
      await findInputByLabel(wrapper, 'API Key')!.setValue('sk-x')

      const submitBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('确定'))!
      await submitBtn.trigger('click')
      await flushPromises()
      expect(wrapper.emitted('saved')).toBeTruthy()
    })

    it('shows success message on successful create', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      await findInputByLabel(wrapper, '名称')!.setValue('OAI')
      await findInputByLabel(wrapper, 'API Key')!.setValue('sk-x')

      const submitBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('确定'))!
      await submitBtn.trigger('click')
      await flushPromises()
      expect(ElMessage.success).toHaveBeenCalled()
    })

    it('registers prefill models after create', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      await findInputByLabel(wrapper, '名称')!.setValue('OAI')
      await findInputByLabel(wrapper, 'API Key')!.setValue('sk-x')

      const submitBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('确定'))!
      await submitBtn.trigger('click')
      await flushPromises()
      expect(mockedCreateModel).toHaveBeenCalled()
      const created = mockedCreateModel.mock.calls.map((c) => (c[0] as any).name)
      expect(created).toContain('gpt-4o')
      expect(created).toContain('gpt-4o-mini')
    })

    it('shows error message on failed create', async () => {
      mockedCreate.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      await findInputByLabel(wrapper, '名称')!.setValue('OAI')
      await findInputByLabel(wrapper, 'API Key')!.setValue('sk-x')

      const submitBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('确定'))!
      await submitBtn.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('创建失败')
    })

    it('emits close on cancel button', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const cancelBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('取消'))!
      await cancelBtn.trigger('click')
      expect(wrapper.emitted('close')).toBeTruthy()
    })

    it('shows loading state on submit button', async () => {
      let resolveCreate: (v: any) => void = () => {}
      mockedCreate.mockReturnValueOnce(new Promise((r) => { resolveCreate = r }) as any)
      const wrapper = mountDialog()
      await flushPromises()
      const def = findInputByLabel(wrapper, '渠道类型')!
      await def.setValue('openai')
      await flushPromises()
      await findInputByLabel(wrapper, '名称')!.setValue('OAI')
      await findInputByLabel(wrapper, 'API Key')!.setValue('sk-x')

      const submitBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('确定'))!
      submitBtn.trigger('click')
      await flushPromises()
      expect(submitBtn.attributes('data-loading')).toBe('true')
      resolveCreate({ id: 'p1' })
      await flushPromises()
    })
  })

  describe('submit (edit)', () => {
    it('calls updateProvider with correct payload', async () => {
      const wrapper = mountDialog({
        provider: {
          id: 'p1',
          name: 'Old',
          definition_id: 'openai',
          endpoint: 'https://api.openai.com/v1',
          priority: 5,
          weight: 2,
          timeout: 60,
          max_retries: 3,
          test_model: 'gpt-4',
          tag: 'prod',
          remark: 'note'
        } as any
      })
      await flushPromises()
      const name = findInputByLabel(wrapper, '名称')!
      await name.setValue('New Name')
      const submitBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('确定'))!
      await submitBtn.trigger('click')
      await flushPromises()
      expect(mockedUpdate).toHaveBeenCalledTimes(1)
      const [id, payload] = mockedUpdate.mock.calls[0]
      expect(id).toBe('p1')
      expect((payload as any).name).toBe('New Name')
      expect((payload as any).priority).toBe(5)
    })

    it('omits credentials when empty in edit mode', async () => {
      const wrapper = mountDialog({
        provider: {
          id: 'p1',
          name: 'Old',
          definition_id: 'openai',
          endpoint: 'https://api.openai.com/v1'
        } as any
      })
      await flushPromises()
      const submitBtn = wrapper
        .findAll('.el-button-stub')
        .find((b) => b.text().includes('确定'))!
      await submitBtn.trigger('click')
      await flushPromises()
      const payload = mockedUpdate.mock.calls[0][1] as any
      expect(payload.credentials).toBeUndefined()
    })
  })

  describe('hidden when not visible', () => {
    it('does not render dialog body when visible=false', () => {
      const wrapper = mount(ProviderDialog, {
        props: { visible: false, provider: null },
        global: { stubs: elementStubsKebab }
      })
      expect(wrapper.find('.el-dialog-stub').exists()).toBe(false)
    })
  })
})
