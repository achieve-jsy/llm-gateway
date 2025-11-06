<template>
  <div class="model-card">
    <div class="card-accent"></div>
    <div class="card-body">
      <!-- 顶部：模型名 + 状态 -->
      <div class="card-top">
        <span class="model-name">{{ model.name }}</span>
        <span class="status-dot"></span>
      </div>

      <!-- 标签 -->
      <div class="card-tags" v-if="tags.length > 0">
        <span
          v-for="tag in tags"
          :key="tag.id"
          class="model-tag"
          :style="{ background: tag.color + '20', color: tag.color, borderColor: tag.color + '40' }"
        >{{ tag.name }}</span>
      </div>

      <!-- 元数据 -->
      <div class="card-meta">
        <el-tag size="small" effect="plain" round>{{ model.model_type || 'chat' }}</el-tag>
        <el-tag v-if="model.provider?.name" size="small" type="info" effect="plain" round>
          {{ model.provider.name }}
        </el-tag>
      </div>

      <!-- 指标 -->
      <div class="card-stats">
        <span v-if="model.context_length" class="stat-item" title="上下文窗口">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><path d="M4 6h16v2H4zm0 5h16v2H4zm0 5h16v2H4z"/></svg>
          {{ formatK(model.context_length) }}
        </span>
        <span v-if="model.max_output_tokens" class="stat-item" title="最大输出">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><path d="M3 17h18v2H3zm0-7h18v2H3zm0-7h18v2H3z"/></svg>
          {{ formatK(model.max_output_tokens) }}
        </span>
        <span v-if="model.input_price" class="stat-item stat-price" title="输入价格 ($/M tokens)">
          ${{ model.input_price }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Model } from '@/types/model'
import type { ModelTag } from '@/types/model-tag'

defineProps<{
  model: Model
  tags?: ModelTag[]
}>()

function formatK(n: number): string {
  if (n >= 1000000) return `${(n / 1000000).toFixed(1)}M`
  if (n >= 1000) return `${(n / 1024).toFixed(0)}K`
  return String(n)
}
</script>

<style scoped>
.model-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 12px;
  display: flex;
  overflow: hidden;
  transition: all 0.2s;
  box-shadow: 0 1px 4px rgba(43, 168, 162, 0.06);
}

.model-card:hover {
  box-shadow: 0 4px 16px rgba(43, 168, 162, 0.14);
  transform: translateY(-1px);
  border-color: var(--el-color-primary);
}

.card-accent {
  width: 3px;
  background: var(--el-color-primary);
  flex-shrink: 0;
}

.card-body {
  padding: 14px 16px;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.card-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.model-name {
  font-size: 13px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  font-family: 'Fira Code', 'SF Mono', monospace;
  letter-spacing: -0.3px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
  background: #27AE60;
  box-shadow: 0 0 4px rgba(39, 174, 96, 0.4);
}

.card-tags {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.model-tag {
  display: inline-block;
  padding: 2px 8px;
  font-size: 11px;
  font-weight: 500;
  border-radius: 4px;
  border: 1px solid;
  line-height: 1.4;
}

.card-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.card-stats {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 2px;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.stat-price {
  color: #EAB308;
  font-weight: 600;
}
</style>
