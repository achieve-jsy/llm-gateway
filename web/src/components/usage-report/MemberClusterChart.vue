<template>
  <div ref="containerRef" class="mcc" v-loading="loading">
    <div v-if="!loading && !members.length" class="mcc__empty">暂无成员数据</div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import * as echarts from 'echarts'
import type { MemberPoint, MemberCluster, DominantIssue } from '@/types/usage-report'

const props = withDefaults(defineProps<{
  members: MemberPoint[]
  clusters?: MemberCluster[]
  loading?: boolean
}>(), { clusters: () => [], loading: false })

const containerRef = ref<HTMLDivElement | null>(null)
let chart: echarts.ECharts | null = null
let ro: ResizeObserver | null = null

const DOMINANT_COLOR: Record<DominantIssue, string> = {
  normal: '#67c23a',
  prefix_pollution: '#e6a23c',
  toothpaste: '#409eff',
  multi_task_session: '#8b5cf6',
  output_runaway: '#f56c6c',
  over_model: '#13c2c2',
}
const DOMINANT_LABEL: Record<DominantIssue, string> = {
  normal: '正常',
  prefix_pollution: '前缀污染',
  toothpaste: '挤牙膏',
  multi_task_session: '多任务',
  output_runaway: '输出失控',
  over_model: '杀鸡用牛刀',
}
const ISSUE_ORDER: DominantIssue[] = [
  'normal', 'prefix_pollution', 'toothpaste', 'multi_task_session', 'output_runaway', 'over_model',
]

// 人少(<30)按主导问题分色蜂群图；人多(≥30 且有分群)渲染聚类热力
const mode = computed<'swarm' | 'heatmap'>(() => {
  if ((props.clusters?.length ?? 0) > 0 && props.members.length >= 30) return 'heatmap'
  return 'swarm'
})

function initChart() {
  if (!containerRef.value) return
  chart?.dispose()
  chart = echarts.init(containerRef.value)
  ro = new ResizeObserver(() => chart?.resize())
  ro.observe(containerRef.value)
  setOption()
}

// 确定性伪随机，避免每次渲染点抖动
function hash(n: number): number {
  return ((n * 2654435761) >>> 0) % 1000 / 1000
}

function setOptionSwarm() {
  if (!chart) return
  const categories = ISSUE_ORDER.filter(c => props.members.some(m => (m.dominant_issue || 'normal') === c))
  const catIndex = new Map(categories.map((c, i) => [c, i]))

  // 蜂群：x 轴 = 主导问题类别，点内小幅抖动；y 轴 = 稳定伪随机展开
  const data = props.members.map((m, i) => {
    const issue = (m.dominant_issue || 'normal') as DominantIssue
    const ci = catIndex.get(issue) ?? 0
    const jx = (hash(i * 7 + ci) - 0.5) * 0.55
    const jy = hash(i * 13 + ci)
    return {
      value: [ci + jx, jy],
      name: m.name,
      user_id: m.user_id,
      issue,
      calls: m.calls,
    }
  })

  chart.setOption({
    tooltip: {
      trigger: 'item',
      formatter: (p: any) => {
        const d = p.data
        if (!d) return ''
        return `<b>${d.name}</b><br/>主导问题：${DOMINANT_LABEL[d.issue as DominantIssue] ?? d.issue}<br/>调用：${d.calls}`
      },
    },
    legend: {
      bottom: 0,
      data: categories,
      formatter: (name: string) => DOMINANT_LABEL[name as DominantIssue] ?? name,
    },
    grid: { left: 36, right: 24, top: 16, bottom: 44 },
    xAxis: {
      type: 'category',
      data: categories.map(c => DOMINANT_LABEL[c as DominantIssue] ?? c),
      axisLabel: { interval: 0 },
    },
    yAxis: {
      type: 'value',
      show: false,
      max: 1,
    },
    series: [
      {
        type: 'scatter',
        data,
        symbolSize: (_val: number[], d: any) => 6 + Math.min(12, Math.sqrt(d?.calls ?? 0)),
        itemStyle: {
          color: (p: any) => DOMINANT_COLOR[(p?.data?.issue ?? 'normal') as DominantIssue],
          borderColor: 'rgba(255,255,255,0.9)',
          borderWidth: 1,
          opacity: 0.9,
        },
        emphasis: { scale: 1.2 },
      },
    ],
  } as any)
}

function setOptionHeatmap() {
  if (!chart) return
  // 矩阵：行 = 分群，列 = 主导问题，值 = 簇内该主导问题的成员数
  const userDominant = new Map<string, DominantIssue>(
    props.members.map(m => [m.user_id, (m.dominant_issue || 'normal') as DominantIssue]),
  )
  const cats = ISSUE_ORDER.filter(c => props.members.some(m => (m.dominant_issue || 'normal') === c))
  const clusters = props.clusters ?? []
  const rows = clusters.map((c, idx) => ({ label: c.label || `簇${idx + 1}`, idx }))

  const maxCount = computed(() => {
    let m = 0
    for (const c of clusters) {
      for (const cat of cats) {
        const n = c.member_ids.filter(id => userDominant.get(id) === cat).length
        if (n > m) m = n
      }
    }
    return Math.max(m, 1)
  }).value

  const heatData: [number, number, number][] = []
  for (const r of rows) {
    for (const [ci, cat] of cats.entries()) {
      const n = clusters[r.idx].member_ids.filter(id => userDominant.get(id) === cat).length
      if (n > 0) heatData.push([ci, r.idx, n])
    }
  }

  chart.setOption({
    tooltip: {
      position: 'top',
      formatter: (p: any) => {
        const [ci, ri, val] = p.value as [number, number, number]
        return `${DOMINANT_LABEL[cats[ci] as DominantIssue] ?? cats[ci]} · ${rows[ri]?.label}: ${val} 人`
      },
    },
    grid: { left: 90, right: 24, top: 16, bottom: 44 },
    xAxis: {
      type: 'category',
      name: '主导问题',
      data: cats.map(c => DOMINANT_LABEL[c as DominantIssue] ?? c),
      splitArea: { show: true },
    },
    yAxis: {
      type: 'category',
      name: '分群',
      data: rows.map(r => r.label),
      splitArea: { show: true },
    },
    visualMap: {
      min: 0,
      max: maxCount,
      calculable: true,
      orient: 'horizontal',
      left: 'center',
      bottom: 0,
      inRange: { color: ['#ffffff', '#67c23a', '#1F8A3B'] },
    },
    series: [
      {
        type: 'heatmap',
        data: heatData,
        label: { show: true, formatter: (p: any) => (p.value?.[2] ?? 0) > 0 ? String(p.value[2]) : '' },
        itemStyle: { borderColor: 'rgba(0,0,0,0.1)', borderWidth: 1 },
        emphasis: { itemStyle: { shadowBlur: 8, shadowColor: 'rgba(0,0,0,0.3)' } },
      },
    ],
  } as any)
}

function setOption() {
  if (!chart) return
  if (props.members.length === 0) {
    chart.clear()
    return
  }
  if (mode.value === 'heatmap') setOptionHeatmap()
  else setOptionSwarm()
}

watch(
  () => [props.members, props.clusters, props.loading],
  async () => { await nextTick(); setOption() },
  { deep: true },
)

onMounted(async () => {
  await nextTick()
  initChart()
})
onUnmounted(() => {
  ro?.disconnect()
  chart?.dispose()
  chart = null
})
</script>

<style scoped>
.mcc {
  position: relative;
  height: 340px;
  width: 100%;
}
.mcc__empty {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--el-text-color-placeholder); font-size: 13px;
}
</style>
