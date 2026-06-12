<template>
  <div class="audit-log-page">
    <div class="page-header">
      <div class="header-info">
        <h2 class="page-title">审计日志</h2>
        <p class="page-desc">记录管理面所有操作，便于合规追踪与问题排查</p>
      </div>
      <div class="header-actions">
        <el-input
          v-model="searchKeyword"
          placeholder="操作人 / 资源 / IP"
          clearable
          prefix-icon="Search"
          class="toolbar-field-lg"
          @clear="resetAndFetch"
          @keyup.enter="resetAndFetch"
        />
        <el-select
          v-model="query.action"
          placeholder="操作类型"
          clearable
          filterable
          class="toolbar-field"
          @change="resetAndFetch"
        >
          <el-option
            v-for="opt in AUDIT_ACTIONS"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
        <el-select
          v-model="query.resource_type"
          placeholder="资源类型"
          clearable
          class="toolbar-field"
          @change="resetAndFetch"
        >
          <el-option
            v-for="opt in RESOURCE_TYPES"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始"
          end-placeholder="结束"
          format="YYYY-MM-DD HH:mm"
          value-format="YYYY-MM-DDTHH:mm:ssZ"
          class="toolbar-field-lg"
          :shortcuts="dateShortcuts"
          @change="onDateRangeChange"
        />
        <el-tooltip content="刷新" placement="top">
          <el-button :icon="Refresh" circle @click="fetchData" :loading="loading" />
        </el-tooltip>
        <el-tooltip :content="autoRefresh ? '关闭自动刷新' : '30 秒自动刷新'" placement="top">
          <el-button
            :icon="Timer"
            circle
            :type="autoRefresh ? 'primary' : 'default'"
            @click="toggleAutoRefresh"
          />
        </el-tooltip>
        <el-tooltip content="导出 CSV（按筛选条件，分页流式导出）" placement="top">
          <el-button :icon="Download" circle @click="exportToCSV" :disabled="isExporting" />
        </el-tooltip>
        <el-tooltip v-if="isExporting" content="取消导出" placement="top">
          <el-button :icon="Close" circle type="danger" @click="cancelExport" />
        </el-tooltip>
      </div>
    </div>

    <!-- 导出进度条 -->
    <div v-if="isExporting" class="export-progress-bar">
      <div class="export-progress-info">
        <span class="export-title">正在导出 CSV...</span>
        <span class="export-status">{{ exportMessage }}</span>
      </div>
      <el-progress
        :percentage="exportProgress"
        :stroke-width="8"
        :format="() => `${exportProgress}%`"
        :indeterminate="exportProgress === 0"
      />
    </div>

    <el-skeleton v-if="loading && !logs.length" :rows="8" animated />

    <el-result
      v-else-if="loadError"
      icon="error"
      title="加载失败"
      :sub-title="loadError"
    >
      <template #extra>
        <el-button type="primary" class="brand-btn" @click="fetchData">重新加载</el-button>
      </template>
    </el-result>

    <template v-else>
      <!-- KPI 卡片行 -->
      <div class="kpi-row">
        <div class="kpi-card">
          <div class="kpi-label">今日操作</div>
          <div class="kpi-value">{{ formatNum(todayCount) }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">总记录数</div>
          <div class="kpi-value">{{ formatNum(total) }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">当前页</div>
          <div class="kpi-value">{{ formatNum(logs.length) }}</div>
        </div>
        <div class="kpi-card">
          <div class="kpi-label">最后刷新</div>
          <div class="kpi-value kpi-value-sm">{{ formatShortTime(lastUpdateTime) }}</div>
        </div>
      </div>

      <!-- 表格 -->
      <el-table
        :data="logs"
        v-loading="loading"
        border
        stripe
        class="audit-table"
        @sort-change="handleSortChange"
        @row-click="showDetails"
        row-key="id"
      >
        <el-table-column label="时间" prop="created_at" width="172" sortable="custom">
          <template #default="{ row }">
            <div class="time-cell">
              <div class="time-main">{{ formatDateTime(row.created_at) }}</div>
              <div class="time-relative">{{ formatRelative(row.created_at) }}</div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="操作人" prop="user_name" min-width="150">
          <template #default="{ row }">
            <div class="user-cell">
              <div class="user-avatar">{{ avatarChar(row.user_name) }}</div>
              <div class="user-info">
                <span class="user-name">{{ row.user_name || '-' }}</span>
                <span class="user-id mono-text">{{ shortId(row.user_id) }}</span>
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="操作" prop="action" min-width="180">
          <template #default="{ row }">
            <el-tag
              :type="actionTagType(row.action)"
              size="small"
              effect="light"
              class="action-tag"
            >
              {{ actionLabel(row.action) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column label="资源" min-width="220">
          <template #default="{ row }">
            <div class="resource-cell">
              <el-tag
                v-if="row.resource_type"
                type="info"
                size="small"
                effect="plain"
                class="resource-type-tag"
              >{{ resourceLabel(row.resource_type) }}</el-tag>
              <span class="resource-name" :title="row.resource_name || row.resource_id">
                {{ row.resource_name || row.resource_id || '-' }}
              </span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="部门" prop="dept_name" min-width="120" show-overflow-tooltip>
          <template #default="{ row }">
            <span>{{ row.dept_name || '-' }}</span>
          </template>
        </el-table-column>

        <el-table-column label="状态" prop="status_code" width="90" align="center">
          <template #default="{ row }">
            <span
              class="status-chip"
              :class="statusClass(row.status_code)"
            >{{ row.status_code }}</span>
          </template>
        </el-table-column>

        <el-table-column label="客户端 IP" prop="client_ip" width="140">
          <template #default="{ row }">
            <code class="mono-text">{{ row.client_ip || '-' }}</code>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="90" align="center" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link size="small" @click.stop="showDetails(row)">详情</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div v-if="total > pageSize" class="pagination-wrapper">
        <span class="page-note">
          共 {{ formatNum(total) }} 条，当前第 {{ currentPage }} 页
        </span>
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100, 200]"
          :total="total"
          layout="total, sizes, prev, pager, next, jumper"
          size="small"
          @current-change="onPageChange"
          @size-change="onSizeChange"
        />
      </div>
    </template>

    <!-- 详情抽屉 -->
    <el-drawer
      v-model="detailsVisible"
      :title="drawerTitle"
      direction="rtl"
      size="560px"
      class="audit-drawer"
    >
      <template #header>
        <div class="drawer-header">
          <div class="drawer-title-row">
            <el-tag
              :type="selectedLog ? actionTagType(selectedLog.action) : 'info'"
              effect="light"
            >{{ selectedLog ? actionLabel(selectedLog.action) : '-' }}</el-tag>
            <span class="status-chip" :class="selectedLog ? statusClass(selectedLog.status_code) : ''">
              {{ selectedLog?.status_code }}
            </span>
          </div>
          <div class="drawer-subtitle">
            {{ selectedLog?.user_name }} · {{ selectedLog ? formatDateTime(selectedLog.created_at) : '' }}
          </div>
        </div>
      </template>

      <div v-if="selectedLog" class="drawer-body">
        <section class="drawer-section">
          <div class="section-title">基本信息</div>
          <dl class="kv-grid">
            <div class="kv-item">
              <dt>用户 ID</dt>
              <dd class="mono-text">{{ selectedLog.user_id }}</dd>
            </div>
            <div class="kv-item">
              <dt>资源类型</dt>
              <dd>{{ resourceLabel(selectedLog.resource_type) }}</dd>
            </div>
            <div class="kv-item">
              <dt>资源 ID</dt>
              <dd class="mono-text kv-copyable" @click="copyText(selectedLog.resource_id)">
                {{ selectedLog.resource_id || '-' }}
                <el-icon v-if="selectedLog.resource_id"><CopyDocument /></el-icon>
              </dd>
            </div>
            <div class="kv-item">
              <dt>资源名称</dt>
              <dd>{{ selectedLog.resource_name || '-' }}</dd>
            </div>
            <div class="kv-item">
              <dt>部门</dt>
              <dd>{{ selectedLog.dept_name || '-' }}</dd>
            </div>
            <div class="kv-item">
              <dt>客户端 IP</dt>
              <dd class="mono-text">{{ selectedLog.client_ip || '-' }}</dd>
            </div>
          </dl>
        </section>

        <section v-if="selectedLog.user_agent" class="drawer-section">
          <div class="section-title">User Agent</div>
          <div class="ua-content">{{ selectedLog.user_agent }}</div>
        </section>

        <section v-if="parsedDetails && (parsedDetails.request_headers || parsedDetails.request_body || parsedDetails.response_body)" class="drawer-section">
          <div class="section-title">HTTP 详情</div>

          <div v-if="parsedDetails.request_headers" class="detail-sub-panel">
            <div class="panel-label">请求头</div>
            <dl class="kv-grid kv-grid-sm">
              <div v-for="(val, key) in parsedDetails.request_headers" :key="key" class="kv-item">
                <dt class="mono-text">{{ key }}</dt>
                <dd class="mono-text">{{ val }}</dd>
              </div>
            </dl>
          </div>

          <div v-if="parsedDetails.request_body" class="detail-sub-panel">
            <div class="panel-label">
              请求体
              <el-button type="primary" link size="small" @click="copyText(parsedDetails!.request_body!)">
                <el-icon><CopyDocument /> 复制</el-icon>
              </el-button>
            </div>
            <pre class="details-json">{{ formatJson(parsedDetails.request_body) }}</pre>
          </div>

          <div v-if="parsedDetails.response_body" class="detail-sub-panel">
            <div class="panel-label">
              响应体
              <el-tag :type="statusClass(selectedLog?.status_code || 200)" size="small">
                {{ selectedLog?.status_code || parsedDetails.status_code }}
              </el-tag>
              <el-button type="primary" link size="small" @click="copyText(parsedDetails!.response_body!)">
                <el-icon><CopyDocument /> 复制</el-icon>
              </el-button>
            </div>
            <pre class="details-json">{{ formatJson(parsedDetails.response_body) }}</pre>
          </div>
        </section>

        <section v-if="parsedDetails && parsedDetails.duration_ms != null" class="drawer-section">
          <div class="section-title">请求信息</div>
          <dl class="kv-grid">
            <div class="kv-item">
              <dt>耗时</dt>
              <dd>{{ formatDuration(parsedDetails.duration_ms!) }}</dd>
            </div>
            <div v-if="parsedDetails.request_id" class="kv-item">
              <dt>Request ID</dt>
              <dd class="mono-text kv-copyable" @click="copyText(parsedDetails!.request_id!)">
                {{ parsedDetails.request_id }}
                <el-icon><CopyDocument /></el-icon>
              </dd>
            </div>
            <div v-if="parsedDetails.referer" class="kv-item">
              <dt>来源页面</dt>
              <dd>{{ parsedDetails.referer }}</dd>
            </div>
            <div v-if="parsedDetails.content_type" class="kv-item">
              <dt>Content-Type</dt>
              <dd class="mono-text">{{ parsedDetails.content_type }}</dd>
            </div>
          </dl>
        </section>

        <section v-if="selectedLog.details" class="drawer-section">
          <div class="section-title">
            原始数据
            <el-button
              type="primary"
              link
              size="small"
              @click="copyText(formatJson(selectedLog.details))"
            >
              <el-icon><CopyDocument /> 复制</el-icon>
            </el-button>
          </div>
          <pre class="details-json details-json-raw">{{ formatJson(selectedLog.details) }}</pre>
        </section>
      </div>

      <template #footer>
        <el-button @click="detailsVisible = false">关闭</el-button>
        <el-button type="primary" class="brand-btn" @click="exportDetailToJSON">
          <el-icon><Download /></el-icon>
          导出 JSON
        </el-button>
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted, computed } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Refresh,
  Timer,
  Download,
  CopyDocument,
  Close,
} from '@element-plus/icons-vue'
import { getAuditLogs } from '@/services/audit'
import { AUDIT_ACTIONS, RESOURCE_TYPES } from '@/types/audit'
import type { AuditLog, AuditLogQuery, AuditDetail } from '@/types/audit'
import { formatDate } from '@/utils/format'

// 状态
const loading = ref(false)
const loadError = ref('')
const logs = ref<AuditLog[]>([])
const total = ref(0)
const todayCount = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)
const lastUpdateTime = ref<Date | null>(null)

