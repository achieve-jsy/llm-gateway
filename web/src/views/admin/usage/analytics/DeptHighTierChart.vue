<template>
  <div class="hi-list">
    <div v-if="!rows.length" class="meta empty">暂无数据</div>
    <div v-for="x in rows" :key="x.dept_name" class="hi-row">
      <div class="hi-lbl">{{ x.dept_name }}</div>
      <div class="hi-track">
        <div
          class="hi-fill"
          :style="{ width: pctWidth(x.cost) + '%', background: barColor(x.cost) }"
        />
      </div>
      <div class="hi-val">
        <b>{{ fmtC(x.cost) }}</b>
        <span class="t-muted">{{ (x.pct ?? 0).toFixed(0) }}%</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { fmtC } from './format'
import type { ModelDeptSummaryRow } from '@/types/analytics'

defineOptions({ name: 'DeptHighTierChart' })

const props = defineProps<{
  deptRows: ModelDeptSummaryRow[] | null
  /** 全局高阶占比（insight.metrics.high_tier_pct）—— 后端 dept_rows 无逐行高阶字段，作注记 */
  highTierPct?: number
}>()

const rows = computed(() =>
  (props.deptRows || [])
    .map(d => ({ dept_name: d.dept_name, cost: d.est_cost_usd, pct: d.cost_pct }))
    .filter(x => x.cost > 0)
    .sort((a, b) => b.cost - a.cost),
)

const maxCost = computed(() => Math.max(1, ...rows.value.map(r => r.cost)))
function pctWidth(cost: number): number {
  return Math.max(2, (cost / maxCost.value) * 100)
}
function barColor(_cost: number): string {
  return 'var(--brand-green, #36b34e)'
}
</script>

<style scoped>
.hi-list { display: flex; flex-direction: column; gap: 8px; }
.hi-row { display: flex; align-items: center; gap: 10px; font-size: 12px; }
.hi-lbl { width: 96px; flex-shrink: 0; color: var(--el-text-color-regular); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hi-track { flex: 1; height: 14px; border-radius: 7px; background: var(--el-fill-color); overflow: hidden; }
.hi-fill { height: 100%; border-radius: 7px; transition: width 0.3s; }
.hi-val { display: flex; align-items: baseline; gap: 6px; width: 110px; flex-shrink: 0; justify-content: flex-end; }
.hi-val b { font-family: var(--font-mono); font-size: 12px; color: var(--el-text-color-primary); }
.t-muted { color: var(--el-text-color-secondary); font-size: 11px; }
.meta.empty { padding: 30px; text-align: center; color: var(--el-text-color-secondary); }
</style>
