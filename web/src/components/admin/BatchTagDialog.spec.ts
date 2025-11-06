import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import BatchTagDialog from './BatchTagDialog.vue'
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
  getModelTags: vi.fn(),
  batchSetModelTags: vi.fn()
}))

import { ElMessage } from 'element-plus'
import { getModelTags, batchSetModelTags } from '@/services/control-plane'

const mockedGet = vi.mocked(getModelTags)
const mockedBatch = vi.mocked(batchSetModelTags)

const sampleTags = [
  { id: 't1', name: 'fast', color: '#0f0', description: '', sort_order: 0, created_at: '', updated_at: '' },
  { id: 't2', name: 'cheap', color: '#00f', description: '', sort_order: 1, created_at: '', updated_at: '' }
]

async function mountAndOpen(modelIds: string[] = ['m1', 'm2']) {
  const wrapper = mount(BatchTagDialog, {
    props: { visible: false, modelIds },
    global: { stubs: elementStubsKebab }
  })
  await wrapper.setProps({ visible: true })
  await flushPromises()
  return wrapper
}

describe('BatchTagDialog.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGet.mockResolvedValue(sampleTags as any)
    mockedBatch.mockResolvedValue({ updated: 2 } as any)
  })

  it('loads tags when dialog becomes visible', async () => {
    await mountAndOpen()
    expect(mockedGet).toHaveBeenCalled()
  })

  it('sets tagOptions to empty array on load failure', async () => {
    mockedGet.mockRejectedValueOnce(new Error('boom'))
    const wrapper = await mountAndOpen()
    expect(wrapper.findAll('option').length).toBe(0)
  })

  it('shows the count of selected models', async () => {
    const wrapper = await mountAndOpen(['a', 'b', 'c'])
    expect(wrapper.text()).toContain('已选择')
    expect(wrapper.text()).toContain('3')
  })

  it('warns when confirm clicked without selection', async () => {
    const wrapper = await mountAndOpen()
    const confirm = wrapper.findAll('.el-button-stub').find((b) => b.text() === '确认设置')!
    await confirm.trigger('click')
    await flushPromises()
    expect(ElMessage.warning).toHaveBeenCalledWith('请至少选择一个标签')
    expect(mockedBatch).not.toHaveBeenCalled()
  })

  it('calls batchSetModelTags with selected ids and modelIds', async () => {
    const wrapper = await mountAndOpen(['m1', 'm2'])
    const select = wrapper.findAllComponents({ name: 'ElSelectStub' })[0]
    select.vm.$emit('update:modelValue', ['t1'])
    await flushPromises()
    const confirm = wrapper.findAll('.el-button-stub').find((b) => b.text() === '确认设置')!
    await confirm.trigger('click')
    await flushPromises()
    expect(mockedBatch).toHaveBeenCalledWith(['m1', 'm2'], ['t1'])
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('2'))
  })

  it('emits saved on success', async () => {
    const wrapper = await mountAndOpen(['m1'])
    const select = wrapper.findAllComponents({ name: 'ElSelectStub' })[0]
    select.vm.$emit('update:modelValue', ['t1'])
    await flushPromises()
    const confirm = wrapper.findAll('.el-button-stub').find((b) => b.text() === '确认设置')!
    await confirm.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeTruthy()
  })

  it('shows error on batch failure', async () => {
    mockedBatch.mockRejectedValueOnce(new Error('boom'))
    const wrapper = await mountAndOpen(['m1'])
    const select = wrapper.findAllComponents({ name: 'ElSelectStub' })[0]
    select.vm.$emit('update:modelValue', ['t1'])
    await flushPromises()
    const confirm = wrapper.findAll('.el-button-stub').find((b) => b.text() === '确认设置')!
    await confirm.trigger('click')
    await flushPromises()
    expect(ElMessage.error).toHaveBeenCalledWith('批量设置标签失败')
  })

  it('emits close on cancel', async () => {
    const wrapper = await mountAndOpen()
    const cancel = wrapper.findAll('.el-button-stub').find((b) => b.text() === '取消')!
    await cancel.trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
