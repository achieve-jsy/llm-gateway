import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TagDialog from './TagDialog.vue'
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
  createModelTag: vi.fn(),
  updateModelTag: vi.fn()
}))

import { ElMessage } from 'element-plus'
import { createModelTag, updateModelTag } from '@/services/control-plane'

const mockedCreate = vi.mocked(createModelTag)
const mockedUpdate = vi.mocked(updateModelTag)

function mountDialog(props: any = {}) {
  return mount(TagDialog, {
    props: { visible: true, tag: null, ...props },
    global: { stubs: elementStubsKebab }
  })
}

describe('TagDialog.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedCreate.mockResolvedValue({ id: 't1' } as any)
    mockedUpdate.mockResolvedValue({ id: 't1' } as any)
  })

  describe('rendering', () => {
    it('shows create-mode title', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('新建标签')
    })

    it('shows edit-mode title and prefilled fields', async () => {
      const wrapper = mountDialog({
        tag: {
          id: 't1',
          name: 'fast',
          color: '#22cc88',
          description: 'fast tier',
          sort_order: 5,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('编辑标签')
      const name = findInputByLabel(wrapper, '标签名称')!
      expect((name.element as HTMLInputElement).value).toBe('fast')
    })

    it('renders default color preview swatch', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const preview = wrapper.find('.color-preview')
      expect(preview.exists()).toBe(true)
      expect((preview.element as HTMLElement).style.backgroundColor).toBeTruthy()
    })
  })

  describe('field interactions', () => {
    it('accepts name input (1-32 chars)', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const name = findInputByLabel(wrapper, '标签名称')!
      await name.setValue('a')
      expect((name.element as HTMLInputElement).value).toBe('a')
      const long = 'x'.repeat(32)
      await name.setValue(long)
      expect((name.element as HTMLInputElement).value).toBe(long)
    })

    it('clears name input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const name = findInputByLabel(wrapper, '标签名称')!
      await name.setValue('foo')
      await name.setValue('')
      expect((name.element as HTMLInputElement).value).toBe('')
    })

    it('accepts color input and updates preview swatch live', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const items = wrapper.findAll('.el-form-item-stub')
      const colorItem = items.find((it) => it.attributes('data-label') === '标签颜色')!
      const colorInput = colorItem.find('input')
      await colorInput.setValue('#ff0066')
      await flushPromises()
      const preview = wrapper.find('.color-preview')
      expect((preview.element as HTMLElement).style.backgroundColor).toBeTruthy()
    })

    it('accepts description textarea input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const desc = findInputByLabel(wrapper, '描述')!
      await desc.setValue('production tier tag')
      expect((desc.element as HTMLInputElement).value).toBe('production tier tag')
    })

    it('accepts sort_order number input', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const sort = findInputByLabel(wrapper, '排序')!
      await sort.setValue(99)
      expect(Number((sort.element as HTMLInputElement).value)).toBe(99)
    })
  })

  describe('submit (create)', () => {
    it('calls createModelTag with correct payload', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '标签名称')!.setValue('hot')
      const colorInput = wrapper
        .findAll('.el-form-item-stub')
        .find((it) => it.attributes('data-label') === '标签颜色')!
        .find('input')
      await colorInput.setValue('#ff8800')
      await findInputByLabel(wrapper, '描述')!.setValue('hot tier')
      await findInputByLabel(wrapper, '排序')!.setValue(3)
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(mockedCreate).toHaveBeenCalledTimes(1)
      const payload = mockedCreate.mock.calls[0][0] as any
      expect(payload.name).toBe('hot')
      expect(payload.color).toBe('#ff8800')
      expect(payload.description).toBe('hot tier')
      expect(payload.sort_order).toBe(3)
    })

    it('emits saved on successful create', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '标签名称')!.setValue('hot')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(wrapper.emitted('saved')).toBeTruthy()
      expect(ElMessage.success).toHaveBeenCalledWith('创建成功')
    })

    it('shows error on create failure', async () => {
      mockedCreate.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '标签名称')!.setValue('hot')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('创建失败')
    })

    it('emits close on cancel', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const cancel = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('取消'))!
      await cancel.trigger('click')
      expect(wrapper.emitted('close')).toBeTruthy()
    })

    it('shows loading state on submit button', async () => {
      let resolve: (v: any) => void = () => {}
      mockedCreate.mockReturnValueOnce(new Promise((r) => { resolve = r }) as any)
      const wrapper = mountDialog()
      await flushPromises()
      await findInputByLabel(wrapper, '标签名称')!.setValue('hot')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      submit.trigger('click')
      await flushPromises()
      expect(submit.attributes('data-loading')).toBe('true')
      resolve({ id: 't1' })
      await flushPromises()
    })
  })

  describe('submit (edit)', () => {
    it('calls updateModelTag with correct payload', async () => {
      const wrapper = mountDialog({
        tag: {
          id: 't1',
          name: 'old',
          color: '#000000',
          description: 'old desc',
          sort_order: 0,
          created_at: '',
          updated_at: ''
        }
      })
      await flushPromises()
      await findInputByLabel(wrapper, '标签名称')!.setValue('renamed')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      expect(mockedUpdate).toHaveBeenCalledWith('t1', expect.objectContaining({ name: 'renamed' }))
      expect(ElMessage.success).toHaveBeenCalledWith('更新成功')
    })
  })

  describe('hidden when not visible', () => {
    it('does not render body when visible=false', () => {
      const wrapper = mount(TagDialog, {
        props: { visible: false, tag: null },
        global: { stubs: elementStubsKebab }
      })
      expect(wrapper.find('.el-dialog-stub').exists()).toBe(false)
    })
  })

  describe('color picker alpha (S3-1)', () => {
    it('renders color picker with show-alpha=false (no alpha channel)', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const picker = wrapper.find('.el-color-picker-stub')
      expect(picker.exists()).toBe(true)
      expect(picker.attributes('data-show-alpha')).toBe('false')
    })

    it('color value from picker is passed through to payload as-is', async () => {
      const wrapper = mountDialog()
      await flushPromises()
      const colorItem = wrapper
        .findAll('.el-form-item-stub')
        .find((it) => it.attributes('data-label') === '标签颜色')!
      const picker = colorItem.find('input.el-color-picker-stub')
      await picker.setValue('#ab1234')
      await findInputByLabel(wrapper, '标签名称')!.setValue('myTag')
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确定'))!
      await submit.trigger('click')
      await flushPromises()
      const payload = mockedCreate.mock.calls[0][0] as any
      expect(payload.color).toBe('#ab1234')
      // must be 7 chars max (no alpha)
      expect(payload.color.length).toBeLessThanOrEqual(7)
    })
  })
})
