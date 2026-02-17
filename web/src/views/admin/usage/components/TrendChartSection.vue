<template>
  <div class="ud-section">
    <div class="ud-section__header">
      <span class="ud-section__title">调用趋势</span>
      <div class="ud-trend-header-ctrls">
        <div class="ud-trend-tabs">
          <button
            v-for="m in trendMetrics" :key="m.value"
            :class="['ud-trend-tab', { 'ud-trend-tab--active': props.trendMetric === m.value }]"
            @click="onSwitchMetric(m.value)"
          >{{ m.label }}</button>
        </div>
        <div class="ud-trend-tabs">
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.trendGranularity === 'day' }]" @click="onSwitchGranularity('day')">日</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.trendGranularity === 'week' }]" @click="onSwitchGranularity('week')">周</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.trendGranularity === 'month' }]" @click="onSwitchGranularity('month')">月</button>
        </div>
      </div>
    </div>
    <div class="ud-chart-wrap" v-loading="props.loading">
      <canvas ref="trendCanvasRef" class="ud-trend-canvas" />
      <div v-if="!props.loading && props.trendData.length === 0" class="ud-chart-empty">暂无趋势数据</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted, nextTick } from 'vue'
import { Chart, registerables } from 'chart.js'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'

Chart.register(...registerables)

export interface TrendChartSectionProps {
  loading: boolean
  trendData: any[]
  prevTrendData?: any[]
  trendMetric: string
  trendGranularity: string
  currency?: Currency
}

const props = withDefaults(defineProps<TrendChartSectionProps>(), {
  prevTrendData: () => [],
  currency: 'CNY',
})

const emit = defineEmits<{
  'update:trendMetric': [value: string]
  'update:trendGranularity': [value: string]
  switchMetric: [value: string]
  switchGranularity: [value: string]
}>()

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const trendMetrics = [
  { value: 'all', label: '综合' },
  { value: 'cost', label: '成本' },
  { value: 'calls', label: '调用量' },
  { value: 'tokens', label: 'Token' },
] as const

const trendCanvasRef = ref<HTMLCanvasElement | null>(null)
let trendChartInst: Chart | null = null

function onSwitchMetric(value: string) {
  emit('update:trendMetric', value)
  emit('switchMetric', value)
}

function onSwitchGranularity(value: string) {
  emit('update:trendGranularity', value)
  emit('switchGranularity', value)
}

// ── Helper functions ────────────────────────────────────────────

function getISOWeekLabel(date: Date): string {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  d.setDate(d.getDate() + 3 - (d.getDay() + 6) % 7)
  const week1 = new Date(d.getFullYear(), 0, 4)
  const weekNum = 1 + Math.round(((d.getTime() - week1.getTime()) / 86400000 - 3 + (week1.getDay() + 6) % 7) / 7)
  return `W${String(weekNum).padStart(2, '0')}`
}

const MONTH_ABBR = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']

function getMonthLabel(date: Date): string {
  return `${MONTH_ABBR[date.getMonth()]} ${date.getFullYear()}`
}

function aggregateTrendData(data: any[], granularity: 'day' | 'week' | 'month'): any[] {
  if (granularity === 'day' || !data.length) return data
  const groups = new Map<string, any>()
  for (const d of data) {
    const dateStr = d.date
    if (!dateStr) continue
    const date = new Date(dateStr)
    const key = granularity === 'week' ? getISOWeekLabel(date) : getMonthLabel(date)
    if (!groups.has(key)) {
      groups.set(key, { date: key, cost: 0, calls: 0, errors: 0, total_tokens: 0 })
    }
    const g = groups.get(key)!
    g.cost         += d.cost         ?? 0
    g.calls        += d.calls        ?? 0
    g.errors       += d.errors       ?? 0
    g.total_tokens += d.total_tokens ?? 0
  }
  return Array.from(groups.values())
}

// ── Formatting (adapted from parent) ────────────────────────────

