<template>
  <div class="ops-chart-card">
    <div class="ops-chart-header">
      <h3 class="ops-chart-title">
        <svg class="ops-chart-icon ops-chart-icon--red" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        错误趋势
      </h3>
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
import type { OpsErrorPoint } from '@/api/ops'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler)

const props = defineProps<{
  points: OpsErrorPoint[]
  loading: boolean
}>()

const isDark = () => document.documentElement.classList.contains('dark')

const chartData = computed(() => {
  if (!props.points.length) return null

  return {
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
    legend: {
      display: false,
    },
    tooltip: {
      backgroundColor: isDark() ? '#1E293B' : '#0F172A',
      titleColor: '#F1F5F9',
      bodyColor: '#CBD5E1',
      borderColor: isDark() ? '#334155' : '#1E293B',
      borderWidth: 1,
      padding: 12,
      displayColors: true,
    }
  },
  scales: {
    x: {
      grid: {
        display: false,
      },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 10 },
        maxTicksLimit: 12,
      }
    },
    y: {
      position: 'left' as const,
      grid: {
        color: isDark() ? '#1E293B' : '#F1F5F9',
      },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 10 },
      }
    }
  }
}))

function formatTime(timestamp: string): string {
  const d = new Date(timestamp)
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

.ops-chart-icon--red { color: #ef4444; }

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
  height: 320px;
}

.ops-chart-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-placeholder);
}
</style>
