<script setup lang="ts">
import { computed } from 'vue'
import type { HubSyncStatus } from '@/types/org'

// 展示组件：只管显示 + 触发，请求与防重复由页面持有（页面知道刷新后要恢复状态）。
const props = defineProps<{
  status: HubSyncStatus | null
  syncing: boolean
}>()

const emit = defineEmits<{ (e: 'trigger'): void }>()

const lastSyncText = computed(() => {
  const at = props.status?.last_sync_at
  if (!at) return '尚未同步'
  const d = new Date(at)
  if (Number.isNaN(d.getTime())) return at
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
})

const failed = computed(() => props.status?.status === 'failed')
</script>

<template>
  <div class="sync-card" data-test="sync-card">
    <div class="sync-row">
      <span class="sync-label">数据同步</span>
      <span class="sync-time" data-test="sync-time" :class="{ failed }">
        上次同步：{{ lastSyncText }}
        <template v-if="status?.status === 'success'">· 成功</template>
        <template v-else-if="status?.status === 'failed'">· 失败</template>
      </span>
      <el-button
        size="small"
        type="primary"
        :disabled="syncing"
        :loading="syncing"
        data-test="sync-trigger"
        @click="emit('trigger')"
      >
        {{ syncing ? '同步中…' : '立即同步' }}
      </el-button>
    </div>

    <div v-if="status?.last_sync_at" class="sync-row sync-meta">
      部门 {{ status.departments }} · 人员 {{ status.employees }} ·
      未归属 {{ status.unmapped_departments }} · 离职关停 {{ status.resigned_accounts_disabled }} ·
      本次变更：工作室 +{{ status.diff?.studios?.inserted ?? 0 }} ~{{ status.diff?.studios?.updated ?? 0 }} -{{ status.diff?.studios?.deleted ?? 0 }}，
      部门 +{{ status.diff?.departments?.inserted ?? 0 }} ~{{ status.diff?.departments?.updated ?? 0 }} -{{ status.diff?.departments?.deleted ?? 0 }}
    </div>

    <div v-if="status?.error" class="sync-row sync-error" data-test="sync-error">{{ status.error }}</div>
  </div>
</template>

<style scoped>
.sync-card {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 8px 12px;
  margin-bottom: 8px;
  background: var(--el-fill-color-lighter);
  font-size: 12px;
}
.sync-row { display: flex; align-items: center; gap: 8px; }
.sync-row + .sync-row { margin-top: 4px; }
.sync-label { font-weight: 600; }
.sync-time { color: var(--el-text-color-regular); flex: 1; }
.sync-time.failed { color: var(--el-color-danger); }
.sync-meta { color: var(--el-text-color-secondary); flex-wrap: wrap; }
.sync-error { color: var(--el-color-danger); word-break: break-all; }
</style>
