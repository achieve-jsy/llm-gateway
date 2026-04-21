<template>
  <div class="ud-section ud-panel">
    <!-- Section A: Header -->
    <div class="ud-panel__header ud-model-panel__header">
      <div class="ud-panel__user-info">
        <el-icon class="ud-panel__user-icon" :size="28"><Cpu /></el-icon>
        <div class="ud-panel__user-meta">
          <span class="ud-panel__user-name">{{ modelName }}</span>
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

    <!-- Loading state -->
    <div v-if="loading" class="ud-panel__loading">
      <el-icon class="is-loading" :size="24"><Loading /></el-icon>
      <span>加载中...</span>
    </div>

    <!-- No data state -->
    <div v-if="!loading && !detail" class="ud-panel__empty">
      <span>暂无模型详情数据</span>
    </div>

    <!-- Section B: KPI Cards Row (8 cards) -->
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
        <span class="ud-kpi-label">用户数</span>
        <span class="ud-kpi-value">{{ detail.summary.user_count.toLocaleString() }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">部门数</span>
        <span class="ud-kpi-value">{{ detail.summary.dept_count.toLocaleString() }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">成功率</span>
        <span
          class="ud-kpi-value"
          :class="successRateClass"
        >{{ successRate }}%</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">平均延迟</span>
        <span class="ud-kpi-value">{{ fmtDuration(detail.summary.avg_duration_ms) }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">千Token成本</span>
        <span class="ud-kpi-value">{{ fmtCost(costPer1kTokens) }}</span>
      </div>
      <div class="ud-kpi-card">
        <span class="ud-kpi-label">错误率</span>
        <span
          class="ud-kpi-value"
          :class="errorRateClass"
        >{{ detail.summary.error_rate.toFixed(1) }}%</span>
      </div>
    </div>

    <!-- Section C: vs Team Average Comparison Box -->
    <div v-if="detail && teamAvg" class="ud-model-vs-box">
      <div class="ud-model-vs-row">
        <span class="ud-model-vs-label">成功率</span>
        <span class="ud-model-vs-values">
          {{ successRate }}% vs {{ teamAvg.success_rate.toFixed(1) }}%
        </span>
        <span class="ud-model-vs-result" :class="vsSuccessRateClass">
          {{ vsSuccessRateText }}
        </span>
      </div>
      <div class="ud-model-vs-row">
        <span class="ud-model-vs-label">延迟</span>
        <span class="ud-model-vs-values">
          {{ fmtDuration(detail.summary.avg_duration_ms) }} vs {{ fmtDuration(teamAvg.avg_duration_ms) }}
        </span>
        <span class="ud-model-vs-result" :class="vsDurationClass">
          {{ vsDurationText }}
        </span>
      </div>
      <div class="ud-model-vs-row">
        <span class="ud-model-vs-label">千Token成本</span>
        <span class="ud-model-vs-values">
          {{ fmtCost(costPer1kTokens) }} vs {{ fmtCost(teamAvg.cost_per_1k_tokens) }}
        </span>
        <span class="ud-model-vs-result" :class="vsCostPer1kClass">
          {{ vsCostPer1kText }}
        </span>
      </div>
    </div>

    <!-- Section D: Daily Trend Chart (full width) -->
    <div v-if="detail" class="ud-model-panel__trend-section">
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

    <!-- Section E: Two-column grid -->
    <div v-if="detail" class="ud-model-panel__bottom-grid">
      <!-- Left: 使用者排行 Top 5 -->
      <div class="ud-model-panel__col">
        <div class="ud-panel__chart-header">
          <span class="ud-panel__chart-title">使用者排行</span>
        </div>
        <div class="ud-model-user-list">
          <div
            v-for="(user, idx) in topUsers"
            :key="user.user_id"
            class="ud-model-user-item"
          >
            <span class="ud-model-user-rank">{{ idx + 1 }}</span>
            <div class="ud-model-user-info">
              <span
                class="ud-model-user-name"
                @click="emit('user-click', user.user_id, user.user_name)"
              >{{ user.user_name }}</span>
              <span class="ud-model-user-dept">{{ user.dept_name }}</span>
            </div>
            <div class="ud-model-user-stats">
              <span class="ud-model-user-calls">{{ user.calls.toLocaleString() }} 次</span>
              <span class="ud-model-user-cost">{{ fmtCost(user.cost) }}</span>
            </div>
          </div>
          <!-- Progress bars -->
          <div class="ud-model-user-progress-row" v-for="user in topUsers" :key="'p-' + user.user_id">
            <div class="ud-model-progress">
              <div
                class="ud-model-progress-bar"
                :style="{ width: userCostPct(user.cost) + '%' }"
              ></div>
            </div>
          </div>
          <!-- Others + link -->
          <div v-if="otherUsersCount > 0" class="ud-model-user-others">
            <span class="ud-model-user-others-text">其他 {{ otherUsersCount }} 人</span>
            <span class="ud-model-user-others-link">[查看全部 →]</span>
          </div>
        </div>
      </div>

      <!-- Right: 部门覆盖 (horizontal bar chart) -->
      <div class="ud-model-panel__col">
        <div class="ud-panel__chart-header">
          <span class="ud-panel__chart-title">部门覆盖</span>
        </div>
        <div class="ud-model-dept-chart">
          <canvas ref="deptCanvas"></canvas>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { Cpu, Close, Loading } from '@element-plus/icons-vue'
import { Chart, registerables } from 'chart.js'
import { useSettingsStore } from '@/stores/settings'
import { currencySymbol, convertCost, type Currency } from '@/utils/currency'
import { fetchModelDetail } from '@/api/dashboard-enhanced'
import type {
  ModelDetailResponse,
  DailyTrendItem,
} from '@/types/dashboard-enhanced'

Chart.register(...registerables)

defineOptions({ name: 'ModelDetailPanel' })

// ── Props ───────────────────────────────────────────────────────────
const props = defineProps<{
  modelName: string
  dateRange: [string, string]
  currency: 'CNY' | 'USD'
  teamAvg?: {
    success_rate: number
    avg_duration_ms: number
    cost_per_1k_tokens: number
  }
}>()

// ── Emits ───────────────────────────────────────────────────────────
const emit = defineEmits<{
  'user-click': [userId: string, userName: string]
  'close': []
}>()

// ── State ───────────────────────────────────────────────────────────
const loading = ref(false)
const detail = ref<ModelDetailResponse | null>(null)

const trendMetric = ref<'calls' | 'cost' | 'tokens'>('calls')

const trendCanvas = ref<HTMLCanvasElement | null>(null)
const deptCanvas = ref<HTMLCanvasElement | null>(null)

let trendChart: Chart | null = null
let deptChart: Chart | null = null

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency as Currency,
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const sym = computed(() => currencySymbol(props.currency as Currency))

// ── Computed ────────────────────────────────────────────────────────
const successRate = computed(() => {
  if (!detail.value) return '0'
  const { success_count, total_calls } = detail.value.summary
  if (!total_calls) return '0'
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

const costPer1kTokens = computed(() => {
  if (!detail.value) return 0
  const { est_cost_usd, total_tokens } = detail.value.summary
  if (!total_tokens) return 0
  return est_cost_usd / (total_tokens / 1000)
})

// Top 5 users
const topUsers = computed(() => {
  if (!detail.value) return []
  return [...detail.value.user_distribution]
    .sort((a, b) => b.cost - a.cost)
    .slice(0, 5)
})

const maxUserCost = computed(() => {
  if (topUsers.value.length === 0) return 0
  return Math.max(...topUsers.value.map(u => u.cost))
})

const otherUsersCount = computed(() => {
  if (!detail.value) return 0
  const total = detail.value.user_distribution.length
  return Math.max(0, total - 5)
})

// ── Team average computed ───────────────────────────────────────────
const vsSuccessRateClass = computed(() => {
  if (!props.teamAvg) return ''
  const rate = parseFloat(successRate.value)
  const avg = props.teamAvg.success_rate
  if (rate >= avg + 1) return 'ud-model-vs-good'
  if (rate <= avg - 1) return 'ud-model-vs-bad'
  return 'ud-model-vs-neutral'
})

const vsSuccessRateText = computed(() => {
  if (!props.teamAvg) return ''
  const rate = parseFloat(successRate.value)
  const avg = props.teamAvg.success_rate
  if (rate >= avg + 1) return '✅ 高于平均'
  if (rate <= avg - 1) return '⚠️ 低于平均'
  return '接近平均'
})

const vsDurationClass = computed(() => {
  if (!props.teamAvg || !detail.value) return ''
  const d = detail.value.summary.avg_duration_ms
  const avg = props.teamAvg.avg_duration_ms
  if (d <= avg * 0.8) return 'ud-model-vs-good'
  if (d >= avg * 1.2) return 'ud-model-vs-bad'
  return 'ud-model-vs-neutral'
})

const vsDurationText = computed(() => {
  if (!props.teamAvg || !detail.value) return ''
  const d = detail.value.summary.avg_duration_ms
  const avg = props.teamAvg.avg_duration_ms
  if (d <= avg * 0.8) return '✅ 快于平均'
  if (d >= avg * 1.2) return '⚠️ 慢于平均'
  return '接近平均'
})

const vsCostPer1kClass = computed(() => {
  if (!props.teamAvg) return ''
  const v = costPer1kTokens.value
  const avg = props.teamAvg.cost_per_1k_tokens
  if (v <= avg * 0.8) return 'ud-model-vs-good'
  if (v >= avg * 1.2) return 'ud-model-vs-bad'
  return 'ud-model-vs-neutral'
})

const vsCostPer1kText = computed(() => {
  if (!props.teamAvg) return ''
  const v = costPer1kTokens.value
  const avg = props.teamAvg.cost_per_1k_tokens
  if (v <= avg * 0.8) return '✅ 低于平均'
  if (v >= avg * 1.2) return '⚠️ 高于平均'
  return '接近平均'
})

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

function fmtDuration(ms: number): string {
  if (!ms && ms !== 0) return '0ms'
  if (ms >= 1000) return (ms / 1000).toFixed(1) + 's'
  return Math.round(ms) + 'ms'
}

function userCostPct(cost: number): number {
  const max = maxUserCost.value
  if (!max) return 0
  return (cost / max) * 100
}

// ── Data fetching ───────────────────────────────────────────────────
let loadVersion = 0

async function loadData() {
  if (!props.modelName || !props.dateRange[0] || !props.dateRange[1]) return
  const ver = ++loadVersion
  loading.value = true
  try {
    const res = await fetchModelDetail({
      model: props.modelName,
      from: props.dateRange[0],
      to: props.dateRange[1],
    })
    if (ver !== loadVersion) return // stale request
    detail.value = res
    await nextTick()
    initCharts()
  } catch (e) {
    if (ver !== loadVersion) return
    detail.value = null
    console.warn('Failed to load model detail:', e)
  } finally {
    if (ver === loadVersion) loading.value = false
  }
}

// ── Chart init helpers ─────────────────────────────────────────────
function initTrendChart() {
  if (!trendCanvas.value || !detail.value) return
  if (trendChart) trendChart.destroy()

  const trend = detail.value.daily_trend
  const metric = trendMetric.value

  const labels = trend.map((i: DailyTrendItem) => i.date.slice(5))
  const data = trend.map((i: DailyTrendItem) => {
    if (metric === 'cost') return convertCost(i.cost, cc.value)
    return i[metric as 'calls' | 'tokens']
  })

  const colors: Record<string, string> = {
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
              if (metric === 'cost') return `成本: ${sym.value}${(val as number).toFixed(2)}`
              if (metric === 'calls') return `调用: ${(val as number).toLocaleString()}`
              return `Token: ${fmtTokens(val as number)}`
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

function initDeptChart() {
  if (!deptCanvas.value || !detail.value) return
  if (deptChart) deptChart.destroy()

  // Aggregate user_distribution by dept_name
  const deptMap = new Map<string, number>()
  for (const u of detail.value.user_distribution) {
    const dept = u.dept_name || '(未知)'
    deptMap.set(dept, (deptMap.get(dept) || 0) + u.cost)
  }

  // Sort by cost descending, take top 8
  const entries = Array.from(deptMap.entries()) as [string, number][]
  const sorted = entries
    .sort((a, b) => b[1] - a[1])
    .slice(0, 8)

  const labels = sorted.map(([name]: [string, number]) => name)
  const data = sorted.map(([, val]: [string, number]) => convertCost(val, cc.value))

  // Reverse so highest is at top
  labels.reverse()
  data.reverse()

  deptChart = new Chart(deptCanvas.value, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: sym.value + 'cost',
        data,
        backgroundColor: '#409eff',
        borderRadius: 3,
        borderSkipped: false,
      }],
    },
    options: {
      indexAxis: 'y',
      responsive: true,
      maintainAspectRatio: true,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label(ctx) {
              const val = (ctx.parsed as { x: number }).x
              if (val == null) return ''
              return `成本: ${sym.value}${val.toFixed(2)}`
            },
          },
        },
      },
      scales: {
        x: {
          beginAtZero: true,
          grid: { color: 'var(--ud-border, #eee)' },
          ticks: {
            font: { size: 10 },
            callback(value) {
              return sym.value + Number(value).toFixed(1)
            },
          },
        },
        y: {
          grid: { display: false },
          ticks: {
            font: { size: 10 },
            maxTicksLimit: 8,
          },
        },
      },
    },
  })
}

