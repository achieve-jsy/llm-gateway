<template>
  <div class="ops-header-card">
    <!-- Top Toolbar -->
    <div class="ops-toolbar">
      <div class="ops-toolbar__left">
        <h1 class="ops-toolbar-title">
          <el-icon color="#3b82f6"><DataBoard /></el-icon>
          运维监控
        </h1>
        <div class="ops-toolbar-status">
          <span class="ops-status-indicator">
            <span class="ops-status-dot" :class="{ 'ops-status-dot--loading': loading, 'ops-status-dot--ready': !loading }"></span>
            {{ loading ? '加载中...' : '就绪' }}
          </span>
          <span class="ops-toolbar-divider">·</span>
          <span>自动刷新: {{ autoRefresh ? '开启' : '关闭' }}</span>
        </div>
      </div>
    </div>

    <!-- Top Section: Health Score + Realtime Traffic -->
    <div class="ops-top-section">
      <!-- Health Score Circle -->
      <div class="ops-health-circle">
        <svg class="ops-health-svg">
          <!-- Background circle -->
          <circle
            cx="64"
            cy="64"
            r="56"
            :stroke="circleBgColor"
            stroke-width="8"
            fill="none"
          />
          <!-- Progress circle -->
          <circle
            cx="64"
            cy="64"
            r="56"
            :stroke="healthColor"
            stroke-width="8"
            fill="none"
            :stroke-dasharray="circumference"
            :stroke-dashoffset="dashOffset"
            stroke-linecap="round"
            class="ops-health-progress"
          />
        </svg>
        <div class="ops-health-score">
          <span class="ops-health-value" :style="{ color: healthColor }">{{ healthScore }}</span>
          <span class="ops-health-label">健康评分</span>
        </div>
      </div>

      <!-- Realtime Traffic -->
      <div class="ops-realtime-traffic">
        <div class="ops-realtime-header">
          <span class="ops-realtime-ping"></span>
          <span class="ops-realtime-label">实时流量</span>
        </div>
        <div class="ops-realtime-metrics">
          <div class="ops-realtime-item">
            <span class="ops-realtime-value">{{ currentQPS.toFixed(1) }}</span>
            <span class="ops-realtime-unit">QPS</span>
          </div>
          <div class="ops-realtime-item">
            <span class="ops-realtime-value">{{ currentTPS.toFixed(1) }}</span>
            <span class="ops-realtime-unit">TPS</span>
          </div>
        </div>
        <div class="ops-realtime-peak">
          峰值 QPS: {{ peakQPS.toFixed(1) }} / TPS: {{ peakTPS.toFixed(1) }}
        </div>
      </div>
    </div>

    <!-- Metrics Cards Grid -->
    <div class="ops-metrics-grid">
      <!-- Total Requests -->
      <div class="ops-metric-card">
        <div class="ops-metric-label">请求总量</div>
        <div class="ops-metric-value">{{ formatNumber(traffic.total_requests) }}</div>
        <div class="ops-metric-sub">
          Tokens: {{ formatNumber(traffic.total_tokens) }}
        </div>
      </div>

      <!-- Success Rate -->
      <div class="ops-metric-card">
        <div class="ops-metric-label">成功率 (SLA)</div>
        <div class="ops-metric-value" :style="{ color: slaColor }">
          {{ (sla.success_rate * 100).toFixed(2) }}%
        </div>
        <div class="ops-metric-sub">
          失败: {{ sla.sla_error_count }}
        </div>
      </div>

      <!-- Error Stats -->
      <div class="ops-metric-card">
        <div class="ops-metric-label">错误统计</div>
        <div class="ops-metric-value ops-metric-value--danger">
          {{ errors.request_errors + errors.upstream_errors }}
        </div>
        <div class="ops-metric-sub">
          4xx: {{ errors.request_errors }} / 5xx: {{ errors.upstream_errors }}
        </div>
      </div>

      <!-- Latency P99 -->
      <div class="ops-metric-card">
        <div class="ops-metric-label">延迟 P99</div>
        <div class="ops-metric-value">{{ latency.duration_p99_ms }}ms</div>
        <div class="ops-metric-sub">
          P50: {{ latency.duration_p50_ms }}ms / P95: {{ latency.duration_p95_ms }}ms
        </div>
      </div>

      <!-- TTFT P99 -->
      <div class="ops-metric-card">
        <div class="ops-metric-label">首字延迟 P99</div>
        <div class="ops-metric-value">{{ latency.ttft_p99_ms }}ms</div>
        <div class="ops-metric-sub">
          P50: {{ latency.ttft_p50_ms }}ms / P95: {{ latency.ttft_p95_ms }}ms
        </div>
      </div>

      <!-- Upstream Error Rate -->
      <div class="ops-metric-card">
        <div class="ops-metric-label">上游错误率</div>
        <div class="ops-metric-value" :style="{ color: upstreamErrorColor }">
          {{ upstreamErrorRate.toFixed(2) }}%
        </div>
        <div class="ops-metric-sub">
          {{ errors.upstream_errors }} / {{ traffic.total_requests }}
        </div>
      </div>
    </div>

    <!-- System Metrics Bar -->
    <div class="ops-system-metrics">
      <!-- CPU -->
      <div class="ops-system-item">
        <div class="ops-system-label">CPU</div>
        <div class="ops-system-value" :style="{ color: cpuColor }">
          {{ systemMetrics.cpu_usage_percent.toFixed(1) }}%
        </div>
      </div>

      <!-- Memory -->
      <div class="ops-system-item">
        <div class="ops-system-label">内存</div>
        <div class="ops-system-value" :style="{ color: memoryColor }">
          {{ systemMetrics.memory_usage_percent.toFixed(1) }}%
        </div>
        <div class="ops-system-sub">
          {{ systemMetrics.memory_used_mb.toFixed(0) }} / {{ systemMetrics.memory_total_mb.toFixed(0) }} MB
        </div>
      </div>

      <!-- DB Connections -->
      <div class="ops-system-item">
        <div class="ops-system-label">DB 连接</div>
        <div class="ops-system-value" :style="{ color: dbColor }">
          {{ dbUsagePercent.toFixed(0) }}%
        </div>
        <div class="ops-system-sub">
          {{ systemMetrics.db_active_count }} / {{ systemMetrics.db_max_open_conns }}
        </div>
      </div>

      <!-- Redis Connections -->
      <div class="ops-system-item">
        <div class="ops-system-label">Redis 连接</div>
        <div class="ops-system-value" :style="{ color: redisColor }">
          {{ redisUsagePercent.toFixed(0) }}%
        </div>
        <div class="ops-system-sub">
          {{ systemMetrics.redis_total_conns }} / {{ systemMetrics.redis_pool_size }}
        </div>
      </div>

      <!-- Goroutines -->
      <div class="ops-system-item">
        <div class="ops-system-label">Goroutine</div>
        <div class="ops-system-value" :style="{ color: goroutineColor }">
          {{ systemMetrics.goroutine_count }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { DataBoard } from '@element-plus/icons-vue'
import type { OpsOverview } from '@/api/ops'

const props = defineProps<{
  overview: OpsOverview | null
  loading: boolean
  autoRefresh?: boolean
}>()

const autoRefresh = computed(() => props.autoRefresh ?? false)

// Health score calculations
const healthScore = computed(() => props.overview?.health_score ?? 0)
const healthColor = computed(() => {
  if (healthScore.value >= 80) return '#10b981'
  if (healthScore.value >= 60) return '#f59e0b'
  return '#ef4444'
})

const circleBgColor = computed(() => {
  return document.documentElement.classList.contains('dark') ? '#334155' : '#e5e7eb'
})

const circumference = 2 * Math.PI * 56 // radius = 56
const dashOffset = computed(() => {
  const percent = healthScore.value / 100
  return circumference * (1 - percent)
})

// Traffic data
const traffic = computed(() => props.overview?.traffic ?? {
  total_requests: 0,
  total_tokens: 0,
  avg_qps: 0,
  avg_tps: 0,
  current_qps: 0,
  current_tps: 0,
  peak_qps: 0,
  peak_tps: 0,
})

const currentQPS = computed(() => traffic.value.current_qps)
const currentTPS = computed(() => traffic.value.current_tps)
const peakQPS = computed(() => traffic.value.peak_qps)
const peakTPS = computed(() => traffic.value.peak_tps)

// SLA data
const sla = computed(() => props.overview?.sla ?? {
  success_rate: 0,
  error_rate: 0,
  sla_error_count: 0,
})

const slaColor = computed(() => {
  if (sla.value.success_rate >= 0.99) return '#10b981'
  if (sla.value.success_rate >= 0.95) return '#f59e0b'
  return '#ef4444'
})

// Error data
const errors = computed(() => props.overview?.errors ?? {
  request_errors: 0,
  upstream_errors: 0,
  business_errors: 0,
})

const upstreamErrorRate = computed(() => {
  if (traffic.value.total_requests === 0) return 0
  return (errors.value.upstream_errors / traffic.value.total_requests) * 100
})

const upstreamErrorColor = computed(() => {
  if (upstreamErrorRate.value <= 1) return '#10b981'
  if (upstreamErrorRate.value <= 5) return '#f59e0b'
  return '#ef4444'
})

// Latency data
const latency = computed(() => props.overview?.latency ?? {
  duration_p50_ms: 0,
  duration_p90_ms: 0,
  duration_p95_ms: 0,
  duration_p99_ms: 0,
  duration_avg_ms: 0,
  ttft_p50_ms: 0,
  ttft_p90_ms: 0,
  ttft_p95_ms: 0,
  ttft_p99_ms: 0,
  ttft_avg_ms: 0,
})

// System metrics
const systemMetrics = computed(() => props.overview?.system_metrics ?? {
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
})

// Color calculations for system metrics
const cpuColor = computed(() => {
  if (systemMetrics.value.cpu_usage_percent <= 70) return '#10b981'
  if (systemMetrics.value.cpu_usage_percent <= 90) return '#f59e0b'
  return '#ef4444'
})

const memoryColor = computed(() => {
  if (systemMetrics.value.memory_usage_percent <= 80) return '#10b981'
  if (systemMetrics.value.memory_usage_percent <= 90) return '#f59e0b'
  return '#ef4444'
})

const dbUsagePercent = computed(() => {
  if (systemMetrics.value.db_max_open_conns === 0) return 0
  return (systemMetrics.value.db_active_count / systemMetrics.value.db_max_open_conns) * 100
})

const dbColor = computed(() => {
  if (dbUsagePercent.value <= 70) return '#10b981'
  if (dbUsagePercent.value <= 90) return '#f59e0b'
  return '#ef4444'
})

const redisUsagePercent = computed(() => {
  if (systemMetrics.value.redis_pool_size === 0) return 0
  return (systemMetrics.value.redis_total_conns / systemMetrics.value.redis_pool_size) * 100
})

const redisColor = computed(() => {
  if (redisUsagePercent.value <= 70) return '#10b981'
  if (redisUsagePercent.value <= 90) return '#f59e0b'
  return '#ef4444'
})

const goroutineColor = computed(() => {
  if (systemMetrics.value.goroutine_count <= 1000) return '#10b981'
  if (systemMetrics.value.goroutine_count <= 2000) return '#f59e0b'
  return '#ef4444'
})

// Format number helper
function formatNumber(num: number): string {
  if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + 'M'
  }
  if (num >= 1000) {
    return (num / 1000).toFixed(1) + 'K'
  }
  return num.toString()
}
</script>

