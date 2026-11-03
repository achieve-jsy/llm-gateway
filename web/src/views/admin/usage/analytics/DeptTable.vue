<template>
  <div class="dept-wrap">
    <div class="note">{{ note }}</div>
    <table>
      <thead>
        <tr>
          <th>部门</th>
          <th class="num">费用</th>
          <th class="num">环比</th>
          <th class="num">人数</th>
          <th class="num">人均</th>
          <th class="num">vs 中位</th>
          <th class="num">费用占比</th>
          <th>预警</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="d in rows" :key="d.dept_name">
          <tr class="dept-tr" @click="toggle(d.dept_name)">
            <td class="t-link">
              {{ d.dept_name }}
              <span class="caret">{{ openDept === d.dept_name ? '▾' : '▸' }}</span>
            </td>
            <td class="num cost">{{ fmtC(d.cost) }}</td>
            <td class="num">
              <span class="delta" :class="d.delta.cls">{{ d.delta.txt }}</span>
            </td>
            <td class="num">{{ d.user_count }}</td>
            <td class="num">{{ fmtC(d.avg) }}</td>
            <td class="num">
              <span :class="ratioCls(d.ratio)">{{ d.ratio.toFixed(1) }}x</span>
              <span class="t-dim"> 中位</span>
            </td>
            <td class="num">
              <span class="bar">
                <span class="bar-track">
                  <span class="bar-fill" :style="{ width: Math.min(d.pct, 100) + '%' }" />
                </span>
                <span class="t-muted">{{ d.pct.toFixed(0) }}%</span>
              </span>
            </td>
            <td>
              <span v-if="d.warn" class="badge" :class="d.warn.cls">{{ d.warn.txt }}</span>
              <span v-else class="t-muted">—</span>
            </td>
          </tr>
          <!-- 用户明细下钻 -->
          <tr v-if="openDept === d.dept_name" class="drill-row">
            <td colspan="8">
              <div class="drill">
                <h4>{{ d.dept_name }} · 用户明细（{{ d.users.length }} 人）</h4>
                <table class="sub">
                  <thead>
                    <tr>
                      <th>用户</th>
                      <th class="num">费用</th>
                      <th class="num">$/调用</th>
                      <th class="num">cache</th>
                      <th class="num">占比</th>
                      <th class="num">调用</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="u in d.users" :key="u.user_id">
                      <td class="u-name">{{ u.user_name }}</td>
                      <td class="num cost">{{ fmtC(u.cost) }}</td>
                      <td class="num">{{ u.cpc.toFixed(4) }}</td>
                      <td class="num">{{ u.cacheRate.toFixed(0) }}%</td>
                      <td class="num">
                        <span class="bar">
                          <span class="bar-track"><span class="bar-fill" :style="{ width: Math.min(u.pct, 100) + '%' }" /></span>
                          <span class="t-muted">{{ u.pct.toFixed(0) }}%</span>
                        </span>
                      </td>
                      <td class="num t-muted">{{ fmtN(u.calls) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </td>
          </tr>
        </template>
        <tr v-if="!rows.length">
          <td colspan="8" class="empty">暂无数据</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { fmtC, fmtN, median, deltaText } from './format'
import type { ModelDeptSummaryRow, ModelUserDayRow } from '@/types/analytics'

defineOptions({ name: 'DeptTable' })

const props = defineProps<{
  deptRows: ModelDeptSummaryRow[] | null
  prevDeptRows: ModelDeptSummaryRow[] | null
  /** 用户日级明细（按 dept_name 聚合下钻） */
  userRows?: ModelUserDayRow[] | null
  /** 全司人均中位数 */
  gmed: number
  periodLabel?: string
}>()

const openDept = ref<string | null>(null)

interface DeptView {
  dept_name: string
  cost: number
  user_count: number
  avg: number
  ratio: number
  pct: number
  delta: { cls: string; txt: string }
  warn: { cls: string; txt: string } | null
  users: Array<{ user_id: string; user_name: string; cost: number; cpc: number; cacheRate: number; pct: number; calls: number }>
}

const note = computed(() => props.periodLabel ? `人均 vs 全司中位数 · 点击部门展开用户明细（${props.periodLabel}）` : '人均 vs 全司中位数 · 点击部门展开用户明细')

const rows = computed<DeptView[]>(() => {
  const depts = props.deptRows || []
  const prevMap = new Map((props.prevDeptRows || []).map(d => [d.dept_name, d.est_cost_usd]))
  // 用户明细按 dept_name 聚合（来自 user_day_rows，若父级传入）
  const usersByDept = new Map<string, ModelUserDayRow[]>()
  for (const u of props.userRows || []) {
    if (!usersByDept.has(u.dept_name)) usersByDept.set(u.dept_name, [])
    usersByDept.get(u.dept_name)!.push(u)
  }
  const med = props.gmed || median([])
  return depts
    .filter(d => d.est_cost_usd > 0)
    .map(d => {
      const avg = d.user_count > 0 ? d.est_cost_usd / d.user_count : 0
      const ratio = med > 0 ? avg / med : 0
      const prev = prevMap.get(d.dept_name) ?? 0
      const delta = deltaText(prev > 0 ? ((d.est_cost_usd - prev) / prev) * 100 : 0)
      let warn: DeptView['warn'] = null
      if (ratio > 3) warn = { cls: 'badge-red', txt: '人均超标' }
      else if (ratio > 2) warn = { cls: 'badge-amber', txt: '人均偏高' }

      const us = (usersByDept.get(d.dept_name) || [])
      const uAgg = new Map<string, { user_id: string; user_name: string; cost: number; calls: number; inT: number; cached: number }>()
      for (const u of us) {
        if (!uAgg.has(u.user_id)) uAgg.set(u.user_id, { user_id: u.user_id, user_name: u.user_name, cost: 0, calls: 0, inT: 0, cached: 0 })
        const a = uAgg.get(u.user_id)!
        a.cost += u.est_cost_usd
        a.calls += u.call_count
        a.inT += u.prompt_tokens
        a.cached += u.cached_tokens
      }
      const uTotal = [...uAgg.values()].reduce((s, a) => s + a.cost, 0)
      const users = [...uAgg.values()]
        .sort((a, b) => b.cost - a.cost)
        .map(a => ({
          ...a,
          cpc: a.calls > 0 ? a.cost / a.calls : 0,
          cacheRate: a.inT + a.cached > 0 ? (a.cached / (a.inT + a.cached)) * 100 : 0,
          pct: uTotal > 0 ? (a.cost / uTotal) * 100 : 0,
        }))
      return {
        dept_name: d.dept_name,
        cost: d.est_cost_usd,
        user_count: d.user_count,
        avg,
        ratio,
        pct: d.cost_pct ?? 0,
        delta,
        warn,
        users,
      }
    })
    .sort((a, b) => b.cost - a.cost)
})

function toggle(dept: string) {
  openDept.value = openDept.value === dept ? null : dept
}
function ratioCls(r: number): string {
  if (r > 3) return 't-red'
  if (r > 2) return 't-amber'
  return 't-green'
}
</script>

<style scoped>
.dept-wrap { font-size: 12.5px; }
.note { font-size: 11.5px; color: var(--el-text-color-secondary); margin-bottom: 8px; }
table { width: 100%; border-collapse: collapse; }
thead th {
  font-size: 11px; color: var(--el-text-color-secondary); font-weight: 600;
  text-align: left; padding: 7px 10px; border-bottom: 1px solid var(--el-border-color);
}
th.num, td.num { text-align: right; }
.dept-tr { cursor: pointer; }
.dept-tr:hover td { background: var(--el-fill-color-light); }
td {
  padding: 9px 10px; border-bottom: 1px solid var(--el-border-color-lighter, rgba(0,0,0,.05));
  color: var(--el-text-color-regular);
}
.t-link { font-weight: 600; color: var(--el-text-color-primary); }
.caret { color: var(--el-text-color-secondary); margin-left: 4px; }
.cost { font-family: var(--font-mono); color: var(--el-text-color-primary); }
.delta { font-size: 11px; font-weight: 700; border-radius: 4px; padding: 1px 5px; }
.delta.up { color: #dc4848; background: rgba(220,72,72,.1); }
.delta.down { color: #1f8a3b; background: rgba(54,179,78,.1); }
.delta.flat { color: var(--el-text-color-secondary); background: var(--el-fill-color); }
.t-red { color: #dc4848; font-weight: 700; }
.t-amber { color: #b87112; font-weight: 700; }
.t-green { color: #1f8a3b; font-weight: 700; }
.t-dim { color: var(--el-text-color-secondary); font-size: 11px; }
.t-muted { color: var(--el-text-color-secondary); font-size: 11px; }
.badge { font-size: 10.5px; font-weight: 700; padding: 1px 7px; border-radius: 999px; }
.badge-red { color: #c0392b; background: rgba(220,72,72,.12); }
.badge-amber { color: #b87112; background: rgba(230,162,60,.16); }
.bar { display: inline-flex; align-items: center; gap: 5px; }
.bar-track { display: inline-block; width: 52px; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; vertical-align: middle; }
.bar-fill { display: block; height: 100%; background: var(--brand-green, #36b34e); }
.empty { text-align: center; padding: 20px; color: var(--el-text-color-secondary); }

.drill-row td { background: var(--el-fill-color-lighter, rgba(0,0,0,.02)); padding: 0 10px 14px; }
.drill h4 { font-size: 13px; margin: 0 0 8px; color: var(--el-text-color-primary); }
table.sub { margin-top: 4px; }
table.sub td { padding: 6px 10px; }
.u-name { font-weight: 600; color: var(--el-text-color-primary); }
</style>
