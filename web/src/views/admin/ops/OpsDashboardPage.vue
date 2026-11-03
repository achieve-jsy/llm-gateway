<template>
  <div class="op">

    <!-- ═══════════════════════════════════════════
         页头
    ═══════════════════════════════════════════ -->
    <div class="op-header">
      <div class="op-header__left">
        <h2 class="op-title">运维总览</h2>
        <el-tag :type="systemHealthTag" effect="dark" size="small">{{ systemHealthLabel }}</el-tag>
        <span class="op-badge">系统状态 · 实时监控</span>
      </div>
      <div class="op-header__right">
        <el-switch v-model="autoRefresh" size="small" @change="toggleAutoRefresh" inline-prompt active-text="自动" inactive-text="手动" class="op-auto-switch" />
        <el-button :loading="loading" :icon="Refresh" circle size="small" title="刷新" @click="reloadAll" />
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         统计卡片（sub2api 风格，数据来自 ops/status 轻量接口）
    ═══════════════════════════════════════════ -->
    <div class="op-stats" v-loading="statsLoading">
      <div class="op-stat-card">
        <div class="op-stat-card__icon op-stat-card__icon--blue">
          <el-icon size="22"><Connection /></el-icon>
        </div>
        <div class="op-stat-card__body">
          <p class="op-stat-card__label">渠道总数</p>
          <p class="op-stat-card__value">{{ fmtNum(stats.providers.total) }}</p>
          <p class="op-stat-card__sub">
            启用 <span class="c-green">{{ stats.providers.enabled }}</span>
            · 禁用 <span class="c-gray">{{ stats.providers.disabled }}</span>
          </p>
        </div>
      </div>

      <div class="op-stat-card">
        <div class="op-stat-card__icon op-stat-card__icon--amber">
          <el-icon size="22"><Cpu /></el-icon>
        </div>
        <div class="op-stat-card__body">
          <p class="op-stat-card__label">模型总数</p>
          <p class="op-stat-card__value">{{ fmtNum(stats.models.total) }}</p>
          <p class="op-stat-card__sub">
            启用 <span class="c-green">{{ stats.models.enabled }}</span>
            · 禁用 <span class="c-gray">{{ stats.models.disabled }}</span>
          </p>
        </div>
      </div>

      <div class="op-stat-card">
        <div class="op-stat-card__icon op-stat-card__icon--cyan">
          <el-icon size="22"><Key /></el-icon>
        </div>
        <div class="op-stat-card__body">
          <p class="op-stat-card__label">API Keys</p>
          <p class="op-stat-card__value">{{ fmtNum(stats.keys.total) }}</p>
          <p class="op-stat-card__sub">
            启用 <span class="c-green">{{ stats.keys.enabled }}</span>
            · 禁用 <span class="c-gray">{{ stats.keys.disabled }}</span>
          </p>
        </div>
      </div>

      <div class="op-stat-card op-stat-card--cost">
        <div class="op-stat-card__icon op-stat-card__icon--green">
          <el-icon size="22"><Collection /></el-icon>
        </div>
        <div class="op-stat-card__body">
          <p class="op-stat-card__label">模型组</p>
          <p class="op-stat-card__value">{{ fmtNum(stats.groups.total) }}</p>
          <p class="op-stat-card__sub">
            公有 <span class="c-blue">{{ stats.groups.public }}</span>
            · 受控 <span class="c-amber">{{ stats.groups.controlled }}</span>
          </p>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         今日快照
    ═══════════════════════════════════════════ -->
    <div class="op-section op-section--snapshot">
      <div class="op-section__header">
        <span class="op-section__title">今日快照</span>
        <el-tag size="small" type="info" effect="plain">{{ todayStr }}</el-tag>
      </div>
      <div class="op-snapshot-body" v-loading="snapshotLoading">
        <div class="op-snapshot-grid">
          <div class="op-snapshot-item">
            <p class="op-snapshot-label">调用次数</p>
            <p class="op-snapshot-value c-blue">{{ fmtNum(todayStats.calls) }}</p>
            <p class="op-snapshot-sub">次</p>
          </div>
          <div class="op-snapshot-item">
            <p class="op-snapshot-label">Token 消耗</p>
            <p class="op-snapshot-value c-purple">{{ formatTokens(todayStats.tokens) }}</p>
            <p class="op-snapshot-sub">Tokens</p>
          </div>
          <div class="op-snapshot-item">
            <p class="op-snapshot-label">调用成本</p>
            <p class="op-snapshot-value c-green">{{ formatUSD(todayStats.cost) }}</p>
            <p class="op-snapshot-sub">USD</p>
          </div>
          <div class="op-snapshot-item">
            <p class="op-snapshot-label">成功率</p>
            <p class="op-snapshot-value" :class="successRateClass">{{ todayStats.successRate.toFixed(1) }}%</p>
            <p class="op-snapshot-sub c-red">{{ todayStats.errors }} 次失败</p>
          </div>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         趋势图表（调用 / 成本）
    ═══════════════════════════════════════════ -->
    <div class="op-charts">
      <div class="op-chart-card">
        <div class="op-chart-card__header">
          <span class="op-chart-card__title">调用趋势</span>
          <el-radio-group v-model="chartPeriod" size="small" @change="loadTrend">
            <el-radio-button value="week">7 天</el-radio-button>
            <el-radio-button value="month">30 天</el-radio-button>
          </el-radio-group>
        </div>
        <div class="op-chart-card__body" v-loading="trendLoading">
          <canvas ref="trendCanvasRef" />
          <div v-if="!trendLoading && trendData.length === 0" class="op-chart-empty">暂无趋势数据</div>
        </div>
      </div>

      <div class="op-chart-card">
        <div class="op-chart-card__header">
          <span class="op-chart-card__title">成本趋势</span>
          <el-radio-group v-model="chartPeriod" size="small" @change="loadTrend">
            <el-radio-button value="week">7 天</el-radio-button>
            <el-radio-button value="month">30 天</el-radio-button>
          </el-radio-group>
        </div>
        <div class="op-chart-card__body" v-loading="trendLoading">
          <canvas ref="costCanvasRef" />
          <div v-if="!trendLoading && trendData.length === 0" class="op-chart-empty">暂无趋势数据</div>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         性能概览（复用 /api/v1/health/performance）
    ═══════════════════════════════════════════ -->
    <div class="op-section">
      <div class="op-section__header">
        <span class="op-section__title">性能概览（近 1 小时）</span>
        <div style="display:flex;gap:8px;align-items:center">
          <el-radio-group v-model="perfView" size="small" @change="loadPerformance">
            <el-radio-button value="group">按组</el-radio-button>
            <el-radio-button value="model">按模型</el-radio-button>
          </el-radio-group>
          <el-button size="small" text @click="$router.push('/admin/performance')">详情</el-button>
        </div>
      </div>
      <div class="op-perf-overview" v-loading="perfLoading">
        <div v-if="!perfLoading && perfItems.length === 0" class="op-empty" style="padding:32px">
          <p>当前无活跃流量数据</p>
        </div>
        <el-table v-else :data="perfItems" size="small" :header-cell-style="TABLE_HEADER" style="width:100%" max-height="320">
          <el-table-column prop="name" label="名称" min-width="140" show-overflow-tooltip>
            <template #default="{ row }">
              <span class="op-model-name">{{ row.name }}</span>
            </template>
          </el-table-column>
          <el-table-column label="请求" width="90" align="right">
            <template #default="{ row }">{{ fmtNum(row.req_count) }}</template>
          </el-table-column>
          <el-table-column label="成功率" width="90" align="right">
            <template #default="{ row }">
              <span :class="perfRateClass(row.success_rate)">{{ ((row.success_rate || 0) * 100).toFixed(1) }}%</span>
            </template>
          </el-table-column>
          <el-table-column label="P50" width="90" align="right">
            <template #default="{ row }">
              <span class="op-mono">{{ row.p50_ms }}ms</span>
            </template>
          </el-table-column>
          <el-table-column label="P95" width="90" align="right">
            <template #default="{ row }">
              <span class="op-mono">{{ row.p95_ms }}ms</span>
            </template>
          </el-table-column>
          <el-table-column label="TTFT P50" width="100" align="right">
            <template #default="{ row }">
              <span class="op-mono">{{ row.ttft_p50_ms }}ms</span>
            </template>
          </el-table-column>
          <el-table-column label="Failover" width="90" align="right">
            <template #default="{ row }">
              <span :class="row.failover_count > 0 ? 'c-orange' : 'c-gray'">{{ row.failover_count }}</span>
            </template>
          </el-table-column>
          <el-table-column label="4xx/5xx" width="90" align="right">
            <template #default="{ row }">
              <span :class="row.err_5xx > 0 ? 'c-red' : 'c-gray'" title="5xx 错误">{{ row.err_5xx }}</span>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         健康热力图（复用 HealthTimeline 组件）
    ═══════════════════════════════════════════ -->
    <div class="op-section">
      <div class="op-section__header">
        <span class="op-section__title">健康热力图（近 6 小时）</span>
        <el-button size="small" text @click="$router.push('/admin/health-monitor')">全部</el-button>
      </div>
      <div class="op-heatmap" v-loading="heatLoading">
        <div v-if="!heatLoading && heatGroups.length === 0" class="op-empty" style="padding:24px">
          <p>暂无监控数据（需要开启健康检测）</p>
        </div>
        <div v-else class="op-heatmap-list">
          <div class="op-heatmap-row" v-for="g in heatGroups" :key="g.target_name + g.provider_name">
            <div class="op-heatmap-meta">
              <el-tag type="info" size="small" effect="plain" class="op-heatmap-tag">
                <span class="op-mono">{{ g.target_name }}</span>
              </el-tag>
              <el-tag v-if="g.total_checks > 0" :type="rateType(g.health_percent)" size="small" effect="dark">
                {{ g.health_percent }}%
              </el-tag>
            </div>
            <div class="op-heatmap-bar">
              <HealthTimeline :timeline="g.timeline" lookback="6h" />
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ═══════════════════════════════════════════
         双栏：模型成本排行 + 最近状态变更
    ═══════════════════════════════════════════ -->
    <div class="op-two-col">
      <!-- 模型成本排行（参考 sub2api UsageTable） -->
      <div class="op-section">
        <div class="op-section__header">
          <span class="op-section__title">模型成本排行（近 30 天）</span>
          <span class="op-section__count">Top {{ topModels.length }}</span>
        </div>
        <div class="op-table-wrap" v-loading="modelsLoading">
          <el-table :data="topModels" size="small" :header-cell-style="TABLE_HEADER" style="width:100%">
            <el-table-column type="index" label="#" width="40" />
            <el-table-column prop="model" label="模型名称" min-width="160" show-overflow-tooltip />
            <el-table-column label="调用次数" width="110" align="right">
              <template #default="{ row }">
                <div class="op-bar-cell">
                  <span>{{ fmtNum(row.calls) }}</span>
                  <div class="op-minibar"><div class="op-minibar__fill op-minibar__fill--blue" :style="{ width: pct(row.calls, maxCalls) + '%' }" /></div>
                </div>
              </template>
            </el-table-column>
            <el-table-column label="成本" width="110" align="right">
              <template #default="{ row }">
                <div class="op-bar-cell">
                  <span class="c-green fw6">{{ formatUSD(row.cost) }}</span>
                  <div class="op-minibar"><div class="op-minibar__fill op-minibar__fill--red" :style="{ width: pct(row.cost, maxCostVal) + '%' }" /></div>
                </div>
              </template>
            </el-table-column>
          </el-table>
          <div v-if="!modelsLoading && topModels.length === 0" class="op-empty">
            <p>暂无数据</p>
          </div>
        </div>
      </div>

      <!-- 最近状态变更 -->
      <div class="op-section">
        <div class="op-section__header">
          <span class="op-section__title">最近状态变更</span>
          <el-button size="small" text @click="$router.push('/admin/model-status-logs')">查看全部</el-button>
        </div>
        <div class="op-table-wrap" v-loading="logsLoading">
          <el-table :data="recentLogs" size="small" :header-cell-style="TABLE_HEADER" style="width:100%">
            <el-table-column label="时间" width="115">
              <template #default="{ row }">
                <span class="op-time">{{ fmtDateTime(row.created_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="model_name" label="模型" min-width="110" show-overflow-tooltip />
            <el-table-column label="类型" width="100">
              <template #default="{ row }">
                <el-tag :type="changeTypeTag(row.change_type)" size="small" effect="plain">
                  {{ changeTypeLabel(row.change_type) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="reason" label="原因" min-width="120" show-overflow-tooltip />
          </el-table>
          <div v-if="!logsLoading && recentLogs.length === 0" class="op-empty">
            <p>暂无记录</p>
          </div>
        </div>
      </div>
    </div>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { Refresh, Connection, Cpu, Key, Collection } from '@element-plus/icons-vue'
import {
  Chart, BarController, LineController, BarElement, LineElement,
  CategoryScale, LinearScale, PointElement, Tooltip, Legend, Filler
} from 'chart.js'
import http from '@/services/http'
import {
  getOpsStatus,
  getHealthTimeline,
} from '@/services/control-plane'
import { getPerformance, type TimelineGroup, type PerformanceItem } from '@/services/control-plane'
import { getModelStatusLogs } from '@/services/control-plane'
import { formatUSD, formatTokens } from '@/utils/format'
import HealthTimeline from '@/components/admin/HealthTimeline.vue'

Chart.register(
  BarController, LineController, BarElement, LineElement,
  CategoryScale, LinearScale, PointElement, Tooltip, Legend, Filler,
)

const TABLE_HEADER = {
  background: 'var(--el-fill-color-light)',
  fontWeight: '600',
  color: 'var(--el-text-color-secondary)',
  fontSize: '12px',
  borderBottom: '1px solid var(--el-border-color-light)',
}

const CHART_C = {
  req:   '#E0A100',
  cost:  '#36B34E',
}

// ── 全局状态 ──
const loading = ref(false)
const statsLoading = ref(false)
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null
const autoRefresh = ref(true)

// ── Stats (from ops/status) ──
const stats = ref({
  providers: { total: 0, enabled: 0, disabled: 0 },
  models: { total: 0, enabled: 0, disabled: 0 },
  keys: { total: 0, enabled: 0, disabled: 0 },
  groups: { total: 0, public: 0, controlled: 0 },
  health_check: { enabled: false, running: false, recent: [] as any[] },
})

// ── Today Snapshot ──
const todayStr = new Date().toISOString().split('T')[0]
const snapshotLoading = ref(false)
const todayStats = ref({ calls: 0, tokens: 0, cost: 0, errors: 0, successRate: 100 })

// ── Trend Chart ──
const chartPeriod = ref<'week' | 'month'>('month')
const trendLoading = ref(false)
const trendData = ref<any[]>([])
const trendCanvasRef = ref<HTMLCanvasElement | null>(null)
const costCanvasRef = ref<HTMLCanvasElement | null>(null)
let trendChart: Chart | null = null
let costChart: Chart | null = null

// ── Performance ──
const perfView = ref<'group' | 'model'>('group')
const perfLoading = ref(false)
const perfItems = ref<PerformanceItem[]>([])

// ── Health Heatmap ──
const heatLoading = ref(false)
const heatGroups = ref<TimelineGroup[]>([])

// ── Top Models ──
const modelsLoading = ref(false)
const topModels = ref<any[]>([])

// ── Recent Logs ──
const logsLoading = ref(false)
const recentLogs = ref<any[]>([])

// ── Derived ──
const successRateClass = computed(() => {
  const rate = todayStats.value.successRate
  if (rate >= 99) return 'c-green'
  if (rate >= 95) return 'c-orange'
  return 'c-red'
})

const systemHealthLabel = computed(() => {
  if (!stats.value.health_check.enabled) return '未启用检测'
  if (stats.value.health_check.running) return '检测运行中'
  return '已暂停'
})

const systemHealthTag = computed(() => {
  if (!stats.value.health_check.enabled) return 'info'
  if (stats.value.health_check.running) return 'success'
  return 'warning'
})

const maxCalls = computed(() => Math.max(...topModels.value.map((m: any) => m.calls || 0), 1))
const maxCostVal = computed(() => Math.max(...topModels.value.map((m: any) => m.cost || 0), 0.01))
const pct = (v: number, max: number) => Math.min(Math.round((v / max) * 100), 100)

// ── Formatters ──
const fmtNum = (n?: number) => (n ?? 0).toLocaleString()

function perfRateClass(rate: number) {
  const pct = rate * 100
  if (pct >= 99) return 'c-green'
  if (pct >= 95) return 'c-orange'
  return 'c-red'
}

function rateType(pct: number): 'success' | 'warning' | 'danger' {
  if (pct >= 95) return 'success'
  if (pct >= 80) return 'warning'
  return 'danger'
}

function changeTypeLabel(type: string): string {
  const map: Record<string, string> = {
    auto_disabled: '自动禁用',
    auto_recovered: '自动恢复',
    manual_disabled: '手动禁用',
    manual_enabled: '手动启用',
  }
  return map[type] || type
}

function changeTypeTag(type: string): 'danger' | 'success' | 'warning' | 'info' {
  const map: Record<string, 'danger' | 'success' | 'warning' | 'info'> = {
    auto_disabled: 'danger',
    auto_recovered: 'success',
    manual_disabled: 'warning',
    manual_enabled: 'info',
  }
  return map[type] || 'info'
}

function fmtDateTime(ts: string): string {
  const d = new Date(ts)
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hour = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  return `${month}-${day} ${hour}:${min}`
}

// ── Auto-refresh ──
function toggleAutoRefresh(val: boolean) {
  if (val) {
    autoRefreshTimer = setInterval(reloadAll, 60000)
  } else {
    if (autoRefreshTimer) { clearInterval(autoRefreshTimer); autoRefreshTimer = null }
  }
}

onBeforeUnmount(() => {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer)
  if (trendChart) { trendChart.destroy(); trendChart = null }
  if (costChart) { costChart.destroy(); costChart = null }
})

// ── Data Loading ──
async function loadStats() {
  statsLoading.value = true
  try {
    const data = await getOpsStatus()
    stats.value.providers = data.providers
    stats.value.models = data.models
    stats.value.keys = data.keys
    stats.value.groups = data.groups
    stats.value.health_check = data.health_check
  } catch (err) {
    console.error('Failed to load ops status:', err)
  } finally {
    statsLoading.value = false
  }
}

async function loadTodaySnapshot() {
  snapshotLoading.value = true
  try {
    const res = await http.get('/api/v1/usage', {
      params: { from: todayStr, to: todayStr, group_by: 'day' }
    })
    const rows = res.data?.data || []
    if (rows.length > 0) {
      const row = rows[0]
      const calls = row.req_count || 0
      const tokens = (row.prompt_tokens || 0) + (row.completion_tokens || 0) + (row.reasoning_tokens || 0)
      const cost = row.est_cost_usd || 0
      const errors = row.error_count || 0
      todayStats.value = {
        calls, tokens, cost, errors,
        successRate: calls > 0 ? ((calls - errors) / calls) * 100 : 100,
      }
    }
  } catch (err) {
    console.error('Failed to load today snapshot:', err)
  } finally {
    snapshotLoading.value = false
  }
}

async function loadTrend() {
  trendLoading.value = true
  try {
    const days = chartPeriod.value === 'week' ? 7 : 30
    const end = new Date()
    const start = new Date(end)
    start.setDate(start.getDate() - days + 1)

    const res = await http.get('/api/v1/usage', {
      params: {
        from: start.toISOString().split('T')[0],
        to: end.toISOString().split('T')[0],
        group_by: 'day',
      }
    })
    trendData.value = res.data?.data || []
    await nextTick()
    renderTrendCharts()
  } catch (err) {
    console.error('Failed to load trend:', err)
  } finally {
    trendLoading.value = false
  }
}

function renderTrendCharts() {
  renderTrendChart()
  renderCostChart()
}

function renderTrendChart() {
  if (!trendCanvasRef.value) return
  if (trendChart) { trendChart.destroy(); trendChart = null }

  const labels = trendData.value.map((r: any) => {
    const d = new Date(r.stat_date || r.date)
    return `${d.getMonth() + 1}/${d.getDate()}`
  })
  const callsData = trendData.value.map((r: any) => r.req_count || 0)
  const ctx = trendCanvasRef.value.getContext('2d')
  if (!ctx) return

  trendChart = new Chart(ctx, {
    type: 'line',
    data: {
      labels,
      datasets: [{
        label: '调用次数',
        data: callsData,
        borderColor: CHART_C.req,
        backgroundColor: 'rgba(224, 161, 0, 0.1)',
        tension: 0.3,
        fill: true,
        pointRadius: 0,
        pointHoverRadius: 4,
      }],
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: (ctx) => `调用: ${ctx.parsed.y.toLocaleString()} 次` } }
      },
      scales: {
        x: { grid: { display: false } },
        y: { beginAtZero: true, grid: { color: 'rgba(0,0,0,0.04)' } },
      },
    },
  })
}

function renderCostChart() {
  if (!costCanvasRef.value) return
  if (costChart) { costChart.destroy(); costChart = null }

  const labels = trendData.value.map((r: any) => {
    const d = new Date(r.stat_date || r.date)
    return `${d.getMonth() + 1}/${d.getDate()}`
  })
  const costData = trendData.value.map((r: any) => r.est_cost_usd || 0)
  const ctx = costCanvasRef.value.getContext('2d')
  if (!ctx) return

  costChart = new Chart(ctx, {
    type: 'line',
    data: {
      labels,
      datasets: [{
        label: '成本 (USD)',
        data: costData,
        borderColor: CHART_C.cost,
        backgroundColor: 'rgba(54, 179, 78, 0.1)',
        tension: 0.3,
        fill: true,
        pointRadius: 0,
        pointHoverRadius: 4,
      }],
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: (ctx) => `成本: ${formatUSD(ctx.parsed.y)}` } }
      },
      scales: {
        x: { grid: { display: false } },
        y: { beginAtZero: true, grid: { color: 'rgba(0,0,0,0.04)' } },
      },
    },
  })
}

