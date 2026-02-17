<template>
  <div v-if="props.show" class="ud-section">
    <div class="ud-section__header">
      <span class="ud-section__title">花销构成</span>
      <span class="ud-section__hint">{{ props.currentScope === 'dept' ? '成员' : '子部门' }} · {{ props.dateRange[0] }} ~ {{ props.dateRange[1] }}</span>
    </div>
    <div class="ud-breakdown-body">
      <!-- 预算池 -->
      <div v-if="props.budgetPoolItems.length > 0" class="ud-breakdown-budgets">
        <div v-for="item in props.budgetPoolItems" :key="item.scope_type + item.scope_id" class="ud-budget-pool">
          <div class="ud-budget-pool__hdr">
            <span class="ud-budget-pool__label">{{ item.label }}</span>
            <span :class="['ud-budget-pool__ratio', item.used_ratio >= 1 ? 'c-red fw6' : item.used_ratio >= 0.8 ? 'c-orange fw6' : 'c-green fw6']">
              {{ Math.round((item.used_ratio || 0) * 100) }}%
            </span>
          </div>
          <el-progress
            :percentage="Math.min((item.used_ratio || 0) * 100, 100)"
            :status="item.used_ratio >= 1 ? 'exception' : item.used_ratio >= 0.8 ? 'warning' : 'success'"
            :stroke-width="6"
            :show-text="false"
            class="ud-budget-progress"
          />
          <div class="ud-budget-pool__amts">
            <span>已用 {{ fmtCost(item.used_usd) }}</span>
            <span>共 {{ fmtCost(item.amount_usd) }}</span>
          </div>
        </div>
      </div>
      <!-- Top 5 成员/部门 -->
      <div class="ud-breakdown-top5">
        <div class="ud-breakdown-heading">{{ props.currentScope === 'dept' ? '成员' : '子部门' }}花销 Top 5</div>
        <div v-if="props.top5Breakdown.length > 0" class="ud-top5-list">
          <div v-for="(row, i) in props.top5Breakdown" :key="row.id ?? i" class="ud-top5-item">
            <span :class="['ud-rank-num', i < 3 ? `ud-rank-num--top${i+1}` : '']">{{ i + 1 }}</span>
            <span class="ud-top5-name">{{ row.name || row.id || '—' }}</span>
            <div class="ud-top5-bar"><div class="ud-top5-fill" :style="{ width: barPct(row.cost_estimate, props.top5Max) + '%' }" /></div>
            <span class="ud-top5-cost c-green fw6">{{ fmtCost(row.cost_estimate) }}</span>
            <span class="ud-top5-pct">{{ barPct(row.cost_estimate, props.totalCost) }}%</span>
          </div>
        </div>
        <div v-else class="ud-chart-empty" style="position:static;padding:16px 0">暂无数据</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import ElProgress from 'element-plus'
import { useSettingsStore } from '@/stores/settings'
import { type Currency, currencySymbol, convertCost } from '@/utils/currency'

export interface CostBreakdownSectionProps {
  show: boolean
  currentScope: string
  dateRange: [string, string]
  budgetPoolItems: any[]
  top5Breakdown: any[]
  top5Max: number
  totalCost: number
  currency?: Currency
}

const props = withDefaults(defineProps<CostBreakdownSectionProps>(), {
  currency: 'CNY',
})

const settingsStore = useSettingsStore()
const cc = computed(() => ({
  currency: props.currency ?? 'CNY',
  rate: settingsStore.publicSettings.pricing_usd_to_cny_rate || 7.2,
}))

function fmtCost(v?: number | null): string {
  const n = convertCost(Number(v || 0), cc.value)
  const sym = currencySymbol(cc.value.currency)
  if (n === 0) return `${sym}0.00`
  let s: string
  if (Math.abs(n) >= 1)        s = n.toFixed(2)
  else if (Math.abs(n) >= 0.01) s = n.toFixed(4)
  else if (Math.abs(n) >= 0.0001) s = n.toFixed(6)
  else s = n.toFixed(8)
  return sym + s.replace(/(\.\d{2,}?)0+$/, '$1')
}

