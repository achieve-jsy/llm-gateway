import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import ElementPlus from 'element-plus'
import UsageTraceDrawer from '../UsageTraceDrawer.vue'

const MY_ID = 'user-me'
const authStore = reactive({ isAdmin: false, effectiveRole: 'dept_admin', user: { id: MY_ID } })

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/settings', () => ({
  useSettingsStore: () => ({ publicSettings: { pricing_usd_to_cny_rate: 7.2 } }),
}))

// 提示词口径：用户只能看自己的提示词，别人的看不了；admin 例外，可看全部。
describe('UsageTraceDrawer 提示词可见性', () => {
  function makeRow(userId: string, keyType: 'personal' | 'service' = 'personal') {
    return {
      request_id: 'req-test-001',
      model_name: 'gpt-test',
      inbound_endpoint: '/v1/chat/completions',
      user_id: userId,
      key_type: keyType,
    }
  }
  const traceData = {
    request_body: { messages: [{ role: 'user', content: '机密提示词内容' }] },
    response_body: { choices: [{ message: { content: 'ok' } }] },
  }

  async function mountDrawer(userId: string, keyType: 'personal' | 'service' = 'personal') {
    const wrapper = mount(UsageTraceDrawer, {
      props: {
        modelValue: true,
        row: makeRow(userId, keyType) as never,
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
    authStore.isAdmin = false
    authStore.effectiveRole = 'dept_admin'
    authStore.user = { id: MY_ID }
  })

  it('admin 可看别人的提示词', async () => {
    authStore.isAdmin = true
    authStore.effectiveRole = 'admin'

    const labels = tabLabels(await mountDrawer('user-other'))
    expect(labels.some(l => l.includes('提示词'))).toBe(true)
    expect(labels.some(l => l.includes('原始请求'))).toBe(true)
  })

  it('普通用户可看自己的提示词', async () => {
    const wrapper = await mountDrawer(MY_ID)
    const labels = tabLabels(wrapper)

    expect(labels.some(l => l.includes('提示词'))).toBe(true)
    expect(labels.some(l => l.includes('原始请求'))).toBe(true)
  })

  it('程序（服务）Key 的调用：本人也看不到提示词', async () => {
    const wrapper = await mountDrawer(MY_ID, 'service')
    const labels = tabLabels(wrapper)

    expect(labels.some(l => l.includes('提示词'))).toBe(false)
    expect(labels.some(l => l.includes('原始请求'))).toBe(false)
    expect(wrapper.find('.el-tabs__item.is-active').text()).toContain('调用信息')
  })

  it('程序（服务）Key 的调用：admin 仍可看提示词', async () => {
    authStore.isAdmin = true
    authStore.effectiveRole = 'admin'

    const labels = tabLabels(await mountDrawer('user-other', 'service'))
    expect(labels.some(l => l.includes('提示词'))).toBe(true)
    expect(labels.some(l => l.includes('原始请求'))).toBe(true)
  })

  it('普通用户看不到别人的提示词，且默认落在调用信息', async () => {
    const wrapper = await mountDrawer('user-other')
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