function fmtCost(v?: number | null): string {
  const n = convertCost(Number(v || 0), cc.value)
  const sym = currencySymbol(cc.value.currency)
  if (n === 0) return `${sym}0.00`
  let s: string
  if (Math.abs(n) >= 1)        s = n.toFixed(2)
  else if (Math.abs(n) >= 0.01) s = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001) s = n.toFixed(6)
  else s = n.toFixed(8)
  return sym + s.replace(/(\.\d{2,}?)0+$/, '$1')
}

function fmtTokens(n?: number | null) {
  if (!n) return '0'
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}

// ── Chart rendering ────────────────────────────────────────────

function renderTrendChart() {
  if (!trendCanvasRef.value) return
  trendChartInst?.destroy()
  const g = props.trendGranularity as 'day' | 'week' | 'month'
  const data = aggregateTrendData(props.trendData, g)
  if (!data.length) return

  const isDayView = g === 'day'
  const labels     = data.map(d => isDayView ? (d.date?.slice(5) ?? '') : (d.date ?? ''))
  const costs      = data.map(d => d.cost ?? 0)
  const calls      = data.map(d => d.calls ?? 0)
  const errors     = data.map(d => d.errors ?? 0)
  const tokens     = data.map(d => d.total_tokens ?? 0)
  const showPoints = data.length <= 31
  const metric     = props.trendMetric

  const prevAggData = aggregateTrendData(props.prevTrendData ?? [], g)
  const hasPrev     = prevAggData.length > 0
  const alignPrev   = (arr: number[]) => labels.map((_, i) => i < arr.length ? arr[i] : null)

  // 综合模式：成本（折线，左轴）+ 调用（柱，右轴）
  if (metric === 'all') {
    trendChartInst = new Chart(trendCanvasRef.value, {
      type: 'bar',
      data: {
        labels,
        datasets: [
          {
            type: 'line' as const, label: '成本 (' + cc.value.currency + ')', data: costs,
            yAxisID: 'yCost', borderColor: '#67c23a', backgroundColor: 'rgba(103,194,58,0.10)',
            fill: true, tension: 0.35, pointRadius: showPoints ? 3 : 0, pointHoverRadius: 5, borderWidth: 2, order: 1,
          },
          {
            type: 'bar' as const, label: '调用次数', data: calls,
            yAxisID: 'yCalls', backgroundColor: 'rgba(64,158,255,0.45)', borderRadius: 3, order: 2,
          },
          {
            type: 'bar' as const, label: '失败次数', data: errors,
            yAxisID: 'yCalls', backgroundColor: 'rgba(245,108,108,0.7)', borderRadius: 3, order: 3,
          },
        ],
      },
      options: {
        responsive: true, maintainAspectRatio: false,
        interaction: { intersect: false, mode: 'index' },
        scales: {
          x: { grid: { display: false }, ticks: { maxTicksLimit: 15, font: { size: 11 } } },
          yCost: {
            type: 'linear', position: 'left',
            grid: { color: 'rgba(0,0,0,0.05)' },
            ticks: { font: { size: 11 }, callback: (v: any) => `${currencySymbol(cc.value.currency)}${convertCost(Number(v), cc.value).toFixed(2)}` },
          },
          yCalls: {
            type: 'linear', position: 'right',
            grid: { drawOnChartArea: false },
            ticks: { font: { size: 11 }, callback: (v: any) => Number(v).toLocaleString() },
          },
        },
        plugins: {
          legend: { position: 'bottom', labels: { boxWidth: 12, padding: 16, font: { size: 12 } } },
          tooltip: {
            callbacks: {
              label: (ctx: any) => ctx.dataset.yAxisID === 'yCost'
                ? ` 成本: ${fmtCost(ctx.raw as number)}`
                : ` ${ctx.dataset.label}: ${Number(ctx.raw).toLocaleString()}`,
            },
          },
        },
      },
    } as any)
    return
  }

  // 单指标模式（带上期虚线对比）
  const metricCfg = {
    cost:   { curr: costs,  prev: prevAggData.map(d => d.cost ?? 0),         color: '#67c23a', bgColor: 'rgba(103,194,58,0.12)', prevColor: 'rgba(103,194,58,0.5)', label: '成本 (' + cc.value.currency + ')',  fmtFn: (v: any) => fmtCost(v) },
    calls:  { curr: calls,  prev: prevAggData.map(d => d.calls ?? 0),         color: '#409eff', bgColor: 'rgba(64,158,255,0.15)', prevColor: 'rgba(64,158,255,0.5)', label: '调用次数',    fmtFn: (v: any) => Number(v).toLocaleString() },
    tokens: { curr: tokens, prev: prevAggData.map(d => d.total_tokens ?? 0), color: '#8b5cf6', bgColor: 'rgba(139,92,246,0.12)', prevColor: 'rgba(139,92,246,0.5)', label: 'Token 消耗',  fmtFn: (v: any) => fmtTokens(v) },
  }[metric]!

  if (!metricCfg) {
    trendChartInst = null
    return
  }

  const datasets: any[] = [
    {
      label: metricCfg.label, data: metricCfg.curr,
      borderColor: metricCfg.color, backgroundColor: metricCfg.bgColor,
      fill: true, tension: 0.35,
      pointRadius: showPoints ? 3 : 0, pointHoverRadius: 5, borderWidth: 2, order: 1,
    },
  ]

  if (hasPrev) {
    datasets.push({
      label: '上期 ' + metricCfg.label,
      data: alignPrev(metricCfg.prev),
      borderColor: metricCfg.prevColor,
      backgroundColor: 'transparent',
      borderDash: [5, 5],
      fill: false, tension: 0.35,
      pointRadius: 0, pointHoverRadius: 4, borderWidth: 1.5, order: 2,
    })
  }

  trendChartInst = new Chart(trendCanvasRef.value, {
    type: 'line',
    data: { labels, datasets },
    options: {
      responsive: true, maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      scales: {
        x: { grid: { display: false }, ticks: { maxTicksLimit: 15, font: { size: 11 } } },
        y: {
          grid: { color: 'rgba(0,0,0,0.05)' },
          ticks: { font: { size: 11 }, callback: metricCfg.fmtFn },
        },
      },
      plugins: {
        legend: { display: hasPrev, position: 'bottom', labels: { boxWidth: 12, padding: 16, font: { size: 12 } } },
        tooltip: { callbacks: { label: (ctx: any) => ` ${ctx.dataset.label}: ${metricCfg.fmtFn(ctx.raw)}` } },
      },
    },
  } as any)
}

