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
  VideoPause: { template: '<i />' },
  VideoPlay: { template: '<i />' },
}))

vi.mock('@/api/ops', () => ({
  opsAPI: {
    getDashboard: () => Promise.resolve({ overview: null }),
    getSnapshotV2: () => Promise.resolve({ overview: null, throughput_trend: { points: [] }, error_trend: { points: [] } }),
    getTrafficTrend: () => Promise.resolve({ points: [] }),
    getThroughputTrend: () => Promise.resolve({ points: [], by_provider: [], top_groups: [] }),
    getErrorTrend: () => Promise.resolve({ points: [] }),
    getLatencyHistogram: () => Promise.resolve({ buckets: [], total_requests: 0 }),
    getErrorDistribution: () => Promise.resolve({ distribution: [] }),
    getSwitchRateTrend: () => Promise.resolve({ points: [] }),
    getDashboardSettings: () => Promise.resolve({ auto_refresh_interval: 30 }),
    listProviders: () => Promise.resolve([]),
    listModels: () => Promise.resolve([]),
    getGroupOptions: () => Promise.resolve([]),
    subscribeQPS: () => () => {},
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
