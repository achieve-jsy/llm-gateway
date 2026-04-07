<template>
  <article
    class="mobile-item"
    role="button"
    tabindex="0"
    :aria-label="`查看 ${item.scope_name || item.scope_id} 的扩容申请`"
    @click="$emit('open')"
    @keydown.enter="$emit('open')"
    @keydown.space.prevent="$emit('open')"
  >
    <div><strong>{{ item.scope_name || item.scope_id }}</strong><el-tag size="small">{{ STATUS_LABELS[item.status] }}</el-tag></div>
    <p>${{ item.current_amount_snapshot }} → ${{ item.target_amount_usd }} · {{ PERIOD_LABELS[item.period_type] }}</p>
    <small>{{ item.requester_name }} · {{ new Date(item.created_at).toLocaleString() }}</small>
  </article>
</template>
<script setup lang="ts">
import { PERIOD_LABELS, STATUS_LABELS, type ExpansionRequest } from '@/types/expansion'
defineProps<{ item: ExpansionRequest }>()
defineEmits<{ open: [] }>()
</script>
<style scoped>
.mobile-item { padding: 14px; border: 1px solid var(--el-border-color-light); border-radius: 8px; cursor: pointer; }
.mobile-item div { display: flex; justify-content: space-between; gap: 8px; } p { margin: 8px 0; } small { color: var(--el-text-color-secondary); }
</style>
