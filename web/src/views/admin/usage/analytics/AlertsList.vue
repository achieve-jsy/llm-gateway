<template>
  <div v-if="alerts.length" class="alerts">
    <div v-for="(a, i) in alerts" :key="i" class="alert" :class="a.lvl">
      <span class="dot" aria-hidden="true" />
      <span v-html="a.txt" />
      <span class="act" @click="$emit('drill', a.drill)">下钻 →</span>
    </div>
  </div>
  <div v-else-if="!loading" class="alerts alerts--empty">当前范围暂无异常，指标受控。</div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { fmtC, median } from './format'
import type { ModelDeptSummaryRow } from '@/types/analytics'

defineOptions({ name: 'AlertsList' })

const props = defineProps<{
  deptRows: ModelDeptSummaryRow[] | null
  /** 全司人均中位数（全局参照） */
  gmed: number
  loading?: boolean
}>()

defineEmits<{ drill: [payload: { dept: string; scroll?: string }] }>()

interface AlertItem {
  lvl: 'danger' | 'warn'
  txt: string
  drill: { dept: string; scroll: string }
}

/** 部门人均超标预警：人均/中位 ≥3 危险，≥2 关注（对齐原型 buildAlerts） */
const alerts = computed<AlertItem[]>(() => {
  const rows = props.deptRows || []
  const med = props.gmed || median([])
  const list: AlertItem[] = []
  for (const d of rows) {
    if (!d.user_count) continue
    const avg = d.cost_per_user
    if (med > 0 && avg / med > 3) {
      list.push({
        lvl: 'danger',
        txt: `<b>${d.dept_name}</b> 人均 ${fmtC(avg)}，超全司中位数 ${(avg / med).toFixed(1)}x`,
        drill: { dept: d.dept_name, scroll: 'dept' },
      })
    } else if (med > 0 && avg / med > 2) {
      list.push({
        lvl: 'warn',
        txt: `<b>${d.dept_name}</b> 人均 ${fmtC(avg)}，超全司中位数 ${(avg / med).toFixed(1)}x`,
        drill: { dept: d.dept_name, scroll: 'dept' },
      })
    }
  }
  return list
})
</script>

<style scoped>
.alerts { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
.alerts--empty {
  padding: 12px 14px; font-size: 12.5px; color: var(--el-text-color-secondary);
  border: 1px dashed var(--el-border-color); border-radius: var(--radius-base, 8px);
}
.alert {
  display: flex; align-items: center; gap: 8px;
  font-size: 12.5px; line-height: 1.6; padding: 8px 12px;
  border-radius: var(--radius-base, 8px); border: 1px solid;
}
.alert :deep(b) { color: var(--el-text-color-primary); }
.alert.danger { border-color: rgba(220,72,72,.4); background: rgba(220,72,72,.07); }
.alert.warn { border-color: rgba(230,162,60,.4); background: rgba(230,162,60,.08); }
.dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.alert.danger .dot { background: #dc4848; }
.alert.warn .dot { background: #e6a23c; }
.act {
  margin-left: auto; font-size: 11.5px; color: var(--brand-green-text, #2f9e4a);
  font-weight: 600; cursor: pointer; flex-shrink: 0;
}
.act:hover { text-decoration: underline; }
</style>
