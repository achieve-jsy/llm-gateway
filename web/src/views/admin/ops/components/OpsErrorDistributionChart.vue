<template>
  <div class="ops-chart-card">
    <div class="ops-chart-header">
      <h3 class="ops-chart-title">
        <svg class="ops-chart-icon ops-chart-icon--orange" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 3.055A9.001 9.001 0 1020.945 13H11V3.055z" />
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.488 9H15V3.512A9.025 9.025 0 0120.488 9z" />
        </svg>
        错误分布
      </h3>
      <div class="ops-chart-sub">
        总错误: {{ formatNumber(totalErrors) }}
      </div>
    </div>
    <div class="ops-chart-body">
      <Doughnut v-if="chartData" :data="chartData" :options="chartOptions" />
      <div v-else class="ops-chart-empty">
        {{ loading ? '加载中...' : '暂无数据' }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Doughnut } from 'vue-chartjs'
import {
  Chart as ChartJS,
  ArcElement,
  Tooltip,
  Legend
} from 'chart.js'
import type { OpsErrorDistributionItem } from '@/api/ops'

ChartJS.register(ArcElement, Tooltip, Legend)

const props = defineProps<{
  distribution: OpsErrorDistributionItem[]
  loading: boolean
}>()

const isDark = () => document.documentElement.classList.contains('dark')

const totalErrors = computed(() => {
  return props.distribution.reduce((sum, item) => sum + item.count, 0)
})

const chartData = computed(() => {
  if (!props.distribution.length) return null

  const categoryMap = new Map<string, number>()
  for (const item of props.distribution) {
    const current = categoryMap.get(item.category) || 0
    categoryMap.set(item.category, current + item.count)
  }

  const categories = Array.from(categoryMap.entries())
  const labels = categories.map(([cat]) => {
    switch (cat) {
      case 'client': return '客户端错误 (4xx)'
      case 'upstream': return '上游错误 (5xx)'
      default: return '其他错误'
    }
  })
  const data = categories.map(([, count]) => count)
  const colors = [
    'rgba(245, 158, 11, 0.8)',
    'rgba(239, 68, 68, 0.8)',
    'rgba(107, 114, 128, 0.8)',
  ]

  return {
    labels,
    datasets: [
      {
        data,
        backgroundColor: colors,
        borderColor: colors.map(c => c.replace('0.8', '1')),
        borderWidth: 2,
      }
    ]
  }
})

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: {
      display: true,
      position: 'bottom' as const,
      labels: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 11 },
        padding: 16,
      }
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
          const value = context.parsed
          const percentage = totalErrors.value > 0
            ? ((value / totalErrors.value) * 100).toFixed(1)
            : '0'
          return `${context.label}: ${value} (${percentage}%)`
        }
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

.ops-chart-icon--orange { color: #f97316; }

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
