<template>
  <div class="video-dist-grid" v-loading="loading">
    <div class="video-dist-card">
      <div class="video-dist-card__header">
        <span class="video-dist-card__title">渠道分布</span>
      </div>
      <div class="video-dist-card__body">
        <canvas v-show="data.providers.length" ref="providerCanvas" />
        <el-empty v-if="!loading && !data.providers.length" description="暂无渠道数据" :image-size="64" />
      </div>
    </div>

    <div class="video-dist-card">
      <div class="video-dist-card__header">
        <span class="video-dist-card__title">状态分布</span>
      </div>
      <div class="video-dist-card__body">
        <canvas v-show="data.statuses.length" ref="statusCanvas" />
        <el-empty v-if="!loading && !data.statuses.length" description="暂无状态数据" :image-size="64" />
      </div>
    </div>

    <div class="video-dist-card">
      <div class="video-dist-card__header">
        <span class="video-dist-card__title">模型分布</span>
        <span class="video-dist-card__hint">Top 20</span>
      </div>
      <div class="video-dist-card__body">
        <canvas v-show="data.models.length" ref="modelCanvas" />
        <el-empty v-if="!loading && !data.models.length" description="暂无模型数据" :image-size="64" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import {
  Chart, BarController, DoughnutController, CategoryScale, LinearScale,
  BarElement, ArcElement, Tooltip, Legend,
} from 'chart.js'
import { videoTasksAPI, type VideoTaskDistribution } from '@/api/videoTasks'

Chart.register(
  BarController, DoughnutController, CategoryScale, LinearScale,
  BarElement, ArcElement, Tooltip, Legend,
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
const data = reactive<VideoTaskDistribution>({
  providers: [],
  statuses: [],
  models: [],
})

const providerCanvas = ref<HTMLCanvasElement | null>(null)
const statusCanvas = ref<HTMLCanvasElement | null>(null)
const modelCanvas = ref<HTMLCanvasElement | null>(null)
let providerChart: Chart | null = null
let statusChart: Chart | null = null
let modelChart: Chart | null = null

function cssVar(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function palette() {
  return [
    cssVar('--el-color-primary'),
    cssVar('--el-color-success'),
    cssVar('--el-color-warning'),
    cssVar('--el-color-danger'),
    cssVar('--el-color-info'),
    cssVar('--el-color-primary-light-3'),
    cssVar('--el-color-success-light-3'),
    cssVar('--el-color-warning-light-3'),
  ].filter(Boolean)
}

function theme() {
  return {
    text: cssVar('--el-text-color-regular'),
    muted: cssVar('--el-text-color-secondary'),
    grid: cssVar('--el-border-color-lighter'),
    primary: cssVar('--el-color-primary'),
    success: cssVar('--el-color-success'),
    danger: cssVar('--el-color-danger'),
  }
}

function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    submitted: '排队中',
    running: '生成中',
    succeeded: '成功',
    failed: '失败',
  }
  return labels[status] || status || '未知'
}

function renderHorizontalBar(canvas: HTMLCanvasElement | null, labels: string[], values: number[], title: string, color: string, current: Chart | null) {
  if (!canvas) return current
  if (!values.length) {
    current?.destroy()
    return null
  }
  current?.destroy()
  const t = theme()
  return new Chart(canvas, {
    type: 'bar',
    data: {
      labels,
      datasets: [{
        label: title,
        data: values,
        backgroundColor: color,
        borderRadius: 6,
        maxBarThickness: 22,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      indexAxis: 'y',
      plugins: {
        legend: { display: false },
        tooltip: { callbacks: { label: ctx => `${formatNum(Number(ctx.raw || 0))} 个任务` } },
      },
      scales: {
        x: {
          beginAtZero: true,
          grid: { color: t.grid },
          border: { display: false },
          ticks: { color: t.muted, font: { size: 10 }, callback: (value: any) => formatNum(Number(value)) },
        },
        y: {
          grid: { display: false },
          border: { display: false },
          ticks: { color: t.text, font: { size: 11 } },
        },
      },
    },
  })
}

function renderStatus() {
  const canvas = statusCanvas.value
  if (!canvas) return
  if (!data.statuses.length) {
    statusChart?.destroy()
    statusChart = null
    return
  }
  statusChart?.destroy()
  const colors = palette()
  const t = theme()
  const labels = data.statuses.map(row => statusLabel(row.status))
  const values = data.statuses.map(row => row.count)
  const total = values.reduce((sum, n) => sum + n, 0)

  statusChart = new Chart(canvas, {
    type: 'doughnut',
    data: {
      labels,
      datasets: [{
        data: values,
        backgroundColor: colors.slice(0, values.length),
        borderWidth: 0,
      }],
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      cutout: '62%',
      plugins: {
        legend: {
          position: 'bottom',
          labels: { color: t.text, usePointStyle: true, padding: 10, font: { size: 11 } },
        },
        tooltip: {
          callbacks: {
            label: ctx => {
              const value = Number(ctx.raw || 0)
              const pct = total ? Math.round(value / total * 100) : 0
              return `${ctx.label}: ${formatNum(value)} (${pct}%)`
            },
          },
        },
      },
    },
  })
}

function renderCharts() {
  const t = theme()
  providerChart = renderHorizontalBar(
    providerCanvas.value,
    data.providers.map(row => row.provider_name || '未知渠道'),
    data.providers.map(row => row.count),
    '任务数',
    t.primary,
    providerChart,
  )
  renderStatus()
  modelChart = renderHorizontalBar(
    modelCanvas.value,
    data.models.map(row => row.model || '未知模型'),
    data.models.map(row => row.count),
    '任务数',
    t.success,
    modelChart,
  )
}

async function load() {
  loading.value = true
  try {
    const res = await videoTasksAPI.distribution({
      start: props.start,
      end: props.end,
      provider_name: props.providerName,
      model_name: props.modelName,
    }, { mine: props.mine })
    data.providers = res.providers || []
    data.statuses = res.statuses || []
    data.models = res.models || []
  } catch {
    data.providers = []
    data.statuses = []
    data.models = []
  } finally {
    loading.value = false
  }
  await nextTick()
  renderCharts()
}

watch(() => [props.refreshToken, props.start, props.end, props.mine, props.providerName, props.modelName], () => load())
onMounted(load)
onUnmounted(() => {
  providerChart?.destroy()
  statusChart?.destroy()
  modelChart?.destroy()
})
</script>

<style scoped>
.video-dist-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 14px;
}
.video-dist-card {
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;
}
.video-dist-card__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--el-border-color-light);
}
.video-dist-card__title {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.video-dist-card__title::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: var(--el-color-primary);
}
.video-dist-card__hint {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.video-dist-card__body {
  height: 240px;
  padding: 12px 16px;
  position: relative;
}
@media (max-width: 1100px) {
  .video-dist-grid {
    grid-template-columns: 1fr;
  }
}
</style>
