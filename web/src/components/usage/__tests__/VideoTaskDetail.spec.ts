import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ElementPlus from 'element-plus'
import VideoTaskDetail from '../VideoTaskDetail.vue'

// Mock element-plus icons (avoid loading actual icon files)
vi.mock('@element-plus/icons-vue', () => ({
  DataBoard: { template: '<i />' },
  Refresh: { template: '<i />' },
  VideoPause: { template: '<i />' },
  VideoPlay: { template: '<i />' },
}))

describe('VideoTaskDetail', () => {
  let wrapper: ReturnType<typeof mount>

  afterEach(() => {
    // Clean up any teleported drawer content left in the DOM
    document.body.innerHTML = ''
  })
  const mockTask = {
    task_id: 'cgt-test-123',
    upstream_task_id: 'cgt-upstream-456',
    model: 'doubao-seedance-2-0-260128',
    provider_name: '中转 A',
    status: 'succeeded',
    created_at: '2026-07-08 14:30:00',
    updated_at: '2026-07-08 14:30:45',
    video_url: 'https://example.com/video.mp4',
    request_body: { prompt: 'test prompt', size: '1280x720' },
    response_body: { task_id: 'cgt-test-123', status: 'succeeded' },
    events: [
      { timestamp: '2026-07-08 14:30:00', type: 'submitted', message: '任务创建成功' },
      { timestamp: '2026-07-08 14:30:05', type: 'running', message: '上游开始处理' },
      { timestamp: '2026-07-08 14:30:45', type: 'succeeded', message: '生成完成' },
    ],
  }

  const defaultProps = {
    task: mockTask as any,
    modelValue: true,
  }

  function mountComponent(props = {}) {
    wrapper = mount(VideoTaskDetail, {
      props: { ...defaultProps, ...props },
      global: {
        plugins: [ElementPlus],
      },
      attachTo: document.body,
    })
    return wrapper
  }

  // Helper: get text from teleported drawer content
  function getDrawerText() {
    return document.body.textContent || ''
  }

  describe('render', () => {
    it('renders task info in teleported drawer', async () => {
      const wrapper = mountComponent()
      await nextTick()

      const text = getDrawerText()
      expect(text).toContain('cgt-test-123')
      expect(text).toContain('doubao-seedance-2-0-260128')
      expect(text).toContain('中转 A')
    })

    it('shows video download button when status is succeeded', async () => {
      const wrapper = mountComponent()
      await nextTick()

      const text = getDrawerText()
      expect(text).toContain('下载视频')
    })

    it('shows empty state when status is not succeeded', async () => {
      const wrapper = mountComponent({
        task: { ...mockTask, status: 'running' },
      })
      await nextTick()

      const text = getDrawerText()
      expect(text).toContain('视频尚未生成完成')
    })

    it('shows empty state when status is failed', async () => {
      const wrapper = mountComponent({
        task: { ...mockTask, status: 'failed' },
      })
      await nextTick()

      const text = getDrawerText()
      expect(text).toContain('视频尚未生成完成')
    })

    it('has 5 tabs', async () => {
      const wrapper = mountComponent()
      await nextTick()

      const tabs = document.body.querySelectorAll('.el-tabs__item')
      expect(tabs.length).toBe(5)
    })

    it('renders JSON viewer', async () => {
      const wrapper = mountComponent()
      await nextTick()

      const jsonViewers = document.body.querySelectorAll('.json-viewer')
      expect(jsonViewers.length).toBeGreaterThan(0)
    })

    it('does not crash when task is null', async () => {
      const wrapper = mountComponent({
        task: null,
      })
      await nextTick()

      expect(wrapper.exists()).toBe(true)
      const text = getDrawerText()
      expect(text).toContain('视频任务详情')
    })

    it('renders status tag', async () => {
      const wrapper = mountComponent()
      await nextTick()

      const text = getDrawerText()
      expect(text).toContain('succeeded')
    })
  })

  describe('getStatusType', () => {
    it('maps status to correct type', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.getStatusType('submitted')).toBe('info')
      expect(vm.getStatusType('running')).toBe('warning')
      expect(vm.getStatusType('succeeded')).toBe('success')
      expect(vm.getStatusType('failed')).toBe('danger')
      expect(vm.getStatusType(undefined)).toBe('info')
      expect(vm.getStatusType('unknown')).toBe('info')
    })
  })

  describe('getEventType', () => {
    it('maps event type correctly', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.getEventType('submitted')).toBe('primary')
      expect(vm.getEventType('running')).toBe('warning')
      expect(vm.getEventType('succeeded')).toBe('success')
      expect(vm.getEventType('failed')).toBe('danger')
      expect(vm.getEventType('unknown')).toBe('info')
    })
  })

  describe('formatJSON', () => {
    it('formats object to JSON string', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      const result = vm.formatJSON({ a: 1, b: 'test' })
      expect(result).toBe('{\n  "a": 1,\n  "b": "test"\n}')
    })

    it('returns placeholder for null/undefined data', () => {
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      expect(vm.formatJSON(null)).toBe('暂无数据')
      expect(vm.formatJSON(undefined)).toBe('暂无数据')
    })
  })

  describe('downloadVideo', () => {
    it('opens new window when video_url exists', () => {
      const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
      const wrapper = mountComponent()
      const vm = wrapper.vm as any

      vm.downloadVideo()

      expect(openSpy).toHaveBeenCalledWith('https://example.com/video.mp4', '_blank')
      openSpy.mockRestore()
    })

    it('does not throw when video_url is undefined', () => {
      const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)
      const wrapper = mountComponent({
        task: { ...mockTask, video_url: undefined },
      })
      const vm = wrapper.vm as any

      expect(() => vm.downloadVideo()).not.toThrow()
      openSpy.mockRestore()
    })
  })

  describe('v-model', () => {
    it('accepts modelValue prop', () => {
      const wrapper = mountComponent({
        modelValue: false,
      })

      expect(wrapper.props('modelValue')).toBe(false)
    })
  })
})