function barPct(v: number, max: number) {
  return !max || !v ? 0 : Math.round(v / max * 100)
}
</script>

<style scoped>
/* ── Section 通用 ── */
.ud-section {
  background: var(--ud-surface, var(--el-fill-color-blank));
  border: 1px solid var(--ud-border, var(--el-border-color-light));
  border-radius: 14px;
}
.ud-section__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 14px 6px; border-bottom: 1px solid var(--ud-border, var(--el-border-color-light));
  flex-wrap: wrap; gap: 6px;
}
.ud-section__title { font-size: 14px; font-weight: 600; color: var(--el-text-color-primary); }
.ud-section__hint  { font-size: 12px; color: var(--ud-faint, var(--el-text-color-placeholder)); }

/* ── 花销构成区块 ── */
.ud-breakdown-body {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 0;
}
@media (max-width: 768px) { .ud-breakdown-body { grid-template-columns: 1fr; } }

.ud-breakdown-budgets {
  border-right: 1px solid var(--ud-border, var(--el-border-color-light));
  padding: 10px 14px;
  display: flex; flex-direction: column; gap: 8px;
}
.ud-breakdown-top5 {
  padding: 10px 14px;
}
.ud-breakdown-heading {
  font-size: 12px; font-weight: 600; color: var(--ud-muted, var(--el-text-color-secondary));
  text-transform: uppercase; letter-spacing: .04em;
  margin-bottom: 12px;
}

.ud-budget-pool { display: flex; flex-direction: column; gap: 5px; }
.ud-budget-pool__hdr { display: flex; justify-content: space-between; align-items: center; font-size: 12px; }
.ud-budget-pool__label { color: var(--el-text-color-regular); font-weight: 500; }
.ud-budget-pool__ratio { font-size: 12px; }
.ud-budget-pool__amts  { display: flex; justify-content: space-between; font-size: 11px; color: var(--ud-faint, var(--el-text-color-placeholder)); }
.ud-budget-progress :deep(.el-progress-bar__outer) { border-radius: 3px; }
.ud-budget-progress :deep(.el-progress-bar__inner) { border-radius: 3px; }

.ud-top5-list { display: flex; flex-direction: column; gap: 8px; }
.ud-top5-item { display: flex; align-items: center; gap: 8px; }
.ud-top5-name { flex: 1; font-size: 13px; color: var(--el-text-color-primary); min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ud-top5-bar  { width: 80px; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; flex-shrink: 0; }
.ud-top5-fill { height: 100%; border-radius: 3px; background: linear-gradient(90deg, #67c23a, #95d475); }
.ud-top5-cost { font-size: 12px; white-space: nowrap; min-width: 60px; text-align: right; }
.ud-top5-pct  { font-size: 11px; color: var(--ud-muted, var(--el-text-color-secondary)); min-width: 36px; text-align: right; }

.ud-rank-num {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px; border-radius: 6px;
  background: var(--el-fill-color); color: var(--ud-muted, var(--el-text-color-secondary));
  font-size: 12px; font-weight: 600;
}
.ud-rank-num--top1 { background: #ffd700; color: #7a5f00; }
.ud-rank-num--top2 { background: #c0c0c0; color: #555; }
.ud-rank-num--top3 { background: #cd7f32; color: #fff; }

.ud-chart-empty {
  display: flex; align-items: center; justify-content: center;
  color: var(--ud-faint, var(--el-text-color-placeholder)); font-size: 13px;
}

/* ── 颜色工具 ── */
.c-green  { color: #67c23a; }
.c-orange { color: #e6a23c; }
.c-red    { color: #f56c6c; }
.fw6      { font-weight: 600; }
</style>