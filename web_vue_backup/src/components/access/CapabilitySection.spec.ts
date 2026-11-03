import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import CapabilitySection from './CapabilitySection.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'
import config from '@/assets/capability.json'

function mountSection() {
  return mount(CapabilitySection, {
    global: { stubs: elementStubsKebab }
  })
}

describe('CapabilitySection.vue', () => {
  it('renders one capability card per config entry', () => {
    const wrapper = mountSection()
    const cards = wrapper.findAll('.capability-card')
    expect(cards.length).toBe(config.capabilities.length)
  })

  it('renders capability titles and descriptions', () => {
    const wrapper = mountSection()
    config.capabilities.forEach((cap) => {
      expect(wrapper.text()).toContain(cap.title)
      expect(wrapper.text()).toContain(cap.description)
    })
  })

  it('renders one exclude item per config entry', () => {
    const wrapper = mountSection()
    const items = wrapper.findAll('.exclude-item')
    expect(items.length).toBe(config.excludes.length)
  })

  it('renders exclude reasons', () => {
    const wrapper = mountSection()
    config.excludes.forEach((ex) => {
      expect(wrapper.text()).toContain(ex.reason)
    })
  })

  it('renders supported and unsupported section titles', () => {
    const wrapper = mountSection()
    expect(wrapper.text()).toContain('支持能力')
    expect(wrapper.text()).toContain('暂不支持')
  })

  it('renders capability icon component slot in each card', () => {
    const wrapper = mountSection()
    const icons = wrapper.findAll('.card-icon')
    expect(icons.length).toBe(config.capabilities.length)
  })
})
