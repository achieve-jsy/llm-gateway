<template>
  <div class="ud-section ud-panel">
    <!-- Section A: Header -->
    <div class="ud-panel__header">
      <div class="ud-panel__user-info">
        <el-icon class="ud-panel__user-icon" :size="28"><User /></el-icon>
        <div class="ud-panel__user-meta">
          <span class="ud-panel__user-name">{{ userName }}</span>
          <span v-if="deptName" class="ud-panel__dept-name">{{ deptName }}</span>
        </div>
      </div>
      <el-button
        class="ud-panel__close-btn"
        :icon="Close"
        circle
        size="small"
        text
        @click="$emit('close')"
      />
    </div>

    <!-- Section B: KPI Cards Row -->
    <div class="ud-panel__kpi-row" v-if="detail">
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">总成本</span>
        <span class="ud-kpi-value ud-kpi-cost">{{ fmtCost(detail.summary.est_cost_usd) }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">调用次数</span>
        <span class="ud-kpi-value">{{ detail.summary.total_calls.toLocaleString() }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">成功率</span>
        <span
          class="ud-kpi-value"
          :class="successRateClass"
        >{{ successRate }}%</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">Token 消耗</span>
        <span class="ud-kpi-value">{{ fmtTokens(detail.summary.total_tokens) }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">模型数</span>
        <span class="ud-kpi-value">{{ detail.summary.model_count }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">错误率</span>
        <span
          class="ud-kpi-value"
          :class="errorRateClass"
        >{{ detail.summary.error_rate.toFixed(1) }}%</span>
      </div>
    </div>

    <!-- Section B2: Efficiency Metrics Row -->
    <div class="ud-panel__eff-row" v-if="detail">
      <div class="ud-eff-item">
        <span class="ud-eff-label">平均响应</span>
        <span class="ud-eff-value" :class="avgDurationClass">
          {{ fmtDuration(detail.summary.avg_duration_ms) }}
        </span>
      </div>
      <div class="ud-eff-divider"></div>
      <div class="ud-eff-item">
        <span class="ud-eff-label">每次调用成本</span>
        <span class="ud-eff-value">{{ fmtCostPerCall }}</span>
      </div>
      <div class="ud-eff-divider"></div>
      <div class="ud-eff-item">
        <span class="ud-eff-label">成功 / 失败</span>
        <span class="ud-eff-value">
          <span class="ud-kpi--green">{{ detail.summary.success_count.toLocaleString() }}</span>
          <span class="ud-eff-sep">/</span>
          <span :class="detail.summary.total_calls - detail.summary.success_count > 0 ? 'ud-kpi--red' : ''">
            {{ (detail.summary.total_calls - detail.summary.success_count).toLocaleString() }}
          </span>
        </span>
      </div>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="ud-panel__loading">
      <el-icon class="is-loading" :size="24"><Loading /></el-icon>
      <span>加载中...</span>
    </div>

    <!-- No data state -->
    <div v-if="!loading && !detail" class="ud-panel__empty">
      <span>暂无用户详情数据</span>
    </div>

    <!-- Section C: Two-column grid (Pie + Line) -->
    <div v-if="detail" class="ud-panel__charts-grid">
      <div class="ud-panel__chart-box">
        <div class="ud-panel__chart-header">
          <span class="ud-panel__chart-title">模型分布</span>
          <el-radio-group v-model="pieMetric" size="small" class="ud-metric-toggle">
            <el-radio-button value="cost">成本</el-radio-button>
            <el-radio-button value="calls">调用</el-radio-button>
            <el-radio-button value="tokens">Token</el-radio-button>
          </el-radio-group>
        </div>
        <div class="ud-pie-wrap">
          <canvas ref="pieCanvas"></canvas>
        </div>
      </div>
      <div class="ud-panel__chart-box">
        <div class="ud-panel__chart-header">
          <span class="ud-panel__chart-title">每日趋势</span>
          <el-radio-group v-model="trendMetric" size="small" class="ud-metric-toggle">
            <el-radio-button value="calls">调用</el-radio-button>
            <el-radio-button value="cost">成本</el-radio-button>
            <el-radio-button value="tokens">Token</el-radio-button>
          </el-radio-group>
        </div>
        <div class="ud-trend-wrap">
          <canvas ref="trendCanvas"></canvas>
        </div>
      </div>
    </div>

    <!-- Section D: Hourly Distribution Bar Chart (full width) -->
    <div v-if="detail" class="ud-panel__hourly-section">
      <div class="ud-panel__chart-header">
        <span class="ud-panel__chart-title">小时分布</span>
        <span class="ud-heat-legend">
          <span class="ud-heat-legend__bar"></span>
          <span class="ud-heat-legend__label">低</span>
          <span class="ud-heat-legend__label">高</span>
        </span>
      </div>
      <div class="ud-hourly-wrap">
        <canvas ref="hourlyCanvas"></canvas>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { User, Close, Loading } from '@element-plus/icons-vue'
import { Chart, registerables } from 'chart.js'
import { useSettingsStore } from '@/stores/settings'
import { currencySymbol, convertCost, type Currency } from '@/utils/currency'
import { fetchUserDetail } from '@/api/dashboard-enhanced'
import type {
  UserDetailResponse,
  UserDetailSummary,
  DailyTrendItem,
  ModelDistributionItem,
  HourlyDistributionItem,
} from '@/types/dashboard-enhanced'

Chart.register(...registerables)

defineOptions({ name: 'UserDetailPanel' })

// ── Props ───────────────────────────────────────────────────────────
const props = defineProps<{
  userId: string
  userName: string
  deptName?: string
  dateRange: [string, string]
  currency: 'CNY' | 'USD'
}>()

// ── Emits ───────────────────────────────────────────────────────────
const emit = defineEmits<{
  'model-click': [modelName: string]
  'close': []
}>()

// ── State ───────────────────────────────────────────────────────────
const loading = ref(false)
const detail = ref<UserDetailResponse | null>(null)

const pieMetric = ref<'cost' | 'calls' | 'tokens'>('cost')
const trendMetric = ref<'calls' | 'cost' | 'tokens'>('calls')

const pieCanvas = ref<HTMLCanvasElement | null>(null)
const trendCanvas = ref<HTMLCanvasElement | null>(null)
const hourlyCanvas = ref<HTMLCanvasElement | null>(null)

let pieChart: Chart | null = null
let trendChart: Chart | null = null
let hourlyChart: Chart | null = null

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency as Currency,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const sym = computed(() => currencySymbol(props.currency as Currency))

// ── Computed ────────────────────────────────────────────────────────
const successRate = computed(() => {
  if (!detail.value) return 0
  const { success_count, total_calls } = detail.value.summary
  if (!total_calls) return 0
  return ((success_count / total_calls) * 100).toFixed(1)
})

const successRateClass = computed(() => {
  const rate = parseFloat(successRate.value)
  if (rate >= 99) return 'ud-kpi--green'
  if (rate >= 95) return 'ud-kpi--yellow'
  return 'ud-kpi--red'
})

const errorRateClass = computed(() => {
  if (!detail.value) return ''
  const er = detail.value.summary.error_rate
  if (er < 2) return 'ud-kpi--green'
  if (er <= 5) return 'ud-kpi--yellow'
  return 'ud-kpi--red'
})

const avgDurationClass = computed(() => {
  if (!detail.value) return ''
  const ms = detail.value.summary.avg_duration_ms
  if (ms < 2000) return 'ud-kpi--green'
  if (ms <= 5000) return 'ud-kpi--yellow'
  return 'ud-kpi--red'
})

const fmtCostPerCall = computed(() => {
  if (!detail.value || !detail.value.summary.total_calls) return `${sym.value}0.00`
  const perCall = detail.value.summary.est_cost_usd / detail.value.summary.total_calls
  return fmtCost(perCall)
})

function fmtDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

// ── Helpers ─────────────────────────────────────────────────────────
function fmtCost(usd: number): string {
  const n = convertCost(Number(usd || 0), cc.value)
  const s = sym.value
  if (n === 0) return `${s}0.00`
  let v: string
  if (Math.abs(n) >= 1) v = n.toFixed(2)
  else if (Math.abs(n) >= 0.01) v = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001) v = n.toFixed(6)
  else v = n.toFixed(8)
  return s + v.replace(/(\.\d{2,}?)0+$/, '$1')
}

function fmtTokens(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}

// ── Data fetching ───────────────────────────────────────────────────
async function loadData() {
  if (loading.value) return
  if (!props.userId || !props.dateRange[0] || !props.dateRange[1]) return
  loading.value = true
  try {
    const res = await fetchUserDetail({
      user_id: props.userId,
      from: props.dateRange[0],
      to: props.dateRange[1],
    })
    detail.value = res
    await nextTick()
    initCharts()
  } catch (e) {
    detail.value = null
    console.warn('Failed to load user detail:', e)
  } finally {
    loading.value = false
  }
}

// ── Chart init helpers ─────────────────────────────────────────────
const PIE_COLORS = [
  '#409eff', '#67c23a', '#e6a23c', '#f56c6c', '#909399',
]
const PIE_OTHER_COLOR = '#dcdfe6'

function initPieChart() {
  if (!pieCanvas.value || !detail.value) return
  if (pieChart) pieChart.destroy()

  const items = detail.value.model_distribution
  const metric = pieMetric.value
  const total = items.reduce((s, i) => s + i[metric], 0)

  // Top 5 + Other
  const sorted = [...items].sort((a, b) => b[metric] - a[metric])
  const top5 = sorted.slice(0, 5)
  const other = sorted.slice(5)
  const otherSum = other.reduce((s, i) => s + i[metric], 0)

  const labels = top5.map(i => i.model)
  const data = top5.map(i => i[metric])
  const colors = PIE_COLORS.slice(0, top5.length)

  if (otherSum > 0) {
    labels.push('Other')
    data.push(otherSum)
    colors.push(PIE_OTHER_COLOR)
  }

  const modelMap = new Map(top5.map(i => [i.model, i]))

  pieChart = new Chart(pieCanvas.value, {
    type: 'doughnut',
    data: {
      labels,
      datasets: [{
        data,
        backgroundColor: colors,
        borderColor: '#fff',
        borderWidth: 2,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: true,
      plugins: {
        legend: {
          position: 'right',
          labels: {
            padding: 12,
            usePointStyle: true,
            pointStyle: 'circle',
            font: { size: 12 },
            generateLabels(chart) {
              const ds = chart.data.datasets[0]
              const totalVal = (ds.data as number[]).reduce((a, b) => a + b, 0)
              return (chart.data.labels || []).map((label, i) => {
                const val = (ds.data as number[])[i]
                const pct = totalVal > 0 ? ((val / totalVal) * 100).toFixed(1) : '0.0'
                const m = metric === 'cost' ? fmtCost(val) : metric === 'calls' ? val.toLocaleString() : fmtTokens(val)
                return {
                  text: `${label}  ${pct}%  (${m})`,
                  fillStyle: (ds.backgroundColor as string[])[i],
                  strokeStyle: (ds.borderColor as string[])[i] || '#fff',
                  pointStyle: 'circle',
                  hidden: false,
                  index: i,
                }
              })
            },
          },
        },
        tooltip: {
          callbacks: {
            label(ctx) {
              const label = ctx.label || ''
              const val = ctx.parsed as number
              const pct = ((val / (ctx.dataset.data as number[]).reduce((a, b) => a + b, 0)) * 100).toFixed(1)
              let formatted: string
              if (metric === 'cost') formatted = fmtCost(val)
              else if (metric === 'calls') formatted = val.toLocaleString()
              else formatted = fmtTokens(val)
              return `${label}: ${formatted} (${pct}%)`
            },
          },
        },
      },
      onClick(_e, elements) {
        if (elements.length > 0) {
          const idx = elements[0].index
          const label = labels[idx]
          if (label && label !== 'Other') {
            emit('model-click', label)
          }
        }
      },
    },
  })
}

function initTrendChart() {
  if (!trendCanvas.value || !detail.value) return
  if (trendChart) trendChart.destroy()

  const trend = detail.value.daily_trend
  const metric = trendMetric.value

  const labels = trend.map(i => i.date.slice(5))
  const data = trend.map(i => {
    if (metric === 'cost') return convertCost(i.cost, cc.value)
    return i[metric]
  })

  const colors = {
    calls: '#409eff',
    cost: '#67c23a',
    tokens: '#e6a23c',
  }

  const yLabel = metric === 'cost' ? `${sym.value}cost` : metric === 'calls' ? '调用次数' : 'Token 数'

  trendChart = new Chart(trendCanvas.value, {
    type: 'line',
    data: {
      labels,
      datasets: [{
        label: yLabel,
        data,
        borderColor: colors[metric],
        backgroundColor: colors[metric] + '22',
        fill: true,
        tension: 0.3,
        pointRadius: 3,
        pointHoverRadius: 5,
        borderWidth: 2,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: true,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label(ctx) {
              const val = ctx.parsed.y
              if (metric === 'cost') return `成本: ${fmtCost(val)}`
              if (metric === 'calls') return `调用: ${val.toLocaleString()}`
              return `Token: ${fmtTokens(val)}`
            },
          },
        },
      },
      scales: {
        x: {
          grid: { display: false },
          ticks: { font: { size: 10 }, maxTicksLimit: 10 },
        },
        y: {
          beginAtZero: true,
          grid: { color: 'var(--ud-border, #eee)' },
          ticks: {
            font: { size: 10 },
            callback(value) {
              const n = Number(value)
              if (metric === 'tokens') return n >= 1e6 ? (n / 1e6).toFixed(1) + 'M' : n >= 1e3 ? (n / 1e3).toFixed(1) + 'K' : String(n)
              if (metric === 'cost') return sym.value + n.toFixed(1)
              return n >= 1000 ? (n / 1000).toFixed(1) + 'K' : String(n)
            },
          },
        },
      },
    },
  })
}

function initHourlyChart() {
  if (!hourlyCanvas.value || !detail.value) return
  if (hourlyChart) hourlyChart.destroy()

  const hours = detail.value.hourly_distribution
  const map = new Map<number, number>()
  for (let i = 0; i < 24; i++) map.set(i, 0)
  for (const h of hours) map.set(h.hour, h.calls)

  const labels = Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, '0')}:00`)
  const data = Array.from({ length: 24 }, (_, i) => map.get(i) ?? 0)

  // Heatmap coloring: map value intensity to HSL hue (210° blue → 20° orange-red)
  const maxVal = Math.max(...data, 1)
  const barColors = data.map(v => {
    const ratio = v / maxVal
    // Hue: 210 (cool blue) → 30 (warm orange) as ratio increases
    const hue = 210 - ratio * 180
    // Saturation: 60% → 85%
    const sat = 60 + ratio * 25
    // Lightness: 65% → 50%
    const light = 65 - ratio * 15
    return `hsl(${hue}, ${sat}%, ${light}%)`
  })

  hourlyChart = new Chart(hourlyCanvas.value, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: '调用次数',
        data,
        backgroundColor: barColors,
        borderRadius: 3,
        borderSkipped: false,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: true,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label(ctx) {
              return `调用: ${(ctx.parsed.y as number).toLocaleString()}`
            },
          },
        },
      },
      scales: {
        x: {
          grid: { display: false },
          ticks: {
            font: { size: 9 },
            maxTicksLimit: 12,
            callback(_value, index) {
              if (index % 2 === 0) return labels[index]
              return ''
            },
          },
        },
        y: {
          beginAtZero: true,
          grid: { color: 'var(--ud-border, #eee)' },
          ticks: {
            font: { size: 10 },
            callback(value) {
              const n = Number(value)
              return n >= 1000 ? (n / 1000).toFixed(1) + 'K' : String(n)
            },
          },
        },
      },
    },
  })
}

function initCharts() {
  initPieChart()
  initTrendChart()
  initHourlyChart()
}

function destroyCharts() {
  if (pieChart) { pieChart.destroy(); pieChart = null }
  if (trendChart) { trendChart.destroy(); trendChart = null }
  if (hourlyChart) { hourlyChart.destroy(); hourlyChart = null }
}

// ── Watchers ────────────────────────────────────────────────────────
watch(() => [props.userId, props.dateRange[0], props.dateRange[1]] as const, () => {
  loadData()
})

watch(pieMetric, () => nextTick(initPieChart))
watch(trendMetric, () => nextTick(initTrendChart))

// ── Lifecycle ───────────────────────────────────────────────────────
onMounted(() => {
  loadData()
})

onBeforeUnmount(() => {
  destroyCharts()
})
</script>

<style scoped>
/* ── Panel wrapper ── */
.ud-panel {
  border-radius: 14px;
  background: var(--ud-surface);
  border: 1px solid var(--ud-border);
  overflow: hidden;
  margin-bottom: 16px;
}

/* ── Section A: Header ── */
.ud-panel__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--ud-border);
}
.ud-panel__user-info {
  display: flex;
  align-items: center;
  gap: 10px;
}
.ud-panel__user-icon {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  border-radius: 50%;
  padding: 4px;
}
.ud-panel__user-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.ud-panel__user-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  line-height: 1.3;
}
.ud-panel__dept-name {
  font-size: 12px;
  color: var(--ud-muted);
  line-height: 1.2;
}
.ud-panel__close-btn {
  flex-shrink: 0;
  color: var(--ud-muted);
}
.ud-panel__close-btn:hover {
  color: var(--el-color-primary);
}

/* ── Loading / Empty ── */
.ud-panel__loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 48px 0;
  color: var(--ud-muted);
  font-size: 14px;
}
.ud-panel__empty {
  text-align: center;
  padding: 48px 0;
  color: var(--ud-faint, var(--ud-muted));
  font-size: 14px;
}

/* ── Section B: KPI Cards ── */
.ud-panel__kpi-row {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  padding: 16px 18px;
  border-bottom: 1px solid var(--ud-border);
}
.ud-kpi-card {
  flex: 1;
  min-width: 100px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px 14px;
  border-radius: 10px;
  background: var(--el-fill-color-extra-light, var(--el-fill-color-light));
}
.ud-kpi-label {
  font-size: 12px;
  color: var(--ud-muted);
  line-height: 1.2;
  white-space: nowrap;
}
.ud-kpi-value {
  font-size: 20px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
}
.ud-kpi-cost {
  color: var(--el-color-primary);
}

/* KPI color classes */
.ud-kpi--green { color: var(--el-color-success); }
.ud-kpi--yellow { color: var(--el-color-warning); }
.ud-kpi--red { color: var(--el-color-danger); }

/* ── Section B2: Efficiency Metrics ── */
.ud-panel__eff-row {
  display: flex;
  align-items: center;
  gap: 0;
  padding: 10px 18px;
  border-bottom: 1px solid var(--ud-border);
  background: var(--el-fill-color-blank, #fafbfc);
}
.ud-eff-item {
  flex: 1;
  display: flex;
  align-items: baseline;
  gap: 8px;
  justify-content: center;
}
.ud-eff-label {
  font-size: 12px;
  color: var(--ud-muted);
  white-space: nowrap;
}
.ud-eff-value {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}
.ud-eff-sep {
  color: var(--ud-muted);
  margin: 0 2px;
  font-weight: 400;
}
.ud-eff-divider {
  width: 1px;
  height: 20px;
  background: var(--ud-border);
  flex-shrink: 0;
}

/* ── Section C: Two-column grid ── */
.ud-panel__charts-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
  border-bottom: 1px solid var(--ud-border);
}
.ud-panel__chart-box {
  padding: 14px 16px;
  min-height: 280px;
  display: flex;
  flex-direction: column;
}
.ud-panel__chart-box:first-child {
  border-right: 1px solid var(--ud-border);
}
.ud-panel__chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  flex-wrap: wrap;
  gap: 6px;
}
.ud-panel__chart-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
}
.ud-metric-toggle .el-radio-button__inner {
  font-size: 11px;
  padding: 4px 10px;
}

.ud-pie-wrap,
.ud-trend-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 220px;
}
.ud-pie-wrap canvas,
.ud-trend-wrap canvas {
  max-width: 100%;
  max-height: 240px;
}

/* ── Section D: Hourly Bar ── */
.ud-panel__hourly-section {
  padding: 14px 16px;
}
.ud-hourly-wrap {
  min-height: 180px;
}
/* Heat legend */
.ud-heat-legend {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
}
.ud-heat-legend__bar {
  width: 60px;
  height: 8px;
  border-radius: 4px;
  background: linear-gradient(90deg, hsl(210, 60%, 65%), hsl(120, 70%, 55%), hsl(30, 85%, 50%));
}
.ud-heat-legend__label {
  font-size: 11px;
  color: var(--ud-muted);
}
.ud-hourly-wrap canvas {
  max-width: 100%;
  max-height: 200px;
}

/* ── Responsive ── */
@media (max-width: 767px) {
  .ud-panel__charts-grid {
    grid-template-columns: 1fr;
  }
  .ud-panel__chart-box:first-child {
    border-right: none;
    border-bottom: 1px solid var(--ud-border);
  }
  .ud-panel__kpi-row {
    flex-direction: column;
  }
  .ud-kpi-card {
    min-width: unset;
  }
}
</style>
