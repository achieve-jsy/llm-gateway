import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus, { ElMessage } from 'element-plus'
import ChatPlaygroundPage from '../ChatPlaygroundPage.vue'

// ─── mock 上游依赖 ──────────────────────────────────────────────────────
const mockListGroups = vi.fn()
const mockListGroupModels = vi.fn()
const mockGetModels = vi.fn()
const mockListKeys = vi.fn()
const mockRevealKey = vi.fn()

vi.mock('@/api/groups', () => ({
  groupsApi: {
    listGroups: (...args: unknown[]) => mockListGroups(...args),
    listGroupModels: (...args: unknown[]) => mockListGroupModels(...args),
  },
}))

vi.mock('@/services/control-plane', () => ({
  getModels: (...args: unknown[]) => mockGetModels(...args),
  listKeys: (...args: unknown[]) => mockListKeys(...args),
  revealKey: (...args: unknown[]) => mockRevealKey(...args),
}))

// ─── 数据工厂 ───────────────────────────────────────────────────────────
function makeChatModel(overrides: Record<string, unknown> = {}) {
  return {
    id: 'm-1',
    name: 'model-1',
    upstream_model_id: 'upstream-1',
    model_type: 'chat',
    enabled: true,
    provider: { name: 'openai' },
    ...overrides,
  }
}

function makeGroup(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    name: '测试组',
    description: '',
    strategy: 'round_robin',
    model_type: 'chat',
    enabled: true,
    max_switches: 3,
    same_model_retries: 1,
    failover_cooldown_seconds: 10,
    timeout_seconds: 60,
    open_mode: 'controlled',
    is_capability_override: false,
    override_supports_vision: false,
    override_supports_function_calling: false,
    override_supports_tool_choice: false,
    override_mode: '',
    override_context_length: null,
    override_max_output_tokens: null,
    sticky_enabled: null,
    sticky_ttl_seconds: null,
    created_at: '',
    updated_at: '',
    strategy_label: '轮询',
    ...overrides,
  }
}

function makeGroupModel(overrides: Record<string, unknown> = {}) {
  return {
    id: 11,
    group_id: 1,
    model_id: 'g1-model-1',
    model_name: '组内模型A',
    provider_name: 'openai',
    priority: 1,
    weight: null,
    ...overrides,
  }
}

function makeRawKey(overrides: Record<string, unknown> = {}) {
  return {
    id: 'k1',
    name: '我的Key',
    masked_key: 'sk-****1234',
    key_hint: 'test key',
    enabled: true,
    log_enabled: true,
    access_all_groups: false,
    created_at: '',
    updated_at: '',
    ...overrides,
  }
}

async function mountPage() {
  const wrapper = mount(ChatPlaygroundPage, {
    global: { plugins: [ElementPlus] },
  })
  await flushPromises()
  return wrapper
}

describe('ChatPlaygroundPage：模型 / 模型组 / API Key 三种调用方式', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.spyOn(ElMessage, 'error').mockImplementation(() => undefined as never)
    vi.spyOn(ElMessage, 'warning').mockImplementation(() => undefined as never)
    mockListGroups.mockResolvedValue({ data: { data: [makeGroup()] } })
    mockListGroupModels.mockResolvedValue({
      data: {
        data: [
          makeGroupModel(),
          makeGroupModel({ id: 12, model_id: 'g1-model-2', model_name: '组内模型B' }),
        ],
      },
    })
    mockGetModels.mockResolvedValue({
      data: [
        makeChatModel(),
        makeChatModel({ id: 'm-2', name: 'model-2', upstream_model_id: 'upstream-2' }),
      ],
    })
    mockListKeys.mockResolvedValue({
      code: 0,
      message: 'ok',
      data: { items: [makeRawKey()], total: 1, page: 1, page_size: 100 },
    })
    mockRevealKey.mockResolvedValue({ key: 'sk-real-123' })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ data: [] }),
      }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  describe('模型模式', () => {
    it('默认模型模式，展示全部已注册 chat 模型', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      expect(vm.mode).toBe('model')
      expect(vm.modelOptions).toHaveLength(2)
      expect(vm.modelOptions[0].name).toBe('model-1')
      expect(vm.modelOptions[1].name).toBe('model-2')
    })
  })

  describe('模型组模式', () => {
    it('选组后 config.model 设为组名（走组调度）', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      vm.mode = 'group'
      await nextTick()
      await flushPromises()

      vm.selectedGroupId = 1
      await vm.onGroupChange()
      await flushPromises()

      expect(vm.config.model).toBe('测试组')
      expect(mockListGroupModels).toHaveBeenCalledWith(1)
    })

    it('切换模式时清空之前已选模型', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      vm.config.model = 'upstream-1'
      vm.mode = 'group'
      await nextTick()
      await flushPromises()

      expect(vm.config.model).toBe('')
      expect(vm.selectedGroupId).toBe('')
    })

    it('同一模型组不重复请求成员', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      vm.mode = 'group'
      await nextTick()
      await flushPromises()

      vm.selectedGroupId = 1
      await vm.onGroupChange()
      await flushPromises()
      await vm.onGroupChange()
      await flushPromises()

      expect(mockListGroupModels).toHaveBeenCalledTimes(1)
    })
  })

  describe('API Key 模式 · 我的 Key 选择', () => {
    it('选择我的 Key 后 reveal 回填 config.apiKey 并清空模型', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      vm.mode = 'apikey'
      await nextTick()
      await flushPromises()

      await vm.onKeySelect('k1')
      await flushPromises()

      expect(mockRevealKey).toHaveBeenCalledWith('k1')
      expect(vm.config.apiKey).toBe('sk-real-123')
      expect(vm.selectedKeyId).toBe('k1')
      expect(vm.config.model).toBe('')
    })

    it('不存在的 Key 不触发 reveal', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      await vm.onKeySelect('nonexistent')
      await flushPromises()

      expect(mockRevealKey).not.toHaveBeenCalled()
    })

    it('手动输入 API Key 清空 model 和 selectedKeyId', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      vm.mode = 'apikey'
      await nextTick()
      await flushPromises()

      vm.config.apiKey = 'sk-manual'
      vm.config.model = 'some-model'
      vm.selectedKeyId = 'k1'
      await vm.onManualApiKeyChange()
      await flushPromises()

      expect(vm.config.model).toBe('')
      expect(vm.selectedKeyId).toBe('')
    })
  })

  describe('模式切换', () => {
    it('切换模式时清空 selectedGroupId 和 selectedKeyId', async () => {
      const wrapper = await mountPage()
      const vm = wrapper.vm as any

      vm.selectedGroupId = 1
      vm.selectedKeyId = 'k1'
      vm.mode = 'apikey'
      await nextTick()
      await flushPromises()

      expect(vm.selectedGroupId).toBe('')
      expect(vm.selectedKeyId).toBe('')
    })
  })
})
