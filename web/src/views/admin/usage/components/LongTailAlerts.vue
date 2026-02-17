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

    <div class="lt-body" :class="{ 'lt-body--open': !collapsed }">
      <div class="lt-body__inner">
        <!-- Sort toolbar -->
        <div class="lt-toolbar">
          <span class="lt-toolbar__label">排序：</span>
          <button
            v-for="opt in sortOptions"
            :key="opt.field"
            class="lt-toolbar__btn"
            :class="{ 'lt-toolbar__btn--active': (sortBy ?? 'calls') === opt.field }"
            @click.stop="toggleSort(opt.field)"
          >
            {{ opt.label }}
            <span v-if="(sortBy ?? 'calls') === opt.field" class="lt-toolbar__arrow">
              {{ (sortOrder ?? 'desc') === 'desc' ? '↓' : '↑' }}
            </span>
          </button>
        </div>

        <div
          v-for="item in sortedModels"
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
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { WarningFilled, ArrowRight } from '@element-plus/icons-vue'
import { currencySymbol, type Currency } from '@/utils/currency'

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
  sortBy?: 'calls' | 'cost' | 'error_rate'
  sortOrder?: 'asc' | 'desc'
}>()

const emit = defineEmits<{
  'model-click': [modelName: string]
  'update:sortBy': [value: 'calls' | 'cost' | 'error_rate']
  'update:sortOrder': [value: 'asc' | 'desc']
}>()

const collapsed = ref(true)

const longTailModels = computed(() =>
  props.modelsData.filter((m: any) => m.is_long_tail)
)

/** Sorted view of long-tail models */
const sortedModels = computed(() => {
  const field = props.sortBy ?? 'calls'
  const order = props.sortOrder === 'asc' ? 1 : -1
  return [...longTailModels.value].sort((a, b) => {
    const va = a[field] ?? 0
    const vb = b[field] ?? 0
    return (va - vb) * order
  })
})

const sortOptions = [
  { field: 'calls' as const, label: '调用量' },
  { field: 'cost' as const, label: '成本' },
  { field: 'error_rate' as const, label: '错误率' },
]

function toggleSort(field: 'calls' | 'cost' | 'error_rate') {
  if ((props.sortBy ?? 'calls') === field) {
    emit('update:sortOrder', (props.sortOrder ?? 'desc') === 'desc' ? 'asc' : 'desc')
  } else {
    emit('update:sortBy', field)
    emit('update:sortOrder', 'desc')
  }
}

function getStatusClass(item: LongTailModel) {
  // Red: 0 calls (likely dead model)
  if (item.calls === 0) return 'lt-item__status--dead'
  // Yellow: low activity (long-tail)
  return 'lt-item__status--low'
}

function fmtCost(v: number) {
  const sym = currencySymbol((props.currency || 'CNY') as Currency)
  if (v < 0.01) return `< ${sym}0.01`
  return `${sym}${v.toFixed(2)}`
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
  color: var(--el-color-warning);
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

/* ── CSS Grid height transition (no max-height hack) ── */
.lt-body {
  display: grid;
  grid-template-rows: 0fr;
  transition: grid-template-rows 0.25s cubic-bezier(0.4, 0, 0.2, 1);
  border-top: 1px solid transparent;
}
.lt-body--open {
  grid-template-rows: 1fr;
  border-top-color: var(--el-border-color-lighter, #ebeef5);
}
.lt-body__inner {
  overflow: hidden;
  min-height: 0;
}

/* ── Sort toolbar ── */
.lt-toolbar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 8px 16px 4px;
  border-bottom: 1px solid var(--el-border-color-extra-light, #f2f3f5);
}
.lt-toolbar__label {
  font-size: 12px;
  color: var(--el-text-color-secondary, #909399);
  margin-right: 4px;
}
.lt-toolbar__btn {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 2px 8px;
  border: 1px solid var(--el-border-color-lighter, #e4e7ed);
  border-radius: 4px;
  background: transparent;
  font-size: 12px;
  color: var(--el-text-color-regular, #606266);
  cursor: pointer;
  transition: all 0.15s;
  line-height: 1.6;
}
.lt-toolbar__btn:hover {
  border-color: var(--el-color-primary, #409eff);
  color: var(--el-color-primary, #409eff);
}
.lt-toolbar__btn--active {
  border-color: var(--el-color-primary, #409eff);
  color: var(--el-color-primary, #409eff);
  background: var(--el-color-primary-light-9, #ecf5ff);
}
.lt-toolbar__arrow {
  font-size: 11px;
  font-weight: 600;
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
  background: var(--el-color-danger);
  box-shadow: 0 0 4px var(--el-color-danger-light-5);
}
.lt-item__status--low .lt-item__dot {
  background: var(--el-color-warning);
  box-shadow: 0 0 4px var(--el-color-warning-light-5);
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


</style>
