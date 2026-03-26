import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import ModelListPage from '../ModelListPage.vue'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Search: { template: '<i />' },
  Setting: { template: '<i />' },
}))

// Mock element-plus message
vi.mock('element-plus', async () => {
  const actual = await vi.importActual('element-plus')
  return {
    ...(actual as any),
    ElMessage: {
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
    },
    ElMessageBox: {
      confirm: vi.fn().mockResolvedValue('confirm'),
    },
  }
})

// Mock vue-router
const mockPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: mockPush,
  }),
}))

// Mock control-plane services
vi.mock('@/services/control-plane', () => ({
  getModels: vi.fn().mockResolvedValue({
    data: [
      {
        id: 'model-1',
        name: 'doubao-seedance-2-0-260128',
        provider_id: 'provider-1',
        model_type: 'video',
        enabled: true,
        context_length: 0,
        input_price: 0.1,
        output_price: 0.2,
        response_time: 120,
        consecutive_failures: 0,
        last_check_error: '',
        created_at: '2026-07-01T10:00:00Z',
        updated_at: '2026-07-08T14:30:00Z',
        weight: 1,
        description: '',
        max_output_tokens: 0,
        ctx_overridden: false,
        provider: { id: 'provider-1', name: '中转 A' },
      },
      {
        id: 'model-2',
        name: 'doubao-seedance-1-0-pro',
        provider_id: 'provider-2',
        model_type: 'video',
        enabled: true,
        context_length: 0,
        input_price: 0.15,
        output_price: 0.25,
        response_time: 0,
        consecutive_failures: 0,
        last_check_error: '',
        created_at: '2026-07-01T10:00:00Z',
        updated_at: '2026-07-08T14:30:00Z',
        weight: 1,
        description: '',
        max_output_tokens: 0,
        ctx_overridden: false,
        provider: { id: 'provider-2', name: '中转 B' },
      },
    ],
    total: 2,
  }),
  deleteModel: vi.fn().mockResolvedValue({}),
  updateModel: vi.fn().mockResolvedValue({}),
  getProviders: vi.fn().mockResolvedValue({
    data: [
      { id: 'provider-1', name: '中转 A' },
      { id: 'provider-2', name: '中转 B' },
    ],
  }),
  getModelTags: vi.fn().mockResolvedValue([]),
  batchGetModelTags: vi.fn().mockResolvedValue({}),
  batchDeleteModels: vi.fn().mockResolvedValue({ deleted: 0 }),
  batchUpdateModelStatus: vi.fn().mockResolvedValue({ updated: 0 }),
  resetModelOverride: vi.fn().mockResolvedValue({}),
  checkModel: vi.fn().mockResolvedValue({ success: true, response_time: 120 }),
}))

// Mock quota API
vi.mock('@/api/quota', () => ({
  QUOTA_DEFAULTS: {
    model: { rpm: 60, max_concurrency: 10 },
  },
  getModelQuota: vi.fn().mockResolvedValue({
    data: {
      data: {
        source: 'default',
        rpm: 60,
        max_concurrency: 10,
      },
    },
  }),
}))

// Mock child components to prevent their script setup from running
vi.mock('@/components/admin/ModelDialog.vue', () => ({
  default: { name: 'ModelDialog', template: '<div />', props: ['visible', 'model', 'providers'] },
}))
vi.mock('@/components/admin/BatchTagDialog.vue', () => ({
  default: { name: 'BatchTagDialog', template: '<div />', props: ['visible', 'modelIds'] },
}))
vi.mock('@/components/admin/ModelCheckSettingsDialog.vue', () => ({
  default: { name: 'ModelCheckSettingsDialog', template: '<div />', props: ['visible'] },
}))
vi.mock('@/components/QuotaEditDialog.vue', () => ({
  default: { name: 'QuotaEditDialog', template: '<div />', props: ['visible', 'subjectType', 'subjectId', 'subjectLabel', 'defaultRpm', 'defaultConcurrency'] },
}))

