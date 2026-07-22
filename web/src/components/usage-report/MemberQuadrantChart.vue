<template>
  <div ref="containerRef" class="mqc" v-loading="loading">
    <div v-if="!loading && !members.length" class="mqc__empty">暂无成员数据</div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue'
import * as echarts from 'echarts'
import type { MemberPoint, DominantIssue } from '@/types/usage-report'

const props = withDefaults(defineProps<{
  members: MemberPoint[]
  costMedian: number
  issueRatioMedian: number
  loading?: boolean
}>(), { loading: false })

const emit = defineEmits<{
  select: [payload: { user_id: string; name: string; quadrant: string }]
}>()

const containerRef = ref<HTMLDivElement | null>(null)
let chart: echarts.ECharts | null = null
let ro: ResizeObserver | null = null

// 主导问题 → 颜色（与后端 dominant_issue 枚举对齐）
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

function initChart() {
  if (!containerRef.value) return
  chart?.dispose()
  chart = echarts.init(containerRef.value)
  ro = new ResizeObserver(() => chart?.resize())
  ro.observe(containerRef.value)
  chart.on('click', (params: any) => {
    const d = params?.data
    if (d?.user_id) {
      emit('select', { user_id: d.user_id, name: d.name, quadrant: d.quadrant })
    }
  })
  setOption()
}

function setOption() {
  if (!chart) return
  const ratioMedian = props.issueRatioMedian || 0

  // log 轴要求 x>0：cost_multiple<=0 时沉到 0.001（成本极低成员）
  const data = props.members.map(m => ({
    value: [m.cost_multiple > 0 ? m.cost_multiple : 0.001, m.issue_cost_ratio],
    user_id: m.user_id,
    name: m.name,
    quadrant: m.quadrant,
    dominant_issue: m.dominant_issue || 'normal',
    cost_multiple: m.cost_multiple,
    issue_cost_ratio: m.issue_cost_ratio,
    safety_anomaly: m.safety_anomaly,
    suspected_only: m.suspected_only,
    calls: m.calls,
  }))

  const categories = Object.keys(DOMINANT_COLOR) as DominantIssue[]

  chart.setOption({
    tooltip: {
      trigger: 'item',
      formatter: (p: any) => {
        const d = p.data
        if (!d) return ''
        return [
          `<b>${d.name}</b>`,
          `象限：${quadrantLabel(d.quadrant)}`,
          `成本倍数：${(d.cost_multiple ?? 0).toFixed(2)}`,
          `问题占比：${(d.issue_cost_ratio * 100).toFixed(1)}%`,
          `主导：${DOMINANT_LABEL[d.dominant_issue as DominantIssue] ?? d.dominant_issue}`,
          d.safety_anomaly ? '<span style="color:#f56c6c">严重异常（待核查）</span>' : '',
          d.suspected_only ? '<span style="color:#e6a23c">疑似</span>' : '',
        ].filter(Boolean).join('<br/>')
      },
    },
    legend: {
      bottom: 0,
      data: categories.filter(c => data.some(d => d.dominant_issue === c)),
      formatter: (name: string) => DOMINANT_LABEL[name as DominantIssue] ?? name,
    },
    grid: { left: 50, right: 24, top: 24, bottom: 40 },
    xAxis: {
      type: 'log',
      name: '成本倍数 (vs 中位)',
      nameLocation: 'middle',
      nameGap: 26,
      // log 轴不支持 0；用 0.1 起确保常见值都落在可视域
      axisLabel: { formatter: (v: number) => v >= 1 ? v.toFixed(0) : v.toFixed(2) },
      splitLine: { lineStyle: { color: 'rgba(0,0,0,0.04)' } },
    },
    yAxis: {
      type: 'linear',
      min: 0,
      max: 1,
      name: '问题占比',
      nameLocation: 'middle',
      nameGap: 36,
      axisLabel: { formatter: (v: number) => `${(v * 100).toFixed(0)}%` },
      splitLine: { lineStyle: { color: 'rgba(0,0,0,0.04)' } },
    },
    series: [
      {
        type: 'scatter',
        symbolSize: (_val: number[], p: any) => {
          const calls = p?.data?.calls ?? 0
          return 6 + Math.min(12, Math.sqrt(calls))
        },
        data,
        itemStyle: {
          color: (p: any) => DOMINANT_COLOR[(p?.data?.dominant_issue ?? 'normal') as DominantIssue],
          borderColor: (p: any) => {
            if (p?.data?.safety_anomaly) return '#f56c6c'
            if (p?.data?.suspected_only) return '#e6a23c'
            return 'rgba(255,255,255,0.9)'
          },
          borderWidth: (p: any) => (p?.data?.safety_anomaly ? 2.5 : (p?.data?.suspected_only ? 1.5 : 1)),
          borderType: (p: any) => (p?.data?.suspected_only ? 'dashed' : 'solid'),
          opacity: 0.9,
        },
        markLine: {
          silent: false,
          symbol: 'none',
          lineStyle: { color: '#909399', type: 'dashed', width: 1 },
          label: {
            show: true,
            formatter: (p: any) => p.data?.xAxis != null ? '中位成本' : '中位问题占比',
            position: (p: any) => (p.data?.xAxis != null ? 'end' : 'start'),
            fontSize: 11,
            color: '#909399',
          },
          data: [
            { xAxis: 1 },
            { yAxis: ratioMedian },
          ],
        },
        emphasis: { scale: 1.3 },
      },
    ],
  } as any)
}

function quadrantLabel(q: string): string {
  return ({
    priority_improve: '右上·优先改善',
    coach: '左上·习惯辅导',
    high_cost_normal: '右下·高成本正常',
    routine: '左下·常规',
  } as Record<string, string>)[q] ?? q
}

watch(
  () => [props.members, props.costMedian, props.issueRatioMedian, props.loading],
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
.mqc {
  position: relative;
  height: 340px;
  width: 100%;
}
.mqc__empty {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--el-text-color-placeholder); font-size: 13px;
}
</style>
