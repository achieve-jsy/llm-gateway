import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import DataFreshnessNotice from '../DataFreshnessNotice.vue'

function mountNotice(props: Record<string, unknown>) {
  return mount(DataFreshnessNotice, {
    props,
    global: { plugins: [ElementPlus] },
  })
}

describe('DataFreshnessNotice', () => {
  it('只读场景不提示写操作暂停', () => {
    const wrapper = mountNotice({ stale: true, updatedAt: '2026-07-14T09:53:55Z', mode: 'readonly' })

    expect(wrapper.text()).toContain('最新调用记录可能尚未汇总')
    expect(wrapper.text()).not.toContain('写操作已暂停')
  })

  it('写操作场景保留冻结提示', () => {
    const wrapper = mountNotice({ stale: true, updatedAt: '2026-07-14T09:53:55Z' })

    expect(wrapper.text()).toContain('涉及额度的写操作已暂停')
  })
})
