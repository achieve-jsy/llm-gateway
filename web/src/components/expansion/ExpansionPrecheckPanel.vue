<template>
  <div v-if="value" class="precheck" role="status">
    <DataFreshnessNotice :stale="value.stale" :updated-at="value.data_as_of" />
    <el-descriptions :column="2" border>
      <el-descriptions-item label="当前上限">${{ value.current_limit_usd }}</el-descriptions-item>
      <el-descriptions-item label="当前已用">${{ value.current_used_usd }}</el-descriptions-item>
      <el-descriptions-item label="目标额度">${{ value.target_amount_usd }}</el-descriptions-item>
      <el-descriptions-item label="增加额度">${{ value.increase_usd }}</el-descriptions-item>
    </el-descriptions>
    <el-alert
      v-if="value.blocking_reason"
      :title="`${value.blocking_reason.code}（${value.blocking_reason.source_level}）`"
      type="warning"
      :closable="false"
      show-icon
    />
    <p v-if="value.parent_constraints.length">
      上级约束：{{ value.parent_constraints.map(item => item.source_level).join('、') }}
    </p>
  </div>
</template>
<script setup lang="ts">
import DataFreshnessNotice from '@/components/common/DataFreshnessNotice.vue'
import type { ExpansionPrecheck } from '@/types/expansion'
defineProps<{ value: ExpansionPrecheck | null }>()
</script>
<style scoped>.precheck { display: grid; gap: 12px; } p { margin: 0; color: var(--el-text-color-secondary); }</style>
