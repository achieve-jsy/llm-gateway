<template>
  <el-alert
    v-if="stale"
    type="warning"
    :closable="false"
    show-icon
    title="数据可能已陈旧"
    :description="`数据截至 ${formatted}，涉及额度的写操作已暂停，请刷新后重试。`"
    role="status"
  />
  <p v-else-if="updatedAt" class="freshness" aria-live="polite">数据更新于 {{ formatted }}</p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ stale: boolean; updatedAt?: string }>()
const formatted = computed(() => props.updatedAt ? new Date(props.updatedAt).toLocaleString() : '未知时间')
</script>

<style scoped>
.freshness { margin: 0; color: var(--el-text-color-secondary); font-size: 12px; }
</style>
