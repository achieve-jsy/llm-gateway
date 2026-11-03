import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SyncModelsDialog from './SyncModelsDialog.vue'
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
  confirmSync: vi.fn()
}))

import { ElMessage } from 'element-plus'
import { confirmSync } from '@/services/control-plane'

const mockedConfirm = vi.mocked(confirmSync)

const sampleSyncResult = {
  added: [
    { id: 'gpt-4o', owned_by: 'openai' },
    { id: 'gpt-4o-mini', owned_by: 'openai' }
  ],
  existing: [{ id: 'gpt-3.5-turbo', owned_by: 'openai' }],
  removed: [{ id: 'davinci', owned_by: 'openai' }],
  total_upstream: 4,
  total_local: 2,
  unsupported: false
}

function mountDialog(props: any = {}) {
  return mount(SyncModelsDialog, {
    props: {
      visible: true,
      providerId: 'p1',
      providerName: 'OpenAI',
      syncResult: sampleSyncResult,
      ...props
    },
    global: { stubs: elementStubsKebab }
  })
}

describe('SyncModelsDialog.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedConfirm.mockResolvedValue({ registered: 2, skipped: 0, disabled: 0 } as any)
  })

  describe('rendering', () => {
    it('renders dialog title with provider name', () => {
      const wrapper = mountDialog()
      expect(wrapper.find('.el-dialog-stub').attributes('data-title')).toBe('同步模型 — OpenAI')
    })

    it('shows stat counts for added/existing/removed and total', () => {
      const wrapper = mountDialog()
      const stats = wrapper.findAll('.stat-item')
      expect(stats.length).toBe(4)
      expect(stats[0].text()).toContain('4')
      expect(stats[1].text()).toContain('2')
      expect(stats[2].text()).toContain('1')
      expect(stats[3].text()).toContain('1')
    })

    it('does not render anything if syncResult is null', () => {
      const wrapper = mountDialog({ syncResult: null })
      expect(wrapper.find('.sync-stats').exists()).toBe(false)
    })

    it('hides dialog body when visible=false', () => {
      const wrapper = mountDialog({ visible: false })
      expect(wrapper.find('.el-dialog-stub').exists()).toBe(false)
    })
  })

  describe('search filter', () => {
    it('has a search input with correct placeholder', () => {
      const wrapper = mountDialog()
      const searchInput = wrapper.findAll('input').find((i) => i.attributes('placeholder')?.includes('搜索'))
      expect(searchInput).toBeDefined()
    })
  })

  /** 通过 visible: false→true 触发 watch */
  async function mountAndOpen(extraProps: any = {}) {
    const wrapper = mount(SyncModelsDialog, {
      props: {
        visible: false,
        providerId: 'p1',
        providerName: 'OpenAI',
        syncResult: sampleSyncResult,
        ...extraProps
      },
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    await wrapper.setProps({ visible: true })
    await flushPromises()
    return wrapper
  }

  describe('default selection behavior', () => {
    it('does NOT auto-select models when opened', async () => {
      const wrapper = await mountAndOpen()
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确认注册'))!
      expect(submit.text()).toContain('(0)')
      expect(submit.attributes('disabled')).toBeDefined()
    })

    it('shows hint text for empty selection on added tab', async () => {
      const wrapper = await mountAndOpen()
      const hint = wrapper.find('.sync-hint')
      expect(hint.exists()).toBe(true)
      expect(hint.text()).toContain('新模型可注册')
    })
  })

  describe('confirm flow', () => {
    it('emits saved and close after confirm success', async () => {
      const wrapper = await mountAndOpen()
      // Simulate selecting models by directly setting the internal state
      const vm = wrapper.vm as any
      vm.selectedModels = ['gpt-4o', 'gpt-4o-mini']
      await flushPromises()

      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确认注册'))!
      await submit.trigger('click')
      await flushPromises()
      expect(mockedConfirm).toHaveBeenCalledWith('p1', {
        models_to_add: ['gpt-4o', 'gpt-4o-mini']
      })
      expect(wrapper.emitted('saved')).toBeTruthy()
      expect(wrapper.emitted('close')).toBeTruthy()
      expect(ElMessage.success).toHaveBeenCalledWith(
        expect.stringContaining('注册完成')
      )
    })

    it('shows error on confirm failure', async () => {
      mockedConfirm.mockRejectedValueOnce(new Error('boom'))
      const wrapper = await mountAndOpen()
      const vm = wrapper.vm as any
      vm.selectedModels = ['gpt-4o']
      await flushPromises()

      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确认注册'))!
      await submit.trigger('click')
      await flushPromises()
      expect(ElMessage.error).toHaveBeenCalledWith('注册失败')
    })

    it('disables confirm button when nothing selected', async () => {
      const wrapper = mountDialog({
        syncResult: {
          added: [],
          existing: [],
          removed: [],
          total_upstream: 0,
          total_local: 0,
          unsupported: false
        }
      })
      await flushPromises()
      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确认注册'))!
      expect(submit.attributes('disabled')).toBeDefined()
    })

    it('emits close on cancel button', async () => {
      const wrapper = await mountAndOpen()
      const cancel = wrapper.findAll('.el-button-stub').find((b) => b.text() === '取消')!
      await cancel.trigger('click')
      expect(wrapper.emitted('close')).toBeTruthy()
    })

    it('shows loading state on confirm button', async () => {
      let resolve: (v: any) => void = () => {}
      mockedConfirm.mockReturnValueOnce(new Promise((r) => { resolve = r }) as any)
      const wrapper = await mountAndOpen()
      const vm = wrapper.vm as any
      vm.selectedModels = ['gpt-4o']
      await flushPromises()

      const submit = wrapper.findAll('.el-button-stub').find((b) => b.text().includes('确认注册'))!
      submit.trigger('click')
      await flushPromises()
      expect(submit.attributes('data-loading')).toBe('true')
      resolve({ registered: 1, skipped: 0, disabled: 0 })
      await flushPromises()
    })
  })

  describe('model type detection', () => {
    it('detects embedding type from model name', () => {
      const wrapper = mountDialog({
        syncResult: {
          ...sampleSyncResult,
          added: [{ id: 'text-embedding-3-small', owned_by: 'openai' }]
        }
      })
      const vm = wrapper.vm as any
      expect(vm.getModelType('text-embedding-3-small')).toBe('embedding')
    })

    it('detects vision type from model name', () => {
      const wrapper = mountDialog()
      const vm = wrapper.vm as any
      expect(vm.getModelType('qwen-vl-plus')).toBe('vision')
      expect(vm.getModelType('gpt-4-vision')).toBe('vision')
    })

    it('defaults to chat for regular models', () => {
      const wrapper = mountDialog()
      const vm = wrapper.vm as any
      expect(vm.getModelType('gpt-4o')).toBe('chat')
    })
  })

  describe('quick select actions', () => {
    it('selectChatOnly excludes embedding and vision models', async () => {
      const wrapper = mountDialog({
        syncResult: {
          ...sampleSyncResult,
          added: [
            { id: 'gpt-4o', owned_by: 'openai' },
            { id: 'text-embedding-3', owned_by: 'openai' },
            { id: 'qwen-vl-plus', owned_by: 'qwen' }
          ]
        }
      })
      const vm = wrapper.vm as any
      vm.handleSelectChatOnly()
      await flushPromises()
      expect(vm.selectedModels).toEqual(['gpt-4o'])
    })

    it('handleSelectAllAdded selects all added models', async () => {
      const wrapper = mountDialog()
      const vm = wrapper.vm as any
      vm.handleSelectAllAdded()
      await flushPromises()
      expect(vm.selectedModels).toContain('gpt-4o')
      expect(vm.selectedModels).toContain('gpt-4o-mini')
    })

    it('handleClearSelection clears all selections', async () => {
      const wrapper = mountDialog()
      const vm = wrapper.vm as any
      vm.selectedModels = ['gpt-4o']
      vm.handleClearSelection()
      await flushPromises()
      expect(vm.selectedModels).toEqual([])
    })
  })
})
