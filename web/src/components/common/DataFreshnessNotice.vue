<template>
  <el-alert
    v-if="stale"
    type="warning"
    :closable="false"
    show-icon
    title="数据可能已陈旧"
    :description="description"
    role="status"
  />
  <p v-else-if="updatedAt" class="freshness" aria-live="polite">数据更新于 {{ formatted }}</p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
const props = withDefaults(defineProps<{
  stale: boolean
  updatedAt?: string
  mode?: 'readonly' | 'write'
}>(), { mode: 'write' })
const formatted = computed(() => props.updatedAt ? new Date(props.updatedAt).toLocaleString() : '未知时间')
const description = computed(() => props.mode === 'readonly'
  ? `数据截至 ${formatted.value}，最新调用记录可能尚未汇总，刷新后会重新获取。`
  : `数据截至 ${formatted.value}，涉及额度的写操作已暂停，请刷新后重试。`)
</script>

<style scoped>
.freshness { margin: 0; color: var(--el-text-color-secondary); font-size: 12px; }
</style>
