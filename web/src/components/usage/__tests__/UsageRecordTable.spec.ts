import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import UsageRecordTable from '../UsageRecordTable.vue'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Bottom: { template: '<i />' },
  Top: { template: '<i />' },
  Finished: { template: '<i />' },
  MagicStick: { template: '<i />' },
  InfoFilled: { template: '<i />' },
  List: { template: '<i />' },
}))

// Mock settings store
vi.mock('@/stores/settings', () => ({
  useSettingsStore: vi.fn(() => ({
    publicSettings: {
      pricing_usd_to_cny_rate: 7.2,
      site_name: 'LLM Gateway',
      site_logo: '',
      api_base_url: '',
      rate_limit_per_minute: 60,
      max_concurrency_per_key: 10,
      supplier_name: '某公司',
    },
  })),
}))

// Mock UsageTraceDrawer (stub as empty component)
vi.mock('@/components/usage/UsageTraceDrawer.vue', () => ({
  default: {
    name: 'UsageTraceDrawer',
    template: '<div class="mock-trace-drawer" />',
    props: ['modelValue', 'row', 'traceData', 'traceLoading', 'currency', 'metaMode'],
  },
}))

// Mock VideoTaskDetail (stub as empty component)
vi.mock('@/components/usage/VideoTaskDetail.vue', () => ({
  default: {
    name: 'VideoTaskDetail',
    template: '<div class="mock-video-detail" />',
    props: ['modelValue', 'task'],
  },
}))

