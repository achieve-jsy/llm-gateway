<template>
  <div class="tier-budget-tags">
    <span v-if="items.length === 0" class="tier-budget-tags__empty">未配置子预算</span>
    <el-tag
      v-for="item in visibleItems"
      v-else
      :key="item.id"
      :type="tagType(item)"
      effect="plain"
      class="tier-budget-tags__item"
    >
      <TierColorDot :color="item.color" />
      <span>{{ item.label }}</span>
    </el-tag>
    <el-tag v-if="hiddenCount > 0" type="info" effect="plain">+{{ hiddenCount }}</el-tag>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { ElTag } from 'element-plus'
import type { TierBudgetTagItem } from '@/composables/useBudgetDisplay'
import TierColorDot from './TierColorDot.vue'

const props = defineProps<{
  items: TierBudgetTagItem[]
}>()

const visibleItems = computed(() => props.items.slice(0, 3))
const hiddenCount = computed(() => Math.max(0, props.items.length - 3))

function tagType(item: TierBudgetTagItem): 'success' | 'warning' | 'danger' | 'info' {
  if (item.state === 'blocked') return 'danger'
  if (item.state === 'warning') return 'warning'
  if (item.state === 'normal') return 'success'
  return 'info'
}
</script>

<style scoped>
.tier-budget-tags {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1);
}

.tier-budget-tags__empty {
  color: var(--text-placeholder);
  font-size: var(--font-size-xs);
}

.tier-budget-tags__item {
  max-width: 100%;
}

.tier-budget-tags__item :deep(.el-tag__content) {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: var(--space-1);
}
</style>
