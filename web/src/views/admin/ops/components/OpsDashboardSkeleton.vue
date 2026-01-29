<template>
  <div class="ops-skeleton" :class="{ 'ops-skeleton--fullscreen': fullscreen }">
    <!-- Header Skeleton -->
    <div class="ops-skeleton__header">
      <div class="ops-skeleton__toolbar">
        <div class="skeleton-block skeleton-block--title"></div>
        <div class="skeleton-block skeleton-block--subtitle"></div>
      </div>
      <div class="ops-skeleton__top-section">
        <!-- Health Score Circle Skeleton -->
        <div class="skeleton-health-circle"></div>
        <!-- Realtime Traffic Skeleton -->
        <div class="skeleton-realtime">
          <div class="skeleton-block skeleton-block--label"></div>
          <div class="skeleton-metrics-row">
            <div class="skeleton-block skeleton-block--metric"></div>
            <div class="skeleton-block skeleton-block--metric"></div>
          </div>
          <div class="skeleton-block skeleton-block--peak"></div>
        </div>
      </div>
      <!-- 6 Metric Cards Skeleton -->
      <div class="skeleton-metrics-grid">
        <div v-for="i in 6" :key="i" class="skeleton-metric-card">
          <div class="skeleton-block skeleton-block--label"></div>
          <div class="skeleton-block skeleton-block--value"></div>
          <div class="skeleton-block skeleton-block--sub"></div>
        </div>
      </div>
      <!-- System Metrics Bar Skeleton -->
      <div class="skeleton-system-bar">
        <div v-for="i in 5" :key="i" class="skeleton-system-item">
          <div class="skeleton-block skeleton-block--label"></div>
          <div class="skeleton-block skeleton-block--sys-value"></div>
        </div>
      </div>
    </div>
    <!-- Chart Area Skeletons -->
    <div class="skeleton-chart-card">
      <div class="skeleton-block skeleton-block--chart-title"></div>
      <div class="skeleton-block skeleton-block--chart-area"></div>
    </div>
    <div class="skeleton-two-col">
      <div class="skeleton-chart-card">
        <div class="skeleton-block skeleton-block--chart-title"></div>
        <div class="skeleton-block skeleton-block--chart-area"></div>
      </div>
      <div class="skeleton-chart-card">
        <div class="skeleton-block skeleton-block--chart-title"></div>
        <div class="skeleton-block skeleton-block--chart-area"></div>
      </div>
    </div>
    <div class="skeleton-chart-card">
      <div class="skeleton-block skeleton-block--chart-title"></div>
      <div class="skeleton-block skeleton-block--chart-area"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  fullscreen?: boolean
}>()
</script>

<style scoped>
.ops-skeleton {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.ops-skeleton--fullscreen {
  min-height: 100vh;
  padding: var(--space-6);
}

.ops-skeleton__header {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-lg);
  padding: var(--space-6);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.ops-skeleton__toolbar {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding-bottom: var(--space-4);
  border-bottom: 1px solid var(--border-light);
}

.ops-skeleton__top-section {
  display: flex;
  align-items: center;
  gap: var(--space-8);
}

.skeleton-health-circle {
  width: 128px;
  height: 128px;
  border-radius: 50%;
  background: var(--bg-subtle);
  flex-shrink: 0;
  animation: pulse 2s ease-in-out infinite;
}

.skeleton-realtime {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.skeleton-metrics-row {
  display: flex;
  gap: var(--space-4);
}

.skeleton-metrics-grid {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: var(--space-4);
}

.skeleton-metric-card {
  background: var(--bg-subtle);
  border-radius: var(--radius-base);
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.skeleton-system-bar {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: var(--space-4);
}

.skeleton-system-item {
  background: var(--bg-subtle);
  border-radius: var(--radius-base);
  padding: var(--space-3);
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.skeleton-two-col {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: var(--space-4);
}

.skeleton-chart-card {
  background: var(--bg-surface);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-lg);
  padding: var(--space-6);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.skeleton-block {
  background: var(--bg-subtle);
  border-radius: 4px;
  animation: pulse 2s ease-in-out infinite;
}

.skeleton-block--title {
  width: 120px;
  height: 24px;
}

.skeleton-block--subtitle {
  width: 280px;
  height: 14px;
}

.skeleton-block--label {
  width: 64px;
  height: 12px;
}

.skeleton-block--value {
  width: 80px;
  height: 28px;
  animation-delay: 0.1s;
}

.skeleton-block--sub {
  width: 100px;
  height: 12px;
  animation-delay: 0.2s;
}

.skeleton-block--sys-value {
  width: 50px;
  height: 20px;
  animation-delay: 0.15s;
}

.skeleton-block--metric {
  width: 100px;
  height: 32px;
  animation-delay: 0.1s;
}

.skeleton-block--peak {
  width: 200px;
  height: 14px;
  animation-delay: 0.2s;
}

.skeleton-block--chart-title {
  width: 120px;
  height: 18px;
}

.skeleton-block--chart-area {
  width: 100%;
  height: 280px;
  animation-delay: 0.3s;
}

@keyframes pulse {
  0%, 100% {
    opacity: 0.6;
  }
  50% {
    opacity: 0.3;
  }
}

@media (max-width: 1200px) {
  .skeleton-metrics-grid {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 900px) {
  .ops-skeleton__top-section {
    flex-direction: column;
    align-items: flex-start;
  }
  .skeleton-metrics-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .skeleton-system-bar {
    grid-template-columns: repeat(2, 1fr);
  }
  .skeleton-two-col {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 600px) {
  .skeleton-metrics-grid {
    grid-template-columns: 1fr;
  }
  .skeleton-system-bar {
    grid-template-columns: 1fr;
  }
}
</style>