function initCharts() {
  initTrendChart()
  initDeptChart()
}

function destroyCharts() {
  if (trendChart) { trendChart.destroy(); trendChart = null }
  if (deptChart) { deptChart.destroy(); deptChart = null }
}

// ── Watchers ────────────────────────────────────────────────────────
watch(() => [props.modelName, props.dateRange[0], props.dateRange[1]] as const, () => {
  loadData()
})

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
.ud-model-panel__header .ud-panel__user-info {
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

/* ── Section C: vs Team Average Comparison Box ── */
.ud-model-vs-box {
  margin: 0 18px;
  padding: 14px 16px;
  border: 1px solid var(--ud-border);
  border-radius: 10px;
  background: var(--el-fill-color-extra-light, var(--el-fill-color-light));
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.ud-model-vs-row {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 13px;
  flex-wrap: wrap;
}
.ud-model-vs-label {
  font-weight: 600;
  color: var(--el-text-color-primary);
  min-width: 80px;
  white-space: nowrap;
}
.ud-model-vs-values {
  color: var(--el-text-color-regular);
  font-variant-numeric: tabular-nums;
  flex: 1;
  min-width: 120px;
}
.ud-model-vs-result {
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
}
.ud-model-vs-good { color: var(--el-color-success); }
.ud-model-vs-neutral { color: var(--el-text-color-secondary); }
.ud-model-vs-bad { color: var(--el-color-danger); }

/* ── Section D: Daily Trend Chart (full width) ── */
.ud-model-panel__trend-section {
  padding: 14px 16px;
  border-bottom: 1px solid var(--ud-border);
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
.ud-trend-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 220px;
}
.ud-trend-wrap canvas {
  max-width: 100%;
  max-height: 240px;
}

/* ── Section E: Two-column grid ── */
.ud-model-panel__bottom-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0;
}
.ud-model-panel__col {
  padding: 14px 16px;
  min-height: 280px;
  display: flex;
  flex-direction: column;
}
.ud-model-panel__col:first-child {
  border-right: 1px solid var(--ud-border);
}

/* User ranking list */
.ud-model-user-list {
  display: flex;
  flex-direction: column;
  gap: 0;
  flex: 1;
}
.ud-model-user-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 4px;
  border-radius: 6px;
  transition: background 0.15s;
  cursor: default;
}
.ud-model-user-item:hover {
  background: var(--el-fill-color-light, #f5f7fa);
}
.ud-model-user-rank {
  width: 20px;
  font-size: 12px;
  font-weight: 700;
  color: var(--ud-muted);
  text-align: center;
  flex-shrink: 0;
}
.ud-model-user-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}
.ud-model-user-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-color-primary);
  cursor: pointer;
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ud-model-user-name:hover {
  text-decoration: underline;
}
.ud-model-user-dept {
  font-size: 11px;
  color: var(--ud-muted);
  line-height: 1.2;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ud-model-user-stats {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 1px;
  flex-shrink: 0;
}
.ud-model-user-calls {
  font-size: 12px;
  color: var(--el-text-color-regular);
  font-variant-numeric: tabular-nums;
}
.ud-model-user-cost {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-color-primary);
  font-variant-numeric: tabular-nums;
}

