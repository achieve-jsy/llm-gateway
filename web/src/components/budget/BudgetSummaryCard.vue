<template>
  <article class="budget-summary__item">
    <header class="budget-summary-card__header">
      <div>
        <h3>{{ title }}</h3>
        <p v-if="meta">{{ meta }}</p>
      </div>
      <slot name="status" />
    </header>
    <div class="budget-summary__value">{{ amount }}</div>
    <div
      v-if="usagePercent !== undefined"
      class="budget-summary-card__progress"
      role="progressbar"
      aria-label="预算使用率"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-valuenow="normalizedUsage"
    >
      <span :style="{ width: `${normalizedUsage}%` }" />
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  title: string
  meta?: string
  amount: string | number
  usagePercent?: number
}>()

const normalizedUsage = computed(() => {
  const value = Number(props.usagePercent)
  return Number.isFinite(value) ? Math.min(100, Math.max(0, value)) : 0
})
</script>

<style scoped>
.budget-summary-card__header {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-2);
}

h3 {
  margin: 0;
  color: var(--text-primary);
  font-size: var(--font-size-section);
  font-weight: var(--font-weight-semibold);
}

p {
  margin: var(--space-1) 0 0;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.budget-summary-card__progress {
  height: 6px;
  overflow: hidden;
  margin-top: var(--space-3);
  background: var(--bg-subtle);
  border-radius: var(--radius-full);
}

.budget-summary-card__progress span {
  display: block;
  height: 100%;
  background: var(--brand-green);
  border-radius: inherit;
  transition: width 0.2s ease;
}
</style>
