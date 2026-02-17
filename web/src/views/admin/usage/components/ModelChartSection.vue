<template>
  <div class="ud-section">
    <div class="ud-section__header">
      <span class="ud-section__title">模型成本分布</span>
      <div class="ud-model-ctrl">
        <div class="ud-trend-tabs">
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelMetric === 'cost' }]" @click="onSwitchMetric('cost')">成本</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelMetric === 'calls' }]" @click="onSwitchMetric('calls')">调用量</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelMetric === 'tokens' }]" @click="onSwitchMetric('tokens')">Token</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelMetric === 'error_rate' }]" @click="onSwitchMetric('error_rate')">错误率</button>
        </div>
        <div class="ud-trend-tabs">
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelChartType === 'bar' }]" @click="onSwitchChartType('bar')">柱状图</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelChartType === 'doughnut' }]" @click="onSwitchChartType('doughnut')">饼图</button>
        </div>
        <span class="ud-section__hint">Top {{ Math.min(props.modelsData.length, 6) }}{{ props.modelsData.length > 6 ? ` / 共 ${props.modelsData.length} 种` : '' }}<span v-if="hasLongTail" class="ud-longtail-legend"><span class="ud-longtail-dot"></span>长尾</span></span>
      </div>
    </div>
    <div class="ud-chart-wrap ud-chart-wrap--model" v-loading="props.loading">
      <canvas ref="modelCanvasRef" class="ud-model-canvas" @click="onCanvasClick" />
      <div v-if="!props.loading && props.modelsData.length === 0" class="ud-chart-empty">暂无数据</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted, nextTick } from 'vue'
import { Chart, registerables } from 'chart.js'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'

Chart.register(...registerables)

export interface ModelChartSectionProps {
  loading: boolean
  modelsData: any[]
  modelMetric: string
  modelChartType: string
  currency?: Currency
}

const props = withDefaults(defineProps<ModelChartSectionProps>(), {
  currency: 'CNY',
})

const emit = defineEmits<{
  'update:modelMetric': [value: string]
  'update:modelChartType': [value: string]
  switchMetric: [value: string]
  switchChartType: [value: string]
  'model-click': [modelName: string]
}>()

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const modelCanvasRef = ref<HTMLCanvasElement | null>(null)
let modelChartInst: Chart | null = null
let renderVersion = 0
let unmounted = false

const MODEL_COLORS = [
  '#409eff', '#67c23a', '#e6a23c', '#f56c6c',
  '#909399', '#8B5CF6', '#06B6D4', '#F59E0B',
  '#10B981', '#EF4444',
]

const hasLongTail = computed(() =>
  props.modelsData.some((d: any) => d.is_long_tail)
)

function onSwitchMetric(value: string) {
  emit('update:modelMetric', value)
  emit('switchMetric', value)
}

function onSwitchChartType(value: string) {
  emit('update:modelChartType', value)
  emit('switchChartType', value)
}

/** Handle canvas click → detect which model bar/slice was clicked */
function onCanvasClick(evt: MouseEvent) {
  if (!modelChartInst) return
  const elements = modelChartInst.getElementsAtEventForMode(
    evt as any, 'nearest', { intersect: true }, false
  )
  if (elements.length > 0) {
    const idx = elements[0].index
    const raw = props.modelsData
    const TOP_N = 6
    // If index is the "其他" aggregate (last item beyond top N), ignore
    if (raw.length > TOP_N && idx >= TOP_N) return
    const item = raw[idx]
    if (item?.model) emit('model-click', item.model)
  }
}

// ── Formatting helpers ──────────────────────────────────────────

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

