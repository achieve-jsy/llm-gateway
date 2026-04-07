<template>
  <section class="tier-panel" aria-labelledby="tier-limits-heading">
    <header><div><h2 id="tier-limits-heading">等级额度</h2><p>未配置表示只受总预算约束；$0 表示禁止使用；停用表示等级不可用于新关联。</p></div><el-button :loading="loading" @click="load">刷新</el-button></header>
    <el-table v-loading="loading" :data="rows" empty-text="暂无等级配置">
      <el-table-column prop="model_tier_name" label="模型等级" min-width="140" />
      <el-table-column prop="period_type" label="周期" width="90"><template #default="{ row }">{{ period(row.period_type) }}</template></el-table-column>
      <el-table-column label="额度" min-width="150">
        <template #default="{ row }">
          <el-tag v-if="!row.enabled" type="info">停用</el-tag>
          <el-tag v-else-if="Number(row.limit_usd) === 0" type="danger">$0 · 禁止使用</el-tag>
          <span v-else>${{ row.limit_usd }}</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100"><template #default="{ row }"><el-button link type="primary" @click="edit(row)">调整</el-button></template></el-table-column>
    </el-table>
    <BudgetAdjustDrawer
      v-if="selected"
      v-model="drawer"
      :allocation-id="allocationId"
      :model-tier-id="selected.model_tier_id"
      :initial="selected"
      @saved="load"
    />
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { listBudgetTierLimits } from '@/services/control-plane'
import type { BudgetTierLimit } from '@/types/model-tier'
import BudgetAdjustDrawer from './BudgetAdjustDrawer.vue'
const props = defineProps<{ allocationId: string }>()
const rows = ref<BudgetTierLimit[]>([])
const selected = ref<BudgetTierLimit | null>(null)
const loading = ref(false)
const drawer = ref(false)
async function load() {
  if (!props.allocationId) return
  loading.value = true
  try { rows.value = await listBudgetTierLimits(props.allocationId) } finally { loading.value = false }
}
function edit(row: BudgetTierLimit) { selected.value = row; drawer.value = true }
const period = (value: string) => ({ daily: '日', weekly: '周', monthly: '月' }[value] || value)
onMounted(load)
</script>

<style scoped>
.tier-panel { margin: 18px 0; padding: 18px; border: 1px solid var(--el-border-color-light); border-radius: 8px; }
header { display: flex; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
h2 { margin: 0 0 5px; font-size: 18px; }
p { margin: 0; color: var(--el-text-color-secondary); font-size: 13px; }
</style>
