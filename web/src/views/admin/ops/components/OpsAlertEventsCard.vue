<template>
  <div class="ops-card" v-loading="loading">
    <div class="ops-card-header">
      <h3 class="ops-card-title">告警事件</h3>
      <el-button size="small" :icon="RefreshIcon" circle @click="fetchData" :loading="loading" />
    </div>

    <!-- Filter Bar -->
    <div class="ops-filter-bar">
      <el-select v-model="filters.timeRange" size="small" class="ops-filter-select">
        <el-option label="最近 1 小时" value="1h" />
        <el-option label="最近 3 小时" value="3h" />
        <el-option label="最近 6 小时" value="6h" />
        <el-option label="最近 24 小时" value="24h" />
        <el-option label="最近 7 天" value="7d" />
      </el-select>
      <el-select v-model="filters.severity" size="small" class="ops-filter-select" clearable placeholder="严重程度">
        <el-option label="全部" value="" />
        <el-option label="P0 - 紧急" value="P0" />
        <el-option label="P1 - 严重" value="P1" />
        <el-option label="P2 - 警告" value="P2" />
        <el-option label="P3 - 提示" value="P3" />
      </el-select>
      <el-select v-model="filters.status" size="small" class="ops-filter-select" clearable placeholder="状态">
        <el-option label="全部" value="" />
        <el-option label="告警中" value="firing" />
        <el-option label="已恢复" value="resolved" />
        <el-option label="手动恢复" value="manual_resolved" />
      </el-select>
    </div>

    <!-- Events Table -->
    <div class="ops-events-list" v-if="events.length > 0">
      <table class="ops-table ops-table--compact">
        <thead>
          <tr>
            <th>时间</th>
            <th>严重程度</th>
            <th>规则</th>
            <th>标题</th>
            <th>持续时间</th>
            <th>状态</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="evt in events"
            :key="evt.id"
            class="ops-table-row"
            @click="openDetail(evt)"
          >
            <td class="ops-td-time">{{ formatTime(evt.fired_at) }}</td>
            <td>
              <el-tag size="small" :type="severityTagType(evt.severity)" effect="dark">
                {{ evt.severity }}
              </el-tag>
            </td>
            <td class="ops-td-rule">{{ evt.rule_name || `规则#${evt.rule_id}` }}</td>
            <td class="ops-td-title">{{ evt.title }}</td>
            <td class="ops-td-duration">{{ eventDuration(evt) }}</td>
            <td>
              <el-tag size="small" :type="statusTagType(evt.status)" effect="plain">
                {{ statusLabel(evt.status) }}
              </el-tag>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="ops-load-more" v-if="hasMore">
        <el-button size="small" @click="loadMore" :loading="loadingMore">加载更多</el-button>
      </div>
    </div>
    <div v-else class="ops-card-empty">
      {{ loading ? '加载中...' : '暂无告警事件' }}
    </div>

    <!-- Detail Dialog -->
    <el-dialog
      :model-value="!!detailEvent"
      :title="detailEvent ? '告警详情 - ' + detailEvent.title : ''"
      width="680px"
      @update:model-value="detailEvent = null"
    >
      <div v-if="detailEvent" class="ops-detail-body">
        <div class="ops-detail-grid">
          <div class="ops-detail-item">
            <span class="ops-detail-label">严重程度</span>
            <el-tag :type="severityTagType(detailEvent.severity)" effect="dark">{{ detailEvent.severity }}</el-tag>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">状态</span>
            <el-tag :type="statusTagType(detailEvent.status)" effect="plain">{{ statusLabel(detailEvent.status) }}</el-tag>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">触发时间</span>
            <span>{{ detailEvent.fired_at }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">恢复时间</span>
            <span>{{ detailEvent.resolved_at || '-' }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">当前值</span>
            <span>{{ detailEvent.metric_value }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">阈值</span>
            <span>{{ detailEvent.threshold_value }}</span>
          </div>
          <div class="ops-detail-item">
            <span class="ops-detail-label">邮件已发送</span>
            <span>{{ detailEvent.email_sent ? '是' : '否' }}</span>
          </div>
        </div>
        <div class="ops-detail-section">
          <span class="ops-detail-label">描述</span>
          <div class="ops-detail-text">{{ detailEvent.description || '无' }}</div>
        </div>
        <div class="ops-detail-section" v-if="detailEvent.dimensions && Object.keys(detailEvent.dimensions).length">
          <span class="ops-detail-label">维度</span>
          <pre class="ops-detail-pre">{{ JSON.stringify(detailEvent.dimensions, null, 2) }}</pre>
        </div>

        <div class="ops-detail-actions">
          <span class="ops-detail-label">静默</span>
          <div class="ops-silence-btns">
            <el-button size="small" @click="createSilence(1)">1 小时</el-button>
            <el-button size="small" @click="createSilence(24)">24 小时</el-button>
            <el-button size="small" @click="createSilence(168)">7 天</el-button>
          </div>
          <el-button
            v-if="detailEvent.status === 'firing'"
            type="warning"
            size="small"
            @click="manualResolve"
          >
            手动恢复
          </el-button>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh as RefreshIcon } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'
import type { AlertEvent, AlertEventStatus, OpsSeverity } from '@/api/ops'

const props = withDefaults(
  defineProps<{
    refreshToken?: number
  }>(),
  { refreshToken: 0 }
)

const loading = ref(false)
const loadingMore = ref(false)
const events = ref<AlertEvent[]>([])
const hasMore = ref(false)
const detailEvent = ref<AlertEvent | null>(null)

const filters = reactive({
  timeRange: '6h',
  severity: '',
  status: '',
})

const PAGE_LIMIT = 20

watch(filters, () => {
  events.value = []
  hasMore.value = false
  fetchData()
})

watch(() => props.refreshToken, () => {
  events.value = []
  hasMore.value = false
  fetchData()
})

onMounted(() => {
  fetchData()
})

function severityTagType(sev: OpsSeverity): 'danger' | 'warning' | 'info' {
  if (sev === 'P0' || sev === 'P1') return 'danger'
  if (sev === 'P2') return 'warning'
  return 'info'
}

function statusTagType(st: AlertEventStatus): 'danger' | 'success' | 'info' {
  if (st === 'firing') return 'danger'
  return 'success'
}

function statusLabel(st: AlertEventStatus): string {
  switch (st) {
    case 'firing': return '告警中'
    case 'resolved': return '已恢复'
    case 'manual_resolved': return '手动恢复'
    default: return st
  }
}

function formatTime(ts: string): string {
  const d = new Date(ts)
  const M = (d.getMonth() + 1).toString().padStart(2, '0')
  const D = d.getDate().toString().padStart(2, '0')
  const h = d.getHours().toString().padStart(2, '0')
  const m = d.getMinutes().toString().padStart(2, '0')
  return `${M}-${D} ${h}:${m}`
}

function eventDuration(evt: AlertEvent): string {
  const start = new Date(evt.fired_at).getTime()
  const end = evt.resolved_at ? new Date(evt.resolved_at).getTime() : Date.now()
  const mins = Math.floor((end - start) / 60000)
  if (mins < 60) return `${mins} 分钟`
  const hours = Math.floor(mins / 60)
  const rm = mins % 60
  return `${hours} 小时 ${rm} 分钟`
}

async function fetchData() {
  loading.value = true
  try {
    const result = await opsAPI.listAlertEvents({
      time_range: filters.timeRange,
      severity: filters.severity,
      status: filters.status,
      limit: PAGE_LIMIT,
    })
    events.value = result
    hasMore.value = result.length >= PAGE_LIMIT
  } catch {
    ElMessage.error('加载告警事件失败')
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  if (!events.value.length) return
  loadingMore.value = true
  const last = events.value[events.value.length - 1]
  try {
    const result = await opsAPI.listAlertEvents({
      time_range: filters.timeRange,
      severity: filters.severity,
      status: filters.status,
      limit: PAGE_LIMIT,
      before_fired_at: last.fired_at,
      before_id: last.id,
    })
    events.value.push(...result)
    hasMore.value = result.length >= PAGE_LIMIT
  } catch {
    ElMessage.error('加载更多失败')
  } finally {
    loadingMore.value = false
  }
}

function openDetail(evt: AlertEvent) {
  detailEvent.value = evt
}

async function createSilence(hours: number) {
  if (!detailEvent.value) return
  const until = new Date(Date.now() + hours * 3600000).toISOString()
  try {
    await opsAPI.createAlertSilence({
      rule_id: detailEvent.value.rule_id,
      until,
      reason: `临时静默 ${hours} 小时`,
    })
    ElMessage.success(`已静默 ${hours} 小时`)
  } catch {
    ElMessage.error('静默失败')
  }
}

async function manualResolve() {
  if (!detailEvent.value) return
  try {
    const updated = await opsAPI.updateAlertEventStatus(detailEvent.value.id, 'manual_resolved')
    detailEvent.value = { ...detailEvent.value, status: updated.status }
    const idx = events.value.findIndex(e => e.id === detailEvent.value!.id)
    if (idx >= 0) events.value[idx] = { ...events.value[idx], status: updated.status }
    ElMessage.success('已标记手动恢复')
  } catch {
    ElMessage.error('状态更新失败')
  }
}
</script>

<style scoped>




.ops-filter-select {
  width: 140px;
}

.ops-events-list {
  overflow-x: auto;
}


.ops-table th {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-light);
  color: var(--text-secondary);
  font-weight: var(--font-weight-semibold);
  white-space: nowrap;
}

.ops-table td {
  padding: 8px 10px;
  border-bottom: 1px solid var(--border-light);
  color: var(--text-primary);
}

.ops-table-row {
  cursor: pointer;
  transition: background 0.15s;
}

.ops-table-row:hover {
  background: var(--bg-subtle);
}

.ops-td-time {
  white-space: nowrap;
  color: var(--text-secondary);
}

.ops-td-rule,
.ops-td-title {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ops-td-duration {
  white-space: nowrap;
  color: var(--text-secondary);
}

.ops-load-more {
  display: flex;
  justify-content: center;
  padding: var(--space-3) 0;
}


.ops-detail-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ops-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 12px;
}

.ops-detail-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ops-detail-label {
  font-size: var(--font-size-xxs);
  color: var(--text-placeholder);
  font-weight: var(--font-weight-semibold);
}

.ops-detail-section {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.ops-detail-text {
  font-size: var(--font-size-xs);
  color: var(--text-primary);
  line-height: 1.6;
}

.ops-detail-pre {
  background: var(--bg-subtle);
  border-radius: var(--radius-base);
  padding: 8px 12px;
  font-size: 11px;
  color: var(--text-secondary);
  overflow-x: auto;
  margin: 0;
}

.ops-detail-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.ops-silence-btns {
  display: flex;
  gap: 6px;
}
</style>