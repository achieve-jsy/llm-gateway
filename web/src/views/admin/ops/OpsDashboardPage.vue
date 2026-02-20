<template>
  <div :class="['ops-page', { 'ops-page--fullscreen': isFullscreen }]">
    <OpsDashboardSkeleton v-if="initialLoading" :fullscreen="isFullscreen" />

    <template v-else>
      <!-- Dashboard Header (toolbar + health + metrics) -->
      <OpsDashboardHeader
        :overview="dashboardData"
        :loading="loading"
        :group-id="groupId"
        :provider="provider"
        :model="model"
        :time-range="timeRange"
        :auto-refresh="autoRefresh"
        :auto-refresh-countdown="countdown"
        :last-updated="lastUpdated"
        :fullscreen="isFullscreen"
        :custom-start-time="customStartTime"
        :custom-end-time="customEndTime"
        @update:time-range="timeRange = $event"
        @update:group="groupId = $event"
        @update:provider="provider = $event"
        @update:model="model = $event"
        @update:custom-time-range="onCustomTimeRange"
        @update:auto-refresh="autoRefresh = $event; if (autoRefresh) startAutoRefresh(); else stopAutoRefresh()"
        @refresh="fetchAllData"
        @open-request-details="showRequestDetails = true"
        @open-error-details="openErrorDetails"
        @open-settings="showSettings = true"
        @open-alert-rules="showAlertRules = true"
        @enter-fullscreen="isFullscreen = true"
        @exit-fullscreen="isFullscreen = false"
      />

      <!-- Row 1: Concurrency + Switch Rate + Traffic Trend (1:1:2, 对齐 sub2api grid-cols-4) -->
      <div class="ops-row-main">
        <OpsConcurrencyCard
          v-if="settings?.show_concurrency_card"
          :refresh-token="refreshToken"
          :group-id-filter="groupId"
        />
        <OpsSwitchRateTrendChart
          v-if="settings?.show_switch_rate_chart"
          :points="switchRatePoints"
          :loading="loadingSwitchRate"
          :time-range="'5h'"
          :fullscreen="isFullscreen"
        />
        <OpsTrafficTrendChart
          class="ops-row-span-2"
          :points="trafficTrendData"
          :loading="loadingTrend"
          :by-provider="throughputByProvider"
          :top-groups="throughputTopGroups"
          @select-provider="onSelectProvider"
          @select-group="onSelectGroup"
          @open-details="showRequestDetails = true"
        />
      </div>

      <!-- No-traffic notice: visible when all trend data is empty -->
      <div v-if="isIdle" class="ops-idle-notice">
        <span class="ops-idle-icon">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <line x1="12" y1="8" x2="12" y2="12"/>
            <line x1="12" y1="16" x2="12.01" y2="16"/>
          </svg>
        </span>
        <span class="ops-idle-text">
          当前系统处于<strong>空闲状态</strong>——所选时段内无 API 流量通过网关，趋势图表暂无数据可展示。
          当有实际请求经过 gateway 时，流量趋势、错误趋势、延迟分布等图表将自动填充数据。
          系统级指标（CPU / 内存 / Goroutine / DB 连接池 / Redis 连接池）已实时采集并显示在顶部状态栏。
        </span>
      </div>

      <!-- Row 2: Latency Histogram + Error Distribution + Error Trend (1:1:1) -->
      <div class="ops-row-visual">
        <OpsLatencyHistogram
          :buckets="latencyHistogramData"
          :total-requests="latencyTotalRequests"
          :loading="loadingLatency"
        />
        <OpsErrorDistributionChart
          :distribution="errorDistributionData"
          :loading="loadingErrorDistribution"
          @open-details="openErrorDetails('request')"
        />
        <OpsErrorTrendChart
          :points="errorTrendData"
          :loading="loadingErrorTrend"
          @open-error-details="openErrorDetails"
        />
      </div>

      <!-- Token Stats (full-width) -->
      <OpsTokenStatsCard v-if="settings?.show_token_stats" :refresh-token="refreshToken" />

      <!-- Error Logs (full-width) -->
      <OpsErrorLogTable v-if="!isFullscreen" :refresh-token="refreshToken" />

      <!-- Alert Events (full-width) -->
      <OpsAlertEventsCard v-if="settings?.show_alert_events" :refresh-token="refreshToken" />

      <!-- System Logs (full-width) -->
      <OpsSystemLogTable v-if="!isFullscreen" :refresh-token="refreshToken" />

      <!-- Modals & Dialogs (hidden in fullscreen) -->
      <template v-if="!isFullscreen">
        <OpsSettingsDialog
          :show="showSettings"
          @close="showSettings = false"
          @saved="onSettingsSaved"
          @open-email-notification="showEmailNotification = true"
          @open-runtime-settings="showRuntimeSettings = true"
        />

        <el-dialog
          v-model="showAlertRules"
          title="告警规则管理"
          width="90%"
          destroy-on-close
        >
          <OpsAlertRulesCard />
        </el-dialog>

        <OpsErrorDetailsModal
          :show="showErrorDetails"
          :time-range="timeRange"
          :error-type="errorDetailsType"
          @update:show="showErrorDetails = $event"
          @open-detail="openErrorDetail"
        />

        <OpsErrorDetailModal
          :show="!!selectedErrorId"
          :error-id="selectedErrorId"
          :error-type="errorDetailsType"
          @update:show="selectedErrorId = null"
        />

        <OpsRequestDetailsModal
          :show="showRequestDetails"
          :time-range="timeRange"
          @update:show="showRequestDetails = $event"
        />

        <el-dialog
          v-model="showEmailNotification"
          title="邮件通知配置"
          width="680px"
          destroy-on-close
        >
          <OpsEmailNotificationCard />
        </el-dialog>

        <el-dialog
          v-model="showRuntimeSettings"
          title="告警运行时设置"
          width="720px"
          destroy-on-close
        >
          <OpsRuntimeSettingsCard />
        </el-dialog>
      </template>
    </template>
  </div>

  <!-- Floating toolbar when fullscreen -->
  <div v-if="isFullscreen" class="ops-fullscreen-bar">
    <el-select v-model="timeRange" size="small" class="ops-time-select">
      <el-option label="最近 5 分钟" value="5m" />
      <el-option label="最近 30 分钟" value="30m" />
      <el-option label="最近 1 小时" value="1h" />
      <el-option label="最近 6 小时" value="6h" />
      <el-option label="最近 24 小时" value="24h" />
    </el-select>

    <el-tooltip :content="autoRefresh ? '暂停自动刷新' : '恢复自动刷新'" placement="bottom">
      <el-button
        size="small"
        :icon="autoRefresh ? VideoPause : VideoPlay"
        :type="autoRefresh ? '' : 'info'"
        @click="autoRefresh = !autoRefresh; autoRefresh ? startAutoRefresh() : stopAutoRefresh()"
        circle
      />
    </el-tooltip>

    <span v-if="autoRefresh && countdown > 0" class="ops-fs-countdown">{{ countdown }}s</span>

    <el-button size="small" :icon="Refresh" :loading="loading" @click="fetchAllData" circle />
    <el-button size="small" @click="isFullscreen = false">退出全屏</el-button>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, computed, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Refresh, VideoPause, VideoPlay } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'
