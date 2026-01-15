<template>
  <div class="health-timeline">
    <div class="timeline-grid">
      <el-tooltip
        v-for="(segment, idx) in segments"
        :key="idx"
        :content="segment.tooltip"
        placement="top"
        :show-after="200"
      >
        <div class="timeline-block" :class="segment.colorClass" />
      </el-tooltip>
    </div>
    <div class="timeline-labels">
      <span>{{ earliestTime }}</span>
      <span>{{ latestTime }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

export interface TimelineEntry {
  checked_at: string
  success: boolean
  response_time_ms: number
  error_message?: string
}

const props = defineProps<{
  timeline?: TimelineEntry[]
  timeRangeStart?: string | null
  timeRangeEnd?: string | null
  lookback?: string
}>()

const GRID = 100

const segments = computed(() => {
  const events = props.timeline || []
  const hours = parseHours(props.lookback || '6h')
  if (events.length === 0) {
    return Array.from({ length: GRID }, () => ({ colorClass: 'cell-empty', tooltip: '暂无检测数据' }))
  }
  const now = Date.now()
  const start = now - hours * 3600000
  const perGrid = (hours * 3600000) / GRID
  const buckets: TimelineEntry[][] = Array.from({ length: GRID }, () => [])
  for (const e of events) {
    const i = Math.floor((new Date(e.checked_at).getTime() - start) / perGrid)
    if (i >= 0 && i < GRID) buckets[i].push(e)
  }
  let lastKnown: { colorClass: string; success: boolean } | null = null
  return buckets.map((cell, i) => {
    const ts = new Date(start + i * perGrid)
    if (cell.length === 0) {
      if (lastKnown) {
        return { colorClass: lastKnown.colorClass, tooltip: `${fmt(ts)}\n延续上次状态（无新检测）` }
      }
      return { colorClass: 'cell-empty', tooltip: `${fmt(ts)}\n暂无检测数据` }
    }
    if (cell.length === 1) {
      const e = cell[0]
      const cls = e.success ? 'cell-ok' : 'cell-err'
      lastKnown = { colorClass: cls, success: e.success }
      return {
        colorClass: cls,
        tooltip: `${fmt(new Date(e.checked_at))}\n${e.success ? '成功' : '失败'} • ${e.response_time_ms}ms${e.error_message ? ' • ' + e.error_message : ''}`
      }
    }
    const ok = cell.filter(e => e.success).length
    const fail = cell.length - ok
    const cls = fail > ok ? 'cell-err' : ok > fail ? 'cell-ok' : 'cell-warn'
    lastKnown = { colorClass: cls, success: fail === 0 }
    return { colorClass: cls, tooltip: `${fmt(new Date(cell[0].checked_at))}\n共${cell.length}次\n成功:${ok} 失败:${fail}` }
  })
})

const earliestTime = computed(() => {
  if (props.timeRangeStart) return fmt(new Date(props.timeRangeStart))
  return fmt(new Date(Date.now() - parseHours(props.lookback || '6h') * 3600000))
})
const latestTime = computed(() => {
  if (props.timeRangeEnd) return fmt(new Date(props.timeRangeEnd))
  return fmt(new Date())
})

function parseHours(lb: string): number {
  switch (lb) { case '12h': return 12; case '24h': return 24; case '7d': return 168; default: return 6 }
}
function fmt(d: Date): string {
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}
</script>

<style scoped>
.health-timeline { width: 100%; }
.timeline-grid { display: flex; gap: 1px; height: 24px; width: 100%; }
.timeline-block { flex: 1; height: 24px; border-radius: 2px; cursor: pointer; transition: all 0.15s; }
.timeline-block:hover { transform: scaleY(1.15); filter: brightness(1.1); }
.cell-ok    { background: #0EA371; }
.cell-warn  { background: #EAB308; }
.cell-err   { background: #E74C3C; }
.cell-empty { background: #E5E7EB; }
.timeline-labels { display: flex; justify-content: space-between; margin-top: 4px; font-size: 10px; color: #94A3B8; }
</style>