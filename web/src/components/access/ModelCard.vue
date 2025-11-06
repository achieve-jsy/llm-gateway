<template>
  <div class="model-card" :class="{ 'is-hover': isHover }" @mouseenter="isHover = true" @mouseleave="isHover = false">
    <div class="card-accent"></div>
    <div class="card-body">
      <div class="card-top">
        <span class="model-name">{{ model.id }}</span>
        <el-tag size="small" effect="plain" round>
          {{ model.owned_by || 'unknown' }}
        </el-tag>
      </div>
      <div class="card-meta">
        <span class="meta-item">
          <span class="meta-dot"></span>
          可用
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ModelInfo } from '@/types/model'

defineProps<{
  model: ModelInfo
}>()

const isHover = ref(false)
</script>

<style scoped>
.model-card {
  background: #FFFFFF;
  border: 1px solid #D1E9E7;
  border-radius: 12px;
  display: flex;
  overflow: hidden;
  transition: all 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
  cursor: default;
  box-shadow: 0 2px 10px rgba(43, 168, 162, 0.06);
}

.model-card.is-hover {
  box-shadow: 0 4px 20px rgba(43, 168, 162, 0.18);
  transform: translateY(-2px);
  border-color: #2BA8A2;
}

.card-accent {
  width: 4px;
  background: #2BA8A2;
  flex-shrink: 0;
  transition: background 0.2s;
}

.model-card.is-hover .card-accent {
  background: linear-gradient(180deg, #FFD23F, #2BA8A2);
}

.card-body {
  padding: 16px 20px;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.model-name {
  font-size: 14px;
  font-weight: 700;
  color: #1E8C86;
  font-family: 'Fira Code', monospace;
  letter-spacing: -0.3px;
}

.card-meta {
  display: flex;
  align-items: center;
  gap: 12px;
}

.meta-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #27AE60;
  font-weight: 500;
}

.meta-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #27AE60;
  animation: pulse 2s infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

/* 深色模式 */
:global(html.dark) .model-card {
  background: #163B3A;
  border-color: #1E5653;
}

:global(html.dark) .model-name {
  color: #3CC4BD;
}

:global(html.dark) .model-card.is-hover {
  border-color: #3CC4BD;
}
</style>
