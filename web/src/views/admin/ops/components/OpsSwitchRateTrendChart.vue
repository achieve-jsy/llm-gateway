<template>
  <div class="ops-chart-card" :class="{ 'ops-chart-card--fullscreen': fullscreen }">
    <div class="ops-chart-header">
      <h3 class="ops-chart-title">
        <svg class="ops-chart-icon ops-chart-icon--teal" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4" />
        </svg>
        切换率趋势
      </h3>
      <span class="ops-chart-legend">
        <span class="ops-legend-item">
          <span class="ops-legend-dot" style="background: #14b8a6"></span>switch_rate
        </span>
      </span>
    </div>
    <div class="ops-chart-body">
      <Line v-if="chartData" :data="chartData" :options="chartOptions" />
      <div v-else class="ops-chart-empty">
        {{ loading ? '加载中...' : '暂无数据' }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
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
  Filler
} from 'chart.js'
import type { OpsSwitchRatePoint } from '@/api/ops'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler)

const props = defineProps<{
  points: OpsSwitchRatePoint[]
  loading: boolean
  timeRange?: string
  fullscreen?: boolean
}>()

const isDark = () => document.documentElement.classList.contains('dark')

const chartData = computed(() => {
  if (!props.points.length) return null
  return {
    labels: props.points.map(p => formatTime(p.timestamp)),
    datasets: [
      {
        label: '切换率 (%)',
        data: props.points.map(p => p.switch_rate * 100),
        borderColor: '#14b8a6',
        backgroundColor: 'rgba(20, 184, 166, 0.15)',
        fill: true,
        tension: 0.4,
        pointRadius: 0,
        pointHoverRadius: 4,
      }
    ]
  }
})

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
      callbacks: {
        label: (ctx: any) => {
          const idx = ctx.dataIndex
          const pt = props.points[idx]
          return [
            `切换率: ${(pt.switch_rate * 100).toFixed(2)}%`,
            `切换次数: ${pt.switch_count}`,
            `请求总数: ${pt.request_count}`,
          ]
        }
      }
    }
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 10 },
        maxTicksLimit: 12,
      }
    },
    y: {
      grid: { color: isDark() ? '#1E293B' : '#F1F5F9' },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 10 },
        callback: (v: number) => v.toFixed(1) + '%',
      }
    }
  }
}))

function formatTime(ts: string): string {
  const d = new Date(ts)
  return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`
}
</script>

<style scoped>
.ops-chart-card {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-lg);
  padding: var(--space-6);
}

.ops-chart-card--fullscreen {
  min-height: 100vh;
}

.ops-chart-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--space-4);
}

.ops-chart-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.ops-chart-icon {
  width: 16px;
  height: 16px;
}

.ops-chart-icon--teal {
  color: #14b8a6;
}

.ops-chart-legend {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}

.ops-legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
}

.ops-legend-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.ops-chart-body {
  height: 280px;
}

.ops-chart-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-placeholder);
}
</style>