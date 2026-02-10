<template>
  <div class="ops-chart-card">
    <div class="ops-chart-header">
      <h3 class="ops-chart-title">
        <svg class="ops-chart-icon ops-chart-icon--red" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        错误趋势
      </h3>
      <div class="ops-chart-actions">
        <div class="ops-chart-legend">
          <span class="ops-legend-item">
            <span class="ops-legend-dot" style="background: #f59e0b"></span>4xx
          </span>
          <span class="ops-legend-item">
            <span class="ops-legend-dot" style="background: #ef4444"></span>5xx
          </span>
          <span class="ops-legend-item">
            <span class="ops-legend-dot" style="background: #f97316"></span>业务
          </span>
        </div>
        <div class="ops-chart-btns">
          <el-button size="small" plain @click="$emit('open-error-details', 'request')">请求错误</el-button>
          <el-button size="small" plain @click="$emit('open-error-details', 'upstream')">上游错误</el-button>
          <el-button v-if="hasData" size="small" plain @click="resetZoom">重置缩放</el-button>
          <el-button v-if="hasData" size="small" plain @click="downloadPNG">下载 PNG</el-button>
        </div>
      </div>
    </div>

    <!-- Chart Body -->
    <div class="ops-chart-body">
      <Line
        v-if="hasData"
        ref="chartRef"
        :data="chartData"
        :options="chartOptions"
      />
      <div v-else class="ops-chart-empty">
        <div class="ops-chart-empty-icon">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <circle cx="12" cy="12" r="9" opacity="0.4" />
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4m0 4h.01" />
          </svg>
        </div>
        <span>{{ loading ? '加载中...' : '暂无错误数据' }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { Line } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js'
import type { ChartData } from 'chart.js'
import zoomPlugin from 'chartjs-plugin-zoom'
import { ElButton } from 'element-plus'
import type { OpsErrorPoint } from '@/api/ops'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler, zoomPlugin)

interface Props {
  points: OpsErrorPoint[]
  loading: boolean
}

const props = defineProps<Props>()

const emit = defineEmits<{
  (e: 'open-error-details', type: 'request' | 'upstream'): void
}>()

const chartRef = shallowRef<{ chart: ChartJS } | null>(null)
const hasData = computed(() => props.points.length > 0)

const isDark = () => document.documentElement.classList.contains('dark')

const chartData = computed<ChartData<'line'>>(() => ({
  labels: props.points.map(p => formatTime(p.timestamp)),
  datasets: [
    {
      label: '4xx 客户端错误',
      data: props.points.map(p => p.request_errors),
      borderColor: '#f59e0b',
      backgroundColor: 'rgba(245, 158, 11, 0.1)',
      fill: true,
      tension: 0.4,
      pointRadius: 0,
      pointHoverRadius: 4,
    },
    {
      label: '5xx 上游错误',
      data: props.points.map(p => p.upstream_errors),
      borderColor: '#ef4444',
      backgroundColor: 'rgba(239, 68, 68, 0.1)',
      fill: true,
      tension: 0.4,
      pointRadius: 0,
      pointHoverRadius: 4,
    },
    {
      label: '业务错误 (429)',
      data: props.points.map(p => p.business_errors),
      borderColor: '#f97316',
      backgroundColor: 'rgba(249, 115, 22, 0.1)',
      fill: true,
      tension: 0.4,
      pointRadius: 0,
      pointHoverRadius: 4,
    },
  ],
}))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const,
  },
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: isDark() ? '#1E293B' : '#0F172A',
      titleColor: '#F1F5F9',
      bodyColor: '#CBD5E1',
      borderColor: isDark() ? '#334155' : '#1E293B',
      borderWidth: 1,
      padding: 12,
      displayColors: true,
    },
    zoom: {
      pan: { enabled: true, mode: 'x' as const },
      zoom: {
        wheel: { enabled: true },
        pinch: { enabled: true },
        mode: 'x' as const,
      },
    },
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 10 },
        maxTicksLimit: 12,
      },
    },
    y: {
      position: 'left' as const,
      beginAtZero: true,
      grid: { color: isDark() ? '#1E293B' : '#F1F5F9' },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 10 },
      },
    },
  },
}))

function resetZoom() {
  if (chartRef.value?.chart) {
    chartRef.value.chart.resetZoom()
  }
}

function downloadPNG() {
  if (!chartRef.value?.chart) return
  const canvas = chartRef.value.chart.canvas
  canvas.toBlob((blob) => {
    if (!blob) return
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `error-trend-${Date.now()}.png`
    a.click()
    URL.revokeObjectURL(url)
  })
}

function formatTime(timestamp: string): string {
  const d = new Date(timestamp)
  return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`
}
</script>

<style scoped>

.ops-chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--space-4);
  gap: var(--space-4);
  flex-wrap: wrap;
}

.ops-chart-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  margin: 0;
  white-space: nowrap;
}

.ops-chart-icon { width: 16px; height: 16px; }
.ops-chart-icon--red { color: #ef4444; }

.ops-chart-actions {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.ops-chart-legend {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}

.ops-chart-btns {
  display: flex;
  gap: var(--space-2);
}

.ops-legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ops-legend-dot {
  width: 8px; height: 8px; border-radius: 50%;
}

.ops-chart-body {
  height: 320px;
}

/* Empty state */
.ops-chart-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-3);
  height: 100%;
  color: var(--text-placeholder);
  font-size: var(--font-size-sm);
}

.ops-chart-empty-icon {
  color: var(--text-placeholder);
  opacity: 0.5;
}
</style>