const searchKeyword = ref('')
const dateRange = ref<[string, string] | null>(null)

const query = reactive<AuditLogQuery>({
  action: '',
  resource_type: '',
  from: undefined,
  to: undefined,
})

// 自动刷新
const autoRefresh = ref(false)
let autoTimer: ReturnType<typeof setInterval> | null = null

// 导出状态（流式分页）
const isExporting = ref(false)
const exportProgress = ref(0)
const exportMessage = ref('')
let exportCancelFlag = false

// 抽屉
const detailsVisible = ref(false)
const selectedLog = ref<AuditLog | null>(null)

const drawerTitle = computed(() => {
  if (!selectedLog.value) return '操作详情'
  return `${actionLabel(selectedLog.value.action)} · ${selectedLog.value.user_name || '-'}`
})

const parsedDetails = computed((): AuditDetail | null => {
  if (!selectedLog.value?.details) return null
  try {
    return JSON.parse(selectedLog.value.details) as AuditDetail
  } catch {
    return null
  }
})

// 日期快捷
const dateShortcuts = [
  {
    text: '今天',
    value: () => {
      const end = new Date()
      const start = new Date()
      start.setHours(0, 0, 0, 0)
      return [start, end]
    },
  },
  {
    text: '昨天',
    value: () => {
      const end = new Date()
      end.setHours(0, 0, 0, 0)
      const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
      return [start, end]
    },
  },
  {
    text: '近 7 天',
    value: () => {
      const end = new Date()
      const start = new Date(Date.now() - 7 * 24 * 3600 * 1000)
      return [start, end]
    },
  },
  {
    text: '近 30 天',
    value: () => {
      const end = new Date()
      const start = new Date(Date.now() - 30 * 24 * 3600 * 1000)
      return [start, end]
    },
  },
]