async function loadPerformance() {
  perfLoading.value = true
  try {
    const result = await getPerformance('1h', perfView.value, '', 20, 0)
    perfItems.value = result.items || []
  } catch (err) {
    console.error('Failed to load performance:', err)
  } finally {
    perfLoading.value = false
  }
}

async function loadHeatmap() {
  heatLoading.value = true
  try {
    const result = await getHealthTimeline('6h', '', 0, 6)
    heatGroups.value = (result.groups || []).slice(0, 6)
  } catch (err) {
    console.error('Failed to load heatmap:', err)
  } finally {
    heatLoading.value = false
  }
}

async function loadTopModels() {
  modelsLoading.value = true
  try {
    const end = new Date()
    const start = new Date(end)
    start.setDate(start.getDate() - 29)

    const res = await http.get('/api/v1/usage', {
      params: {
        from: start.toISOString().split('T')[0],
        to: end.toISOString().split('T')[0],
        group_by: 'model',
        page: 1,
        page_size: 10,
      }
    })
    const rows = res.data?.data || []
    topModels.value = rows
      .sort((a: any, b: any) => (b.est_cost_usd || 0) - (a.est_cost_usd || 0))
      .slice(0, 10)
      .map((r: any) => ({
        model: r.model || '—',
        calls: r.req_count || 0,
        cost: r.est_cost_usd || 0,
      }))
  } catch (err) {
    console.error('Failed to load top models:', err)
  } finally {
    modelsLoading.value = false
  }
}

