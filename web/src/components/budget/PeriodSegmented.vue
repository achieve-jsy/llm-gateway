<template>
  <el-segmented
    class="budget-period"
    aria-label="预算周期"
    :model-value="modelValue"
    :options="options"
    @update:model-value="onUpdate"
  >
    <template #default="{ item }">
      <span
        class="budget-period__item"
        :class="{ 'is-active': modelValue === item.value }"
        :aria-pressed="modelValue === item.value"
        :data-test="`period-${item.value}`"
      >
        {{ item.label }}
      </span>
    </template>
  </el-segmented>
</template>

<script setup lang="ts">
import { ElSegmented } from 'element-plus'
import type { ExpansionPeriod } from '@/types/expansion'

defineProps<{
  modelValue: ExpansionPeriod
}>()

const emit = defineEmits<{
  'update:modelValue': [value: ExpansionPeriod]
}>()

const options: Array<{ value: ExpansionPeriod; label: string }> = [
  { value: 'daily', label: '日' },
  { value: 'weekly', label: '周' },
  { value: 'monthly', label: '月' },
]

function onUpdate(value: string | number | boolean): void {
  emit('update:modelValue', value as ExpansionPeriod)
}
</script>

<style scoped>
.budget-period {
  padding: var(--space-1);
}

.budget-period__item {
  display: flex;
  min-height: 28px;
  align-items: center;
  justify-content: center;
  padding: 0 var(--space-3);
}

.budget-period :deep(.el-segmented__item-label) {
  padding: 0;
}

.budget-period :deep(.el-segmented__item.is-selected) {
  color: var(--brand-green-text);
}
</style>
