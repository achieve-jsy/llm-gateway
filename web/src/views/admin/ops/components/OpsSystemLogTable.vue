<template>
  <div class="ops-card">
    <div class="ops-card-header">
      <h3 class="ops-card-title">系统日志</h3>
    </div>

    <!-- Sink Health -->
    <div class="ops-sink-bar" v-if="sinkHealth">
      <span class="ops-sink-chip">队列 {{ sinkHealth.queue_depth }}/{{ sinkHealth.queue_capacity }}</span>
      <span class="ops-sink-chip" :class="{ 'ops-sink-chip--warn': sinkHealth.dropped > 0 }">丢弃 {{ sinkHealth.dropped }}</span>
      <span class="ops-sink-chip" :class="{ 'ops-sink-chip--danger': sinkHealth.write_failures > 0 }">写入失败 {{ sinkHealth.write_failures }}</span>
      <span class="ops-sink-chip">延迟 {{ sinkHealth.avg_write_delay_ms.toFixed(2) }}ms</span>
      <el-tag size="small" :type="sinkHealth.status === 'healthy' ? 'success' : 'warning'">{{ sinkHealth.status }}</el-tag>
    </div>

    <!-- Runtime Log Config -->
    <div class="ops-config-section">
      <details class="ops-details">
        <summary class="ops-details-summary">运行时配置</summary>
        <div class="ops-config-grid">
          <div class="ops-config-item">
            <span class="ops-config-label">日志级别</span>
            <el-select v-model="logConfig.level" size="small" class="ops-config-select">
              <el-option label="DEBUG" value="debug" />
              <el-option label="INFO" value="info" />
              <el-option label="WARN" value="warn" />
              <el-option label="ERROR" value="error" />
            </el-select>
          </div>
          <div class="ops-config-item">
            <span class="ops-config-label">保留天数</span>
            <el-input-number v-model="logConfig.retention_days" :min="1" :max="365" size="small" />
          </div>
          <div class="ops-config-item">
            <span class="ops-config-label">Caller</span>
            <el-switch v-model="logConfig.enable_caller" size="small" />
          </div>
          <div class="ops-config-item">
            <span class="ops-config-label">Stacktrace</span>
            <el-switch v-model="logConfig.enable_stacktrace" size="small" />
          </div>
        </div>
        <div class="ops-config-actions">
          <el-button size="small" type="primary" @click="saveLogConfig" :loading="configSaving">保存配置</el-button>
          <el-button size="small" @click="resetLogConfig" :loading="configSaving">恢复默认</el-button>
          <el-button size="small" type="danger" @click="cleanupLogs">清理旧日志</el-button>
        </div>
      </details>
    </div>

    <!-- Filter Bar -->
    <div class="ops-filter-bar">
      <el-select v-model="filters.timeRange" size="small" class="ops-filter-select">
        <el-option label="最近 30 分钟" value="30m" />
        <el-option label="最近 1 小时" value="1h" />
        <el-option label="最近 3 小时" value="3h" />
        <el-option label="最近 6 小时" value="6h" />
        <el-option label="最近 24 小时" value="24h" />
      </el-select>
      <el-select v-model="filters.level" size="small" class="ops-filter-select" clearable placeholder="级别">
        <el-option label="全部" value="" />
        <el-option label="DEBUG" value="DEBUG" />
        <el-option label="INFO" value="INFO" />
        <el-option label="WARN" value="WARN" />
        <el-option label="ERROR" value="ERROR" />
      </el-select>
      <el-select v-model="filters.component" size="small" class="ops-filter-select" clearable placeholder="组件" allow-create filterable>
        <el-option label="gateway" value="gateway" />
        <el-option label="scheduler" value="scheduler" />
        <el-option label="auth" value="auth" />
        <el-option label="provider" value="provider" />
        <el-option label="control-plane" value="control-plane" />
      </el-select>
      <el-input v-model="filters.request_id" size="small" class="ops-filter-input" clearable placeholder="request_id" />
      <el-input v-model="filters.search" size="small" class="ops-filter-input" clearable placeholder="搜索" :icon="SearchIcon" @keyup.enter="fetchData(1)" @input="onSearchInput" />
    </div>

    <!-- Log Table -->
    <div v-loading="loading" class="ops-logs-body">
      <table class="ops-table ops-table--compact" v-if="logs.length > 0">
        <thead>
          <tr>
            <th>时间</th>
            <th>级别</th>
            <th>组件</th>
            <th>消息</th>
            <th>渠道</th>
            <th>模型</th>
            <th>耗时</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="log in logs" :key="log.id">
            <tr class="ops-table-row" @click="toggleExpand(log.id)">
              <td class="ops-td-time">{{ formatTime(log.ts) }}</td>
              <td>
                <el-tag size="small" :type="logLevelToTagType(log.level)" effect="dark" disable-transitions>
                  {{ log.level }}
                </el-tag>
              </td>
              <td>{{ log.component }}</td>
              <td class="ops-td-msg">{{ truncate(log.message, 80) }}</td>
              <td>{{ log.provider || '-' }}</td>
              <td>{{ log.model || '-' }}</td>
              <td>{{ log.duration_ms != null ? log.duration_ms + 'ms' : '-' }}</td>
            </tr>
            <tr v-if="expandedId === log.id" class="ops-expand-row">
              <td colspan="7">
                <div class="ops-expand-content">
                  <div><strong>完整消息:</strong></div>
                  <pre class="ops-pre">{{ log.message }}</pre>
                  <div v-if="log.extra" style="margin-top: 8px;"><strong>Extra:</strong></div>
                  <pre v-if="log.extra" class="ops-pre">{{ JSON.stringify(log.extra, null, 2) }}</pre>
                  <div v-if="log.error_message" style="margin-top: 8px;"><strong>错误:</strong> {{ log.error_message }}</div>
                  <div class="ops-expand-meta">
                    <span v-if="log.request_id">request_id: {{ log.request_id }}</span>
                    <span v-if="log.client_request_id">client_request_id: {{ log.client_request_id }}</span>
                    <span v-if="log.user_id">user_id: {{ log.user_id }}</span>
                    <span v-if="log.group">group: {{ log.group }}</span>
                  </div>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
      <div v-else class="ops-card-empty">
        {{ loading ? '加载中...' : '暂无日志' }}
      </div>
    </div>

    <!-- Pagination -->
    <div class="ops-pagination" v-if="total > 0">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        small
        @current-change="fetchData"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, onMounted, onUnmounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Search as SearchIcon } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'
