<template>
  <div class="chart-box tall">
    <canvas ref="canvasRef" />
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { Chart, registerables } from 'chart.js'
import type { ModelHourlyPoint } from '@/types/analytics'

Chart.register(...registerables)

defineOptions({ name: 'CostTrendChart' })

const props = defineProps<{
  /** 当前区间小时级数据（group_by=model） */
  hourly: ModelHourlyPoint[] | null
  /** 上一等长区间小时级数据（前段灰色） */
  prevHourly: ModelHourlyPoint[] | null
  /** 当前区间日期序列（YYYY-MM-DD，用于对齐堆叠柱） */
  currDates: string[]
  /** 上一区间日期序列 */
  prevDates: string[]
}>()

const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart | null = null

const TIER_HEX: Record<string, string> = {
  'L3-Premium': '#f97316',
  'L2-Advanced': '#8b5cf6',
  'L1-Basic': '#06b6d4',
  unclassified: '#6b6b75',
}

/** 等宽调色盘：按模型名 hash 取色，避免真实后端模型过多时颜色相撞 */
function colorOf(model: string): string {
  if (TIER_HEX[model]) return TIER_HEX[model]
  let h = 2166136261
  for (let i = 0; i < model.length; i++) {
    h ^= model.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  const hue = (h >>> 0) % 360
  return `hsl(${hue} 70% 55%)`
}

function destroy() {
  if (chart) {
    chart.destroy()
    chart = null
  }
}

function render() {
  destroy()
  const canvas = canvasRef.value
  if (!canvas) return

  const labels = [...props.prevDates, ...props.currDates].map(d => d.slice(5))
  const prevAgg: Record<string, number> = {}
  for (const r of props.prevHourly || []) {
    const date = r.hour.slice(0, 10)
    prevAgg[date] = (prevAgg[date] || 0) + r.est_cost_usd
  }

  // 模型按费用降序（堆叠时高费用在下层清晰）
  const curByModel = new Map<string, Record<string, number>>()
  for (const r of props.hourly || []) {
    const date = r.hour.slice(0, 10)
    if (!curByModel.has(r.group_key)) curByModel.set(r.group_key, {})
    const m = curByModel.get(r.group_key)!
    m[date] = (m[date] || 0) + r.est_cost_usd
  }
  const modelOrder = [...curByModel.entries()].sort(
    (a, b) =>
      Object.values(b[1]).reduce((s, v) => s + v, 0) - Object.values(a[1]).reduce((s, v) => s + v, 0),
  ).map(([m]) => m)

  const datasets: any[] = [
    {
      label: '上一区间费用',
      data: props.prevDates.map(d => (prevAgg[d] ? Math.round(prevAgg[d] * 100) / 100 : null)),
      borderColor: '#CBD5E1',
      backgroundColor: 'rgba(148,163,184,.14)',
      fill: true,
      pointRadius: 0,
      borderWidth: 1.5,
      borderDash: [4, 3],
      yAxisID: 'y',
    },
  ]

  for (const m of modelOrder) {
    const agg = curByModel.get(m)!
    const data = props.currDates.map(d => (agg[d] ? Math.round(agg[d] * 100) / 100 : null))
    if (!data.some((v: number | null) => v)) continue
    datasets.push({
      label: m,
      data,
      borderColor: 'transparent',
      backgroundColor: colorOf(m) + 'cc',
      stack: 'cur',
      barPercentage: 0.6,
      categoryPercentage: 0.7,
      order: 1,
      yAxisID: 'y',
    })
  }

  if (datasets.length <= 1) {
    canvas.parentElement!.innerHTML = '<div class="meta" style="padding:60px;text-align:center;color:var(--el-text-color-secondary)">暂无数据</div>'
    return
  }

  chart = new Chart(canvas, {
    type: 'bar',
    data: { labels, datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: {
        legend: {
          labels: {
            boxWidth: 10, font: { size: 11 },
            color: '#64748B',
            padding: 14,
            filter: (item: any) => item.text !== '上一区间费用',
          },
        },
        tooltip: { callbacks: { label: (c: any) => ' ' + '$' + Number(c.parsed.y ?? 0).toFixed(2) } },
      },
      scales: {
        x: {
          stacked: true,
          grid: { display: false },
          ticks: { color: '#64748B', font: { size: 11 } },
        },
        y: {
          beginAtZero: true,
          grid: { color: 'rgba(100,116,139,.12)' },
          ticks: { color: '#64748B', font: { size: 10 }, callback: (v: any) => '$' + v },
        },
      },
    },
  })
}

watch(() => [props.hourly, props.prevHourly, props.currDates, props.prevDates], render)
onMounted(render)
onUnmounted(destroy)
</script>

<style scoped>
.chart-box.tall { height: 260px; position: relative; }
</style>
