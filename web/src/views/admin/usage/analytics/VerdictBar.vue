<template>
  <div v-if="verdict" class="verdict" :class="verdict.cls">
    <span class="v-dot" aria-hidden="true" />
    <div class="v-body">
      <div class="v-line1">
        <span class="status">{{ verdict.status }}</span>
        总费用 <b class="cost">{{ fmtC(metrics.cost) }}</b>
      </div>
      <div class="v-line2" v-html="verdict.line2" />
    </div>
    <a class="v-action" href="#sec-alerts" @click.prevent="$emit('jump', 'alerts')">查看异常清单 →</a>
  </div>
  <div v-else class="verdict verdict--loading">
    <span class="v-dot" aria-hidden="true" />
    <div class="v-body">
      <div class="v-line1"><span class="status">加载中</span></div>
      <div class="v-line2">正在汇总区间指标…</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { fmtC } from './format'
import type { InsightMetrics } from '@/types/analytics'

defineOptions({ name: 'VerdictBar' })

const props = defineProps<{ metrics: InsightMetrics | null }>()
defineEmits<{ jump: [sec: string] }>()

interface VerdictView {
  cls: 'bad' | 'warn' | 'ok'
  status: string
  line2: string
}

/** 红绿灯裁决：对齐原型 renderVerdict —— 超支 > 需干预 > 需关注 > 受控 */
const verdict = computed<VerdictView | null>(() => {
  const m = props.metrics
  if (!m) return null
  const { delta_pct, budget_pct, high_tier_pct, cost, prev_cost } = m
  const d = delta_pct ?? 0
  const absD = Math.abs(d)
  const over = (m.budget_configured && (budget_pct ?? 0) >= 100)
  const hiHp = (high_tier_pct ?? 0) >= 65
  const hiDelta = d > 12

  if (over) {
    return {
      cls: 'bad',
      status: '超支',
      line2: `区间费用 <b class="cost">${fmtC(cost)}</b> 已超预算，高阶模型占比 <b>${(high_tier_pct ?? 0).toFixed(1)}%</b>。`,
    }
  }
  if (hiHp && hiDelta) {
    return {
      cls: 'bad',
      status: '需干预',
      line2: `费用环比 <b class="t-red">+${absD.toFixed(1)}%</b> 且高阶占比 <b class="t-red">${(high_tier_pct ?? 0).toFixed(1)}%</b>，重点排查高阶模型用量与人均异常部门。`,
    }
  }
  if (hiDelta || hiHp) {
    const parts = []
    if (hiDelta) parts.push(`费用环比 <b class="t-red">${d > 0 ? '+' : ''}${absD.toFixed(1)}%</b>`)
    if (hiHp) parts.push(`高阶模型占比 <b class="t-amber">${(high_tier_pct ?? 0).toFixed(1)}%</b>`)
    parts.push('建议关注下列异常清单。')
    return { cls: 'warn', status: '需关注', line2: parts.join('，') }
  }
  const prev = prev_cost ?? 0
  return {
    cls: 'ok',
    status: '受控',
    line2: `费用环比 <b class="t-green">${d > 0 ? '+' : ''}${absD.toFixed(1)}%</b>（上区间 ${fmtC(prev)}），高阶占比 <b>${(high_tier_pct ?? 0).toFixed(1)}%</b> 受控，无需干预。`,
  }
})
</script>

<style scoped>
.verdict {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-radius: var(--radius-base, 8px);
  border: 1px solid;
  margin-bottom: 14px;
}
.verdict--loading { border-color: var(--el-border-color); background: var(--el-fill-color-light); }
.verdict.ok { border-color: var(--brand-green-line, rgba(54,179,78,.4)); background: rgba(54,179,78,.08); }
.verdict.warn { border-color: rgba(230,162,60,.5); background: rgba(230,162,60,.1); }
.verdict.bad { border-color: rgba(220,72,72,.5); background: rgba(220,72,72,.1); }

.v-dot {
  width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0;
}
.ok .v-dot { background: #36b34e; box-shadow: 0 0 0 3px rgba(54,179,78,.2); }
.warn .v-dot { background: #e6a23c; box-shadow: 0 0 0 3px rgba(230,162,60,.2); }
.bad .v-dot { background: #dc4848; box-shadow: 0 0 0 3px rgba(220,72,72,.2); }

.v-body { flex: 1; min-width: 0; }
.v-line1 { font-size: 14px; font-weight: 700; color: var(--el-text-color-primary); }
.v-line1 .status {
  display: inline-block; font-size: 11px; font-weight: 700; padding: 1px 8px;
  border-radius: 999px; margin-right: 8px; vertical-align: 1px;
}
.ok .status { color: #1f8a3b; background: rgba(54,179,78,.14); }
.warn .status { color: #b87112; background: rgba(230,162,60,.16); }
.bad .status { color: #c0392b; background: rgba(220,72,72,.14); }
.v-line2 { font-size: 12.5px; color: var(--el-text-color-regular); margin-top: 3px; line-height: 1.6; }
.v-line2 :deep(b) { color: var(--el-text-color-primary); }
.v-line2 :deep(.cost) { font-family: var(--font-mono); color: var(--el-text-color-primary); font-weight: 700; }
.v-line2 :deep(.t-red) { color: #dc4848; font-weight: 700; }
.v-line2 :deep(.t-amber) { color: #b87112; font-weight: 700; }
.v-line2 :deep(.t-green) { color: #1f8a3b; font-weight: 700; }

.v-action {
  font-size: 12px; color: var(--brand-green-text, #2f9e4a); font-weight: 600;
  text-decoration: none; flex-shrink: 0;
}
.v-action:hover { text-decoration: underline; }
</style>
