import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, computed, nextTick } from 'vue'
import OpsDashboardHeader from '../OpsDashboardHeader.vue'

// Mock element-plus icons (避免加载实际图标文件)
vi.mock('@element-plus/icons-vue', () => ({
  DataBoard: { template: '<i />' },
  Refresh: { template: '<i />' },
  VideoPause: { template: '<i />' },
  VideoPlay: { template: '<i />' },
}))

// Mock opsAPI 让其返回空数据
vi.mock('@/api/ops', () => ({
  opsAPI: {
    getRealtimeTrafficSummary: vi.fn().mockResolvedValue({ current_qps: 0, current_tps: 0, window: '1min' }),
    getGroupOptions: vi.fn().mockResolvedValue([]),
  },
  subscribeQPS: vi.fn(() => vi.fn()),
}))

describe('OpsDashboardHeader', () => {
  describe('TDD: toFixed 空指针崩溃修复', () => {
    it('当 overview 为 null 时，渲染不应抛出 toFixed TypeError', async () => {
      // 这是触发线上 bug 的场景：props.overview 初始为 null 时
      // 所有 computed（currentQPS/peakQPS/sla.success_rate 等）应为数字而不是 undefined

      // 先 stub 掉所有 element-plus 组件为普通 div
      const stubs = {
        ElIcon: true,
        ElButton: true,
        ElTooltip: true,
        ElProgress: true,
        ElPopover: true,
        ElSelect: true,
        ElOption: true,
        ElDialog: true,
        ElDatePicker: true,
        ElInput: true,
        ElTag: true,
        ElCheckbox: true,
      }

      const wrapper = mount(OpsDashboardHeader, {
        props: {
          overview: null,
          platform: '',
          groupId: null,
          timeRange: '1h',
          queryMode: 'auto',
          loading: false,
          autoRefresh: true,
          autoRefreshCountdown: 30,
          lastUpdated: null,
          fullscreen: false,
          customStartTime: null,
          customEndTime: null,
        },
        global: { stubs },
      })

      // 核心断言：mount 应该成功（不抛异常）
      expect(wrapper.exists()).toBe(true)

      // 渲染后触发 Vue computed 求值（强制访问所有 computed）
      await nextTick()

      // 显式验证所有可能触发 toFixed 的 computed 值都是数字
      const vm = wrapper.vm as any
      expect(typeof vm.currentQPS).toBe('number')
      expect(typeof vm.currentTPS).toBe('number')
      expect(typeof vm.peakQPS).toBe('number')
      expect(typeof vm.peakTPS).toBe('number')
      expect(typeof vm.sla?.success_rate).toBe('number')
      expect(typeof vm.upstreamErrorRate).toBe('number')
      expect(typeof vm.systemMetrics?.cpu_usage_percent).toBe('number')
      expect(typeof vm.systemMetrics?.memory_usage_percent).toBe('number')
      expect(typeof vm.systemMetrics?.memory_used_mb).toBe('number')
      expect(typeof vm.systemMetrics?.memory_total_mb).toBe('number')
      expect(typeof vm.dbUsagePercent).toBe('number')
      expect(typeof vm.redisUsagePercent).toBe('number')
      expect(typeof vm.healthScore).toBe('number')

      // 验证 toFixed 可以被安全调用（这才是真正触发 bug 的调用）
      expect(() => vm.currentQPS.toFixed(1)).not.toThrow()
      expect(() => vm.currentTPS.toFixed(1)).not.toThrow()
      expect(() => vm.peakQPS.toFixed(1)).not.toThrow()
      expect(() => vm.peakTPS.toFixed(1)).not.toThrow()
      expect(() => vm.sla.success_rate.toFixed(2)).not.toThrow()
      expect(() => vm.upstreamErrorRate.toFixed(2)).not.toThrow()
      expect(() => vm.systemMetrics.cpu_usage_percent.toFixed(1)).not.toThrow()
      expect(() => vm.systemMetrics.memory_usage_percent.toFixed(1)).not.toThrow()
      expect(() => vm.systemMetrics.memory_used_mb.toFixed(0)).not.toThrow()
      expect(() => vm.systemMetrics.memory_total_mb.toFixed(0)).not.toThrow()
      expect(() => vm.dbUsagePercent.toFixed(0)).not.toThrow()
      expect(() => vm.redisUsagePercent.toFixed(0)).not.toThrow()

      wrapper.unmount()
    })

    it('当 overview 中 traffic 字段部分缺失时，不应崩溃', async () => {
      // 后端可能返回不完整的 traffic 对象（某些字段缺失）
      const partialOverview = {
        health_score: 50,
        system_metrics: {
          cpu_usage_percent: 0,
          memory_used_mb: 0,
          memory_total_mb: 0,
          memory_usage_percent: 0,
          goroutine_count: 0,
          db_active_count: 0,
          db_idle_count: 0,
          db_waiting_count: 0,
          db_max_open_conns: 0,
          redis_total_conns: 0,
          redis_idle_conns: 0,
          redis_pool_size: 0,
          collected_at: '',
        },
        // traffic 故意省略 peak_qps, current_tps 等
        traffic: {
          total_requests: 1,
          total_tokens: 100,
          avg_qps: 0.1,
        },
        sla: { success_rate: 1, error_rate: 0, sla_error_count: 0 },
        latency: {},
        errors: { upstream_errors: 0, request_errors: 0 },
      }

      const stubs = {
        ElIcon: true, ElButton: true, ElTooltip: true, ElProgress: true,
        ElPopover: true, ElSelect: true, ElOption: true, ElDialog: true,
        ElDatePicker: true, ElInput: true, ElTag: true, ElCheckbox: true,
      }

      const wrapper = mount(OpsDashboardHeader, {
        props: {
          overview: partialOverview as any,
          platform: '',
          groupId: null,
          timeRange: '1h',
          queryMode: 'auto',
          loading: false,
          autoRefresh: true,
          autoRefreshCountdown: 30,
          lastUpdated: null,
          fullscreen: false,
          customStartTime: null,
          customEndTime: null,
        },
        global: { stubs },
      })

      await nextTick()

      const vm = wrapper.vm as any

      // 即使 traffic 缺少字段，computed 仍应回退到 0
      expect(typeof vm.peakQPS).toBe('number')
      expect(typeof vm.peakTPS).toBe('number')
      expect(() => vm.peakQPS.toFixed(1)).not.toThrow()
      expect(() => vm.peakTPS.toFixed(1)).not.toThrow()

      // 即使 sla 完整但 success_rate=1，乘以 100 后 toFixed 也应可调用
      expect(() => (vm.sla.success_rate * 100).toFixed(2)).not.toThrow()

      // errors 字段也应兜底
      expect(vm.errors?.request_errors).toBe(0)
      expect(vm.errors?.upstream_errors).toBe(0)

      wrapper.unmount()
    })
  })
})