import type {
  OpsOverview,
  OpsTrafficPoint,
  OpsErrorPoint,
  OpsLatencyBucket,
  OpsErrorDistributionItem,
  OpsSwitchRatePoint,
  OpsThroughputByProvider,
  OpsThroughputByGroup,
  OpsDashboardSettings
} from '@/api/ops'
import OpsDashboardHeader from './components/OpsDashboardHeader.vue'
import OpsDashboardSkeleton from './components/OpsDashboardSkeleton.vue'
import OpsTrafficTrendChart from './components/OpsTrafficTrendChart.vue'
import OpsErrorTrendChart from './components/OpsErrorTrendChart.vue'
import OpsLatencyHistogram from './components/OpsLatencyHistogram.vue'
import OpsErrorDistributionChart from './components/OpsErrorDistributionChart.vue'
import OpsConcurrencyCard from './components/OpsConcurrencyCard.vue'
import OpsSwitchRateTrendChart from './components/OpsSwitchRateTrendChart.vue'
import OpsAlertEventsCard from './components/OpsAlertEventsCard.vue'
import OpsAlertRulesCard from './components/OpsAlertRulesCard.vue'
import OpsSystemLogTable from './components/OpsSystemLogTable.vue'
import OpsTokenStatsCard from './components/OpsTokenStatsCard.vue'
import OpsSettingsDialog from './components/OpsSettingsDialog.vue'
import OpsErrorDetailsModal from './components/OpsErrorDetailsModal.vue'
import OpsErrorDetailModal from './components/OpsErrorDetailModal.vue'
import OpsRequestDetailsModal from './components/OpsRequestDetailsModal.vue'
import OpsEmailNotificationCard from './components/OpsEmailNotificationCard.vue'
import OpsRuntimeSettingsCard from './components/OpsRuntimeSettingsCard.vue'
import OpsErrorLogTable from './components/OpsErrorLogTable.vue'