// 格式化
function formatNum(n: number): string {
  return n.toLocaleString('zh-CN')
}

function formatDateTime(iso: string): string {
  return formatDate(iso)
}

function formatShortTime(d: Date | null): string {
  if (!d) return '-'
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function formatRelative(iso: string): string {
  const d = new Date(iso)
  const diff = Date.now() - d.getTime()
  const sec = Math.floor(diff / 1000)
  if (sec < 60) return `${sec} 秒前`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min} 分钟前`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} 小时前`
  const day = Math.floor(hr / 24)
  if (day < 30) return `${day} 天前`
  return formatDate(iso)
}

function formatJson(str: string): string {
  try {
    return JSON.stringify(JSON.parse(str), null, 2)
  } catch {
    return str
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)} s`
  const min = Math.floor(ms / 60000)
  const sec = Math.floor((ms % 60000) / 1000)
  return `${min} 分 ${sec} 秒`
}

function avatarChar(name: string | null | undefined): string {
  if (!name) return '?'
  return name.charAt(0).toUpperCase()
}

function shortId(id: string): string {
  if (!id) return '-'
  return `${id.slice(0, 8)}…`
}

function actionLabel(action: string): string {
  const found = AUDIT_ACTIONS.find(a => a.value === action)
  return found ? found.label : action
}

function resourceLabel(type: string): string {
  const found = RESOURCE_TYPES.find(r => r.value === type)
  return found ? found.label : type || '-'
}

function actionTagType(action: string): '' | 'success' | 'warning' | 'info' | 'danger' {
  if (action.includes('create')) return 'success'
  if (action.includes('delete')) return 'danger'
  if (action.includes('update') || action.includes('toggle')) return 'warning'
  if (action.includes('login')) return ''
  return 'info'
}

function statusClass(code: number): string {
  if (code >= 200 && code < 300) return 'is-success'
  if (code >= 400 && code < 500) return 'is-warning'
  if (code >= 500) return 'is-danger'
  return 'is-info'
}

// 事件
function onDateRangeChange(val: [string, string] | null) {
  if (val) {
    query.from = val[0]
    query.to = val[1]
  } else {
    query.from = undefined
    query.to = undefined
  }
  currentPage.value = 1
  fetchData()
}

function onPageChange(page: number) {
  currentPage.value = page
  fetchData()
}

function onSizeChange(size: number) {
  pageSize.value = size
  currentPage.value = 1
  fetchData()
}

function resetAndFetch() {
  currentPage.value = 1
  fetchData()
}

function handleSortChange({ prop, order }: any) {
  if (prop === 'created_at' && order) {
    query.sort = order === 'ascending' ? 'created_at:asc' : 'created_at:desc'
  } else {
    query.sort = undefined
  }
  currentPage.value = 1
  fetchData()
}

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
  if (autoRefresh.value) {
    autoTimer = setInterval(() => fetchData(), 30000)
    ElMessage.success('已开启 30 秒自动刷新')
  } else {
    if (autoTimer) clearInterval(autoTimer)
    autoTimer = null
  }
}

async function fetchData() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await getAuditLogs({
      ...query,
      search: searchKeyword.value || undefined,
      page: currentPage.value,
      page_size: pageSize.value,
    })
    logs.value = res.items || []
    total.value = res.total || 0
    lastUpdateTime.value = new Date()
    await fetchTodayCount()
  } catch (e: any) {
    loadError.value = e?.response?.data?.error || e?.message || '获取审计日志失败'
    logs.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function fetchTodayCount() {
  try {
    const start = new Date()
    start.setHours(0, 0, 0, 0)
    const res = await getAuditLogs({
      from: start.toISOString(),
      page: 1,
      page_size: 1,
    })
    todayCount.value = res.total || 0
  } catch {
    todayCount.value = 0
  }
}

function showDetails(log: AuditLog) {
  selectedLog.value = log
  detailsVisible.value = true
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败')
  }
}

function exportToCSV() {
  if (isExporting.value) return
  if (!logs.value.length && total.value === 0) {
    ElMessage.warning('没有数据可导出')
    return
  }

  // 流式分页导出：按当前筛选条件，逐页拉取所有日志拼接
  // 导出全部字段（对应 AuditLog 模型）
  const headers = [
    '日志ID',
    '用户ID',
    '用户名',
    '操作类型',
    '资源类型',
    '资源ID',
    '资源名称',
    '部门ID',
    '部门名称',
    '状态码',
    '客户端IP',
    'User-Agent',
    '详细信息',
    '创建时间'
  ]
  const csvRows: string[] = [headers.join(',')]
  const pageSize = 200 // 每页拉取量
  let fetchedCount = 0
  let totalCount = 0
  let page = 1

  isExporting.value = true
  exportCancelFlag = false
  exportProgress.value = 0
  exportMessage.value = '准备导出...'

  const encode = (v: unknown): string => `"${String(v ?? '').replace(/"/g, '""')}"`

  const run = async () => {
    try {
      while (true) {
        if (exportCancelFlag) {
          ElMessage.info('已取消导出')
          break
        }

        const res = await getAuditLogs({
          ...query,
          search: searchKeyword.value || undefined,
          page,
          page_size: pageSize,
        })

        const items = res.items || []
        totalCount = res.total || 0

        if (items.length === 0 || fetchedCount >= totalCount) break

        for (const l of items) {
          csvRows.push([
            l.id,
            l.user_id,
            l.user_name,
            actionLabel(l.action),
            resourceLabel(l.resource_type),
            l.resource_id,
            l.resource_name || '',
            l.dept_id || '',
            l.dept_name || '',
            l.status_code,
            l.client_ip || '',
            l.user_agent || '',
            l.details || '',
            formatDateTime(l.created_at),
          ].map(encode).join(','))
        }

        fetchedCount += items.length
        const pct = totalCount > 0 ? Math.min(100, Math.floor((fetchedCount / totalCount) * 100)) : 0
        exportProgress.value = pct
        exportMessage.value = `${formatNum(fetchedCount)} / ${formatNum(totalCount)}`

        page += 1
        if (fetchedCount >= totalCount) break
      }

      if (!exportCancelFlag && csvRows.length > 1) {
        const blob = new Blob(['\ufeff' + csvRows.join('\n')], { type: 'text/csv;charset=utf-8;' })
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = `audit-logs-${new Date().toISOString().slice(0, 10)}.csv`
        a.click()
        URL.revokeObjectURL(url)
        ElMessage.success(`导出成功，共 ${formatNum(csvRows.length - 1)} 条`)
      }
    } catch (e: any) {
      ElMessage.error(e?.response?.data?.error || e?.message || '导出失败')
    } finally {
      isExporting.value = false
      exportProgress.value = 0
      exportMessage.value = ''
    }
  }

  run()
}

