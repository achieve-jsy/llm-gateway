import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SyncStatusCard from '../SyncStatusCard.vue'
import type { HubSyncStatus } from '@/types/org'

const baseStatus: HubSyncStatus = {
  last_sync_at: '2026-11-03T13:50:16+08:00',
  status: 'success',
  scope: 'all',
  error: '',
  departments: 100,
  employees: 547,
  unmapped_departments: 7,
  resigned_accounts_disabled: 127,
  diff: {
    studios: { inserted: 0, updated: 1, deleted: 0 },
    departments: { inserted: 0, updated: 3, deleted: 0 },
    mappings: { inserted: 0, updated: 0, deleted: 0 },
  },
  running: false,
}

const stubs = { 'el-button': { template: '<button :disabled="disabled"><slot /></button>', props: ['disabled', 'loading', 'type', 'size'] } }

function mountCard(props: Record<string, unknown>) {
  return mount(SyncStatusCard, { props, global: { stubs } })
}

describe('SyncStatusCard', () => {
  it('同步中：按钮置灰且文案为「同步中…」', () => {
    const wrapper = mountCard({ status: { ...baseStatus, running: true }, syncing: true })
    const btn = wrapper.get('[data-test="sync-trigger"]')
    expect(btn.text()).toBe('同步中…')
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('空闲：按钮可点，点击发出 trigger（未返回前不会自发第二次）', async () => {
    const wrapper = mountCard({ status: baseStatus, syncing: false })
    const btn = wrapper.get('[data-test="sync-trigger"]')
    expect(btn.text()).toBe('立即同步')
    expect(btn.attributes('disabled')).toBeUndefined()

    await btn.trigger('click')
    expect(wrapper.emitted('trigger')).toHaveLength(1)
  })

  it('成功后展示上次同步时间与各项计数', () => {
    const wrapper = mountCard({ status: baseStatus, syncing: false })
    expect(wrapper.get('[data-test="sync-time"]').text()).toContain('2026-11-03 13:50')
    const text = wrapper.text()
    expect(text).toContain('部门 100')
    expect(text).toContain('人员 547')
    expect(text).toContain('未归属 7')
    expect(text).toContain('离职关停 127')
  })

  it('失败时展示错误原文', () => {
    const wrapper = mountCard({ status: { ...baseStatus, status: 'failed', error: 'ehr unauthorized' }, syncing: false })
    expect(wrapper.get('[data-test="sync-error"]').text()).toBe('ehr unauthorized')
  })

  it('从未同步：显示「尚未同步」且不展示计数行', () => {
    const wrapper = mountCard({ status: null, syncing: false })
    expect(wrapper.get('[data-test="sync-time"]').text()).toContain('尚未同步')
    expect(wrapper.text()).not.toContain('部门 0')
  })
})
