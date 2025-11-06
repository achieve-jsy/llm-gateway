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
  }
}))

vi.mock('@/services/control-plane', () => ({
  createModel: vi.fn(),
  updateModel: vi.fn(),
  checkModelNameUnique: vi.fn(),
  getModelTags: vi.fn(),
  getModelTagsForModel: vi.fn(),
  setModelTags: vi.fn()
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

import { ElMessage } from 'element-plus'
import {
  createModel,
  updateModel,
  checkModelNameUnique,
  getModelTags,
  getModelTagsForModel,
  setModelTags
} from '@/services/control-plane'
import { resolveErrorMessage } from '@/services/http'

const mockedCreate = vi.mocked(createModel)
const mockedUpdate = vi.mocked(updateModel)
const mockedCheck = vi.mocked(checkModelNameUnique)
const mockedGetTags = vi.mocked(getModelTags)
const mockedGetForModel = vi.mocked(getModelTagsForModel)
const mockedSetTags = vi.mocked(setModelTags)
const mockedResolveError = vi.mocked(resolveErrorMessage)

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

describe('ModelDialog.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGetTags.mockResolvedValue(sampleTags as any)
    mockedGetForModel.mockResolvedValue([] as any)
    mockedCheck.mockResolvedValue(true)
    mockedCreate.mockResolvedValue({ id: 'm1' } as any)
    mockedUpdate.mockResolvedValue({ id: 'm1' } as any)
    mockedSetTags.mockResolvedValue([] as any)
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
})