async function loadRecentLogs() {
  logsLoading.value = true
  try {
    const res = await getModelStatusLogs({ page: 1, page_size: 8 })
    recentLogs.value = res.items || []
  } catch (err) {
    console.error('Failed to load recent logs:', err)
  } finally {
    logsLoading.value = false
  }
}

async function reloadAll() {
  loading.value = true
  await Promise.all([
    loadStats(),
    loadTodaySnapshot(),
    loadTrend(),
    loadPerformance(),
    loadHeatmap(),
    loadTopModels(),
    loadRecentLogs(),
  ])
  loading.value = false
}

onMounted(() => {
  reloadAll()
  if (autoRefresh.value) {
    autoRefreshTimer = setInterval(reloadAll, 60000)
  }
})
</script>

<style scoped>
/* ── 调色板 ── */
.op {
  --op-input:  #36B34E;
  --op-output: #0EA371;
  --op-cache:  #5DADE2;
  --op-cost:   #C7900A;
  --op-reason: #8B5CF6;
  --op-danger: #E74C3C;
  --op-warn:   #E0A100;

  --op-surface: var(--el-fill-color-blank);
  --op-border:  var(--el-border-color-light);
  --op-muted:   var(--el-text-color-secondary);
  --op-faint:   var(--el-text-color-placeholder);
  --op-ink:     var(--el-text-color-regular);
  --op-mono: 'SFMono-Regular', ui-monospace, 'JetBrains Mono', Consolas, monospace;

  min-height: 100vh; box-sizing: border-box;
  display: flex; flex-direction: column; gap: 16px;
  overflow-y: auto;
}

