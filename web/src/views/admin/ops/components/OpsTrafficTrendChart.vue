<template>
  <div class="ops-chart-card">
    <div class="ops-chart-header">
      <h3 class="ops-chart-title">
        <svg class="ops-chart-icon ops-chart-icon--blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6" />
        </svg>
        流量趋势
      </h3>
      <div class="ops-chart-actions">
        <div class="ops-chart-legend">
          <span class="ops-legend-item">
            <span class="ops-legend-dot" style="background: #3b82f6"></span>QPS
          </span>
          <span class="ops-legend-item">
            <span class="ops-legend-dot" style="background: #10b981"></span>TPS (K)
          </span>
        </div>
        <div class="ops-chart-btns">
          <el-button v-if="hasData" size="small" plain @click="resetZoom">重置缩放</el-button>
          <el-button v-if="hasData" size="small" plain @click="downloadPNG">下载 PNG</el-button>
          <el-button size="small" plain @click="$emit('open-details')">详情</el-button>
        </div>
      </div>
    </div>

    <!-- Provider Chips -->
    <div v-if="byProvider && byProvider.length" class="ops-provider-chips">
      <el-tag
        v-for="p in byProvider"
        :key="p.provider_id"
        class="ops-provider-chip"
        :effect="selectedProviderId === p.provider_id ? 'dark' : 'plain'"
        size="small"
        style="cursor: pointer"
        @click="handleProviderClick(p)"
      >
        {{ p.provider_name }} ({{ formatNumber(p.request_count) }})
      </el-tag>
    </div>

    <!-- Top Groups Chips -->
    <div v-if="topGroups && topGroups.length" class="ops-group-chips">
      <el-tag
        v-for="g in topGroups"
        :key="g.group_id"
        class="ops-group-chip"
        :effect="selectedGroupId === g.group_id ? 'dark' : 'plain'"
        type="success"
        size="small"
        style="cursor: pointer"
        @click="handleGroupClick(g)"
      >
        {{ g.group_name }} ({{ formatNumber(g.request_count) }})
      </el-tag>
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
            <path stroke-linecap="round" stroke-linejoin="round" d="M3 3v18h18" />
            <path stroke-linecap="round" stroke-linejoin="round" d="M7 14l4-4 4 4 5-5" opacity="0.4" />
          </svg>
        </div>
        <span>{{ loading ? '加载中...' : '暂无流量数据' }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, shallowRef, ref } from 'vue'
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
import { ElButton, ElTag } from 'element-plus'
import type { OpsTrafficPoint, OpsThroughputByProvider, OpsThroughputByGroup } from '@/api/ops'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Title, Tooltip, Legend, Filler, zoomPlugin)

interface Props {
  points: OpsTrafficPoint[]
  loading: boolean
  byProvider?: OpsThroughputByProvider[]
  topGroups?: OpsThroughputByGroup[]
}

const props = withDefaults(defineProps<Props>(), {
  byProvider: () => [],
  topGroups: () => [],
})

const emit = defineEmits<{
  (e: 'select-provider', provider: OpsThroughputByProvider): void
  (e: 'select-group', groupId: number): void
  (e: 'open-details'): void
}>()

const selectedProviderId = ref<number | null>(null)
const selectedGroupId = ref<number | null>(null)

function handleProviderClick(p: OpsThroughputByProvider) {
  if (selectedProviderId.value === p.provider_id) {
    selectedProviderId.value = null
    emit('select-provider', p)
  } else {
    selectedProviderId.value = p.provider_id
    emit('select-provider', p)
  }
}

function handleGroupClick(g: OpsThroughputByGroup) {
  if (selectedGroupId.value === g.group_id) {
    selectedGroupId.value = null
    emit('select-group', g.group_id)
  } else {
    selectedGroupId.value = g.group_id
    emit('select-group', g.group_id)
  }
}

// Chart ref for resetZoom
const chartRef = shallowRef<{ chart: ChartJS } | null>(null)

const hasData = computed(() => props.points.length > 0)

const isDark = () => document.documentElement.classList.contains('dark')

const chartData = computed<ChartData<'line'>>(() => ({
  labels: props.points.map(p => formatTime(p.timestamp)),
  datasets: [
    {
      label: 'QPS',
      data: props.points.map(p => p.qps),
      borderColor: '#3b82f6',
      backgroundColor: 'rgba(59, 130, 246, 0.1)',
      fill: true,
      tension: 0.4,
      pointRadius: 0,
      pointHoverRadius: 4,
    },
    {
      label: 'TPS (K)',
      data: props.points.map(p => p.tps / 1000),
      borderColor: '#10b981',
      backgroundColor: 'rgba(16, 185, 129, 0.1)',
      fill: true,
      tension: 0.4,
      pointRadius: 0,
      pointHoverRadius: 4,
      yAxisID: 'y1',
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
      grid: { color: isDark() ? '#1E293B' : '#F1F5F9' },
      ticks: {
        color: isDark() ? '#94A3B8' : '#64748B',
        font: { size: 10 },
      },
    },
    y1: {
      position: 'right' as const,
      grid: { display: false },
      ticks: {
        color: '#10b981',
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
    a.download = `traffic-trend-${Date.now()}.png`
    a.click()
    URL.revokeObjectURL(url)
  })
}

function formatTime(timestamp: string): string {
  const d = new Date(timestamp)
  return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`
}

function formatNumber(num: number): string {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M'
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K'
  return num.toString()
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
.ops-chart-icon--blue { color: #3b82f6; }
.ops-chart-icon--red { color: #ef4444; }
.ops-chart-icon--purple { color: #8b5cf6; }
.ops-chart-icon--orange { color: #f97316; }

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

.ops-group-chips {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-2);
}

.ops-provider-chip {
  cursor: pointer;
  transition: all 0.15s ease;
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