import type { OpsSystemLog, OpsSystemLogSinkHealth, OpsRuntimeLogConfig } from '@/api/ops'
import { logLevelToTagType } from '../utils/opsFormatters'

const props = withDefaults(
  defineProps<{
    refreshToken?: number
  }>(),
  { refreshToken: 0 }
)

const loading = ref(false)
const logs = ref<OpsSystemLog[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 50
const expandedId = ref<number | null>(null)
const sinkHealth = ref<OpsSystemLogSinkHealth | null>(null)
const configSaving = ref(false)

const logConfig = ref<OpsRuntimeLogConfig>({
  level: 'info',
  sampling_initial: 5,
  sampling_thereafter: 200,
  enable_caller: true,
  enable_stacktrace: false,
  stacktrace_level: 'error',
  retention_days: 30,
})

const filters = reactive({
  timeRange: '1h',
  level: '',
  component: '',
  request_id: '',
  search: '',
})

let searchDebounce: ReturnType<typeof setTimeout> | null = null

function onSearchInput() {
  if (searchDebounce) clearTimeout(searchDebounce)
  searchDebounce = setTimeout(() => {
    page.value = 1
    fetchData()
  }, 300)
}

onUnmounted(() => {
  if (searchDebounce) clearTimeout(searchDebounce)
})

// 非搜索字段变化时立即触发（搜索字段由 debounce 单独处理）。
watch([() => filters.timeRange, () => filters.level, () => filters.component, () => filters.request_id], () => {
  page.value = 1
  fetchData()
})

watch(() => props.refreshToken, () => {
  page.value = 1
  fetchData()
  fetchSinkHealth()
})

onMounted(async () => {
  await Promise.all([fetchData(), fetchSinkHealth(), fetchLogConfig()])
})

function formatTime(ts: string): string {
  const d = new Date(ts)
  const h = d.getHours().toString().padStart(2, '0')
  const m = d.getMinutes().toString().padStart(2, '0')
  const s = d.getSeconds().toString().padStart(2, '0')
  return `${h}:${m}:${s}`
}

function truncate(s: string, n: number): string {
  if (!s) return ''
  return s.length > n ? s.slice(0, n) + '...' : s
}

function toggleExpand(id: number) {
  expandedId.value = expandedId.value === id ? null : id
}

async function fetchData(p?: number) {
  if (p != null) page.value = p
  loading.value = true
  try {
    const result = await opsAPI.listSystemLogs({
      time_range: filters.timeRange,
      level: filters.level || undefined,
      component: filters.component || undefined,
      request_id: filters.request_id || undefined,
      search: filters.search || undefined,
      page: page.value,
      page_size: pageSize,
    })
    logs.value = result.items ?? []
    total.value = result.total ?? 0
  } catch (err: any) {
    // 静默降级：后端 500 时（如 system_logs 表未建立）展示空列表，不阻塞页面渲染
    console.warn('[OpsSystemLogTable] 加载日志失败（已降级为空列表）:', err?.message || err)
    logs.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function fetchSinkHealth() {
  try {
    sinkHealth.value = await opsAPI.getSystemLogSinkHealth()
  } catch {
    // silent
  }
}

async function fetchLogConfig() {
  try {
    logConfig.value = await opsAPI.getRuntimeLogConfig()
  } catch {
    // silent
  }
}

async function saveLogConfig() {
  configSaving.value = true
  try {
    const updated = await opsAPI.updateRuntimeLogConfig(logConfig.value)
    logConfig.value = updated
    ElMessage.success('配置已保存')
  } catch {
    ElMessage.error('保存失败')
  } finally {
    configSaving.value = false
  }
}

async function resetLogConfig() {
  configSaving.value = true
  try {
    logConfig.value = await opsAPI.resetRuntimeLogConfig()
    ElMessage.success('已恢复默认配置')
  } catch {
    ElMessage.error('重置失败')
  } finally {
    configSaving.value = false
  }
}

async function cleanupLogs() {
  try {
    await ElMessageBox.confirm(
      `将清理 ${logConfig.value.retention_days} 天前的日志，此操作不可逆。确认清理？`,
      '清理旧日志',
      { type: 'warning', confirmButtonText: '清理', cancelButtonText: '取消' }
    )
    const result = await opsAPI.cleanupSystemLogs(logConfig.value.retention_days)
    ElMessage.success(`已清理 ${result.deleted} 条旧日志`)
  } catch {
    // cancelled
  }
}
</script>

<style scoped>



.ops-sink-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: var(--space-3);
  flex-wrap: wrap;
}

.ops-sink-chip {
  font-size: var(--font-size-xxs);
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--bg-subtle);
  color: var(--text-secondary);
}

.ops-sink-chip--warn {
  background: rgba(245, 158, 11, 0.12);
  color: #f59e0b;
}

.ops-sink-chip--danger {
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
}

.ops-config-section {
  margin-bottom: var(--space-3);
}

.ops-details {
  border: 1px solid var(--border-light);
  border-radius: var(--radius-base);
}

.ops-details-summary {
  cursor: pointer;
  padding: 8px 12px;
  font-size: var(--font-size-xs);
  font-weight: 600;
  color: var(--text-secondary);
  background: var(--bg-subtle);
  border-radius: var(--radius-base);
}

.ops-config-grid {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  padding: 12px;
}

.ops-config-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 120px;
}

.ops-config-label {
  font-size: var(--font-size-xxs);
  color: var(--text-placeholder);
}

.ops-config-select {
  width: 130px;
}

.ops-config-actions {
  display: flex;
  gap: 8px;
  padding: 0 12px 12px;
}


.ops-filter-select {
  width: 130px;
}

.ops-filter-input {
  width: 160px;
}

.ops-logs-body {
  overflow-x: auto;
}





.ops-table-row:hover {
  background: var(--bg-subtle);
}


.ops-td-msg {
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-family-mono);
  font-size: var(--font-size-xxs);
}

.ops-expand-row td {
  padding: 12px;
  background: var(--bg-subtle);
}

.ops-expand-content {
  font-size: var(--font-size-xs);
}

.ops-expand-meta {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  margin-top: 8px;
  font-size: var(--font-size-xxs);
  color: var(--text-secondary);
}

.ops-pre {
  background: var(--bg-card);
  border: 1px solid var(--border-light);
  border-radius: var(--radius-base);
  padding: 8px 12px;
  font-size: var(--font-size-xxs);
  color: var(--text-secondary);
  overflow-x: auto;
  margin: 4px 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: var(--font-family-mono);
}


</style>