const route = useRoute()
const router = useRouter()

// -- State --
const initialLoading = ref(true)
const loading = ref(false)
const loadingTrend = ref(false)
const loadingErrorTrend = ref(false)
const loadingLatency = ref(false)
const loadingErrorDistribution = ref(false)
const loadingSwitchRate = ref(false)
const lastUpdated = ref<Date | null>(null)

const dashboardData = ref<OpsOverview | null>(null)
const trafficTrendData = ref<OpsTrafficPoint[]>([])
const errorTrendData = ref<OpsErrorPoint[]>([])
const latencyHistogramData = ref<OpsLatencyBucket[]>([])
const latencyTotalRequests = ref(0)
const errorDistributionData = ref<OpsErrorDistributionItem[]>([])
const switchRatePoints = ref<OpsSwitchRatePoint[]>([])
const throughputByProvider = ref<OpsThroughputByProvider[]>([])
const throughputTopGroups = ref<OpsThroughputByGroup[]>([])

const timeRange = ref('1h')
const groupId = ref<number | null>(null)
const provider = ref<string>('')
const model = ref<string>('')
const customStartTime = ref<string | null>(null)
const customEndTime = ref<string | null>(null)
const autoRefresh = ref(true)
const isFullscreen = ref(false)
const countdown = ref(30)
const refreshToken = ref(0)

const settings = ref<OpsDashboardSettings>({
  auto_refresh_interval: 30,
  show_alert_events: true,
  show_token_stats: true,
  show_concurrency_card: true,
  show_switch_rate_chart: true,
  default_query_mode: 'auto'
})

const showSettings = ref(false)
const showAlertRules = ref(false)
const showErrorDetails = ref(false)
const showRequestDetails = ref(false)
const showEmailNotification = ref(false)
const showRuntimeSettings = ref(false)
const errorDetailsType = ref<'request' | 'upstream'>('request')
const selectedErrorId = ref<number | null>(null)

let refreshTimer: number | null = null
let countdownTimer: number | null = null
const REFRESH_INTERVAL = computed(() => (settings.value.auto_refresh_interval || 30) * 1000)

const REFRESH_SECONDS = computed(() => settings.value.auto_refresh_interval || 30)

// 判断当前是否处于"无流量"空闲态（dashboard 返回的流量/错误均为 0）
const isIdle = computed(() => {
  const ov = dashboardData.value
  if (!ov) return true
  // 防御性兜底：后端不完整响应（traffic 缺失）视为空闲，不抛 TypeError
  return (ov.traffic?.total_requests ?? 0) === 0 && (ov.traffic?.current_qps ?? 0) === 0
})

// -- Filter Helpers --

function buildFilter(extraTimeRange?: string): { time_range?: string; group_id?: number; provider?: string; model?: string } {
  const filter: { time_range?: string; group_id?: number; provider?: string; model?: string } = {}
  filter.time_range = extraTimeRange || timeRange.value
  if (groupId.value != null) filter.group_id = groupId.value
  if (provider.value) filter.provider = provider.value
  if (model.value) filter.model = model.value
  return filter
}

