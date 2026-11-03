import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ModelDiscoverySection from './ModelDiscoverySection.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

vi.mock('@/services/gateway-api', () => ({
  fetchModels: vi.fn()
}))

import { fetchModels } from '@/services/gateway-api'
const mockedFetch = vi.mocked(fetchModels)

const sampleModels = [
  { id: 'gpt-4o', object: 'model', created: 0, owned_by: 'openai' },
  { id: 'claude-3-5-sonnet', object: 'model', created: 0, owned_by: 'anthropic' },
  { id: 'qwen-turbo', object: 'model', created: 0, owned_by: 'bailian' }
]

describe('ModelDiscoverySection.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows loading skeleton while fetching', async () => {
    let resolve: (v: any) => void = () => {}
    mockedFetch.mockReturnValueOnce(new Promise((r) => { resolve = r }) as any)
    const wrapper = mount(ModelDiscoverySection, {
      global: { stubs: elementStubsKebab }
    })
    await wrapper.vm.$nextTick()
    expect(wrapper.findAll('.skeleton-card').length).toBeGreaterThan(0)
    resolve(sampleModels)
    await flushPromises()
  })

  it('renders model cards for each fetched model', async () => {
    mockedFetch.mockResolvedValueOnce(sampleModels)
    const wrapper = mount(ModelDiscoverySection, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    const cards = wrapper.findAllComponents({ name: 'ModelCard' })
    expect(cards.length).toBe(3)
  })

  it('shows empty state when no models exist', async () => {
    mockedFetch.mockResolvedValueOnce([])
    const wrapper = mount(ModelDiscoverySection, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(wrapper.find('.empty-state').exists()).toBe(true)
    expect(wrapper.text()).toContain('暂无可用模型')
  })

  it('shows error UI when fetchModels rejects', async () => {
    mockedFetch.mockRejectedValueOnce(new Error('Network down'))
    const wrapper = mount(ModelDiscoverySection, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(wrapper.find('.error-box').exists()).toBe(true)
    expect(wrapper.text()).toContain('Network down')
  })

  it('uses default error message when error has no message', async () => {
    mockedFetch.mockRejectedValueOnce({} as any)
    const wrapper = mount(ModelDiscoverySection, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('获取模型列表失败')
  })

  it('reloads models when refresh button is clicked', async () => {
    mockedFetch.mockResolvedValueOnce(sampleModels)
    const wrapper = mount(ModelDiscoverySection, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(mockedFetch).toHaveBeenCalledTimes(1)

    mockedFetch.mockResolvedValueOnce(sampleModels)
    const refresh = wrapper.find('.refresh-btn')
    expect(refresh.exists()).toBe(true)
    await refresh.trigger('click')
    await flushPromises()
    expect(mockedFetch).toHaveBeenCalledTimes(2)
  })

  it('retries when error retry button is clicked', async () => {
    mockedFetch.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mount(ModelDiscoverySection, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.error-box').exists()).toBe(true)

    mockedFetch.mockResolvedValueOnce(sampleModels)
    const retry = wrapper.find('.error-box').find('.el-button-stub')
    expect(retry.exists()).toBe(true)
    await retry.trigger('click')
    await flushPromises()
    await wrapper.vm.$nextTick()
    expect(mockedFetch).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.error-box').exists()).toBe(false)
  })
})
