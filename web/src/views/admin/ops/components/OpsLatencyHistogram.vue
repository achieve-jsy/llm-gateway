<template>
  <div class="ops-chart-card">
    <div class="ops-chart-header">
      <h3 class="ops-chart-title">
        <svg class="ops-chart-icon ops-chart-icon--purple" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
        </svg>
        延迟分布
      </h3>
      <div class="ops-chart-sub">
        总请求: {{ formatNumber(totalRequests) }}
      </div>
    </div>
    <div class="ops-chart-body">
      <Bar v-if="chartData" :data="chartData" :options="chartOptions" />
      <div v-else class="ops-chart-empty">
        {{ loading ? '加载中...' : '暂无数据' }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Bar } from 'vue-chartjs'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  BarElement,
  Title,
  Tooltip,
  Legend
} from 'chart.js'
import type { OpsLatencyBucket } from '@/api/ops'

ChartJS.register(CategoryScale, LinearScale, BarElement, Title, Tooltip, Legend)

const props = defineProps<{
  buckets: OpsLatencyBucket[]
  totalRequests: number
  loading: boolean
}>()

const isDark = () => document.documentElement.classList.contains('dark')

const chartData = computed(() => {
  if (!props.buckets.length) return null

  return {
    labels: props.buckets.map(b => b.range),
    datasets: [
      {
        label: '请求数',
        data: props.buckets.map(b => b.count),
        backgroundColor: 'rgba(139, 92, 246, 0.6)',
        borderColor: '#8b5cf6',
        borderWidth: 1,
        borderRadius: 4,
      }
    ]
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
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
      callbacks: {
        label: (context: any) => {
          const value = context.parsed.y
          const percentage = props.totalRequests > 0
            ? ((value / props.totalRequests) * 100).toFixed(1)
            : '0'
          return `${value} 请求 (${percentage}%)`
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        display: false,
      },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 11 },
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

function formatNumber(num: number): string {
  if (num >= 1000000) {
    return (num / 1000000).toFixed(1) + 'M'
  }
  if (num >= 1000) {
    return (num / 1000).toFixed(1) + 'K'
  }
  return num.toString()
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

.ops-chart-icon--purple { color: #8b5cf6; }

.ops-chart-sub {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
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
