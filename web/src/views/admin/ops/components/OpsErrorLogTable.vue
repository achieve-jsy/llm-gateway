<template>
  <div class="ops-card" v-loading="loading">
    <div class="ops-card-header">
      <h3 class="ops-card-title">错误日志</h3>
      <el-button
        size="small"
        :icon="Refresh"
        :loading="loading"
        circle
        @click="fetchData()"
        title="刷新"
        class="ops-refresh-btn"
      />
    </div>

    <!-- Filter Bar -->
    <div class="ops-filter-bar">
      <el-select v-model="filters.timeRange" size="small" class="ops-filter-select" @change="fetchData(1)">
        <el-option label="最近 30 分钟" value="30m" />
        <el-option label="最近 1 小时" value="1h" />
        <el-option label="最近 3 小时" value="3h" />
        <el-option label="最近 6 小时" value="6h" />
        <el-option label="最近 24 小时" value="24h" />
        <el-option label="最近 7 天" value="7d" />
      </el-select>
      <el-select v-model="filters.phase" size="small" class="ops-filter-select" clearable placeholder="阶段" @change="fetchData(1)">
        <el-option label="全部" value="" />
        <el-option label="认证" value="auth" />
        <el-option label="路由" value="routing" />
        <el-option label="上游" value="upstream" />
        <el-option label="响应" value="response" />
      </el-select>
      <el-select v-model="filters.status" size="small" class="ops-filter-select" clearable placeholder="状态码" @change="fetchData(1)">
        <el-option label="全部" value="" />
        <el-option label="400" :value="400" />
        <el-option label="401" :value="401" />
        <el-option label="403" :value="403" />
        <el-option label="429" :value="429" />
        <el-option label="500" :value="500" />
        <el-option label="502" :value="502" />
        <el-option label="503" :value="503" />
      </el-select>
      <el-input
        v-model="filters.search"
        size="small"
        class="ops-filter-input"
        clearable
        placeholder="搜索 model / provider / request_id"
        @keyup.enter="fetchData(1)"
      />
    </div>

    <!-- Table -->
    <div v-loading="loading" class="error-log-body">
      <table class="ops-table ops-table--compact" v-if="items.length > 0">
        <thead>
          <tr>
            <th @click="toggleSort('ts')" class="ops-th-sortable">
              时间 {{ sortIcon('ts') }}
            </th>
            <th>类型</th>
            <th>端点</th>
            <th>Provider</th>
            <th>模型</th>
            <th>模型组</th>
            <th>用户</th>
            <th>Key</th>
            <th @click="toggleSort('status')" class="ops-th-sortable">
              状态码 {{ sortIcon('status') }}
            </th>
            <th>错误信息</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="log in items"
            :key="log.id"
            class="ops-table-row"
            :class="{ 'row--resolved': log.resolved }"
          >
            <td class="ops-td-time">
              <el-tooltip :content="log.request_id || ''" placement="top" :show-after="500">
                <span>{{ formatTime(log.ts) }}</span>
              </el-tooltip>
            </td>
            <td>
              <el-tag size="small" :type="phaseTagType(log.phase)" effect="plain">
                {{ phaseLabel(log.phase) }}
              </el-tag>
            </td>
            <td class="ops-td-mono">
              <span class="truncate-text" :title="log.inbound_endpoint">{{ log.inbound_endpoint || '-' }}</span>
            </td>
            <td>{{ log.provider || '-' }}</td>
            <td class="ops-td-mono">{{ log.model || '-' }}</td>
            <td>{{ log.group_name || '-' }}</td>
            <td class="ops-td-mono">
              <span class="truncate-text" :title="log.user_id">{{ log.user_id ? log.user_id.slice(0, 8) + '...' : '-' }}</span>
            </td>
            <td class="ops-td-mono">
              <span v-if="log.api_key_prefix">{{ log.api_key_prefix }}...</span>
              <span v-else>-</span>
            </td>
            <td>
              <el-tag
                size="small"
                :type="statusTagType(log.status)"
                effect="dark"
              >
                {{ log.status || '-' }}
              </el-tag>
            </td>
            <td class="ops-td-msg">
              <el-tooltip v-if="log.error_message" :content="log.error_message" placement="top" :show-after="400">
                <span>{{ truncate(log.error_message, 60) }}</span>
              </el-tooltip>
              <span v-else>-</span>
            </td>
            <td>
              <el-button
                size="small"
                :type="log.resolved ? 'info' : 'warning'"
                link
                @click.stop="toggleResolved(log)"
              >
                {{ log.resolved ? '取消' : '标记解决' }}
              </el-button>
            </td>
          </tr>
        </tbody>
      </table>

      <div v-else-if="!loading" class="ops-card-empty">暂无错误日志</div>
    </div>

    <!-- Pagination -->
    <div class="ops-pagination" v-if="total > filters.pageSize">
      <el-pagination
        v-model:current-page="currentPage"
        :page-size="filters.pageSize"
        :total="total"
        layout="total, prev, pager, next"
        small
        @current-change="fetchData"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