/* Progress bars */
.ud-model-user-progress-row {
  padding: 0 4px 6px 30px;
}
.ud-model-progress {
  height: 4px;
  border-radius: 2px;
  background: var(--el-fill-color-light, #e8eaed);
  overflow: hidden;
}
.ud-model-progress-bar {
  height: 100%;
  border-radius: 2px;
  background: var(--el-color-primary);
  transition: width 0.3s ease;
}

/* Others row */
.ud-model-user-others {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 4px 4px 30px;
  font-size: 12px;
  color: var(--ud-muted);
}
.ud-model-user-others-link {
  color: var(--el-color-primary);
  cursor: pointer;
  font-weight: 500;
}
.ud-model-user-others-link:hover {
  text-decoration: underline;
}

/* Dept horizontal bar chart */
.ud-model-dept-chart {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 240px;
}
.ud-model-dept-chart canvas {
  max-width: 100%;
  max-height: 260px;
}

/* ── Responsive ── */
@media (max-width: 767px) {
  .ud-model-panel__bottom-grid {
    grid-template-columns: 1fr;
  }
  .ud-model-panel__col:first-child {
    border-right: none;
    border-bottom: 1px solid var(--ud-border);
  }
  .ud-panel__kpi-row {
    flex-direction: column;
  }
  .ud-kpi-card {
    min-width: unset;
  }
  .ud-model-vs-row {
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
  }
  .ud-model-vs-label {
    min-width: unset;
  }
  .ud-model-vs-values {
    min-width: unset;
  }
}
</style>