// -- Fetchers --

async function fetchAllData() {
  loading.value = true

  const remainingFetches: Promise<void>[] = [
    fetchLatencyHistogram(),
    fetchErrorDistribution(),
    fetchSwitchRate()
  ]

  try {
    const snap = await opsAPI.getSnapshotV2(buildFilter())
    dashboardData.value = snap.overview
    trafficTrendData.value = snap.throughput_trend.points as any
    errorTrendData.value = snap.error_trend.points as any
  } catch {
    await Promise.all([
      fetchDashboard(),
      fetchTrafficAndThroughput(),
      fetchErrorTrendFn()
    ])
  }

  await Promise.all(remainingFetches)
  loading.value = false
  lastUpdated.value = new Date()
  refreshToken.value++
}

async function fetchDashboard() {
  loading.value = true
  try {
    const response = await opsAPI.getDashboard(buildFilter())
    dashboardData.value = response.overview
  } catch (error) {
    console.error('Failed to fetch dashboard:', error)
  } finally {
    loading.value = false
  }
}

async function fetchTrafficAndThroughput() {
  loadingTrend.value = true
  try {
    const [trendResp, throughputResp] = await Promise.all([
      opsAPI.getTrafficTrend(buildFilter()),
      opsAPI.getThroughputTrend(buildFilter()).catch(() => null)
    ])
    trafficTrendData.value = trendResp.points
    if (throughputResp) {
      throughputByProvider.value = throughputResp.by_provider || []
      throughputTopGroups.value = throughputResp.top_groups || []
    }
  } catch (error) {
    console.error('Failed to fetch traffic trend:', error)
  } finally {
    loadingTrend.value = false
  }
}

async function fetchErrorTrendFn() {
  loadingErrorTrend.value = true
  try {
    const response = await opsAPI.getErrorTrend(buildFilter())
    errorTrendData.value = response.points
  } catch (error) {
    console.error('Failed to fetch error trend:', error)
  } finally {
    loadingErrorTrend.value = false
  }
}

async function fetchLatencyHistogram() {
  loadingLatency.value = true
  try {
    const response = await opsAPI.getLatencyHistogram(buildFilter())
    latencyHistogramData.value = response.buckets
    latencyTotalRequests.value = response.total_requests
  } catch (error) {
    console.error('Failed to fetch latency histogram:', error)
  } finally {
    loadingLatency.value = false
  }
}

async function fetchErrorDistribution() {
  loadingErrorDistribution.value = true
  try {
    const response = await opsAPI.getErrorDistribution(buildFilter())
    errorDistributionData.value = response.distribution
  } catch (error) {
    console.error('Failed to fetch error distribution:', error)
  } finally {
    loadingErrorDistribution.value = false
  }
}

async function fetchSwitchRate() {
  loadingSwitchRate.value = true
  try {
    const response = await opsAPI.getSwitchRateTrend(buildFilter('5h'))
    switchRatePoints.value = response.points
  } catch (error) {
    console.error('Failed to fetch switch rate trend:', error)
  } finally {
    loadingSwitchRate.value = false
  }
}

async function loadSettings() {
  try {
    const s = await opsAPI.getDashboardSettings()
    settings.value = s
    if (s.auto_refresh_interval) {
      countdown.value = s.auto_refresh_interval
    }
  } catch {
    // Use defaults
  }
}

function onSettingsSaved() {
  loadSettings()
  restartTimers()
}

// -- Event handlers --

function onCustomTimeRange(range: { start: string; end: string }) {
  customStartTime.value = range.start
  customEndTime.value = range.end
  timeRange.value = 'custom'
}

function onSelectProvider(providerName: string) {
  provider.value = providerName
}

function onSelectGroup(groupIdVal: number) {
  groupId.value = groupIdVal
}

function openErrorDetails(type: 'request' | 'upstream') {
  errorDetailsType.value = type
  showErrorDetails.value = true
}

function openErrorDetail(id: number) {
  selectedErrorId.value = id
}

