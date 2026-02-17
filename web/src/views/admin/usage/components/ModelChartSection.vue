<template>
  <div class="ud-section">
    <div class="ud-section__header">
      <span class="ud-section__title">模型成本分布</span>
      <div class="ud-model-ctrl">
        <div class="ud-trend-tabs">
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelMetric === 'cost' }]" @click="onSwitchMetric('cost')">成本</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelMetric === 'calls' }]" @click="onSwitchMetric('calls')">调用量</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelMetric === 'tokens' }]" @click="onSwitchMetric('tokens')">Token</button>
        </div>
        <div class="ud-trend-tabs">
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelChartType === 'bar' }]" @click="onSwitchChartType('bar')">柱状图</button>
          <button :class="['ud-trend-tab', { 'ud-trend-tab--active': props.modelChartType === 'doughnut' }]" @click="onSwitchChartType('doughnut')">饼图</button>
        </div>
        <span class="ud-section__hint">Top {{ Math.min(props.modelsData.length, 6) }}{{ props.modelsData.length > 6 ? ` / 共 ${props.modelsData.length} 种` : '' }}</span>
      </div>
    </div>
    <div class="ud-chart-wrap ud-chart-wrap--model" v-loading="props.loading">
      <canvas ref="modelCanvasRef" class="ud-model-canvas" />
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
}>()

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const modelCanvasRef = ref<HTMLCanvasElement | null>(null)
let modelChartInst: Chart | null = null

const MODEL_COLORS = [
  '#409eff', '#67c23a', '#e6a23c', '#f56c6c',
  '#909399', '#8B5CF6', '#06B6D4', '#F59E0B',
  '#10B981', '#EF4444',
]

function onSwitchMetric(value: string) {
  emit('update:modelMetric', value)
  emit('switchMetric', value)
}

function onSwitchChartType(value: string) {
  emit('update:modelChartType', value)
  emit('switchChartType', value)
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
  modelChartInst?.destroy()
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
    data = [...top, { model: `其他 (${rest.length} 种)`, cost: restCost, calls: restCalls, total_tokens: restTokens }]
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
    : (d.total_tokens ?? 0)
  )
  const metricLabel = metric === 'cost' ? `成本 (${cc.value.currency})` : metric === 'calls' ? '调用次数' : 'Token 消耗'
  const total = metricData.reduce((s: number, v: number) => s + v, 0)

  const fmtMetric = (v: number) =>
    metric === 'cost' ? fmtCost(v)
    : metric === 'calls' ? v.toLocaleString() + ' 次'
    : fmtTokens(v)

  if (props.modelChartType === 'doughnut') {
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
                return (chart.data.labels as string[]).map((label: string, i: number) => ({
                  text: label,
                  fillStyle: colors[i],
                  strokeStyle: colors[i],
                  hidden: meta.data[i]?.hidden,
                  index: i,
                }))
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

  // 横向柱状图模式
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
    options: {
      indexAxis: 'y' as const,
      responsive: true,
      maintainAspectRatio: false,
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
              : fmtTokens(Number(v)),
          },
        },
        y: {
          grid: { display: false },
          ticks: { font: { size: 11 } },
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
</style>