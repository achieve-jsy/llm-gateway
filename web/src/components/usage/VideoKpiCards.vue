<template>
  <div class="vkpi" v-loading="loading">
    <div class="vkpi-card">
      <div class="vkpi-label">任务总数</div>
      <div class="vkpi-value">{{ formatNum(stats?.total) }}</div>
    </div>
    <div class="vkpi-card">
      <div class="vkpi-label">成功率</div>
      <div class="vkpi-value">{{ successRate }}</div>
    </div>
    <div class="vkpi-card">
      <div class="vkpi-label">平均生成耗时</div>
      <div class="vkpi-value">{{ formatDuration(stats?.avg_duration_seconds) }}</div>
    </div>
    <div class="vkpi-card">
      <div class="vkpi-label">进行中</div>
      <div class="vkpi-value vkpi-value--active">{{ formatNum(inProgress) }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { videoTasksAPI, type VideoTaskStats } from '@/api/videoTasks'

const props = defineProps<{
  refreshToken?: number
  start?: string
  end?: string
  mine?: boolean
  providerName?: string
  modelName?: string
}>()

const loading = ref(false)
const stats = ref<VideoTaskStats | null>(null)

const inProgress = computed(() => (stats.value?.submitted || 0) + (stats.value?.running || 0))

const successRate = computed(() => {
  const total = stats.value?.total || 0
  const succeeded = stats.value?.succeeded || 0
  if (total <= 0) return '—'
  return `${((succeeded / total) * 100).toFixed(1)}%`
})

function formatNum(n?: number): string {
  if (n == null) return '—'
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
  return String(n)
}

function formatDuration(seconds?: number): string {
  if (seconds == null || seconds <= 0) return '—'
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const mins = Math.floor(seconds / 60)
  const secs = Math.round(seconds % 60)
  return `${mins}m${secs}s`
}

async function load() {
  loading.value = true
  try {
    stats.value = await videoTasksAPI.stats({
      start: props.start,
      end: props.end,
      provider_name: props.providerName,
      model_name: props.modelName,
    }, { mine: props.mine })
  } catch {
    stats.value = null
  } finally {
    loading.value = false
  }
}

watch(() => [props.refreshToken, props.start, props.end, props.mine, props.providerName, props.modelName], () => load())
onMounted(load)

defineExpose({ reload: load })
</script>

<style scoped>
.vkpi {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 16px;
}
.vkpi-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  padding: 16px;
}
.vkpi-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}
.vkpi-value {
  font-size: 24px;
  font-weight: 700;
  font-family: var(--el-font-family);
  color: var(--el-text-color-primary);
}
.vkpi-value--active {
  color: var(--el-color-primary);
}
@media (max-width: 900px) {
  .vkpi {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
