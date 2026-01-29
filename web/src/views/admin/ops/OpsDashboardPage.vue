<template>
  <div class="ops-page">
    <!-- Header Section -->
    <div class="ops-header">
      <div class="ops-header__left">
        <h2 class="ops-title">运维监控看板</h2>
        <span class="ops-subtitle">实时监控 API 性能、流量和系统健康状态</span>
      </div>
      <div class="ops-header__right">
        <el-select v-model="timeRange" size="default" class="ops-time-select">
          <el-option label="最近 5 分钟" value="5m" />
          <el-option label="最近 30 分钟" value="30m" />
          <el-option label="最近 1 小时" value="1h" />
          <el-option label="最近 6 小时" value="6h" />
          <el-option label="最近 24 小时" value="24h" />
        </el-select>

        <el-button
          :type="autoRefresh ? 'primary' : 'default'"
          :icon="Refresh"
          @click="toggleAutoRefresh"
        >
          {{ autoRefresh ? '自动刷新中' : '自动刷新' }}
        </el-button>

        <el-button
          :icon="Refresh"
          :loading="loading"
          @click="fetchAllData"
          circle
          size="small"
          title="刷新"
        />
      </div>
    </div>

    <!-- Main Content -->
    <div class="ops-body">
      <!-- Dashboard Header (Health Score + System Metrics) -->
      <OpsDashboardHeader
        :overview="dashboardData"
        :loading="loading"
      />

      <!-- Traffic Trend Chart -->
      <OpsTrafficTrendChart
        :points="trafficTrendData"
        :loading="loadingTrend"
      />

      <!-- Two Column Layout for Error Charts -->
      <div class="ops-two-col">
        <!-- Error Trend Chart -->
        <OpsErrorTrendChart
          :points="errorTrendData"
          :loading="loadingErrorTrend"
        />

        <!-- Latency Histogram -->
        <OpsLatencyHistogram
          :buckets="latencyHistogramData"
          :total-requests="latencyTotalRequests"
          :loading="loadingLatency"
        />
      </div>

      <!-- Error Distribution Chart -->
      <OpsErrorDistributionChart
        :distribution="errorDistributionData"
        :loading="loadingErrorDistribution"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'
import type {
  OpsOverview,
  OpsTrafficPoint,
  OpsErrorPoint,
  OpsLatencyBucket,
  OpsErrorDistributionItem
} from '@/api/ops'
import OpsDashboardHeader from './components/OpsDashboardHeader.vue'
import OpsTrafficTrendChart from './components/OpsTrafficTrendChart.vue'
import OpsErrorTrendChart from './components/OpsErrorTrendChart.vue'
import OpsLatencyHistogram from './components/OpsLatencyHistogram.vue'
import OpsErrorDistributionChart from './components/OpsErrorDistributionChart.vue'

// Data refs
const dashboardData = ref<OpsOverview | null>(null)
const trafficTrendData = ref<OpsTrafficPoint[]>([])
const errorTrendData = ref<OpsErrorPoint[]>([])
const latencyHistogramData = ref<OpsLatencyBucket[]>([])
const latencyTotalRequests = ref(0)
const errorDistributionData = ref<OpsErrorDistributionItem[]>([])

// Loading states
const loading = ref(false)
const loadingTrend = ref(false)
const loadingErrorTrend = ref(false)
const loadingLatency = ref(false)
const loadingErrorDistribution = ref(false)

// Time range
const timeRange = ref('1h')

// Auto refresh
const autoRefresh = ref(true)
let refreshTimer: number | null = null
const REFRESH_INTERVAL = 30000 // 30 seconds

// Fetch all data
async function fetchAllData() {
  await Promise.all([
    fetchDashboard(),
    fetchTrafficTrend(),
    fetchErrorTrend(),
    fetchLatencyHistogram(),
    fetchErrorDistribution()
  ])
}

// Fetch dashboard overview
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

// Fetch traffic trend
async function fetchTrafficTrend() {
  loadingTrend.value = true
  try {
    const response = await opsAPI.getTrafficTrend(timeRange.value)
    trafficTrendData.value = response.points
  } catch (error) {
    console.error('Failed to fetch traffic trend:', error)
  } finally {
    loadingTrend.value = false
  }
}

// Fetch error trend
async function fetchErrorTrend() {
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

// Fetch latency histogram
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

// Fetch error distribution
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

// Auto refresh control
function startAutoRefresh() {
  if (refreshTimer) {
    clearInterval(refreshTimer)
  }
  refreshTimer = window.setInterval(() => {
    if (!loading.value) {
      fetchAllData()
    }
  }, REFRESH_INTERVAL)
}

function stopAutoRefresh() {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
  if (autoRefresh.value) {
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
}

// Watch time range changes
watch(timeRange, () => {
  fetchAllData()
})

// Lifecycle
onMounted(() => {
  fetchAllData()
  if (autoRefresh.value) {
    startAutoRefresh()
  }
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.ops-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  min-height: 100%;
}

.ops-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.ops-header__left {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
}

.ops-title {
  font-size: var(--font-size-title);
  font-weight: 700;
  color: var(--text-primary);
  margin: 0;
}

.ops-subtitle {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}

.ops-header__right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.ops-time-select {
  width: 150px;
}

.ops-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.ops-two-col {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--space-4);
}

@media (max-width: 900px) {
  .ops-two-col {
    grid-template-columns: 1fr;
  }

  .ops-header {
    flex-direction: column;
  }
}

@media (max-width: 600px) {
  .ops-header__right {
    flex-wrap: wrap;
  }
}
</style>
