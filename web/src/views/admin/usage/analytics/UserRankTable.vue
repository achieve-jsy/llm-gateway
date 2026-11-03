<template>
  <div class="ur-wrap">
    <div v-if="!rows.length" class="meta empty">暂无数据</div>
    <table v-else>
      <thead>
        <tr>
          <th class="num">#</th>
          <th>用户</th>
          <th>部门</th>
          <th class="num">费用</th>
          <th class="num">占比</th>
          <th class="num">调用</th>
          <th class="num">$/调用</th>
          <th class="num">cache</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(u, i) in rows" :key="u.user_id + u.date" :class="{ top: i < 3 }">
          <td class="num rank">{{ i + 1 }}</td>
          <td class="u-name">{{ u.user_name }}</td>
          <td class="t-dim">{{ u.dept_name }}</td>
          <td class="num cost">{{ fmtC(u.cost) }}</td>
          <td class="num">
            <span class="bar">
              <span class="bar-track"><span class="bar-fill" :style="{ width: Math.min(u.pct, 100) + '%' }" /></span>
              <span class="t-muted">{{ u.pct.toFixed(1) }}%</span>
            </span>
          </td>
          <td class="num t-muted">{{ fmtN(u.calls) }}</td>
          <td class="num">{{ u.cpc.toFixed(4) }}</td>
          <td class="num t-muted">{{ u.cacheRate.toFixed(0) }}%</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { fmtC, fmtN } from './format'
import type { ModelUserDayRow } from '@/types/analytics'

defineOptions({ name: 'UserRankTable' })

const props = defineProps<{
  /** 用户日级明细（可能多日，按用户聚合） */
  userRows: ModelUserDayRow[] | null
}>()

interface UserView {
  user_id: string
  date: string
  user_name: string
  dept_name: string
  cost: number
  pct: number
  calls: number
  cpc: number
  cacheRate: number
}

const rows = computed<UserView[]>(() => {
  const agg = new Map<string, { user_id: string; date: string; user_name: string; dept_name: string; cost: number; calls: number; inT: number; cached: number }>()
  for (const u of props.userRows || []) {
    if (!agg.has(u.user_id)) agg.set(u.user_id, { user_id: u.user_id, date: u.date, user_name: u.user_name, dept_name: u.dept_name, cost: 0, calls: 0, inT: 0, cached: 0 })
    const a = agg.get(u.user_id)!
    a.cost += u.est_cost_usd
    a.calls += u.call_count
    a.inT += u.prompt_tokens
    a.cached += u.cached_tokens
  }
  const total = [...agg.values()].reduce((s, a) => s + a.cost, 0)
  return [...agg.values()]
    .map(a => ({
      ...a,
      pct: total > 0 ? (a.cost / total) * 100 : 0,
      cpc: a.calls > 0 ? a.cost / a.calls : 0,
      cacheRate: a.inT + a.cached > 0 ? (a.cached / (a.inT + a.cached)) * 100 : 0,
    }))
    .sort((a, b) => b.cost - a.cost)
    .slice(0, 50)
})
</script>

<style scoped>
.ur-wrap { font-size: 12.5px; }
table { width: 100%; border-collapse: collapse; }
thead th {
  font-size: 11px; color: var(--el-text-color-secondary); font-weight: 600;
  text-align: left; padding: 7px 10px; border-bottom: 1px solid var(--el-border-color);
}
th.num, td.num { text-align: right; }
td {
  padding: 8px 10px; border-bottom: 1px solid var(--el-border-color-lighter, rgba(0,0,0,.05));
  color: var(--el-text-color-regular);
}
tr.top td { background: rgba(54,179,78,.04); }
.u-name { font-weight: 600; color: var(--el-text-color-primary); }
.rank { color: var(--el-text-color-secondary); font-family: var(--font-mono); }
.cost { font-family: var(--font-mono); color: var(--el-text-color-primary); }
.t-dim { color: var(--el-text-color-secondary); font-size: 11.5px; }
.t-muted { color: var(--el-text-color-secondary); font-size: 11px; }
.bar { display: inline-flex; align-items: center; gap: 5px; }
.bar-track { display: inline-block; width: 52px; height: 6px; border-radius: 3px; background: var(--el-fill-color); overflow: hidden; vertical-align: middle; }
.bar-fill { display: block; height: 100%; background: var(--brand-green, #36b34e); }
.meta.empty { padding: 30px; text-align: center; color: var(--el-text-color-secondary); }
</style>
