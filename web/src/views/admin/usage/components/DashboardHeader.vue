<template>
  <div class="ud-header">
    <div class="ud-header__left">
      <h2 class="ud-title">用量看板</h2>
      <span class="ud-badge">{{ scopeLabel }}</span>
    </div>
    <div class="ud-header__right">
      <el-radio-group v-model="currency" size="small">
        <el-radio-button value="USD">$ USD</el-radio-button>
        <el-radio-button value="CNY">¥ CNY</el-radio-button>
      </el-radio-group>
      <el-button :loading="loading" :icon="Refresh"
        circle size="small" title="刷新" @click="$emit('refresh')" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { Refresh } from '@element-plus/icons-vue'
import type { Currency } from '@/utils/currency'

defineOptions({ name: 'DashboardHeader' })

defineProps<{
  scopeLabel: string
  loading: boolean
}>()

const currency = defineModel<Currency>('currency', { default: 'CNY' })

defineEmits<{
  refresh: []
}>()
</script>

<style scoped>
/* ── 页头 ── */
.ud-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
.ud-header__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.ud-header__right { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
.ud-title {
  font-size: 20px; font-weight: 700; margin: 0; letter-spacing: .2px;
  color: var(--el-text-color-primary);
  display: inline-flex; align-items: center; gap: 10px;
}
.ud-title::before {
  content: ''; width: 4px; height: 18px; border-radius: 3px;
  background: linear-gradient(180deg, var(--el-color-primary), var(--el-color-primary-dark-2, #1F8A3B));
}
.ud-badge {
  font-size: 11px; font-weight: 500; color: var(--el-color-primary);
  background: var(--el-color-primary-light-9); border: 1px solid var(--el-color-primary-light-7);
  border-radius: 999px; padding: 3px 12px; white-space: nowrap;
}

/* Mobile responsive header */
@media (max-width: 640px) {
  .ud-header {
    flex-direction: column;
    gap: 8px;
  }
  .ud-header__right {
    width: 100%;
    justify-content: flex-end;
  }
  .ud-title {
    font-size: 16px;
  }
}
</style>