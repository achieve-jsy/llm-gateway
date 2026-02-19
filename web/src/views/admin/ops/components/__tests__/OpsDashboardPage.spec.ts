import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import OpsDashboardPage from '../../OpsDashboardPage.vue'

// Mock 路由
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ replace: vi.fn() }),
}))

// Mock 所有子组件为 stub，避免 element-plus 依赖问题
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
  DataBoard: { template: '<i />' },
}))

vi.mock('@/api/ops', () => ({
  opsAPI: {
    getDashboard: vi.fn().mockResolvedValue({
      overview: {
        health_score: 50,
        system_metrics: {
          cpu_usage_percent: 0, memory_used_mb: 0, memory_total_mb: 0,
          memory_usage_percent: 0, goroutine_count: 0, db_active_count: 0,
          db_idle_count: 0, db_waiting_count: 0, db_max_open_conns: 0,
          redis_total_conns: 0, redis_idle_conns: 0, redis_pool_size: 0,
          collected_at: '',
        },
        // 故意省略 traffic / sla / latency / errors 字段，模拟后端不完整响应
        traffic: { total_requests: 0, total_tokens: 0, avg_qps: 0, avg_tps: 0, current_qps: 0, current_tps: 0, peak_qps: 0, peak_tps: 0 },
        sla: { success_rate: 1, error_rate: 0, sla_error_count: 0 },
        latency: {},
        errors: { upstream_errors: 0, request_errors: 0, business_errors: 0 },
        jobs: [],
      },
      timestamp: '2026-06-30T00:00:00Z',
    }),
    getDashboardSettings: vi.fn().mockResolvedValue({
      auto_refresh_interval: 30, show_alert_events: false,
      show_token_stats: false, show_concurrency_card: false,
      show_switch_rate_chart: false, default_query_mode: 'auto',
    }),
    getTrafficTrend: vi.fn().mockResolvedValue({ points: [], bucket: '5 minutes' }),
    getThroughputTrend: vi.fn().mockResolvedValue({ points: [], by_provider: [], top_groups: [] }),
    getErrorTrend: vi.fn().mockResolvedValue({ points: [] }),
    getLatencyHistogram: vi.fn().mockResolvedValue({ buckets: [], total_requests: 0 }),
    getErrorDistribution: vi.fn().mockResolvedValue({ distribution: [] }),
    getSwitchRateTrend: vi.fn().mockResolvedValue({ points: [] }),
  },
}))

const commonStubs = {
  OpsDashboardSkeleton: true,
  OpsDashboardHeader: true,
  OpsConcurrencyCard: true,
  OpsSwitchRateTrendChart: true,
  OpsTrafficTrendChart: true,
  OpsLatencyHistogram: true,
  OpsErrorDistributionChart: true,
  OpsErrorTrendChart: true,
  OpsTokenStatsCard: true,
  OpsErrorLogTable: true,
  OpsAlertEventsCard: true,
  OpsSystemLogTable: true,
  OpsSettingsDialog: true,
  OpsAlertRulesCard: true,
  OpsErrorDetailsModal: true,
  OpsErrorDetailModal: true,
  OpsRequestDetailsModal: true,
  OpsEmailNotificationCard: true,
  OpsRuntimeSettingsCard: true,
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

describe('OpsDashboardPage', () => {
  describe('TDD: isIdle computed 防御性修复', () => {
    it('当 dashboardData 中 traffic 为 null/undefined 时，isIdle 不应崩溃', async () => {
      const wrapper = mount(OpsDashboardPage, {
        global: { stubs: commonStubs },
      })

      // 直接改 dashboardData 让 traffic 缺失，模拟后端不完整响应
      const vm = wrapper.vm as any
      vm.dashboardData = {
        health_score: 50,
        // ov.traffic === undefined
      }

      // 核心断言：isIdle 不应抛 TypeError
      expect(() => vm.isIdle).not.toThrow()

      // 在 traffic 缺失时，isIdle 应回退到 true（视为空闲）
      expect(vm.isIdle).toBe(true)

      wrapper.unmount()
    })

    it('当 dashboardData 本身为 null 时，isIdle 不应崩溃', async () => {
      const wrapper = mount(OpsDashboardPage, {
        global: { stubs: commonStubs },
      })

      const vm = wrapper.vm as any
      vm.dashboardData = null

      expect(() => vm.isIdle).not.toThrow()
      expect(vm.isIdle).toBe(true)

      wrapper.unmount()
    })

    it('当 dashboardData 完整时，isIdle 应按 traffic 字段计算', async () => {
      const wrapper = mount(OpsDashboardPage, {
        global: { stubs: commonStubs },
      })

      const vm = wrapper.vm as any
      vm.dashboardData = {
        traffic: { total_requests: 0, current_qps: 0 },
        sla: { error_rate: 0 },
      }

      expect(() => vm.isIdle).not.toThrow()
      expect(vm.isIdle).toBe(true)

      // 有流量时应返回 false
      vm.dashboardData = {
        traffic: { total_requests: 100, current_qps: 1.5 },
        sla: { error_rate: 0.01 },
      }

      expect(() => vm.isIdle).not.toThrow()
      expect(vm.isIdle).toBe(false)

      wrapper.unmount()
    })
  })
})
