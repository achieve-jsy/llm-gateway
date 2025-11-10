<template>
  <div class="health-timeline">
    <div class="timeline-grid">
      <el-tooltip
        v-for="(segment, index) in segments"
        :key="index"
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
  return buckets.map((cell, i) => {
    const ts = new Date(start + i * perGrid)
    if (cell.length === 0) return { colorClass: 'cell-empty', tooltip: `${fmt(ts)}\n暂无检测数据` }
    if (cell.length === 1) {
      const e = cell[0]
      return {
        colorClass: e.success ? 'cell-ok' : 'cell-err',
        tooltip: `${fmt(new Date(e.checked_at))}\n${e.success ? '成功' : '失败'} • ${e.response_time_ms}ms${e.error_message ? ' • ' + e.error_message : ''}`
      }
    }
    const ok = cell.filter(e => e.success).length
    const fail = cell.length - ok
    const icon = fail > ok ? 'cell-err' : ok > fail ? 'cell-ok' : 'cell-warn'
    return { colorClass: icon, tooltip: `${fmt(new Date(cell[0].checked_at))}\n共${cell.length}次\n成功:${ok} 失败:${fail}` }
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
.cell-ok    { background: #27AE60; }
.cell-warn  { background: #EAB308; }
.cell-err   { background: #E74C3C; }
.cell-empty { background: #E5E7EB; }
.timeline-labels { display: flex; justify-content: space-between; margin-top: 4px; font-size: 10px; color: #94A3B8; }
</style>