import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import TagListPage from './TagListPage.vue'
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
  deleteModelTag: vi.fn()
}))

import { ElMessage } from 'element-plus'
import { getModelTags, deleteModelTag } from '@/services/control-plane'

const mockedGet = vi.mocked(getModelTags)
const mockedDelete = vi.mocked(deleteModelTag)

const sampleTags = [
  {
    id: 't1',
    name: 'fast',
    color: '#22cc88',
    description: 'fast tier',
    sort_order: 1,
    created_at: '',
    updated_at: ''
  },
  {
    id: 't2',
    name: 'cheap',
    color: '#ffaa00',
    description: '',
    sort_order: 2,
    created_at: '',
    updated_at: ''
  }
]

const TagDialogStub = defineComponent({
  name: 'TagDialog',
  props: ['visible', 'tag'],
  emits: ['saved', 'close', 'update:visible'],
  setup(props) {
    return () =>
      h('div', {
        class: 'tag-dialog-stub',
        'data-visible': String(!!props.visible),
        'data-tag-id': props.tag?.id || ''
      })
  }
})

function mountPage() {
  return mount(TagListPage, {
    global: {
      stubs: {
        ...elementStubsKebab,
        TagDialog: TagDialogStub
      }
    }
  })
}

describe('TagListPage.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGet.mockImplementation(async () => sampleTags.map((t) => ({ ...t })) as any)
    mockedDelete.mockResolvedValue(undefined as any)
  })

  describe('initial load', () => {
    it('loads tags on mount', async () => {
      mountPage()
      await flushPromises()
      expect(mockedGet).toHaveBeenCalled()
    })

    it('renders one card per tag', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const cards = wrapper.findAll('.tag-card')
      expect(cards.length).toBe(2)
      expect(cards[0].text()).toContain('fast')
      expect(cards[1].text()).toContain('cheap')
    })

    it('renders color swatch styled with tag color', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const dots = wrapper.findAll('.color-dot')
      expect(dots.length).toBe(2)
      expect((dots[0].element as HTMLElement).style.backgroundColor).toBeTruthy()
    })

    it('shows description fallback when empty', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const cards = wrapper.findAll('.tag-card')
      expect(cards[0].text()).toContain('fast tier')
      expect(cards[1].text()).toContain('暂无描述')
    })

    it('shows empty state when no tags', async () => {
      mockedGet.mockResolvedValueOnce([] as any)
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.find('.el-empty-stub').exists()).toBe(true)
    })

    it('shows error when load fails', async () => {
      mockedGet.mockRejectedValueOnce(new Error('boom'))
      mountPage()
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('加载标签列表失败')
    })
  })

  describe('create/edit dialog', () => {
    it('opens dialog with no tag on new button', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const newBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '新建标签')!
      await newBtn.trigger('click')
      await flushPromises()
      const d = wrapper.find('.tag-dialog-stub')
      expect(d.attributes('data-visible')).toBe('true')
      expect(d.attributes('data-tag-id')).toBe('')
    })

    it('opens dialog with the clicked tag on edit', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const editBtn = wrapper.findAll('.el-button-stub').find((b) => b.text() === '编辑')!
      await editBtn.trigger('click')
      await flushPromises()
      const d = wrapper.find('.tag-dialog-stub')
      expect(d.attributes('data-tag-id')).toBe('t1')
    })

    it('reloads tags when dialog saved', async () => {
      const wrapper = mountPage()
      await flushPromises()
      mockedGet.mockClear()
      const dlg = wrapper.findComponent(TagDialogStub)
      dlg.vm.$emit('saved')
      await flushPromises()
      expect(mockedGet).toHaveBeenCalled()
    })
  })

  describe('delete', () => {
    it('calls deleteModelTag on popconfirm', async () => {
      const wrapper = mountPage()
      await flushPromises()
      const pop = wrapper.findAll('.el-popconfirm-stub').at(0)!
      await pop.trigger('click')
      await flushPromises()
      expect(mockedDelete).toHaveBeenCalledWith('t1')
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