/* ── 页头 ── */
.op-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.op-header__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.op-header__right { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.op-title {
  font-size: 20px; font-weight: 700; margin: 0; letter-spacing: .2px;
  color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 10px;
}
.op-title::before {
  content: ''; width: 4px; height: 18px; border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1F8A3B));
}
.op-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}
.op-auto-switch { --el-switch-off-color: var(--el-text-color-placeholder); }

/* ── 卡片通用 ── */
.op-stat-card, .op-chart-card, .op-section {
  background: var(--op-surface);
  border: 1px solid var(--op-border);
  border-radius: 14px;
}

/* ── 统计卡片 ── */
.op-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 14px; }
@media (max-width: 1100px) { .op-stats { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 560px)  { .op-stats { grid-template-columns: 1fr 1fr; } }

.op-stat-card {
  position: relative; overflow: hidden;
  padding: 18px; display: flex; align-items: flex-start; gap: 14px;
  min-width: 0; transition: box-shadow .25s ease, transform .25s ease, border-color .25s ease;
}
.op-stat-card::after {
  content: ''; position: absolute; inset: 0 0 auto 0; height: 3px;
  background: var(--accent, var(--el-color-primary)); opacity: .9;
}
.op-stat-card:hover {
  box-shadow: 0 8px 24px rgba(54, 179, 78, .14);
  transform: translateY(-2px);
  border-color: var(--el-color-primary-light-5);
}

.op-stat-card__icon {
  width: 46px; height: 46px; border-radius: 13px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.op-stat-card__icon--blue   { background: color-mix(in srgb, var(--op-input) 14%, transparent);  color: var(--op-input); }
.op-stat-card__icon--amber  { background: color-mix(in srgb, var(--op-warn) 16%, transparent);   color: var(--op-warn); }
.op-stat-card__icon--cyan   { background: color-mix(in srgb, var(--op-cache) 16%, transparent);  color: var(--op-cache); }
.op-stat-card__icon--green  { background: color-mix(in srgb, var(--op-output) 14%, transparent); color: var(--op-output); }

.op-stat-card:nth-child(1) { --accent: var(--op-input); }
.op-stat-card:nth-child(2) { --accent: var(--op-warn); }
.op-stat-card:nth-child(3) { --accent: var(--op-cache); }
.op-stat-card:nth-child(4) { --accent: var(--op-output); }

.op-stat-card__body { min-width: 0; flex: 1; }
.op-stat-card__label { font-size: 12px; color: var(--op-muted); margin: 0 0 6px; white-space: nowrap; }
.op-stat-card__value {
  font-size: 25px; font-weight: 700; color: var(--el-text-color-primary);
  margin: 0 0 6px; line-height: 1.15; letter-spacing: -.3px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.op-stat-card__sub { font-size: 12px; color: var(--op-muted); margin: 0; display: flex; gap: 5px; flex-wrap: wrap; align-items: center; }

/* ── 区块 ── */
.op-section { overflow: hidden; }
.op-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-bottom: 1px solid var(--op-border); flex-wrap: wrap; gap: 8px;
}
.op-section__title {
  font-size: 14px; font-weight: 600; color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 8px;
}
.op-section__title::before {
  content: ''; width: 7px; height: 7px; border-radius: 999px;
  background: var(--el-color-primary);
}
.op-section__count { font-size: 12px; color: var(--op-muted); }

/* ── 今日快照 ── */
.op-snapshot-body { padding: 20px; }
.op-snapshot-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 24px; }
@media (max-width: 768px) { .op-snapshot-grid { grid-template-columns: repeat(2, 1fr); } }
.op-snapshot-item { text-align: center; }
.op-snapshot-label { font-size: 12px; color: var(--op-muted); margin: 0 0 10px; }
.op-snapshot-value {
  font-size: 28px; font-weight: 700; margin: 0 0 4px; line-height: 1;
  font-variant-numeric: tabular-nums;
}
.op-snapshot-sub { font-size: 11px; color: var(--op-faint); margin: 0; }

/* ── 图表 ── */
.op-charts { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
@media (max-width: 900px) { .op-charts { grid-template-columns: 1fr; } }
.op-chart-card { overflow: hidden; }
.op-chart-card__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-bottom: 1px solid var(--op-border); flex-wrap: wrap; gap: 8px;
}
.op-chart-card__title {
  font-size: 14px; font-weight: 600; color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 8px;
}
.op-chart-card__title::before {
  content: ''; width: 7px; height: 7px; border-radius: 999px;
  background: var(--el-color-primary);
}
.op-chart-card__body { padding: 14px 16px; height: 200px; position: relative; }
.op-chart-empty {
  position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%);
  color: var(--op-faint); font-size: 13px;
}

