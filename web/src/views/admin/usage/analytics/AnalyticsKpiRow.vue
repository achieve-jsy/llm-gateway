<template>
  <div class="kpi-row">
    <!-- 总费用 + 预算条 -->
    <div class="kpi">
      <div class="kpi-label">总费用</div>
      <div class="kpi-row-1">
        <div class="kpi-val">{{ fmtC(metrics?.cost ?? 0) }}</div>
        <span class="delta" :class="costDelta.cls">{{ costDelta.txt }}</span>
      </div>
      <div class="kpi-sub2">{{ costSub }}</div>
      <div class="budget">
        <div class="budget-row"><span>区间预算</span><span>{{ budgetTxt }}</span></div>
        <div class="budget-track">
          <div class="budget-fill" :class="budgetCls" :style="{ width: budgetPct + '%' }" />
        </div>
      </div>
    </div>
    <!-- 人均费用 -->
    <div class="kpi">
      <div class="kpi-label">人均费用</div>
      <div class="kpi-row-1">
        <div class="kpi-val">{{ metrics ? fmtC(perUser) : '–' }}</div>
        <span v-if="metrics" class="delta" :class="perUserDelta.cls">{{ perUserDelta.txt }}</span>
      </div>
      <div class="kpi-sub2">均值会被头部用户拉高 · 见部门 vs 中位</div>
    </div>
    <!-- 缓存命中率 -->
    <div class="kpi">
      <div class="kpi-label">缓存命中率</div>
      <div class="kpi-row-1">
        <div class="kpi-val">{{ metrics ? (metrics.cache_rate ?? 0).toFixed(1) + '%' : '–' }}</div>
      </div>
      <div class="kpi-sub2">命中率越高 → 输入费用越低</div>
    </div>
    <!-- 单次调用成本 -->
    <div class="kpi">
      <div class="kpi-label">单次调用成本</div>
      <div class="kpi-row-1">
        <div class="kpi-val">{{ metrics ? '$' + cpc.toFixed(4) : '–' }}</div>
      </div>
      <div class="kpi-sub2">{{ totalCalls ? `共 ${fmtN(totalCalls)} 次调用` : '—' }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { fmtC, fmtN, deltaPct, deltaText } from './format'
import type { InsightMetrics } from '@/types/analytics'
import type { ModelUsersSummary } from '@/types/analytics'

defineOptions({ name: 'AnalyticsKpiRow' })

const props = defineProps<{
  metrics: InsightMetrics | null
  /** model-users summary（补人数/调用次数 → 人均、单次成本） */
  summary: ModelUsersSummary | null
  /** 区间天数（预算 = BUDGET_PER_DAY × 天数） */
  days: number
}>()

const BUDGET_PER_DAY = 1400

const totalUsers = computed(() => props.summary?.total_users ?? 0)
const totalCalls = computed(() => props.summary?.total_calls ?? 0)

const perUser = computed(() => {
  const c = props.metrics?.cost ?? 0
  return totalUsers.value > 0 ? c / totalUsers.value : 0
})

const cpc = computed(() => {
  const c = props.metrics?.cost ?? 0
  return totalCalls.value > 0 ? c / totalCalls.value : 0
})

const costDelta = computed(() => deltaText(props.metrics?.delta_pct ?? 0))
const perUserDelta = computed(() => {
  const m = props.metrics
  if (!m) return { cls: 'flat', txt: '持平' }
  return deltaText(deltaPct(perUser.value, m.prev_cost / Math.max(1, totalUsers.value)))
})

const budgetTxt = computed(() => {
  const m = props.metrics
  if (!m) return '– / –'
  const budget = BUDGET_PER_DAY * props.days
  return `${fmtC(m.cost)} / ${fmtC(budget)}`
})
const budgetPct = computed(() => {
  const m = props.metrics
  if (!m) return 0
  const budget = BUDGET_PER_DAY * props.days
  return budget > 0 ? Math.min(100, (m.cost / budget) * 100) : 0
})
const budgetCls = computed(() => {
  const p = budgetPct.value
  return p >= 100 ? 'over' : p >= 80 ? 'warn' : ''
})

const costSub = computed(() => {
  const m = props.metrics
  if (!m) return '—'
  const days = Math.max(1, props.days)
  return `共 ${totalUsers.value} 人 · 日均 ${fmtC(m.cost / days)}`
})
</script>

<style scoped>
.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}
@media (max-width: 1100px) { .kpi-row { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 640px) { .kpi-row { grid-template-columns: 1fr; } }

.kpi {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: var(--radius-base, 8px);
  padding: 13px 15px;
  box-shadow: var(--shadow-sm);
}
.kpi-label { font-size: 11.5px; color: var(--el-text-color-secondary); margin-bottom: 6px; }
.kpi-row-1 { display: flex; align-items: baseline; gap: 8px; }
.kpi-val {
  font-size: 21px; font-weight: 700; font-family: var(--font-mono);
  color: var(--el-text-color-primary); letter-spacing: -0.01em;
}
.kpi-sub2 { font-size: 11px; color: var(--el-text-color-secondary); margin-top: 5px; }

.delta { font-size: 11px; font-weight: 700; border-radius: 4px; padding: 1px 5px; }
.delta.up { color: #dc4848; background: rgba(220,72,72,.1); }
.delta.down { color: #1f8a3b; background: rgba(54,179,78,.1); }
.delta.flat { color: var(--el-text-color-secondary); background: var(--el-fill-color); }

.budget { margin-top: 9px; }
.budget-row {
  display: flex; justify-content: space-between;
  font-size: 11px; color: var(--el-text-color-secondary); margin-bottom: 4px;
}
.budget-track {
  height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden;
}
.budget-fill {
  height: 100%; border-radius: 3px; background: var(--brand-green, #36b34e);
  transition: width 0.3s;
}
.budget-fill.warn { background: #e6a23c; }
.budget-fill.over { background: #dc4848; }
</style>
