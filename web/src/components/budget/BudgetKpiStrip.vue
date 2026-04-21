<template>
  <div class="budget-kpi-strip">
    <component
      :is="item.clickable ? 'button' : 'div'"
      v-for="item in items"
      :key="item.key"
      class="budget-kpi-strip__item"
      :class="[
        item.tone ? `budget-kpi-strip__item--${item.tone}` : '',
        { 'is-clickable': item.clickable },
      ]"
      :type="item.clickable ? 'button' : undefined"
      :data-test="`budget-kpi-${item.key}`"
      @click="item.clickable && emit('select', item.key)"
    >
      <span class="budget-kpi-strip__label">{{ item.label }}</span>
      <strong class="budget-kpi-strip__value">{{ item.value }}</strong>
      <span v-if="item.note ?? item.meta" class="budget-kpi-strip__meta">
        {{ item.note ?? item.meta }}
      </span>
      <div
        v-if="item.progress !== undefined"
        class="budget-kpi-strip__progress"
        role="progressbar"
        :aria-label="`${item.label}进度`"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-valuenow="normalizeProgress(item.progress)"
      >
        <span :style="{ width: `${normalizeProgress(item.progress)}%` }" />
      </div>
    </component>
  </div>
</template>

<script setup lang="ts">
import type { BudgetTone } from '@/composables/useBudgetDisplay'

interface BudgetKpiItem {
  key: string
  label: string
  value: string | number
  note?: string
  meta?: string
  tone?: BudgetTone
  progress?: number
  clickable?: boolean
}

defineProps<{
  items: BudgetKpiItem[]
}>()

const emit = defineEmits<{
  select: [key: string]
}>()

function normalizeProgress(progress: number): number {
  const value = Number(progress)
  return Number.isFinite(value) ? Math.min(100, Math.max(0, value)) : 0
}
</script>

<style scoped>
.budget-kpi-strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  overflow: hidden;
  background: var(--budget-panel-bg);
  border: 1px solid var(--budget-panel-border);
  border-radius: var(--budget-panel-radius);
}

.budget-kpi-strip__item {
  display: grid;
  min-width: 0;
  padding: var(--space-4);
  gap: var(--space-1);
  color: inherit;
  background: transparent;
  border: 0;
  border-right: 1px solid var(--budget-row-border);
  font: inherit;
  text-align: left;
}

.budget-kpi-strip__item:last-child {
  border-right: 0;
}

.budget-kpi-strip__item.is-clickable {
  cursor: pointer;
}

.budget-kpi-strip__item.is-clickable:hover,
.budget-kpi-strip__item.is-clickable:focus-visible {
  background: var(--bg-hover);
}

.budget-kpi-strip__item.is-clickable:focus-visible {
  outline: 2px solid var(--brand-green);
  outline-offset: -2px;
}

.budget-kpi-strip__label,
.budget-kpi-strip__meta {
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.budget-kpi-strip__value {
  color: var(--text-primary);
  font-size: var(--font-size-title);
  font-variant-numeric: tabular-nums;
}

.budget-kpi-strip__progress {
  height: 4px;
  overflow: hidden;
  margin-top: var(--space-1);
  background: var(--bg-subtle);
  border-radius: var(--radius-full);
}

.budget-kpi-strip__progress span {
  display: block;
  height: 100%;
  background: currentColor;
  border-radius: inherit;
}

.budget-kpi-strip__item--success .budget-kpi-strip__value { color: var(--budget-status-success); }
.budget-kpi-strip__item--warning .budget-kpi-strip__value { color: var(--budget-status-warning); }
.budget-kpi-strip__item--danger .budget-kpi-strip__value { color: var(--budget-status-danger); }
.budget-kpi-strip__item--info .budget-kpi-strip__value { color: var(--budget-status-info); }
.budget-kpi-strip__item--neutral .budget-kpi-strip__value { color: var(--budget-status-neutral); }

@media (max-width: 760px) {
  .budget-kpi-strip {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .budget-kpi-strip__item {
    border-bottom: 1px solid var(--budget-row-border);
  }
}
</style>
