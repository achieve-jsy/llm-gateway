import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import GroupEditDialog from '../GroupEditDialog.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

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

vi.mock('@/api/groups', () => ({
  groupsApi: {
    createGroup: vi.fn(),
    updateGroup: vi.fn(),
    listGroupModels: vi.fn(),
    addModelsToGroup: vi.fn(),
    removeModelFromGroup: vi.fn()
  }
}))

import { ElMessage } from 'element-plus'
import { groupsApi } from '@/api/groups'

const mockedCreateGroup = vi.mocked(groupsApi.createGroup)
const mockedUpdateGroup = vi.mocked(groupsApi.updateGroup)
const mockedListGroupModels = vi.mocked(groupsApi.listGroupModels)
const mockedAddModelsToGroup = vi.mocked(groupsApi.addModelsToGroup)

function mountDialog(props: any = {}) {
  return mount(GroupEditDialog, {
    props: {
      visible: true,
      group: null,
      ...props
    },
    global: {
      stubs: {
        ...elementStubsKebab,
        ModelGroupSelector: {
          template: '<div class="model-selector-stub"></div>',
          emits: ['confirm'],
          props: ['visible', 'strategy', 'existingModelIds']
        }
      }
    }
  })
}

const sampleGroup = {
  id: 1,
  name: 'My Group',
  description: 'test desc',
  strategy: 'round_robin' as const,
  strategy_label: '轮询',
  enabled: true,
  model_count: 3,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z'
}

describe('GroupEditDialog.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedCreateGroup.mockResolvedValue({ data: { data: { id: 1 } } } as any)
    mockedUpdateGroup.mockResolvedValue({ data: {} } as any)
    mockedListGroupModels.mockResolvedValue({ data: { data: [] } } as any)
    mockedAddModelsToGroup.mockResolvedValue({ data: {} } as any)
  })

  it('renders create mode title "创建模型组"', async () => {
    const wrapper = mountDialog({ visible: true, group: null })
    await flushPromises()
    expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('创建模型组')
  })

  it('renders edit mode title "编辑模型组"', async () => {
    const wrapper = mountDialog({ visible: true, group: sampleGroup })
    await flushPromises()
    expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('编辑模型组')
  })

  it('form validation: name required', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    const nameItem = wrapper
      .findAll('.el-form-item-stub')
      .find(item => item.attributes('data-label') === '名称')
    expect(nameItem).toBeTruthy()
    expect(nameItem!.attributes('data-prop')).toBe('name')
  })

  it('strategy radio group renders 3 options', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    const radioGroup = wrapper.find('.el-radio-group-stub')
    expect(radioGroup.exists()).toBe(true)
    const radios = radioGroup.findAll('.el-radio-stub')
    expect(radios.length).toBe(3)
    expect(radios[0].text()).toContain('轮询')
    expect(radios[1].text()).toContain('优先级')
    expect(radios[2].text()).toContain('加权')
  })

  it('"添加模型" button exists', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    const addBtn = wrapper.findAll('button').find(b => b.text() === '添加模型')
    expect(addBtn).toBeTruthy()
  })

  it('submit works without models (optional)', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    const submitBtn = wrapper.findAll('button').find(b => b.text() === '确定')
    expect(submitBtn).toBeTruthy()
    await submitBtn!.trigger('click')
    await flushPromises()
    // 模型是可选的不再弹警告，submit 应正常触发 API
    expect(ElMessage.warning).not.toHaveBeenCalled()
  })

  it('cancel button closes dialog', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    const cancelBtn = wrapper.findAll('button').find(b => b.text() === '取消')
    expect(cancelBtn).toBeTruthy()
    await cancelBtn!.trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.emitted('close')!.length).toBe(1)
  })
})
