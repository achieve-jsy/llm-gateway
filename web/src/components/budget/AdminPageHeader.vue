<template>
  <header class="admin-page-header">
    <div class="admin-page-header__copy">
      <h1>{{ title }}</h1>
      <p v-if="description || subtitle">{{ description || subtitle }}</p>
      <div v-if="$slots.meta" class="admin-page-header__meta">
        <slot name="meta" />
      </div>
    </div>
    <div class="admin-page-header__actions">
      <button
        type="button"
        class="admin-page-header__refresh"
        data-test="admin-page-header-refresh"
        aria-label="刷新"
        :aria-busy="loading"
        :disabled="loading"
        @click="emit('refresh')"
      >
        {{ loading ? '刷新中…' : '刷新' }}
      </button>
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  title: string
  description?: string
  subtitle?: string
  loading?: boolean
}>(), {
  description: '',
  subtitle: '',
  loading: false,
})

const emit = defineEmits<{
  refresh: []
}>()
</script>

<style scoped>
.admin-page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
}

.admin-page-header__copy {
  min-width: 0;
}

h1 {
  margin: 0;
  color: var(--text-primary);
  font-size: var(--font-size-display);
  font-weight: var(--font-weight-bold);
  line-height: 1.2;
}

p {
  margin: var(--space-1) 0 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}

.admin-page-header__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  margin-top: var(--space-2);
  gap: var(--space-2);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}

.admin-page-header__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
}

.admin-page-header__refresh {
  min-height: 32px;
  padding: 0 var(--space-3);
  color: var(--text-regular);
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-base);
  cursor: pointer;
  font: inherit;
}

.admin-page-header__refresh:hover:not(:disabled),
.admin-page-header__refresh:focus-visible {
  color: var(--brand-green-text);
  border-color: var(--brand-green-light);
}

.admin-page-header__refresh:focus-visible {
  outline: 2px solid var(--brand-green);
  outline-offset: 1px;
}

.admin-page-header__refresh:disabled {
  cursor: wait;
  opacity: 0.65;
}

@media (max-width: 760px) {
  .admin-page-header {
    flex-direction: column;
  }

  .admin-page-header__actions {
    width: 100%;
    justify-content: flex-start;
  }
}
</style>
