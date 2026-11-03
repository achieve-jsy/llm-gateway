<template>
  <div class="urb">
    <div class="urb__chart-wrap" v-loading="loading">
      <canvas ref="canvasRef" class="urb__canvas" />
      <div v-if="!loading && !hasData" class="urb__empty">暂无成本数据</div>
    </div>

    <div class="urb__summary">
      <div class="urb__total">
        <span class="urb__total-label">本周总成本</span>
        <span class="urb__total-val">{{ fmtCost(thisTotal) }}</span>
        <el-tag v-if="hasPrev" :type="momType(totalMom)" size="small" effect="light">
          {{ momText(totalMom) }}
        </el-tag>
        <el-tag v-else size="small" effect="plain">无上期</el-tag>
      </div>

      <div v-if="hasPrev" class="urb__buckets">
        <span v-for="b in buckets" :key="b.key" class="urb__bucket">
          <span class="urb__bucket-dot" :class="`urb__dot--${b.color}`"></span>
          <span class="urb__bucket-label">{{ b.label }}</span>
          <span class="urb__bucket-mom">{{ momText(b.mom) }}</span>
        </span>
      </div>
      <p v-else class="urb__no-prev">无上一周期数据，仅展示本周绝对构成。</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { Chart, registerables } from 'chart.js'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, type CurrencyConfig, formatCost } from '@/utils/currency'
import type { CostBreakdown } from '@/types/usage-report'

Chart.register(...registerables)

const props = withDefaults(defineProps<{
  costBreakdown: CostBreakdown
  currency?: Currency
  loading?: boolean
}>(), { currency: 'CNY', loading: false })

const settingsStore = useSettingsStore()
const cc = computed<CurrencyConfig>(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const canvasRef = ref<HTMLCanvasElement | null>(null)
let inst: Chart | null = null

const thisTotal = computed(() =>
  (props.costBreakdown.uncached_input_cost_usd || 0) +
  (props.costBreakdown.cache_cost_usd || 0) +
  (props.costBreakdown.output_cost_usd || 0),
)
const prev = computed(() => props.costBreakdown.prev)
// 环比按「未换算美元」比值更高稳定；展示按报告币种换算
const hasPrev = computed(() => !!prev.value)
const hasData = computed(() => thisTotal.value > 0)

function pct(delta: number): string {
  return `${delta >= 0 ? '+' : ''}${(delta * 100).toFixed(1)}%`
}
function momText(delta: number | null): string {
  if (delta === null) return '无上期'
  return pct(delta)
}
function momType(delta: number | null): 'success' | 'warning' | 'danger' | 'info' {
  if (delta === null) return 'info'
  if (delta > 0.02) return 'danger' // 成本上升
  if (delta < -0.02) return 'success'
  return 'info'
}

// 成本三分桶：本周值与上周值，及各自环比
interface BucketView {
  key: string
  label: string
  color: 'green' | 'blue' | 'orange'
  thisVal: number
  prevVal: number | null
  mom: number | null
}
const buckets = computed<BucketView[]>(() => {
  const p = prev.value
  const defs = [
    { key: 'uncached_input', label: '非缓存输入', color: 'green' as const, field: 'uncached_input_cost_usd' as const },
    { key: 'cache', label: '缓存', color: 'blue' as const, field: 'cache_cost_usd' as const },
    { key: 'output', label: '输出', color: 'orange' as const, field: 'output_cost_usd' as const },
  ]
  return defs.map(d => {
    const thisVal = props.costBreakdown[d.field] || 0
    const prevVal = p ? (p[d.field] ?? 0) : null
    const mom = prevVal && prevVal !== 0 ? (thisVal - prevVal) / prevVal : (prevVal === null ? null : (thisVal === 0 ? 0 : Infinity))
    return { key: d.key, label: d.label, color: d.color, thisVal, prevVal, mom: mom === Infinity ? null : mom }
  })
})

const totalMom = computed<number | null>(() => {
  const p = prev.value
  if (!p) return null
  const prevTotal = (p.uncached_input_cost_usd || 0) + (p.cache_cost_usd || 0) + (p.output_cost_usd || 0)
  if (prevTotal === 0) return null
  return (thisTotal.value - prevTotal) / prevTotal
})

function fmtCost(v: number): string {
  return formatCost(v, cc.value)
}
// ── 本周 vs 上周 堆叠条 ───────────────────────────────────────
function renderChart() {
  if (!canvasRef.value) return
  inst?.destroy()
  if (!hasData.value) { inst = null; return }

  const p = prev.value
  const weeks = p ? ['本周', '上周'] : ['本周']
  const colorMap = {
    green: { border: '#36B34E', bg: 'rgba(54,179,78,0.80)' },
    blue: { border: '#409EFF', bg: 'rgba(64,158,255,0.80)' },
    orange: { border: '#E6A23C', bg: 'rgba(230,162,60,0.80)' },
  }

  const datasets = buckets.value.map(b => ({
    label: b.label,
    data: p ? [b.thisVal, b.prevVal ?? 0] : [b.thisVal],
    backgroundColor: colorMap[b.color].bg,
    borderColor: colorMap[b.color].border,
    borderWidth: 1,
    borderRadius: 3,
  }))

  inst = new Chart(canvasRef.value, {
    type: 'bar',
    data: { labels: weeks, datasets: datasets as any },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      scales: {
        x: { stacked: true, grid: { display: false }, ticks: { font: { size: 11 } } },
        y: {
          stacked: true,
          grid: { color: 'rgba(0,0,0,0.05)' },
          ticks: { font: { size: 11 }, callback: (v: any) => fmtCost(Number(v)) },
        },
      },
      plugins: {
        legend: { position: 'bottom', labels: { boxWidth: 12, padding: 16, font: { size: 12 } } },
        tooltip: {
          callbacks: {
            label: (ctx: any) => ` ${ctx.dataset.label}: ${fmtCost(Number(ctx.raw))}`,
          },
        },
      },
    },
  } as any)
}

watch(() => [props.costBreakdown, props.currency, props.loading], async () => {
  await nextTick()
  renderChart()
}, { deep: true })

onMounted(async () => { await nextTick(); renderChart() })
onUnmounted(() => inst?.destroy())
</script>

<style scoped>
.urb {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.urb__chart-wrap {
  position: relative;
  height: 240px;
}
.urb__canvas { width: 100% !important; height: 100% !important; }
.urb__empty {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--el-text-color-placeholder); font-size: 13px;
}
.urb__summary {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 0 2px;
}
.urb__total {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
}
.urb__total-label { font-size: 13px; color: var(--el-text-color-secondary); }
.urb__total-val { font-size: 18px; font-weight: 600; color: var(--el-text-color-primary); }
.urb__buckets {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
}
.urb__bucket {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--el-text-color-secondary);
}
.urb__bucket-dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
.urb__dot--green { background: #36B34E; }
.urb__dot--blue { background: #409EFF; }
.urb__dot--orange { background: #E6A23C; }
.urb__bucket-mom { color: var(--el-text-color-primary); }
.urb__no-prev {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
</style>
