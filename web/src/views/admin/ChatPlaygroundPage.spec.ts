import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import ChatPlaygroundPage from './ChatPlaygroundPage.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

// ─── Mocks ────────────────────────────────────────────────────────────────────

vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn()
  }
}))

vi.mock('@/services/control-plane', () => ({
  getModels: vi.fn()
}))

vi.mock('@/services/http', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn()
  }
}))

import { ElMessage } from 'element-plus'
import { getModels } from '@/services/control-plane'
import http from '@/services/http'

const mockedGetModels = vi.mocked(getModels)
const mockedHttpGet = vi.mocked(http.get)

// ─── Stubs ────────────────────────────────────────────────────────────────────

const ElSlider = defineComponent({
  name: 'ElSliderStub',
  props: { modelValue: Number, min: Number, max: Number, step: Number },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('input', {
      class: 'el-slider-stub',
      type: 'range',
      value: props.modelValue,
      onInput: (e: any) => emit('update:modelValue', Number(e.target.value))
    })
  }
})

const ElCollapse = defineComponent({
  name: 'ElCollapseStub',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-collapse-stub' }, slots.default?.())
  }
})

const ElCollapseItem = defineComponent({
  name: 'ElCollapseItemStub',
  props: { title: String },
  setup(_, { slots }) {
    return () => h('div', { class: 'el-collapse-item-stub' }, slots.default?.())
  }
})

const stubs = {
  ...elementStubsKebab,
  'el-slider': ElSlider,
  'el-collapse': ElCollapse,
  'el-collapse-item': ElCollapseItem,
  'el-avatar': defineComponent({ setup() { return () => h('span') } }),
  'el-dropdown': defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } }),
  'el-dropdown-menu': defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } }),
  'el-dropdown-item': defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } }),
  'el-result': defineComponent({ props: ['icon', 'title'], setup(p) { return () => h('div', p.title) } }),
}

// ─── Sample data ──────────────────────────────────────────────────────────────

const sampleModels = [
  {
    id: 'uuid-1',
    name: 'qwen3-next-80b',
    provider_id: 'p1',
    model_type: 'chat',
    enabled: true,
    upstream_model_id: 'qwen3-next-80b',
    description: '',
    context_length: 131072,
    max_output_tokens: 8192,
    input_price: 0.01,
    output_price: 0.03,
    weight: 1,
    consecutive_failures: 0,
    last_check_error: '',
    response_time: 0,
    created_at: '2025-01-01',
    updated_at: '2025-01-01',
    provider: { id: 'p1', name: '百炼', definition_id: 'dashscope', enabled: true },
  },
  {
    id: 'uuid-2',
    name: 'deepseek-v3',
    provider_id: 'p2',
    model_type: 'chat',
    enabled: true,
    upstream_model_id: 'deepseek-v3',
    description: '',
    context_length: 64000,
    max_output_tokens: 4096,
    input_price: 0.01,
    output_price: 0.03,
    weight: 1,
    consecutive_failures: 0,
    last_check_error: '',
    response_time: 0,
    created_at: '2025-01-02',
    updated_at: '2025-01-02',
    provider: { id: 'p2', name: 'DeepSeek', definition_id: 'deepseek', enabled: true },
  },
]

// ─── Helper ───────────────────────────────────────────────────────────────────