import { opsAPI } from '@/api/ops'
import type { OpsErrorLogEntry } from '@/api/ops'
import { formatDateTime, truncateMessage } from '../utils/opsFormatters'

const props = withDefaults(
  defineProps<{
    refreshToken?: number
  }>(),
  { refreshToken: 0 }
)

const loading = ref(false)
const items = ref<OpsErrorLogEntry[]>([])
const total = ref(0)
const currentPage = ref(1)

const filters = reactive({
  timeRange: '1h',
  phase: '',
  status: '' as string | number,
  search: '',
  pageSize: 20
})

const sortBy = ref('ts')
const sortOrder = ref<'asc' | 'desc'>('desc')

function formatTime(ts: string): string {
  return formatDateTime(ts)
}

function truncate(msg: string, len: number): string {
  return truncateMessage(msg, len)
}

function phaseLabel(phase: string): string {
  const map: Record<string, string> = {
    auth: '认证',
    routing: '路由',
    upstream: '上游',
    response: '响应'
  }
  return map[phase] || phase
}

function phaseTagType(phase: string): 'danger' | 'warning' | '' | 'info' {
  switch (phase) {
    case 'auth': return 'warning'
    case 'routing': return 'info'
    case 'upstream': return 'danger'
    case 'response': return ''
    default: return 'info'
  }
}

function statusTagType(status: number | null): 'danger' | 'warning' | 'info' | 'success' | '' {
  if (status == null) return 'info'
  if (status >= 500) return 'danger'
  if (status === 429) return 'warning'
  if (status >= 400) return 'warning'
  return 'success'
}

function sortIcon(field: string): string {
  if (sortBy.value !== field) return ''
  return sortOrder.value === 'asc' ? '↑' : '↓'
}

function toggleSort(field: string) {
  if (sortBy.value === field) {
    sortOrder.value = sortOrder.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortBy.value = field
    sortOrder.value = 'desc'
  }
  fetchData(1)
}

async function fetchData(page?: number) {
  if (page) currentPage.value = page
  loading.value = true
  try {
    const res = await opsAPI.listErrorLogs({
      time_range: filters.timeRange,
      page: currentPage.value,
      page_size: filters.pageSize,
      phase: filters.phase || undefined,
      status: filters.status ? Number(filters.status) : undefined,
      search: filters.search || undefined,
      sort_by: sortBy.value,
      sort_order: sortOrder.value
    })
    items.value = res.items || []
    total.value = res.total
  } catch (err: any) {
    console.error('[ErrorLogTable] 加载失败', err)
    ElMessage.error('加载错误日志失败')
  } finally {
    loading.value = false
  }
}

async function toggleResolved(log: OpsErrorLogEntry) {
  try {
    await opsAPI.resolveErrorLog(log.id, !log.resolved)
    log.resolved = !log.resolved
    ElMessage.success(log.resolved ? '已标记为已解决' : '已取消标记')
  } catch (err: any) {
    console.error('[ErrorLogTable] 更新失败', err)
    ElMessage.error('更新状态失败')
  }
}

onMounted(() => fetchData(1))

watch(() => props.refreshToken, () => {
  currentPage.value = 1
  fetchData()
})
</script>

<style scoped>
.ops-card {
  background: var(--bg-card);
  border: 1px solid var(--border-default);
  border-radius: var(--radius-3xl);
  padding: var(--space-4);
}

.ops-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--space-4);
}

.ops-card-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-bold);
  color: var(--text-primary);
  margin: 0;
}

.ops-refresh-btn {
  width: 28px;
  height: 28px;
}

.ops-filter-bar {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
}

.ops-filter-select {
  width: 140px;
}

.ops-filter-input {
  width: 220px;
}

.error-log-body {
  overflow-x: auto;
}

.ops-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-xs);
}

.ops-table thead th {
  text-align: left;
  padding: 6px 8px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-secondary);
  border-bottom: 1px solid var(--border-default);
  white-space: nowrap;
}

.ops-th-sortable {
  cursor: pointer;
  user-select: none;
}

.ops-th-sortable:hover {
  color: var(--text-primary);
}

.ops-table tbody td {
  padding: 6px 8px;
  border-bottom: 1px solid var(--border-light);
  vertical-align: middle;
}

.ops-table-row:hover {
  background: var(--bg-subtle);
}

.row--resolved {
  opacity: 0.55;
}

.ops-td-time {
  white-space: nowrap;
  font-family: var(--font-family-mono);
  color: var(--text-secondary);
}

.ops-td-mono {
  font-family: var(--font-family-mono);
  font-size: 11px;
}

.ops-td-msg {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.truncate-text {
  display: inline-block;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
}

.ops-card-empty {
  padding: var(--space-8) 0;
  text-align: center;
  color: var(--text-placeholder);
  font-size: var(--font-size-sm);
}

.ops-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: var(--space-3);
}
</style>
