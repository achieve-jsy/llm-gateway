<template>
  <section class="tier-panel" aria-labelledby="tier-limits-heading">
    <header>
      <div>
        <h2 id="tier-limits-heading">等级子预算摘要</h2>
        <p>所有调整统一在预算配置抽屉中预览并保存。</p>
      </div>
      <el-button :loading="loading" @click="load">刷新</el-button>
    </header>
    <div v-loading="loading" class="tier-panel__summary">
      <TierBudgetTags :items="tagItems" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { listBudgetTierLimits } from '@/services/control-plane'
import type { BudgetTierLimit } from '@/types/model-tier'
import type { TierBudgetTagItem } from '@/composables/useBudgetDisplay'
import { formatBudgetUSD } from '@/composables/useBudgetDisplay'
import TierBudgetTags from './TierBudgetTags.vue'

const props = defineProps<{ allocationId: string }>()
const rows = ref<BudgetTierLimit[]>([])
const loading = ref(false)

const tagItems = computed<TierBudgetTagItem[]>(() => rows.value.map(row => {
  const usedAmount = Number(row.used_usd ?? 0)
  const limitUSD = row.effective_limit_usd ?? row.limit_usd ?? row.base_limit_usd
  const limitAmount = Number(limitUSD)
  const ratio = Number.isFinite(usedAmount) && Number.isFinite(limitAmount) && limitAmount > 0
    ? usedAmount / limitAmount
    : 0
  const state: TierBudgetTagItem['state'] = !row.enabled
    ? 'disabled'
    : limitAmount === 0
      ? 'blocked'
      : ratio >= 0.8
        ? 'warning'
        : 'normal'
  const status = !row.enabled
    ? '已停用'
    : limitAmount === 0
      ? '$0 · 不可使用'
      : formatBudgetUSD(limitUSD)
  return {
    id: row.id ?? `${row.model_tier_id}:${row.period_type}`,
    name: row.model_tier_name ?? row.model_tier_id,
    usageRatio: ratio,
    state,
    label: `${row.model_tier_name ?? row.model_tier_id} · ${period(row.period_type)} ${status}`,
  }
}))

async function load() {
  if (!props.allocationId) return
  loading.value = true
  try { rows.value = await listBudgetTierLimits(props.allocationId) } finally { loading.value = false }
}
const period = (value: string) => ({ daily: '日', weekly: '周', monthly: '月' }[value] || value)

watch(() => props.allocationId, (newId) => { if (newId) load() })

defineExpose({ refresh: load })
onMounted(load)
</script>

<style scoped>
.tier-panel { margin: 18px 0; padding: 18px; border: 1px solid var(--el-border-color-light); border-radius: 8px; }
header { display: flex; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
h2 { margin: 0 0 5px; font-size: 18px; }
p { margin: 0; color: var(--el-text-color-secondary); font-size: 13px; }
.tier-panel__summary { min-height: 32px; }
</style>