/* ── 性能表格 ── */
.op-perf-overview { overflow-x: auto; }
.op-model-name { font-size: 13px; font-weight: 500; color: var(--el-text-color-regular); }

/* ── 健康热力图 ── */
.op-heatmap { padding: 16px; }
.op-heatmap-list { display: flex; flex-direction: column; gap: 10px; }
.op-heatmap-row {
  display: flex; align-items: center; gap: 12px;
  padding: 8px 12px;
  background: var(--el-fill-color-light);
  border-radius: 10px;
}
.op-heatmap-meta { display: flex; align-items: center; gap: 6px; flex-shrink: 0; min-width: 150px; }
.op-heatmap-tag { max-width: 140px; }
.op-heatmap-bar { flex: 1; min-width: 0; }

/* ── 双栏 ── */
.op-two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
@media (max-width: 1024px) { .op-two-col { grid-template-columns: 1fr; } }

/* ── 表格 ── */
.op-table-wrap { overflow-x: auto; }
.op-table-wrap :deep(.el-table) {
  --el-table-border-color: var(--op-border);
  --el-table-bg-color: var(--op-surface);
  --el-table-tr-bg-color: var(--op-surface);
  --el-table-row-hover-bg-color: var(--el-color-primary-light-9);
  background: var(--op-surface);
}
.op-table-wrap :deep(.el-table td.el-table__cell) {
  background: var(--op-surface);
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.op-table-wrap :deep(.el-table__row:hover > td.el-table__cell) { background: var(--el-color-primary-light-9) !important; }

/* ── Mini-bar ── */
.op-bar-cell { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
.op-minibar { width: 100%; height: 4px; background: var(--el-fill-color); border-radius: 999px; overflow: hidden; }
.op-minibar__fill { height: 100%; border-radius: 999px; transition: width .4s ease; }
.op-minibar__fill--blue  { background: linear-gradient(90deg, color-mix(in srgb, var(--op-input) 45%, transparent), var(--op-input)); }
.op-minibar__fill--red   { background: linear-gradient(90deg, color-mix(in srgb, var(--op-cost) 45%, transparent), var(--op-cost)); }

/* ── 时间 / 等宽 ── */
.op-time { font-size: 12px; color: var(--op-muted); white-space: nowrap; font-family: var(--op-mono); }
.op-mono { font-family: var(--op-mono); font-size: 11px; color: var(--op-muted); }

/* ── 空态 ── */
.op-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 48px 24px; }
.op-empty p { font-size: 13px; color: var(--op-faint); margin: 0; }

/* ── 颜色 ── */
.c-blue   { color: var(--op-input); }
.c-green  { color: var(--op-output); }
.c-cyan   { color: var(--op-cache); }
.c-orange { color: var(--op-warn); }
.c-amber  { color: var(--op-warn); }
.c-purple { color: var(--op-reason); }
.c-red    { color: var(--op-danger); }
.c-gray   { color: var(--op-muted); }
.fw6      { font-weight: 600; }

/* ── Mobile ── */
@media (max-width: 640px) {
  .op-header { flex-direction: column; gap: 8px; }
  .op-header__right { width: 100%; justify-content: flex-end; }
  .op-title { font-size: 16px; }
  .op-stats { grid-template-columns: 1fr !important; }
  .op-two-col { grid-template-columns: 1fr; }
  .op-section__header { flex-direction: column; align-items: flex-start; }
  .op-chart-card__header { flex-direction: column; align-items: flex-start; }
  .op-chart-card__body { height: 160px; }
  .op-heatmap-row { flex-direction: column; align-items: flex-start; gap: 8px; }
  .op-heatmap-meta { min-width: 0; }
}
</style>