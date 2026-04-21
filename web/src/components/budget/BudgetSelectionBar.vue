<template>
  <div v-if="count > 0" class="budget-selection" role="status">
    <div class="budget-selection__copy batch-bar-left">
      <strong class="batch-bar-selected">已选择 {{ count }} 项</strong>
      <span v-if="remainingText" class="batch-bar-divider" aria-hidden="true">·</span>
      <span v-if="remainingText" class="batch-bar-unallocated">{{ remainingText }}</span>
    </div>
    <div class="budget-selection__actions batch-bar-right">
      <slot name="actions" />
      <el-button
        text
        class="budget-selection__clear"
        data-test="budget-selection-clear"
        aria-label="清除选择"
        @click="emit('clear')"
      >
        取消选择
      </el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  count: number
  remainingText?: string
}>()

const emit = defineEmits<{
  clear: []
}>()
</script>

<style scoped>
.budget-selection {
  justify-content: space-between;
}

.budget-selection__copy,
.budget-selection__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
}

.budget-selection__copy span {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.budget-selection__clear {
  padding: var(--space-1) var(--space-2);
  color: var(--budget-selection-text);
  background: transparent;
  border: 0;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font: inherit;
}

.budget-selection__clear:hover,
.budget-selection__clear:focus-visible {
  background: var(--bg-surface);
}

.budget-selection__clear:focus-visible {
  outline: 2px solid var(--brand-green);
  outline-offset: 1px;
}
</style>
