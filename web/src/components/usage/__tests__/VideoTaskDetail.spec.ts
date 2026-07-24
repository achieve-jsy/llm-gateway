import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import ElementPlus from 'element-plus'
import VideoTaskDetail from '../VideoTaskDetail.vue'

const MY_ID = 'user-me'
const authStore = reactive({ isAdmin: true, effectiveRole: 'admin', user: { id: MY_ID } })

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

// 详情接口返回的任务归属：信息页的身份判定来自接口数据（打开时会覆盖 props）。
const apiState = vi.hoisted(() => ({ ownerUserID: 'user-me' }))

vi.mock('@/api/videoTasks', () => ({
  videoTasksAPI: {
    get: vi.fn().mockImplementation(() => Promise.resolve({
      task_id: 'cgt-test-001',
      model: 'seedance-video-test',
      provider_name: '中转 A',
      provider_type: 'seedance-relay',
      status: 'succeeded',
      created_at: '2026-07-08T14:30:00Z',
      updated_at: '2026-07-08T14:31:00Z',
      api_key_name: 'demo-key',
      user_name: '张三',
      user_id: apiState.ownerUserID,
      video_url: 'https://example.com/video.mp4',
      request_body: JSON.stringify({
        model: 'doubao-seedance-2-0-260128',
        content: [{ type: 'text', text: '一只橘猫在阳光下缓慢走过' }],
        resolution: '720p',
        ratio: '16:9',
        duration: 5,
      }),
      response_body: '{"status":"succeeded","content":{"video_url":"https://example.com/video.mp4"},"resolution":"720p","ratio":"16:9","duration":5}',
      events: [
        { event_type: 'submitted', event_data: '任务已提交', created_at: '2026-07-08T14:30:00Z' },
        { event_type: 'succeeded', event_data: '任务已成功', created_at: '2026-07-08T14:31:00Z' },
      ],
    })),
  },
}))

describe('VideoTaskDetail', () => {
  const task = {
    task_id: 'cgt-test-001',
    model: 'seedance-video-test',
    provider_name: '中转 A',
    provider_type: 'seedance-relay',
    status: 'succeeded',
    created_at: '2026-07-08T14:30:00Z',
    updated_at: '2026-07-08T14:31:00Z',
    api_key_name: 'demo-key',
    user_id: MY_ID,
    request_body: JSON.stringify({
      model: 'doubao-seedance-2-0-260128',
      content: [{ type: 'text', text: '一只橘猫在阳光下缓慢走过' }],
      resolution: '720p',
      ratio: '16:9',
      duration: 5,
    }),
  }

  beforeEach(() => {
    vi.clearAllMocks()
    authStore.isAdmin = true
    authStore.effectiveRole = 'admin'
    authStore.user = { id: MY_ID }
    apiState.ownerUserID = MY_ID
  })

  async function mountAs(t: typeof task) {
    apiState.ownerUserID = t.user_id
    const wrapper = mount(VideoTaskDetail, {
      props: {
        modelValue: true,
        task: t,
      },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()
    await nextTick()
    return wrapper
  }

  it('以五个标签页展示任务详情、视频结果、请求响应和时间线', async () => {
    const wrapper = mount(VideoTaskDetail, {
      props: {
        modelValue: true,
        task,
      },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()
    await nextTick()

    const text = wrapper.text()
    expect(text).toContain('cgt-test-001')
    expect(text).toContain('seedance-video-test')
    expect(text).toContain('doubao-seedance-2-0-260128')
    expect(text).toContain('中转 A')
    expect(text).toContain('succeeded')
    expect(text).toContain('任务信息')
    expect(text).toContain('生成结果')
    expect(text).toContain('请求')
    expect(text).toContain('响应')
    expect(text).toContain('事件时间线')
    expect(wrapper.find('.el-tabs').exists()).toBe(true)
    // 提示词在详情任务信息中展示，不依赖「请求」Tab
    expect(text).toContain('一只橘猫在阳光下缓慢走过')
    expect(text).toContain('720p')
    expect(text).toContain('16:9')
    expect(text).toContain('5s')
    expect(text).toContain('demo-key')


    await clickTab(wrapper, '生成结果')
    expect(wrapper.find('video').attributes('src')).toBe('https://example.com/video.mp4')
    expect(wrapper.find('a[download]').attributes('href')).toBe('https://example.com/video.mp4')

    await clickTab(wrapper, '请求')
    expect(wrapper.text()).toContain('一只橘猫')

    await clickTab(wrapper, '响应')
    expect(wrapper.text()).toContain('content')

    await clickTab(wrapper, '事件时间线')
    expect(wrapper.text()).toContain('任务已成功')
    // 时间线时间戳应为北京时间，而非原始 ISO
    expect(wrapper.text()).toContain('2026-07-08 22:30:00')
    expect(wrapper.text()).not.toContain('2026-07-08T14:30:00Z')
  })

  it('任务信息页创建/更新时间使用北京时间格式', async () => {    const wrapper = mount(VideoTaskDetail, {
      props: {
        modelValue: true,
        task,
      },
      global: { plugins: [ElementPlus] },
    })
    await flushPromises()
    await nextTick()

    expect(wrapper.text()).toContain('2026-07-08 22:30:00')
    expect(wrapper.text()).toContain('2026-07-08 22:31:00')
    expect(wrapper.text()).not.toContain('2026-07-08T14:30:00Z')
  })

  it('非 admin 看自己的任务：提示词与请求体可见', async () => {
    authStore.isAdmin = false
    authStore.effectiveRole = 'dept_admin'

    const wrapper = await mountAs(task)
    const text = wrapper.text()

    expect(text).toContain('提示词')
    expect(text).toContain('一只橘猫在阳光下缓慢走过')
    expect(wrapper.findAll('.el-tabs__item').some(item => item.text().includes('请求'))).toBe(true)
  })

  it('非 admin 看别人的任务：提示词与请求体隐藏，任务本身仍可见', async () => {
    authStore.isAdmin = false
    authStore.effectiveRole = 'dept_admin'

    const wrapper = await mountAs({ ...task, user_id: 'user-other' })
    const text = wrapper.text()

    expect(text).not.toContain('提示词')
    expect(text).not.toContain('一只橘猫在阳光下缓慢走过')
    // 请求体 Tab 内含提示词正文，一并隐藏
    expect(wrapper.findAll('.el-tabs__item').some(item => item.text().includes('请求'))).toBe(false)
    // 任务记录本身正常展示
    expect(text).toContain('cgt-test-001')
    expect(text).toContain('720p')
    expect(text).toContain('16:9')
  })

  it('admin 看别人的任务：提示词可见', async () => {
    authStore.isAdmin = true
    authStore.effectiveRole = 'admin'

    const wrapper = await mountAs({ ...task, user_id: 'user-other' })

    expect(wrapper.text()).toContain('提示词')
    expect(wrapper.text()).toContain('一只橘猫在阳光下缓慢走过')
  })
})

async function clickTab(wrapper: ReturnType<typeof mount>, label: string) {
  const tab = wrapper.findAll('.el-tabs__item').find(item => item.text().includes(label))
  expect(tab, `tab ${label} should exist`).toBeTruthy()
  await tab!.trigger('click')
  await flushPromises()
  await nextTick()
}