// ── Watchers & lifecycle ───────────────────────────────────────

watch(
  () => [props.trendData, props.prevTrendData, props.trendMetric, props.trendGranularity, props.currency],
  async () => {
    await nextTick()
    renderTrendChart()
  },
  { deep: true },
)

onUnmounted(() => {
  trendChartInst?.destroy()
})
</script>

<style scoped>
/* ── Section 通用 ── */
.ud-section {
  background: var(--ud-surface, var(--el-fill-color-blank));
  border: 1px solid var(--ud-border, var(--el-border-color-light));
  border-radius: 14px;
}
.ud-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 14px 6px; border-bottom: 1px solid var(--ud-border, var(--el-border-color-light));
  flex-wrap: wrap; gap: 6px;
}
.ud-section__title { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }

/* 趋势图头部双控件行 */
.ud-trend-header-ctrls { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

/* 趋势图指标切换 */
.ud-trend-tabs { display: flex; gap: 2px; background: var(--el-fill-color); border-radius: 6px; padding: 2px; }
.ud-trend-tab {
  padding: 3px 10px; font-size: 12px; border-radius: 4px; border: none; cursor: pointer;
  background: transparent; color: var(--el-text-color-secondary); transition: all .15s;
}
.ud-trend-tab:hover { color: var(--el-text-color-primary); }
.ud-trend-tab--active {
  background: var(--el-bg-color); color: var(--el-color-primary);
  font-weight: 600; box-shadow: 0 1px 3px rgba(0,0,0,.1);
}

/* 趋势图 */
.ud-chart-wrap {
  padding: 10px 14px;
  height: 280px;
  position: relative;
}
.ud-trend-canvas { width: 100% !important; height: 100% !important; }
.ud-chart-empty {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--ud-faint, var(--el-text-color-placeholder)); font-size: 13px;
}
</style>