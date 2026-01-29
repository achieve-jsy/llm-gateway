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
      <div class="ops-chart-actions">
        <div class="ops-chart-sub">
          总错误: {{ formatNumber(totalErrors) }}
        </div>
        <div class="ops-chart-btns">
          <el-button size="small" plain @click="$emit('open-details')">详情</el-button>
        </div>
      </div>
    </div>

    <!-- Top reason label -->
    <div v-if="topReason" class="ops-top-reason">
      <span class="ops-top-reason-badge">TOP 1</span>
      <span class="ops-top-reason-text">
        {{ topReason.label }}：{{ topReason.count }} 次
        <span class="ops-top-reason-pct">({{ topReason.pct }}%)</span>
      </span>
    </div>

    <!-- Chart Body -->
    <div class="ops-chart-body">
      <Doughnut
        v-if="hasData"
        :data="chartData"
        :options="chartOptions"
      />
      <div v-else class="ops-chart-empty">
        <div class="ops-chart-empty-icon">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <circle cx="12" cy="12" r="9" opacity="0.4" />
            <path stroke-linecap="round" d="M12 3a9 9 0 019 9" opacity="0.7" />
          </svg>
        </div>
        <span>{{ loading ? '加载中...' : '暂无错误数据' }}</span>
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
  Legend,
} from 'chart.js'
import type { ChartData } from 'chart.js'
import { ElButton } from 'element-plus'
import type { OpsErrorDistributionItem } from '@/api/ops'

ChartJS.register(ArcElement, Tooltip, Legend)

interface Props {
  distribution: OpsErrorDistributionItem[]
  loading: boolean
}

const props = defineProps<Props>()

defineEmits<{
  (e: 'open-details'): void
}>()

const hasData = computed(() => props.distribution.length > 0)

const isDark = () => document.documentElement.classList.contains('dark')

const totalErrors = computed(() =>
  props.distribution.reduce((sum, item) => sum + item.count, 0),
)

const CATEGORY_LABELS: Record<string, string> = {
  client: '客户端错误 (4xx)',
  upstream: '上游错误 (5xx)',
  system: '系统错误',
  other: '其他错误',
}

const categoryAggregated = computed(() => {
  const map = new Map<string, number>()
  for (const item of props.distribution) {
    map.set(item.category, (map.get(item.category) ?? 0) + item.count)
  }
  return Array.from(map.entries()).sort((a, b) => b[1] - a[1])
})

const topReason = computed(() => {
  if (totalErrors.value === 0) return null
  const top = categoryAggregated.value[0]
  if (!top) return null
  return {
    label: CATEGORY_LABELS[top[0]] ?? top[0],
    count: top[1],
    pct: ((top[1] / totalErrors.value) * 100).toFixed(1),
  }
})

const chartData = computed<ChartData<'doughnut'>>(() => {
  const categories = categoryAggregated.value
  const labels = categories.map(([cat]) => CATEGORY_LABELS[cat] ?? cat)
  const data = categories.map(([, count]) => count)

  const palette = [
    'rgba(245, 158, 11, 0.8)',
    'rgba(239, 68, 68, 0.8)',
    'rgba(139, 92, 246, 0.8)',
    'rgba(107, 114, 128, 0.8)',
  ]

  return {
    labels,
    datasets: [
      {
        data,
        backgroundColor: palette,
        borderColor: palette.map(c => c.replace('0.8', '1')),
        borderWidth: 2,
      },
    ],
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
      },
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
        },
      },
    },
  },
}))

function formatNumber(num: number): string {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M'
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K'
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
  gap: var(--space-4);
  flex-wrap: wrap;
}

.ops-chart-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
  white-space: nowrap;
}

.ops-chart-icon { width: 16px; height: 16px; }
.ops-chart-icon--orange { color: #f97316; }

.ops-chart-actions {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.ops-chart-sub {
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
}

.ops-chart-btns {
  display: flex;
  gap: var(--space-2);
}

/* Top reason badge */
.ops-top-reason {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  background: rgba(245, 158, 11, 0.08);
  border: 1px solid rgba(245, 158, 11, 0.25);
  border-radius: var(--radius-base);
  margin-bottom: var(--space-4);
}

.ops-top-reason-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: #fff;
  background: #f59e0b;
  line-height: 1;
}

.ops-top-reason-text {
  font-size: var(--font-size-xs);
  color: var(--text-primary);
  font-weight: 500;
}

.ops-top-reason-pct {
  color: var(--text-secondary);
  font-weight: 400;
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
