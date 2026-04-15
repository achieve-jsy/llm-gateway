<template>
  <el-tag
    :type="elType"
    :effect="status.tone === 'neutral' ? 'plain' : 'light'"
    size="small"
    :class="`budget-status-tag--${status.tone}`"
  >
    {{ status.label }}
  </el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { ElTag } from 'element-plus'
import type { BudgetDisplayStatus } from '@/composables/useBudgetDisplay'

const props = defineProps<{
  status: BudgetDisplayStatus
}>()

const elType = computed<'success' | 'warning' | 'danger' | 'info'>(() => {
  if (props.status.tone === 'neutral') return 'info'
  return props.status.tone
})
</script>

<style scoped>
.budget-status-tag--neutral {
  color: var(--budget-status-neutral);
  background: var(--budget-status-neutral-bg);
}
</style>