<style scoped>
.ops-header-card {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-lg);
  padding: var(--space-6);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.ops-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding-bottom: var(--space-4);
  border-bottom: 1px solid var(--border-light);
}

.ops-toolbar__left {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.ops-toolbar-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-section);
  font-weight: 700;
  color: var(--text-primary);
  margin: 0;
}

.ops-toolbar-status {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}

.ops-status-indicator {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ops-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.ops-status-dot--loading {
  background: var(--text-placeholder);
}

.ops-status-dot--ready {
  background: #10b981;
}

.ops-toolbar-divider {
  color: var(--text-placeholder);
}

.ops-top-section {
  display: flex;
  align-items: center;
  gap: var(--space-8);
}

.ops-health-circle {
  position: relative;
  flex-shrink: 0;
}

.ops-health-svg {
  width: 128px;
  height: 128px;
  transform: rotate(-90deg);
}

.ops-health-progress {
  transition: all 1s ease-in-out;
}

.ops-health-score {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.ops-health-value {
  font-size: 32px;
  font-weight: 700;
  line-height: 1;
}

.ops-health-label {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  margin-top: 4px;
}

.ops-realtime-traffic {
  flex: 1;
}

.ops-realtime-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  margin-bottom: var(--space-2);
}

.ops-realtime-ping {
  position: relative;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #3b82f6;
}

.ops-realtime-ping::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: #3b82f6;
  opacity: 0.75;
  animation: ping 1s cubic-bezier(0, 0, 0.2, 1) infinite;
}

