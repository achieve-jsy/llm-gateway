<template>
  <el-table
    :data="rows"
    v-loading="loading"
    row-key="user_id"
    :default-expand-all="false"
    border
    size="small"
  >
    <el-table-column type="expand">
      <template #default="{ row }">
        <div class="pml-expand">
          <div v-if="row.usage_issues?.length" class="pml-cards">
            <IssueDiagnosisCard
              v-for="issue in row.usage_issues"
              :key="issue.type"
              :issue="issue"
              :currency="currency"
            />
          </div>
          <div v-else class="pml-empty">该成员无命中情况详情</div>
        </div>
      </template>
    </el-table-column>

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

    <!-- 问题占比：只聚合五种情况归因（Y 轴） -->
    <el-table-column label="问题占比" width="110" align="right">
      <template #default="{ row }">{{ pct(row.issue_cost_ratio) }}</template>
    </el-table-column>

    <!-- 严重异常占比：独立列（读成员级 unrelated_cost_ratio，缺失时用 safety_anomaly「待核查」标记兜底），绝不与问题占比合并 -->
    <el-table-column label="严重异常占比" width="120" align="center">
      <template #default="{ row }">
        <span v-if="row.unrelated_cost_ratio != null">{{ pct(row.unrelated_cost_ratio) }}</span>
        <el-tag v-else-if="row.safety_anomaly" type="warning" size="small" effect="light">待核查</el-tag>
        <span v-else class="pml-muted">—</span>
      </template>
    </el-table-column>

    <el-table-column label="主导类型" width="110">
      <template #default="{ row }">{{ dominantLabel(row.dominant_issue) }}</template>
    </el-table-column>

    <el-table-column label="归属费用" width="120" align="right">
      <template #default="{ row }">{{ fmtCost(row.issue_attributed_cost_usd) }}</template>
    </el-table-column>

    <el-table-column label="建议" min-width="120">
      <template #default="{ row }">{{ advice(row) }}</template>
    </el-table-column>
  </el-table>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, type CurrencyConfig, formatCost } from '@/utils/currency'
import IssueDiagnosisCard from '@/components/usage-report/IssueDiagnosisCard.vue'
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

// 管理优先级：严重异常待核查 → 右上优先改善 → 左上习惯辅导（非总成本）
function priorityRank(m: MemberPoint): number {
  if (m.safety_anomaly) return 0
  if (m.quadrant === 'priority_improve') return 1
  if (m.quadrant === 'coach') return 2
  return 3
}

const rows = computed(() => {
  const problem = props.members.filter(m => m.safety_anomaly || m.quadrant === 'priority_improve' || m.quadrant === 'coach')
  return [...problem].sort((a, b) => {
    const r = priorityRank(a) - priorityRank(b)
    if (r !== 0) return r
    return (b.issue_cost_ratio ?? 0) - (a.issue_cost_ratio ?? 0)
  })
})

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
function advice(m: MemberPoint): string {
  if (m.safety_anomaly) return '待核查'
  if (m.quadrant === 'priority_improve') return '优先改善'
  if (m.quadrant === 'coach') return '习惯辅导'
  return '—'
}
</script>

<style scoped>
.pml-expand { padding: 8px 12px; }
.pml-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 8px;
}
.pml-empty {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  padding: 8px 0;
}
.pml-muted { color: var(--el-text-color-placeholder); }
</style>
