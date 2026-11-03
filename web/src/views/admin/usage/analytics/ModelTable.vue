<template>
  <div class="model-list">
    <div v-if="!rows.length" class="meta empty">暂无数据</div>
    <div
      v-for="r in rows"
      :key="r.model"
      class="model-row"
      :class="{ open: openModel === r.model }"
      @click="toggle(r.model)"
    >
      <div class="m-name">
        <span class="nm">{{ r.model }}</span>
      </div>
      <div class="m-num m-col-cost"><b>{{ fmtC(r.cost) }}</b></div>
      <div class="m-num m-col-delta">
        <span class="delta" :class="r.delta.cls">{{ r.delta.txt }}</span>
      </div>
      <div class="m-num m-col-metric">cache <b>{{ r.cacheRate.toFixed(0) }}%</b></div>
      <div class="m-num m-col-metric">{{ fmtN(r.calls) }} 次调用</div>
      <span class="m-caret">{{ openModel === r.model ? '▾' : '▸' }}</span>
    </div>

    <!-- 展开明细 -->
    <div v-if="detail && openModel" class="model-detail open">
      <div class="detail-title">
        {{ openModel }} · 24h 小时级明细（{{ fmtC(detail.cost) }} / {{ fmtN(detail.inT) }} in · {{ fmtN(detail.outT) }} out）
      </div>
      <div class="chip-group">
        <span class="chip" :class="{ on: detailDay === 'all' }" @click="detailDay = 'all'">全部</span>
        <span
          v-for="d in detail.days"
          :key="d"
          class="chip"
          :class="{ on: detailDay === d }"
          @click="detailDay = d"
        >{{ d.slice(5) }}</span>
      </div>
      <div class="grid-3">
        <div class="mini">
          <div class="mini-hd">小时费用</div>
          <div class="chart-box"><canvas ref="stackRef" /></div>
        </div>
        <div class="mini">
          <div class="mini-hd">小时 Token</div>
          <div class="chart-box"><canvas ref="tokRef" /></div>
        </div>
        <div class="mini">
          <div class="mini-hd">小时费用热力</div>
          <div class="heatmap">
            <div
              v-for="h in 24"
              :key="h"
              class="h-cell"
              :style="{ background: heatColor(heatByHour[h - 1] || 0) }"
              :title="`${String(h - 1).padStart(2, '0')}:00 · ${fmtC(heatByHour[h - 1] || 0)}`"
            />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onUnmounted, nextTick } from 'vue'
import { Chart, registerables } from 'chart.js'
import { fmtC, fmtN, deltaText } from './format'
import type { ModelHourlyPoint } from '@/types/analytics'

Chart.register(...registerables)

defineOptions({ name: 'ModelTable' })

const props = defineProps<{
  /** 当前区间小时级数据（group_by=model） */
  hourly: ModelHourlyPoint[] | null
  /** 上一等长区间小时级数据（供环比） */
  prevHourly: ModelHourlyPoint[] | null
  /** 模型等级映射（无 tier 时省略标签） */
  tierOf?: (model: string) => string
}>()

const openModel = ref<string | null>(null)
const detailDay = ref<string>('all')

const stackRef = ref<HTMLCanvasElement | null>(null)
const tokRef = ref<HTMLCanvasElement | null>(null)
let stackChart: Chart | null = null
let tokChart: Chart | null = null

const TOKEN_HEX = { p: '#3b82f6', c: '#f59e0b', o: '#10b981' }

interface ModelAgg {
  model: string
  cost: number
  prevCost: number
  delta: { cls: string; txt: string }
  calls: number
  cacheRate: number
}

