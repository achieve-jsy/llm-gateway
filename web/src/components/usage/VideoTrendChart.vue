<template>
  <div class="video-chart-card" v-loading="loading">
    <div class="video-chart-card__header">
      <span class="video-chart-card__title">视频任务趋势</span>
      <span class="video-chart-card__hint">任务量 · 成功率</span>
    </div>
    <div class="video-chart-card__body">
      <canvas v-show="rows.length" ref="canvasRef" />
      <el-empty v-if="!loading && !rows.length" description="暂无趋势数据" :image-size="72" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  Chart, BarController, LineController, CategoryScale, LinearScale,
  BarElement, LineElement, PointElement, Tooltip, Legend,
} from 'chart.js'
import { videoTasksAPI, type VideoTaskTrendRow } from '@/api/videoTasks'
import { fmtDate } from '@/utils/time'

Chart.register(
  BarController, LineController, CategoryScale, LinearScale,
  BarElement, LineElement, PointElement, Tooltip, Legend,
)

const props = withDefaults(defineProps<{
  refreshToken?: number
  start?: string
  end?: string
  mine?: boolean
  providerName?: string
  modelName?: string
}>(), {
  mine: false,
})

const loading = ref(false)
const rows = ref<VideoTaskTrendRow[]>([])
const canvasRef = ref<HTMLCanvasElement | null>(null)
let chart: Chart | null = null

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function chartTheme() {
  return {
    primary: cssVar('--el-color-primary'),
    success: cssVar('--el-color-success'),
    text: cssVar('--el-text-color-regular'),
    muted: cssVar('--el-text-color-secondary'),
    grid: cssVar('--el-border-color-lighter'),
  }
}

function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

function renderChart() {
  const canvas = canvasRef.value
  if (!canvas) return
  if (!rows.value.length) {
    chart?.destroy()
    chart = null
    return
  }
  chart?.destroy()

  const theme = chartTheme()
  const sorted = [...rows.value].sort((a, b) => a.date.localeCompare(b.date))
  const labels = sorted.map(row => fmtDate(row.date))
  const successRates = sorted.map(row => row.tasks > 0 ? Number(((row.succeeded / row.tasks) * 100).toFixed(1)) : 0)

  chart = new Chart(canvas, {
    type: 'bar',
    data: {
      labels,
      datasets: [
        {
          type: 'bar',
          label: '任务数',
          data: sorted.map(row => row.tasks),
          yAxisID: 'y',
          backgroundColor: theme.primary,
          borderRadius: 6,
          maxBarThickness: 28,
        },
        {
          type: 'line',
          label: '成功率',
          data: successRates,
          yAxisID: 'y1',
          borderColor: theme.success,
          backgroundColor: theme.success,
          borderWidth: 2,
          tension: 0.35,
          pointRadius: sorted.length > 20 ? 0 : 3,
          pointHoverRadius: 5,
        },
      ],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: 'index' },
      plugins: {
        legend: {
          position: 'top',
          labels: { color: theme.text, usePointStyle: true, padding: 14, font: { size: 11 } },
        },
        tooltip: {
          callbacks: {
            label: ctx => {
              if (ctx.datasetIndex === 1) return `成功率: ${ctx.raw}%`
              return `任务数: ${formatNum(Number(ctx.raw || 0))}`
            },
          },
        },
      },
      scales: {
        x: {
          grid: { color: theme.grid },
          border: { display: false },
          ticks: { color: theme.muted, font: { size: 10 }, maxRotation: 30 },
        },
        y: {
          type: 'linear',
          position: 'left',
          beginAtZero: true,
          grid: { color: theme.grid },
          border: { display: false },
          ticks: { color: theme.muted, font: { size: 10 }, callback: (value: any) => formatNum(Number(value)) },
        },
        y1: {
          type: 'linear',
          position: 'right',
          beginAtZero: true,
          min: 0,
          max: 100,
          grid: { drawOnChartArea: false },
          border: { display: false },
          ticks: { color: theme.muted, font: { size: 10 }, callback: (value: any) => `${value}%` },
        },
      },
    },
  } as any)
}

async function load() {
  loading.value = true
  try {
    rows.value = await videoTasksAPI.trend({
      start: props.start,
      end: props.end,
      provider_name: props.providerName,
      model_name: props.modelName,
    }, { mine: props.mine })
  } catch {
    rows.value = []
  } finally {
    loading.value = false
  }
  await nextTick()
  renderChart()
}

watch(() => [props.refreshToken, props.start, props.end, props.mine, props.providerName, props.modelName], () => load())
onMounted(load)
onUnmounted(() => chart?.destroy())
</script>

<style scoped>
.video-chart-card {
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;
  margin-bottom: 14px;
}
.video-chart-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--el-border-color-light);
}
.video-chart-card__title {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.video-chart-card__title::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: var(--el-color-primary);
}
.video-chart-card__hint {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.video-chart-card__body {
  height: 260px;
  padding: 12px 16px;
  position: relative;
}
</style>