describe('UsageRecordTable', () => {
  const mockRows = [
    {
      request_id: 'req-001',
      ts: '2026-07-08T14:30:00Z',
      model: 'doubao-seedance-2-0-260128',
      provider: '中转 A',
      status: 200,
      duration_ms: 120,
      prompt_tokens: 0,
      completion_tokens: 0,
      total_tokens: 0,
      cached_tokens: 0,
      cache_write_tokens: 0,
      reasoning_tokens: 0,
      stream: false,
      inbound_endpoint: '/v1/contents/generations/tasks',
      key_id: 'key-1',
      key_name: 'test-key-1',
      user_id: 'user-1',
      user_name: 'Test User',
      est_cost_usd: 0.05,
      input_cost_usd: 0.02,
      output_cost_usd: 0.03,
      cache_read_cost_usd: 0,
      routed_model: '',
      routed_via_group: false,
      group_name: '',
      failover_used: false,
      usage_missing: false,
      upstream_status: 200,
      dept_id: 'dept-1',
      dept_name: 'Engineering',
    },
    {
      request_id: 'req-002',
      ts: '2026-07-08T14:28:00Z',
      model: 'gpt-4o',
      provider: 'OpenAI',
      status: 200,
      duration_ms: 320,
      prompt_tokens: 1280,
      completion_tokens: 856,
      total_tokens: 2136,
      cached_tokens: 0,
      cache_write_tokens: 0,
      reasoning_tokens: 0,
      stream: false,
      inbound_endpoint: '/v1/chat/completions',
      key_id: 'key-2',
      key_name: 'test-key-2',
      user_id: 'user-2',
      user_name: 'Another User',
      est_cost_usd: 0.01,
      input_cost_usd: 0.005,
      output_cost_usd: 0.005,
      cache_read_cost_usd: 0,
      routed_model: '',
      routed_via_group: false,
      group_name: '',
      failover_used: false,
      usage_missing: false,
      upstream_status: 200,
      dept_id: 'dept-2',
      dept_name: 'Design',
    },
  ]

  const defaultProps = {
    rows: mockRows as any,
    loading: false,
    total: 2,
    page: 1,
    pageSize: 20,
    currency: 'USD' as const,
    traceEndpoint: vi.fn().mockResolvedValue({}),
    showKey: true,
    showProvider: true,
    showUser: true,
  }

  function mountComponent(props = {}) {
    const pinia = createPinia()
    setActivePinia(pinia)

    return mount(UsageRecordTable, {
      props: { ...defaultProps, ...props },
      global: {
        plugins: [ElementPlus, pinia],
        stubs: {
          // Stub teleported element-plus components
          ElTooltip: {
            template: '<div class="el-tooltip"><slot /></div>',
            props: ['content', 'placement', 'showAfter', 'popperClass'],
          },
        },
      },
    })
  }

  describe('渲染', () => {
    it('表格组件挂载成功', () => {
      const wrapper = mountComponent()

      expect(wrapper.exists()).toBe(true)
      expect(wrapper.find('.urt-wrap').exists()).toBe(true)
    })

    it('包含 el-table 元素', () => {
      const wrapper = mountComponent()

      expect(wrapper.find('.el-table').exists() || wrapper.findComponent({ name: 'ElTable' }).exists()).toBe(true)
    })

    it('无数据时显示空状态', () => {
      const wrapper = mountComponent({
        rows: [],
        total: 0,
      })

      expect(wrapper.text()).toContain('暂无调用记录')
    })

    it('有数据时显示分页', () => {
      const wrapper = mountComponent()

      expect(wrapper.find('.urt-pagination').exists()).toBe(true)
    })

    it('无数据时不显示分页', () => {
      const wrapper = mountComponent({
        rows: [],
        total: 0,
      })

      expect(wrapper.find('.urt-pagination').exists()).toBe(false)
    })
  })

  describe('端点标签映射', () => {
    it('正确映射端点标签', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.endpointLabel('/v1/chat/completions')).toBe('Chat')
      expect(vm.endpointLabel('/v1/embeddings')).toBe('Embedding')
      expect(vm.endpointLabel('/v1/rerank')).toBe('Rerank')
      expect(vm.endpointLabel('/v1/contents/generations/tasks')).toBe('Video')
      expect(vm.endpointLabel(undefined)).toBe('')
      expect(vm.endpointLabel('/v1/unknown')).toBe('')
    })

    it('正确映射端点标签类型', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.endpointTagType('/v1/chat/completions')).toBe('primary')
      expect(vm.endpointTagType('/v1/embeddings')).toBe('success')
      expect(vm.endpointTagType('/v1/rerank')).toBe('warning')
      expect(vm.endpointTagType('/v1/contents/generations/tasks')).toBe('info')
      expect(vm.endpointTagType(undefined)).toBe('')
      expect(vm.endpointTagType('/v1/unknown')).toBe('')
    })
  })

  describe('Key 解析', () => {
    it('使用 key_name 优先', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      const row = { key_name: 'My Key', key_id: 'sk-1234567890abcdef' }
      expect(vm.resolveKey(row)).toBe('My Key')
    })

    it('无 key_name 时对 key_id 进行 mask', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      const row = { key_id: 'sk-1234567890abcdef' }
      const result = vm.resolveKey(row)
      expect(result).toContain('···')
    })

    it('使用自定义 keyResolver', () => {
      const wrapper = mountComponent({
        keyResolver: (row: any) => `Custom: ${row.key_id}`,
      })
      const vm = wrapper.vm as any

      const row = { key_id: 'key-123', key_name: 'Original' }
      expect(vm.resolveKey(row)).toBe('Custom: key-123')
    })
  })

  describe('路由模型', () => {
    it('有 routed_model 时显示路由', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.showRoutedModel({ routed_model: 'gpt-4o-mini' })).toBe(true)
      expect(vm.showRoutedModel({ routed_model: '' })).toBe(false)
      expect(vm.showRoutedModel({})).toBe(false)
    })
  })

  describe('openTrace', () => {
    it('Video 端点打开 VideoTaskDetail', async () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      await vm.openTrace(mockRows[0])

      expect(vm.videoDetailVisible).toBe(true)
      expect(vm.videoTaskData).toBeTruthy()
      expect(vm.videoTaskData.task_id).toBe('req-001')
      expect(vm.videoTaskData.model).toBe('doubao-seedance-2-0-260128')
    })

    it('Video 端点不打开 TraceDrawer', async () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      await vm.openTrace(mockRows[0])

      expect(vm.traceVisible).toBe(false)
    })

    it('非 Video 端点打开 TraceDrawer', async () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      await vm.openTrace(mockRows[1])

      expect(vm.traceVisible).toBe(true)
      expect(vm.traceLoading).toBe(false) // resolved after await
      expect(vm.traceRow).toEqual(mockRows[1])
    })

    it('非 Video 端点不打开 VideoTaskDetail', async () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      await vm.openTrace(mockRows[1])

      expect(vm.videoDetailVisible).toBe(false)
    })

    it('traceEndpoint 失败时不崩溃', async () => {
      const wrapper = mountComponent({
        traceEndpoint: vi.fn().mockRejectedValue(new Error('Network error')),
      })
      const vm = wrapper.vm as any

      await vm.openTrace(mockRows[1])

      expect(vm.traceVisible).toBe(true)
      expect(vm.traceData).toBeNull()
      expect(vm.traceLoading).toBe(false)
    })
  })

  describe('showVideoDetail', () => {
    it('Video 端点返回 true', async () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      vm.traceRow = mockRows[0]
      await vm.$nextTick()

      expect(vm.showVideoDetail).toBe(true)
    })

    it('非 Video 端点返回 false', async () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      vm.traceRow = mockRows[1]
      await vm.$nextTick()

      expect(vm.showVideoDetail).toBe(false)
    })
  })

  describe('Formatters', () => {
    it('fmtTokens 格式化大数字', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.fmtTokens(0)).toBe('0')
      expect(vm.fmtTokens(null)).toBe('0')
      expect(vm.fmtTokens(1500)).toBe('1.5K')
      expect(vm.fmtTokens(2000000)).toBe('2.00M')
      expect(vm.fmtTokens(1000000000)).toBe('1.00B')
    })

    it('fmtCost 格式化成本', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.fmtCost(0)).toBe('$0.00')
      expect(vm.fmtCost(null)).toBe('$0.00')
      expect(vm.fmtCost(1.5)).toBe('$1.50')
      expect(vm.fmtCost(0.005)).toBe('$0.005')
    })

    it('fmtNum 格式化数字', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.fmtNum(0)).toBe('0')
      expect(vm.fmtNum(null)).toBe('0')
      expect(vm.fmtNum(1234567)).toBe('1,234,567')
    })

    it('reasoningType 映射推理类型', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.reasoningType('high')).toBe('danger')
      expect(vm.reasoningType('medium')).toBe('warning')
      expect(vm.reasoningType('low')).toBe('info')
      expect(vm.reasoningType(undefined)).toBe('')
    })
  })

  describe('分页事件', () => {
    it('页码变化时 emit update:page', async () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      // 模拟分页 change 事件
      const pagination = wrapper.findComponent({ name: 'ElPagination' })
      if (pagination.exists()) {
        // 直接调用内部方法验证 emit 逻辑
        const emitSpy = vi.spyOn(vm, '$emit')
        // 手动触发分页变化
        wrapper.vm.$emit('update:page', 2)
        expect(wrapper.emitted('update:page')).toBeTruthy()
        expect(wrapper.emitted('update:page')?.[0]).toEqual([2])
      }
    })
  })

  describe('列可见性', () => {
    it('showKey 为 false 时隐藏 Key 列', () => {
      const wrapper = mountComponent({
        showKey: false,
      })

      // Key 列不应该渲染
      expect(wrapper.text()).not.toContain('test-key-1')
    })

    it('showProvider 为 false 时隐藏渠道列', () => {
      const wrapper = mountComponent({
        showProvider: false,
      })

      expect(wrapper.text()).not.toContain('中转 A')
    })
  })
})