function createWrapper() {
  return mount(ChatPlaygroundPage, {
    global: {
      stubs
    }
  })
}

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('ChatPlaygroundPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedGetModels.mockResolvedValue({ data: sampleModels as any, total: 2 })
    global.fetch = vi.fn()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('模型加载（从管理模型列表）', () => {
    it('挂载时从 control-plane 加载模型列表', async () => {
      createWrapper()
      await flushPromises()
      expect(mockedGetModels).toHaveBeenCalledTimes(1)
    })

    it('默认选中第一个模型', async () => {
      const wrapper = createWrapper()
      await flushPromises()
      const vm = wrapper.vm as any
      expect(vm.config.model).toBe('qwen3-next-80b')
    })

    it('列表为空时 config.model 保持空', async () => {
      mockedGetModels.mockResolvedValue({ data: [], total: 0 })
      const wrapper = createWrapper()
      await flushPromises()
      expect((wrapper.vm as any).config.model).toBe('')
    })

    it('模型加载失败不影响页面渲染', async () => {
      mockedGetModels.mockRejectedValue(new Error('network error'))
      const wrapper = createWrapper()
      await flushPromises()
      expect(wrapper.find('.pg').exists()).toBe(true)
    })

    it('模型选项显示名称和渠道名', async () => {
      const wrapper = createWrapper()
      await flushPromises()
      const vm = wrapper.vm as any
      // 模型列表包含两个带 provider 的条目
      expect(vm.models).toHaveLength(2)
      expect(vm.models[0].name).toBe('qwen3-next-80b')
      expect(vm.models[0].provider?.name).toBe('百炼')
    })
  })

  describe('上下文窗口提示（来自模型列表）', () => {
    it('选中模型后上下文提示来自模型列表数据（无需额外 API 调用）', async () => {
      const wrapper = createWrapper()
      await flushPromises()
      await flushPromises()
      // 不应该再单独调 /api/v1/models 查元数据
      expect(mockedHttpGet).not.toHaveBeenCalled()
      // 上下文提示应显示
      const hint = wrapper.find('.field__hint')
      expect(hint.exists()).toBe(true)
      expect(hint.text()).toContain('128K')
    })
  })

  describe('参数配置', () => {
    it('页面包含温度、Top P、最大令牌数、停止序列字段', async () => {
      const wrapper = createWrapper()
      await flushPromises()
      const html = wrapper.html()
      expect(html).toContain('温度')
      expect(html).toContain('Top P')
      expect(html).toContain('最大令牌数')
      expect(html).toContain('停止序列')
    })

    it('页面包含系统提示词字段', async () => {
      const wrapper = createWrapper()
      await flushPromises()
      expect(wrapper.html()).toContain('系统提示词')
    })

    it('高级设置默认折叠', async () => {
      const wrapper = createWrapper()
      await flushPromises()
      expect(wrapper.html()).toContain('高级设置')
      expect(wrapper.html()).not.toContain('接口地址')
    })
  })

  describe('消息发送', () => {
    it('输入为空时不发送', async () => {
      const wrapper = createWrapper()
      await flushPromises()
      const sendBtn = wrapper.find('.input-box__send')
      await sendBtn.trigger('click')
      expect(global.fetch).not.toHaveBeenCalled()
    })

    it('未选择模型时显示警告', async () => {
      mockedGetModels.mockResolvedValue({ data: [], total: 0 })
      const wrapper = createWrapper()
      await flushPromises()
      ;(wrapper.vm as any).config.model = ''
      ;(wrapper.vm as any).userInput = 'hello'
      await (wrapper.vm as any).sendMessage()
      expect(ElMessage.warning).toHaveBeenCalledWith('请先选择或输入模型')
    })

    it('发送时使用 upstream_model_id 作为模型名', async () => {
      const mockResponse = {
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn()
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: {"choices":[{"delta":{"content":"Hi"}}]}\n\n') })
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: [DONE]\n\n') })
              .mockResolvedValueOnce({ done: true, value: undefined })
          })
        }
      }
      ;(global.fetch as any).mockResolvedValue(mockResponse)

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'deepseek-v3'
      vm.userInput = '你好'
      await vm.sendMessage()
      await flushPromises()

      const [url, opts] = (global.fetch as any).mock.calls[0]
      const body = JSON.parse(opts.body)
      expect(body.model).toBe('deepseek-v3')
    })

    it('发送时构建正确的请求体（包含 top_p 和 stop）', async () => {
      const mockResponse = {
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn()
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: {"choices":[{"delta":{"content":"Hi"}}]}\n\n') })
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: [DONE]\n\n') })
              .mockResolvedValueOnce({ done: true, value: undefined })
          })
        }
      }
      ;(global.fetch as any).mockResolvedValue(mockResponse)

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'qwen3-next-80b'
      vm.config.temperature = 0.5
      vm.config.topP = 0.9
      vm.config.maxTokens = 4096
      vm.config.stop = 'END,STOP'
      vm.config.systemPrompt = '你是一个助手'
      vm.userInput = '你好'

      await vm.sendMessage()
      await flushPromises()

      expect(global.fetch).toHaveBeenCalledTimes(1)
      const [url, opts] = (global.fetch as any).mock.calls[0]
      expect(url).toBe('/v1/chat/completions')

      const body = JSON.parse(opts.body)
      expect(body.model).toBe('qwen3-next-80b')
      expect(body.temperature).toBe(0.5)
      expect(body.top_p).toBe(0.9)
      expect(body.max_tokens).toBe(4096)
      expect(body.stop).toEqual(['END', 'STOP'])
      expect(body.stream).toBe(true)
      expect(body.messages).toHaveLength(2)
      expect(body.messages[0].role).toBe('system')
      expect(body.messages[0].content).toBe('你是一个助手')
      expect(body.messages[1].role).toBe('user')
      expect(body.messages[1].content).toBe('你好')
    })

    it('不填停止序列时请求体中不包含 stop 字段', async () => {
      const mockResponse = {
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn()
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: [DONE]\n\n') })
              .mockResolvedValueOnce({ done: true, value: undefined })
          })
        }
      }
      ;(global.fetch as any).mockResolvedValue(mockResponse)

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'qwen3-next-80b'
      vm.config.stop = ''
      vm.userInput = 'test'

      await vm.sendMessage()
      await flushPromises()

      const body = JSON.parse((global.fetch as any).mock.calls[0][1].body)
      expect(body.stop).toBeUndefined()
    })
  })

  describe('流式响应', () => {
    it('正确解析流式 SSE 数据并显示助手回复', async () => {
      const chunks = [
        'data: {"choices":[{"delta":{"content":"你"}}]}\n\n',
        'data: {"choices":[{"delta":{"content":"好"}}]}\n\n',
        'data: [DONE]\n\n',
      ]
      const encoder = new TextEncoder()
      let callIndex = 0
      const mockReader = {
        read: vi.fn(() => {
          if (callIndex < chunks.length) {
            return Promise.resolve({ done: false, value: encoder.encode(chunks[callIndex++]) })
          }
          return Promise.resolve({ done: true, value: undefined })
        })
      }
      ;(global.fetch as any).mockResolvedValue({
        ok: true,
        body: { getReader: () => mockReader }
      })

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'qwen3-next-80b'
      vm.userInput = 'hello'

      await vm.sendMessage()
      await flushPromises()

      expect(vm.messages).toHaveLength(2)
      expect(vm.messages[1].role).toBe('assistant')
      expect(vm.messages[1].content).toBe('你好')
    })

    it('解析 reasoning_content 字段', async () => {
      const chunks = [
        'data: {"choices":[{"delta":{"reasoning_content":"让我想想"}}]}\n\n',
        'data: {"choices":[{"delta":{"content":"答案是42"}}]}\n\n',
        'data: [DONE]\n\n',
      ]
      const encoder = new TextEncoder()
      let callIndex = 0
      ;(global.fetch as any).mockResolvedValue({
        ok: true,
        body: {
          getReader: () => ({
            read: () => {
              if (callIndex < chunks.length) {
                return Promise.resolve({ done: false, value: encoder.encode(chunks[callIndex++]) })
              }
              return Promise.resolve({ done: true, value: undefined })
            }
          })
        }
      })

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'deepseek-v3'
      vm.userInput = '问题'

      await vm.sendMessage()
      await flushPromises()

      expect(vm.messages[1].reasoning_content).toBe('让我想想')
      expect(vm.messages[1].content).toBe('答案是42')
    })
  })

  describe('错误处理', () => {
    it('HTTP 错误时显示错误消息', async () => {
      ;(global.fetch as any).mockResolvedValue({
        ok: false,
        status: 429,
        json: () => Promise.resolve({ error: { message: '请求过于频繁' } })
      })

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'qwen3-next-80b'
      vm.userInput = 'test'

      await vm.sendMessage()
      await flushPromises()

      expect(vm.messages[1].content).toContain('请求过于频繁')
      expect(ElMessage.error).toHaveBeenCalled()
    })
  })

  describe('清空对话', () => {
    it('点击清空后消息列表为空', async () => {
      ;(global.fetch as any).mockResolvedValue({
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn()
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: [DONE]\n\n') })
              .mockResolvedValueOnce({ done: true, value: undefined })
          })
        }
      })

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'qwen3-next-80b'
      vm.userInput = 'hi'
      await vm.sendMessage()
      await flushPromises()

      expect(vm.messages.length).toBeGreaterThan(0)
      vm.clearChat()
      expect(vm.messages).toHaveLength(0)
    })
  })

  describe('自定义接口地址', () => {
    it('使用自定义 API Base 时请求正确的 URL', async () => {
      ;(global.fetch as any).mockResolvedValue({
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn()
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: [DONE]\n\n') })
              .mockResolvedValueOnce({ done: true, value: undefined })
          })
        }
      })

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'gpt-4'
      vm.config.apiBase = 'https://custom.api.com/v1'
      vm.userInput = 'test'

      await vm.sendMessage()
      await flushPromises()

      const [url] = (global.fetch as any).mock.calls[0]
      expect(url).toBe('https://custom.api.com/v1/chat/completions')
    })

    it('使用自定义 API Key 时请求带正确的 Authorization 头', async () => {
      ;(global.fetch as any).mockResolvedValue({
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn()
              .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode('data: [DONE]\n\n') })
              .mockResolvedValueOnce({ done: true, value: undefined })
          })
        }
      })

      const wrapper = createWrapper()
      await flushPromises()

      const vm = wrapper.vm as any
      vm.config.model = 'gpt-4'
      vm.config.apiKey = 'sk-custom-key-123'
      vm.userInput = 'test'

      await vm.sendMessage()
      await flushPromises()

      const [, opts] = (global.fetch as any).mock.calls[0]
      expect(opts.headers['Authorization']).toBe('Bearer sk-custom-key-123')
    })
  })
})