<template>
  <el-table
    :data="rows"
    v-loading="loading"
    row-key="user_id"
    border
    size="small"
  >
    <el-table-column prop="name" label="姓名" min-width="90">
      <template #default="{ row }">{{ row.name }}</template>
    </el-table-column>

    <el-table-column label="象限" width="130">
      <template #default="{ row }">
        <el-tag :type="quadrantTag(row.quadrant)" size="small" effect="light">{{ quadrantLabel(row.quadrant) }}</el-tag>
      </template>
    </el-table-column>

    <el-table-column label="成本" width="120" align="right">
      <template #default="{ row }">{{ fmtCost(row.cost_usd) }}</template>
    </el-table-column>

    <el-table-column label="问题占比" width="110" align="right">
      <template #default="{ row }">{{ pct(row.issue_cost_ratio) }}</template>
    </el-table-column>

    <el-table-column label="主导类型" width="110">
      <template #default="{ row }">{{ dominantLabel(row.dominant_issue) }}</template>
    </el-table-column>

    <el-table-column label="调用次数" width="100" align="right">
      <template #default="{ row }">{{ row.calls }}</template>
    </el-table-column>

    <el-table-column label="结论" min-width="100">
      <template #default>
        <span class="nml-ok">用法正常</span>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, type CurrencyConfig, formatCost } from '@/utils/currency'
import type { MemberPoint, Quadrant, DominantIssue } from '@/types/usage-report'

const props = withDefaults(defineProps<{
  members: MemberPoint[]
  currency?: Currency
  loading?: boolean
}>(), { currency: 'CNY', loading: false })

const settingsStore = useSettingsStore()
const cc = computed<CurrencyConfig>(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

const QUADRANT_LABEL: Record<Quadrant, string> = {
  priority_improve: '右上·优先改善',
  coach: '左上·习惯辅导',
  high_cost_normal: '右下·高成本正常',
  routine: '左下·常规',
}
const QUADRANT_TAG: Record<Quadrant, 'success' | 'warning' | 'info' | 'danger'> = {
  priority_improve: 'danger',
  coach: 'warning',
  high_cost_normal: 'info',
  routine: 'success',
}
const DOMINANT_LABEL: Record<DominantIssue, string> = {
  normal: '正常',
  prefix_pollution: '前缀污染',
  toothpaste: '挤牙膏',
  multi_task_session: '多任务',
  output_runaway: '输出失控',
  over_model: '杀鸡用牛刀',
}

// 正常使用：非严重异常且不在「问题人」象限（routine / high_cost_normal）
const rows = computed(() =>
  props.members
    .filter(m => !m.safety_anomaly && m.quadrant !== 'priority_improve' && m.quadrant !== 'coach')
    .sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)),
)

function quadrantLabel(q: Quadrant): string {
  return QUADRANT_LABEL[q as Quadrant] ?? q
}
function quadrantTag(q: Quadrant): 'success' | 'warning' | 'info' | 'danger' {
  return QUADRANT_TAG[q as Quadrant] ?? 'info'
}
function dominantLabel(d: DominantIssue): string {
  return DOMINANT_LABEL[d as DominantIssue] ?? d ?? '—'
}
function pct(v: number): string {
  return `${(v * 100).toFixed(1)}%`
}
function fmtCost(v: number): string {
  return formatCost(Number(v || 0), cc.value)
}
</script>

<style scoped>
.nml-ok { font-size: 12px; color: var(--el-color-success); }
</style>
