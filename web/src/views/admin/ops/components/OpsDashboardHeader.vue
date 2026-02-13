<template>
  <div class="ops-header-card">
    <!-- Top Toolbar -->
    <div class="ops-toolbar">
      <div class="ops-toolbar__left">
        <h1 class="ops-toolbar-title">
          <el-icon class="ops-toolbar-icon"><DataBoard /></el-icon>
          运维监控
        </h1>
        <div class="ops-toolbar-status">
          <span class="ops-status-indicator">
            <span
              class="ops-status-dot"
              :class="{
                'ops-status-dot--loading': loading,
                'ops-status-dot--ready': !loading,
                'ops-status-dot--idle': isIdle && !loading,
              }"
            ></span>
            {{ statusLabel }}
          </span>
          <span class="ops-toolbar-divider">|</span>
          <span>自动刷新: {{ autoRefresh ? '开启' : '关闭' }}</span>
          <span v-if="autoRefresh && countdown > 0" class="ops-countdown">
            {{ countdown }}s
          </span>
          <span v-if="lastUpdated" class="ops-toolbar-divider">|</span>
          <span v-if="lastUpdated" class="ops-last-updated">
            最后更新: {{ formatLastUpdated(lastUpdated) }}
          </span>
        </div>
      </div>

      <!-- Filters + Actions -->
      <div class="ops-toolbar__right">
        <div class="ops-filters">
          <el-select
            :model-value="platform"
            placeholder="Platform"
            size="small"
            class="ops-filter-select"
            @update:model-value="onPlatformChange"
          >
            <el-option label="全部平台" value="" />
            <el-option label="OpenAI" value="openai" />
            <el-option label="Anthropic" value="anthropic" />
            <el-option label="Gemini" value="gemini" />
            <el-option label="其他" value="other" />
          </el-select>

          <el-select
            :model-value="groupId"
            placeholder="Group"
            size="small"
            class="ops-filter-select"
            clearable
            :loading="groupsLoading"
            @update:model-value="onGroupChange"
          >
            <el-option
              v-for="g in groupOptions"
              :key="g.id"
              :label="g.name"
              :value="g.id"
            />
          </el-select>

          <el-select
            :model-value="effectiveTimeRange"
            placeholder="Time Range"
            size="small"
            class="ops-time-select"
            @update:model-value="onTimeRangeChange"
          >
            <el-option
              v-for="opt in timeRangeOptions"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            />
          </el-select>
        </div>

        <el-button
          size="small"
          :icon="Refresh"
          :loading="loading"
          @click="emit('refresh')"
        />
        <el-button
          size="small"
          plain
          @click="emit(fullscreen ? 'exit-fullscreen' : 'enter-fullscreen')"
        >
          {{ fullscreen ? '退出全屏' : '全屏' }}
        </el-button>
      </div>
    </div>

    <!-- Top Section: Health Score + Realtime Traffic -->
    <div class="ops-top-section">
      <!-- Health Score Circle with Popover -->
      <el-popover
        trigger="hover"
        placement="right"
        :width="320"
        popper-class="ops-diagnosis-popover"
      >
        <template #reference>
          <div
            class="ops-health-circle"
            :class="{ 'ops-health-circle--idle': isIdle }"
          >
            <svg class="ops-health-svg">
              <circle
                cx="64"
                cy="64"
                r="56"
                :stroke="circleBgColor"
                stroke-width="8"
                fill="none"
              />
              <circle
                cx="64"
                cy="64"
                r="56"
                :stroke="isIdle ? HEALTH_COLORS.IDLE : healthColor"
                stroke-width="8"
                fill="none"
                :stroke-dasharray="circumference"
                :stroke-dashoffset="dashOffset"
                stroke-linecap="round"
                class="ops-health-progress"
              />
            </svg>
            <div class="ops-health-score">
              <span v-if="isIdle" class="ops-health-idle-label">空闲</span>
              <template v-else>
                <span class="ops-health-value" :style="{ color: healthColor }">{{ healthScore }}</span>
                <span class="ops-health-label">健康评分</span>
              </template>
            </div>
          </div>
        </template>

        <!-- Diagnosis Content -->
        <div class="ops-diagnosis">
          <div class="ops-diagnosis-title">健康诊断报告</div>
          <div class="ops-diagnosis-list">
            <div v-for="item in diagnosisItems" :key="item.name" class="ops-diagnosis-row">
              <span class="ops-diagnosis-name">{{ item.name }}</span>
              <span class="ops-diagnosis-val">{{ item.value }}</span>
              <span class="ops-diagnosis-threshold">/ {{ item.threshold }}</span>
              <span
                class="ops-diagnosis-badge"
                :class="`ops-diagnosis-badge--${item.status}`"
              >
                {{ item.statusText }}
              </span>
            </div>
          </div>
        </div>
      </el-popover>

      <!-- Realtime Traffic -->
      <div class="ops-realtime-traffic">
        <div class="ops-realtime-header">
          <span class="ops-realtime-ping" :class="{ paused: isIdle }"></span>
          <span class="ops-realtime-label">实时流量</span>
          <span v-if="isIdle" class="ops-realtime-idle-tag">空闲中</span>
          <span v-else-if="realtimeWindow" class="ops-realtime-window-tag">
            {{ realtimeWindow }}
          </span>
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
        <!-- Realtime window button group -->
        <div class="ops-realtime-windows">
          <button
            v-for="w in realtimeWindowOptions"
            :key="w.value"
            class="ops-window-btn"
            :class="{ 'is-active': realtimeWindow === w.value }"
            :disabled="loadingRealtime"
            @click="onRealtimeWindowClick(w.value)"
          >
            {{ w.label }}
          </button>
          <el-button
            size="small"
            plain
            :loading="loadingRealtime"
            class="ops-window-refresh"
            @click="refreshRealtime"
          >
            刷新
          </el-button>
        </div>
      </div>
    </div>

    <!-- Metrics Cards Grid -->
    <div class="ops-metrics-grid">
      <!-- 1. 请求总量 -->
      <div class="ops-metric-card">
        <div class="ops-metric-label ops-section-label">
          <el-tooltip content="选中时段内所有请求的总数量和消耗的总 Tokens" placement="top">
            <span>请求总量</span>
          </el-tooltip>
        </div>
        <div class="ops-metric-value">{{ formatNumber(traffic.total_requests) }}</div>
        <div class="ops-metric-sub">
          Tokens: {{ formatNumber(traffic.total_tokens) }}
        </div>
        <div class="ops-metric-action">
          <el-button link size="small" @click="emit('open-request-details')">钻取</el-button>
        </div>
      </div>

      <!-- 2. 成功率 (SLA) -->
      <div class="ops-metric-card">
        <div class="ops-metric-label ops-section-label">
          <el-tooltip content="SLA 成功率，计算口径为扣除 SLA 错误数后的请求成功率" placement="top">
            <span>成功率 (SLA)</span>
          </el-tooltip>
        </div>
        <div class="ops-metric-value" :style="{ color: slaColor }">
          {{ (sla.success_rate * 100).toFixed(2) }}%
        </div>
        <div class="ops-metric-sub">
          失败: {{ sla.sla_error_count }}
        </div>
        <el-progress
          :percentage="Math.min(sla.success_rate * 100, 100)"
          :show-text="false"
          :stroke-width="4"
          :color="slaColor"
          class="ops-sla-bar"
        />
      </div>

      <!-- 3. 错误统计 -->
      <div class="ops-metric-card">
        <div class="ops-metric-label ops-section-label">
          <el-tooltip content="请求错误 (4xx 客户端) 和上游错误 (5xx 网关/上游服务) 分类统计" placement="top">
            <span>错误统计</span>
          </el-tooltip>
        </div>
        <div class="ops-metric-value ops-metric-value--danger">
          {{ errors.request_errors + errors.upstream_errors }}
        </div>
        <div class="ops-metric-sub-detail">
          <span>4xx: {{ errors.request_errors }}</span>
          <span>5xx: {{ errors.upstream_errors }}</span>
          <span class="ops-metric-sub-hint">excl 429/529: {{ errors.upstream_errors_excl_429_529 }}</span>
        </div>
        <div class="ops-metric-action">
          <el-button link size="small" @click="emit('open-error-details', 'request')">钻取</el-button>
        </div>
      </div>

      <!-- 4. 延迟 P99 -->
      <div class="ops-metric-card">
        <div class="ops-metric-label ops-section-label">
          <el-tooltip content="端到端请求延迟的 P99 百分位值，含网关处理 + 上游调用完整链路" placement="top">
            <span>延迟 P99</span>
          </el-tooltip>
        </div>
        <div class="ops-metric-value">{{ latency.duration_p99_ms }}ms</div>
        <div class="ops-latency-grid">
          <div class="ops-latency-cell"><span class="ops-latency-k">P95</span><span class="ops-latency-v">{{ latency.duration_p95_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">P90</span><span class="ops-latency-v">{{ latency.duration_p90_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">P50</span><span class="ops-latency-v">{{ latency.duration_p50_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">Avg</span><span class="ops-latency-v">{{ latency.duration_avg_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">Max</span><span class="ops-latency-v">{{ latency.duration_max_ms }}</span></div>
        </div>
      </div>

      <!-- 5. 首字延迟 P99 (TTFT) -->
      <div class="ops-metric-card">
        <div class="ops-metric-label ops-section-label">
          <el-tooltip content="首 Token 延迟 (Time To First Token) 的 P99 百分位值，衡量流式响应首字节速度" placement="top">
            <span>首字延迟 P99</span>
          </el-tooltip>
        </div>
        <div class="ops-metric-value">{{ latency.ttft_p99_ms }}ms</div>
        <div class="ops-latency-grid">
          <div class="ops-latency-cell"><span class="ops-latency-k">P95</span><span class="ops-latency-v">{{ latency.ttft_p95_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">P90</span><span class="ops-latency-v">{{ latency.ttft_p90_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">P50</span><span class="ops-latency-v">{{ latency.ttft_p50_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">Avg</span><span class="ops-latency-v">{{ latency.ttft_avg_ms }}</span></div>
          <div class="ops-latency-cell"><span class="ops-latency-k">Max</span><span class="ops-latency-v">{{ latency.ttft_max_ms }}</span></div>
        </div>
      </div>

      <!-- 6. 上游错误率 -->
      <div class="ops-metric-card">
        <div class="ops-metric-label ops-section-label">
          <el-tooltip content="上游服务端错误占比，含 5xx 返回；excl 429/529 为扣除限流后的真实错误量" placement="top">
            <span>上游错误率</span>
          </el-tooltip>
        </div>
        <div class="ops-metric-value" :style="{ color: upstreamErrorColor }">
          {{ upstreamErrorRate.toFixed(2) }}%
        </div>
        <div class="ops-metric-sub-detail">
          <span>excl 429/529: {{ errors.upstream_errors_excl_429_529 }}</span>
          <span class="ops-metric-sub-hint">限流 429/529: {{ errors.rate_limit_429_529 }}</span>
        </div>
        <div class="ops-metric-action">
          <el-button link size="small" @click="emit('open-error-details', 'upstream')">钻取</el-button>
        </div>
      </div>
    </div>

    <!-- System Metrics Bar -->
    <div class="ops-system-metrics">
      <div class="ops-system-item">
        <div class="ops-system-label ops-section-label">
          <el-tooltip content="当前进程 CPU 使用率，基于 Go runtime 采样" placement="top">
            <span>CPU</span>
          </el-tooltip>
        </div>
        <div class="ops-system-value" :style="{ color: cpuColor }">
          {{ systemMetrics.cpu_usage_percent.toFixed(1) }}%
        </div>
      </div>

      <div class="ops-system-item">
        <div class="ops-system-label ops-section-label">
          <el-tooltip content="进程 RSS 内存使用，包含 HeapAlloc + 栈 + 系统占用" placement="top">
            <span>内存</span>
          </el-tooltip>
        </div>
        <div class="ops-system-value" :style="{ color: memoryColor }">
          {{ systemMetrics.memory_usage_percent.toFixed(1) }}%
        </div>
        <div class="ops-system-sub">
          {{ systemMetrics.memory_used_mb.toFixed(0) }} / {{ systemMetrics.memory_total_mb.toFixed(0) }} MB
        </div>
      </div>

      <div class="ops-system-item">
        <div class="ops-system-label ops-section-label">
          <el-tooltip content="PostgreSQL 连接池活跃连接占比，接近上限时可能出现请求等待" placement="top">
            <span>DB 连接</span>
          </el-tooltip>
        </div>
        <div class="ops-system-value" :style="{ color: dbColor }">
          {{ dbUsagePercent.toFixed(0) }}%
        </div>
        <div class="ops-system-sub">
          {{ systemMetrics.db_active_count }} / {{ systemMetrics.db_max_open_conns }}
        </div>
      </div>

      <div class="ops-system-item">
        <div class="ops-system-label ops-section-label">
          <el-tooltip content="Redis 连接池使用率，过高可能导致获取连接超时" placement="top">
            <span>Redis 连接</span>
          </el-tooltip>
        </div>
        <div class="ops-system-value" :style="{ color: redisColor }">
          {{ redisUsagePercent.toFixed(0) }}%
        </div>
        <div class="ops-system-sub">
          {{ systemMetrics.redis_total_conns }} / {{ systemMetrics.redis_pool_size }}
        </div>
      </div>

      <div class="ops-system-item">
        <div class="ops-system-label ops-section-label">
          <el-tooltip content="当前 Goroutine 数量，持续上涨可能存在泄漏风险" placement="top">
            <span>Goroutine</span>
          </el-tooltip>
        </div>
        <div class="ops-system-value" :style="{ color: goroutineColor }">
          {{ systemMetrics.goroutine_count }}
        </div>
      </div>

      <div class="ops-system-item">
        <div class="ops-system-label ops-section-label">
          <el-tooltip content="后台定时任务状态，running 为正常运行，failed/errored 需要关注" placement="top">
            <span>Jobs</span>
          </el-tooltip>
        </div>
        <div class="ops-system-value" :style="{ color: jobsColor }">
          {{ jobsSummary.count }}
        </div>
        <div class="ops-system-sub">{{ jobsSummary.statusText }}</div>
      </div>
    </div>

    <!-- Custom Time Range Dialog -->
    <el-dialog
      v-model="customDialogVisible"
      title="自定义时间范围"
      width="480px"
      :close-on-click-modal="false"
      class="ops-custom-time-dialog"
    >
      <div class="ops-custom-time-body">
        <div class="ops-custom-time-row">
          <label>开始时间</label>
          <el-date-picker
            v-model="customStart"
            type="datetime"
            placeholder="选择开始时间"
            format="YYYY-MM-DD HH:mm"
            value-format="YYYY-MM-DD HH:mm"
            class="ops-custom-time-picker"
          />
        </div>
        <div class="ops-custom-time-row">
          <label>结束时间</label>
          <el-date-picker
            v-model="customEnd"
            type="datetime"
            placeholder="选择结束时间"
            format="YYYY-MM-DD HH:mm"
            value-format="YYYY-MM-DD HH:mm"
            class="ops-custom-time-picker"
          />
        </div>
      </div>
      <template #footer>
        <el-button @click="customDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmCustomTimeRange">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, watch } from 'vue'
import {
  ElIcon,
  ElButton,
  ElTooltip,
  ElProgress,
  ElPopover,
  ElSelect,
  ElOption,
  ElDialog,
  ElDatePicker,
} from 'element-plus'
import { DataBoard, Refresh } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'
import type { OpsOverview, OpsJobHeartbeat, OpsGroupOption } from '@/api/ops'
import {
  getHealthScoreColor,
  getThresholdColor,
  getThresholdColorInverse,
  HEALTH_COLORS,
} from '../utils/opsFormatters'

interface Props {
  overview: OpsOverview | null
  platform: string
  groupId: number | null
  timeRange: string
  loading: boolean
  lastUpdated: Date | null
  autoRefresh: boolean
  autoRefreshCountdown: number
  fullscreen: boolean
  customStartTime: string | null
  customEndTime: string | null
}

const props = withDefaults(defineProps<Props>(), {
  overview: null,
  platform: '',
  groupId: null,
  timeRange: '1h',
  loading: false,
  lastUpdated: null,
  autoRefresh: false,
  autoRefreshCountdown: 0,
  fullscreen: false,
  customStartTime: null,
  customEndTime: null,
})

const emit = defineEmits<{
  (e: 'update:time-range', value: string): void
  (e: 'update:platform', value: string): void
  (e: 'update:group', value: number | null): void
  (e: 'update:custom-time-range', value: { start: string; end: string }): void
  (e: 'refresh'): void
  (e: 'open-request-details'): void
  (e: 'open-error-details', type: 'request' | 'upstream'): void
  (e: 'open-settings'): void
  (e: 'open-alert-rules'): void
  (e: 'enter-fullscreen'): void
  (e: 'exit-fullscreen'): void
}>()

// ============ Filter State & Options ============

const groupOptions = ref<OpsGroupOption[]>([])
const groupsLoading = ref(false)

const loadGroupOptions = async () => {
  groupsLoading.value = true
  try {
    const list = await opsAPI.getGroupOptions()
    groupOptions.value = list ?? []
  } catch (err) {
    console.error('Failed to load group options:', err)
    groupOptions.value = []
  } finally {
    groupsLoading.value = false
  }
}

onMounted(loadGroupOptions)

const onPlatformChange = (value: string) => {
  emit('update:platform', value)
  // 平台切换时清空 groupId（由父组件响应 update:platform 后更新 groupId）
  if (props.groupId !== null) {
    emit('update:group', null)
  }
}

const onGroupChange = (value: number | null) => {
  emit('update:group', value ?? null)
}

// ============ Time Range ============

const timeRangeOptions = [
  { label: '最近 5 分钟', value: '5m' },
  { label: '最近 30 分钟', value: '30m' },
  { label: '最近 1 小时', value: '1h' },
  { label: '最近 6 小时', value: '6h' },
  { label: '最近 24 小时', value: '24h' },
  { label: '自定义', value: 'custom' },
]

const customDialogVisible = ref(false)
const customStart = ref<string>('')
const customEnd = ref<string>('')

// 当前显示用的 timeRange（custom 状态下保持 'custom' 选中）
const effectiveTimeRange = computed(() => {
  if (props.customStartTime && props.customEndTime) return 'custom'
  return props.timeRange
})

const onTimeRangeChange = (value: string) => {
  if (value === 'custom') {
    customStart.value = props.customStartTime || ''
    customEnd.value = props.customEndTime || ''
    customDialogVisible.value = true
  } else {
    emit('update:time-range', value)
    // 选择非 custom 选项时清空自定义时间
    if (props.customStartTime || props.customEndTime) {
      emit('update:custom-time-range', { start: '', end: '' })
    }
  }
}

const confirmCustomTimeRange = () => {
  if (!customStart.value || !customEnd.value) {
    return
  }
  // 转换为 ISO 字符串
  const startISO = new Date(customStart.value).toISOString()
  const endISO = new Date(customEnd.value).toISOString()
  emit('update:custom-time-range', { start: startISO, end: endISO })
  customDialogVisible.value = false
}

// ============ Realtime Traffic Window ============

const realtimeWindowOptions = [
  { label: '1min', value: '1min' },
  { label: '5min', value: '5min' },
  { label: '30min', value: '30min' },
  { label: '1h', value: '1h' },
]
const realtimeWindow = ref<string>('1min')
const loadingRealtime = ref(false)
const realtimeQPS = ref<number | null>(null)
const realtimeTPS = ref<number | null>(null)

const onRealtimeWindowClick = async (w: string) => {
  realtimeWindow.value = w
  await fetchRealtimeSummary(w)
}

const refreshRealtime = async () => {
  if (realtimeWindow.value) {
    await fetchRealtimeSummary(realtimeWindow.value)
  }
}

const fetchRealtimeSummary = async (window: string) => {
  loadingRealtime.value = true
  try {
    const resp = await opsAPI.getRealtimeTrafficSummary(window)
    realtimeQPS.value = resp.current_qps
    realtimeTPS.value = resp.current_tps
  } catch (err) {
    console.error('Failed to fetch realtime traffic summary:', err)
  } finally {
    loadingRealtime.value = false
  }
}

// 当 overview 变化时，重置 realtime 覆盖数据
watch(
  () => props.overview,
  () => {
    realtimeQPS.value = null
    realtimeTPS.value = null
  },
)

// ============ Status & Display ============

const isIdle = computed(() => {
  if (!props.overview) return true
  return props.overview.traffic.current_qps === 0 && props.overview.sla.error_rate === 0
})

const statusLabel = computed(() => {
  if (props.loading) return '加载中...'
  if (isIdle.value) return '空闲'
  return '就绪'
})

const countdown = computed(() => props.autoRefreshCountdown)

const formatLastUpdated = (d: Date): string => {
  return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}:${d.getSeconds().toString().padStart(2, '0')}`
}

// ============ Health Score ============

const healthScore = computed(() => props.overview?.health_score ?? 0)

const healthColor = computed(() => getHealthScoreColor(healthScore.value, isIdle.value))

const circleBgColor = computed(() =>
  document.documentElement.classList.contains('dark') ? '#334155' : '#e5e7eb',
)

const circumference = 2 * Math.PI * 56
const dashOffset = computed(() => {
  const percent = healthScore.value / 100
  return circumference * (1 - percent)
})

// ============ Diagnosis ============

interface DiagnosisItem {
  name: string
  value: string
  threshold: string
  status: 'ok' | 'warning' | 'critical'
  statusText: string
}

const emptySystemMetrics = {
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
}

const systemMetrics = computed(() => props.overview?.system_metrics ?? emptySystemMetrics)

const sla = computed(
  () =>
    props.overview?.sla ?? {
      success_rate: 0,
      error_rate: 0,
      sla_error_count: 0,
    },
)

const latency = computed(
  () =>
    props.overview?.latency ?? {
      duration_p50_ms: 0,
      duration_p90_ms: 0,
      duration_p95_ms: 0,
      duration_p99_ms: 0,
      duration_avg_ms: 0,
      duration_max_ms: 0,
      ttft_p50_ms: 0,
      ttft_p90_ms: 0,
      ttft_p95_ms: 0,
      ttft_p99_ms: 0,
      ttft_avg_ms: 0,
      ttft_max_ms: 0,
    },
)

const dbUsagePercent = computed(() =>
  systemMetrics.value.db_max_open_conns === 0
    ? 0
    : (systemMetrics.value.db_active_count / systemMetrics.value.db_max_open_conns) * 100,
)

const redisUsagePercent = computed(() =>
  systemMetrics.value.redis_pool_size === 0
    ? 0
    : (systemMetrics.value.redis_total_conns / systemMetrics.value.redis_pool_size) * 100,
)

const statusOf = <T extends number>(val: T, warn: T, crit: T): 'ok' | 'warning' | 'critical' => {
  if (val >= crit) return 'critical'
  if (val >= warn) return 'warning'
  return 'ok'
}

const statusLabelMap = { ok: '正常', warning: '告警', critical: '危险' } as const

const diagnosisItems = computed<DiagnosisItem[]>(() => {
  const sm = systemMetrics.value
  const l = latency.value
  const s = sla.value
  const items: Array<{
    name: string
    value: string
    threshold: string
    raw: number
    warn: number
    crit: number
  }> = [
    {
      name: 'DB 连接池',
      value: `${dbUsagePercent.value.toFixed(0)}%`,
      threshold: '70% / 90%',
      raw: dbUsagePercent.value,
      warn: 70,
      crit: 90,
    },
    {
      name: 'Redis 连接池',
      value: `${redisUsagePercent.value.toFixed(0)}%`,
      threshold: '70% / 90%',
      raw: redisUsagePercent.value,
      warn: 70,
      crit: 90,
    },
    {
      name: 'CPU',
      value: `${sm.cpu_usage_percent.toFixed(1)}%`,
      threshold: '70% / 90%',
      raw: sm.cpu_usage_percent,
      warn: 70,
      crit: 90,
    },
    {
      name: '内存',
      value: `${sm.memory_usage_percent.toFixed(1)}%`,
      threshold: '80% / 90%',
      raw: sm.memory_usage_percent,
      warn: 80,
      crit: 90,
    },
    {
      name: 'TTFT P99',
      value: `${l.ttft_p99_ms}ms`,
      threshold: '3000ms / 5000ms',
      raw: l.ttft_p99_ms,
      warn: 3000,
      crit: 5000,
    },
    {
      name: 'SLA',
      value: `${(s.success_rate * 100).toFixed(2)}%`,
      threshold: '95% / 99%',
      raw: s.success_rate * 100,
      warn: 95,
      crit: 99,
    },
    {
      name: 'Goroutine',
      value: `${sm.goroutine_count}`,
      threshold: '1000 / 2000',
      raw: sm.goroutine_count,
      warn: 1000,
      crit: 2000,
    },
  ]
  return items.map((it) => {
    // SLA 反向：越高越好
    if (it.name === 'SLA') {
      const status: 'ok' | 'warning' | 'critical' =
        it.raw >= 99 ? 'ok' : it.raw >= 95 ? 'warning' : 'critical'
      return { ...it, status, statusText: statusLabelMap[status] }
    }
    const status = statusOf(it.raw, it.warn, it.crit)
    return { ...it, status, statusText: statusLabelMap[status] }
  })
})

// ============ Traffic Data ============

const emptyTraffic = {
  total_requests: 0,
  total_tokens: 0,
  avg_qps: 0,
  avg_tps: 0,
  current_qps: 0,
  current_tps: 0,
  peak_qps: 0,
  peak_tps: 0,
}

const traffic = computed(() => props.overview?.traffic ?? emptyTraffic)

const currentQPS = computed(() => realtimeQPS.value ?? traffic.value.current_qps)
const currentTPS = computed(() => realtimeTPS.value ?? traffic.value.current_tps)
const peakQPS = computed(() => traffic.value.peak_qps)
const peakTPS = computed(() => traffic.value.peak_tps)

// ============ Errors ============

const emptyErrors = {
  request_errors: 0,
  upstream_errors: 0,
  business_errors: 0,
  upstream_errors_excl_429_529: 0,
  rate_limit_429_529: 0,
}

const errors = computed(() => props.overview?.errors ?? emptyErrors)

const upstreamErrorRate = computed(() =>
  traffic.value.total_requests === 0
    ? 0
    : (errors.value.upstream_errors / traffic.value.total_requests) * 100,
)

const upstreamErrorColor = computed(() =>
  getThresholdColorInverse(upstreamErrorRate.value, 1, 5),
)

// ============ SLA / System Colors ============
// 阈值染色统一使用 opsFormatters 的 helper，避免各处重复定义阈值/颜色。

const slaColor = computed(() =>
  getThresholdColorInverse(sla.value.success_rate * 100, 95, 99),
)

const cpuColor = computed(() =>
  getThresholdColor(systemMetrics.value.cpu_usage_percent, 70, 90),
)

const memoryColor = computed(() =>
  getThresholdColor(systemMetrics.value.memory_usage_percent, 80, 90),
)

const dbColor = computed(() => getThresholdColor(dbUsagePercent.value, 70, 90))

const redisColor = computed(() => getThresholdColor(redisUsagePercent.value, 70, 90))

const goroutineColor = computed(() =>
  getThresholdColor(systemMetrics.value.goroutine_count, 1000, 2000),
)

// ============ Jobs ============

const jobsSummary = computed(() => {
  const jobs: OpsJobHeartbeat[] = props.overview?.jobs ?? []
  const failed = jobs.filter((j) => j.status === 'failed' || j.status === 'error').length
  const running = jobs.filter((j) => j.status === 'running').length
  if (failed > 0)
    return { count: jobs.length, statusText: `${failed} failed`, color: HEALTH_COLORS.CRITICAL }
  if (running > 0)
    return { count: jobs.length, statusText: `${running} running`, color: HEALTH_COLORS.OK }
  return {
    count: jobs.length,
    statusText: jobs.length ? `${jobs.length} 个` : '无数据',
    color: HEALTH_COLORS.IDLE,
  }
})

const jobsColor = computed(() => jobsSummary.value.color)

// ============ Utils ============

const formatNumber = (num: number): string => {
  if (num >= 1_000_000) return (num / 1_000_000).toFixed(1) + 'M'
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K'
  return num.toString()
}
</script>

<style scoped>
/* ============ 容器对齐 sub2api ============ */
.ops-header-card {
  background: var(--bg-card);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-3xl);
  padding: var(--space-6);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  transition: box-shadow 0.2s ease;
}

/* ============ 工具栏 ============ */
.ops-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  padding-bottom: var(--space-4);
  border-bottom: 1px solid var(--border-light);
  flex-wrap: wrap;
}

.ops-toolbar__left {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.ops-toolbar-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-section);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
  margin: 0;
}

/* 工具栏左侧图标（强调蓝） */
.ops-toolbar-icon {
  color: var(--accent-light);
}

.ops-toolbar-status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  flex-wrap: wrap;
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
  display: inline-block;
}
.ops-status-dot--loading {
  background: var(--text-placeholder);
}
.ops-status-dot--ready {
  background: var(--health-ok);
  box-shadow: 0 0 0 3px rgba(16, 185, 129, 0.15);
}
.ops-status-dot--idle {
  background: var(--health-idle);
}

.ops-toolbar-divider {
  color: var(--text-placeholder);
}

.ops-countdown {
  color: var(--accent-light);
  font-weight: 500;
}

.ops-last-updated {
  color: var(--text-secondary);
}

/* ============ Filters / Actions ============ */
.ops-toolbar__right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.ops-filters {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.ops-filter-select {
  width: 140px;
}

.ops-time-select {
  width: 130px;
}

/* 按钮统一圆角 8px（对齐 sub2api rounded-lg） */
.ops-toolbar__right :deep(.el-button) {
  border-radius: 8px;
}

/* ============ 健康圆环（sub2api 风格，添加细腻动画） ============ */
.ops-top-section {
  display: flex;
  align-items: center;
  gap: var(--space-8);
}

.ops-health-circle {
  position: relative;
  flex-shrink: 0;
  cursor: pointer;
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1),
    filter 0.3s ease;
  filter: drop-shadow(0 2px 8px rgba(0, 0, 0, 0.08));
}

.ops-health-circle:hover {
  transform: scale(1.06);
  filter: drop-shadow(0 4px 14px rgba(59, 130, 246, 0.25));
}

.ops-health-circle--idle {
  opacity: 0.7;
}

.ops-health-svg {
  width: 128px;
  height: 128px;
  transform: rotate(-90deg);
}

.ops-health-progress {
  transition: stroke-dashoffset 1s cubic-bezier(0.4, 0, 0.2, 1),
    stroke 0.6s ease;
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
  font-weight: var(--font-weight-black);
  line-height: 1;
  transition: color 0.6s ease;
}

.ops-health-label {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  margin-top: 4px;
}

.ops-health-idle-label {
  font-size: var(--font-size-sm);
  color: var(--health-idle);
  font-weight: 600;
}

/* ============ 实时流量 ============ */
.ops-realtime-traffic {
  flex: 1;
  min-width: 0;
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
  background: var(--accent-light);
}

.ops-realtime-ping::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: var(--accent-light);
  opacity: 0.75;
  animation: ping 1s cubic-bezier(0, 0, 0.2, 1) infinite;
}

.ops-realtime-ping.paused {
  background: var(--health-idle);
}
.ops-realtime-ping.paused::before {
  background: var(--health-idle);
  animation: none;
}

@keyframes ping {
  75%,
  100% {
    transform: scale(2);
    opacity: 0;
  }
}

.ops-realtime-label {
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-secondary);
}

.ops-realtime-idle-tag,
.ops-realtime-window-tag {
  font-size: var(--font-size-xs);
  color: var(--health-idle);
  background: var(--bg-subtle);
  padding: 1px 8px;
  border-radius: 10px;
  line-height: 1.5;
}
.ops-realtime-window-tag {
  color: var(--accent-light);
  background: rgba(59, 130, 246, 0.1);
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
  font-weight: var(--font-weight-black);
  color: var(--text-primary);
  transition: color 0.3s ease;
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

/* 实时流量窗口按钮组 */
.ops-realtime-windows {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 12px;
}

.ops-window-btn {
  height: 28px;
  padding: 0 12px;
  font-size: 12px;
  font-weight: 500;
  background: var(--bg-subtle);
  color: var(--text-secondary);
  border: 1px solid var(--border-default);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.2s ease;
  line-height: 1;
  user-select: none;
}
.ops-window-btn:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ops-window-btn:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
.ops-window-btn.is-active {
  background: var(--accent-light);
  color: #ffffff;
  border-color: var(--accent-light);
}
.ops-window-btn.is-active:hover:not(:disabled) {
  background: var(--accent);
  color: #ffffff;
}
.ops-window-refresh {
  margin-left: auto;
  border-radius: 8px;
}

/* ============ 指标卡网格 ============ */
.ops-metrics-grid {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: var(--space-4);
}

.ops-metric-card {
  background: var(--bg-subtle);
  border-radius: 12px;
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: 4px;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}
.ops-metric-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
}

.ops-metric-label {
  font-size: var(--font-size-xxs);
  font-weight: var(--font-weight-bold);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  gap: 2px;
}

.ops-metric-value {
  font-size: 24px;
  font-weight: var(--font-weight-black);
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.ops-metric-value--danger {
  color: var(--health-critical);
}

.ops-metric-sub {
  font-size: var(--font-size-xs);
  color: var(--text-placeholder);
}

.ops-metric-sub-detail {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}
.ops-metric-sub-detail span {
  white-space: nowrap;
}
.ops-metric-sub-hint {
  color: var(--text-placeholder) !important;
}

.ops-metric-action {
  display: flex;
  justify-content: flex-end;
  margin-top: 2px;
}

/* SLA 进度条（4px 高度，圆角） */
.ops-sla-bar {
  margin-top: 4px;
}
:deep(.ops-sla-bar .el-progress-bar__outer) {
  background-color: var(--border-light);
  border-radius: 2px;
}
:deep(.ops-sla-bar .el-progress-bar__inner) {
  border-radius: 2px;
}

/* 延迟 mini grid */
.ops-latency-grid {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 2px;
  margin-top: 4px;
}

.ops-latency-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 2px;
  border-radius: 4px;
  background: var(--bg-surface);
}

.ops-latency-k {
  font-size: var(--font-size-xxs);
  color: var(--text-placeholder);
  line-height: 1;
}

.ops-latency-v {
  font-size: var(--font-size-xxs);
  color: var(--text-secondary);
  font-weight: var(--font-weight-semibold);
  line-height: 1.2;
}

/* ============ 系统指标条 ============ */
.ops-system-metrics {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: var(--space-4);
}

.ops-system-item {
  background: var(--bg-subtle);
  border-radius: 8px;
  padding: var(--space-3);
  transition: background 0.3s ease;
}
.ops-system-item:hover {
  background: var(--bg-hover);
}

.ops-system-label {
  font-size: var(--font-size-xxs);
  font-weight: var(--font-weight-bold);
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-secondary);
}

.ops-system-value {
  font-size: 18px;
  font-weight: var(--font-weight-semibold);
  margin-top: 4px;
  transition: color 0.4s ease;
}

.ops-system-sub {
  font-size: var(--font-size-xs);
  color: var(--text-placeholder);
  margin-top: 2px;
}

/* ============ Custom Time Range Dialog ============ */
:deep(.ops-custom-time-dialog .el-dialog__body) {
  padding-top: 20px;
}

.ops-custom-time-body {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.ops-custom-time-row {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ops-custom-time-row label {
  font-size: 13px;
  color: var(--text-secondary);
  font-weight: 500;
}

.ops-custom-time-picker {
  width: 100%;
}

/* 状态徽章（el-tag 覆盖样式） */
:deep(.el-tag) {
  border-radius: 10px;
  padding: 0 10px;
  height: 22px;
  line-height: 22px;
  font-size: 12px;
  border: none;
}

/* ============ 响应式 ============ */
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
    grid-template-columns: repeat(3, 1fr);
  }
  .ops-filters {
    flex-wrap: wrap;
  }
}
@media (max-width: 600px) {
  .ops-metrics-grid {
    grid-template-columns: 1fr;
  }
  .ops-system-metrics {
    grid-template-columns: repeat(2, 1fr);
  }
  .ops-header-card {
    padding: var(--space-4);
    border-radius: var(--radius-xl);
  }
  .ops-filter-select,
  .ops-time-select {
    width: 120px;
  }
}
</style>

<style>
/* 全局（非 scoped）:popover 及 dialog 样式 */
.ops-diagnosis-popover {
  padding: 16px !important;
  border-radius: 12px !important;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
}

/* 自定义时间范围 dialog（Element Plus render to body） */
.ops-custom-time-dialog .el-dialog {
  border-radius: 12px;
}

/* 诊断 popover 内部样式 */
.ops-diagnosis {
  font-size: 13px;
}
.ops-diagnosis-title {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 12px;
  color: var(--text-primary, #1f2937);
}
.ops-diagnosis-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ops-diagnosis-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
  border-bottom: 1px dashed var(--border-light, #e5e7eb);
}
.ops-diagnosis-row:last-child {
  border-bottom: none;
}
.ops-diagnosis-name {
  color: var(--text-secondary, #6b7280);
  min-width: 80px;
}
.ops-diagnosis-val {
  color: var(--text-primary, #1f2937);
  font-weight: 600;
  min-width: 60px;
}
.ops-diagnosis-threshold {
  color: var(--text-placeholder, #9ca3af);
  font-size: 12px;
  flex: 1;
}
.ops-diagnosis-badge {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.5;
}
.ops-diagnosis-badge--ok {
  background: rgba(5, 150, 105, 0.12);
  color: var(--health-ok);
}
.ops-diagnosis-badge--warning {
  background: rgba(217, 119, 6, 0.12);
  color: var(--health-warn);
}
.ops-diagnosis-badge--critical {
  background: rgba(220, 38, 38, 0.12);
  color: var(--health-critical);
}
</style>