describe('ModelListPage - Video Tab', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  function mountComponent() {
    return mount(ModelListPage, {
      global: {
        plugins: [
          ElementPlus,
          createPinia(),
        ],
        stubs: {
          // Stub table so scoped slots don't try to access row data
          'el-table': { template: '<div class="el-table" />', props: ['data'] },
          'el-table-column': { template: '<div class="el-table-column" />', props: ['prop', 'label', 'type', 'width', 'min-width', 'sortable', 'align', 'fixed', 'show-overflow-tooltip'] },
          'el-button': { template: '<button class="el-button"><slot /></button>' },
          'el-tag': { template: '<span class="el-tag"><slot /></span>' },
          'el-input': { template: '<input class="el-input" />' },
          'el-select': { template: '<select class="el-select"><slot /></select>' },
          'el-option': { template: '<option class="el-option"><slot /></option>' },
          'el-pagination': { template: '<div class="el-pagination" />' },
          'el-switch': { template: '<div class="el-switch" />' },
          'el-tooltip': { template: '<div class="el-tooltip"><slot /></div>' },
          'el-popconfirm': { template: '<div class="el-popconfirm"><slot /></div>' },
          'el-icon': { template: '<span class="el-icon" />' },
          'el-button-group': { template: '<div class="el-button-group"><slot /></div>' },
          'el-dialog': { template: '<div class="el-dialog" />' },
          'el-form': { template: '<div class="el-form"><slot /></div>' },
          'el-form-item': { template: '<div class="el-form-item"><slot /></div>' },
          'el-divider': { template: '<hr />' },
          'el-tabs': { template: '<div class="el-tabs"><slot /></div>' },
          'el-tab-pane': { template: '<div class="el-tab-pane"><slot /></div>' },
          'el-radio-group': { template: '<div class="el-radio-group"><slot /></div>' },
          'el-radio': { template: '<div class="el-radio"><slot /></div>' },
          'el-checkbox': { template: '<div class="el-checkbox"><slot /></div>' },
          'el-input-number': { template: '<input class="el-input-number" />' },
          'el-descriptions': { template: '<div class="el-descriptions"><slot /></div>' },
          'el-descriptions-item': { template: '<div class="el-descriptions-item"><slot /></div>' },
          'el-timeline': { template: '<div class="el-timeline"><slot /></div>' },
          'el-timeline-item': { template: '<div class="el-timeline-item"><slot /></div>' },
          'el-empty': { template: '<div class="el-empty" />' },
          'el-avatar': { template: '<div class="el-avatar" />' },
          'el-card': { template: '<div class="el-card"><slot /></div>' },
          'el-link': { template: '<a class="el-link"><slot /></a>' },
          'el-badge': { template: '<div class="el-badge"><slot /></div>' },
          'el-alert': { template: '<div class="el-alert"><slot /></div>' },
          'router-link': { template: '<a class="router-link"><slot /></a>' },
          'router-view': { template: '<div class="router-view"><slot /></div>' },
        },
      },
    })
  }

  describe('Video Tab rendering', () => {
    it('renders the Video model type tab', () => {
      const wrapper = mountComponent()

      expect(wrapper.html()).toContain('Video')
    })

    it('renders all model type tabs', () => {
      const wrapper = mountComponent()

      expect(wrapper.html()).toContain('Chat')
      expect(wrapper.html()).toContain('Embedding')
      expect(wrapper.html()).toContain('Embedding-VL')
      expect(wrapper.html()).toContain('Rerank')
      expect(wrapper.html()).toContain('Video')
    })

    it('has "模型管理" page title', () => {
      const wrapper = mountComponent()

      expect(wrapper.text()).toContain('模型管理')
    })

    it('has "注册模型" button', () => {
      const wrapper = mountComponent()

      expect(wrapper.text()).toContain('注册模型')
    })
  })

  describe('MODEL_TYPE_LABEL', () => {
    it('Video type label is "Video"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.MODEL_TYPE_LABEL.video).toBe('Video')
    })

    it('chat type label is "Chat"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.MODEL_TYPE_LABEL.chat).toBe('Chat')
    })
  })

  describe('modelTypeTagType', () => {
    it('video type returns empty string (default style)', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.modelTypeTagType('video')).toBe('')
    })

    it('chat type returns primary', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.modelTypeTagType('chat')).toBe('primary')
    })

    it('embedding type returns success', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.modelTypeTagType('embedding')).toBe('success')
    })

    it('rerank type returns warning', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.modelTypeTagType('rerank')).toBe('warning')
    })

    it('unknown type returns info', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.modelTypeTagType('unknown')).toBe('info')
    })
  })

  describe('formatTokenCount', () => {
    it('formats token count >= 1M', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.formatTokenCount(2000000)).toBe('2.0M')
      expect(vm.formatTokenCount(1500000)).toBe('1.5M')
    })

    it('formats token count >= 1K', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.formatTokenCount(128000)).toBe('128K')
      expect(vm.formatTokenCount(1000)).toBe('1K')
    })

    it('returns string for counts < 1K', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.formatTokenCount(500)).toBe('500')
      expect(vm.formatTokenCount(0)).toBe('0')
    })
  })

  describe('shortenError', () => {
    it('maps timeout error to "连接超时"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('request timeout')).toBe('连接超时')
      expect(vm.shortenError('连接超时了')).toBe('连接超时')
    })

    it('maps 401 error to "Key无效"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('401 Unauthorized')).toBe('Key无效')
      expect(vm.shortenError('invalid key')).toBe('Key无效')
    })

    it('maps 403 error to "无权限"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('403 Forbidden')).toBe('无权限')
    })

    it('maps connection error to "连接失败"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('connection refused')).toBe('连接失败')
    })

    it('maps DNS error to "DNS解析失败"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('no such host')).toBe('DNS解析失败')
    })

    it('maps 502 error to "上游异常"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('502 Bad Gateway')).toBe('上游异常')
    })

    it('maps 503 error to "服务不可用"', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('503 Service Unavailable')).toBe('服务不可用')
    })

    it('returns empty string for empty input', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.shortenError('')).toBe('')
    })

    it('truncates long unmatched error messages', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      const result = vm.shortenError('SomeVeryLongErrorThatDoesNotMatchAnyPattern')
      expect(result.length).toBeLessThanOrEqual(13) // 10 chars + '...'
      expect(result).toContain('...')
    })
  })

  describe('goToStatusLogs', () => {
    it('navigates to model status history page', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      vm.goToStatusLogs({ id: 'model-1', name: 'test-model' })

      expect(mockPush).toHaveBeenCalledWith({
        path: '/admin/model-status-logs',
        query: { model_id: 'model-1' },
      })
    })
  })

  describe('formatDateTime', () => {
    it('formats ISO time to readable format', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      const result = vm.formatDateTime('2026-07-08T14:30:00Z')
      expect(result).toMatch(/2026-07-08/)
    })

    it('returns "-" for empty value', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.formatDateTime('')).toBe('-')
    })
  })

  describe('filter behavior', () => {
    it('resets page to 1 when filter changes', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      vm.currentPage = 5
      vm.handleFilterChange()

      expect(vm.currentPage).toBe(1)
    })

    it('resets page to 1 when model type changes', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      vm.currentPage = 3
      vm.handleModelTypeChange()

      expect(vm.currentPage).toBe(1)
    })

    it('resets page to 1 when page size changes', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      vm.currentPage = 3
      vm.handleSizeChange()

      expect(vm.currentPage).toBe(1)
    })
  })
})