function cancelExport() {
  exportCancelFlag = true
  exportMessage.value = '正在取消...'
}

function exportDetailToJSON() {
  if (!selectedLog.value) return
  const json = JSON.stringify(selectedLog.value, null, 2)
  const blob = new Blob([json], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `audit-${selectedLog.value.id.slice(0, 8)}.json`
  a.click()
  URL.revokeObjectURL(url)
  ElMessage.success('已导出')
}

onMounted(() => fetchData())
onUnmounted(() => {
  if (autoTimer) clearInterval(autoTimer)
})
</script>

<style scoped>
.audit-log-page {
  width: 100%;
}

/* ─── Page Header（与其他页面一致的结构）──────────── */
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
  gap: 16px;
  flex-wrap: wrap;
}

.header-info {
  flex: 1;
  min-width: 200px;
}

.page-title {
  color: var(--el-text-color-primary);
  font-size: var(--font-title-size, 20px);
  font-weight: var(--font-title-weight, 600);
  margin: 0 0 4px 0;
}

.page-desc {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  margin: 0;
  line-height: 1.5;
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

/* ─── KPI 卡片 ─────────────────────────────────── */
.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.kpi-card {
  background: var(--bg-surface, #fff);
  border: 1px solid var(--border-default, #E2E8F0);
  border-radius: var(--radius-base, 8px);
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.kpi-label {
  font-size: var(--font-size-xs, 12px);
  color: var(--text-secondary, #64748B);
  font-weight: 500;
  letter-spacing: 0.5px;
  text-transform: uppercase;
}

.kpi-value {
  font-size: var(--font-size-display, 28px);
  font-weight: 700;
  color: var(--text-primary, #0F172A);
  font-variant-numeric: tabular-nums;
  line-height: 1.1;
}

.kpi-value-sm {
  font-size: 18px;
  font-family: var(--font-mono, 'JetBrains Mono', monospace);
  font-weight: 500;
}

/* ─── Toolbar 字段通用 class（复用全局规范）────────── */
.toolbar-field {
  width: 180px;
}

.toolbar-field-sm {
  width: 160px;
}

.toolbar-field-lg {
  width: 200px;
}

/* ─── 表格 ─────────────────────────────────────── */
.audit-table {
  margin-bottom: 8px;
}

.time-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  line-height: 1.3;
}

.time-main {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  font-variant-numeric: tabular-nums;
}

.time-relative {
  font-size: 11px;
  color: var(--el-text-color-secondary);
}

.user-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

.user-avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--bg-subtle, #F1F5F9);
  color: var(--text-secondary, #64748B);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 13px;
  flex-shrink: 0;
  border: 1px solid var(--border-default, #E2E8F0);
}

.user-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.user-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.user-id {
  font-size: 11px;
}

.action-tag {
  font-family: var(--font-mono, monospace);
  letter-spacing: 0.3px;
}

.resource-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.resource-type-tag {
  flex-shrink: 0;
  font-family: var(--font-mono, monospace);
  font-size: 10px;
  font-weight: 600;
}

.resource-name {
  font-size: 13px;
  color: var(--el-text-color-regular);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.status-chip {
  display: inline-block;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  line-height: 1.4;
  background: var(--bg-subtle, #F1F5F9);
  color: var(--text-secondary, #64748B);
}

.status-chip.is-success {
  background: var(--color-success-bg, #ECFDF5);
  color: var(--color-success, #059669);
}

.status-chip.is-warning {
  background: var(--color-warning-bg, #FFFBEB);
  color: var(--color-warning, #D97706);
}

.status-chip.is-danger {
  background: var(--color-danger-bg, #FEF2F2);
  color: var(--color-danger, #DC2626);
}

.status-chip.is-info {
  background: var(--color-info-bg, #EFF6FF);
  color: var(--color-info, #2563EB);
}

.mono-text {
  font-family: var(--font-mono, 'JetBrains Mono', monospace);
  font-size: 12px;
  color: var(--text-secondary, #64748B);
}

/* 表格行可点击 */
.audit-table :deep(.el-table__row) {
  cursor: pointer;
}

/* ─── 分页 ─────────────────────────────────────── */
.pagination-wrapper {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
}

.page-note {
  font-size: 12px;
  color: var(--text-secondary, #64748B);
  font-variant-numeric: tabular-nums;
}

/* ─── 抽屉 ─────────────────────────────────────── */
:deep(.audit-drawer .el-drawer__header) {
  margin-bottom: 0;
  padding: 18px 20px 14px;
  border-bottom: 1px solid var(--border-default, #E2E8F0);
}

.drawer-header {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.drawer-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.drawer-subtitle {
  font-size: 13px;
  color: var(--text-secondary, #64748B);
  font-variant-numeric: tabular-nums;
}

.drawer-body {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.drawer-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #0F172A);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border-light, #F1F5F9);
}

.kv-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px 16px;
  margin: 0;
}

.kv-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.kv-item dt {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary, #64748B);
  text-transform: uppercase;
  letter-spacing: 0.4px;
}

.kv-item dd {
  margin: 0;
  font-size: 13px;
  color: var(--text-primary, #0F172A);
  word-break: break-all;
  line-height: 1.5;
}

.kv-copyable {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  margin: -2px -6px;
  border-radius: 4px;
  transition: background-color 0.2s;
}

.kv-copyable:hover {
  background: var(--bg-subtle, #F1F5F9);
}

.kv-copyable .el-icon {
  font-size: 12px;
  opacity: 0;
  transition: opacity 0.2s;
}

.kv-copyable:hover .el-icon {
  opacity: 1;
}

.detail-sub-panel {
  margin-top: 10px;
}

.panel-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  color: var(--text-secondary, #64748B);
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.kv-grid-sm {
  grid-template-columns: 1fr;
  gap: 6px 12px;
}

.kv-grid-sm .kv-item {
  flex-direction: row;
  align-items: baseline;
  gap: 8px;
}

.kv-grid-sm .kv-item dt {
  min-width: 120px;
  flex-shrink: 0;
}

.details-json-raw {
  max-height: 240px;
  font-size: 11px;
  opacity: 0.85;
}

.ua-content {
  font-family: var(--font-mono, monospace);
  font-size: 12px;
  color: var(--text-secondary, #64748B);
  background: var(--bg-subtle, #F1F5F9);
  padding: 10px 12px;
  border-radius: 6px;
  line-height: 1.6;
  word-break: break-all;
  border: 1px solid var(--border-light, #F1F5F9);
}

.details-json {
  margin: 0;
  background: var(--text-primary, #0F172A);
  color: #E2E8F0;
  padding: 14px 16px;
  border-radius: 6px;
  font-family: var(--font-mono, 'JetBrains Mono', monospace);
  font-size: 12px;
  line-height: 1.65;
  max-height: 360px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

/* ─── 品牌按钮 ─────────────────────────────────── */
.brand-btn {
  background-color: var(--brand-green, #36B34E);
  border-color: var(--brand-green, #36B34E);
  color: #fff;
}

.brand-btn:hover,
.brand-btn:focus {
  background-color: var(--brand-green-deep, #2A9C4B);
  border-color: var(--brand-green-deep, #2A9C4B);
}

/* ─── 响应式 ─────────────────────────────────── */
@media (max-width: 900px) {
  .page-header {
    flex-direction: column;
    align-items: flex-start;
  }
  .header-actions {
    width: 100%;
    justify-content: flex-start;
  }
  .kpi-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 600px) {
  .kpi-row {
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }
  .kpi-value {
    font-size: 22px;
  }
}

/* ─── 暗色模式 ─────────────────────────────────── */
html.dark .kpi-card {
  background: var(--bg-surface, #1E293B);
  border-color: var(--border-default, #334155);
}

html.dark .details-json {
  background: #0B1220;
  color: #E2E8F0;
}

/* ─── 导出进度条 ─────────────────────────────────── */
.export-progress-bar {
  margin-bottom: 16px;
  padding: 14px 18px;
  background: var(--bg-surface, #fff);
  border: 1px solid var(--border-default, #E2E8F0);
  border-radius: var(--radius-base, 8px);
  display: flex;
  flex-direction: column;
  gap: 10px;
  box-shadow: var(--shadow-sm);
}

.export-progress-info {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 13px;
}

.export-title {
  color: var(--text-primary, #0F172A);
  font-weight: 600;
}

.export-status {
  color: var(--text-secondary, #64748B);
  font-variant-numeric: tabular-nums;
  font-family: var(--font-mono, monospace);
  font-size: 12px;
}

.export-progress-bar :deep(.el-progress-bar__innerText) {
  transition: width 0.3s ease;
}
</style>