const rows = computed<ModelAgg[]>(() => {
  const agg = new Map<string, ModelAgg>()
  for (const r of props.hourly || []) {
    const g = r.group_key
    if (!agg.has(g)) agg.set(g, { model: g, cost: 0, prevCost: 0, delta: { cls: 'flat', txt: '持平' }, calls: 0, cacheRate: 0 })
    const a = agg.get(g)!
    a.cost += r.est_cost_usd
    a.calls += r.call_count
  }
  const prevAgg = new Map<string, { cost: number; inT: number; cached: number }>()
  for (const r of props.prevHourly || []) {
    if (!prevAgg.has(r.group_key)) prevAgg.set(r.group_key, { cost: 0, inT: 0, cached: 0 })
    const p = prevAgg.get(r.group_key)!
    p.cost += r.est_cost_usd
    p.inT += r.prompt_tokens
    p.cached += r.cached_tokens
  }
  const out = [...agg.values()].map(a => {
    a.prevCost = prevAgg.get(a.model)?.cost ?? 0
    a.delta = deltaText(((a.cost - a.prevCost) / (a.prevCost || 1)) * 100)
    // 缓存率：当前区间按模型累计
    let inT = 0, cached = 0
    for (const r of props.hourly || []) {
      if (r.group_key !== a.model) continue
      inT += r.prompt_tokens
      cached += r.cached_tokens
    }
    a.cacheRate = inT + cached > 0 ? (cached / (inT + cached)) * 100 : 0
    return a
  })
  return out.sort((a, b) => b.cost - a.cost)
})

const detail = computed(() => {
  if (!openModel.value) return null
  const rows = (props.hourly || []).filter(r => r.group_key === openModel.value)
  const sel = detailDay.value === 'all' ? rows : rows.filter(r => r.hour.slice(0, 10) === detailDay.value)
  const cost = sel.reduce((s, r) => s + r.est_cost_usd, 0)
  const inT = sel.reduce((s, r) => s + r.prompt_tokens, 0)
  const cached = sel.reduce((s, r) => s + r.cached_tokens, 0)
  const outT = sel.reduce((s, r) => s + r.completion_tokens, 0)
  const days = [...new Set((props.hourly || []).filter(r => r.group_key === openModel.value).map(r => r.hour.slice(0, 10)))]
  return { cost, inT, cached, outT, days }
})

const heatByHour = computed(() => {
  const arr = new Array(24).fill(0)
  if (!openModel.value) return arr
  const rows = (props.hourly || []).filter(r => r.group_key === openModel.value)
  for (const r of rows) {
    const hh = parseInt(r.hour.slice(11, 13), 10)
    if (!isNaN(hh) && hh >= 0 && hh < 24) arr[hh] += r.est_cost_usd
  }
  return arr
})

const HEAT = ['#F1F5F9', '#DBEAFE', '#93C5FD', '#3B82F6', '#F59E0B']
function heatColor(v: number): string {
  if (v <= 0) return HEAT[0]
  const max = Math.max(1, ...heatByHour.value)
  const r = v / max
  if (r < 0.25) return HEAT[1]
  if (r < 0.5) return HEAT[2]
  if (r < 0.75) return HEAT[3]
  return HEAT[4]
}

function toggle(model: string) {
  openModel.value = openModel.value === model ? null : model
  detailDay.value = 'all'
  nextTick(renderDetail)
}

function destroyDetail() {
  if (stackChart) { stackChart.destroy(); stackChart = null }
  if (tokChart) { tokChart.destroy(); tokChart = null }
}

