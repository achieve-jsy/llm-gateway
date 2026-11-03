import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelCard from './ModelCard.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

const sampleModel = {
  id: 'gpt-4o',
  object: 'model',
  created: 1716777600,
  owned_by: 'openai',
  type: 'chat' as const
}

function mountCard(model = sampleModel) {
  return mount(ModelCard, {
    props: { model },
    global: { stubs: elementStubsKebab }
  })
}

describe('ModelCard.vue', () => {
  it('renders the model id as the model name', () => {
    const wrapper = mountCard()
    expect(wrapper.find('.model-name').text()).toBe('gpt-4o')
  })

  it('renders owned_by as a tag', () => {
    const wrapper = mountCard()
    expect(wrapper.find('.el-tag-stub').text()).toBe('openai')
  })

  it('renders fallback "unknown" when owned_by is empty', () => {
    const wrapper = mountCard({ ...sampleModel, owned_by: '' })
    expect(wrapper.find('.el-tag-stub').text()).toBe('unknown')
  })

  it('renders the availability indicator', () => {
    const wrapper = mountCard()
    expect(wrapper.find('.meta-item').text()).toContain('可用')
    expect(wrapper.find('.meta-dot').exists()).toBe(true)
  })

  it('toggles is-hover class on mouseenter/mouseleave', async () => {
    const wrapper = mountCard()
    expect(wrapper.find('.model-card').classes()).not.toContain('is-hover')
    await wrapper.find('.model-card').trigger('mouseenter')
    expect(wrapper.find('.model-card').classes()).toContain('is-hover')
    await wrapper.find('.model-card').trigger('mouseleave')
    expect(wrapper.find('.model-card').classes()).not.toContain('is-hover')
  })

  it('renders different model ids correctly', () => {
    const wrapper = mountCard({
      ...sampleModel,
      id: 'claude-3-5-sonnet-20241022',
      owned_by: 'anthropic'
    })
    expect(wrapper.find('.model-name').text()).toBe('claude-3-5-sonnet-20241022')
    expect(wrapper.find('.el-tag-stub').text()).toBe('anthropic')
  })
})
