<template>
  <div class="ops-card" v-loading="loading">
    <div class="ops-card-header">
      <h3 class="ops-card-title">🎬 视频任务</h3>
    </div>
    <div class="video-stats-grid">
      <div class="video-stat-item">
        <div class="video-stat-label">生成中</div>
        <div class="video-stat-value video-stat-value--running">
          {{ loading ? '...' : (stats ? formatNum(stats.running) : '--') }}
        </div>
      </div>
      <div class="video-stat-item">
        <div class="video-stat-label">排队中</div>
        <div class="video-stat-value">
          {{ loading ? '...' : (stats ? formatNum(stats.submitted) : '--') }}
        </div>
      </div>
      <div class="video-stat-item">
        <div class="video-stat-label">平均耗时</div>
        <div class="video-stat-value">
          {{ loading ? '...' : (stats ? formatDuration(stats.avg_duration_seconds) : '--') }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { opsAPI } from '@/api/ops'
import type { OpsVideoTaskStats } from '@/api/ops'

const props = withDefaults(
  defineProps<{
    refreshToken?: number
  }>(),
  { refreshToken: 0 }
)

const loading = ref(false)
const stats = ref<OpsVideoTaskStats | null>(null)

watch(() => props.refreshToken, () => {
  fetchData()
})

onMounted(fetchData)

function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return n.toString()
}

function formatDuration(seconds: number): string {
  if (seconds <= 0) return '--'
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const mins = Math.floor(seconds / 60)
  const secs = Math.round(seconds % 60)
  return `${mins}m${secs}s`
}

async function fetchData() {
  loading.value = true
  try {
    stats.value = await opsAPI.getVideoTaskStats()
  } catch {
    stats.value = null
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.video-stats-grid {
  display: flex;
  gap: 32px;
}

.video-stat-item {
  text-align: center;
}

.video-stat-label {
  font-size: 12px;
  color: var(--text-secondary);
}

.video-stat-value {
  font-size: 28px;
  font-weight: 800;
  font-family: var(--font-mono);
  color: var(--text-primary);
}

.video-stat-value--running {
  color: var(--color-video, var(--el-color-primary));
}
</style>
