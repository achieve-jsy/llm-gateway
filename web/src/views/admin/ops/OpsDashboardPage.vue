<template>
  <div :class="['ops-page', { 'ops-page--fullscreen': isFullscreen }]">
    <OpsDashboardSkeleton v-if="initialLoading" :fullscreen="isFullscreen" />

    <template v-else>
      <!-- Dashboard Header -->
      <OpsDashboardHeader
        :overview="dashboardData"
        :loading="loading"
        :platform="platform"
        :group-id="groupId"
        :time-range="timeRange"
        :auto-refresh="autoRefresh"
        :auto-refresh-countdown="countdown"
        :last-updated="lastUpdated"
        :fullscreen="isFullscreen"
        :custom-start-time="customStartTime"
        :custom-end-time="customEndTime"
        @update:time-range="timeRange = $event"
        @update:platform="platform = $event; groupId = null"
        @update:group="groupId = $event"
        @update:custom-time-range="onCustomTimeRange"
        @refresh="fetchAllData"
        @open-request-details="showRequestDetails = true"
        @open-error-details="openErrorDetails"
        @open-settings="showSettings = true"
        @open-alert-rules="showAlertRules = true"
        @enter-fullscreen="isFullscreen = true"
        @exit-fullscreen="isFullscreen = false"
      />

      <!-- Row: Concurrency + Switch Rate + Throughput -->
      <div class="ops-row-concurrency">
        <OpsConcurrencyCard
          v-if="settings?.show_concurrency_card"
          :refresh-token="refreshToken"
          :platform-filter="platform"
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
          :points="trafficTrendData"
          :loading="loadingTrend"
          :by-provider="throughputByProvider"
          :top-groups="throughputTopGroups"
          @select-provider="onSelectProvider"
          @select-group="onSelectGroup"
          @open-details="showRequestDetails = true"
        />
      </div>

      <!-- Row: Visual Analysis -->
      <div class="ops-three-col">
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

      <!-- Token Stats -->
      <OpsTokenStatsCard v-if="settings?.show_token_stats" />

      <!-- Alert Events -->
      <OpsAlertEventsCard v-if="settings?.show_alert_events" />

      <!-- System Logs -->
      <OpsSystemLogTable v-if="!isFullscreen" />

      <!-- Modals & Dialogs (hidden in fullscreen) -->
      <template v-if="!isFullscreen">
        <OpsSettingsDialog
          :show="showSettings"
          @close="showSettings = false"
          @saved="onSettingsSaved"
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
    <el-button size="small" :icon="Refresh" :loading="loading" @click="fetchAllData" circle />
    <el-button size="small" @click="isFullscreen = false">退出全屏</el-button>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, computed, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
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
const platform = ref('')
const groupId = ref<number | null>(null)
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
const errorDetailsType = ref<'request' | 'upstream'>('request')
const selectedErrorId = ref<number | null>(null)

let refreshTimer: number | null = null
let countdownTimer: number | null = null
const REFRESH_INTERVAL = computed(() => (settings.value.auto_refresh_interval || 30) * 1000)

const REFRESH_SECONDS = computed(() => settings.value.auto_refresh_interval || 30)

// -- Fetchers --

async function fetchAllData() {
  await Promise.all([
    fetchDashboard(),
    fetchTrafficAndThroughput(),
    fetchErrorTrendFn(),
    fetchLatencyHistogram(),
    fetchErrorDistribution(),
    fetchSwitchRate()
  ])
  lastUpdated.value = new Date()
  refreshToken.value++
}

async function fetchDashboard() {
  loading.value = true
  try {
    const response = await opsAPI.getDashboard(timeRange.value)
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
      opsAPI.getTrafficTrend(timeRange.value),
      opsAPI.getThroughputTrend(timeRange.value).catch(() => null)
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
    const response = await opsAPI.getErrorTrend(timeRange.value)
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
    const response = await opsAPI.getLatencyHistogram(timeRange.value)
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
    const response = await opsAPI.getErrorDistribution(timeRange.value)
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
    const response = await opsAPI.getSwitchRateTrend('5h')
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

function onSelectProvider(providerId: number) {
  // TODO: Implement platform filter based on providerId
  console.log('Selected provider:', providerId)
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
  gap: var(--space-4);
  min-height: 100%;
}

.ops-page--fullscreen {
  padding: var(--space-4);
  background: var(--bg-page, #f5f7fa);
  min-height: 100vh;
  overflow-y: auto;
}

.ops-row-concurrency {
  display: grid;
  grid-template-columns: 1fr 1fr 2fr;
  gap: var(--space-4);
}

.ops-three-col {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--space-4);
}

.ops-fullscreen-bar {
  position: fixed;
  top: 12px;
  right: 16px;
  z-index: 9999;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  background: var(--bg-card, #fff);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-lg);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.12);
}

.ops-time-select {
  width: 130px;
}

@media (max-width: 1200px) {
  .ops-row-concurrency {
    grid-template-columns: 1fr 1fr;
  }
}

@media (max-width: 900px) {
  .ops-row-concurrency,
  .ops-three-col {
    grid-template-columns: 1fr;
  }
}
</style>
