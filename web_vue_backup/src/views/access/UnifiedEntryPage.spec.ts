import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import UnifiedEntryPage from './UnifiedEntryPage.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

vi.mock('@/services/gateway-api', () => ({
  fetchModels: vi.fn()
}))

import { fetchModels } from '@/services/gateway-api'
const mockedFetch = vi.mocked(fetchModels)

describe('UnifiedEntryPage.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockedFetch.mockResolvedValue([
      { id: 'gpt-4o', object: 'model', created: 0, owned_by: 'openai' },
      { id: 'claude-3-5-sonnet', object: 'model', created: 0, owned_by: 'anthropic' }
    ])
  })

  it('renders the hero section', async () => {
    const wrapper = mount(UnifiedEntryPage, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(wrapper.find('.hero-section').exists()).toBe(true)
    expect(wrapper.find('.hero-title').text()).toBe('LLM Gateway')
    expect(wrapper.find('.hero-badge').text()).toContain('统一接入')
  })

  it('renders the capability section', async () => {
    const wrapper = mount(UnifiedEntryPage, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(wrapper.find('.capability-section').exists()).toBe(true)
    expect(wrapper.text()).toContain('支持能力')
  })

  it('renders the model discovery section', async () => {
    const wrapper = mount(UnifiedEntryPage, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(wrapper.find('.model-discovery-section').exists()).toBe(true)
    expect(wrapper.text()).toContain('可用模型')
  })

  it('shows model cards from gateway API', async () => {
    const wrapper = mount(UnifiedEntryPage, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(mockedFetch).toHaveBeenCalled()
    expect(wrapper.text()).toContain('gpt-4o')
    expect(wrapper.text()).toContain('claude-3-5-sonnet')
  })

  it('shows hero stat values', async () => {
    const wrapper = mount(UnifiedEntryPage, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    const stats = wrapper.findAll('.stat-value')
    expect(stats.length).toBe(3)
    expect(stats[0].text()).toBe('5+')
    expect(stats[1].text()).toBe('50+')
    expect(stats[2].text()).toBe('100%')
  })

  it('shows hero code endpoints', async () => {
    const wrapper = mount(UnifiedEntryPage, {
      global: { stubs: elementStubsKebab }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('/v1/chat/completions')
    expect(wrapper.text()).toContain('/v1/models')
  })
})