function renderModelChart() {
  if (!modelCanvasRef.value) return
  const ver = ++renderVersion
  modelChartInst?.destroy()
  modelChartInst = null
  const raw = props.modelsData
  if (!raw.length) return

  const TOP_N = 6
  let data: any[]
  if (raw.length > TOP_N) {
    const top  = raw.slice(0, TOP_N)
    const rest = raw.slice(TOP_N)
    const restCost   = rest.reduce((s: number, d: any) => s + (d.cost ?? 0), 0)
    const restCalls  = rest.reduce((s: number, d: any) => s + (d.calls ?? 0), 0)
    const restTokens = rest.reduce((s: number, d: any) => s + (d.total_tokens ?? 0), 0)
    const restErrorCount   = rest.reduce((s: number, d: any) => s + (d.error_count ?? 0), 0)
    const restCallsTotal   = rest.reduce((s: number, d: any) => s + (d.calls ?? 0), 0)
    const restErrorRate    = restCallsTotal > 0 ? (restErrorCount / restCallsTotal) * 100 : 0
    data = [...top, { model: `其他 (${rest.length} 种)`, cost: restCost, calls: restCalls, total_tokens: restTokens, error_rate: restErrorRate }]
  } else {
    data = raw
  }

  const labels = data.map(d => {
    const m = d.model ?? ''
    return m.length > 28 ? m.slice(0, 26) + '…' : m
  })
  const colors = data.map((_: any, i: number) => MODEL_COLORS[i % MODEL_COLORS.length])

  const metric = props.modelMetric
  const metricData = data.map(d =>
    metric === 'cost' ? (d.cost ?? 0)
    : metric === 'calls' ? (d.calls ?? 0)
    : metric === 'tokens' ? (d.total_tokens ?? 0)
    : (d.error_rate ?? 0)
  )
  const metricLabel = metric === 'cost' ? `成本 (${cc.value.currency})`
    : metric === 'calls' ? '调用次数'
    : metric === 'tokens' ? 'Token 消耗'
    : '错误率'
  const total = metric === 'error_rate'
    ? 0  // error_rate doesn't sum, so no percentage
    : metricData.reduce((s: number, v: number) => s + v, 0)

  const fmtMetric = (v: number) =>
    metric === 'cost' ? fmtCost(v)
    : metric === 'calls' ? v.toLocaleString() + ' 次'
    : metric === 'tokens' ? fmtTokens(v)
    : v.toFixed(1) + '%'

  if (props.modelChartType === 'doughnut') {
    // Stale guard: a newer render was triggered before we got here
    if (ver !== renderVersion || unmounted) return
    modelChartInst = new Chart(modelCanvasRef.value, {
      type: 'doughnut',
      data: {
        labels,
        datasets: [{
          data: metricData,
          backgroundColor: colors,
          borderColor: 'rgba(255,255,255,0.8)',
          borderWidth: 2,
          hoverOffset: 8,
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            position: 'right',
            labels: {
              boxWidth: 12, padding: 10, font: { size: 11 },
              generateLabels: (chart: any) => {
                const meta = chart.getDatasetMeta(0)
                const ds = chart.data.datasets[0].data as number[]
                const sum = ds.reduce((a: number, b: number) => a + b, 0)
                return (chart.data.labels as string[]).map((label: string, i: number) => {
                  const pct = sum > 0 ? ((ds[i] / sum) * 100).toFixed(1) : '0.0'
                  // Show long-tail indicator for non-aggregate items
                  const isLongTail = i < raw.length && raw[i]?.is_long_tail
                  const prefix = isLongTail ? '● ' : ''
                  return {
                    text: prefix + label + ' (' + pct + '%)',
                    fillStyle: colors[i],
                    strokeStyle: colors[i],
                    hidden: meta.data[i]?.hidden,
                    index: i,
                  }
                })
              },
            },
          },
          tooltip: {
            callbacks: {
              label: (ctx: any) => {
                const val = ctx.raw as number
                const pct = total > 0 ? ((val / total) * 100).toFixed(1) : '0.0'
                return ` ${fmtMetric(val)} (${pct}%)`
              },
            },
          },
        },
      },
    } as any)
    return
  }

  // 横向柱状图模式 — 自定义插件：在柱子右侧绘制百分比标注
  const pctLabelPlugin = {
    id: 'pctLabels',
    afterDatasetsDraw(chart: any) {
      if (total <= 0 && metric !== 'error_rate') return
      const { ctx } = chart
      const meta = chart.getDatasetMeta(0)
      ctx.save()
      ctx.font = '11px system-ui, sans-serif'
      ctx.textBaseline = 'middle'
      meta.data.forEach((bar: any, i: number) => {
        const val = metricData[i]
        const pct = metric === 'error_rate'
          ? val.toFixed(1) + '%'
          : total > 0 ? ((val / total) * 100).toFixed(1) + '%' : ''
        // Long-tail indicator
        const isLongTail = i < raw.length && raw[i]?.is_long_tail
        const suffix = isLongTail ? ' ●' : ''
        const text = pct + suffix
        const x = bar.x + 6
        const y = bar.y
        ctx.fillStyle = isLongTail ? '#e6a23c' : 'rgba(0,0,0,0.45)'
        ctx.textAlign = 'left'
        ctx.fillText(text, x, y)
      })
      ctx.restore()
    },
  }

  // Stale guard: a newer render was triggered before we got here
  if (ver !== renderVersion || unmounted) return
  modelChartInst = new Chart(modelCanvasRef.value, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: metricLabel,
        data: metricData,
        backgroundColor: colors,
        borderRadius: 4,
        borderSkipped: false,
      }],
    },
      plugins: [pctLabelPlugin],
      options: {
      indexAxis: 'y' as const,
      responsive: true,
      maintainAspectRatio: false,
      layout: { padding: { right: 60 } },
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (ctx: any) => {
              const val = ctx.raw as number
              const pct = total > 0 ? ((val / total) * 100).toFixed(1) : '0.0'
              return ` ${fmtMetric(val)} (${pct}%)`
            },
          },
        },
      },
      scales: {
        x: {
          grid: { color: 'rgba(0,0,0,0.05)' },
          ticks: {
            font: { size: 11 },
            callback: (v: any) =>
              metric === 'cost' ? `${currencySymbol(cc.value.currency)}${convertCost(Number(v), cc.value).toFixed(2)}`
              : metric === 'calls' ? Number(v).toLocaleString()
              : metric === 'tokens' ? fmtTokens(Number(v))
              : Number(v).toFixed(1) + '%',
          },
        },
        y: {
          grid: { display: false },
          ticks: {
            font: { size: 11 },
            callback: (_v: any, idx: number) => {
              const item = idx < raw.length ? raw[idx] : null
              const prefix = item?.is_long_tail ? '● ' : ''
              return prefix + (labels[idx] || '')
            },
            color: (ctx: any) => {
              const idx = ctx.index ?? 0
              const item = idx < raw.length ? raw[idx] : null
              return item?.is_long_tail ? '#e6a23c' : '#606266'
            },
          },
        },
      },
    },
  })
}

// ── Watchers & lifecycle ───────────────────────────────────────

watch(
  () => [props.modelsData, props.modelMetric, props.modelChartType, props.currency],
  async () => {
    await nextTick()
    renderModelChart()
  },
  { deep: true },
)

onUnmounted(() => {
  unmounted = true
  modelChartInst?.destroy()
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
.ud-section__hint  { font-size: 12px; color: var(--ud-faint, var(--el-text-color-placeholder)); }

/* 趋势图指标切换 (reused from parent) */
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

/* 模型图控制栏 */
.ud-model-ctrl { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }

/* 模型图 */
.ud-chart-wrap {
  padding: 10px 14px;
  height: 280px;
  position: relative;
}
.ud-chart-wrap--model { height: 300px; }
.ud-model-canvas { width: 100% !important; height: 100% !important; }
.ud-chart-empty {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--ud-faint, var(--el-text-color-placeholder)); font-size: 13px;
}

/* 长尾模型图例 */
.ud-longtail-legend {
  display: inline-flex; align-items: center; gap: 3px;
  margin-left: 8px; font-size: 11px; color: #e6a23c;
}
.ud-longtail-dot {
  width: 6px; height: 6px; border-radius: 50%;
  background: #e6a23c; display: inline-block;
}
</style>