function renderDetail() {
  destroyDetail()
  if (!openModel.value) return
  const rows = (props.hourly || []).filter(r => r.group_key === openModel.value)
  const sel = detailDay.value === 'all' ? rows : rows.filter(r => r.hour.slice(0, 10) === detailDay.value)
  const hL = Array.from({ length: 24 }, (_, h) => String(h).padStart(2, '0') + ':00')

  const stackEl = stackRef.value
  if (stackEl) {
    const hrs = new Array(24).fill(0)
    for (const r of sel) {
      const hh = parseInt(r.hour.slice(11, 13), 10)
      if (!isNaN(hh)) hrs[hh] += r.est_cost_usd
    }
    stackChart = new Chart(stackEl, {
      type: 'bar',
      data: { labels: hL, datasets: [{ data: hrs, backgroundColor: '#36b34ecc', borderRadius: 2 }] },
      options: {
        responsive: true, maintainAspectRatio: false,
        plugins: { legend: { display: false }, tooltip: { callbacks: { label: (c: any) => ' ' + fmtC(c.parsed.y) } } },
        scales: {
          x: { grid: { display: false }, ticks: { font: { size: 9 }, maxTicksLimit: 12 } },
          y: { beginAtZero: true, grid: { color: 'rgba(100,116,139,.12)' }, ticks: { font: { size: 9 }, callback: (v: any) => '$' + v } },
        },
      },
    })
  }

  const tokEl = tokRef.value
  if (tokEl) {
    const p = new Array(24).fill(0), c = new Array(24).fill(0), o = new Array(24).fill(0)
    for (const r of sel) {
      const hh = parseInt(r.hour.slice(11, 13), 10)
      if (isNaN(hh)) continue
      p[hh] += r.prompt_tokens
      c[hh] += r.cached_tokens
      o[hh] += r.completion_tokens
    }
    tokChart = new Chart(tokEl, {
      type: 'bar',
      data: {
        labels: hL,
        datasets: [
          { label: '输入', data: p, backgroundColor: TOKEN_HEX.p + '99', stack: 't', borderRadius: 2 },
          { label: '缓存', data: c, backgroundColor: TOKEN_HEX.c + '99', stack: 't', borderRadius: 2 },
          { label: '输出', data: o, backgroundColor: TOKEN_HEX.o + '99', stack: 't', borderRadius: 2 },
        ],
      },
      options: {
        responsive: true, maintainAspectRatio: false,
        plugins: { legend: { labels: { boxWidth: 8, font: { size: 10 } } }, tooltip: { callbacks: { label: (c: any) => ` ${c.dataset.label} ${fmtN(c.parsed.y)}` } } },
        scales: {
          x: { stacked: true, grid: { display: false }, ticks: { font: { size: 9 }, maxTicksLimit: 12 } },
          y: { stacked: true, beginAtZero: true, grid: { color: 'rgba(100,116,139,.12)' }, ticks: { font: { size: 9 }, callback: (v: any) => fmtN(v) } },
        },
      },
    })
  }
}

watch(() => props.hourly, () => { if (openModel.value) nextTick(renderDetail) })
onUnmounted(destroyDetail)
</script>

<style scoped>
.model-list { display: flex; flex-direction: column; }
.model-row {
  display: flex; align-items: center; gap: 12px;
  padding: 9px 12px; font-size: 12.5px; cursor: pointer;
  border-bottom: 1px solid var(--el-border-color-lighter, rgba(0,0,0,.05));
  transition: background 0.15s;
}
.model-row:hover { background: var(--el-fill-color-light); }
.model-row.open { background: var(--el-fill-color-light); }
.m-name { flex: 1; min-width: 0; font-weight: 600; color: var(--el-text-color-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.m-num { text-align: right; font-family: var(--font-mono); }
.m-col-cost { width: 90px; }
.m-col-cost b { font-size: 13px; color: var(--el-text-color-primary); }
.m-col-delta { width: 80px; }
.m-col-metric { width: 90px; color: var(--el-text-color-regular); font-size: 11.5px; }
.m-col-metric b { color: var(--el-text-color-primary); }
.delta { font-size: 11px; font-weight: 700; border-radius: 4px; padding: 1px 5px; }
.delta.up { color: #dc4848; background: rgba(220,72,72,.1); }
.delta.down { color: #1f8a3b; background: rgba(54,179,78,.1); }
.delta.flat { color: var(--el-text-color-secondary); background: var(--el-fill-color); }
.m-caret { width: 14px; color: var(--el-text-color-secondary); flex-shrink: 0; }

.model-detail.open { padding: 14px; }
.detail-title { font-size: 13px; font-weight: 700; color: var(--el-text-color-primary); margin-bottom: 10px; }
.chip-group { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
.chip {
  font-size: 11px; padding: 2px 10px; border-radius: 999px; cursor: pointer;
  color: var(--el-text-color-regular); border: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}
.chip.on { color: var(--brand-green-text, #2f9e4a); border-color: var(--brand-green, #36b34e); background: rgba(54,179,78,.1); }

.grid-3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; }
@media (max-width: 900px) { .grid-3 { grid-template-columns: 1fr; } }
.mini-hd { font-size: 12px; font-weight: 600; color: var(--el-text-color-regular); margin-bottom: 6px; }
.chart-box { height: 180px; position: relative; }
.heatmap {
  display: grid; grid-template-columns: repeat(12, 1fr); gap: 3px;
}
.h-cell { height: 26px; border-radius: 3px; }

.meta.empty { padding: 30px; text-align: center; color: var(--el-text-color-secondary); }
</style>