@keyframes ping {
  75%, 100% {
    transform: scale(2);
    opacity: 0;
  }
}

.ops-realtime-label {
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-secondary);
}

.ops-realtime-metrics {
  display: flex;
  align-items: baseline;
  gap: var(--space-4);
}

.ops-realtime-item {
  display: flex;
  align-items: baseline;
  gap: 4px;
}

.ops-realtime-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-primary);
}

.ops-realtime-unit {
  font-size: var(--font-size-sm);
  color: var(--text-secondary);
}

.ops-realtime-peak {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  margin-top: var(--space-2);
}

.ops-metrics-grid {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: var(--space-4);
}

.ops-metric-card {
  background: var(--bg-subtle);
  border-radius: var(--radius-base);
  padding: var(--space-4);
}

.ops-metric-label {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.ops-metric-value {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-primary);
}

.ops-metric-value--danger {
  color: #ef4444;
}

.ops-metric-sub {
  font-size: var(--font-size-xs);
  color: var(--text-placeholder);
  margin-top: 4px;
}

.ops-system-metrics {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: var(--space-4);
}

.ops-system-item {
  background: var(--bg-subtle);
  border-radius: var(--radius-base);
  padding: var(--space-3);
}

.ops-system-label {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}

.ops-system-value {
  font-size: 18px;
  font-weight: 600;
  margin-top: 4px;
}

.ops-system-sub {
  font-size: var(--font-size-xs);
  color: var(--text-placeholder);
  margin-top: 2px;
}

@media (max-width: 1200px) {
  .ops-metrics-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 900px) {
  .ops-top-section {
    flex-direction: column;
    align-items: flex-start;
  }

  .ops-metrics-grid {
    grid-template-columns: repeat(2, 1fr);
  }

  .ops-system-metrics {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 600px) {
  .ops-metrics-grid {
    grid-template-columns: 1fr;
  }

  .ops-system-metrics {
    grid-template-columns: 1fr;
  }
}
</style>
