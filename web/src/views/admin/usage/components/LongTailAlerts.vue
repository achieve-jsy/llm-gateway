<template>
  <div v-if="longTailModels.length" class="lt-section">
    <div class="lt-header" @click="collapsed = !collapsed">
      <span class="lt-header__icon">
        <el-icon :size="16"><WarningFilled /></el-icon>
      </span>
      <span class="lt-header__title">长尾模型</span>
      <el-badge :value="longTailModels.length" type="warning" class="lt-header__badge" />
      <el-icon class="lt-header__arrow" :class="{ 'lt-header__arrow--open': !collapsed }">
        <ArrowRight />
      </el-icon>
    </div>

    <Transition name="lt-slide">
      <div v-show="!collapsed" class="lt-body">
        <div
          v-for="item in longTailModels"
          :key="item.model"
          class="lt-item"
          @click="$emit('model-click', item.model)"
        >
          <span class="lt-item__status" :class="getStatusClass(item)">
            <span class="lt-item__dot" />
          </span>
          <div class="lt-item__info">
            <span class="lt-item__name">{{ item.model }}</span>
            <span class="lt-item__meta">
              <span>{{ item.calls.toLocaleString() }} 次调用</span>
              <span class="lt-item__sep">·</span>
              <span>{{ fmtCost(item.cost) }}</span>
              <span v-if="item.error_rate" class="lt-item__sep">·</span>
              <span v-if="item.error_rate">错误率 {{ item.error_rate.toFixed(1) }}%</span>
            </span>
          </div>
          <el-icon class="lt-item__arrow"><ArrowRight /></el-icon>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { WarningFilled, ArrowRight } from '@element-plus/icons-vue'

defineOptions({ name: 'LongTailAlerts' })

interface LongTailModel {
  model: string
  calls: number
  cost: number
  error_rate?: number
  is_long_tail?: boolean
}

const props = defineProps<{
  modelsData: any[]
  currency?: string
}>()

defineEmits<{
  'model-click': [modelName: string]
}>()

const collapsed = ref(true)

const longTailModels = computed(() =>
  props.modelsData.filter((m: any) => m.is_long_tail)
)

function getStatusClass(item: LongTailModel) {
  // Red: 0 calls (likely dead model)
  if (item.calls === 0) return 'lt-item__status--dead'
  // Yellow: low activity (long-tail)
  return 'lt-item__status--low'
}

function fmtCost(v: number) {
  if (v < 0.01) return '< ¥0.01'
  return `¥${v.toFixed(2)}`
}
</script>

<style scoped>
.lt-section {
  margin-top: 12px;
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  border-radius: 8px;
  background: var(--el-bg-color, #fff);
  overflow: hidden;
}

.lt-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}
.lt-header:hover {
  background: var(--el-fill-color-light, #f5f7fa);
}

.lt-header__icon {
  color: #e6a23c;
  display: flex;
  align-items: center;
}

.lt-header__title {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary, #303133);
}

.lt-header__badge {
  margin-left: 4px;
}

.lt-header__arrow {
  margin-left: auto;
  font-size: 12px;
  color: var(--el-text-color-placeholder, #a8abb2);
  transition: transform 0.2s;
}
.lt-header__arrow--open {
  transform: rotate(90deg);
}

.lt-body {
  border-top: 1px solid var(--el-border-color-lighter, #ebeef5);
}

.lt-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  cursor: pointer;
  transition: background 0.15s;
}
.lt-item:hover {
  background: var(--el-fill-color-light, #f5f7fa);
}
.lt-item + .lt-item {
  border-top: 1px solid var(--el-border-color-extra-light, #f2f3f5);
}

.lt-item__status {
  flex-shrink: 0;
  display: flex;
  align-items: center;
}
.lt-item__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.lt-item__status--dead .lt-item__dot {
  background: #f56c6c;
  box-shadow: 0 0 4px rgba(245, 108, 108, 0.4);
}
.lt-item__status--low .lt-item__dot {
  background: #e6a23c;
  box-shadow: 0 0 4px rgba(230, 162, 60, 0.3);
}

.lt-item__info {
  flex: 1;
  min-width: 0;
}

.lt-item__name {
  display: block;
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary, #303133);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.lt-item__meta {
  display: flex;
  align-items: center;
  gap: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary, #909399);
  margin-top: 2px;
}

.lt-item__sep {
  margin: 0 4px;
  color: var(--el-text-color-placeholder, #c0c4cc);
}

.lt-item__arrow {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--el-text-color-placeholder, #a8abb2);
}

/* Slide transition */
.lt-slide-enter-active {
  transition: all 0.25s ease-out;
}
.lt-slide-leave-active {
  transition: all 0.2s ease-in;
}
.lt-slide-enter-from,
.lt-slide-leave-to {
  opacity: 0;
  max-height: 0;
}
.lt-slide-enter-to,
.lt-slide-leave-from {
  opacity: 1;
  max-height: 500px;
}
</style>
