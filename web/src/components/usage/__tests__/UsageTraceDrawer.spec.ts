import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import ElementPlus from 'element-plus'
import UsageTraceDrawer from '../UsageTraceDrawer.vue'

const authStore = reactive({ isAdmin: true, effectiveRole: 'admin' })

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/settings', () => ({
  useSettingsStore: () => ({ publicSettings: { pricing_usd_to_cny_rate: 7.2 } }),
}))

describe('UsageTraceDrawer 提示词可见性', () => {
  const row = {
    request_id: 'req-test-001',
    model_name: 'gpt-test',
    inbound_endpoint: '/v1/chat/completions',
  }
  const traceData = {
    request_body: { messages: [{ role: 'user', content: '机密提示词内容' }] },
    response_body: { choices: [{ message: { content: 'ok' } }] },
  }

  async function mountDrawer() {
    const wrapper = mount(UsageTraceDrawer, {
      props: {
        modelValue: true,
        row: row as never,
        traceData: traceData as never,
        traceLoading: false,
      },
      global: { plugins: [ElementPlus] },
    })
    // el-drawer 的内容在首次渲染后一个 tick 才挂载
    await flushPromises()
    await nextTick()
    return wrapper
  }

  function tabLabels(wrapper: ReturnType<typeof mount>) {
    return wrapper.findAll('.el-tabs__item').map(item => item.text())
  }

  beforeEach(() => {
    authStore.isAdmin = true
    authStore.effectiveRole = 'admin'
  })

  it('admin 可见「提示词」与「原始请求」Tab', async () => {
    const labels = tabLabels(await mountDrawer())
    expect(labels.some(l => l.includes('提示词'))).toBe(true)
    expect(labels.some(l => l.includes('原始请求'))).toBe(true)
  })

  it('非 admin 隐藏「提示词」与「原始请求」Tab，且默认落在调用信息', async () => {
    authStore.isAdmin = false
    authStore.effectiveRole = 'dept_admin'

    const wrapper = await mountDrawer()
    const labels = tabLabels(wrapper)

    expect(labels.some(l => l.includes('提示词'))).toBe(false)
    expect(labels.some(l => l.includes('原始请求'))).toBe(false)
    // 记录本身正常展示：调用信息 / 响应仍在
    expect(labels.some(l => l.includes('调用信息'))).toBe(true)
    expect(labels.some(l => l.includes('响应'))).toBe(true)
    // 默认选中项不能是被隐藏的 Tab（否则抽屉空白）
    expect(wrapper.find('.el-tabs__item.is-active').text()).toContain('调用信息')
  })
})