// -- Timers --

function startAutoRefresh() {
  stopAutoRefresh()
  countdown.value = REFRESH_SECONDS.value
  countdownTimer = window.setInterval(() => {
    countdown.value -= 1
    if (countdown.value <= 0) {
      countdown.value = REFRESH_SECONDS.value
      if (!loading.value) {
        fetchAllData()
      }
    }
  }, 1000)
}

function stopAutoRefresh() {
  if (countdownTimer) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
}

function restartTimers() {
  if (autoRefresh.value) {
    startAutoRefresh()
  }
}

// -- Fullscreen --

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && isFullscreen.value) {
    isFullscreen.value = false
  }
}

// -- URL state --

function syncFromRoute() {
  const tr = route.query.tr
  if (typeof tr === 'string' && ['5m', '30m', '1h', '6h', '24h'].includes(tr)) {
    timeRange.value = tr
  }
  isFullscreen.value = route.query.fullscreen === '1'
}

function syncToRoute() {
  const q: Record<string, string> = { ...route.query }
  if (timeRange.value !== '1h') q.tr = timeRange.value
  else delete q.tr
  if (isFullscreen.value) q.fullscreen = '1'
  else delete q.fullscreen
  router.replace({ query: q }).catch(() => {})
}

// -- Watchers --

watch(timeRange, () => {
  fetchAllData()
  syncToRoute()
})

watch(isFullscreen, () => {
  syncToRoute()
})

// -- Lifecycle --

onMounted(async () => {
  syncFromRoute()
  document.addEventListener('keydown', handleKeydown)

  await loadSettings()

  initialLoading.value = true
  await fetchAllData()
  initialLoading.value = false

  if (autoRefresh.value) {
    startAutoRefresh()
  }
})

onUnmounted(() => {
  stopAutoRefresh()
  document.removeEventListener('keydown', handleKeydown)
})
</script>

<style scoped>
.ops-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
  padding-bottom: 48px;
  min-height: 100%;
}

.ops-page--fullscreen {
  padding: var(--space-6);
  background: var(--bg-page, #f5f7fa);
  min-height: 100vh;
  overflow-y: auto;
}

.ops-row-main {
  display: grid;
  grid-template-columns: 1fr 1fr 2fr;
  gap: var(--space-6);
}

.ops-row-main > * {
  min-height: 360px;
}

.ops-row-span-2 {
  grid-column: span 2;
}

.ops-row-visual {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--space-6);
}

.ops-idle-notice {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  background: var(--el-color-info-light-9, #f0f9ff);
  border: 1px solid var(--el-color-info-light-5, #bae6fd);
  border-radius: var(--radius-lg);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

:global(html.dark) .ops-idle-notice {
  background: linear-gradient(135deg, #0c1e2e 0%, #0f2b3d 100%);
  border-color: #164e63;
  color: #7dd3fc;
}

.ops-idle-icon {
  flex-shrink: 0;
  color: var(--el-color-info, #0ea5e9);
  margin-top: 2px;
}

.ops-idle-text strong {
  font-weight: 600;
  color: var(--text-primary);
}

.ops-fullscreen-bar {
  position: fixed;
  top: 12px;
  right: 16px;
  z-index: 9999;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  background: rgba(255, 255, 255, 0.85);
  backdrop-filter: blur(8px);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-lg);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.12);
}

:global(html.dark) .ops-fullscreen-bar {
  background: rgba(24, 24, 27, 0.85);
}

.ops-fs-countdown {
  font-size: 12px;
  color: var(--text-secondary, #888);
  min-width: 28px;
  text-align: center;
  font-variant-numeric: tabular-nums;
}

.ops-time-select {
  width: 130px;
}

@media (max-width: 1200px) {
  .ops-row-main {
    grid-template-columns: 1fr 1fr;
  }
  .ops-row-span-2 {
    grid-column: span 2;
  }
}

@media (max-width: 900px) {
  .ops-row-main,
  .ops-row-visual {
    grid-template-columns: 1fr;
  }
  .ops-row-span-2 {
    grid-column: span 1;
  }
}
</style>
